# 多查询结果与 Python 计算：第一步 review

状态：待 review 的设计，尚未实现工具或执行迁移。仅涉及 Agent 自有 MySQL；线上 PostgreSQL 不做任何写入或结构变更。

## 目标与边界

允许 Agent 执行多个受审核的 SQL，再用 Python 合并有限规模的结果。SQL 负责筛选和基础聚合，Python 负责合并、派生指标与统计。结果引用无需模型复制数据，结论可追溯至每次查询与计算。

第一步确定统一结果结构、存储、权限及工具协议。后续才实现沙箱、Agent 调度和前端。现有分支 feature/llm-clarification 继续开发，保留已有改动。

## 当前实现与需要补充的部分

- `querymcp.Result` 已有 result_id、columns、types、rows、truncated、queried_at、实际 SQL 和参数。
- MCP 最多返回 200 行、约 256 KiB 行数据；Agent 的 `preview` 另限制模型预览约 24 KB。两种截断不是一回事。
- `agent.Evidence` 保存 SQL、目的、时间窗口和结果；`dialogueSaved` 将证据放在最终回复 JSON 中。
- 当前轮结果在内存中复用，历史结果从最近完成的会话记录中提取；没有独立、可按权限读取的结果存储。
- `read_result` 只读取当前目录中的历史证据，最多 3 次新查询。新协议需要兼容旧消息，不能把旧数据默认判定为完整或线上。

## 统一结果结构 v1

结果内容不可修改；ID 由服务生成。查询和计算共享下列结构：

| 字段 | 类型 / 规则 |
| --- | --- |
| schema_version | 整数，首版为 1 |
| result_id | 不透明 UUID，保持已有查询结果 ID |
| kind | query / computation |
| purpose | 目的，最多 1000 字符 |
| data_source | live / synthetic，由可信输入确定，不接受模型自行声明 |
| columns | 有序列描述：name、logical_type、source_type（可空） |
| rows | 二维数组，严格匹配列数；不接受重复列名 |
| returned_rows | 服务按 rows 计算，不能由脚本声明 |
| truncated | 数据因服务行数或字节上限而不完整 |
| truncation_reason | null / row_limit / byte_limit / legacy_unknown |
| created_at | 服务生成的 UTC 时间 |
| provenance | 查询信息或计算信息，见下文 |
| limitations | 口径、样本及时间差限制；不能覆盖可信截断状态 |

查询 provenance 保存逻辑 SQL、绑定参数、实际 SQL/参数、窗口、queried_at 和耗时。`truncated=false` 仅表示完整取得该 SQL 的结果，不代表覆盖全部业务数据：SQL 自带 LIMIT、WHERE 或抽样仍须解释。

计算 provenance 保存有序 input_result_ids、输入内容 SHA-256、Python 原文及哈希、运行环境镜像摘要、开始/完成时间和耗时。直接依赖建边，读取时展开祖先，形成“结论 → 计算 → 查询”的有向无环图；禁止覆盖旧结果、循环引用或跨来源混算。第一版深度最多 3。

计算结果不声称有单一查询时间或统一时间窗口；保留每个根查询的窗口和获取时间。分次查询不共享 PostgreSQL 快照，不能声称数据来自同一瞬间。

### 类型与数据完整性

- integer：十进制字符串，Python 转任意精度 int，避免 BIGINT 精度丢失。
- decimal：十进制字符串，Python 转 Decimal；金额计算不得隐式转 float，比例的精度和舍入方式须由代码明确。
- float：仅有限 JSON 数值；NaN/Infinity 拒绝保存。
- string / boolean / date / timestamp：分别保持字符串、布尔、ISO 日期、带时区时间；时间统一 UTC，展示时再转换。
- NULL 保留 JSON null，与 0、空字符串区分；未知数据库类型拒绝进入计算，不自行猜测。
- 所有结果在 Go 边界做字段、类型、长度、行宽和字节校验。不依赖模型 JSON Schema 或 Python 声明代替校验。
- 合并需在代码中明确 join 类型、键是否唯一、缺失值处理；输出检查项记录输入/输出行数、重复键和未匹配数量，减少多对多合并导致重复金额的风险。这些检查提供诊断，不自动证明分析正确。

## Agent MySQL 存储设计

新增两张表，后续通过版本化迁移和 GORM 实现，本次不执行建表。

| 表 | 核心字段 | 索引与约束 |
| --- | --- | --- |
| analysis_results | result_id CHAR(36) ASCII 主键；conversation_id、agent_run_id、step_sequence；kind；schema_version；data_source；purpose；payload JSON；payload_sha256 CHAR(64)；created_at | 索引 (conversation_id, created_at, result_id)；唯一 (agent_run_id, step_sequence)；关联会话和任务，并保证任务属于同一会话 |
| analysis_result_inputs | result_id、input_result_id、ordinal | 主键 (result_id, input_result_id)；唯一 (result_id, ordinal)；双向外键关联结果表 |

payload 是统一结构的完整快照；关键检索字段复制到列，单事务写入且校验一致。首版每条序列化完整 payload 最多 512 KiB，行数据仍保留 200 行 / 256 KiB 上限。计算输入最多 6 份，总 payload 最多 3 MiB。模型预览不存为原始结果。

成功的工具调用先保存结果与依赖关系，再返回 result_id；与本地步骤成功记录同事务提交。持久化失败则工具失败，不让模型引用未保存 ID。相同步骤重试只有内容哈希一致才幂等返回；不重复执行线上 SQL 或覆盖不同结果。

最终回复保存 result_ids，原有 evidence JSON 保留作兼容展示，逐步切换到统一读取；首版不批量搬迁旧消息。旧证据首次显式使用时经所有权、来源、类型及截断校验导入，保留原 ID/查询时间。无法确认完整性的旧结果可展示，但不得计算。

会话删除时在 Agent MySQL 事务中先清理所属依赖边和结果，再按现有逻辑删除会话数据；不允许留下可访问的孤立结果。首版结果与会话同生命周期，界面需要说明结果可能包含邮箱等业务数据，不将其写入普通日志。

### 读取与授权

服务内部接口：`LoadResults(ctx, scope, resultIDs)`；scope 包含已认证用户、当前会话、当前运行及数据源，由服务创建，不能来自工具参数。

- 每次读取联查会话所属用户及任务，ID 本身不授予权限。
- 只允许当前运行已成功保存的结果，或同会话已完成回复所引用的历史结果。失败/取消任务残留结果仅供审计，不自动提供给后续任务。
- 同一用户其他会话也不可直接按 ID 读取；跨会话检索继续只用已有历史工具，不扩大本次数据权限。
- 运行终止、会话不可用或来源不一致，拒绝读取和保存。
- 不存在与越权统一返回 RESULT_NOT_AVAILABLE，防止枚举。

## 工具协议

### read_result（扩展现有工具）

输入仍为 `{ "result_id": "..." }`。返回元数据、来源、血缘及有界预览，增加 `returned_rows`、`preview_rows`、`preview_truncated`。预览最多 20 行且总响应不超过 24 KiB；大单元格可省略并标记，不改变存储数据。计算工具由服务读取完整结果，不调用模型预览作为数据输入。

### analyze_results（新增，沙箱完成后才注册）

```json
{
  "result_ids": ["查询结果A的UUID", "查询结果B的UUID"],
  "purpose": "按客户合并充值和使用汇总",
  "code": "...Python 代码..."
}
```

仅允许以上字段：1–6 个不重复 ID、purpose 1–1000 字符、code 1–32 KiB。不接受用户/会话 ID、文件路径、网络地址、数据库连接或任意依赖列表。

Go 服务先鉴权、验证所有输入、预算及来源，再把规范化输入送入隔离沙箱。沙箱预置 `inputs[result_id]` 数据表和只读元数据，整数和 Decimal 由固定适配器解码；脚本通过 `emit_result(columns, rows, limitations, checks)` 输出一张表。模型不能指定结果 ID、血缘、来源或成功状态。固定输出协议必须再经 Go 验证。

成功返回 `{ "result_id": "...", "kind": "computation", "input_result_ids": [...], "data": {...}, "preview_truncated": false }`，其中 data 是上述统一结构的有界预览，不是全部存储内容。最终回答 result_ids 可以引用查询或计算结果。

失败返回 `{ "error": { "code": "...", "message": "...", "retryable": false } }`，不产生结果 ID。错误码至少包括：RESULT_NOT_AVAILABLE、SOURCE_MISMATCH、INPUT_TRUNCATED、UNSUPPORTED_TYPE、INVALID_ARGUMENT、BUDGET_EXCEEDED、COMPUTE_TIMEOUT、COMPUTE_FAILED、INVALID_OUTPUT。修正代码可以重新调用，但仍消耗预算；错误信息截断并清除凭据、客户值和宿主路径。

第一版只接受未截断且完整性已知的输入。输出超过上限直接失败，不保存可供下轮计算的部分结果。模型预览截断不影响计算；任何祖先实际数据截断均阻止计算。

## 后续执行预算与沙箱边界

建议初始预算：每任务最多 6 次 SQL 执行、3 次计算尝试；每条 SQL 仍审核，单条 10 秒，累计 SQL 执行墙钟时间最多 30 秒，线上查询并发仍为 1。尝试失败也计入预算，不用拆分或重试绕过限制。计算每次最多 10 秒、512 MiB 内存、1 CPU，整个对话仍受总截止时间约束。

上述为后续配置默认值提案，需同步调整 Agent 现有轮数/工具次数限制并测试耗尽行为，本次不改变运行配置。跨会话 worker 并发与进度显示是另一个增量，不与本设计混改。

Python 必须在独立隔离进程/容器执行：无网络、无数据库/API 凭据、非 root、只读根目录、受限临时目录、无宿主或 Docker socket 挂载、限制资源并随任务取消强制终止。固定依赖与镜像，不允许运行时安装包；具体隔离实现属于第二步。不能把 AST 过滤或禁用 import 当作沙箱。

## 验收与实施顺序

1. 本次 review：确认结构、MySQL 两表、同会话权限、类型/截断规则和工具输入输出。
2. 实现结果库与兼容适配：使用隔离 MySQL 测试真实事务、幂等、越权、取消后写入拒绝和会话删除；现有回复展示不回退。
3. 接入隔离计算工具：只用人工数据验证精确金额、空值、重复键、截断、资源耗尽、网络/文件隔离；通过后注册 Agent 工具。
4. 调整 Skill、预算与证据界面，验证“两个 SQL → 合并计算 → 引用结论 → 刷新读取”端到端流程，覆盖失败和重试。

行为实现交付前运行 make check，持久化测试指定 TEST_MYSQL_DSN 的 _test 库；PostgreSQL 相关测试也只用 _test。当前设计稿没有运行时改动，不代表上述测试或沙箱已经通过。

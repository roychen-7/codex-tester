# 线上只读接入：结构核对与范围 review

2026-10-15。第一增量完成元数据核对；第二增量已实现应用层受限投影、来源隔离和线上启动配置。

## 不变的边界

线上 PostgreSQL 禁止任何业务数据写入、DDL、迁移、授权、建视图、建索引、ANALYZE。模型不能发起管理命令。只使用现有表和当前账号权限。会话和受限查询结果仍存到 Agent 自有 MySQL。

`make query-inspect` 从根目录 `.env` 加载已配置连接，执行固定的元数据检查，不调用 LLM、不读取业务行、不读取 syslog。使用 GORM 和参数绑定，显式只读事务；事务内只设置本次连接的语句超时和锁等待超时，不修改数据库持久配置。每条语句最多 3 秒，锁等待最多 1 秒，结构检查最多 15 秒。报告仅包含字段名、类型、可空性、权限和现有索引定义；索引定义中的 CREATE INDEX 是元数据文本，绝不会执行。

## 已核实的线上结构

PostgreSQL `16.2-ucloudrel1`，检查事务 `transaction_read_only=on`。下列五张表存在，当前账号具有 public schema 使用权限和表 SELECT 权限，所列字段均可读取；这不代表字段已向 Agent 开放。

| 表 | 字段数 | 索引数 | 核对结果 |
| --- | --- | --- | --- |
| public.users | 27 | 9 | 有 id 主键、status/deleted_at 索引；未发现 created_at 索引 |
| public.payment_orders | 40 | 9 | 有 paid_at、created_at、user_id、status、payment_type + paid_at 索引 |
| public.usage_logs | 63 | 26 | 有 created_at、user_id + created_at、model + created_at 等索引 |
| public.redeem_codes | 12 | 6 | 有 used_by、status 等索引；未发现 used_at 索引 |
| public.usage_dashboard_daily | 12 | 2 | bucket_date 主键和日期索引；有 computed_at 字段 |

只读取系统目录，未执行 COUNT(*)、业务抽样或统计，也未执行 EXPLAIN。索引存在不能证明某个统计查询开销可接受，后续须对具体 SQL 检查计划。不重新估算 6.4 GB 日志表大小。

## 第一版开放范围

| 表 | 明确允许的字段 | 用途与边界 |
| --- | --- | --- |
| users | id, email, role, status, created_at, deleted_at, balance, total_recharged, signup_source, last_login_at, last_active_at | 客户标识、注册与余额；支持 id 或 email 查找客户，可展示邮箱；不暴露用户名、备注或认证信息 |
| payment_orders | id, user_id, amount, pay_amount, refund_amount, status, order_type, payment_type, paid_at, completed_at, refund_at, created_at | 先支持按 paid_at 窗口的订单分析；不直接用累计值推算本月收入 |
| usage_logs | id, user_id, model, requested_model, upstream_model, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens, total_cost, actual_cost, billing_type, duration_ms, first_token_ms, created_at | 按 created_at 限制窗口；统计客户使用、模型分布、耗时，排除密钥关联和请求追踪信息 |

`syslog` 完全排除。密码哈希、TOTP 密文、IP、支付链接、二维码、充值码、供应商配置快照、自由文本备注等不进入模型可查询字段。账号底层拥有 SELECT 权限不等于 Agent 可以读取任意列，必须由应用层执行字段隔离。

`redeem_codes` 和 `usage_dashboard_daily` 本轮只核对结构，先不开放。兑换金额不能直接与支付订单金额相加；日报需先核对时区、汇总口径和更新时效，不能把每日活跃人数相加当作月活。

## MCP 适配实现

1. 增加显式数据源模式，模拟模式与线上只读模式隔离；来源标记随查询结果保存，旧模拟结果不能被当作线上证据。同步调整 Skill、描述工具和界面提示。
2. 保留模型侧受控逻辑表名。线上模式在通过 AST 校验之后，将每个允许关系转换成服务生成的子查询：明确字段投影 + public 原表 + 参数化时间边界。它是 SELECT 的一部分，不是数据库视图，也不创建任何对象。不得直接把 analytics 前缀替换成 public，否则会泄露字段并丢失强制窗口。
3. 用户表用于客户关联，不强制限定注册日期，以免漏掉窗口内活跃的老客户；订单强制 paid_at、日志强制 created_at 的左闭右开窗口，最长 31 天。注册日期筛选由问题明确。缺少必要权限或字段直接报告限制，不执行任何修复权限 SQL。
4. 原有 AST 函数和语法允许列表、只读事务、EXPLAIN（不含 ANALYZE）、成本限制、单并发、查询超时、行数和输出体积上限继续生效；查询与检查必须覆盖真实执行的投影 SQL。
5. 隔离测试验证敏感列、整行引用、跨 schema、子查询/别名绕过、写入、超时、时间窗口和来源串用；通过后再用线上小时间范围只读验收，不在业务库运行测试或迁移。

## 需要在具体问题中明确的口径

- 充值是实付金额、到账额度还是扣退款净额；按 paid_at 还是 completed_at；包含哪些订单类型和状态。
- “活跃/留存”按登录还是 API 使用，基准人群、回访区间、时区是什么。KA 不预设固定定义。
- 使用日志行数不自动代表成功请求，total_cost 与 actual_cost 的业务意义需要结合 v0.2.7 源码确认。

这些口径由 Agent 在收到具体问题时按需追问，不要求用户在接入阶段一次性定义完。

# DDSHUB Agent 工作台

当前默认提供连续对话：直接回答、按需追问、按需调用数据库 Skill/MCP、带历史依据继续，以及停止和重试。旧的“确认需求 → Skill → MCP → 模拟数据分析”流程保留为 legacy 模式，供重构期间回归。前端使用 React + TypeScript + Ant Design + CSS Modules；后端使用 Go 1.25+、Gin、GORM；Agent 自有数据存储于 MySQL 8.0。

本阶段尚未接入跨会话历史检索和线上 ddshub PostgreSQL，统计结论仅基于隔离的人工模拟数据。没有公开注册、找回密码、分享、附件、消息编辑或会话删除入口。

消息区按角色区分：用户消息靠右、浅绿色，Agent 回复靠左、白色。Agent 回复支持 Markdown 标题、列表、引用、代码块和 GFM 表格；用户原文保留换行。查询依据与回复之间留有独立间距，代码与宽表格在区块内滚动。Markdown 不执行原始 HTML，也不自动加载外部图片。

## 连续对话与数据库 Skill（重构第 2 步）

`AGENT_MODE=conversation` 为默认模式。每条用户消息直接进入同一个对话执行入口，模型根据当前问题、最近消息和累计摘要决定回答还是追问。没有必经的需求表、字段清单或确认按钮；问题充分时可以直接回答。`waiting_for_user` 仅记录模型正在等待补充，不强制每次进入该状态。

```sh
docker compose up -d --wait api web  # 显式迁移 006 后启动新模式，保留旧数据
# 可选真实模型检查，会产生模型用量；测试内容不写入会话
docker compose exec api go run ./cmd/manage check-dialogue
```

可以先问“留存和转化有什么区别？”，再讨论“我想优化网站”，回答 Agent 的追问后继续。当前对话保留最近最多 32 条消息（历史正文约 40,000 字节）及最多 12,000 字节累计摘要，最新用户消息完整保留。超出窗口的旧消息通过摘要保留关键信息，原始消息仍在 MySQL；跨会话检索尚未实现。

006 迁移新增 `agent_runs` 与 `agent_run_steps`，旧任务和分析表不删除。每条用户消息对应一个执行尝试，状态为 pending/running/waiting_for_user/succeeded/failed/cancelled。追问与普通回答都保存为 assistant 消息；等待用户时释放请求、清除截止时间，不自动重跑或因等待超时失败。用户补充后创建下一轮并携带已有上下文。回复、摘要、状态和步骤在同一事务提交。

生成过程中可继续补充：新消息在当前会话事务中取消旧执行并排队新一轮，worker 传播取消，旧回复不能晚到后写入。关闭页面不取消执行。模型请求超时、鉴权失败、限流/额度不足及上游不可用会显示各自的脱敏提示；日志仅记录运行 ID 和错误类别，不记录凭据或上游响应正文。失败或手动停止可重试；重复重试复用后继尝试，旧消息不能覆盖更新的用户输入。排队最多 5 分钟，执行总预算 10 分钟，每次模型请求受 OPENAI_TIMEOUT_SECONDS 限制，服务重启后过期运行标记失败，不自动重发已开始的模型请求。

配置 MCP 后，模型根据任务选择数据库 Skill，按需加载资料、查看结构、准备并执行只读查询，无需再点击确认。可以先查询明确的部分，再追问缺失的业务口径；用户补充后继续分析。普通讨论不建立 MCP 连接，MCP 暂时不可用时仍可解释限制或继续讨论。**当前只有模拟数据库能力，尚无搜索或网页抓取工具，也未连接线上库。** 模型是否追问由模型判断，提示词和测试不能保证每次都符合用户预期。

查询依据保存在现有 `agent_runs.result` JSON 中，与回复、状态、摘要同事务提交，不新增或修改迁移。每个模型/工具步骤及用量记录在 `agent_run_steps`，前端显示当前执行阶段。对应 assistant 消息下展示表格、SQL、参数、时间窗口、结果 ID 和查询时间，刷新后保留；等待补充的回复也可以带查询依据。

下一轮从当前会话最近 4 个带依据的已完成/等待运行中，选取最多 3 个不同的历史结果。先给模型结果目录，模型调用 `read_result` 后才获得有界预览；引用校验拒绝未读取的历史结果或不存在的 ID。读取历史依据不调用 PostgreSQL，即使 MCP 未配置也可续聊。时间或口径变化时应重新查询；模型摘要不是统计依据。失败、取消或中途被新消息替代的运行不发布部分结果；重试会重新执行，不能恢复旧查询票据。

每轮最多 10 次模型请求、8 次工具调用、3 次实际查询，保留 MCP 原有窗口、EXPLAIN、只读权限、行数和超时限制。新查询及被引用历史结果随回复保存；不会自动分页拉全量日志。当前已登录用户共享同一个模拟业务数据集，历史会话依据仍按账号和会话隔离。

`AGENT_MODE=legacy` 可以回归旧流程；`make dev-analysis` 的覆盖文件固定开启 conversation。模式切换不批量取消其他会话任务；旧模式队列在该模式 worker 恢复时按原截止时间处理。切换前应让当前任务结束或主动停止；不同模式不要同时对同一数据库部署 worker。历史消息始终可读，旧分析表格可切回 legacy 查看。兼容模式暂时保留，用于已有数据查看和回归。

## 旧模式：LLM 与需求澄清

在根目录 `.env` 配置 `OPENAI_API_KEY` 和 `OPENAI_MODEL`；模型须支持 Responses API 和严格 JSON Schema 输出。默认 `OPENAI_BASE_URL=https://api.openai.com/v1`、`OPENAI_TIMEOUT_SECONDS=120`。两项必填配置均为空时，仅保存消息；只配置其中一项会拒绝启动。

```sh
# .env 修改后重新创建 API，使环境变量生效
docker compose up -d --no-deps api
# 可选：发送一个固定测试问题，校验连接与结构化输出（会产生模型用量）
docker compose run --rm --no-deps api go run ./cmd/manage check-llm
```

发送消息后，后端在同一个 GORM 事务中保存消息并加入 MySQL 调用队列。后台 worker 调用模型，前端轮询状态并显示完整回复，暂不提供流式输出。关闭网页不会取消调用；重启后可继续处理未开始的队列，超时运行显示失败，用户可重试。单进程串行调用，同会话生成期间禁止再次发送；排队超过 5 分钟会失败。

模型根据任务判断必要信息，每轮最多追问三个问题，不预定义 KA 等业务概念。需求卡片区分用户说明、历史参考、模型建议；信息充分后需要用户点击确认。确认后继续补充会重新进入澄清。当前一个会话维护一份需求，独立任务请新建会话。配置 MCP 时，“确认并分析”在同一事务中排队执行已确认版本；未配置时只确认需求。

上下文包含当前会话摘要、既有需求和最近最多 32 条消息；历史消息正文总量限制约 40,000 字节，最新用户消息完整保留。摘要由模型累计生成，原始消息不删除。历史内容仅作参考；模型可能误解口径，应检查需求卡片再确认。

调用快照、状态、用量及脱敏错误保存在 MySQL；重试新增尝试记录，重复点击不会重复排队，同一触发消息最多保存一次成功回复。上游超时后手动重试可能产生额外模型用量。请求使用 `store: false`，API Key 只传给后端，不写入调用记录。当前会话的消息和摘要会发送给配置的模型服务。

## 连续对话查询体验（仅模拟数据）

```sh
make dev-analysis   # 启动模拟 MCP，保留 MySQL 数据并迁移，启用连续对话查询
# 可选：真实模型与模拟 MCP 联调，会产生模型用量；不写入会话
docker compose -f compose.yaml -f compose.agent-query.yaml exec api go run ./cmd/manage check-agent
```

打开 http://127.0.0.1:5173，新建会话，输入：“按 UTC 统计 2026 年 9 月所有客户每天已完成的 balance 充值订单，金额取 pay_amount，不扣退款，给出金额和订单数表格。”无需额外确认，可看到执行进度、回答及对应消息下的查询表格和 SQL/参数/时间窗口。然后可以继续问“沿用刚才的数据，平均每笔充值是多少？”模拟数据的预期金额为 9 月 1 日 150、9 月 2 日 20。

Skill 位于 `apps/api/internal/agent/skills/ddshub-analysis/`，内嵌到后端二进制。先向模型提供简介，由模型调用 `load_skill` 和 `read_skill_reference` 读取固定允许的文件；后端通过 MCP 发现三个允许的工具，映射为 Responses 函数工具。模型决定调用，后端校验和执行，将结果传回模型继续下一轮。凭据及可信调用身份不来自模型；`store:false` 时保留当次工具输出和加密 reasoning 项用于多轮继续，内部推理不落库或展示。

新模式使用一个串行对话 worker，旧模式分析队列和澄清队列各有一个 worker。每次分析最多 10 分钟、10 轮模型请求、8 次工具调用、3 次查询，单次模型超时仍由 OPENAI_TIMEOUT_SECONDS 控制。查询结果会发送给配置的模型服务，单次预览约 24 KiB，完整受限表格在 MySQL 保存。当前所有已登录 Agent 用户可分析同一模拟运营数据集。

005 迁移新增 `analysis_runs`（需求版本、Skill 哈希、快照和状态）、`analysis_steps`（工具输入输出与模型用量）、`query_results`（SQL、参数及结果）。成功回复与结果同事务提交，重复确认不重复执行。取消或补充需求后传播取消，阻止旧版本提交；上游可能已产生模型用量。失败可手动重试；重启不恢复执行到一半的工具调用，超时后标记失败。分析中发现口径缺失会回到澄清，需再次确认。刷新可恢复结果；跨会话历史检索仍未实现。

默认 `make dev` 不启用模拟连接；`make dev-analysis` 使用单独 Compose 覆盖文件，不修改真实 `.env`。停止主项目用 `make down`，停止模拟 MCP/PG 用 `make query-down`。

## 容器开发

需要 Docker Desktop、make 和 Python 3。根目录执行：

```sh
make init-env
make dev
# 另一个终端创建账号，密码交互输入，不回显、不进入命令行参数
make create-user USERNAME=roy
```

`make init-env` 只为缺失/为空的 MySQL 密码生成随机值，保留已有非空配置；`.env` 不提交。MySQL 数据卷初始化后不要直接修改密码变量，应先在数据库内执行相应密码轮换。

打开 http://127.0.0.1:5173 。API 健康检查为 http://127.0.0.1:13000/health 。MySQL 在 `127.0.0.1:13306`，数据库 `agent`，账号 `agent`；密码在本地 `.env`。MySQL root 密码只传入数据库容器，不传给 API 或前端。

容器名和数据卷按 Compose 项目隔离。API 启动前运行显式迁移命令，MySQL 健康后启动 API，API 健康后启动 web。也可后台运行 `docker compose up --build -d --wait`。

```sh
make logs
make down
```

`make down` 保留 MySQL、依赖及构建缓存卷。不要用 `down -v` 作为日常停止命令。真实业务 ddshub PostgreSQL 不参与本项目的开发或测试。

## 账号与登录

用户名为 3–64 位小写字母/数字/点/下划线/连字符，以字母或数字开头。密码为 12–72 字节，使用 bcrypt 加盐哈希。`make create-user USERNAME=roy DISPLAY_NAME=Roy` 创建账号；同名账号报错，不覆盖密码。

登录令牌由安全随机数生成，数据库只存 SHA-256 哈希。HttpOnly、SameSite=Strict Cookie 保存登录状态；默认固定有效期 7 天，退出立即撤销当前浏览器会话。每次鉴权检查账号是否禁用。此阶段不提供禁用或改密页面。

写请求必须包含 `X-Agent-Request: 1`，拒绝跨站 Fetch 请求且不开放跨域 CORS，防止浏览器 CSRF。每个来源 IP 在 15 分钟内最多允许 20 次登录尝试。当前限速为单实例内存实现；不信任未经配置的代理 IP 头，部署到代理后须单独评估限速粒度。

## 表结构与迁移

实际迁移文件位于 `apps/api/internal/database/migrations/`：001 为会话基础，002 增加消息所属会话的复合唯一键，003 增加会话摘要，004 创建需求与模型调用表。第一阶段设计说明：`docs/schema/agent-session-v1.draft.sql`。使用 MySQL 8.0.44；CHECK 约束要求至少 8.0.16。

- `agent_users`：Agent 用户，与 ddshub 客户独立。
- `auth_sessions`：登录令牌哈希、到期和撤销时间。
- `conversations`：所属用户、标题、活动时间。
- `messages`：消息正文、角色、请求 ID；唯一键 `(conversation_id, request_id)` 防重复。
- `schema_migrations`：迁移版本、校验和及执行时间（基础设施表）。
- `agent_tasks`：任务来源消息、动态需求 JSON、待澄清问题数组、需求版本及确认时间。
- `llm_runs`：每次模型调用尝试，保存上下文快照、结果、用量、脱敏错误及执行时间；未知用量为 NULL。

会话摘要使用 `context_summary` 和 `summary_through_message_id`；二者必须同时为空或同时填写，摘要来源必须属于当前会话。摘要是内部上下文，不随现有会话列表接口输出，原始消息仍保留。

需求状态为 `clarifying` → `awaiting_confirmation` → `confirmed`，支持 `cancelled`。同一会话最多有一个处于澄清/待确认状态的任务；confirmed 表示需求已确认，不代表分析完成。修改需求时服务层必须增加 revision 并重新确认。需求项应标记来源消息及用户说明、历史参考、模型建议，历史内容不自动视为当次确认。

调用状态为 `pending`、`running`、`succeeded`、`failed`、`cancelled`。数据库通过生成列与唯一键保证同会话最多一个 pending/running、同触发消息最多一个 succeeded，且 `(trigger_message_id, attempt_no)` 唯一；每次重试新增记录。复合外键约束任务、来源消息、回复消息属于同一会话。API Key 和原始上游错误不得写入调用记录。

服务层校验用户归属、消息角色、JSON 内引用、需求版本和状态流转。完成调用时锁定会话和调用记录，检查 running 状态、截止时间和需求版本；保存回复、更新需求和摘要处于同一个 GORM 事务，外部模型请求在事务外进行。确认接口必须登录，并提交当前需求版本；过时版本返回 409。

所有时间以 UTC 存取，浏览器按本地时区展示；BIGINT ID 在 JSON 中输出字符串。消息按 ID 游标分页；会话按 `(updated_at, id)` 倒序分页。保存消息、更新会话时间在同一 GORM 事务中完成，锁定会话序列化并发写入。

相同请求 ID 与相同正文重试返回原消息，不更新会话时间；相同 ID 改正文返回 409。接口校验消息非全空白、最多 10,000 个 Unicode 码点，请求体最多 64 KiB。原正文换行与空白保留。用户隔离通过所有会话操作的 owner 条件执行；跨用户访问返回 404。

迁移通过 `cmd/manage migrate` 执行，不使用 AutoMigrate。建表使用 IF NOT EXISTS，持有连接级迁移锁，成功后登记校验和；MySQL DDL 不支持整批事务回滚。002/003 各为一个原子 ALTER：若 DDL 成功但登记前中断，重跑通过结构标记恢复登记，标记不完整时报错。健康检查校验全部内嵌迁移记录及校验和。已执行迁移不得修改，应新增后续编号文件。禁止对任意已有同名业务库执行本迁移。

```sh
# MySQL 已运行时，单独执行迁移
make migrate
```

## 环境配置

后端统一通过 `internal/config` 校验环境变量，不自行读取 `.env`；Compose 从根目录 `.env` 读取，并只传入各服务需要的值。

| 变量 | 默认值 / 作用 |
| --- | --- |
| APP_ENV | local；仅 local/production |
| LOG_LEVEL | 未设置或为空时 local=debug、production=info；支持 debug/info/warn/error |
| HOST / PORT | 本机 127.0.0.1:3000；容器 0.0.0.0:3000 |
| WEB_PORT / API_PORT | 宿主机 5173 / 13000 |
| MYSQL_HOST / MYSQL_PORT | 本机 127.0.0.1 / 13306；容器 mysql / 3306 |
| MYSQL_DATABASE / MYSQL_USER | 本机默认 agent / agent；开发 Compose 固定使用 agent |
| MYSQL_PASSWORD | 必填；`make init-env` 生成 |
| MYSQL_ROOT_PASSWORD | 仅用于 MySQL 容器初始化 |
| MYSQL_TLS | false；可设 true，使用验证服务器证书的 TLS |
| SESSION_TTL_HOURS | 168；范围 1–720 小时 |
| COOKIE_SECURE | 本机 local 默认 false，production 默认 true 且禁止 false；Compose 的生产配置需显式设 true |
| OPENAI_API_KEY | 模型服务凭据；仅后端使用 |
| AGENT_MCP_URL / AGENT_MCP_TOKEN | 后端连接 MCP；同时为空禁用，Token 至少 32 字节；生产要求 HTTPS |
| AGENT_MODE | 默认 conversation；legacy 为旧确认分析流程的临时兼容模式 |
| OPENAI_MODEL | 支持 Responses API 和结构化输出的模型名，与 Key 一起配置 |
| OPENAI_BASE_URL | https://api.openai.com/v1；不包含 /responses，生产要求 HTTPS |
| OPENAI_TIMEOUT_SECONDS | 120；范围 10–300 秒 |
| TEST_MYSQL_DSN | 仅集成测试读取；必须连接名字以 `_test` 结尾的数据库 |
| E2E_MYSQL_PORT | 仅浏览器测试启动器读取；默认 13307，CI 使用 3306 |

生产部署需要 HTTPS、正确的 Cookie/TLS 设置和独立的迁移权限安排；当前 Compose 是本地开发环境，不是生产部署模板。

## 验证

```sh
# 启动独立测试 MySQL，端口 13307，数据仅存 tmpfs
make test-db

# 完整本机检查（包含真实 MySQL 权限、持久化、并发和回滚测试）
TEST_MYSQL_DSN='agent:agent-test-only@tcp(127.0.0.1:13307)/agent_test' make check

# 容器内完整后端/前端检查，自动使用隔离测试库
make check-docker

# 浏览器测试，首次安装 Chromium
pnpm --dir apps/web exec playwright install chromium
make check-e2e
```

`make check` 执行 Go 格式检查、Git Flow 规则测试、go vet、竞态测试、API/管理命令构建，以及前端格式、类型和构建检查。未设置 `TEST_MYSQL_DSN` 时，真实 MySQL 集成测试会明确跳过，不能将其视为已验证。

数据库测试还覆盖重复迁移、ALTER 已提交而登记缺失时的恢复、缺失/错误校验和拒绝就绪、GORM JSON 往返、任务确认字段、并发调用唯一键、重试记录与跨会话外键。数据测试在事务中回滚，不删除业务库数据。

端到端测试在测试库创建专用账号，启动独立 API 13001、web 15173 和模拟模型服务 13002，验证登录、会话切换、重命名、发送、刷新恢复、退出、用户隔离、手机布局、发送响应丢失后的重试，以及澄清、确认、需求修改和模型失败重试。启动器强制使用模拟凭据和服务，不调用真实模型。测试账号密码只用于隔离测试库，不在正常 agent 库中创建。

CI 已配置 MySQL 服务和上述检查、浏览器测试；配置存在不代表云端运行已通过。Ant Design 当前传递依赖的声明存在类型冲突，浏览器 tsconfig 使用 skipLibCheck 跳过依赖声明检查，项目源码仍启用 strict，接口响应仍做运行时校验。

## 本机开发与代码目录

本机需要 Go 1.25+、Node.js 22.13+、pnpm 11.3.0、Python 3 和 make。`make install` 安装依赖；宿主机与容器 node_modules 保持分离。

```sh
# 已通过 Compose 启动 MySQL 后，在可信的本地 .env 中导出配置
set -a
source .env
set +a
PORT=13001 make dev-local
```

`make dev-api` 编译后启动 API，`make dev-web` 启动 Vite，`make build` 构建 API、manage 和前端。`make format` 统一使用 gofumpt / Prettier 格式化；gofumpt 通过 Go tool 依赖固定版本，无需全局安装，首次运行需要下载工具依赖。`make check`（包括 CI）和 `make check-docker` 均检查格式。

Go 编码以[官方 Code Review Comments](https://go.dev/wiki/CodeReviewComments)为基线，参考 [Google Go Style Guide](https://google.github.io/styleguide/go/guide)。调用和错误检查相邻，不同逻辑步骤之间空一行；相关语句保持紧凑。gofumpt 处理机械格式，逻辑分组仍需人工判断，具体约定见 AGENTS.md。

Vite 开发代理将 `/api` 转发给 API；实际部署时需同源反向代理。

```text
apps/api/cmd/server             HTTP 服务
apps/api/cmd/manage             数据库迁移、初始化账号、LLM 连接检查
apps/api/internal/config       环境配置与校验
apps/api/internal/database     MySQL 连接及版本化 SQL 迁移
apps/api/internal/chat         GORM 模型、登录、会话、消息存储
apps/api/internal/llm          OpenAI Responses 适配、提示词、结构化输出校验
apps/api/internal/handler      HTTP 路由、权限、输入校验及集成测试
apps/web/src                   登录页、聊天工作台、接口校验、CSS Modules
apps/web/e2e                   浏览器验收测试
```

## 接口

成功返回 `{"data": ...}`，错误返回 `{"error":{"code":"...","message":"..."}}`。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | /health | 数据库与迁移就绪检查 |
| POST | /auth/login | 用户名、密码登录 |
| POST | /auth/logout | 撤销登录，204 |
| GET | /auth/me | 当前用户 |
| GET / POST | /conversations | 分页列表 / 新建 |
| GET / PATCH | /conversations/:id | 会话详情 / 修改标题 |
| GET / POST | /conversations/:id/messages | 分页消息 / 保存消息 |
| GET | /conversations/:id/agent | 当前需求、最近调用状态及模型是否启用 |
| POST | /conversations/:id/tasks/:task/confirm | 提交 revision，确认当前需求 |
| POST | /conversations/:id/runs/:run/retry | 空 JSON 对象，重试最近失败调用 |
| POST | /conversations/:id/tasks/:task/analyze | revision、retry；开始或重试分析 |
| POST | /conversations/:id/analysis/:run/cancel | 空 JSON 对象，取消分析 |
| GET | /conversations/:id/analysis/:run/result | 已完成分析的回答、SQL 和查询结果 |
| GET | /conversations/:id/messages/:message/evidence | 当前用户会话内某条回复的查询依据 |
| POST | /conversations/:id/turns/:run/retry | 空 JSON 对象，重试连续对话的失败或取消尝试 |
| POST | /conversations/:id/turns/:run/cancel | 空 JSON 对象，停止当前对话回复 |

分页默认 20 条、最多 100 条。会话使用 `cursor`，消息使用 `before`，结果包含 `items` 和 `next_cursor`。所有会话与消息接口必须登录；客户端不能指定 owner 或发送 assistant 角色。之前的 `/tasks` 演示接口已不再由服务提供。

## Git Flow 与 review

`main` 是发布分支，`develop` 是集成分支。`feature/<主题>` 从最新 develop 创建，完成后 PR 回 develop；普通修复使用 `feature/fix-<主题>`。`release/<版本>` 从 develop 创建，`hotfix/<主题>` 从 main 创建，均 PR 合入 main 并回合 develop；也允许 main → develop 同步 PR。

主分支禁止直接提交或推送，合并需要他人 Review 和 GitHub 必需检查。发布与回合使用 merge commit 保留祖先关系，关闭强制线性历史。发布标签 `vX.Y.Z` 对应 main 上的发布提交；回合完成前保留来源分支。安装本地 hooks 使用 `make install-hooks`，它不能替代 GitHub 保护。

每完成一个可 review 的步骤暂停。提交、推送、PR、合并和发布按当前任务授权执行，稳定约定见 AGENTS.md。

## PostgreSQL 查询 MCP 与聊天分析

独立 Go MCP 服务提供 `describe_dataset`、`prepare_query`、`execute_query`。已通过后端 Agent 工具循环接入聊天：按需读取 Skill 和数据字典，然后准备查询、执行查询并返回带依据的回答。尚未接入线上库。

```sh
make query-dev       # 独立 Compose 项目；PostgreSQL 15432，MCP 13003
make query-smoke     # 只访问本地模拟服务，验证每日充值金额及人数
make query-down      # 停止模拟环境，tmpfs 数据随容器删除
```

测试环境固定使用 PostgreSQL 17，实际生产版本尚未核验。数据是人工构造的字段子集，不是线上备份或完整 sub2api 安装。结构依据本地 sub2api `v0.2.7`（`7484192016807acf55c6ef4f2827d371eb61ec1c`）的 `backend/ent/schema/payment_order.go`、`usage_log.go`、`user.go`；日志表实际名为 `usage_logs`。`COMPLETED` 仅用于测试口径，真实金额、退款及成功请求定义仍须业务确认。

只读角色仅可访问三个 `analytics` 视图：`users`、`payment_orders`、`usage_logs`。不暴露 email、IP、密钥等字段，也不能访问原始表或 `syslog`。订单视图按 `paid_at`、日志视图按 `created_at`，由服务在只读事务中设置强制时间窗口，左闭右开，最长 31 天。无支付时间的订单不在订单视图结果内。部署其他数据源前必须由管理员核验等价视图、角色权限、字段类型、索引和数据规模；本阶段的初始化 SQL 仅用于全新的测试库。

`prepare_query` 接收 `sql`、`parameters`（字符串数组，使用 `$1` 等参数）、`purpose`、`window_start/window_end`（RFC3339）。使用 PostgreSQL 原生解析器做 AST 允许列表校验，支持单条 SELECT、基本聚合、连接和子查询；第一版不支持 CTE、窗口函数、自定义类型/函数/操作符、锁定查询和多语句。SQL 显式使用 `analytics.<view>`。查询计划用 `EXPLAIN (FORMAT JSON)`，不执行 ANALYZE；总估算成本不得超过 100000，顶层估算行数不得超过 100000。计划成本只是初步预算，不能保证实际执行耗时。

`execute_query` 只能使用准备阶段返回的 `query_id`，不能替换 SQL 或参数。票据在服务内存中保存，绑定可信调用方身份（`X-Agent-Subject`），两分钟过期、最多保存 256 个、只能消费一次；重启或失败后重新 prepare，不自动重复查询。当前单进程最多执行一条查询，超时 10 秒、锁等待 1 秒；取消 HTTP 请求会传递到查询上下文。返回最多 200 行、行数据约 256 KiB，超限标记 `truncated`，不自动翻页。BIGINT 和精确金额以字符串保留，另返回列类型。无完整结果时不得据此推断整体统计。

MCP 使用官方 Go SDK 的 Streamable HTTP（固定依赖版本），仅 `/mcp` 路径，要求 Bearer Token 和 `X-Agent-Subject`，拒绝浏览器 Origin，不开放 CORS。身份头必须由持有服务凭据的后端生成，不能信任模型参数。当前视图是同一运营数据集；身份绑定隔离查询票据，不提供业务客户层面的行级授权。

| 新变量 | 用途 |
| --- | --- |
| QUERY_POSTGRES_DSN | 独立 MCP 的 PostgreSQL URL，必填；生产要求 sslmode=verify-full |
| QUERY_MCP_TOKEN | 独立 MCP 服务凭据，至少 32 字节 |
| QUERY_MCP_ADDRESS | 默认 127.0.0.1:13003；容器内设 0.0.0.0:3001 |
| TEST_POSTGRES_DSN | 仅测试，必须连接 `_test` 后缀的库 |

`compose.query-test.yaml` 使用公开的专用测试凭据且只绑定本机端口，不读取真实数据库凭据，不是生产部署模板。聊天 API 只持有 MCP 服务凭据，不接收 PostgreSQL 凭据。AST 解析器依赖 CGO；本机需要 C 编译器，开发 Go Bookworm 镜像自带所需工具链。

```sh
make test-db
make test-query-db
TEST_MYSQL_DSN='agent:agent-test-only@tcp(127.0.0.1:13307)/agent_test' \
TEST_POSTGRES_DSN='postgres://analyst:analyst-test-only@127.0.0.1:15432/sub2api_test?sslmode=disable' \
make check
```

测试覆盖实际 PostgreSQL 聚合、参数注入、角色权限、时间窗口、超时、票据隔离/过期/并发，以及官方 MCP 客户端发现和调用工具。CI 配置初始化独立模拟库并执行测试；未配置 `TEST_POSTGRES_DSN` 时测试明确跳过。

`make check-e2e` 还需要先执行 `make test-query-db`；依次验证 conversation 和 legacy 两种模式，固定使用测试库及模拟模型凭据。新模式覆盖直接回答、追问、刷新后补充、上下文继续、重试和停止，以及无确认查询、查后追问、历史结果续聊、消息表格刷新和手机布局；旧模式启动独立 MCP 13004，覆盖确认、Skill→MCP 调用、表格和 SQL 展示及刷新恢复。CI 的 `E2E_POSTGRES_PORT=5432`，本机默认 15432。也可用 `E2E_AGENT_MODE=conversation pnpm --dir apps/web test:e2e` 单独验证新模式。

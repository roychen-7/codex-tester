# DDSHUB Agent 工作台

当前实现第 1 步「会话基础」：用户名/密码登录、退出、新建和重命名会话、保存文字消息、历史分页、刷新恢复及用户隔离。前端使用 React + TypeScript + Ant Design + CSS Modules；后端使用 Go 1.25+、Gin、GORM；Agent 自有数据存储于 MySQL 8.0。

当前只保存用户消息，不生成 AI 回复。LLM、任务澄清、历史检索、业务分析和 ddshub PostgreSQL 接入属于后续步骤。没有公开注册、找回密码、分享、附件、消息编辑或会话删除入口。

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

实际迁移文件：`apps/api/internal/database/migrations/001_sessions.sql`。设计说明：`docs/schema/agent-session-v1.draft.sql`。使用 MySQL 8.0.44；CHECK 约束要求至少 8.0.16。

- `agent_users`：Agent 用户，与 ddshub 客户独立。
- `auth_sessions`：登录令牌哈希、到期和撤销时间。
- `conversations`：所属用户、标题、活动时间。
- `messages`：消息正文、角色、请求 ID；唯一键 `(conversation_id, request_id)` 防重复。
- `schema_migrations`：迁移版本、校验和及执行时间（基础设施表）。

所有时间以 UTC 存取，浏览器按本地时区展示；BIGINT ID 在 JSON 中输出字符串。消息按 ID 游标分页；会话按 `(updated_at, id)` 倒序分页。保存消息、更新会话时间在同一 GORM 事务中完成，锁定会话序列化并发写入。

相同请求 ID 与相同正文重试返回原消息，不更新会话时间；相同 ID 改正文返回 409。接口校验消息非全空白、最多 10,000 个 Unicode 码点，请求体最多 64 KiB。原正文换行与空白保留。用户隔离通过所有会话操作的 owner 条件执行；跨用户访问返回 404。

迁移通过 `cmd/manage migrate` 执行，不使用 AutoMigrate。初始建表使用 IF NOT EXISTS，持有连接级迁移锁，成功后登记校验和；MySQL DDL 不支持整批事务回滚，失败后修正问题再执行。已执行迁移不得修改，应新增后续编号文件。禁止对任意已有同名业务库执行本迁移。

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

端到端测试在测试库创建专用账号，启动独立 API 13001 和 web 15173，验证登录、会话切换、重命名、发送、刷新恢复、退出、用户隔离、手机布局，以及服务端已保存但响应丢失后的重试。测试账号密码只用于隔离测试库，不在正常 agent 库中创建。

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
apps/api/cmd/manage             数据库迁移、初始化账号
apps/api/internal/config       环境配置与校验
apps/api/internal/database     MySQL 连接及版本化 SQL 迁移
apps/api/internal/chat         GORM 模型、登录、会话、消息存储
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

分页默认 20 条、最多 100 条。会话使用 `cursor`，消息使用 `before`，结果包含 `items` 和 `next_cursor`。所有会话与消息接口必须登录；客户端不能指定 owner 或发送 assistant 角色。之前的 `/tasks` 演示接口已不再由服务提供。

## Git Flow 与 review

`main` 是发布分支，`develop` 是集成分支。`feature/<主题>` 从最新 develop 创建，完成后 PR 回 develop；普通修复使用 `feature/fix-<主题>`。`release/<版本>` 从 develop 创建，`hotfix/<主题>` 从 main 创建，均 PR 合入 main 并回合 develop；也允许 main → develop 同步 PR。

主分支禁止直接提交或推送，合并需要他人 Review 和 GitHub 必需检查。发布与回合使用 merge commit 保留祖先关系，关闭强制线性历史。发布标签 `vX.Y.Z` 对应 main 上的发布提交；回合完成前保留来源分支。安装本地 hooks 使用 `make install-hooks`，它不能替代 GitHub 保护。

每完成一个可 review 的步骤暂停。提交、推送、PR、合并和发布按当前任务授权执行，稳定约定见 AGENTS.md。

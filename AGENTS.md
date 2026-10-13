# 项目约定

这是前后端单仓库开发练习项目。后端位于 apps/api，使用 Go 1.25+、Gin 和标准库测试；前端位于 apps/web，使用 React、TypeScript 和 Vite，组件统一采用 Ant Design，自定义样式使用 CSS Modules。

- 默认容器开发：先 `make init-env`，`make dev` 启动 web/api/MySQL，`make down` 停止，`make logs` 查看日志，`make check-docker` 在隔离测试库中验证。默认宿主机端口为 web 5173、api 13000、MySQL 13306；对应 WEB_PORT/API_PORT/MYSQL_PORT。
- 本机依赖：`make install`；本机启动：`make dev-local`；仅本机后端：`make dev-api`；本机及 CI 验证：`make check`。前端本机依赖供编辑器使用，不能与容器 node_modules 混用。
- apps/api/cmd/server 是启动入口；cmd/manage 提供迁移和初始化用户；internal/handler 负责 HTTP 和输入校验；internal/chat 使用 GORM 管理用户、登录和会话；internal/database 管理连接及版本化迁移。业务读写必须使用 GORM 和参数绑定，禁止拼接用户输入构造 SQL。
- Agent 自有数据库使用 MySQL 8.0（当前本地镜像 8.0.44）；ddshub PostgreSQL 是后续只读数据源，不在本阶段连接。迁移 SQL 版本化，禁止 AutoMigrate 在服务启动时隐式修改结构。
- Go 测试放同目录 *_test.go。格式化使用 go.mod 固定版本的 gofumpt（兼容 gofmt），前端使用 Prettier；写完立即格式化。可用 make format 统一执行，make check 和 make check-docker 检查格式。
- Go 编码以 [官方 Code Review Comments](https://go.dev/wiki/CodeReviewComments) 为基线，参考 [Google Go Style Guide](https://google.github.io/styleguide/go/guide)。以下是本项目补充的可读性约定，逻辑分组仍需人工检查：
  - 控制流和业务函数体展开写，不把多个语句压在一行。允许惯用的 `if err := call(); err != nil`，分支体仍换行。
  - 函数内按输入校验、数据读取、业务处理、持久化、结果返回分段，段落之间留一个空行；不把整个函数挤在一起，也不在每条语句之间机械加空行。
  - 调用与紧接的错误检查保持相邻；错误处理块与后续正常流程之间空一行。变量声明与直接使用它的语句保持相邻。
  - 顶层声明间距交给 gofumpt。没有固定行宽；难读的长表达式优先简化或拆分，必要时将链式调用和结构体初始化展开，不为凑行宽机械折行。
  - 测试遵循相同规则，准备、执行、断言及不同场景之间按逻辑分组。
- 环境变量统一由 apps/api/internal/config 读取和校验；新增配置同步更新 .env.example 与 README。真实 .env 不提交，Compose 仅显式传入服务需要的变量；后端凭据不得传给前端。
- 前端组件使用 apps/web/tsconfig.json（浏览器类型），Vite 配置使用 tsconfig.node.json（Node 类型）；新增配置文件时确保纳入类型检查。
- 前端依赖使用 pnpm，package.json、pnpm-lock.yaml 和 node_modules 均位于 apps/web；后端仅使用 go.mod/go.sum，不保留 Node 配置或依赖。根目录 Makefile 提供统一命令，go.work 让编辑器识别后端模块。
- make typecheck 只检查前端；完整验证必须用 make check，它显式执行后端 go vet、竞态测试与构建，以及前端类型检查和构建。
- 开始任务先读 README 和相关代码，明确验收标准。复杂改动先给简短方案。
- 默认在 Codex 桌面端讨论方案，用户确认后在 VS Code 实施；每完成一个可 review 的步骤就暂停，等待用户反馈后再继续。
- 每次聚焦一个目标，保留用户已有改动；新增依赖同步更新 lockfile。
- 类型检查不能替代运行时输入校验。错误返回统一 JSON。
- 行为修改覆盖成功和失败路径，交付前运行 make check。
- 数据持久化和权限变更需以 TEST_MYSQL_DSN 指定 _test 数据库运行真实集成测试；未设置时集成测试跳过，不能声称其通过。make check-e2e 验证浏览器流程，测试不使用业务数据库。
- 汇报实际验证结果和限制，不能把已配置 CI 说成云端 CI 已通过。
- 严格使用 Git Flow：main 为发布分支，develop 为集成分支；feature/<主题> 从 develop 创建并 PR 回 develop；release/<版本> 从 develop 创建，hotfix/<主题> 从 main 创建，两者都通过 PR 合入 main 并回合 develop。main → develop 的同步 PR 也允许。禁止直接提交或推送 main/develop。
- 分支主题使用小写字母、数字、点和连字符；发布在 main 的对应提交打 vX.Y.Z 标签。安装本地 hooks 使用 make install-hooks；GitHub 必须对 main/develop 要求 Review、check 与 git-flow 检查通过。本地 hooks 不能替代服务端保护。
- 提交、推送、标签和发布遵循当前任务授权。

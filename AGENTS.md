# 项目约定

这是前后端单仓库开发练习项目。后端位于 apps/api，使用 Go 1.25+、Gin 和标准库测试；前端位于 apps/web，使用 React、TypeScript 和 Vite，组件统一采用 Ant Design，自定义样式使用 CSS Modules。

- 默认容器开发：`make dev` 启动 web/api 两个容器，`make down` 停止，`make logs` 查看日志，`make check-docker` 在容器内验证。默认宿主机端口为 web 5173、api 13000，可用 WEB_PORT/API_PORT 覆盖。
- 本机依赖：`make install`；本机启动：`make dev-local`；仅本机后端：`make dev-api`；本机及 CI 验证：`make check`。前端本机依赖供编辑器使用，不能与容器 node_modules 混用。
- apps/api/cmd/server 是启动入口；internal/handler 负责 HTTP 和输入校验；internal/task 负责当前的任务规则和内存存储。Go 测试放在同目录的 *_test.go；格式化使用 gofmt。
- 环境变量统一由 apps/api/internal/config 读取和校验；新增配置同步更新 .env.example 与 README。真实 .env 不提交，Compose 仅显式传入服务需要的变量；后端凭据不得传给前端。
- 前端组件使用 apps/web/tsconfig.json（浏览器类型），Vite 配置使用 tsconfig.node.json（Node 类型）；新增配置文件时确保纳入类型检查。
- 前端依赖使用 pnpm，package.json、pnpm-lock.yaml 和 node_modules 均位于 apps/web；后端仅使用 go.mod/go.sum，不保留 Node 配置或依赖。根目录 Makefile 提供统一命令，go.work 让编辑器识别后端模块。
- make typecheck 只检查前端；完整验证必须用 make check，它显式执行后端 go vet、竞态测试与构建，以及前端类型检查和构建。
- 开始任务先读 README 和相关代码，明确验收标准。复杂改动先给简短方案。
- 默认在 Codex 桌面端讨论方案，用户确认后在 VS Code 实施；每完成一个可 review 的步骤就暂停，等待用户反馈后再继续。
- 每次聚焦一个目标，保留用户已有改动；新增依赖同步更新 lockfile。
- 类型检查不能替代运行时输入校验。错误返回统一 JSON。
- 行为修改覆盖成功和失败路径，交付前运行 make check。
- 汇报实际验证结果和限制，不能把已配置 CI 说成云端 CI 已通过。
- 严格使用 Git Flow：main 为发布分支，develop 为集成分支；feature/<主题> 从 develop 创建并 PR 回 develop；release/<版本> 从 develop 创建，hotfix/<主题> 从 main 创建，两者都通过 PR 合入 main 并回合 develop。main → develop 的同步 PR 也允许。禁止直接提交或推送 main/develop。
- 分支主题使用小写字母、数字、点和连字符；发布在 main 的对应提交打 vX.Y.Z 标签。安装本地 hooks 使用 make install-hooks；GitHub 必须对 main/develop 要求 Review、check 与 git-flow 检查通过。本地 hooks 不能替代服务端保护。
- 提交、推送、标签和发布遵循当前任务授权。

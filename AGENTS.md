# 项目约定

这是 TypeScript + Node.js 后端开发练习项目，使用 strict 类型检查、原生 HTTP、node:test 和 pnpm。

- 安装：`pnpm install --frozen-lockfile`；开发：`pnpm dev`；验证：`pnpm check`。
- src/app.ts 处理 HTTP 和输入校验；src/tasks.ts 处理业务；src/server.ts 负责启动；tests/ 放接口测试。
- 开始任务先读 README 和相关代码，明确验收标准。复杂改动先给简短方案。
- 每次聚焦一个目标，保留用户已有改动；新增依赖同步更新 lockfile。
- 类型检查不能替代运行时输入校验。错误返回统一 JSON。
- 行为修改覆盖成功和失败路径，交付前运行 pnpm check。
- 汇报实际验证结果和限制，不能把已配置 CI 说成云端 CI 已通过。
- 有首个提交后使用 codex/<主题> 功能分支。提交、推送、发布遵循当前任务授权。

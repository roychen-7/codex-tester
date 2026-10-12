# Codex 后端开发练习场

使用 TypeScript + Node.js 22.13+ 的任务管理 API，练习需求、实现、验证、审查和交付。

## 开始

使用 pnpm 11.3.0（版本固定在 package.json 的 packageManager 字段）。

```sh
pnpm install --frozen-lockfile
pnpm dev
# 另一个终端
pnpm check
```

默认地址 http://127.0.0.1:3000，可用 PORT 环境变量调整端口。`pnpm build` 编译，`pnpm start` 运行编译产物。

## 接口

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | /health | 健康检查 |
| GET | /tasks | 任务列表 |
| POST | /tasks | 新增，body: `{"title":"写接口测试"}` |
| PATCH | /tasks/:id | 更新，body: `{"status":"done"}` |
| DELETE | /tasks/:id | 删除，成功返回 204 |

状态为 todo、doing、done。标题去除首尾空白后为 1–120 个 UTF-16 代码单元。写入接口要求 application/json，请求体最多 16 KiB。

成功返回 `{"data":...}`；错误返回 `{"error":{"code":"...","message":"..."}}`。输入错误返回 400，资源不存在返回 404，超大请求返回 413，错误媒体类型返回 415。

```sh
curl http://127.0.0.1:3000/health
curl -X POST http://127.0.0.1:3000/tasks -H 'Content-Type: application/json' -d '{"title":"完成第一次接口开发"}'
curl http://127.0.0.1:3000/tasks
```

当前为内存存储，重启清空；暂未加入数据库、认证和部署，适合本地练习。

## 开发流程

1. 描述需求及可验证的验收条件。
2. 让 Codex 阅读相关代码，复杂任务先讨论方案。
3. 有首个提交后使用 codex/<主题> 分支，一次完成一个目标，更新接口测试。
4. 执行 pnpm check，验证成功路径、无效输入和资源不存在。
5. 审查 diff，修复行为错误和测试遗漏，再运行相关检查。
6. 汇报证据，按任务授权提交、推送及创建 PR。GitHub Actions 在 push 和 PR 时运行同一检查命令，需要查看真实运行结果。

稳定约定放在 AGENTS.md，单次需求放在任务描述中。[官方 AGENTS.md 说明](https://learn.chatgpt.com/docs/agent-configuration/agents-md)。

## 下一轮演练

> 为 GET /tasks 增加 status 过滤。不传返回全部，传 todo、doing、done 返回对应任务，非法值返回 400。先读代码，再实现、补充接口测试、更新文档并运行检查。

审查时：

> 审查当前改动，关注运行时输入校验、HTTP 状态码和缺失测试，给出文件位置和复现方式。

后续按独立任务加入分页、数据库与迁移、日志、认证、部署与回滚。

# Codex 全栈开发练习场

使用单仓库管理前后端代码，练习需求、实现、验证、审查和交付。后端使用 Go 1.25+ 和 Gin；前端使用 React + TypeScript + Vite，UI 方案为 Ant Design + CSS Modules（页面改造进行中）。

## 目录结构

```text
apps/
  api/
    cmd/server/          # Go 启动入口
    internal/config/     # 环境变量读取、默认值与校验
    internal/handler/    # HTTP 层及接口测试
    internal/task/       # 任务规则、内存存储及测试
    go.mod / go.sum      # Go 依赖
  web/
    src/                 # React 页面和启动入口
    package.json         # 前端依赖、pnpm 版本和命令
    pnpm-lock.yaml       # 前端锁文件
    tsconfig.json        # 浏览器代码类型检查
    tsconfig.node.json   # Vite 配置类型检查
    vite.config.ts       # 本地服务和 API 代理
Makefile                 # 全仓库统一命令
compose.yaml             # 本地开发：web 和 api 两个容器
.env.example             # 本地配置模板，可提交 Git
.env                     # 本地实际配置，不提交 Git
go.work                  # Go 工作区，包含 apps/api
```

## 开始

默认使用 Docker Compose 开发，需要启动 Docker Desktop，并安装 make。运行服务无需本机 Go 或 Node；VS Code 的 Go 语言服务、前端类型提示仍可使用本机 Go 和前端依赖。

```sh
# 首次使用且 .env 不存在时复制，已有配置无需覆盖
cp -n .env.example .env
make dev
# 另一个终端
make check-docker
make logs
# 停止服务，保留依赖缓存
make down
```

以上命令在仓库根目录执行。`make dev` 构建开发镜像并启动两个容器，前台显示日志，Ctrl-C 停止服务。首次启动需要下载镜像和依赖。后台启动可执行 `docker compose up --build -d --wait`。

打开 http://127.0.0.1:5173 查看页面；后端可直接通过 http://127.0.0.1:13000/health 访问。宿主机 3000、3001 已被其他项目占用，因此 API 默认映射为 13000；容器内仍监听 3000。端口占用会明确报错，不会停止其他项目。

前端请求 `/api/tasks`，web 容器中的 Vite 通过 `API_TARGET=http://api:3000` 转发为后端 `/tasks`。API 健康检查通过后再启动 web。容器内监听 0.0.0.0，宿主机发布端口仅绑定 127.0.0.1。

可通过 `WEB_PORT=15173 API_PORT=13001 make dev` 调整宿主机端口，也可将变量放入根目录 `.env`。无需同步修改容器间的代理地址。

源码挂载到容器：前端使用 Vite 热更新，后端使用固定版本 Air 自动编译、重启（重启会清空当前内存任务）。容器内 node_modules、Go 编译产物、前端构建产物及依赖缓存使用独立卷，避免混用 macOS 和 Linux 文件。前端每次启动依据锁文件安装依赖；更新 Go 模块后 Air 会重新编译。变更 Dockerfile 或 Air 版本后需重新执行 `make dev`。

`make check-docker` 构建镜像并创建临时检查容器，执行 Go 静态检查、竞态测试、构建和前端类型检查、构建，不占用额外宿主机端口。`make down` 保留缓存卷；不要为了正常停止服务使用 `down -v`。

## 环境配置

本地编辑根目录 `.env`；只提交 `.env.example`，真实配置和密码不进入 Git。Compose 读取 `.env` 用于变量替换，并仅将各服务需要的变量显式传入容器；shell 中同名变量优先于 `.env`。修改后执行 `docker compose up -d --force-recreate --wait` 重新创建容器，单纯 restart 不会更新环境变量。重建 API 会清空当前内存任务。

| 变量 | 使用方 | 默认值与规则 |
| --- | --- | --- |
| APP_ENV | 后端 | local；仅允许 local、production，显式空值报错 |
| LOG_LEVEL | 后端 | 未设置或为空时：local 为 debug，production 为 info；允许 debug/info/warn/error |
| WEB_PORT | Compose | 5173，宿主机前端端口 |
| API_PORT | Compose | 13000，宿主机后端端口 |
| HOST | 后端 | 本机默认 127.0.0.1，容器固定 0.0.0.0；必须是有效 IP 地址 |
| PORT | 后端 | 默认及容器内均为 3000；必须为 1–65535 的整数 |

模板显式设置 `LOG_LEVEL=debug`，因此切换线上环境时应同时设置 `APP_ENV=production`、`LOG_LEVEL=info`。后端从进程环境读取配置，不自行加载 `.env`；不通过 Compose 启动时，需在命令前设置变量，例如 `APP_ENV=local LOG_LEVEL=debug PORT=13001 make dev-api`。

`internal/config` 在监听端口之前统一校验配置，非法配置使 API 进程以非零状态退出。Air 是开发监督进程，子进程启动失败时仍会等待代码修改；修正 `.env` 后需重新创建容器。日志采用 JSON，并按 LOG_LEVEL 过滤，不打印配置全集或错误变量的原始值。

线上沿用相同变量名，由部署平台或服务器环境文件注入值和 Secret；不将真实线上配置提交仓库或写入镜像。`APP_ENV=production` 只选择应用环境与默认日志级别，不会把当前开发 Compose 自动变成生产部署。本轮尚未添加生产镜像/Compose，也未接入数据库、Redis 或密码配置。前端仅接收本地代理地址，页面继续请求 `/api`；后端凭据不得注入浏览器配置。

## 本机命令与编辑器

本机检查需要 Go 1.25+、Node.js 22.13+、pnpm 11.3.0 和 make（pnpm 版本固定在 apps/web/package.json）。`make install` 安装本机依赖；前端也可单独执行 `pnpm --dir apps/web install --frozen-lockfile`，供 VS Code 类型提示使用。Go 后端不依赖 Node。

`make dev-local` 保留本机联合启动；`make dev-api`、`make dev-web` 分别在本机启动服务。Go 本机启动先编译后运行，修改后需手动重启。可用 `PORT=13001 make dev-local` 调整本机后端及 Vite 代理端口。容器与本机模式不要同时占用同一前端端口。

后端默认地址 http://127.0.0.1:3000，可用 PORT 环境变量调整端口。`make build` 构建前后端，后端产物为 apps/api/bin/api；`make start-api` 运行已编译的后端。

`make check` 显式执行 Go 静态检查、竞态测试、后端编译和前端检查/构建。`make typecheck` 仅检查前端，`make test` 执行 Go 测试。也可直接执行 `go -C apps/api test -race ./...`。

前端产物输出到 apps/web/dist；`pnpm --dir apps/web preview` 在 4173 端口预览构建结果，需另行启动后端。Vite 的代理用于本地开发和预览，部署时需由托管服务配置 `/api` 路由转发。

CI 安装 Node.js 和 Go，分别恢复 pnpm 与 Go 依赖，然后执行根目录的 `make check`。本地检查通过不代表云端 CI 已运行。

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
curl http://127.0.0.1:13000/health
curl -X POST http://127.0.0.1:13000/tasks -H 'Content-Type: application/json' -d '{"title":"完成第一次接口开发"}'
curl http://127.0.0.1:13000/tasks
```

当前为内存存储，重启清空。MySQL（GORM）、Redis（go-redis）、SQL 迁移（Goose）和 Swagger（Swaggo）已选型，尚未接入；后续按独立 review 步骤添加。

## 开发流程

1. 描述需求及可验证的验收条件。
2. 让 Codex 阅读相关代码，复杂任务先讨论方案。
3. 有首个提交后使用 codex/<主题> 分支，一次完成一个目标，更新接口测试。
4. 执行 make check，验证成功路径、无效输入和资源不存在。
5. 审查 diff，修复行为错误和测试遗漏，再运行相关检查。
6. 汇报证据，按任务授权提交、推送及创建 PR。GitHub Actions 在 push 和 PR 时运行同一检查命令，需要查看真实运行结果。

稳定约定放在 AGENTS.md，单次需求放在任务描述中。[官方 AGENTS.md 说明](https://learn.chatgpt.com/docs/agent-configuration/agents-md)。

## 下一轮演练

> 为 GET /tasks 增加 status 过滤。不传返回全部，传 todo、doing、done 返回对应任务，非法值返回 400。先读代码，再实现、补充接口测试、更新文档并运行检查。

审查时：

> 审查当前改动，关注运行时输入校验、HTTP 状态码和缺失测试，给出文件位置和复现方式。

后续按独立任务加入分页、数据库与迁移、日志、认证、部署与回滚。

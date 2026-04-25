# my_feed_system

这是一个本地短视频 Feed 系统示例项目，当前仓库主要包含：

- `backend/`：Go 编写的 API 服务与异步 worker
- `frontend/`：Vue 3 + Vite 本地联调页面
- `nginx/`：可选的本地代理配置

## 本地开发依赖

开始本地开发前，请先准备好以下环境：

- Go `1.25+`
- Node.js `20+`
- MySQL `8+`
- Redis `6+`
- RabbitMQ `3+`

仓库内提供的一键启动脚本只负责拉起项目自身进程，不会帮你启动 MySQL、Redis 和 RabbitMQ，这几个基础服务需要你本机先运行好。

后端默认读取配置文件 [backend/configs/config.yaml](/d:/Go_Project/my_feed_system/backend/configs/config.yaml)。
当前默认端口如下：

- API：`8081`
- MySQL：`3306`
- Redis：`6379`
- RabbitMQ：`5672`

如果你的本地环境端口、账号或密码不同，建议先修改 `backend/configs/config.yaml` 再启动。

## 一键启动本机联调

在仓库根目录执行：

```powershell
.\start-local.ps1
```

如果你的 PowerShell 执行策略拦截了本地脚本，可以改用：

```powershell
powershell -ExecutionPolicy Bypass -File .\start-local.ps1
```

该脚本会自动启动以下进程：

- `backend/cmd`：后端 API 服务
- `backend/cmd/worker`：异步 worker
- `frontend`：Vite 开发服务器

如果 `frontend/node_modules` 不存在，脚本会自动执行一次 `npm install`。

启动成功后，默认访问地址如下：

- 前端页面：`http://127.0.0.1:5173`
- 后端接口：`http://127.0.0.1:8081`

运行日志会写入 `.run/dev/logs/`，进程状态会记录到 `.run/dev/pids.json`。

停止本地联调环境：

```powershell
.\stop-local.ps1
```

## 手动启动方式

如果你想单独排查某个服务，也可以分别启动。

启动后端 API：

```powershell
cd .\backend
go run ./cmd
```

启动 worker：

```powershell
cd .\backend
go run ./cmd/worker
```

启动前端：

```powershell
cd .\frontend
npm run dev
```

## 前后端联调说明

前端通过 Vite 代理转发接口，请求规则定义在 [frontend/vite.config.ts](/d:/Go_Project/my_feed_system/frontend/vite.config.ts)。

默认代理关系如下：

- `/api/*` -> `http://127.0.0.1:8081/*`
- `/static/*` -> `http://127.0.0.1:8081/static/*`

也就是说，前端本地开发时直接访问 `5173` 即可，接口会自动转发到后端 `8081`。

## Nginx 本地访问方式

如果你希望通过 Nginx 访问页面，而不是直接使用 Vite 开发服务器，可以执行：

```powershell
.\nginx\start-dev.ps1
```

然后访问：

```text
http://127.0.0.1:8090
```

停止 Nginx：

```powershell
.\nginx\stop-dev.ps1
```

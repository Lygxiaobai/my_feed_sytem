# my_feed_system

`my_feed_system` 是一个短视频 Feed 流系统，当前仓库包含：

- `backend/`：Go 编写的 API 服务与异步 Worker
- `frontend/`：Vue 3 + Vite 前端页面
- `nginx/`：本地代理相关配置

## 1. 项目概览

系统能力覆盖：

- 账号：注册、登录、改名、改密、登出、查询资料
- 视频：上传视频、上传封面、发布、详情、作者视频列表、点赞视频列表
- 互动：点赞、取消点赞、评论、删除评论、关注、取关
- Feed：最新流、点赞排行流、热榜流、关注流
- 工程能力：Redis 缓存、RabbitMQ 异步 Worker、Outbox、幂等、限流、指标与 pprof

详细设计说明见 [my_feed_system系统设计说明.md](/d:/Go_Project/my_feed_system/my_feed_system系统设计说明.md)。

## 2. Docker Compose 一键启动（推荐）

要求：已安装 Docker Desktop / Docker Engine + Docker Compose。

```bash
docker compose up -d --build
```

访问：

- 前端：`http://localhost:5173`
- 后端 API：`http://localhost:8080`
- RabbitMQ 管理台：`http://localhost:15672`（默认账号 `admin` / `password123`）

说明：

- Compose 会启动 `mysql`、`redis`、`rabbitmq`、`backend`（API）、`worker`、`frontend`。
- 容器内后端配置使用 `backend/configs/config.docker.yaml`（会挂载到 `/app/configs/config.yaml`）。
- 上传文件保存到命名卷 `uploads_data`。
- MySQL 会自动创建数据库 `my_feed_system`，应用启动时会自动执行 GORM 迁移。
- 如果本机端口已被占用，可以通过环境变量覆盖宿主机端口，例如设置 `RABBITMQ_MANAGEMENT_PORT=15673` 再执行启动命令。

停止并清理容器：

```bash
docker compose down
```

如果还要连同数据卷一起清理：

```bash
docker compose down -v
```

## 3. 本地手动运行

如果你不想走 Compose，也可以分别启动：

后端 API：

```powershell
cd .\backend
go run ./cmd
```

Worker：

```powershell
cd .\backend
go run ./cmd/worker
```

前端：

```powershell
cd .\frontend
npm install
npm run dev
```

默认本地配置文件是 [backend/configs/config.yaml](/d:/Go_Project/my_feed_system/backend/configs/config.yaml)，本地开发端口如下：

- API：`127.0.0.1:8081`
- Redis：`127.0.0.1:6379`
- RabbitMQ：`127.0.0.1:5672`
- 前端：`127.0.0.1:5173`

## 4. 前后端联调方式

开发态前端通过 Vite 代理对接本地后端，规则定义在 [frontend/vite.config.ts](/d:/Go_Project/my_feed_system/frontend/vite.config.ts)：

- `/api/*` -> `http://127.0.0.1:8081/*`
- `/static/*` -> `http://127.0.0.1:8081/static/*`

容器态前端则由 Nginx 反代到容器内 `backend:8080`，配置位于 [frontend/nginx.conf](/d:/Go_Project/my_feed_system/frontend/nginx.conf)。

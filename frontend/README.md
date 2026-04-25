# feedsystem_video_go frontend

杀8081
Get-NetTCPConnection -LocalPort 8081 -State Listen | Select-Object -ExpandProperty OwningProcess | ForEach-Object { Stop-Process -Id $_ -Force }


杀5173
Get-NetTCPConnection -LocalPort 5173 -State Listen | Select-Object -ExpandProperty OwningProcess | ForEach-Object { Stop-Process -Id $_ -Force }


这是对接 `backend/`（Gin + GORM + MySQL + JWT）的一套 Vue3 前端调试 UI，覆盖全部后端路由：
- Account：注册 / 登录 / 改密码 / 查找 / 改名 / 登出
- Video：发布 / 按作者列出 / 详情
- Like：点赞 / 取消点赞 / 是否点赞
- Comment：列表 / 发布 / 删除
- Social：关注 / 取关 / 粉丝列表 / 关注列表
- Feed：最新流 / 点赞数流 / 关注流

## 本地启动

下面的命令默认都在**项目根目录**执行，也就是 `my_feed_system/`。

### 1. 启动前先确认后端配置

后端默认读取 `backend/configs/config.yaml`：

- 服务端口：`8081`
- MySQL：`127.0.0.1:3306`
- 用户名：`root`
- 密码：`123456`
- 数据库：`my_feed_system`

如果你的本地环境不一样，先修改这个配置文件再启动。

### 2. 启动后端

```bash
cd backend
go run ./cmd
```

启动成功后，后端会监听：

```text
http://127.0.0.1:8081
```

### 3. 启动前端

首次启动先安装依赖：

```bash
cd frontend
npm install
```

启动开发服务器：

```bash
npm run dev
```

前端默认开发地址一般是：

```text
http://127.0.0.1:5173
```

### 4. 前后端联调说明

Vite 已配置代理，前端访问 `/api/...` 时会自动转发到：

```text
http://127.0.0.1:8081/...
```

对应配置文件：`frontend/vite.config.ts`

### 5. 你可以手动测试的页面

启动前后端后，可以优先测这些页面：

- `/account`：注册、登录、个人作品、粉丝/关注抽屉
- `/account/change-password`：登录后修改密码
- `/settings`：改名、退出登录
- `/video`：上传封面、上传视频、发布视频
- `/video/:id`：视频详情、评论、点赞、关注
- `/u/:id`：用户主页、作品列表、粉丝/关注抽屉
- `/`：推荐流、关注流、点赞榜
- `/hot`：热榜

### 6. 构建检查

如果你只是想确认前端代码没问题，可以在 `frontend/` 下执行：

```bash
npm run build
```

# Sub2API Giftcode

Sub2API站长无真实充值渠道时的审批发码平台。

这个项目用于给没有接入真实支付/充值渠道的 Sub2API 站点提供一套轻量的人工审批流程：用户提交充值/兑换申请，站长审核后自动向 Sub2API 生成余额兑换码，用户再领取兑换码完成充值。

## 功能

- 使用已有 Sub2API 用户账号登录
- 用户提交充值发码申请
- 支持 Sub2API 自定义菜单嵌入模式登录
- 邮件发送审批链接给站长
- 站长可通过邮件链接或后台审批申请
- 审批通过后调用 Sub2API 管理接口生成余额兑换码
- 用户可查看自己的申请和兑换码
- 站长可查看申请队列、用户、兑换码和统计数据
- 支持可编辑的充值档位
- 辅助调度器：OpenAI 主力账号临时不可调度或模型冷却时自动启用备用账号
- SQLite 本地保存申请、会话、档位和兑换码同步状态
- 后端可直接托管前端构建产物，统一通过一个端口访问

## 目录

- `backend/`：Go + Gin API 服务
- `frontend/`：Vue 3 + Element Plus 前端
- `config.example.yaml`：运行配置模板
- `compose.yaml`：Docker Compose 部署模板
- `.github/workflows/docker-image.yml`：GitHub Actions 镜像构建与发布流程

## 本地开发

准备配置：

```bash
cp config.example.yaml config.yaml
```

本地前后端分开开发时，建议把 `config.yaml` 里的配置改成：

```yaml
app:
  listen_addr: "127.0.0.1:8080"
  base_url: "http://127.0.0.1:8080"
  frontend_url: "http://127.0.0.1:5173"
  static_dir: "./public"
database:
  path: "./giftcode.db"
```

启动后端：

```bash
cd backend
go run ./cmd/server -config ../config.yaml
```

启动前端：

```bash
cd frontend
pnpm install
pnpm dev
```

前端开发服务器默认监听 `http://127.0.0.1:5173`，并把 `/api` 代理到后端 `http://127.0.0.1:8080`。

## Docker Compose 部署

1. 复制配置文件：

```bash
cp config.example.yaml config.yaml
```

2. 修改 `config.yaml`：

- `app.base_url`：外部访问地址，例如 `https://giftcode.example.com`
- `app.frontend_url`：使用同源部署时保持空字符串
- `app.static_dir`：容器内保持 `/app/public`
- `database.path`：容器内保持 `/app/data/giftcode.db`
- `sub2api.base_url`：你的 Sub2API 地址
- `sub2api.admin_api_key`：Sub2API 管理 API Key
- `mail.*`：SMTP 发信配置
- `session.cookie_secret`：改成足够长的随机字符串

3. 按需修改 `compose.yaml` 里的镜像地址。当前模板使用 `ghcr.io/equentr/sub2api_giftcode:latest`。

4. 启动：

```bash
docker compose up -d
```

服务默认暴露在宿主机 `8080` 端口，SQLite 数据保存在 Compose 命名卷 `sub2api-giftcode-data`。生产环境可以在前面放 Nginx、Caddy 或 Cloudflare Tunnel，再把外部域名写进 `app.base_url`。

## 镜像发布

仓库包含 GitHub Actions workflow：

- 推送到 `main` 时自动构建并推送镜像到 GHCR
- Pull Request 只构建验证，不推送镜像
- 镜像标签包含 `latest` 和短 commit SHA

镜像地址格式：

```text
ghcr.io/<owner>/<repo>:latest
ghcr.io/<owner>/<repo>:sha-<short-sha>
```

如果仓库名是 `sub2api_giftcode`，示例：

```text
ghcr.io/equentr/sub2api_giftcode:latest
```

## 统一前后端访问

Docker 镜像会先构建 `frontend/dist`，再复制到最终镜像的 `/app/public`。后端启动后：

- `/api/*` 走 API 路由
- `/assets/*` 等静态文件从 `/app/public` 读取
- 其他路径返回前端 `index.html`，支持 Vue Router 刷新页面

因此 Compose 部署时只需要暴露后端一个端口。

## 配置

完整配置见 `config.example.yaml`。

关键项：

- `app.listen_addr`：监听地址；容器内应为 `0.0.0.0:8080`
- `app.base_url`：后端和同源前端的公网地址
- `app.frontend_url`：前端独立部署时填写；同源部署时留空
- `app.static_dir`：后端托管前端资源目录；容器内为 `/app/public`
- `database.path`：SQLite 数据库路径；容器内建议 `/app/data/giftcode.db`
- `sub2api.admin_api_key`：用于生成兑换码的管理 Key
- `mail.approval_ttl_hours`：邮件审批链接有效期，默认 72 小时
- `sync.interval_seconds`：兑换码同步间隔，默认 300 秒
- `aux_scheduler.interval_seconds`：辅助调度器扫描间隔，默认 30 秒

## Sub2API 嵌入模式

当 Sub2API 通过自定义菜单打开本应用，并在 URL 中携带 `ui_mode=embedded`、`token`、`user_id` 时，前端会调用 `POST /api/auth/embedded/login` 换取本地会话。换取成功后，前端会清理地址栏中的敏感参数。

## 常用命令

```bash
make backend-test
make frontend-install
make frontend-build
docker build -t sub2api-giftcode:local .
```

## License

Apache License 2.0. See `LICENSE`.

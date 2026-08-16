<h1 align="center">Sub2API Giftcode</h1>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.23%2B-00ADD8" />
  <img alt="Gin" src="https://img.shields.io/badge/Gin-1.10-008ECF" />
  <img alt="Vue 3" src="https://img.shields.io/badge/Vue%203-4FC08D?logo=vuedotjs&logoColor=white" />
  <img alt="SQLite" src="https://img.shields.io/badge/SQLite-003B57?logo=sqlite&logoColor=white" />
  <img alt="Docker" src="https://img.shields.io/badge/Docker-2496ED?logo=docker&logoColor=white" />
  <img alt="GitHub Actions" src="https://img.shields.io/badge/GitHub%20Actions-2088FF?logo=githubactions&logoColor=white" />
  <img alt="License" src="https://img.shields.io/badge/License-Apache%202.0-blue" />
</p>

Sub2API 站长无真实充值渠道时的轻量人工审批平台，支持直充到账与兑换码发放。

用户提交充值/兑换申请后，站长审批并选择交付方式：优先调用 Sub2API 管理接口直接为指定用户充值到账；直充失败时自动回退为生成余额兑换码，用户再领取兑换码完成充值。项目同时提供订阅配额管理、辅助调度、批量补偿、重置次数赠送等运营辅助功能。

## 核心功能

- 使用已有 Sub2API 用户账号登录
- 支持 Sub2API 自定义菜单嵌入模式登录
- 用户提交充值申请时可选择「直充到账（推荐）」或「下发兑换码」
- 直充通过 Sub2API `create-and-redeem` 接口直接到账，使用稳定幂等键，重复审批不会重复充值
- 直充失败时自动回退为兑换码发放，并记录失败原因供审计
- 邮件发送审批链接给站长，站长可通过邮件链接或后台审批
- 审批通过后按申请单选择的方式直充或生成兑换码
- 用户可查看自己的申请、直充结果和兑换码
- 站长可查看申请队列、用户、兑换码和统计数据
- 支持可编辑的充值档位：余额/订阅、原价/实付、订阅限额和并发数
- 订阅管理：查看有效订阅、剩余天数、日/周/月配额使用进度，并支持手动重置配额
- SQLite 本地保存申请、会话、档位和兑换码同步状态
- 后端可直接托管前端构建产物，统一通过一个端口访问

## 辅助功能

- OpenAI 账号管理：批量查看、设置或清空上游账号的 User-Agent
- 辅助调度器：按模型集合与成本泳道逐级启用 OpenAI 账号；故障时最多逐级扩容，低成本容量恢复并稳定约两分钟后自动收缩
- 订阅并发监控：订阅档位配置并发数，自动同步并执行用户并发上限，异常时保留并重试
- 订阅重置权益：跟踪基础周期和活动赠送的重置次数，按最早到期优先扣减，过期/撤销自动失效
- 重置次数赠送：面向全部或指定用户、一个或多个有限额订阅分组批量赠送次数，支持预览、幂等执行和完整审计
- 批量补偿：面向全量用户按有效订阅补天数、无有效订阅补余额，支持排除邮箱域名和查看逐用户结果
- 站点品牌设置：标题、副标题和邮件主题前缀可在管理端直接配置

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

### SQLite WAL 运维

文件数据库使用 WAL 模式、单连接池和显式的 `wal_autocheckpoint=1000`。兑换码同步只会在上游业务字段变化时更新对应记录；每次同步完成仍会在 `sync_state` 中记录全局同步时间。

服务启动后会立即执行一次、随后每 15 分钟执行一次非阻塞的 `PRAGMA wal_checkpoint(PASSIVE)`。日志会输出 `busy`、WAL 帧数、已 checkpoint 帧数和 `wal_size_bytes`；应针对 WAL 文件持续增长、`busy != 0` 或 `log_frames - checkpointed_frames` 长期不回落建立告警。SQLite 无法通过 PRAGMA 直接给出最老读事务，出现此类告警时应排查持有数据库文件的外部进程（例如备份、DB Browser 和监控查询）。

备份时必须使用 SQLite online backup API 或 `VACUUM INTO`；不要只复制主 `.db` 文件。定期清理已过期的 `sessions` 记录，以避免主库随使用时间增长。

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

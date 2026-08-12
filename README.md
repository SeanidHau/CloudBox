# CloudBox

CloudBox 是一个面向个人与受邀朋友的轻量媒体网盘学习项目。项目由 Go API 服务和 React Web 工作台组成，提供邀请注册、文件管理、分片续传、内容去重、受控分享、后台任务、缩略图、可选病毒扫描和可切换的基础设施组件。

Web 工作台的接口边界见 [项目概况.md](项目概况.md)。其中明确列出了已实现接口与仍未提供的后端能力。

## 当前能力

- 邀请码注册、bcrypt 密码哈希、JWT 会话鉴权、账号禁用和强制修改临时密码。
- 文件上传、列表、下载、HTTP Range、回收站、恢复、永久删除、重命名和移动。
- 文件夹创建、浏览、重命名、移动和空目录删除。
- SHA-256 内容去重、秒传和按用户逻辑存储配额。
- 分片上传、断点续传基础、分片校验、完整文件校验和合并。
- 单文件或多文件合并分享、公开图片预览、登录下载/保存副本、可选密码、过期时间和下载次数上限。
- 公开链接按 IP 限制下载频率；密码连续错误会临时锁定，并记录匿名化访问审计。
- 管理员邀请码、账号状态、容量档位、临时密码和分享撤销管理；管理员不默认访问用户私有文件。
- 持久化后台任务、文件完整性校验、缩略图和可选 ClamAV 病毒扫描。
- 本地磁盘或 MinIO 对象存储；SQLite 或 PostgreSQL；可选 Redis 用量缓存和跨实例分享风控。
- JSON 访问日志、Prometheus HTTP 指标、OpenTelemetry HTTP 链路追踪、GitHub Actions 和 Docker Compose。
- React + TypeScript 文件工作台：邀请注册、文件与目录浏览、名称/类型/时间搜索、普通/分片上传与续传、回收站、分享链接、批量删除、合并分享、容量展示、图片预览、相册视图和移动端媒体网格。

## 快速开始

### 前置条件

- Go。当前开发环境使用 `/usr/local/go/bin/go`。
- Node.js 与 npm。前端开发环境使用 Node.js `26`。
- 本地默认模式不需要 Docker、Redis、MinIO、PostgreSQL 或 ClamAV。

### 本地运行

在仓库根目录执行：

```bash
/usr/local/go/bin/go test ./...
/usr/local/go/bin/go run ./cmd/api
```

服务默认监听 `http://localhost:8080`。首次运行会创建：

- `cloudbox.db`：SQLite 数据库。
- `uploads/`：正式文件、缩略图和分片临时文件。

健康检查：

```bash
curl http://localhost:8080/health
```

预期响应：

```json
{"status":"ok"}
```

### Web 工作台

保持 API 服务运行，在另一个终端执行：

```bash
cd web
npm install
npm run dev
```

浏览器访问 `http://127.0.0.1:5173`。Vite 会把 `/api` 请求代理到本地 API 服务 `http://localhost:8080`，因此开发期间不需要配置跨域。

需要将前端连接到其他开发 API 时，启动前设置 `VITE_API_TARGET`。例如：`VITE_API_TARGET=http://127.0.0.1:18080 npm run dev`。

前端提供以下已接入的工作流：

- 邀请注册、登录、JWT 会话保存和临时密码强制修改。
- 根目录与子目录浏览、创建目录、重命名、移动与空目录删除。
- 小文件直接上传；大于 `5 MB` 的文件自动走分片初始化、逐块上传和合并完成接口。
- 文件搜索、下载、回收站恢复/永久删除；选择多个文件后可批量移入回收站或创建一个合并分享链接。
- 分享页支持单文件和多文件合并分享。公开访问者验证密码后可查看文件清单；登录后可逐个下载或一次保存全部文件到自己的目录。
- 存储用量展示、列表与相册双视图，以及使用鉴权请求读取图片和视频封面。

前端不伪造病毒扫描状态。扫描未完成时，下载或缩略图接口返回 `423`，前端会提示当前文件暂不可用；扫描未通过时按后端返回的 `403` 提示处理。文件详情中的“发起完整性校验”调用的是 `file.verify` 后台任务，不是病毒扫描任务。

生成生产静态资源：

```bash
cd web
npm run build
```

输出目录为 `web/dist/`。`compose.production.yaml` 会通过 Caddy 托管该目录，并将 `/api` 反向代理到 Go API。

## 配置

项目从环境变量读取配置。`.env.example` 列出 Docker Compose 使用的敏感配置示例；本地直接运行时也可以设置相同变量。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | HTTP 监听地址。 |
| `DB_PATH` | `cloudbox.db` | SQLite 数据库文件路径。 |
| `DATABASE_DRIVER` | `sqlite` | 数据库驱动：`sqlite` 或 `postgres`。 |
| `DATABASE_URL` | 空 | PostgreSQL 连接 URL；`DATABASE_DRIVER=postgres` 时必填。 |
| `UPLOAD_DIR` | `uploads` | 本地文件、缩略图和分片临时文件目录。 |
| `JWT_SECRET` | `dev-secret-change-me` | JWT 签名密钥。生产环境必须替换。 |
| `ADMIN_USERNAME` | 空 | 首位管理员用户名；与 `ADMIN_PASSWORD` 同时设置时初始化或提升该账号。 |
| `ADMIN_PASSWORD` | 空 | 首位管理员初始密码；生产环境应通过安全的环境注入提供。 |
| `USER_STORAGE_QUOTA_BYTES` | `1073741824` | 单用户逻辑存储配额，单位为字节。 |
| `STORAGE_DRIVER` | `local` | 存储驱动：`local` 或 `minio`。 |
| `MINIO_ENDPOINT` | `localhost:9000` | MinIO 服务地址，不包含协议。 |
| `MINIO_ACCESS_KEY` | 空 | MinIO 访问密钥。 |
| `MINIO_SECRET_KEY` | 空 | MinIO 密钥。 |
| `MINIO_BUCKET` | `cloudbox` | MinIO bucket 名称。 |
| `MINIO_USE_SSL` | `false` | 是否通过 HTTPS 访问 MinIO。 |
| `REDIS_ENABLED` | `false` | 是否启用 Redis 存储用量缓存和公开分享访问控制。 |
| `REDIS_ADDR` | `localhost:6379` | Redis 地址。 |
| `REDIS_PASSWORD` | 空 | Redis 密码。 |
| `REDIS_DB` | `0` | Redis 逻辑数据库编号，不能为负数。 |
| `REDIS_USAGE_CACHE_TTL_SECONDS` | `60` | 存储用量缓存有效期，单位为秒。 |
| `TRASH_RETENTION_HOURS` | `720` | 回收站自动永久删除保留期，单位为小时；设为 `0` 可关闭自动清理。 |
| `LOG_LEVEL` | `info` | JSON 请求日志最低级别：`debug`、`info`、`warn` 或 `error`。 |
| `TRACE_EXPORTER` | `none` | 链路追踪导出器：`none` 或 `stdout`。 |
| `JOB_WORKER_COUNT` | `1` | 后台任务 Worker 数量；`0` 表示只创建任务，不消费任务。 |
| `JOB_POLL_INTERVAL_MILLISECONDS` | `1000` | Worker 队列为空时的轮询间隔，单位为毫秒。 |
| `CLAMAV_ENABLED` | `false` | 是否启用 ClamAV 病毒扫描。 |
| `CLAMAV_ADDRESS` | `127.0.0.1:3310` | clamd TCP 地址。 |
| `CLAMAV_TIMEOUT_SECONDS` | `60` | 单次扫描最长时间，单位为秒。 |

配置值为空、格式无效或不满足限制时，服务对多数变量回退到默认值。`DATABASE_DRIVER` 和 `STORAGE_DRIVER` 只接受上表中的驱动名称；其他值会导致服务启动失败。

## 运行模式

### Docker Compose

1. 启动 Docker Desktop。
2. 在仓库根目录创建本地环境变量文件：

   ```bash
   cp .env.example .env
   ```

3. 修改 `.env` 中的 `JWT_SECRET`。使用 PostgreSQL、Redis 或 MinIO 时，同时填写相应密码和访问凭证。
4. 启动服务：

   ```bash
   docker compose up --build -d
   ```

5. 验证服务：

   ```bash
   curl http://localhost:8080/health
   ```

默认 Compose 模式使用 SQLite 和本地存储。数据库和上传文件保存在命名 volume `cloudbox-data` 的 `/data` 目录。停止容器不会删除该 volume。

查看日志：

```bash
docker compose logs -f api
```

不需要 Docker 时停止服务和 Docker Desktop，避免 Docker 后台持续占用资源：

```bash
docker compose down
docker desktop stop
```

### MinIO

MinIO 覆盖配置使用独立 API 数据卷，不会读取或覆盖默认本地存储模式的 SQLite 数据和上传文件。

```bash
docker compose -f compose.yaml -f compose.minio.yaml up --build -d
docker compose -f compose.yaml -f compose.minio.yaml ps
curl http://localhost:8080/health
```

应用容器通过 Docker 网络中的 `minio:9000` 访问对象存储。MinIO S3 API 地址为 `http://localhost:9000`，管理控制台地址为 `http://localhost:9001`。

### PostgreSQL

```bash
docker compose -f compose.yaml -f compose.postgres.yaml up --build -d
docker compose -f compose.yaml -f compose.postgres.yaml ps
curl http://localhost:8080/health
```

服务启动时执行 `migrations/postgres/` 中配置的迁移。PostgreSQL 数据保存在命名 volume `postgres-data` 中。

### Redis

Redis 缓存 `GET /api/storage` 的用户已用空间，并保存公开分享的短期访问控制状态：下载频率窗口和密码错误锁定。数据库仍是配额判断、文件数据、分享配置和访问审计的最终来源。

```bash
docker compose -f compose.yaml -f compose.redis.yaml up --build -d
docker compose -f compose.yaml -f compose.redis.yaml ps
curl http://localhost:8080/health
```

Redis 不开放宿主机端口，仅供 Docker 网络内的 API 服务访问。上传和秒传成功后，服务删除对应用户的用量缓存；下一次用量查询重新从数据库计算。启用 Redis 后，多个 API 实例共享同一份公开分享限速和密码锁定状态；Redis 不可用时，公开分享访问返回 `503 Service Unavailable`，不会绕过访问控制。

组合使用 PostgreSQL 和 Redis：

```bash
docker compose \
  -f compose.yaml \
  -f compose.postgres.yaml \
  -f compose.redis.yaml \
  up --build -d
```

### 生产部署

生产配置使用 PostgreSQL、MinIO、Redis、Go API 和 Caddy。Caddy 托管前端静态资源、反向代理 API，并在域名解析和 80/443 端口可达时自动申请与续期 HTTPS 证书。

部署前需要完成以下准备：

1. 将域名的 A/AAAA 记录指向服务器。
2. 在服务器防火墙放行 TCP `80` 和 `443`。
3. 创建 `.env`，并设置 `CLOUDBOX_DOMAIN`、`JWT_SECRET`、PostgreSQL、MinIO 与 Redis 的强随机密码。生产环境不要保留 `.env.example` 中的示例值。
4. 确认服务器已安装 Docker Engine 和 Docker Compose 插件。

启动：

```bash
docker compose -f compose.production.yaml config --quiet
docker compose -f compose.production.yaml up --build -d
```

验证：

```bash
curl -f https://$CLOUDBOX_DOMAIN/health
docker compose -f compose.production.yaml ps
```

生产配置只公开 Caddy 的 `80/443`。PostgreSQL、MinIO、Redis 和 API 仅在 Compose 内部网络可见；不要为了排障直接暴露它们的端口。

### 生产备份与恢复

在升级或迁移前，执行备份。备份脚本需要当前目录存在生产 `.env`，并导出 PostgreSQL 元数据和 MinIO 对象数据。Redis 只保存缓存与短期访问控制状态，不纳入备份。

```bash
./scripts/backup-production.sh
```

脚本在 `backups/<UTC 时间戳>/` 生成 `postgres.dump`、`minio-data.tar.gz` 和 `SHA256SUMS`。将整个目录复制到独立于服务器的安全位置。

恢复会停止生产服务，并覆盖 PostgreSQL 和 MinIO 数据。恢复前确认备份目录和目标服务器无误：

```bash
./scripts/restore-production.sh backups/<UTC 时间戳>
docker compose -f compose.production.yaml ps
curl -f https://$CLOUDBOX_DOMAIN/health
```

停止服务：

```bash
docker compose -f compose.production.yaml down
```

## API 概览

除认证接口和公开分享下载外，所有 `/api` 路由需要：

```text
Authorization: Bearer <JWT>
```

| 功能 | 方法与路径 |
| --- | --- |
| 健康检查 | `GET /health` |
| Prometheus 指标 | `GET /metrics` |
| 注册 | `POST /api/auth/register` |
| 登录 | `POST /api/auth/login` |
| 当前用户 | `GET /api/me` |
| 修改密码 | `POST /api/auth/change-password` |
| 小文件上传 | `POST /api/files` |
| 秒传 | `POST /api/files/instant` |
| 活跃文件列表 | `GET /api/files`，可选 `?parent_id=<目录 ID>` |
| 文件搜索 | `GET /api/files/search`，支持 `q`、`kind=image|video|other`、`since` 和 `before`（RFC 3339） |
| 回收站 | `GET /api/files/trash` |
| 下载或 Range 下载 | `GET /api/files/:id/download` |
| 缩略图 | `GET /api/files/:id/thumbnail` |
| 软删除 | `DELETE /api/files/:id` |
| 永久删除 | `DELETE /api/files/:id/permanent` |
| 恢复 | `POST /api/files/:id/restore` |
| 创建完整性校验任务 | `POST /api/files/:id/verify` |
| 查询我的后台任务 | `GET /api/jobs/:id` |
| 移动文件 | `PATCH /api/files/:id/move` |
| 重命名文件 | `PATCH /api/files/:id/rename` |
| 创建文件夹 | `POST /api/folders` |
| 浏览文件夹 | `GET /api/folders`，可选 `?parent_id=<目录 ID>` |
| 重命名文件夹 | `PATCH /api/folders/:id/rename` |
| 移动文件夹 | `PATCH /api/folders/:id/move` |
| 删除空文件夹 | `DELETE /api/folders/:id` |
| 存储用量 | `GET /api/storage` |
| 创建分享链接 | `POST /api/files/:id/shares` |
| 查看我的分享链接 | `GET /api/shares` |
| 撤销分享链接 | `DELETE /api/shares/:token` |
| 创建合并分享 | `POST /api/share-collections`，请求体包含至少两个 `file_ids` |
| 查看我的合并分享 | `GET /api/share-collections` |
| 撤销合并分享 | `DELETE /api/share-collections/:token` |
| 公开分享信息 | `GET /api/shares/:token`，可选 `X-Share-Password` |
| 公开图片预览 | `GET /api/shares/:token/preview`，可选 `X-Share-Password` |
| 公开下载分享文件 | `GET /api/shares/:token/download`，可选 `X-Share-Password` |
| 保存分享副本 | `POST /api/shares/:token/save` |
| 查看公开合并分享 | `GET /api/share-collections/:token`，可选 `X-Share-Password` |
| 下载合并分享中的文件 | `GET /api/share-collections/:token/files/:id/download`，需要登录，可选 `X-Share-Password` |
| 保存合并分享全部副本 | `POST /api/share-collections/:token/save`，需要登录，请求体可包含 `password`、`parent_id` |
| 初始化分片上传 | `POST /api/uploads/init` |
| 上传分片 | `PUT /api/uploads/:id/chunks/:number` |
| 查询上传状态 | `GET /api/uploads/:id` |
| 合并上传 | `POST /api/uploads/:id/complete` |
| 取消上传 | `DELETE /api/uploads/:id` |

## 文件和上传行为

### 内容去重和配额

文件字节存储在 `file_objects`，用户可见记录存储在 `user_files`。内容哈希相同的多个用户文件共享一个物理对象。内容去重不降低用户的逻辑用量。

默认用户逻辑配额为 `1 GiB`。活跃文件和回收站文件都计入用量。文件只有在永久删除后才释放配额。

### 分片上传

分片编号从 `0` 开始。最后一个分片可以小于普通分片；其他分片必须符合初始化时声明的大小。客户端可以乱序上传分片并重复上传同一编号的分片。

```text
POST /api/uploads/init
    |
PUT /api/uploads/:id/chunks/0..N-1
    |
GET /api/uploads/:id
    |
POST /api/uploads/:id/complete
```

合并操作会校验分片、合并内容、校验完整文件哈希，并复用文件对象去重流程。

### 回收站

软删除只改变用户文件状态。永久删除会删除关联分享链接、减少文件对象引用计数，并在引用计数归零时尽力删除实际存储对象和缩略图。存储清理失败不会恢复已完成的数据库删除，服务会记录错误日志。

默认保留期为 30 天。服务在启动时和每小时执行一次回收站清理；清理会撤销关联分享、释放逻辑容量，并在最后一个引用删除后尽力清理实际对象和缩略图。`TRASH_RETENTION_HOURS=0` 可关闭自动清理。

### 公开分享保护

公开分享的下载和保存副本按分享 token 与匿名化 IP 分别限速：单 IP 最多 20 次/分钟。密码连续错误 5 次后，该 IP 在该分享上锁定 10 分钟。系统审计 token、匿名化 IP、动作、结果和时间，不记录原始 IP。Redis 启用时，多个 API 实例共享限速和锁定状态。

合并分享为至少两个同一所有者的活跃文件创建一个统一链接，并共享密码、有效期和下载次数上限。访问者通过验证后只能查看该链接内的文件清单；每下载一个文件都会消耗一次该链接的下载额度。任意源文件移入回收站后，整个合并链接立即失效，避免链接继续暴露不完整的文件集合。

登录用户可将合并分享中的全部文件保存为自己的文件记录。保存前会统一检查目标目录和总逻辑容量；创建记录与对象引用计数在同一个事务中完成。容量不足或任一数据库操作失败时，不会保存任何文件，并会回退本次预留的下载次数。

Redis 未启用时，限制状态保存在 API 进程内，适合本地单实例开发。生产 Compose 默认启用 Redis，用于多实例共享访问控制状态。

## 后台任务和缩略图

后台任务保存于 `background_jobs`。Worker 原子领取 `run_at` 已到期的 `queued` 任务。任务失败后按 1、2、4、8、16、32 秒的间隔重试；达到最大尝试次数后，任务状态为 `failed`。

任务类型：

- `file.verify`：流式重新计算 SHA-256。
- `file.thumbnail`：为 JPEG、PNG、GIF、WebP 生成最长边 `320 px` 的 PNG 缩略图；GIF 仅处理第一帧。视频通过 `ffmpeg` 提取首个可解码帧，并生成最长边 `320 px` 的 PNG 封面。超过 `40,000,000` 像素的源图片会被拒绝。
- `file.scan`：通过 clamd INSTREAM 协议执行病毒扫描。

缩略图属于 `file_object`，相同内容的去重文件共享一份缩略图。生产镜像内置 `ffmpeg`。本地运行缺少 `ffmpeg` 时，视频保留为可下载文件且不生成封面；缩略图任务不会阻塞上传。缩略图尚未生成、文件不支持预览、文件已删除或文件不属于当前用户时，`GET /api/files/:id/thumbnail` 返回 `404 Not Found`。

## 病毒扫描

设置 `CLAMAV_ENABLED=true` 后，上传和秒传创建 `file.scan` 任务。扫描结果按 `file_object` 保存，重复内容共享同一份扫描结果。

| 扫描状态 | 私有下载和缩略图 | 公开分享下载 |
| --- | --- | --- |
| `clean` | 允许访问。 | 允许访问。 |
| `pending`、`scanning`、`failed` 或无记录 | `423 Locked`。 | `423 Locked`。 |
| `infected` | `403 Forbidden`。 | `423 Locked`，不暴露扫描详情。 |

启用扫描后，图片只在扫描结果为 `clean` 时创建缩略图任务。扫描器不可用时，文件保持不可下载状态，不会降级为放行。

### 验证真实 ClamAV

本地验证需要可从 CloudBox 访问的 clamd 服务。可临时启动官方镜像：

```bash
docker run -d --rm --name cloudbox-clamav-test -p 3310:3310 clamav/clamav:stable
```

容器显示为 healthy 后，运行集成测试：

```bash
CLAMAV_TEST_ADDRESS=127.0.0.1:3310 \
  /usr/local/go/bin/go test -count=1 -run TestClamAVScannerIntegration -v ./internal/scanner
```

测试发送普通内容和 EICAR 标准反病毒测试样本。验证结束后停止容器；不需要 Docker 时停止 Docker Desktop：

```bash
docker stop cloudbox-clamav-test
docker desktop stop
```

## 指标、日志和追踪

`GET /metrics` 暴露 Prometheus 文本指标。该路径不要求 JWT。生产环境应通过反向代理、网络策略或独立监听地址限制访问来源。

HTTP 指标：

- `cloudbox_http_requests_total`：按方法、路由模板和状态码统计的请求数。
- `cloudbox_http_request_duration_seconds`：请求耗时直方图。
- `cloudbox_http_requests_in_flight`：正在处理的请求数。

CloudBox 为每个 HTTP 请求创建 OpenTelemetry Server Span。`TRACE_EXPORTER=stdout` 时，Span 以 JSON 输出到标准输出；`TRACE_EXPORTER=none` 时关闭导出。访问日志中的 `trace_id`、`span_id` 和 `request_id` 可用于关联一次请求的日志与追踪。

本地追踪验证：

```bash
TRACE_EXPORTER=stdout /usr/local/go/bin/go run ./cmd/api
curl http://localhost:8080/health
```

## 验证

常规验证不需要 Docker：

```bash
/usr/local/go/bin/go test -count=1 ./...
/usr/local/go/bin/go vet ./...
```

MinIO 集成测试默认不执行。启动 MinIO Compose 后，显式设置连接变量：

```bash
access_key=$(sed -n 's/^MINIO_ROOT_USER=//p' .env)
secret_key=$(sed -n 's/^MINIO_ROOT_PASSWORD=//p' .env)

MINIO_INTEGRATION_ENDPOINT=localhost:9000 \
MINIO_INTEGRATION_ACCESS_KEY="$access_key" \
MINIO_INTEGRATION_SECRET_KEY="$secret_key" \
MINIO_INTEGRATION_BUCKET=cloudbox \
/usr/local/go/bin/go test -tags=integration ./internal/storage
```

该测试验证 MinIO 对象保存、SHA-256 哈希、读取、删除和删除后不可再次读取。

## 目录结构

```text
cmd/api/             API 入口、路由组装和进程生命周期
internal/auth/       注册、登录和 JWT
internal/cache/      Redis 存储用量缓存
internal/config/     环境变量配置
internal/database/   SQLite、PostgreSQL 和迁移执行
internal/file/       文件、去重、缩略图和病毒扫描
internal/job/        持久化后台任务和 Worker
internal/metrics/    Prometheus HTTP 指标
internal/middleware/ 鉴权、请求 ID 和访问日志
internal/scanner/    ClamAV clamd 客户端
internal/share/      分享链接和公开下载
internal/storage/    本地磁盘和 MinIO 存储
internal/telemetry/  OpenTelemetry 追踪
internal/upload/     分片上传和合并
migrations/          SQLite 与 PostgreSQL 迁移
docs/                历史设计与实施记录
```

## 学习重点

- Handler、Service、Repository、Storage 和后台任务处理器的职责边界。
- `io.Reader`、`io.Copy` 和流式 I/O 如何避免大文件一次性进入内存。
- JWT 如何将用户身份传递到业务层，并限制数据访问范围。
- 原子 SQL 状态转换如何处理并发上传完成和后台任务领取。
- 内容哈希如何用于文件去重、分片校验和完整性校验。
- 接口注入如何在不改变业务调用方的前提下替换存储、缓存和病毒扫描器。
- 默认拒绝策略如何保护未完成扫描或感染对象的下载路径。

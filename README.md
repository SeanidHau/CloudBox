# CloudBox

CloudBox 是一个用 Go 实现的网盘后端学习项目。它从本地文件存储 API 起步，逐步加入内容去重、HTTP Range 下载，以及大文件分片上传和断点续传。

## 当前进度

### 已完成

- [x] 用户注册、bcrypt 密码哈希与 JWT 登录
- [x] JWT 鉴权和按用户隔离的数据访问
- [x] 小文件流式上传、文件列表、下载、软删除与回收站恢复
- [x] HTTP Range 下载
- [x] `file_objects` 与 `user_files` 分离
- [x] SHA-256 内容去重和秒传
- [x] 分片上传初始化、乱序上传与重复分片覆盖
- [x] 上传进度查询，支持客户端识别缺失分片
- [x] 分片哈希、完整文件哈希和总大小校验
- [x] 分片合并为正式文件，并复用既有的去重存储流程
- [x] 条件状态转换，避免并发完成请求重复创建用户文件
- [x] 取消未完成上传并清理临时分片
- [x] 服务启动时和每小时自动清理过期上传任务
- [x] Repository、Service 和 HTTP Handler 的自动化测试
- [x] 真实 HTTP 端到端验证：初始化、三块上传、合并、下载校验

### 未完成

- [ ] 文件夹、重命名、移动和存储配额
- [ ] 分享链接、密码、过期时间和下载次数限制
- [ ] PostgreSQL、MinIO 和 Redis 的生产化替换
- [ ] 异步任务，例如缩略图、病毒扫描和失败重试
- [ ] Docker Compose、GitHub Actions、指标、日志和链路追踪
- [ ] Web 前端

## 技术栈

- Go
- Gin
- SQLite
- JWT + bcrypt
- 本地磁盘存储
- SHA-256

## 架构

```text
HTTP 请求
    |
Handler      解析请求、返回状态码和 JSON
    |
Service      执行业务规则、权限边界和文件流程
    |
Repository   访问 SQLite
    |
Storage      保存、打开和删除物理文件
```

主要目录：

```text
cmd/api/             API 入口和路由组装
internal/auth/       注册、登录与 JWT
internal/file/       用户文件、去重和下载
internal/upload/     上传任务、分片、进度和合并
internal/storage/    本地文件存储
internal/database/   SQLite 和版本化迁移
migrations/          数据库迁移 SQL
docs/                学习设计和历史计划
```

## 启动

本机 Go 安装在 `/usr/local/go/bin/go` 时：

```bash
/usr/local/go/bin/go test ./...
/usr/local/go/bin/go run ./cmd/api
```

服务默认监听 `http://localhost:8080`，本地运行时会创建：

- `cloudbox.db`：SQLite 数据库
- `uploads/`：正式文件和上传临时分片

健康检查：

```bash
curl http://localhost:8080/health
```

## API 一览

所有 `/api` 下除认证接口外的路由都需要：

```text
Authorization: Bearer <JWT>
```

| 功能 | 方法与路径 |
| --- | --- |
| 注册 | `POST /api/auth/register` |
| 登录 | `POST /api/auth/login` |
| 上传小文件 | `POST /api/files` |
| 秒传 | `POST /api/files/instant` |
| 活跃文件列表 | `GET /api/files` |
| 回收站 | `GET /api/files/trash` |
| 下载或 Range 下载 | `GET /api/files/:id/download` |
| 软删除 | `DELETE /api/files/:id` |
| 恢复文件 | `POST /api/files/:id/restore` |
| 初始化分片上传 | `POST /api/uploads/init` |
| 上传一个分片 | `PUT /api/uploads/:id/chunks/:number` |
| 查询上传状态 | `GET /api/uploads/:id` |
| 合并并完成上传 | `POST /api/uploads/:id/complete` |

## 分片上传流程

```text
客户端声明文件大小和分片大小
    |
POST /api/uploads/init
    |
得到 upload_id 和 total_chunks
    |
PUT /api/uploads/:id/chunks/0..N-1
    |
GET /api/uploads/:id 查询已完成分片
    |
POST /api/uploads/:id/complete
    |
校验每块哈希、按编号合并、校验完整哈希
    |
创建 user_file，并复用 file_object 去重
```

分片编号从 `0` 开始。最后一块可以小于普通分片大小；其他分片必须与初始化时声明的大小一致。

## 最小使用示例

登录后将响应中的 token 保存到当前终端：

```bash
export TOKEN='登录响应中的 token'
```

初始化一个 25 字节、每块 10 字节的上传任务：

```bash
curl -X POST http://localhost:8080/api/uploads/init \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "original_name":"video.mp4",
    "content_type":"video/mp4",
    "file_size":25,
    "chunk_size":10
  }'
```

从响应中读取 `upload.id` 并设为 `UPLOAD_ID` 后，上传分片：

```bash
curl -X PUT "http://localhost:8080/api/uploads/$UPLOAD_ID/chunks/0" \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary '0123456789'

curl -X PUT "http://localhost:8080/api/uploads/$UPLOAD_ID/chunks/1" \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary 'abcdefghij'

curl -X PUT "http://localhost:8080/api/uploads/$UPLOAD_ID/chunks/2" \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary '12345'

curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/uploads/$UPLOAD_ID/complete"
```

## 学习重点

- Handler、Service、Repository 和 Storage 的职责边界
- `io.Reader`、`io.Copy` 和流式读写如何避免大文件一次性进入内存
- JWT 中间件如何将用户身份传递到业务层
- SQL 查询为什么必须带 `user_id` 以防止越权
- SHA-256 如何用于分片校验、完整性校验和内容去重
- 上传任务状态为何要经历 `uploading -> completing -> completed`
- `defer` 如何在失败时恢复状态并清理临时文件

## 验证状态

当前代码已通过：

```bash
/usr/local/go/bin/go test ./...
```

并已通过本地 HTTP 端到端验证：注册、登录、初始化上传、三块分片上传、状态查询、合并完成、下载内容比对。

# CloudBox 学习项目设计（历史归档）

> 日期：2026-07-22
>
> 本文是项目启动时的学习设计记录。阶段划分、目录和计划中的未实现项保留原始含义，不代表当前功能状态。当前实现见 [README.md](../../../README.md) 和 [项目概况.md](../../../项目概况.md)。

## 项目目标

CloudBox 从小型文件存储 API 开始，逐步演进为用于学习的文件存储和分享平台。

原始学习路径分为三个阶段：

1. 阶段 1：通过一个小而完整的后端学习 Go Web 开发。
2. 阶段 2：增加适合作为项目经历的文件存储功能。
3. 阶段 3：增加工程化、可观测性、异步处理和部署实践。

当时的直接目标是完成阶段 1。

## 阶段 1 范围

阶段 1 使用：

- Go
- Gin
- SQLite
- 本地磁盘存储
- JWT 认证

阶段 1 实现：

- 用户注册。
- 用户登录。
- 受 JWT 保护的文件 API。
- 小文件上传。
- 文件下载。
- 文件列表。
- 软删除。
- 回收站列表。
- 回收站恢复。

阶段 1 明确不包含：

- 分片上传。
- 断点续传。
- 基于哈希的秒传。
- Redis。
- MinIO。
- PostgreSQL。
- 分享链接。
- 文件夹树。
- 异步 Worker。
- Web 前端。

该范围用于聚焦 Go 基础、HTTP 路由、数据库访问、分层设计和流式文件 I/O。

## 推荐架构

使用分层单体服务：

- Handler 解析 HTTP 输入并返回 HTTP 响应。
- Service 承载业务规则。
- Repository 访问 SQLite。
- Storage 读取和写入本地文件。
- Middleware 处理认证。

该结构保持阶段 1 的学习难度，同时为阶段 2 的 PostgreSQL、MinIO 和 Redis 替换预留边界。

## 初始目录设计

```text
cloudbox/
├── cmd/
│   └── api/
│       └── main.go
├── internal/
│   ├── auth/
│   │   ├── handler.go
│   │   ├── service.go
│   │   ├── repository.go
│   │   └── model.go
│   ├── config/
│   │   └── config.go
│   ├── database/
│   │   └── sqlite.go
│   ├── file/
│   │   ├── handler.go
│   │   ├── service.go
│   │   ├── repository.go
│   │   └── model.go
│   ├── middleware/
│   │   └── auth.go
│   └── storage/
│       └── local.go
├── migrations/
│   └── 001_init.sql
├── uploads/
├── docs/
├── go.mod
└── README.md
```

该目录是阶段 1 的计划结构，不等同于当前仓库结构。

## 初始数据模型

阶段 1 使用两张表：

### `users`

- `id`
- `username`
- `password_hash`
- `created_at`

### `user_files`

- `id`
- `user_id`
- `original_name`
- `storage_path`
- `size`
- `content_type`
- `status`
- `created_at`
- `deleted_at`

`status` 使用：

- `active`
- `deleted`

原始设计计划在阶段 2 将物理存储元数据拆分为 `file_objects` 与用户可见文件记录。

## 初始 API 设计

公开路由：

```text
POST /api/auth/register
POST /api/auth/login
```

受认证保护的路由：

```text
POST   /api/files
GET    /api/files
GET    /api/files/trash
GET    /api/files/:id/download
DELETE /api/files/:id
POST   /api/files/:id/restore
```

## 上传流程

1. 客户端向 `POST /api/files` 发送 `multipart/form-data`。
2. 认证中间件从 JWT 提取用户 ID。
3. 文件 Handler 验证请求中存在文件。
4. 文件 Service 请求本地存储保存上传流。
5. Repository 向 SQLite 插入元数据。
6. API 返回保存后的文件元数据。

文件内容必须通过流复制。实现不应将完整文件读入内存。

## 下载流程

1. 客户端请求 `GET /api/files/:id/download`。
2. Service 验证文件属于当前认证用户且未删除。
3. Storage 打开本地文件。
4. Handler 将文件流返回给客户端。

阶段 1 不要求 HTTP Range 下载。该能力规划在阶段 2。

## 错误处理

Handler 返回统一 JSON 错误：

```json
{
  "error": "message"
}
```

原始预期状态码：

- `400`：输入无效。
- `401`：缺少认证或认证无效。
- `403`：禁止访问。
- `404`：文件不存在。
- `409`：用户名重复。
- `500`：未预期的服务器错误。

业务错误应在 Service 包中使用类型错误或哨兵错误表示，Handler 根据错误映射 HTTP 状态。

## 初始测试策略

阶段 1 应覆盖：

- 密码哈希和登录校验。
- JWT 创建和解析。
- 文件 Repository CRUD。
- 文件 Service 所有权校验。

完整 HTTP 集成测试可在第一个可运行版本后补充。

## 阶段 2 方向

- 使用 PostgreSQL 替换 SQLite。
- 使用 MinIO 替换本地磁盘。
- 拆分 `user_files` 和 `file_objects`。
- 增加 SHA-256 文件哈希。
- 增加秒传。
- 增加分片上传。
- 增加断点续传。
- 增加 HTTP Range 下载。
- 增加带密码、过期时间和下载次数上限的分享链接。
- 使用 Redis 保存上传状态、限流和热点元数据。

## 阶段 3 方向

- Redis Streams 异步 Worker。
- 缩略图生成。
- 失败任务重试。
- 过期上传清理。
- Prometheus 指标。
- OpenTelemetry 追踪。
- 结构化日志。
- 压测。
- GitHub Actions。
- Docker Compose。
- Kubernetes 部署说明。

## 阶段 1 完成标准

- API 可以通过一条命令启动。
- 用户可以注册和登录。
- 已认证用户可以上传文件。
- 已认证用户只能查询自己的文件。
- 已认证用户只能下载自己的活跃文件。
- 删除将文件移入回收站，而不是直接删除数据库行。
- 已删除文件可以查询和恢复。
- 文件字节保存在本地磁盘，元数据保存在 SQLite。
- README 说明运行和测试方式。

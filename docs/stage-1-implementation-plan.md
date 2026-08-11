# 阶段 1 实施计划（历史归档）

> 本文记录 CloudBox 的阶段 1 初始实施计划。阶段 1 已完成，文中的范围、目录和接口不代表当前实现。当前能力见 [README.md](../README.md) 和 [项目概况.md](../项目概况.md)。

## 目标

构建第一个可运行的 CloudBox 后端，用于学习 Go 项目开发。

阶段 1 关注以下内容：

- Gin 路由。
- SQLite 持久化。
- JWT 认证。
- 分层后端结构。
- 基于流的本地文件上传和下载。

## 步骤 1：项目骨架

创建 Go 模块和以下目录：

- `cmd/api`：API 进程入口。
- `internal/config`：基于环境变量的配置。
- `internal/database`：SQLite 连接和迁移。
- `internal/auth`：用户注册、登录、密码哈希和 JWT 签发。
- `internal/middleware`：JWT 认证中间件。
- `internal/file`：文件元数据和文件 API 业务逻辑。
- `internal/storage`：本地磁盘存储实现。
- `migrations`：SQL Schema。

## 步骤 2：数据库

阶段 1 使用 SQLite，并创建两张表：

- `users`
- `user_files`

Schema 支持账号归属、文件元数据、软删除、回收站查询和恢复。

## 步骤 3：认证

实现：

- `POST /api/auth/register`
- `POST /api/auth/login`

密码使用 bcrypt 哈希。登录成功后返回 JWT，其中包含：

- `user_id`
- `username`
- 过期时间

## 步骤 4：认证中间件

为文件路由添加 JWT 中间件。中间件执行以下操作：

1. 读取 `Authorization: Bearer <token>`。
2. 验证 token。
3. 将 `user_id` 和 `username` 写入 Gin Context。

## 步骤 5：文件 API

实现：

- `POST /api/files`
- `GET /api/files`
- `GET /api/files/trash`
- `GET /api/files/:id/download`
- `DELETE /api/files/:id`
- `POST /api/files/:id/restore`

规则：

- 用户只能访问自己的文件。
- 已删除文件不能通过正常下载路由访问。
- 删除为软删除。
- 恢复将文件状态改回活跃状态。

## 步骤 6：本地存储

将上传文件保存到 `uploads/`。存储层返回相对存储路径，数据库保存该路径。

实现要求：

- 自动创建上传目录。
- 使用唯一存储名称。
- 使用 `io.Copy` 复制数据流。
- 不将整个上传文件读入内存。

## 步骤 7：文档

更新 `README.md`，说明：

- 项目目标。
- API 列表。
- 环境变量。
- 运行命令。
- `curl` 示例。
- 阶段限制。

## 步骤 8：验证

Go 安装后执行：

```powershell
go mod tidy
go test ./...
go run ./cmd/api
```

手工冒烟验证：

1. 注册用户。
2. 登录并复制 token。
3. 上传文件。
4. 查询文件列表。
5. 下载文件。
6. 删除文件。
7. 查询回收站。
8. 恢复文件。

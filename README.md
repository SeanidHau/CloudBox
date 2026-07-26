# CloudBox

CloudBox 是一个用 Go 编写的文件存储学习项目。第一阶段先实现一个小而完整的文件存储 API，用来练习 Go Web 开发、JWT 鉴权、SQLite 持久化、分层架构和流式文件上传下载。

项目最终目标是逐步扩展成一个简化版网盘系统，后续会加入大文件分片上传、断点续传、秒传、对象存储、分享链接和异步处理等能力。

## 当前阶段

当前实现的是 Stage 1：本地文件存储 API。

Stage 1 的重点不是一次性做完整网盘，而是先把后端基础打通：

- 用户注册
- 用户登录
- JWT 鉴权
- 小文件上传
- 文件列表
- 文件下载
- 文件软删除
- 回收站列表
- 从回收站恢复文件
- SQLite 保存元数据
- 本地磁盘保存文件内容

## 技术栈

- Go
- Gin
- SQLite
- JWT
- bcrypt
- 本地磁盘存储

## 项目结构

```text
cmd/
└── api/
    └── main.go

internal/
├── auth/
│   ├── handler.go
│   ├── model.go
│   ├── repository.go
│   └── service.go
├── config/
│   └── config.go
├── database/
│   └── sqlite.go
├── file/
│   ├── handler.go
│   ├── model.go
│   ├── repository.go
│   └── service.go
├── middleware/
│   └── auth.go
└── storage/
    └── local.go

migrations/
└── 001_init.sql

uploads/
```

## 分层说明

项目使用分层单体结构：

- `handler`：处理 HTTP 请求和响应
- `service`：处理业务逻辑
- `repository`：处理数据库读写
- `storage`：处理文件内容读写
- `middleware`：处理 JWT 鉴权

这样做的好处是：第一阶段代码不会太复杂，同时后续替换 PostgreSQL、MinIO、Redis 时也比较容易。

## 数据模型

### users

```text
id
username
password_hash
created_at
```

### user_files

```text
id
user_id
original_name
storage_path
size
content_type
status
created_at
deleted_at
```

`status` 当前只使用两个值：

- `active`
- `deleted`

## 启动项目

安装依赖：

```bash
go mod tidy
```

启动 API：

```bash
go run ./cmd/api
```

健康检查：

```bash
curl http://localhost:8080/health
```

期望返回：

```json
{"status":"ok"}
```

## API 示例

### 注册

```bash
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"sean","password":"123456"}'
```

### 登录

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"sean","password":"123456"}'
```

登录成功后会返回 token：

```json
{"token":"your.jwt.token"}
```

设置环境变量：

```bash
TOKEN="your.jwt.token"
```

### 查看当前用户

```bash
curl http://localhost:8080/api/me \
  -H "Authorization: Bearer $TOKEN"
```

### 上传文件

```bash
echo "hello cloudbox" > test.txt

curl -X POST http://localhost:8080/api/files \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@test.txt"
```

### 查看文件列表

```bash
curl http://localhost:8080/api/files \
  -H "Authorization: Bearer $TOKEN"
```

### 下载文件

```bash
curl http://localhost:8080/api/files/1/download \
  -H "Authorization: Bearer $TOKEN" \
  -o downloaded-test.txt
```

### 删除文件

这里的删除是软删除，只会把文件移入回收站，不会立刻删除数据库记录。

```bash
curl -X DELETE http://localhost:8080/api/files/1 \
  -H "Authorization: Bearer $TOKEN"
```

### 查看回收站

```bash
curl http://localhost:8080/api/files/trash \
  -H "Authorization: Bearer $TOKEN"
```

### 恢复文件

```bash
curl -X POST http://localhost:8080/api/files/1/restore \
  -H "Authorization: Bearer $TOKEN"
```

## 学习重点

这个阶段需要重点理解：

- Gin 如何注册路由
- handler 如何解析 JSON、表单和路径参数
- middleware 如何拦截请求并写入上下文
- bcrypt 为什么可以保护密码
- JWT 如何携带用户身份
- repository 为什么只负责数据库
- service 为什么负责业务规则
- `io.Reader`、`io.ReadCloser` 和 `io.Copy` 如何实现流式文件处理
- 为什么上传文件时不能一次性读入内存
- 为什么所有文件查询都必须带 `user_id`

## 当前完成标准

Stage 1 完成后应该满足：

- 服务可以用一条命令启动
- 用户可以注册和登录
- 登录后可以拿到 JWT
- 文件接口必须携带 JWT
- 用户只能看到自己的文件
- 用户只能下载自己的 active 文件
- 删除文件会进入回收站
- 回收站文件可以恢复
- 文件内容保存在本地磁盘
- 文件元数据保存在 SQLite

## 后续计划

Stage 2 会把项目升级成更接近真实网盘的系统：

- PostgreSQL
- MinIO
- 文件 SHA-256 哈希
- 秒传
- 文件去重
- 分片上传
- 断点续传
- HTTP Range 下载
- 分享链接
- Redis 上传状态和限流

Stage 3 会补工程化能力：

- Redis Streams 异步任务
- 缩略图生成
- 失败任务重试
- 过期上传清理
- Prometheus 指标
- OpenTelemetry 链路追踪
- 结构化日志
- Docker Compose
- GitHub Actions

# CloudBox

CloudBox is a Go learning project that starts as a small file storage backend and will later grow into a distributed file storage and sharing platform.

Current stage: **Stage 1 - Gin + SQLite + local disk storage**.

## Features In Stage 1

- User registration
- User login
- JWT authentication
- Small file upload
- File list
- File download
- Soft delete
- Trash list
- Restore from trash

Not included yet:

- Chunked upload
- Resume upload
- Instant upload by file hash
- Redis
- MinIO
- PostgreSQL
- Share links
- Async workers
- Frontend UI

## Requirements

- Go 1.22 or newer

The current machine used to scaffold this project does not have `go` installed, so dependencies have not been downloaded yet. After installing Go, run `go mod tidy`.

## Project Structure

```text
cmd/api                 API process entrypoint
internal/auth           registration, login, password hashing, JWT
internal/config         environment configuration
internal/database       SQLite connection and migrations
internal/file           file metadata and file API logic
internal/middleware     Gin middleware
internal/storage        local disk storage
migrations              SQL schema
uploads                 local uploaded files, ignored by Git
```

## Configuration

Environment variables:

```text
CLOUDBOX_ADDR           default :8080
CLOUDBOX_DB_PATH        default cloudbox.db
CLOUDBOX_UPLOAD_DIR     default uploads
CLOUDBOX_JWT_SECRET     default dev-secret-change-me
CLOUDBOX_TOKEN_HOURS    default 24
```

For local learning, the defaults are enough.

## Run

```powershell
go mod tidy
go test ./...
go run ./cmd/api
```

The server listens on:

```text
http://localhost:8080
```

Health check:

```powershell
curl http://localhost:8080/healthz
```

## API

Public routes:

```text
POST /api/auth/register
POST /api/auth/login
```

Authenticated routes:

```text
POST   /api/files
GET    /api/files
GET    /api/files/trash
GET    /api/files/:id/download
DELETE /api/files/:id
POST   /api/files/:id/restore
```

## Smoke Test With Curl

Register:

```powershell
curl -X POST http://localhost:8080/api/auth/register `
  -H "Content-Type: application/json" `
  -d "{\"username\":\"alice\",\"password\":\"password123\"}"
```

Login:

```powershell
curl -X POST http://localhost:8080/api/auth/login `
  -H "Content-Type: application/json" `
  -d "{\"username\":\"alice\",\"password\":\"password123\"}"
```

Copy the `token` value from the response.

Upload:

```powershell
curl -X POST http://localhost:8080/api/files `
  -H "Authorization: Bearer YOUR_TOKEN" `
  -F "file=@项目概况.md"
```

List active files:

```powershell
curl http://localhost:8080/api/files `
  -H "Authorization: Bearer YOUR_TOKEN"
```

Download:

```powershell
curl http://localhost:8080/api/files/1/download `
  -H "Authorization: Bearer YOUR_TOKEN" `
  -o downloaded-file
```

Delete:

```powershell
curl -X DELETE http://localhost:8080/api/files/1 `
  -H "Authorization: Bearer YOUR_TOKEN"
```

List trash:

```powershell
curl http://localhost:8080/api/files/trash `
  -H "Authorization: Bearer YOUR_TOKEN"
```

Restore:

```powershell
curl -X POST http://localhost:8080/api/files/1/restore `
  -H "Authorization: Bearer YOUR_TOKEN"
```

## Learning Notes

The most important Stage 1 code paths are:

- `cmd/api/main.go`: how a Go web process is assembled.
- `internal/auth/service.go`: password hashing and JWT issuing.
- `internal/middleware/auth.go`: how protected APIs identify the current user.
- `internal/file/handler.go`: how Gin receives uploads and streams downloads.
- `internal/storage/local.go`: how file bytes are copied without loading the whole file into memory.
- `internal/file/repository.go`: how file metadata is stored and queried.

# CloudBox Learning Project Design

Date: 2026-07-22

## Goal

CloudBox is a Go learning project that starts as a small file storage API and grows into a resume-ready distributed file storage and sharing platform.

The learning path has three stages:

1. Stage 1: learn Go web development through a small but complete backend.
2. Stage 2: add resume-level file storage features.
3. Stage 3: add engineering maturity, observability, async processing, and deployment practices.

Stage 1 is the immediate implementation target.

## Stage 1 Scope

Stage 1 uses:

- Go
- Gin
- SQLite
- Local disk storage
- JWT authentication

Stage 1 implements:

- User registration
- User login
- JWT-protected file APIs
- Small file upload
- File download
- File list
- Soft delete
- Trash list
- Restore from trash

Stage 1 intentionally excludes:

- Chunked upload
- Resume upload
- Instant upload by hash
- Redis
- MinIO
- PostgreSQL
- Share links
- Folder hierarchy
- Async workers
- Frontend UI

This keeps the first version focused on Go fundamentals, HTTP routing, database access, layered architecture, and streaming file I/O.

## Recommended Approach

Use a layered monolith.

The API should stay in one Go service, but the code should already be split by responsibility:

- Handlers parse HTTP input and return HTTP responses.
- Services hold business logic.
- Repositories handle SQLite queries.
- Storage handles local file reads and writes.
- Middleware handles authentication.

This makes Stage 1 easy enough for learning while keeping the code ready for Stage 2 changes such as PostgreSQL, MinIO, and Redis.

## Project Structure

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

## Data Model

Stage 1 keeps the data model small:

### users

- id
- username
- password_hash
- created_at

### user_files

- id
- user_id
- original_name
- storage_path
- size
- content_type
- status
- created_at
- deleted_at

The `status` field should use simple values:

- `active`
- `deleted`

Stage 2 can split physical storage metadata into `file_objects` and user-visible file records. Stage 1 keeps one `user_files` table so the first implementation is easier to understand.

## API Design

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

## Upload Flow

1. The client sends `multipart/form-data` to `POST /api/files`.
2. The auth middleware extracts the user ID from the JWT.
3. The file handler validates that a file is present.
4. The file service asks local storage to save the uploaded stream.
5. The repository inserts a metadata row into SQLite.
6. The API returns the saved file metadata.

The file content must be copied as a stream. The implementation should not read the full file into memory.

## Download Flow

1. The client requests `GET /api/files/:id/download`.
2. The service checks that the file belongs to the authenticated user and is not deleted.
3. The storage layer opens the local file.
4. The handler streams the file back to the client.

Stage 1 does not need HTTP Range support. That belongs in Stage 2.

## Error Handling

Handlers should return consistent JSON errors:

```json
{
  "error": "message"
}
```

Expected status codes:

- `400` for invalid input
- `401` for missing or invalid authentication
- `403` for forbidden access
- `404` for missing files
- `409` for duplicate usernames
- `500` for unexpected server errors

Business errors should be represented as typed or sentinel errors in service packages so handlers can map them to HTTP status codes.

## Testing Strategy

Stage 1 should include focused tests for:

- Password hashing and login validation
- JWT creation and parsing
- File repository CRUD behavior
- File service ownership checks

Full HTTP integration tests are useful but can come after the first working version.

## Stage 2 Direction

Stage 2 upgrades the project into a resume-ready file storage system:

- Replace SQLite with PostgreSQL.
- Replace local disk storage with MinIO.
- Split `user_files` and `file_objects`.
- Add SHA-256 file hashing.
- Add instant upload when the same content already exists.
- Add chunked upload.
- Add resume upload.
- Add HTTP Range download.
- Add share links with password, expiry, and download limit.
- Add Redis for upload state, rate limiting, and hot metadata.

## Stage 3 Direction

Stage 3 adds engineering maturity:

- Redis Streams async workers.
- Thumbnail generation.
- Failed task retry.
- Expired upload cleanup.
- Prometheus metrics.
- OpenTelemetry tracing.
- Structured logging.
- Load testing.
- GitHub Actions.
- Docker Compose.
- Kubernetes deployment notes.

## Success Criteria For Stage 1

Stage 1 is complete when:

- The API starts with one command.
- A user can register and log in.
- Authenticated users can upload a file.
- Authenticated users can list only their own files.
- Authenticated users can download only their own active files.
- Deleting a file moves it to trash instead of removing the row.
- Deleted files can be listed and restored.
- File bytes are stored on local disk and metadata is stored in SQLite.
- The README explains how to run and test the project.

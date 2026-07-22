# CloudBox Stage 1 Implementation Plan

## Objective

Build the first runnable CloudBox backend for learning Go project development.

Stage 1 focuses on:

- Gin routing
- SQLite persistence
- JWT authentication
- Layered backend structure
- Stream-based local file upload and download

## Step 1: Project Skeleton

Create the Go module and these directories:

- `cmd/api`: API process entrypoint.
- `internal/config`: environment-based configuration.
- `internal/database`: SQLite connection and migrations.
- `internal/auth`: user registration, login, password hashing, JWT issuing.
- `internal/middleware`: JWT authentication middleware.
- `internal/file`: file metadata and file API business logic.
- `internal/storage`: local disk storage implementation.
- `migrations`: SQL schema.

## Step 2: Database

Use SQLite for Stage 1.

Create two tables:

- `users`
- `user_files`

The schema supports simple account ownership, file metadata, soft delete, trash listing, and restore.

## Step 3: Authentication

Implement:

- `POST /api/auth/register`
- `POST /api/auth/login`

Passwords are hashed with bcrypt.

Successful login returns a JWT that includes:

- `user_id`
- `username`
- expiration time

## Step 4: Auth Middleware

Protect file routes with JWT middleware.

The middleware:

- reads `Authorization: Bearer <token>`
- validates the token
- stores `user_id` and `username` in Gin context

## Step 5: File APIs

Implement:

- `POST /api/files`
- `GET /api/files`
- `GET /api/files/trash`
- `GET /api/files/:id/download`
- `DELETE /api/files/:id`
- `POST /api/files/:id/restore`

Rules:

- Users can only access their own files.
- Deleted files cannot be downloaded from the normal download route.
- Delete is a soft delete.
- Restore moves a file back to active state.

## Step 6: Local Storage

Store uploaded file bytes under `uploads/`.

The storage layer returns a relative storage path that can be saved in SQLite.

Implementation requirements:

- create upload directories automatically
- use unique storage names
- copy streams with `io.Copy`
- avoid reading the whole upload into memory

## Step 7: Documentation

Update `README.md` with:

- project purpose
- API list
- environment variables
- run commands
- curl examples
- current stage limitations

## Step 8: Verification

When Go is installed, verify with:

```powershell
go mod tidy
go test ./...
go run ./cmd/api
```

Manual smoke test:

1. Register a user.
2. Log in and copy the token.
3. Upload a file.
4. List files.
5. Download the file.
6. Delete it.
7. List trash.
8. Restore it.

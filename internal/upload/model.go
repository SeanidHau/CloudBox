package upload

import (
	"database/sql"
	"time"
)

const (
	StatusUploading  = "uploading"
	StatusCompleting = "completing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)

type Task struct {
	ID           string         `json:"id"`
	UserID       int64          `json:"user_id"`
	OriginalName string         `json:"original_name"`
	ContentType  string         `json:"content_type"`
	FileSize     int64          `json:"file_size"`
	ChunkSize    int64          `json:"chunk_size"`
	TotalChunks  int64          `json:"total_chunks"`
	FileHash     sql.NullString `json:"-"`
	Status       string         `json:"status"`
	TempDir      string         `json:"-"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

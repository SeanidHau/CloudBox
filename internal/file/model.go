package file

import (
	"database/sql"
	"time"
)

const (
	StatusActive  = "active"
	StatusDeleted = "deleted"
)

type UserFile struct {
	ID           int64        `json:"id"`
	UserID       int64        `json:"user_id"`
	OriginalName string       `json:"original_name"`
	StoragePath  string       `json:"storage_path"`
	Size         int64        `json:"size"`
	ContentType  string       `json:"content_type"`
	Status       string       `json:"status"`
	CreatedAt    time.Time    `json:"created_at"`
	DeletedAt    sql.NullTime `json:"-"`
}

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
	ParentID     *int64       `json:"parent_id"`
	OriginalName string       `json:"original_name"`
	StoragePath  string       `json:"storage_path"`
	Size         int64        `json:"size"`
	ContentType  string       `json:"content_type"`
	Status       string       `json:"status"`
	CreatedAt    time.Time    `json:"created_at"`
	DeletedAt    sql.NullTime `json:"-"`
}

type Folder struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	ParentID  *int64    `json:"parent_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type FileObject struct {
	ID             int64     `json:"id"`
	FileHash       string    `json:"file_hash"`
	StoragePath    string    `json:"storage_path"`
	Size           int64     `json:"size"`
	ContentType    string    `json:"content_type"`
	ReferenceCount int       `json:"reference_count"`
	CreatedAt      time.Time `json:"created_at"`
}

type StorageUsage struct {
	UsedBytes      int64 `json:"used_bytes"`
	QuotaBytes     int64 `json:"quota_bytes"`
	AvailableBytes int64 `json:"available_bytes"`
}

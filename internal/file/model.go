package file

import (
	"database/sql"
	"time"
)

const (
	StatusActive            = "active"
	StatusDeleted           = "deleted"
	AvailabilityReady       = "ready"
	AvailabilityProcessing  = "processing"
	AvailabilityUnavailable = "unavailable"
	ScanStatusPending       = "pending"
	ScanStatusScanning      = "scanning"
	ScanStatusClean         = "clean"
	ScanStatusInfected      = "infected"
	ScanStatusFailed        = "failed"
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
	Availability string       `json:"availability"`
	CreatedAt    time.Time    `json:"created_at"`
	DeletedAt    sql.NullTime `json:"-"`
	CleanupAt    *time.Time   `json:"cleanup_at,omitempty"`
}

type Folder struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	ParentID  *int64    `json:"parent_id"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
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

// InstantUploadInput describes an existing object that should be added to a
// user's workspace without copying its bytes again.
type InstantUploadInput struct {
	OriginalName string
	FileHash     string
}

type SearchFilter struct {
	Query         string
	Kind          string
	CreatedAfter  time.Time
	CreatedBefore time.Time
}

type FilePreview struct {
	FileObjectID int64     `json:"file_object_id"`
	StoragePath  string    `json:"storage_path"`
	Size         int64     `json:"size"`
	ContentType  string    `json:"content_type"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	CreatedAt    time.Time `json:"created_at"`
}

type FileScan struct {
	FileObjectID int64          `json:"file_object_id"`
	Status       string         `json:"status"`
	Signature    sql.NullString `json:"-"`
	ScannedAt    sql.NullTime   `json:"scanned_at"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

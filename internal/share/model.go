package share

import "time"

type Share struct {
	Token         string     `json:"token"`
	UserFileID    int64      `json:"user_file_id"`
	PasswordHash  string     `json:"-"`
	ExpiresAt     *time.Time `json:"expires_at"`
	MaxDownloads  *int64     `json:"max_downloads"`
	DownloadCount int64      `json:"download_count"`
	CreatedAt     time.Time  `json:"created_at"`
	OriginalName  string     `json:"original_name,omitempty"`
	Size          int64      `json:"size,omitempty"`
	ContentType   string     `json:"content_type,omitempty"`
	HasPreview    bool       `json:"has_preview,omitempty"`
}

type SharedFile struct {
	ObjectID     int64
	ID           int64
	OriginalName string
	StoragePath  string
	Size         int64
	ContentType  string
	FileHash     string
}

// PublicFile is the intentionally small file description returned to a
// visitor after the share password and expiry checks have succeeded.
type PublicFile struct {
	OriginalName string `json:"original_name"`
	Size         int64  `json:"size"`
	ContentType  string `json:"content_type"`
	HasPreview   bool   `json:"has_preview"`
}

type SharedPreview struct {
	StoragePath string
	ContentType string
	CreatedAt   time.Time
}

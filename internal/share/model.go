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
}

type SharedFile struct {
	ObjectID     int64
	ID           int64
	OriginalName string
	StoragePath  string
	Size         int64
	ContentType  string
}

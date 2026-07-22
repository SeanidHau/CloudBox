package file

const (
	StatusActive  = "active"
	StatusDeleted = "deleted"
)

type UserFile struct {
	ID           int64   `json:"id"`
	UserID       int64   `json:"user_id"`
	OriginalName string  `json:"original_name"`
	StoragePath  string  `json:"storage_path"`
	Size         int64   `json:"size"`
	ContentType  string  `json:"content_type"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
	DeletedAt    *string `json:"deleted_at,omitempty"`
}

type CreateFileParams struct {
	UserID       int64
	OriginalName string
	StoragePath  string
	Size         int64
	ContentType  string
}

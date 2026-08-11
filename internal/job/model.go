package job

import (
	"database/sql"
	"encoding/json"
	"time"
)

const (
	StatusQueued       = "queued"
	StatusRunning      = "running"
	StatusSucceeded    = "succeeded"
	StatusFailed       = "failed"
	DefaultMaxAttempts = 3
)

const (
	TypeVerifyFile        = "file.verify"
	TypeGenerateThumbnail = "file.thumbnail"
)

type Job struct {
	ID          string          `json:"id"`
	UserID      *int64          `json:"user_id,omitempty"`
	JobType     string          `json:"job_type"`
	Payload     json.RawMessage `json:"payload"`
	Status      string          `json:"status"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
	RunAt       time.Time       `json:"run_at"`
	LockedAt    sql.NullTime    `json:"-"`
	LastError   sql.NullString  `json:"-"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

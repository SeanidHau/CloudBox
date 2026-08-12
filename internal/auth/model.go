package auth

import (
	"database/sql"
	"encoding/json"
	"time"
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"

	StatusActive   = "active"
	StatusDisabled = "disabled"
)

type User struct {
	ID                 int64     `json:"id"`
	Username           string    `json:"username"`
	PasswordHash       string    `json:"-"`
	Role               string    `json:"role"`
	Status             string    `json:"status"`
	StorageQuotaBytes  int64     `json:"storage_quota_bytes"`
	SessionVersion     int64     `json:"-"`
	MustChangePassword bool      `json:"must_change_password"`
	CreatedAt          time.Time `json:"created_at"`
}

type Invitation struct {
	ID              int64         `json:"id"`
	CreatedByUserID int64         `json:"created_by_user_id"`
	ExpiresAt       time.Time     `json:"expires_at"`
	UsedByUserID    sql.NullInt64 `json:"used_by_user_id,omitempty"`
	UsedAt          sql.NullTime  `json:"used_at,omitempty"`
	RevokedAt       sql.NullTime  `json:"revoked_at,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
}

type CreatedInvitation struct {
	Invitation Invitation `json:"invitation"`
	Code       string     `json:"code"`
}

// MarshalJSON keeps SQL null wrappers out of the public API response.
func (i Invitation) MarshalJSON() ([]byte, error) {
	type invitationJSON struct {
		ID              int64      `json:"id"`
		CreatedByUserID int64      `json:"created_by_user_id"`
		ExpiresAt       time.Time  `json:"expires_at"`
		UsedByUserID    *int64     `json:"used_by_user_id,omitempty"`
		UsedAt          *time.Time `json:"used_at,omitempty"`
		RevokedAt       *time.Time `json:"revoked_at,omitempty"`
		CreatedAt       time.Time  `json:"created_at"`
	}

	value := invitationJSON{
		ID:              i.ID,
		CreatedByUserID: i.CreatedByUserID,
		ExpiresAt:       i.ExpiresAt,
		CreatedAt:       i.CreatedAt,
	}
	if i.UsedByUserID.Valid {
		usedBy := i.UsedByUserID.Int64
		value.UsedByUserID = &usedBy
	}
	if i.UsedAt.Valid {
		usedAt := i.UsedAt.Time
		value.UsedAt = &usedAt
	}
	if i.RevokedAt.Valid {
		revokedAt := i.RevokedAt.Time
		value.RevokedAt = &revokedAt
	}
	return json.Marshal(value)
}

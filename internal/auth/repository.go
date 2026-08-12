package auth

import (
	"database/sql"
	"errors"
	"time"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrInvitationNotFound = errors.New("invitation not found")
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(username string, passwordHash string, quotaBytes int64) (*User, error) {
	var id int64
	err := r.db.QueryRow(
		`INSERT INTO users (username, password_hash, storage_quota_bytes) VALUES ($1, $2, $3) RETURNING id`,
		username,
		passwordHash,
		quotaBytes,
	).Scan(&id)
	if err != nil {
		return nil, err
	}

	return r.FindByID(id)
}

func (r *Repository) CreateAdminIfMissing(username string, passwordHash string, quotaBytes int64) (*User, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE role = $1)`, RoleAdmin).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, nil
	}

	var id int64
	err = tx.QueryRow(`SELECT id FROM users WHERE username = $1`, username).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.QueryRow(`INSERT INTO users (username, password_hash, role, storage_quota_bytes) VALUES ($1, $2, $3, $4) RETURNING id`, username, passwordHash, RoleAdmin, quotaBytes).Scan(&id); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else if _, err := tx.Exec(`UPDATE users SET role = $1 WHERE id = $2`, RoleAdmin, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindByID(id)
}

func (r *Repository) FindByUsername(username string) (*User, error) {
	return scanUser(r.db.QueryRow(
		`SELECT id, username, password_hash, role, status, storage_quota_bytes, session_version, must_change_password, created_at FROM users WHERE username = $1`,
		username,
	))
}

func (r *Repository) FindByID(id int64) (*User, error) {
	return scanUser(r.db.QueryRow(
		`SELECT id, username, password_hash, role, status, storage_quota_bytes, session_version, must_change_password, created_at FROM users WHERE id = $1`,
		id,
	))
}

func (r *Repository) StorageQuotaBytes(userID int64) (int64, error) {
	user, err := r.FindByID(userID)
	if err != nil {
		return 0, err
	}
	return user.StorageQuotaBytes, nil
}

func (r *Repository) ListUsers() ([]User, error) {
	rows, err := r.db.Query(`SELECT id, username, password_hash, role, status, storage_quota_bytes, session_version, must_change_password, created_at FROM users ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}
	return users, rows.Err()
}

func (r *Repository) SetUserStatus(userID int64, status string) (*User, error) {
	result, err := r.db.Exec(`UPDATE users SET status = $1, session_version = session_version + 1 WHERE id = $2`, status, userID)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		return nil, ErrUserNotFound
	}
	return r.FindByID(userID)
}

func (r *Repository) SetUserQuota(userID int64, quotaBytes int64) (*User, error) {
	result, err := r.db.Exec(`UPDATE users SET storage_quota_bytes = $1 WHERE id = $2`, quotaBytes, userID)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		return nil, ErrUserNotFound
	}
	return r.FindByID(userID)
}

func (r *Repository) ResetPassword(userID int64, passwordHash string) (*User, error) {
	result, err := r.db.Exec(`UPDATE users SET password_hash = $1, must_change_password = 1, session_version = session_version + 1 WHERE id = $2`, passwordHash, userID)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		return nil, ErrUserNotFound
	}
	return r.FindByID(userID)
}

func (r *Repository) ChangePassword(userID int64, passwordHash string) (*User, error) {
	result, err := r.db.Exec(`UPDATE users SET password_hash = $1, must_change_password = 0, session_version = session_version + 1 WHERE id = $2`, passwordHash, userID)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		return nil, ErrUserNotFound
	}
	return r.FindByID(userID)
}

func (r *Repository) RevokeAllUserShares(userID int64) (int64, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	fileShares, err := tx.Exec(`DELETE FROM file_shares WHERE user_file_id IN (SELECT id FROM user_files WHERE user_id = $1)`, userID)
	if err != nil {
		return 0, err
	}
	collections, err := tx.Exec(`DELETE FROM share_collections WHERE owner_user_id = $1`, userID)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	fileShareCount, err := fileShares.RowsAffected()
	if err != nil {
		return 0, err
	}
	collectionCount, err := collections.RowsAffected()
	if err != nil {
		return 0, err
	}
	return fileShareCount + collectionCount, nil
}

func (r *Repository) CreateInvitation(codeDigest string, codeHash string, createdByUserID int64, expiresAt time.Time) (*Invitation, error) {
	var id int64
	err := r.db.QueryRow(
		`INSERT INTO invitations (code_digest, code_hash, created_by_user_id, expires_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		codeDigest,
		codeHash,
		createdByUserID,
		expiresAt,
	).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.FindInvitationByID(id)
}

func (r *Repository) FindInvitationByID(id int64) (*Invitation, error) {
	return scanInvitation(r.db.QueryRow(`SELECT id, created_by_user_id, expires_at, used_by_user_id, used_at, revoked_at, created_at FROM invitations WHERE id = $1`, id))
}

func (r *Repository) FindInvitationByDigest(codeDigest string) (*Invitation, string, error) {
	var codeHash string
	invitation, err := scanInvitationWithHash(r.db.QueryRow(`SELECT id, created_by_user_id, expires_at, used_by_user_id, used_at, revoked_at, created_at, code_hash FROM invitations WHERE code_digest = $1`, codeDigest), &codeHash)
	return invitation, codeHash, err
}

func (r *Repository) ListInvitations() ([]Invitation, error) {
	rows, err := r.db.Query(`SELECT id, created_by_user_id, expires_at, used_by_user_id, used_at, revoked_at, created_at FROM invitations ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invitations []Invitation
	for rows.Next() {
		invitation, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		invitations = append(invitations, *invitation)
	}
	return invitations, rows.Err()
}

func (r *Repository) ClaimInvitationAndCreateUser(codeDigest string, username string, passwordHash string, quotaBytes int64, now time.Time) (*User, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var invitationID int64
	err = tx.QueryRow(`SELECT id FROM invitations WHERE code_digest = $1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > $2`, codeDigest, now).Scan(&invitationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvitationNotFound
	}
	if err != nil {
		return nil, err
	}

	var userID int64
	if err := tx.QueryRow(`INSERT INTO users (username, password_hash, storage_quota_bytes) VALUES ($1, $2, $3) RETURNING id`, username, passwordHash, quotaBytes).Scan(&userID); err != nil {
		return nil, err
	}
	result, err := tx.Exec(`UPDATE invitations SET used_by_user_id = $1, used_at = $2 WHERE id = $3 AND used_at IS NULL AND revoked_at IS NULL`, userID, now, invitationID)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected != 1 {
		return nil, ErrInvitationNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindByID(userID)
}

func (r *Repository) RevokeInvitation(id int64) (*Invitation, error) {
	result, err := r.db.Exec(`UPDATE invitations SET revoked_at = CURRENT_TIMESTAMP WHERE id = $1 AND used_at IS NULL AND revoked_at IS NULL`, id)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected == 0 {
		return nil, ErrInvitationNotFound
	}
	return r.FindInvitationByID(id)
}

type userScanner interface{ Scan(...any) error }

func scanUser(scanner userScanner) (*User, error) {
	var user User
	var mustChangePassword bool
	err := scanner.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.Status, &user.StorageQuotaBytes, &user.SessionVersion, &mustChangePassword, &user.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	user.MustChangePassword = mustChangePassword
	return &user, nil
}

type invitationScanner interface{ Scan(...any) error }

func scanInvitation(scanner invitationScanner) (*Invitation, error) {
	return scanInvitationWithHash(scanner, nil)
}

func scanInvitationWithHash(scanner invitationScanner, codeHash *string) (*Invitation, error) {
	var invitation Invitation
	args := []any{&invitation.ID, &invitation.CreatedByUserID, &invitation.ExpiresAt, &invitation.UsedByUserID, &invitation.UsedAt, &invitation.RevokedAt, &invitation.CreatedAt}
	if codeHash != nil {
		args = append(args, codeHash)
	}
	if err := scanner.Scan(args...); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvitationNotFound
	} else if err != nil {
		return nil, err
	}
	return &invitation, nil
}

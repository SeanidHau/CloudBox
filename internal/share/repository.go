package share

import (
	"database/sql"
	"errors"
)

var (
	ErrShareNotFound = errors.New("share not found")
	ErrFileNotFound  = errors.New("file not found")
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db: db,
	}
}

func (r *Repository) Create(share *Share) (*Share, error) {
	var passwordHash any

	if share.PasswordHash != "" {
		passwordHash = share.PasswordHash
	}

	_, err := r.db.Exec(
		`INSERT INTO file_shares (token, user_file_id, password_hash, expires_at, max_downloads) VALUES ($1, $2, $3, $4, $5)`,
		share.Token,
		share.UserFileID,
		passwordHash,
		share.ExpiresAt,
		share.MaxDownloads,
	)
	if err != nil {
		return nil, err
	}

	return r.FindByToken(share.Token)
}

func (r *Repository) FindByToken(token string) (*Share, error) {
	row := r.db.QueryRow(
		`SELECT token, user_file_id, password_hash, expires_at, max_downloads, download_count, created_at FROM file_shares WHERE token = $1`,
		token,
	)

	share, err := scanShare(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShareNotFound
	}
	if err != nil {
		return nil, err
	}

	return share, nil
}

func (r *Repository) HasActiveFile(userID int64, fileID int64) (bool, error) {
	var exists bool

	err := r.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM user_files WHERE id = $1 AND user_id = $2 AND status = 'active')`,
		fileID,
		userID,
	).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

type shareScanner interface {
	Scan(...any) error
}

func scanShare(scanner shareScanner) (*Share, error) {
	var (
		share        Share
		passwordHash sql.NullString
		expiresAt    sql.NullTime
		maxDownloads sql.NullInt64
	)

	err := scanner.Scan(
		&share.Token,
		&share.UserFileID,
		&passwordHash,
		&expiresAt,
		&maxDownloads,
		&share.DownloadCount,
		&share.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	if passwordHash.Valid {
		share.PasswordHash = passwordHash.String
	}
	if expiresAt.Valid {
		share.ExpiresAt = &expiresAt.Time
	}
	if maxDownloads.Valid {
		share.MaxDownloads = &maxDownloads.Int64
	}

	return &share, nil
}

func (r *Repository) FindActiveFileByShareToken(token string) (*SharedFile, error) {
	var file SharedFile

	err := r.db.QueryRow(
		`SELECT fo.id, uf.id, uf.original_name, uf.storage_path, uf.size, uf.content_type, fo.file_hash FROM file_shares AS fs JOIN user_files AS uf ON uf.id = fs.user_file_id JOIN file_objects AS fo ON fo.id = uf.object_id WHERE fs.token = $1 AND uf.status = 'active'`,
		token,
	).Scan(
		&file.ObjectID,
		&file.ID,
		&file.OriginalName,
		&file.StoragePath,
		&file.Size,
		&file.ContentType,
		&file.FileHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFileNotFound
	}
	if err != nil {
		return nil, err
	}

	return &file, nil
}

func (r *Repository) ReserveDownload(token string) (bool, error) {
	result, err := r.db.Exec(
		`UPDATE file_shares SET download_count = download_count + 1 WHERE token = $1 AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP) AND (max_downloads IS NULL OR download_count < max_downloads)`,
		token,
	)
	if err != nil {
		return false, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return affected == 1, nil
}

// ReleaseDownloadReservation compensates for a failed save after the share
// limit has been reserved. It never allows the counter to become negative.
func (r *Repository) ReleaseDownloadReservation(token string) error {
	_, err := r.db.Exec(
		`UPDATE file_shares SET download_count = download_count - 1 WHERE token = $1 AND download_count > 0`,
		token,
	)
	return err
}

func (r *Repository) ListByUser(userID int64) ([]Share, error) {
	rows, err := r.db.Query(
		`SELECT fs.token, fs.user_file_id, fs.password_hash, fs.expires_at, fs.max_downloads, fs.download_count, fs.created_at FROM file_shares AS fs JOIN user_files AS uf ON uf.id = fs.user_file_id WHERE uf.user_id = $1 ORDER BY fs.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	shares := make([]Share, 0)

	for rows.Next() {
		share, err := scanShare(rows)
		if err != nil {
			return nil, err
		}

		shares = append(shares, *share)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return shares, nil
}

func (r *Repository) DeleteByToken(userID int64, token string) error {
	result, err := r.db.Exec(
		`DELETE FROM file_shares WHERE token = $1 AND EXISTS ( SELECT 1 FROM user_files WHERE user_files.id = file_shares.user_file_id AND user_files.user_id = $2)`,
		token,
		userID,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrShareNotFound
	}

	return nil
}

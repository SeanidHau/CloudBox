package share

import (
	"database/sql"
	"errors"
	"time"
)

var (
	ErrShareNotFound = errors.New("share not found")
	ErrFileNotFound  = errors.New("file not found")
)

type Repository struct {
	db *sql.DB
}

func (r *Repository) CreateCollection(share *CollectionShare, fileIDs []int64) (*CollectionShare, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var passwordHash any
	if share.PasswordHash != "" {
		passwordHash = share.PasswordHash
	}
	if _, err := tx.Exec(`INSERT INTO share_collections (token, owner_user_id, password_hash, expires_at, max_downloads) VALUES ($1, $2, $3, $4, $5)`, share.Token, share.OwnerUserID, passwordHash, share.ExpiresAt, share.MaxDownloads); err != nil {
		return nil, err
	}
	for _, fileID := range fileIDs {
		if _, err := tx.Exec(`INSERT INTO share_collection_items (collection_token, user_file_id) VALUES ($1, $2)`, share.Token, fileID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindCollectionByToken(share.Token)
}

func (r *Repository) FindCollectionByToken(token string) (*CollectionShare, error) {
	row := r.db.QueryRow(`SELECT sc.token, sc.owner_user_id, sc.password_hash, sc.expires_at, sc.max_downloads, sc.download_count, sc.created_at, (SELECT COUNT(*) FROM share_collection_items sci WHERE sci.collection_token = sc.token) FROM share_collections sc WHERE sc.token = $1`, token)
	share, err := scanCollectionShare(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShareNotFound
	}
	if err != nil {
		return nil, err
	}
	return share, nil
}

func (r *Repository) ListCollectionFiles(token string) ([]PublicFile, error) {
	rows, err := r.db.Query(`SELECT uf.id, uf.original_name, uf.size, uf.content_type, EXISTS(SELECT 1 FROM file_previews fp WHERE fp.file_object_id = uf.object_id) FROM share_collection_items sci JOIN user_files uf ON uf.id = sci.user_file_id WHERE sci.collection_token = $1 AND uf.status = 'active' ORDER BY uf.created_at DESC`, token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make([]PublicFile, 0)
	for rows.Next() {
		var file PublicFile
		if err := rows.Scan(&file.ID, &file.OriginalName, &file.Size, &file.ContentType, &file.HasPreview); err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return files, nil
}

func (r *Repository) ListCollectionSharedFiles(token string) ([]SharedFile, error) {
	rows, err := r.db.Query(`SELECT fo.id, uf.id, uf.original_name, uf.storage_path, uf.size, uf.content_type, fo.file_hash FROM share_collection_items sci JOIN user_files uf ON uf.id = sci.user_file_id JOIN file_objects fo ON fo.id = uf.object_id WHERE sci.collection_token = $1 AND uf.status = 'active' ORDER BY uf.created_at DESC`, token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make([]SharedFile, 0)
	for rows.Next() {
		var file SharedFile
		if err := rows.Scan(&file.ObjectID, &file.ID, &file.OriginalName, &file.StoragePath, &file.Size, &file.ContentType, &file.FileHash); err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, rows.Err()
}

func (r *Repository) FindCollectionFile(token string, fileID int64) (*SharedFile, error) {
	var file SharedFile
	err := r.db.QueryRow(`SELECT fo.id, uf.id, uf.original_name, uf.storage_path, uf.size, uf.content_type, fo.file_hash FROM share_collection_items sci JOIN user_files uf ON uf.id = sci.user_file_id JOIN file_objects fo ON fo.id = uf.object_id WHERE sci.collection_token = $1 AND sci.user_file_id = $2 AND uf.status = 'active'`, token, fileID).Scan(&file.ObjectID, &file.ID, &file.OriginalName, &file.StoragePath, &file.Size, &file.ContentType, &file.FileHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFileNotFound
	}
	if err != nil {
		return nil, err
	}
	return &file, nil
}

func (r *Repository) ReserveCollectionDownload(token string) (bool, error) {
	return r.ReserveCollectionDownloads(token, 1)
}

func (r *Repository) ReserveCollectionDownloads(token string, count int) (bool, error) {
	if count <= 0 {
		return false, nil
	}
	result, err := r.db.Exec(`UPDATE share_collections SET download_count = download_count + $2 WHERE token = $1 AND expires_at > CURRENT_TIMESTAMP AND (max_downloads IS NULL OR download_count + $2 <= max_downloads)`, token, count)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (r *Repository) ReleaseCollectionDownloadReservations(token string, count int) error {
	if count <= 0 {
		return nil
	}
	_, err := r.db.Exec(`UPDATE share_collections SET download_count = CASE WHEN download_count >= $2 THEN download_count - $2 ELSE 0 END WHERE token = $1`, token, count)
	return err
}

func (r *Repository) ListCollectionsByUser(userID int64) ([]CollectionShare, error) {
	rows, err := r.db.Query(`SELECT sc.token, sc.owner_user_id, sc.password_hash, sc.expires_at, sc.max_downloads, sc.download_count, sc.created_at, (SELECT COUNT(*) FROM share_collection_items sci WHERE sci.collection_token = sc.token) FROM share_collections sc WHERE sc.owner_user_id = $1 ORDER BY sc.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shares := make([]CollectionShare, 0)
	for rows.Next() {
		share, err := scanCollectionShare(rows)
		if err != nil {
			return nil, err
		}
		shares = append(shares, *share)
	}
	return shares, rows.Err()
}

func (r *Repository) DeleteCollectionByToken(userID int64, token string) error {
	result, err := r.db.Exec(`DELETE FROM share_collections WHERE token = $1 AND owner_user_id = $2`, token, userID)
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

func scanCollectionShare(scanner shareScanner) (*CollectionShare, error) {
	var (
		share        CollectionShare
		passwordHash sql.NullString
		expiresAt    time.Time
		maxDownloads sql.NullInt64
	)
	if err := scanner.Scan(&share.Token, &share.OwnerUserID, &passwordHash, &expiresAt, &maxDownloads, &share.DownloadCount, &share.CreatedAt, &share.FileCount); err != nil {
		return nil, err
	}
	share.ExpiresAt = &expiresAt
	if passwordHash.Valid {
		share.PasswordHash = passwordHash.String
	}
	if maxDownloads.Valid {
		share.MaxDownloads = &maxDownloads.Int64
	}
	return &share, nil
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

func (r *Repository) FindPreviewByShareToken(token string) (*SharedPreview, error) {
	var preview SharedPreview

	err := r.db.QueryRow(
		`SELECT fp.storage_path, fp.content_type, fp.created_at
		 FROM file_shares AS fs
		 JOIN user_files AS uf ON uf.id = fs.user_file_id
		 JOIN file_previews AS fp ON fp.file_object_id = uf.object_id
		 WHERE fs.token = $1 AND uf.status = 'active'`,
		token,
	).Scan(&preview.StoragePath, &preview.ContentType, &preview.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFileNotFound
	}
	if err != nil {
		return nil, err
	}

	return &preview, nil
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
		`SELECT fs.token, fs.user_file_id, fs.password_hash, fs.expires_at, fs.max_downloads, fs.download_count, fs.created_at,
			uf.original_name, uf.size, uf.content_type,
			EXISTS(SELECT 1 FROM file_previews AS fp WHERE fp.file_object_id = uf.object_id)
		 FROM file_shares AS fs
		 JOIN user_files AS uf ON uf.id = fs.user_file_id
		 WHERE uf.user_id = $1
		 ORDER BY fs.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	shares := make([]Share, 0)

	for rows.Next() {
		share, err := scanShareWithFile(rows)
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

func scanShareWithFile(scanner shareScanner) (*Share, error) {
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
		&share.OriginalName,
		&share.Size,
		&share.ContentType,
		&share.HasPreview,
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

func (r *Repository) RecordShareAccess(audit AccessAudit) error {
	_, err := r.db.Exec(`INSERT INTO share_access_audits (token, ip_hash, action, result) VALUES ($1, $2, $3, $4)`, audit.Token, audit.IPHash, audit.Action, audit.Result)
	return err
}

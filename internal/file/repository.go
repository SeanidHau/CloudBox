package file

import (
	"database/sql"
	"errors"
)

var ErrFileNotFound = errors.New("file not found")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db: db,
	}
}

func (r *Repository) Create(userID int64, originalName string, storagePath string, size int64, contentType string) (*UserFile, error) {
	result, err := r.db.Exec(
		`INSERT INTO user_files (user_id, original_name, storage_path, size, content_type, status) VALUES (?, ?, ?, ?, ?, ?)`,
		userID,
		originalName,
		storagePath,
		size,
		contentType,
		StatusActive,
	)

	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return r.FindActiveByID(userID, id)
}

func (r *Repository) FindActiveByID(userID int64, fileID int64) (*UserFile, error) {
	return r.findByIDAndStatus(userID, fileID, StatusActive)
}

func (r *Repository) FindDeletedByID(userID int64, fileID int64) (*UserFile, error) {
	return r.findByIDAndStatus(userID, fileID, StatusDeleted)
}

func (r *Repository) ListActive(userID int64) ([]UserFile, error) {
	return r.listByStatus(userID, StatusActive)
}

func (r *Repository) ListDeleted(userID int64) ([]UserFile, error) {
	return r.listByStatus(userID, StatusDeleted)
}

func (r *Repository) SoftDelete(userID int64, fileID int64) error {
	result, err := r.db.Exec(
		`UPDATE user_files SET status = ?, deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ? AND status = ?`,
		StatusDeleted,
		fileID,
		userID,
		StatusActive,
	)

	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if affected == 0 {
		return ErrFileNotFound
	}

	return nil
}

func (r *Repository) Restore(userID int64, fileID int64) error {
	result, err := r.db.Exec(
		`UPDATE user_files SET status = ?, deleted_at = NULL WHERE id = ? AND user_id = ? AND status = ?`,
		StatusActive,
		fileID,
		userID,
		StatusDeleted,
	)

	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if affected == 0 {
		return ErrFileNotFound
	}

	return nil
}

func (r *Repository) findByIDAndStatus(userID int64, fileID int64, status string) (*UserFile, error) {
	var file UserFile

	err := r.db.QueryRow(
		`SELECT id, user_id, original_name, storage_path, size, content_type, status, created_at, deleted_at FROM user_files WHERE id = ? AND user_id = ? AND status = ?`,
		fileID,
		userID,
		status,
	).Scan(
		&file.ID,
		&file.UserID,
		&file.OriginalName,
		&file.StoragePath,
		&file.Size,
		&file.ContentType,
		&file.Status,
		&file.CreatedAt,
		&file.DeletedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFileNotFound
	}

	if err != nil {
		return nil, err
	}

	return &file, nil
}

func (r *Repository) listByStatus(userID int64, status string) ([]UserFile, error) {
	rows, err := r.db.Query(
		`SELECT id, user_id, original_name, storage_path, size, content_type, status, created_at, deleted_at FROM user_files WHERE user_id = ? AND status = ? ORDER BY created_at DESC`,
		userID,
		status,
	)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := make([]UserFile, 0)

	for rows.Next() {
		var file UserFile

		if err := rows.Scan(
			&file.ID,
			&file.UserID,
			&file.OriginalName,
			&file.StoragePath,
			&file.Size,
			&file.ContentType,
			&file.Status,
			&file.CreatedAt,
			&file.DeletedAt,
		); err != nil {
			return nil, err
		}

		files = append(files, file)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return files, nil
}

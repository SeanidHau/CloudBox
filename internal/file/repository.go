package file

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrFileNotFound = errors.New("file not found")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, params CreateFileParams) (UserFile, error) {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO user_files (user_id, original_name, storage_path, size, content_type, status)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		params.UserID,
		params.OriginalName,
		params.StoragePath,
		params.Size,
		params.ContentType,
		StatusActive,
	)
	if err != nil {
		return UserFile{}, fmt.Errorf("insert file metadata: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return UserFile{}, fmt.Errorf("read inserted file id: %w", err)
	}

	return r.FindByID(ctx, params.UserID, id)
}

func (r *Repository) ListByStatus(ctx context.Context, userID int64, status string) ([]UserFile, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, original_name, storage_path, size, content_type, status, created_at, deleted_at
		 FROM user_files
		 WHERE user_id = ? AND status = ?
		 ORDER BY created_at DESC, id DESC`,
		userID,
		status,
	)
	if err != nil {
		return nil, fmt.Errorf("query files: %w", err)
	}
	defer rows.Close()

	files := make([]UserFile, 0)
	for rows.Next() {
		userFile, err := scanUserFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, userFile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate files: %w", err)
	}

	return files, nil
}

func (r *Repository) FindByID(ctx context.Context, userID, fileID int64) (UserFile, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, original_name, storage_path, size, content_type, status, created_at, deleted_at
		 FROM user_files
		 WHERE user_id = ? AND id = ?`,
		userID,
		fileID,
	)

	userFile, err := scanUserFile(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserFile{}, ErrFileNotFound
		}
		return UserFile{}, err
	}
	return userFile, nil
}

func (r *Repository) SoftDelete(ctx context.Context, userID, fileID int64) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE user_files
		 SET status = ?, deleted_at = ?
		 WHERE user_id = ? AND id = ? AND status = ?`,
		StatusDeleted,
		time.Now().UTC().Format(time.RFC3339),
		userID,
		fileID,
		StatusActive,
	)
	if err != nil {
		return fmt.Errorf("soft delete file: %w", err)
	}
	return requireAffected(result)
}

func (r *Repository) Restore(ctx context.Context, userID, fileID int64) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE user_files
		 SET status = ?, deleted_at = NULL
		 WHERE user_id = ? AND id = ? AND status = ?`,
		StatusActive,
		userID,
		fileID,
		StatusDeleted,
	)
	if err != nil {
		return fmt.Errorf("restore file: %w", err)
	}
	return requireAffected(result)
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanUserFile(row scanner) (UserFile, error) {
	var userFile UserFile
	var deletedAt sql.NullString
	err := row.Scan(
		&userFile.ID,
		&userFile.UserID,
		&userFile.OriginalName,
		&userFile.StoragePath,
		&userFile.Size,
		&userFile.ContentType,
		&userFile.Status,
		&userFile.CreatedAt,
		&deletedAt,
	)
	if err != nil {
		return UserFile{}, err
	}
	if deletedAt.Valid {
		userFile.DeletedAt = &deletedAt.String
	}
	return userFile, nil
}

func requireAffected(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if rows == 0 {
		return ErrFileNotFound
	}
	return nil
}

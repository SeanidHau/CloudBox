package upload

import (
	"database/sql"
	"errors"
)

var ErrTaskNotFound = errors.New("upload task not found")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(task *Task) (*Task, error) {
	_, err := r.db.Exec(
		`INSERT INTO upload_tasks (id, user_id, original_name, content_type, file_size, chunk_size, total_chunks, file_hash, status, temp_dir) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		task.UserID,
		task.OriginalName,
		task.ContentType,
		task.FileSize,
		task.ChunkSize,
		task.TotalChunks,
		task.FileHash,
		task.Status,
		task.TempDir,
	)
	if err != nil {
		return nil, err
	}

	return r.FindByID(task.UserID, task.ID)
}

func (r *Repository) FindByID(userID int64, taskID string) (*Task, error) {
	var task Task

	err := r.db.QueryRow(
		`SELECT id, user_id, original_name, content_type, file_size, chunk_size, total_chunks, file_hash, status, temp_dir, created_at, updated_at FROM upload_tasks WHERE id = ? AND user_id = ?`,
		taskID,
		userID,
	).Scan(
		&task.ID,
		&task.UserID,
		&task.OriginalName,
		&task.ContentType,
		&task.FileSize,
		&task.ChunkSize,
		&task.TotalChunks,
		&task.FileHash,
		&task.Status,
		&task.TempDir,
		&task.CreatedAt,
		&task.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}

	return &task, nil
}

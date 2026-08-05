package upload

import (
	"database/sql"
	"errors"
	"time"
)

var (
	ErrTaskNotFound  = errors.New("upload task not found")
	ErrChunkNotFound = errors.New("upload chunk not found")
)

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

func (r *Repository) UpsertChunk(chunk *Chunk) (*Chunk, error) {
	_, err := r.db.Exec(
		`INSERT INTO upload_chunks (upload_id, chunk_number, size, chunk_hash) VALUES (?, ?, ?, ?) ON CONFLICT(upload_id, chunk_number) DO UPDATE SET size = excluded.size, chunk_hash = excluded.chunk_hash`,
		chunk.UploadID,
		chunk.Number,
		chunk.Size,
		chunk.Hash,
	)
	if err != nil {
		return nil, err
	}

	return r.FindChunk(chunk.UploadID, chunk.Number)
}

func (r *Repository) FindChunk(uploadID string, chunkNumber int64) (*Chunk, error) {
	var chunk Chunk

	err := r.db.QueryRow(
		`SELECT upload_id, chunk_number, size, chunk_hash, created_at FROM upload_chunks WHERE upload_id = ? AND chunk_number = ?`,
		uploadID,
		chunkNumber,
	).Scan(
		&chunk.UploadID,
		&chunk.Number,
		&chunk.Size,
		&chunk.Hash,
		&chunk.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChunkNotFound
	}
	if err != nil {
		return nil, err
	}

	return &chunk, nil
}

func (r *Repository) ListChunks(uploadID string) ([]Chunk, error) {
	rows, err := r.db.Query(
		`SELECT upload_id, chunk_number, size, chunk_hash, created_at FROM upload_chunks WHERE upload_id = ? ORDER BY chunk_number`,
		uploadID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chunks := make([]Chunk, 0)

	for rows.Next() {
		var chunk Chunk

		if err := rows.Scan(
			&chunk.UploadID,
			&chunk.Number,
			&chunk.Size,
			&chunk.Hash,
			&chunk.CreatedAt,
		); err != nil {
			return nil, err
		}

		chunks = append(chunks, chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return chunks, nil
}

func (r *Repository) TransitionStatus(userID int64, taskID string, fromStatus string, toStatus string) (bool, error) {
	result, err := r.db.Exec(
		`UPDATE upload_tasks SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ? AND status = ?`,
		toStatus,
		taskID,
		userID,
		fromStatus,
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

func (r *Repository) TouchUploading(userID int64, taskID string) (bool, error) {
	result, err := r.db.Exec(
		`UPDATE upload_tasks SET updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ? AND status = ?`,
		taskID,
		userID,
		StatusUploading,
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

func (r *Repository) ListExpiredUploading(before time.Time) ([]Task, error) {
	rows, err := r.db.Query(
		`SELECT id, user_id, original_name, content_type, file_size, chunk_size, total_chunks, file_hash, status, temp_dir, created_at, updated_at FROM upload_tasks WHERE status = ? AND updated_at < ? ORDER BY updated_at`,
		StatusUploading,
		before.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]Task, 0)

	for rows.Next() {
		var task Task
		if err := rows.Scan(
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
		); err != nil {
			return nil, err
		}

		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tasks, nil
}

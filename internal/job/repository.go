package job

import (
	"database/sql"
	"errors"
	"time"
)

var (
	ErrJobNotFound    = errors.New("background job not found")
	ErrNoJobAvailable = errors.New("no background job available")
	ErrJobNotRunning  = errors.New("background job is not running")
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db: db,
	}
}

func (r *Repository) Create(job *Job) (*Job, error) {
	_, err := r.db.Exec(
		`INSERT INTO background_jobs (id, user_id, job_type, payload, status, max_attempts, run_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		job.ID,
		job.UserID,
		job.JobType,
		string(job.Payload),
		job.Status,
		job.MaxAttempts,
		job.RunAt,
	)
	if err != nil {
		return nil, err
	}

	return r.FindByID(job.ID)
}

func (r *Repository) FindByID(jobID string) (*Job, error) {
	row := r.db.QueryRow(
		`SELECT id, user_id, job_type, payload, status, attempts, max_attempts, run_at, locked_at, last_error, created_at, updated_at FROM background_jobs WHERE id = $1`,
		jobID,
	)

	return scanJob(row)
}

func (r *Repository) FindByIDForUser(jobID string, userID int64) (*Job, error) {
	row := r.db.QueryRow(
		`SELECT id, user_id, job_type, payload, status, attempts, max_attempts, run_at, locked_at, last_error, created_at, updated_at FROM background_jobs WHERE id = $1 AND user_id = $2`,
		jobID,
		userID,
	)

	return scanJob(row)
}

func (r *Repository) ClaimNext(now time.Time) (*Job, error) {
	row := r.db.QueryRow(
		`UPDATE background_jobs SET status = $1, attempts = attempts + 1, locked_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = (SELECT id FROM background_jobs WHERE status = $2 AND run_at <= $3 ORDER BY run_at, created_at LIMIT 1) AND status = $2 RETURNING id, user_id, job_type, payload, status, attempts, max_attempts, run_at, locked_at, last_error, created_at, updated_at`,
		StatusRunning,
		StatusQueued,
		now.UTC(),
	)

	job, err := scanJob(row)
	if errors.Is(err, ErrJobNotFound) {
		return nil, ErrNoJobAvailable
	}
	if err != nil {
		return nil, err
	}

	return job, nil
}

func (r *Repository) MarkSucceeded(jobID string) (bool, error) {
	result, err := r.db.Exec(
		`UPDATE background_jobs SET status = $1, locked_at = NULL, last_error = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND status = $3`,
		StatusSucceeded,
		jobID,
		StatusRunning,
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

func (r *Repository) RetryOrFail(
	jobID string,
	lastError string,
	nextRunAt time.Time,
) (*Job, error) {
	row := r.db.QueryRow(
		`UPDATE background_jobs SET status = CASE WHEN attempts >= max_attempts THEN $1 ELSE $2 END, run_at = CASE WHEN attempts >= max_attempts THEN run_at ELSE $3 END, locked_at = NULL, last_error = $4, updated_at = CURRENT_TIMESTAMP WHERE id = $5 AND status = $6 RETURNING id, user_id, job_type, payload, status, attempts, max_attempts, run_at, locked_at, last_error, created_at, updated_at`,
		StatusFailed,
		StatusQueued,
		nextRunAt,
		lastError,
		jobID,
		StatusRunning,
	)

	job, err := scanJob(row)
	if errors.Is(err, ErrJobNotFound) {
		return nil, ErrJobNotRunning
	}
	if err != nil {
		return nil, err
	}

	return job, nil
}

type jobScanner interface {
	Scan(...any) error
}

func scanJob(scanner jobScanner) (*Job, error) {
	var (
		userID  sql.NullInt64
		job     Job
		payload string
	)

	err := scanner.Scan(
		&job.ID,
		&userID,
		&job.JobType,
		&payload,
		&job.Status,
		&job.Attempts,
		&job.MaxAttempts,
		&job.RunAt,
		&job.LockedAt,
		&job.LastError,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}

	if userID.Valid {
		job.UserID = &userID.Int64
	}

	job.Payload = []byte(payload)

	return &job, nil
}

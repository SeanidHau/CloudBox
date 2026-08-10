//go:build integration

package job

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SeanidHau/CloudBox/internal/database"
	"github.com/google/uuid"
)

func TestRepositoryClaimNextPostgres(t *testing.T) {
	databaseURL := os.Getenv("POSTGRES_INTEGRATION_URL")
	if databaseURL == "" {
		t.Skip("set POSTGRES_INTEGRATION_URL to run the PostgreSQL integration test")
	}

	db, err := database.OpenPostgres(databaseURL)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := database.Migrate(
		db,
		filepath.Join("..", "..", "migrations", "postgres", "001_init.sql"),
		filepath.Join("..", "..", "migrations", "postgres", "002_background_jobs.sql"),
		filepath.Join("..", "..", "migrations", "postgres", "003_background_job_user.sql"),
	); err != nil {
		t.Fatalf("apply Postgres migrations: %v", err)
	}

	repo := NewRepository(db)
	readyID := "postgres-ready-" + uuid.NewString()
	futureID := "postgres-future-" + uuid.NewString()
	username := "postgres-job-owner-" + uuid.NewString()
	var userID int64
	if err := db.QueryRow(
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		username,
		"hash",
	).Scan(&userID); err != nil {
		t.Fatalf("create job owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM background_jobs WHERE id IN ($1, $2)`, readyID, futureID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	})

	if _, err := repo.Create(&Job{
		ID:          readyID,
		UserID:      &userID,
		JobType:     TypeVerifyFile,
		Payload:     []byte(`{"file_id":42}`),
		Status:      StatusQueued,
		MaxAttempts: DefaultMaxAttempts,
		RunAt:       time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("create ready job: %v", err)
	}
	if _, err := repo.Create(&Job{
		ID:          futureID,
		JobType:     TypeVerifyFile,
		Payload:     []byte(`{"file_id":43}`),
		Status:      StatusQueued,
		MaxAttempts: DefaultMaxAttempts,
		RunAt:       time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("create future job: %v", err)
	}

	claimed, err := repo.ClaimNext(time.Now().UTC())
	if err != nil {
		t.Fatalf("claim ready job: %v", err)
	}
	if claimed.ID != readyID || claimed.Status != StatusRunning || claimed.Attempts != 1 {
		t.Fatalf("claimed job = %#v, want running first attempt of %q", claimed, readyID)
	}
	if claimed.UserID == nil || *claimed.UserID != userID {
		t.Fatalf("claimed user ID = %v, want %d", claimed.UserID, userID)
	}

	if _, err := repo.ClaimNext(time.Now().UTC()); !errors.Is(err, ErrNoJobAvailable) {
		t.Fatalf("claim future-only queue error = %v, want %v", err, ErrNoJobAvailable)
	}
}

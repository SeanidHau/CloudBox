package job

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestServiceEnqueueCreatesImmediatelyRunnableJob(t *testing.T) {
	service := NewService(newTestRepository(t))

	job, err := service.Enqueue("  "+TypeVerifyFile+"  ", struct {
		FileID int64 `json:"file_id"`
	}{
		FileID: 42,
	})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	if _, err := uuid.Parse(job.ID); err != nil {
		t.Fatalf("job ID = %q, want UUID: %v", job.ID, err)
	}
	if job.JobType != TypeVerifyFile {
		t.Fatalf("job type = %q, want trimmed %q", job.JobType, TypeVerifyFile)
	}
	if job.Status != StatusQueued || job.Attempts != 0 {
		t.Fatalf("job status/attempts = %q/%d, want queued/0", job.Status, job.Attempts)
	}
	if job.MaxAttempts != DefaultMaxAttempts {
		t.Fatalf("max attempts = %d, want %d", job.MaxAttempts, DefaultMaxAttempts)
	}
	if job.RunAt.IsZero() {
		t.Fatal("enqueue run_at should be set")
	}

	var payload struct {
		FileID int64 `json:"file_id"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.FileID != 42 {
		t.Fatalf("payload file ID = %d, want 42", payload.FileID)
	}
}

func TestServiceSchedulePreservesRequestedRunTime(t *testing.T) {
	service := NewService(newTestRepository(t))
	runAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)

	job, err := service.Schedule(TypeVerifyFile, map[string]int64{"file_id": 42}, runAt)
	if err != nil {
		t.Fatalf("schedule job: %v", err)
	}
	if !job.RunAt.Equal(runAt) {
		t.Fatalf("run at = %s, want %s", job.RunAt, runAt)
	}
}

func TestServiceScheduleUsesCurrentTimeForZeroRunTime(t *testing.T) {
	service := NewService(newTestRepository(t))
	before := time.Now().UTC().Add(-time.Second)

	job, err := service.Schedule(TypeVerifyFile, map[string]int64{"file_id": 42}, time.Time{})
	if err != nil {
		t.Fatalf("schedule zero-time job: %v", err)
	}
	if job.RunAt.Before(before) {
		t.Fatalf("zero-time run at = %s, want current time", job.RunAt)
	}
}

func TestServiceEnqueueForUserAssociatesJobWithUser(t *testing.T) {
	service := NewService(newTestRepository(t))

	job, err := service.EnqueueForUser(1, TypeVerifyFile, map[string]int64{"file_id": 42})
	if err != nil {
		t.Fatalf("enqueue user job: %v", err)
	}
	if job.UserID == nil || *job.UserID != 1 {
		t.Fatalf("job user ID = %v, want 1", job.UserID)
	}
}

func TestServiceGetForUserRestrictsOwnership(t *testing.T) {
	service := NewService(newTestRepository(t))

	created, err := service.EnqueueForUser(1, TypeVerifyFile, map[string]int64{"file_id": 42})
	if err != nil {
		t.Fatalf("enqueue user job: %v", err)
	}

	found, err := service.GetForUser(1, created.ID)
	if err != nil {
		t.Fatalf("get job for owner: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("job ID = %q, want %q", found.ID, created.ID)
	}

	if _, err := service.GetForUser(2, created.ID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("get job for another user error = %v, want %v", err, ErrJobNotFound)
	}
	if _, err := service.GetForUser(1, "  "); !errors.Is(err, ErrInvalidJobID) {
		t.Fatalf("get blank job ID error = %v, want %v", err, ErrInvalidJobID)
	}
}

func TestServiceScheduleRejectsInvalidJobTypeAndPayload(t *testing.T) {
	service := NewService(newTestRepository(t))

	for _, jobType := range []string{"", "   "} {
		t.Run("empty type "+jobType, func(t *testing.T) {
			if _, err := service.Enqueue(jobType, map[string]int64{"file_id": 42}); !errors.Is(err, ErrInvalidJobType) {
				t.Fatalf("enqueue error = %v, want %v", err, ErrInvalidJobType)
			}
		})
	}

	if _, err := service.Enqueue(TypeVerifyFile, make(chan int)); err == nil {
		t.Fatal("enqueue channel payload should fail JSON encoding")
	}
	if _, err := service.EnqueueForUser(0, TypeVerifyFile, map[string]int64{"file_id": 42}); !errors.Is(err, ErrInvalidJobUserID) {
		t.Fatalf("enqueue invalid user error = %v, want %v", err, ErrInvalidJobUserID)
	}
}

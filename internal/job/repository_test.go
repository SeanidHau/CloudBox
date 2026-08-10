package job

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SeanidHau/CloudBox/internal/database"
)

func newTestRepository(t *testing.T) *Repository {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "cloudbox-test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := database.Migrate(
		db,
		"../../migrations/001_init.sql",
		"../../migrations/008_background_jobs.sql",
		"../../migrations/009_background_job_user.sql",
	); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'job-owner', 'hash')`); err != nil {
		t.Fatalf("insert job owner: %v", err)
	}

	return NewRepository(db)
}

func TestRepositoryCreateAndFindJob(t *testing.T) {
	repo := newTestRepository(t)
	runAt := time.Now().UTC().Add(time.Minute).Truncate(time.Second)
	userID := int64(1)

	created, err := repo.Create(&Job{
		ID:          "job-1",
		UserID:      &userID,
		JobType:     TypeVerifyFile,
		Payload:     json.RawMessage(`{"file_id":42}`),
		Status:      StatusQueued,
		MaxAttempts: 5,
		RunAt:       runAt,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	if created.ID != "job-1" || created.JobType != TypeVerifyFile {
		t.Fatalf("created job = %#v, want job-1 of type %q", created, TypeVerifyFile)
	}
	if created.UserID == nil || *created.UserID != userID {
		t.Fatalf("job user ID = %v, want %d", created.UserID, userID)
	}
	if string(created.Payload) != `{"file_id":42}` {
		t.Fatalf("payload = %s, want file ID payload", created.Payload)
	}
	if created.Status != StatusQueued {
		t.Fatalf("status = %q, want %q", created.Status, StatusQueued)
	}
	if created.Attempts != 0 || created.MaxAttempts != 5 {
		t.Fatalf("attempts = %d/%d, want 0/5", created.Attempts, created.MaxAttempts)
	}
	if !created.RunAt.Equal(runAt) {
		t.Fatalf("run at = %s, want %s", created.RunAt, runAt)
	}
	if created.LockedAt.Valid || created.LastError.Valid {
		t.Fatalf("new job lock and error = %#v/%#v, want both null", created.LockedAt, created.LastError)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("timestamps = %s/%s, want database timestamps", created.CreatedAt, created.UpdatedAt)
	}

	found, err := repo.FindByID("job-1")
	if err != nil {
		t.Fatalf("find job: %v", err)
	}
	if found.ID != created.ID || found.CreatedAt.IsZero() {
		t.Fatalf("found job = %#v, want stored job", found)
	}
}

func TestRepositoryFindByIDReturnsNotFound(t *testing.T) {
	repo := newTestRepository(t)

	if _, err := repo.FindByID("missing-job"); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("find missing job error = %v, want %v", err, ErrJobNotFound)
	}
}

func TestRepositoryFindByIDForUserRestrictsOwnership(t *testing.T) {
	repo := newTestRepository(t)
	ownerID := int64(1)

	if _, err := repo.Create(&Job{
		ID:          "owned-job",
		UserID:      &ownerID,
		JobType:     TypeVerifyFile,
		Payload:     json.RawMessage(`{"file_id":42}`),
		Status:      StatusQueued,
		MaxAttempts: DefaultMaxAttempts,
		RunAt:       time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create owned job: %v", err)
	}

	found, err := repo.FindByIDForUser("owned-job", ownerID)
	if err != nil {
		t.Fatalf("find job for owner: %v", err)
	}
	if found.ID != "owned-job" {
		t.Fatalf("found job ID = %q, want owned-job", found.ID)
	}

	if _, err := repo.FindByIDForUser("owned-job", 2); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("find job for another user error = %v, want %v", err, ErrJobNotFound)
	}
}

func TestRepositoryClaimNextUsesRunTimeOrder(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Now().UTC().Truncate(time.Second)

	createQueuedJob(t, repo, "later-job", now.Add(-time.Minute))
	createQueuedJob(t, repo, "earlier-job", now.Add(-2*time.Minute))
	createQueuedJob(t, repo, "future-job", now.Add(time.Hour))

	first, err := repo.ClaimNext(now)
	if err != nil {
		t.Fatalf("claim first job: %v", err)
	}
	if first.ID != "earlier-job" {
		t.Fatalf("first claimed job = %q, want earlier-job", first.ID)
	}
	assertClaimedJob(t, first)

	second, err := repo.ClaimNext(now)
	if err != nil {
		t.Fatalf("claim second job: %v", err)
	}
	if second.ID != "later-job" {
		t.Fatalf("second claimed job = %q, want later-job", second.ID)
	}
	assertClaimedJob(t, second)

	if _, err := repo.ClaimNext(now); !errors.Is(err, ErrNoJobAvailable) {
		t.Fatalf("claim future-only queue error = %v, want %v", err, ErrNoJobAvailable)
	}
}

func TestRepositoryClaimNextAllowsOnlyOneConcurrentClaim(t *testing.T) {
	repo := newTestRepository(t)
	createQueuedJob(t, repo, "single-job", time.Now().UTC().Add(-time.Minute))

	results := make(chan *Job, 2)
	errorsByWorker := make(chan error, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup

	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start

			job, err := repo.ClaimNext(time.Now().UTC())
			results <- job
			errorsByWorker <- err
		}()
	}

	close(start)
	workers.Wait()
	close(results)
	close(errorsByWorker)

	claimed := 0
	noJobAvailable := 0

	for job := range results {
		if job != nil {
			claimed++
			if job.ID != "single-job" {
				t.Fatalf("claimed job = %q, want single-job", job.ID)
			}
		}
	}
	for err := range errorsByWorker {
		switch {
		case err == nil:
		case errors.Is(err, ErrNoJobAvailable):
			noJobAvailable++
		default:
			t.Fatalf("concurrent claim error = %v", err)
		}
	}

	if claimed != 1 || noJobAvailable != 1 {
		t.Fatalf("claimed/no-job = %d/%d, want 1/1", claimed, noJobAvailable)
	}
}

func TestRepositoryMarkSucceededCompletesRunningJob(t *testing.T) {
	repo := newTestRepository(t)
	createQueuedJob(t, repo, "successful-job", time.Now().UTC().Add(-time.Minute))

	claimed, err := repo.ClaimNext(time.Now().UTC())
	if err != nil {
		t.Fatalf("claim job: %v", err)
	}

	completed, err := repo.MarkSucceeded(claimed.ID)
	if err != nil {
		t.Fatalf("mark job succeeded: %v", err)
	}
	if !completed {
		t.Fatal("mark succeeded should update the running job")
	}

	updated, err := repo.FindByID(claimed.ID)
	if err != nil {
		t.Fatalf("find completed job: %v", err)
	}
	if updated.Status != StatusSucceeded {
		t.Fatalf("status = %q, want %q", updated.Status, StatusSucceeded)
	}
	if updated.LockedAt.Valid || updated.LastError.Valid {
		t.Fatalf("completed job lock/error = %#v/%#v, want both null", updated.LockedAt, updated.LastError)
	}

	completed, err = repo.MarkSucceeded(claimed.ID)
	if err != nil {
		t.Fatalf("repeat mark succeeded: %v", err)
	}
	if completed {
		t.Fatal("completed job should not be marked succeeded twice")
	}
}

func TestRepositoryRetryOrFailRetriesThenExhaustsAttempts(t *testing.T) {
	repo := newTestRepository(t)

	if _, err := repo.Create(&Job{
		ID:          "retry-job",
		JobType:     TypeVerifyFile,
		Payload:     json.RawMessage(`{"file_id":42}`),
		Status:      StatusQueued,
		MaxAttempts: 2,
		RunAt:       time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("create retry job: %v", err)
	}

	firstClaim, err := repo.ClaimNext(time.Now().UTC())
	if err != nil {
		t.Fatalf("claim first attempt: %v", err)
	}

	nextRunAt := time.Now().UTC().Add(-time.Second).Truncate(time.Second)
	retried, err := repo.RetryOrFail(firstClaim.ID, "temporary storage error", nextRunAt)
	if err != nil {
		t.Fatalf("retry first attempt: %v", err)
	}
	if retried.Status != StatusQueued {
		t.Fatalf("first failure status = %q, want %q", retried.Status, StatusQueued)
	}
	if retried.Attempts != 1 || !retried.LastError.Valid || retried.LastError.String != "temporary storage error" {
		t.Fatalf("retried job = %#v, want first attempt and stored error", retried)
	}
	if retried.LockedAt.Valid {
		t.Fatal("retried job should not remain locked")
	}
	if !retried.RunAt.Equal(nextRunAt) {
		t.Fatalf("retry run at = %s, want %s", retried.RunAt, nextRunAt)
	}

	secondClaim, err := repo.ClaimNext(time.Now().UTC())
	if err != nil {
		t.Fatalf("claim second attempt: %v", err)
	}
	failed, err := repo.RetryOrFail(secondClaim.ID, "permanent storage error", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("fail final attempt: %v", err)
	}
	if failed.Status != StatusFailed || failed.Attempts != 2 {
		t.Fatalf("final job = %#v, want failed after two attempts", failed)
	}
	if !failed.LastError.Valid || failed.LastError.String != "permanent storage error" {
		t.Fatalf("final error = %#v, want permanent storage error", failed.LastError)
	}
	if _, err := repo.ClaimNext(time.Now().UTC()); !errors.Is(err, ErrNoJobAvailable) {
		t.Fatalf("claim exhausted job error = %v, want %v", err, ErrNoJobAvailable)
	}
}

func TestRepositoryRetryOrFailRejectsNonRunningJob(t *testing.T) {
	repo := newTestRepository(t)
	createQueuedJob(t, repo, "queued-job", time.Now().UTC().Add(time.Hour))

	if _, err := repo.RetryOrFail("queued-job", "error", time.Now().UTC()); !errors.Is(err, ErrJobNotRunning) {
		t.Fatalf("retry queued job error = %v, want %v", err, ErrJobNotRunning)
	}
}

func createQueuedJob(t *testing.T, repo *Repository, id string, runAt time.Time) {
	t.Helper()

	if _, err := repo.Create(&Job{
		ID:          id,
		JobType:     TypeVerifyFile,
		Payload:     json.RawMessage(`{"file_id":42}`),
		Status:      StatusQueued,
		MaxAttempts: DefaultMaxAttempts,
		RunAt:       runAt,
	}); err != nil {
		t.Fatalf("create queued job %q: %v", id, err)
	}
}

func assertClaimedJob(t *testing.T, job *Job) {
	t.Helper()

	if job.Status != StatusRunning {
		t.Fatalf("status = %q, want %q", job.Status, StatusRunning)
	}
	if job.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", job.Attempts)
	}
	if !job.LockedAt.Valid {
		t.Fatal("locked_at should be set after claiming a job")
	}
}

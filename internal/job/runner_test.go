package job

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestRunnerProcessNextCompletesSuccessfulJob(t *testing.T) {
	repo := newTestRepository(t)
	service := NewService(repo)

	queued, err := service.Enqueue(TypeVerifyFile, map[string]int64{"file_id": 42})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	var handledJob Job
	runner := NewRunner(
		repo,
		map[string]Handler{
			TypeVerifyFile: func(_ context.Context, job Job) error {
				handledJob = job
				return nil
			},
		},
		WithLogger(testLogger()),
	)

	processed, err := runner.ProcessNext(context.Background())
	if err != nil {
		t.Fatalf("process job: %v", err)
	}
	if !processed {
		t.Fatal("process next should claim the queued job")
	}
	if handledJob.ID != queued.ID || handledJob.Attempts != 1 {
		t.Fatalf("handled job = %#v, want first attempt of %q", handledJob, queued.ID)
	}

	completed, err := repo.FindByID(queued.ID)
	if err != nil {
		t.Fatalf("find completed job: %v", err)
	}
	if completed.Status != StatusSucceeded || completed.Attempts != 1 {
		t.Fatalf("completed job = %#v, want succeeded after one attempt", completed)
	}
}

func TestRunnerProcessNextRetriesHandlerFailure(t *testing.T) {
	repo := newTestRepository(t)
	service := NewService(repo)

	queued, err := service.Enqueue(TypeVerifyFile, map[string]int64{"file_id": 42})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	const retryDelay = 2 * time.Minute
	before := time.Now().UTC()
	runner := NewRunner(
		repo,
		map[string]Handler{
			TypeVerifyFile: func(context.Context, Job) error {
				return errors.New("temporary storage failure")
			},
		},
		WithRetryDelay(func(int) time.Duration { return retryDelay }),
		WithLogger(testLogger()),
	)

	processed, err := runner.ProcessNext(context.Background())
	if err != nil {
		t.Fatalf("process failed job: %v", err)
	}
	if !processed {
		t.Fatal("process next should claim the queued job")
	}

	retried, err := repo.FindByID(queued.ID)
	if err != nil {
		t.Fatalf("find retried job: %v", err)
	}
	if retried.Status != StatusQueued || retried.Attempts != 1 {
		t.Fatalf("retried job = %#v, want queued after first failure", retried)
	}
	if !retried.LastError.Valid || retried.LastError.String != "temporary storage failure" {
		t.Fatalf("last error = %#v, want handler error", retried.LastError)
	}
	if retried.RunAt.Before(before.Add(retryDelay).Add(-time.Second)) {
		t.Fatalf("retry run at = %s, want approximately %s", retried.RunAt, before.Add(retryDelay))
	}
}

func TestRunnerProcessNextRetriesUnknownJobType(t *testing.T) {
	repo := newTestRepository(t)

	if _, err := repo.Create(&Job{
		ID:          "unknown-type-job",
		JobType:     "unknown.type",
		Payload:     []byte(`{}`),
		Status:      StatusQueued,
		MaxAttempts: DefaultMaxAttempts,
		RunAt:       time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("create unknown job: %v", err)
	}

	runner := NewRunner(
		repo,
		nil,
		WithRetryDelay(func(int) time.Duration { return time.Hour }),
		WithLogger(testLogger()),
	)

	processed, err := runner.ProcessNext(context.Background())
	if err != nil {
		t.Fatalf("process unknown job: %v", err)
	}
	if !processed {
		t.Fatal("process next should claim the unknown job")
	}

	updated, err := repo.FindByID("unknown-type-job")
	if err != nil {
		t.Fatalf("find unknown job: %v", err)
	}
	if updated.Status != StatusQueued || !updated.LastError.Valid {
		t.Fatalf("unknown job = %#v, want queued retry with an error", updated)
	}
	if !strings.Contains(updated.LastError.String, ErrJobTypeNotRegistered.Error()) {
		t.Fatalf("unknown job error = %q, want %q", updated.LastError.String, ErrJobTypeNotRegistered)
	}
}

func TestRunnerRunStopsWhenContextIsCancelled(t *testing.T) {
	runner := NewRunner(
		newTestRepository(t),
		nil,
		WithPollInterval(time.Hour),
		WithLogger(testLogger()),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop after context cancellation")
	}
}

func TestWithWorkerCountCanDisableWorkers(t *testing.T) {
	runner := NewRunner(
		newTestRepository(t),
		nil,
		WithWorkerCount(0),
		WithLogger(testLogger()),
	)

	if runner.workerCount != 0 {
		t.Fatalf("worker count = %d, want 0", runner.workerCount)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

package job

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

var ErrJobTypeNotRegistered = errors.New("job type is not registered")

type Handler func(ctx context.Context, job Job) error

type Runner struct {
	repo         *Repository
	handlers     map[string]Handler
	workerCount  int
	pollInterval time.Duration
	retryDelay   func(attempts int) time.Duration
	logger       *slog.Logger
}

type RunnerOption func(*Runner)

func WithWorkerCount(workerCount int) RunnerOption {
	return func(r *Runner) {
		if workerCount >= 0 {
			r.workerCount = workerCount
		}
	}
}

func WithPollInterval(interval time.Duration) RunnerOption {
	return func(r *Runner) {
		if interval > 0 {
			r.pollInterval = interval
		}
	}
}

func WithRetryDelay(delay func(attempts int) time.Duration) RunnerOption {
	return func(r *Runner) {
		if delay != nil {
			r.retryDelay = delay
		}
	}
}

func WithLogger(logger *slog.Logger) RunnerOption {
	return func(r *Runner) {
		if logger != nil {
			r.logger = logger
		}
	}
}

func NewRunner(
	repo *Repository,
	handlers map[string]Handler,
	options ...RunnerOption,
) *Runner {
	handlerCopy := make(map[string]Handler, len(handlers))
	for jobType, handler := range handlers {
		handlerCopy[jobType] = handler
	}

	runner := &Runner{
		repo:         repo,
		handlers:     handlerCopy,
		workerCount:  1,
		pollInterval: time.Second,
		retryDelay:   defaultRetryDelay,
		logger:       slog.Default(),
	}

	for _, option := range options {
		option(runner)
	}

	return runner
}

func (r *Runner) Run(ctx context.Context) {
	done := make(chan struct{}, r.workerCount)

	for range r.workerCount {
		go func() {
			defer func() {
				done <- struct{}{}
			}()

			r.runWorker(ctx)
		}()
	}

	<-ctx.Done()

	for range r.workerCount {
		<-done
	}
}

func (r *Runner) ProcessNext(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	job, err := r.repo.ClaimNext(time.Now().UTC())
	if errors.Is(err, ErrNoJobAvailable) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	handler, exists := r.handlers[job.JobType]
	if !exists {
		err = fmt.Errorf("%w: %s", ErrJobTypeNotRegistered, job.JobType)
	} else {
		err = handler(ctx, *job)
	}

	if err != nil {
		updated, retryErr := r.repo.RetryOrFail(
			job.ID,
			err.Error(),
			time.Now().UTC().Add(r.retryDelay(job.Attempts)),
		)
		if retryErr != nil {
			return true, fmt.Errorf("record job failure: %w", retryErr)
		}

		r.logger.Warn(
			"background job failed",
			"job_id", updated.ID,
			"job_type", updated.JobType,
			"status", updated.Status,
			"attempts", updated.Attempts,
			"error", err,
		)

		return true, nil
	}

	completed, err := r.repo.MarkSucceeded(job.ID)
	if err != nil {
		return true, err
	}
	if !completed {
		return true, ErrJobNotRunning
	}

	r.logger.Info(
		"background job succeeded",
		"job_id", job.ID,
		"job_type", job.JobType,
		"attempts", job.Attempts,
	)

	return true, nil
}

func (r *Runner) runWorker(ctx context.Context) {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()

	for {
		processed, err := r.ProcessNext(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Error("process background job", "error", err)
		}

		if processed {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func defaultRetryDelay(attempts int) time.Duration {
	if attempts <= 1 {
		return time.Second
	}
	if attempts >= 6 {
		return 32 * time.Second
	}

	return time.Second << (attempts - 1)
}

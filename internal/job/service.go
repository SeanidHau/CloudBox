package job

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidJobType   = errors.New("job type is required")
	ErrInvalidJobUserID = errors.New("job user ID must be positive")
	ErrInvalidJobID     = errors.New("job ID is required")
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{
		repo: repo,
	}
}

func (s *Service) Enqueue(jobType string, payload any) (*Job, error) {
	return s.Schedule(jobType, payload, time.Now().UTC())
}

func (s *Service) Schedule(
	jobType string,
	payload any,
	runAt time.Time,
) (*Job, error) {
	return s.schedule(nil, jobType, payload, runAt)
}

func (s *Service) EnqueueForUser(
	userID int64,
	jobType string,
	payload any,
) (*Job, error) {
	return s.ScheduleForUser(userID, jobType, payload, time.Now().UTC())
}

func (s *Service) ScheduleForUser(
	userID int64,
	jobType string,
	payload any,
	runAt time.Time,
) (*Job, error) {
	if userID <= 0 {
		return nil, ErrInvalidJobUserID
	}

	return s.schedule(&userID, jobType, payload, runAt)
}

func (s *Service) schedule(
	userID *int64,
	jobType string,
	payload any,
	runAt time.Time,
) (*Job, error) {
	jobType = strings.TrimSpace(jobType)
	if jobType == "" {
		return nil, ErrInvalidJobType
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	if runAt.IsZero() {
		runAt = time.Now().UTC()
	}

	return s.repo.Create(&Job{
		ID:          uuid.NewString(),
		UserID:      userID,
		JobType:     jobType,
		Payload:     payloadJSON,
		Status:      StatusQueued,
		MaxAttempts: DefaultMaxAttempts,
		RunAt:       runAt.UTC(),
	})
}

func (s *Service) GetForUser(userID int64, jobID string) (*Job, error) {
	if userID <= 0 {
		return nil, ErrInvalidJobUserID
	}

	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, ErrInvalidJobID
	}

	return s.repo.FindByIDForUser(jobID, userID)
}

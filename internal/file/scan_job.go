package file

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	jobmodule "github.com/SeanidHau/CloudBox/internal/job"
)

var (
	ErrScanFileIDRequired     = errors.New("file scan job requires a file ID")
	ErrScanFileUserIDRequired = errors.New("file scan job requires a user ID")
)

type ScanFilePayload struct {
	FileID int64 `json:"file_id"`
}

func NewScanFileJobHandler(service *Service) jobmodule.Handler {
	return func(ctx context.Context, backgroundJob jobmodule.Job) error {
		var payload ScanFilePayload

		if err := json.Unmarshal(backgroundJob.Payload, &payload); err != nil {
			return fmt.Errorf("decode file scan job payload: %w", err)
		}
		if payload.FileID <= 0 {
			return ErrScanFileIDRequired
		}
		if backgroundJob.UserID == nil || *backgroundJob.UserID <= 0 {
			return ErrScanFileUserIDRequired
		}

		// Job ownership is persisted with the task and is checked before opening storage.
		err := service.ScanActiveFile(ctx, *backgroundJob.UserID, payload.FileID)
		if errors.Is(err, ErrFileNotFound) {
			return nil
		}

		return err
	}
}

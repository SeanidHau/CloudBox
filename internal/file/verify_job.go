package file

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	jobmodule "github.com/SeanidHau/CloudBox/internal/job"
)

var ErrVerifyFileIDRequired = errors.New("verify file job requires a file ID")

type VerifyFilePayload struct {
	FileID int64 `json:"file_id"`
}

func NewVerifyFileJobHandler(service *Service) jobmodule.Handler {
	return func(ctx context.Context, backgroundJob jobmodule.Job) error {
		var payload VerifyFilePayload

		if err := json.Unmarshal(backgroundJob.Payload, &payload); err != nil {
			return fmt.Errorf("decode verify file job payload: %w", err)
		}
		if payload.FileID <= 0 {
			return ErrVerifyFileIDRequired
		}

		err := service.VerifyActiveFile(ctx, payload.FileID)

		if errors.Is(err, ErrFileNotFound) {
			return nil
		}

		return err
	}
}

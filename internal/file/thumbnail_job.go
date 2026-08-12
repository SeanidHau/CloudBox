package file

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	jobmodule "github.com/SeanidHau/CloudBox/internal/job"
)

var ErrThumbnailFileIDRequired = errors.New("thumbnail job requires a file ID")

type ThumbnailPayload struct {
	FileID int64 `json:"file_id"`
}

func NewThumbnailJobHandler(service *Service) jobmodule.Handler {
	return func(ctx context.Context, backgroundJob jobmodule.Job) error {
		var payload ThumbnailPayload

		if err := json.Unmarshal(backgroundJob.Payload, &payload); err != nil {
			return fmt.Errorf("decode thumbnail job payload: %w", err)
		}
		if payload.FileID <= 0 {
			return ErrThumbnailFileIDRequired
		}

		err := service.GenerateThumbnailForActiveFile(ctx, payload.FileID)

		if errors.Is(err, ErrFileNotFound) || errors.Is(err, ErrThumbUnsupportedContentType) || errors.Is(err, ErrVideoThumbnailUnavailable) {
			return nil
		}

		return err
	}
}

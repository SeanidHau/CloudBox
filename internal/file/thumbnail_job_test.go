package file

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"strings"
	"testing"

	jobmodule "github.com/SeanidHau/CloudBox/internal/job"
)

func TestThumbnailJobHandlerGeneratesPreview(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)
	content := encodeTestImage(t, "image/png", image.NewRGBA(image.Rect(0, 0, 640, 320)))

	uploaded, err := service.Upload(1, "source.png", "image/png", strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("upload source image: %v", err)
	}
	payload, err := json.Marshal(ThumbnailPayload{FileID: uploaded.ID})
	if err != nil {
		t.Fatalf("marshal thumbnail payload: %v", err)
	}

	if err := NewThumbnailJobHandler(service)(context.Background(), jobmodule.Job{Payload: payload}); err != nil {
		t.Fatalf("handle thumbnail job: %v", err)
	}
	if _, err := service.repo.FindFilePreviewForActiveFile(1, uploaded.ID); err != nil {
		t.Fatalf("find generated preview: %v", err)
	}
}

func TestThumbnailJobHandlerRejectsInvalidPayloadAndIgnoresDeletedFile(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)
	handler := NewThumbnailJobHandler(service)

	if err := handler(context.Background(), jobmodule.Job{Payload: []byte(`{"file_id":0}`)}); !errors.Is(err, ErrThumbnailFileIDRequired) {
		t.Fatalf("missing file ID error = %v, want %v", err, ErrThumbnailFileIDRequired)
	}

	content := encodeTestImage(t, "image/png", image.NewRGBA(image.Rect(0, 0, 10, 10)))
	uploaded, err := service.Upload(1, "deleted.png", "image/png", strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("upload source image: %v", err)
	}
	if err := service.SoftDelete(1, uploaded.ID); err != nil {
		t.Fatalf("soft delete source image: %v", err)
	}
	payload, err := json.Marshal(ThumbnailPayload{FileID: uploaded.ID})
	if err != nil {
		t.Fatalf("marshal deleted payload: %v", err)
	}

	if err := handler(context.Background(), jobmodule.Job{Payload: payload}); err != nil {
		t.Fatalf("deleted thumbnail job error = %v, want success", err)
	}
}

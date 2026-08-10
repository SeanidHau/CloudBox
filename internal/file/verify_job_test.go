package file

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	jobmodule "github.com/SeanidHau/CloudBox/internal/job"
)

func TestVerifyFileJobHandlerVerifiesActiveFile(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	uploaded, err := service.Upload(1, "verified.txt", "text/plain", strings.NewReader("verified content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}

	payload, err := json.Marshal(VerifyFilePayload{FileID: uploaded.ID})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	handler := NewVerifyFileJobHandler(service)
	if err := handler(context.Background(), jobmodule.Job{Payload: payload}); err != nil {
		t.Fatalf("handle verify job: %v", err)
	}
}

func TestVerifyFileJobHandlerRejectsInvalidPayload(t *testing.T) {
	handler := NewVerifyFileJobHandler(newTestServiceWithStorage(t, &fakeStorage{}))

	if err := handler(context.Background(), jobmodule.Job{Payload: []byte("{")}); err == nil {
		t.Fatal("invalid JSON payload should fail")
	}
	if err := handler(context.Background(), jobmodule.Job{Payload: []byte(`{"file_id":0}`)}); !errors.Is(err, ErrVerifyFileIDRequired) {
		t.Fatalf("missing file ID error = %v, want %v", err, ErrVerifyFileIDRequired)
	}
}

func TestVerifyFileJobHandlerIgnoresDeletedFile(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	uploaded, err := service.Upload(1, "deleted.txt", "text/plain", strings.NewReader("deleted content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if err := service.SoftDelete(1, uploaded.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}

	payload, err := json.Marshal(VerifyFilePayload{FileID: uploaded.ID})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := NewVerifyFileJobHandler(service)(context.Background(), jobmodule.Job{Payload: payload}); err != nil {
		t.Fatalf("deleted file verification error = %v, want success", err)
	}
}

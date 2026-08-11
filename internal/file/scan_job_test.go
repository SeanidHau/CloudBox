package file

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	jobmodule "github.com/SeanidHau/CloudBox/internal/job"
	"github.com/SeanidHau/CloudBox/internal/scanner"
)

func TestScanFileJobHandlerScansFileAndIgnoresDeletedFile(t *testing.T) {
	storage := &fakeStorage{}
	virusScanner := &fakeVirusScanner{result: scanner.Result{}}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		storage,
		testStorageQuotaBytes,
		WithVirusScanner(virusScanner),
	)
	handler := NewScanFileJobHandler(service)
	userID := int64(1)

	uploaded, err := service.Upload(1, "source.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("upload source file: %v", err)
	}
	payload, err := json.Marshal(ScanFilePayload{FileID: uploaded.ID})
	if err != nil {
		t.Fatalf("marshal scan payload: %v", err)
	}
	if err := handler(context.Background(), jobmodule.Job{UserID: &userID, Payload: payload}); err != nil {
		t.Fatalf("handle file scan job: %v", err)
	}

	deleted, err := service.Upload(1, "deleted.txt", "text/plain", strings.NewReader("deleted"))
	if err != nil {
		t.Fatalf("upload deleted file: %v", err)
	}
	if err := service.SoftDelete(1, deleted.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}
	deletedPayload, err := json.Marshal(ScanFilePayload{FileID: deleted.ID})
	if err != nil {
		t.Fatalf("marshal deleted payload: %v", err)
	}

	// Queued work for a deleted file is stale and should not be retried.
	if err := handler(context.Background(), jobmodule.Job{UserID: &userID, Payload: deletedPayload}); err != nil {
		t.Fatalf("handle deleted file scan: %v", err)
	}
}

func TestScanFileJobHandlerRejectsInvalidPayload(t *testing.T) {
	handler := NewScanFileJobHandler(newTestServiceWithStorage(t, &fakeStorage{}))

	if err := handler(context.Background(), jobmodule.Job{Payload: []byte("{")}); err == nil {
		t.Fatal("invalid JSON payload should fail")
	}
	if err := handler(context.Background(), jobmodule.Job{Payload: []byte(`{"file_id":0}`)}); !errors.Is(err, ErrScanFileIDRequired) {
		t.Fatalf("missing file ID error = %v, want %v", err, ErrScanFileIDRequired)
	}
	if err := handler(context.Background(), jobmodule.Job{Payload: []byte(`{"file_id":1}`)}); !errors.Is(err, ErrScanFileUserIDRequired) {
		t.Fatalf("missing user ID error = %v, want %v", err, ErrScanFileUserIDRequired)
	}
}

func TestScanJobRunnerProcessesAutomaticallyEnqueuedScan(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		storage,
		testStorageQuotaBytes,
		WithVirusScanner(&fakeVirusScanner{}),
	)
	jobRepo := jobmodule.NewRepository(service.repo.db)
	jobService := jobmodule.NewService(jobRepo)

	// Production injects the queue during construction; tests wire it after creating its DB-backed queue.
	service.jobEnqueuer = jobService

	uploaded, err := service.Upload(1, "source.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("upload source file: %v", err)
	}

	runner := jobmodule.NewRunner(
		jobRepo,
		map[string]jobmodule.Handler{
			jobmodule.TypeScanFile: NewScanFileJobHandler(service),
		},
	)
	processed, err := runner.ProcessNext(context.Background())
	if err != nil {
		t.Fatalf("process scan job: %v", err)
	}
	if !processed {
		t.Fatal("expected the automatically enqueued scan job to be processed")
	}

	object, err := service.repo.FindObjectForActiveFile(uploaded.ID)
	if err != nil {
		t.Fatalf("find uploaded object: %v", err)
	}
	scan, err := service.repo.FindFileScanByObjectID(object.ID)
	if err != nil {
		t.Fatalf("find completed file scan: %v", err)
	}
	if scan.Status != ScanStatusClean || !scan.ScannedAt.Valid {
		t.Fatalf("completed scan = %#v, want clean result with scanned time", scan)
	}
}

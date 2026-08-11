package file

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	jobmodule "github.com/SeanidHau/CloudBox/internal/job"
	"github.com/SeanidHau/CloudBox/internal/scanner"
)

type fakeVirusScanner struct {
	result  scanner.Result
	err     error
	calls   int
	content string
}

type blockingVirusScanner struct {
	calls int
}

func (s *blockingVirusScanner) Scan(ctx context.Context, reader io.Reader) (scanner.Result, error) {
	s.calls++

	// Consume the input first so this fake behaves like a scanner that started work.
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return scanner.Result{}, err
	}

	<-ctx.Done()
	return scanner.Result{}, ctx.Err()
}

func (s *fakeVirusScanner) Scan(_ context.Context, reader io.Reader) (scanner.Result, error) {
	s.calls++

	content, err := io.ReadAll(reader)
	if err != nil {
		return scanner.Result{}, err
	}
	s.content = string(content)

	if s.err != nil {
		return scanner.Result{}, s.err
	}

	return s.result, nil
}

func TestServiceScanActiveFileRecordsTerminalResults(t *testing.T) {
	for _, test := range []struct {
		name      string
		result    scanner.Result
		status    string
		signature string
	}{
		{
			name:   "clean",
			result: scanner.Result{},
			status: ScanStatusClean,
		},
		{
			name: "infected",
			result: scanner.Result{
				Infected:  true,
				Signature: "Eicar-Test-Signature",
			},
			status:    ScanStatusInfected,
			signature: "Eicar-Test-Signature",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			storage := &fakeStorage{}
			virusScanner := &fakeVirusScanner{result: test.result}
			service := newTestServiceWithStorageQuotaAndOptions(
				t,
				storage,
				testStorageQuotaBytes,
				WithVirusScanner(virusScanner),
			)

			uploaded, err := service.Upload(
				1,
				"source.txt",
				"text/plain",
				strings.NewReader("scan this content"),
			)
			if err != nil {
				t.Fatalf("upload file: %v", err)
			}

			if err := service.ScanActiveFile(context.Background(), 1, uploaded.ID); err != nil {
				t.Fatalf("scan active file: %v", err)
			}
			if virusScanner.calls != 1 || virusScanner.content != "scan this content" {
				t.Fatalf("scanner calls/content = %d/%q, want 1/%q", virusScanner.calls, virusScanner.content, "scan this content")
			}

			object, err := service.repo.FindObjectForActiveFile(uploaded.ID)
			if err != nil {
				t.Fatalf("find uploaded object: %v", err)
			}
			scan, err := service.repo.FindFileScanByObjectID(object.ID)
			if err != nil {
				t.Fatalf("find completed scan: %v", err)
			}
			if scan.Status != test.status || !scan.ScannedAt.Valid {
				t.Fatalf("scan = %#v, want %q with completion time", scan, test.status)
			}
			if scan.Signature.String != test.signature || scan.Signature.Valid != (test.signature != "") {
				t.Fatalf("scan signature = %#v, want %q", scan.Signature, test.signature)
			}

			// A terminal result cannot be claimed again, so no second ClamAV call occurs.
			if err := service.ScanActiveFile(context.Background(), 1, uploaded.ID); err != nil {
				t.Fatalf("scan terminal file again: %v", err)
			}
			if virusScanner.calls != 1 {
				t.Fatalf("scanner calls after terminal scan = %d, want 1", virusScanner.calls)
			}
		})
	}
}

func TestServiceScanActiveFileMarksFailuresRetryable(t *testing.T) {
	storage := &fakeStorage{}
	scannerError := errors.New("clamav is unavailable")
	virusScanner := &fakeVirusScanner{err: scannerError}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		storage,
		testStorageQuotaBytes,
		WithVirusScanner(virusScanner),
	)

	uploaded, err := service.Upload(1, "retry.txt", "text/plain", strings.NewReader("retry content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}

	if err := service.ScanActiveFile(context.Background(), 1, uploaded.ID); !errors.Is(err, scannerError) {
		t.Fatalf("scan error = %v, want %v", err, scannerError)
	}

	object, err := service.repo.FindObjectForActiveFile(uploaded.ID)
	if err != nil {
		t.Fatalf("find uploaded object: %v", err)
	}
	failedScan, err := service.repo.FindFileScanByObjectID(object.ID)
	if err != nil {
		t.Fatalf("find failed scan: %v", err)
	}
	if failedScan.Status != ScanStatusFailed || failedScan.ScannedAt.Valid {
		t.Fatalf("failed scan = %#v, want retryable failed state", failedScan)
	}

	// The next task may claim a failed scan and complete it after ClamAV recovers.
	virusScanner.err = nil
	if err := service.ScanActiveFile(context.Background(), 1, uploaded.ID); err != nil {
		t.Fatalf("retry file scan: %v", err)
	}
	if virusScanner.calls != 2 {
		t.Fatalf("scanner calls = %d, want 2", virusScanner.calls)
	}

	retriedScan, err := service.repo.FindFileScanByObjectID(object.ID)
	if err != nil {
		t.Fatalf("find retried scan: %v", err)
	}
	if retriedScan.Status != ScanStatusClean || !retriedScan.ScannedAt.Valid {
		t.Fatalf("retried scan = %#v, want completed clean state", retriedScan)
	}
}

func TestServiceScanActiveFileRequiresConfiguredScanner(t *testing.T) {
	service := newTestServiceWithStorage(t, &fakeStorage{})

	uploaded, err := service.Upload(1, "source.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}

	if err := service.ScanActiveFile(context.Background(), 1, uploaded.ID); !errors.Is(err, ErrVirusScannerUnavailable) {
		t.Fatalf("scan without scanner error = %v, want %v", err, ErrVirusScannerUnavailable)
	}
}

func TestServiceScanActiveFileUsesConfiguredTimeout(t *testing.T) {
	storage := &fakeStorage{}
	virusScanner := &blockingVirusScanner{}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		storage,
		testStorageQuotaBytes,
		WithVirusScanner(virusScanner),
		WithVirusScanTimeout(10*time.Millisecond),
	)

	uploaded, err := service.Upload(1, "timeout.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}

	// A scanner that does not finish must receive the timeout context from Service.
	if err := service.ScanActiveFile(context.Background(), 1, uploaded.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out scan error = %v, want %v", err, context.DeadlineExceeded)
	}
	if virusScanner.calls != 1 {
		t.Fatalf("scanner calls = %d, want 1", virusScanner.calls)
	}

	object, err := service.repo.FindObjectForActiveFile(uploaded.ID)
	if err != nil {
		t.Fatalf("find uploaded object: %v", err)
	}
	scan, err := service.repo.FindFileScanByObjectID(object.ID)
	if err != nil {
		t.Fatalf("find timed-out scan: %v", err)
	}
	if scan.Status != ScanStatusFailed {
		t.Fatalf("timed-out scan status = %q, want %q", scan.Status, ScanStatusFailed)
	}
}

func TestTextUploadAndInstantUploadEnqueueScanJobs(t *testing.T) {
	storage := &fakeStorage{}
	queue := &fakeJobEnqueuer{
		job: &jobmodule.Job{ID: "scan-job", Status: jobmodule.StatusQueued},
	}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		storage,
		testStorageQuotaBytes,
		WithVirusScanner(&fakeVirusScanner{}),
		WithJobEnqueuer(queue),
	)

	uploaded, err := service.Upload(1, "source.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("upload source file: %v", err)
	}
	assertScanJobEnqueued(t, queue, 1, uploaded.ID)

	object, err := service.repo.FindObjectForActiveFile(uploaded.ID)
	if err != nil {
		t.Fatalf("find source object: %v", err)
	}
	scan, err := service.repo.FindFileScanByObjectID(object.ID)
	if err != nil {
		t.Fatalf("find pending scan: %v", err)
	}
	if scan.Status != ScanStatusPending {
		t.Fatalf("scan status = %q, want %q", scan.Status, ScanStatusPending)
	}

	// A deduplicated instant upload may queue another task, but only one worker can claim it.
	queue.calls = 0
	instant, err := service.InstantUpload(2, "copy.txt", object.FileHash)
	if err != nil {
		t.Fatalf("instant upload shared object: %v", err)
	}
	assertScanJobEnqueued(t, queue, 2, instant.ID)
}

func TestScanQueueRequiresScannerAndDoesNotFailUpload(t *testing.T) {
	t.Run("scanner disabled", func(t *testing.T) {
		queue := &fakeJobEnqueuer{job: &jobmodule.Job{ID: "unused-job"}}
		service := newTestServiceWithStorageQuotaAndOptions(
			t,
			&fakeStorage{},
			testStorageQuotaBytes,
			WithJobEnqueuer(queue),
		)

		uploaded, err := service.Upload(1, "source.txt", "text/plain", strings.NewReader("content"))
		if err != nil {
			t.Fatalf("upload with disabled scanner: %v", err)
		}
		if queue.calls != 0 {
			t.Fatalf("queue calls with disabled scanner = %d, want 0", queue.calls)
		}

		object, err := service.repo.FindObjectForActiveFile(uploaded.ID)
		if err != nil {
			t.Fatalf("find uploaded object: %v", err)
		}
		if _, err := service.repo.FindFileScanByObjectID(object.ID); !errors.Is(err, ErrFileScanNotFound) {
			t.Fatalf("scan with disabled scanner error = %v, want %v", err, ErrFileScanNotFound)
		}
	})

	t.Run("queue failure", func(t *testing.T) {
		queue := &fakeJobEnqueuer{err: errors.New("queue unavailable")}
		service := newTestServiceWithStorageQuotaAndOptions(
			t,
			&fakeStorage{},
			testStorageQuotaBytes,
			WithVirusScanner(&fakeVirusScanner{}),
			WithJobEnqueuer(queue),
		)

		uploaded, err := service.Upload(1, "source.txt", "text/plain", strings.NewReader("content"))
		if err != nil {
			t.Fatalf("upload with unavailable queue: %v", err)
		}
		if uploaded.ID == 0 || queue.calls != 1 {
			t.Fatalf("uploaded file/queue calls = %#v/%d, want saved file and one enqueue attempt", uploaded, queue.calls)
		}
	})
}

func assertScanJobEnqueued(t *testing.T, queue *fakeJobEnqueuer, userID int64, fileID int64) {
	t.Helper()

	if queue.calls != 1 || queue.userID != userID || queue.jobType != jobmodule.TypeScanFile {
		t.Fatalf("scan queue call = %#v, want one scan job for user %d", queue, userID)
	}
	payload, ok := queue.payload.(ScanFilePayload)
	if !ok || payload.FileID != fileID {
		t.Fatalf("scan payload = %#v, want file ID %d", queue.payload, fileID)
	}
}

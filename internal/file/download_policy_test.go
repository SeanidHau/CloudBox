package file

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
)

func TestServiceCheckFileObjectDownloadRequiresCleanScan(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  string
		wantErr error
	}{
		{name: "missing scan", wantErr: ErrFileScanIncomplete},
		{name: "pending", status: ScanStatusPending, wantErr: ErrFileScanIncomplete},
		{name: "scanning", status: ScanStatusScanning, wantErr: ErrFileScanIncomplete},
		{name: "failed", status: ScanStatusFailed, wantErr: ErrFileScanIncomplete},
		{name: "infected", status: ScanStatusInfected, wantErr: ErrFileInfected},
		{name: "clean", status: ScanStatusClean},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := newTestServiceWithStorageQuotaAndOptions(
				t,
				&fakeStorage{},
				testStorageQuotaBytes,
				WithVirusScanner(&fakeVirusScanner{}),
			)
			object, err := service.repo.CreateFileObject(
				"download-policy-"+strings.ReplaceAll(test.name, " ", "-"),
				"uploads/source.txt",
				7,
				"text/plain",
			)
			if err != nil {
				t.Fatalf("create file object: %v", err)
			}
			setFileScanStatus(t, service.repo, object.ID, test.status)

			err = service.CheckFileObjectDownload(object.ID)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("download check error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestServiceDownloadPolicyProtectsFilesAndThumbnails(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		storage,
		testStorageQuotaBytes,
		WithVirusScanner(&fakeVirusScanner{}),
	)

	uploaded, err := service.Upload(1, "source.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("upload source file: %v", err)
	}
	object, err := service.repo.FindObjectForActiveFile(uploaded.ID)
	if err != nil {
		t.Fatalf("find uploaded object: %v", err)
	}
	if _, _, err := service.repo.CreatePendingFileScan(object.ID); err != nil {
		t.Fatalf("create pending scan: %v", err)
	}

	if _, _, err := service.OpenForDownload(1, uploaded.ID); !errors.Is(err, ErrFileScanIncomplete) {
		t.Fatalf("open pending file error = %v, want %v", err, ErrFileScanIncomplete)
	}

	if _, claimed, err := service.repo.ClaimFileScan(object.ID); err != nil || !claimed {
		t.Fatalf("claim file scan = claimed:%t err:%v, want true/nil", claimed, err)
	}
	if _, err := service.repo.CompleteFileScan(object.ID, false, ""); err != nil {
		t.Fatalf("complete clean scan: %v", err)
	}

	_, reader, err := service.OpenForDownload(1, uploaded.ID)
	if err != nil {
		t.Fatalf("open clean file: %v", err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read clean file: %v", err)
	}
	if string(content) != "content" {
		t.Fatalf("clean file content = %q, want content", content)
	}

	previewObject, err := service.repo.CreateFileObject(
		"thumbnail-policy-source",
		"uploads/thumbnail-source.png",
		10,
		"image/png",
	)
	if err != nil {
		t.Fatalf("create thumbnail source object: %v", err)
	}
	previewFile, err := service.repo.CreateWithObject(1, "source.png", previewObject)
	if err != nil {
		t.Fatalf("create preview user file: %v", err)
	}
	if _, err := service.repo.CreateFilePreview(&FilePreview{
		FileObjectID: previewObject.ID,
		StoragePath:  "uploads/source-preview.png",
		Size:         5,
		ContentType:  "image/png",
		Width:        1,
		Height:       1,
	}); err != nil {
		t.Fatalf("create preview: %v", err)
	}

	// No scan record is rejected too, so a thumbnail cannot bypass the policy.
	if _, _, err := service.OpenThumbnailForDownload(1, previewFile.ID); !errors.Is(err, ErrFileScanIncomplete) {
		t.Fatalf("open unscanned thumbnail error = %v, want %v", err, ErrFileScanIncomplete)
	}
}

func TestFileHandlerDownloadReturnsScanPolicyStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		&fakeStorage{},
		testStorageQuotaBytes,
		WithVirusScanner(&fakeVirusScanner{}),
	)
	uploaded, err := service.Upload(1, "source.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("upload source file: %v", err)
	}
	object, err := service.repo.FindObjectForActiveFile(uploaded.ID)
	if err != nil {
		t.Fatalf("find uploaded object: %v", err)
	}
	if _, _, err := service.repo.CreatePendingFileScan(object.ID); err != nil {
		t.Fatalf("create pending scan: %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.UserIDKey, int64(1))
		c.Next()
	})
	router.GET("/files/:id/download", NewHandler(service).Download)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/files/"+strconv.FormatInt(uploaded.ID, 10)+"/download",
		nil,
	))
	if response.Code != http.StatusLocked {
		t.Fatalf("pending download status = %d, want %d: %s", response.Code, http.StatusLocked, response.Body.String())
	}

	if _, claimed, err := service.repo.ClaimFileScan(object.ID); err != nil || !claimed {
		t.Fatalf("claim pending scan = claimed:%t err:%v, want true/nil", claimed, err)
	}
	if _, err := service.repo.CompleteFileScan(object.ID, true, "Eicar-Test-Signature"); err != nil {
		t.Fatalf("complete infected scan: %v", err)
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/files/"+strconv.FormatInt(uploaded.ID, 10)+"/download",
		nil,
	))
	if response.Code != http.StatusForbidden {
		t.Fatalf("infected download status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
	}
}

func setFileScanStatus(t *testing.T, repo *Repository, fileObjectID int64, status string) {
	t.Helper()

	if status == "" {
		return
	}
	if _, _, err := repo.CreatePendingFileScan(fileObjectID); err != nil {
		t.Fatalf("create pending scan: %v", err)
	}
	if status == ScanStatusPending {
		return
	}
	if _, claimed, err := repo.ClaimFileScan(fileObjectID); err != nil || !claimed {
		t.Fatalf("claim scan = claimed:%t err:%v, want true/nil", claimed, err)
	}

	switch status {
	case ScanStatusScanning:
		return
	case ScanStatusClean:
		if _, err := repo.CompleteFileScan(fileObjectID, false, ""); err != nil {
			t.Fatalf("complete clean scan: %v", err)
		}
	case ScanStatusInfected:
		if _, err := repo.CompleteFileScan(fileObjectID, true, "Eicar-Test-Signature"); err != nil {
			t.Fatalf("complete infected scan: %v", err)
		}
	case ScanStatusFailed:
		if _, err := repo.FailFileScan(fileObjectID); err != nil {
			t.Fatalf("fail scan: %v", err)
		}
	default:
		t.Fatalf("unsupported test scan status: %q", status)
	}
}

package file

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	jobmodule "github.com/SeanidHau/CloudBox/internal/job"
	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/SeanidHau/CloudBox/internal/scanner"
	"github.com/gin-gonic/gin"
)

func TestGenerateThumbnailForActiveFileDecodesAndScalesImages(t *testing.T) {
	for _, test := range []struct {
		name        string
		contentType string
		fileName    string
	}{
		{name: "PNG", contentType: "image/png", fileName: "source.png"},
		{name: "JPEG", contentType: "image/jpeg", fileName: "source.jpg"},
		{name: "GIF", contentType: "image/gif", fileName: "source.gif"},
	} {
		t.Run(test.name, func(t *testing.T) {
			storage := &fakeStorage{}
			service := newTestServiceWithStorage(t, storage)

			source := image.NewRGBA(image.Rect(0, 0, 640, 320))
			source.Set(0, 0, color.RGBA{R: 255, A: 255})
			content := encodeTestImage(t, test.contentType, source)

			uploaded, err := service.Upload(
				1,
				test.fileName,
				test.contentType,
				strings.NewReader(string(content)),
			)
			if err != nil {
				t.Fatalf("upload source image: %v", err)
			}

			if err := service.GenerateThumbnailForActiveFile(context.Background(), uploaded.ID); err != nil {
				t.Fatalf("generate thumbnail: %v", err)
			}

			preview, err := service.repo.FindFilePreviewForActiveFile(1, uploaded.ID)
			if err != nil {
				t.Fatalf("find generated preview: %v", err)
			}
			if preview.ContentType != "image/png" || preview.Width != 320 || preview.Height != 160 {
				t.Fatalf("preview = %#v, want 320x160 PNG", preview)
			}

			generated, err := png.Decode(strings.NewReader(storage.savedContent))
			if err != nil {
				t.Fatalf("decode generated PNG: %v", err)
			}
			if generated.Bounds().Dx() != 320 || generated.Bounds().Dy() != 160 {
				t.Fatalf("generated dimensions = %dx%d, want 320x160", generated.Bounds().Dx(), generated.Bounds().Dy())
			}
		})
	}
}

func TestGenerateThumbnailForActiveFileReusesExistingPreview(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)
	content := encodeTestImage(t, "image/png", image.NewRGBA(image.Rect(0, 0, 640, 320)))

	uploaded, err := service.Upload(1, "source.png", "image/png", strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("upload source image: %v", err)
	}
	if err := service.GenerateThumbnailForActiveFile(context.Background(), uploaded.ID); err != nil {
		t.Fatalf("generate first thumbnail: %v", err)
	}

	// A second call must return before opening storage because the preview already exists.
	storage.openErr = errors.New("storage should not be opened again")
	if err := service.GenerateThumbnailForActiveFile(context.Background(), uploaded.ID); err != nil {
		t.Fatalf("reuse existing thumbnail: %v", err)
	}
}

func TestGenerateThumbnailForActiveFileRejectsUnsupportedContentType(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	uploaded, err := service.Upload(1, "notes.txt", "text/plain", strings.NewReader("not an image"))
	if err != nil {
		t.Fatalf("upload text file: %v", err)
	}

	if err := service.GenerateThumbnailForActiveFile(context.Background(), uploaded.ID); !errors.Is(err, ErrThumbUnsupportedContentType) {
		t.Fatalf("generate text thumbnail error = %v, want %v", err, ErrThumbUnsupportedContentType)
	}
}

func TestImageUploadAndInstantUploadEnqueueThumbnailJobs(t *testing.T) {
	storage := &fakeStorage{}
	queue := &fakeJobEnqueuer{
		job: &jobmodule.Job{ID: "thumbnail-job", Status: jobmodule.StatusQueued},
	}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		storage,
		testStorageQuotaBytes,
		WithJobEnqueuer(queue),
	)
	content := encodeTestImage(t, "image/png", image.NewRGBA(image.Rect(0, 0, 640, 320)))

	uploaded, err := service.Upload(1, "source.png", "image/png", strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("upload image: %v", err)
	}
	assertThumbnailJobEnqueued(t, queue, 1, uploaded.ID)

	object, err := service.repo.FindObjectForActiveFile(uploaded.ID)
	if err != nil {
		t.Fatalf("find uploaded object: %v", err)
	}
	queue.calls = 0

	instant, err := service.InstantUpload(2, "copy.png", object.FileHash)
	if err != nil {
		t.Fatalf("instant upload image: %v", err)
	}
	assertThumbnailJobEnqueued(t, queue, 2, instant.ID)
}

func TestThumbnailQueueFailureDoesNotFailImageUpload(t *testing.T) {
	storage := &fakeStorage{}
	queue := &fakeJobEnqueuer{err: errors.New("queue unavailable")}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		storage,
		testStorageQuotaBytes,
		WithJobEnqueuer(queue),
	)
	content := encodeTestImage(t, "image/png", image.NewRGBA(image.Rect(0, 0, 10, 10)))

	uploaded, err := service.Upload(1, "source.png", "image/png", strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("upload should succeed when thumbnail queue fails: %v", err)
	}
	if uploaded.ID == 0 || queue.calls != 1 {
		t.Fatalf("uploaded file/queue calls = %#v/%d, want saved file and one enqueue attempt", uploaded, queue.calls)
	}
}

func TestImageUploadDefersThumbnailUntilVirusScanSucceeds(t *testing.T) {
	for _, test := range []struct {
		name             string
		scanResult       scanner.Result
		wantQueueCalls   int
		wantFinalJobType string
	}{
		{
			name:             "clean file",
			scanResult:       scanner.Result{},
			wantQueueCalls:   2,
			wantFinalJobType: jobmodule.TypeGenerateThumbnail,
		},
		{
			name: "infected file",
			scanResult: scanner.Result{
				Infected:  true,
				Signature: "Eicar-Test-Signature",
			},
			wantQueueCalls:   1,
			wantFinalJobType: jobmodule.TypeScanFile,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			storage := &fakeStorage{}
			queue := &fakeJobEnqueuer{job: &jobmodule.Job{ID: "background-job"}}
			service := newTestServiceWithStorageQuotaAndOptions(
				t,
				storage,
				testStorageQuotaBytes,
				WithVirusScanner(&fakeVirusScanner{result: test.scanResult}),
				WithJobEnqueuer(queue),
			)
			content := encodeTestImage(t, "image/png", image.NewRGBA(image.Rect(0, 0, 10, 10)))

			uploaded, err := service.Upload(1, "source.png", "image/png", strings.NewReader(string(content)))
			if err != nil {
				t.Fatalf("upload image: %v", err)
			}

			// Enabling scanning queues only the scan; decoding waits for a clean result.
			if queue.calls != 1 || queue.jobType != jobmodule.TypeScanFile {
				t.Fatalf("upload queue = %#v, want one %q job", queue, jobmodule.TypeScanFile)
			}

			if err := service.ScanActiveFile(context.Background(), 1, uploaded.ID); err != nil {
				t.Fatalf("scan uploaded image: %v", err)
			}
			if queue.calls != test.wantQueueCalls || queue.jobType != test.wantFinalJobType {
				t.Fatalf("queue after scan = %#v, want %d %q jobs", queue, test.wantQueueCalls, test.wantFinalJobType)
			}
		})
	}
}

func TestDownloadThumbnailReturnsOnlyOwnerPreview(t *testing.T) {
	gin.SetMode(gin.TestMode)

	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)
	content := encodeTestImage(t, "image/png", image.NewRGBA(image.Rect(0, 0, 640, 320)))

	uploaded, err := service.Upload(1, "source.png", "image/png", strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("upload source image: %v", err)
	}
	if err := service.GenerateThumbnailForActiveFile(context.Background(), uploaded.ID); err != nil {
		t.Fatalf("generate thumbnail: %v", err)
	}

	ownerRouter := newThumbnailTestRouter(service, 1)
	ownerResponse := httptest.NewRecorder()
	ownerRouter.ServeHTTP(ownerResponse, httptest.NewRequest(
		http.MethodGet,
		"/files/"+strconv.FormatInt(uploaded.ID, 10)+"/thumbnail",
		nil,
	))
	if ownerResponse.Code != http.StatusOK {
		t.Fatalf("owner thumbnail status = %d, want %d: %s", ownerResponse.Code, http.StatusOK, ownerResponse.Body.String())
	}
	if ownerResponse.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("thumbnail content type = %q, want image/png", ownerResponse.Header().Get("Content-Type"))
	}
	if ownerResponse.Header().Get("Content-Disposition") != `inline; filename="thumbnail.png"` {
		t.Fatalf("thumbnail disposition = %q, want inline", ownerResponse.Header().Get("Content-Disposition"))
	}
	if _, err := png.Decode(bytes.NewReader(ownerResponse.Body.Bytes())); err != nil {
		t.Fatalf("decode thumbnail response: %v", err)
	}

	otherUserRouter := newThumbnailTestRouter(service, 2)
	otherUserResponse := httptest.NewRecorder()
	otherUserRouter.ServeHTTP(otherUserResponse, httptest.NewRequest(
		http.MethodGet,
		"/files/"+strconv.FormatInt(uploaded.ID, 10)+"/thumbnail",
		nil,
	))
	if otherUserResponse.Code != http.StatusNotFound {
		t.Fatalf("other user thumbnail status = %d, want %d", otherUserResponse.Code, http.StatusNotFound)
	}
}

func TestDownloadThumbnailReturnsNotFoundBeforeGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)
	content := encodeTestImage(t, "image/png", image.NewRGBA(image.Rect(0, 0, 10, 10)))
	uploaded, err := service.Upload(1, "source.png", "image/png", strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("upload source image: %v", err)
	}

	response := httptest.NewRecorder()
	newThumbnailTestRouter(service, 1).ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/files/"+strconv.FormatInt(uploaded.ID, 10)+"/thumbnail",
		nil,
	))
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing thumbnail status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestThumbnailDimensionsPreserveAspectRatio(t *testing.T) {
	for _, test := range []struct {
		name                  string
		width, height, max    int
		wantWidth, wantHeight int
	}{
		{name: "already small", width: 100, height: 50, max: 320, wantWidth: 100, wantHeight: 50},
		{name: "wide", width: 640, height: 320, max: 320, wantWidth: 320, wantHeight: 160},
		{name: "tall", width: 320, height: 640, max: 320, wantWidth: 160, wantHeight: 320},
	} {
		t.Run(test.name, func(t *testing.T) {
			width, height := thumbnailDimensions(test.width, test.height, test.max)
			if width != test.wantWidth || height != test.wantHeight {
				t.Fatalf("dimensions = %dx%d, want %dx%d", width, height, test.wantWidth, test.wantHeight)
			}
		})
	}
}

func encodeTestImage(t *testing.T, contentType string, source image.Image) []byte {
	t.Helper()

	var encoded bytes.Buffer
	var err error

	switch contentType {
	case "image/png":
		err = png.Encode(&encoded, source)
	case "image/jpeg":
		err = jpeg.Encode(&encoded, source, nil)
	case "image/gif":
		err = gif.Encode(&encoded, source, nil)
	default:
		t.Fatalf("unsupported test image type: %s", contentType)
	}
	if err != nil {
		t.Fatalf("encode %s: %v", contentType, err)
	}

	return encoded.Bytes()
}

func assertThumbnailJobEnqueued(t *testing.T, queue *fakeJobEnqueuer, userID int64, fileID int64) {
	t.Helper()

	if queue.calls != 1 || queue.userID != userID || queue.jobType != jobmodule.TypeGenerateThumbnail {
		t.Fatalf("thumbnail queue call = %#v, want one job for user %d", queue, userID)
	}
	payload, ok := queue.payload.(ThumbnailPayload)
	if !ok || payload.FileID != fileID {
		t.Fatalf("thumbnail payload = %#v, want file ID %d", queue.payload, fileID)
	}
}

func newThumbnailTestRouter(service *Service, userID int64) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.UserIDKey, userID)
		c.Next()
	})
	router.GET("/files/:id/thumbnail", NewHandler(service).DownloadThumbnail)

	return router
}

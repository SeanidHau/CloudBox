package share

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	filemodule "github.com/SeanidHau/CloudBox/internal/file"
	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
)

func newTestHandler(t *testing.T) (*Handler, *Repository, *Service) {
	t.Helper()

	repo := newTestRepository(t)
	storage := &fakeStorage{
		content: map[string][]byte{
			"uploads/document.txt": []byte("shared content"),
		},
	}
	service := NewService(repo, storage)

	return NewHandler(service), repo, service
}

func TestHandlerCreateAndDownloadShare(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, repo, _ := newTestHandler(t)
	fileID := createTestFile(t, repo, 1, "active")

	router := gin.New()
	router.POST("/api/files/:id/shares", func(c *gin.Context) {
		c.Set(middleware.UserIDKey, int64(1))
		handler.Create(c)
	})
	router.GET("/api/shares", func(c *gin.Context) {
		c.Set(middleware.UserIDKey, int64(1))
		handler.List(c)
	})
	router.DELETE("/api/shares/:token", func(c *gin.Context) {
		c.Set(middleware.UserIDKey, int64(1))
		handler.Revoke(c)
	})
	router.GET("/api/shares/:token/download", handler.Download)
	router.GET("/api/shares/:token", handler.PublicInfo)

	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/files/"+strconv.FormatInt(fileID, 10)+"/shares",
		strings.NewReader(`{"password":"share-password","max_downloads":1}`),
	)
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createRequest)

	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}

	var createResponse struct {
		Share Share `json:"share"`
	}
	if err := json.NewDecoder(createRecorder.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createResponse.Share.Token == "" {
		t.Fatal("created share token should not be empty")
	}

	missingPasswordRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/shares/"+createResponse.Share.Token+"/download",
		nil,
	)
	missingPasswordRecorder := httptest.NewRecorder()
	router.ServeHTTP(missingPasswordRecorder, missingPasswordRequest)
	if missingPasswordRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing password status = %d, want %d", missingPasswordRecorder.Code, http.StatusUnauthorized)
	}

	downloadRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/shares/"+createResponse.Share.Token+"/download",
		nil,
	)
	downloadRequest.Header.Set("X-Share-Password", "share-password")
	downloadRecorder := httptest.NewRecorder()
	router.ServeHTTP(downloadRecorder, downloadRequest)

	if downloadRecorder.Code != http.StatusOK {
		t.Fatalf("download status = %d, want %d: %s", downloadRecorder.Code, http.StatusOK, downloadRecorder.Body.String())
	}
	if downloadRecorder.Header().Get("Content-Disposition") != `attachment; filename="document.txt"` {
		t.Fatalf("content disposition = %q", downloadRecorder.Header().Get("Content-Disposition"))
	}
	if downloadRecorder.Body.String() != "shared content" {
		t.Fatalf("download body = %q, want shared content", downloadRecorder.Body.String())
	}

	secondDownloadRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/shares/"+createResponse.Share.Token+"/download",
		nil,
	)
	secondDownloadRequest.Header.Set("X-Share-Password", "share-password")
	secondDownloadRecorder := httptest.NewRecorder()
	router.ServeHTTP(secondDownloadRecorder, secondDownloadRequest)
	if secondDownloadRecorder.Code != http.StatusGone {
		t.Fatalf("over-limit status = %d, want %d: %s", secondDownloadRecorder.Code, http.StatusGone, secondDownloadRecorder.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/shares", nil)
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d: %s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}
	if !strings.Contains(listRecorder.Body.String(), createResponse.Share.Token) {
		t.Fatalf("listed shares should include token %q: %s", createResponse.Share.Token, listRecorder.Body.String())
	}

	revokeRequest := httptest.NewRequest(
		http.MethodDelete,
		"/api/shares/"+createResponse.Share.Token,
		nil,
	)
	revokeRecorder := httptest.NewRecorder()
	router.ServeHTTP(revokeRecorder, revokeRequest)
	if revokeRecorder.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, want %d: %s", revokeRecorder.Code, http.StatusOK, revokeRecorder.Body.String())
	}

	revokedDownloadRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/shares/"+createResponse.Share.Token+"/download",
		nil,
	)
	revokedDownloadRecorder := httptest.NewRecorder()
	router.ServeHTTP(revokedDownloadRecorder, revokedDownloadRequest)
	if revokedDownloadRecorder.Code != http.StatusNotFound {
		t.Fatalf("revoked download status = %d, want %d: %s", revokedDownloadRecorder.Code, http.StatusNotFound, revokedDownloadRecorder.Body.String())
	}
}

func TestHandlerPublicInfoRequiresPasswordBeforeDisclosingFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, repo, service := newTestHandler(t)
	fileID := createTestFile(t, repo, 1, "active")
	share, err := service.Create(1, fileID, "share-password", nil, nil)
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	router := gin.New()
	router.GET("/shares/:token", handler.PublicInfo)

	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/shares/"+share.Token, nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unverified public info status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}
	if strings.Contains(unauthorized.Body.String(), "document.txt") {
		t.Fatalf("unverified response leaks file name: %s", unauthorized.Body.String())
	}

	verifiedRequest := httptest.NewRequest(http.MethodGet, "/shares/"+share.Token, nil)
	verifiedRequest.Header.Set("X-Share-Password", "share-password")
	verified := httptest.NewRecorder()
	router.ServeHTTP(verified, verifiedRequest)
	if verified.Code != http.StatusOK {
		t.Fatalf("verified public info status = %d, want %d: %s", verified.Code, http.StatusOK, verified.Body.String())
	}
	if !strings.Contains(verified.Body.String(), "document.txt") {
		t.Fatalf("verified response should include file name: %s", verified.Body.String())
	}
}

func TestHandlerSaveSharedFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newTestRepository(t)
	fileID := createTestFile(t, repo, 1, "active")
	fileService := filemodule.NewService(
		filemodule.NewRepository(repo.db),
		nil,
		1024,
	)
	service := NewService(repo, nil, WithFileSaver(fileService))
	share, err := service.Create(1, fileID, "share-password", nil, nil)
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	router := gin.New()
	router.POST("/api/shares/:token/save", func(c *gin.Context) {
		c.Set(middleware.UserIDKey, int64(2))
		NewHandler(service).Save(c)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/shares/"+share.Token+"/save",
		strings.NewReader(`{"password":"share-password"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("save status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var response struct {
		File filemodule.UserFile `json:"file"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode save response: %v", err)
	}
	if response.File.UserID != 2 || response.File.OriginalName != "document.txt" {
		t.Fatalf("saved response file = %#v, want user 2 document.txt", response.File)
	}

	var objectID int64
	if err := repo.db.QueryRow(`SELECT object_id FROM user_files WHERE id = $1`, response.File.ID).Scan(&objectID); err != nil {
		t.Fatalf("find saved file object: %v", err)
	}
	var sourceObjectID int64
	if err := repo.db.QueryRow(`SELECT object_id FROM user_files WHERE id = $1`, fileID).Scan(&sourceObjectID); err != nil {
		t.Fatalf("find source file object: %v", err)
	}
	if objectID != sourceObjectID {
		t.Fatalf("saved object ID = %d, want source object ID %d", objectID, sourceObjectID)
	}
}

func TestHandlerDownloadReturnsLockedWhenPolicyRejectsFile(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := newTestRepository(t)
	storage := &fakeStorage{
		content: map[string][]byte{
			"uploads/document.txt": []byte("shared content"),
		},
	}
	service := NewService(
		repo,
		storage,
		WithDownloadPolicy(&fakeDownloadPolicy{err: errors.New("scan is incomplete")}),
	)
	fileID := createTestFile(t, repo, 1, "active")
	share, err := service.Create(1, fileID, "", nil, nil)
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	router := gin.New()
	router.GET("/shares/:token/download", NewHandler(service).Download)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/shares/"+share.Token+"/download",
		nil,
	))
	if response.Code != http.StatusLocked {
		t.Fatalf("blocked shared download status = %d, want %d: %s", response.Code, http.StatusLocked, response.Body.String())
	}
}

func TestHandlerDownloadExpiredShareWritesOneErrorResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, repo, _ := newTestHandler(t)
	fileID := createTestFile(t, repo, 1, "active")
	expiresAt := time.Now().UTC().Add(-time.Hour)

	if _, err := repo.Create(&Share{
		Token:      "expired-share",
		UserFileID: fileID,
		ExpiresAt:  &expiresAt,
	}); err != nil {
		t.Fatalf("create expired share: %v", err)
	}

	router := gin.New()
	router.GET("/api/shares/:token/download", handler.Download)
	req := httptest.NewRequest(http.MethodGet, "/api/shares/expired-share/download", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusGone {
		t.Fatalf("expired status = %d, want %d: %s", recorder.Code, http.StatusGone, recorder.Body.String())
	}
	if strings.Count(recorder.Body.String(), `"error"`) != 1 {
		t.Fatalf("expired response should contain one error body: %s", recorder.Body.String())
	}
}

func TestHandlerDownloadSupportsRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, repo, _ := newTestHandler(t)
	fileID := createTestFile(t, repo, 1, "active")

	if _, err := repo.Create(&Share{
		Token:      "range-share",
		UserFileID: fileID,
	}); err != nil {
		t.Fatalf("create range share: %v", err)
	}

	router := gin.New()
	router.GET("/api/shares/:token/download", handler.Download)
	req := httptest.NewRequest(http.MethodGet, "/api/shares/range-share/download", nil)
	req.Header.Set("Range", "bytes=0-5")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d, want %d: %s", recorder.Code, http.StatusPartialContent, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Range") != "bytes 0-5/14" {
		t.Fatalf("content range = %q, want bytes 0-5/14", recorder.Header().Get("Content-Range"))
	}
	if recorder.Body.String() != "shared" {
		t.Fatalf("range body = %q, want shared", recorder.Body.String())
	}
}

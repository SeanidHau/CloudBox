package file

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const testJWTSecret = "test-secret"

func newTestFileRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()

	gin.SetMode(gin.TestMode)

	service := newTestServiceWithStorage(t, &fakeStorage{})
	handler := NewHandler(service)

	router := gin.New()
	protected := router.Group("")
	protected.Use(middleware.Auth(testJWTSecret))
	protected.POST("/files", handler.Upload)
	protected.GET("/files", handler.ListActive)
	protected.GET("/files/trash", handler.ListDeleted)
	protected.GET("/files/:id/download", handler.Download)
	protected.DELETE("/files/:id", handler.SoftDelete)
	protected.POST("/files/:id/restore", handler.Restore)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": int64(1),
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	tokenText, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}

	return router, tokenText
}

func newAuthenticatedRequest(method string, target string, body io.Reader, token string) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func TestFileHandlerLifecycle(t *testing.T) {
	router, token := newTestFileRouter(t)

	const fileName = "hello.txt"
	const fileContent = "hello cloudbox"

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write([]byte(fileContent)); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	uploadRequest := newAuthenticatedRequest(http.MethodPost, "/files", &body, token)
	uploadRequest.Header.Set("Content-Type", writer.FormDataContentType())
	uploadResponse := httptest.NewRecorder()
	router.ServeHTTP(uploadResponse, uploadRequest)

	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d: %s", uploadResponse.Code, http.StatusCreated, uploadResponse.Body.String())
	}

	var uploaded struct {
		File UserFile `json:"file"`
	}
	if err := json.Unmarshal(uploadResponse.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploaded.File.OriginalName != fileName {
		t.Fatalf("uploaded filename = %q, want %q", uploaded.File.OriginalName, fileName)
	}
	if uploaded.File.Size != int64(len(fileContent)) {
		t.Fatalf("uploaded size = %d, want %d", uploaded.File.Size, len(fileContent))
	}

	listRequest := newAuthenticatedRequest(http.MethodGet, "/files", nil, token)
	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, listRequest)

	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listResponse.Code, http.StatusOK)
	}

	var activeFiles struct {
		Files []UserFile `json:"files"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &activeFiles); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(activeFiles.Files) != 1 || activeFiles.Files[0].ID != uploaded.File.ID {
		t.Fatalf("active files = %#v, want uploaded file", activeFiles.Files)
	}

	filePath := "/files/" + strconv.FormatInt(uploaded.File.ID, 10)
	downloadRequest := newAuthenticatedRequest(http.MethodGet, filePath+"/download", nil, token)
	downloadResponse := httptest.NewRecorder()
	router.ServeHTTP(downloadResponse, downloadRequest)

	if downloadResponse.Code != http.StatusOK {
		t.Fatalf("download status = %d, want %d", downloadResponse.Code, http.StatusOK)
	}
	if downloadResponse.Body.String() != fileContent {
		t.Fatalf("downloaded content = %q, want %q", downloadResponse.Body.String(), fileContent)
	}
	if !strings.Contains(downloadResponse.Header().Get("Content-Disposition"), fileName) {
		t.Fatalf("unexpected content disposition: %q", downloadResponse.Header().Get("Content-Disposition"))
	}

	deleteRequest := newAuthenticatedRequest(http.MethodDelete, filePath, nil, token)
	deleteResponse := httptest.NewRecorder()
	router.ServeHTTP(deleteResponse, deleteRequest)

	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d", deleteResponse.Code, http.StatusOK)
	}

	trashRequest := newAuthenticatedRequest(http.MethodGet, "/files/trash", nil, token)
	trashResponse := httptest.NewRecorder()
	router.ServeHTTP(trashResponse, trashRequest)

	if trashResponse.Code != http.StatusOK {
		t.Fatalf("trash status = %d, want %d", trashResponse.Code, http.StatusOK)
	}

	var trashedFiles struct {
		Files []UserFile `json:"files"`
	}
	if err := json.Unmarshal(trashResponse.Body.Bytes(), &trashedFiles); err != nil {
		t.Fatalf("decode trash response: %v", err)
	}
	if len(trashedFiles.Files) != 1 || trashedFiles.Files[0].ID != uploaded.File.ID {
		t.Fatalf("trashed files = %#v, want uploaded file", trashedFiles.Files)
	}

	restoreRequest := newAuthenticatedRequest(http.MethodPost, filePath+"/restore", nil, token)
	restoreResponse := httptest.NewRecorder()
	router.ServeHTTP(restoreResponse, restoreRequest)

	if restoreResponse.Code != http.StatusOK {
		t.Fatalf("restore status = %d, want %d", restoreResponse.Code, http.StatusOK)
	}

	listAfterRestoreRequest := newAuthenticatedRequest(http.MethodGet, "/files", nil, token)
	listAfterRestoreResponse := httptest.NewRecorder()
	router.ServeHTTP(listAfterRestoreResponse, listAfterRestoreRequest)

	if listAfterRestoreResponse.Code != http.StatusOK {
		t.Fatalf("list after restore status = %d, want %d", listAfterRestoreResponse.Code, http.StatusOK)
	}

	if err := json.Unmarshal(listAfterRestoreResponse.Body.Bytes(), &activeFiles); err != nil {
		t.Fatalf("decode list after restore response: %v", err)
	}
	if len(activeFiles.Files) != 1 || activeFiles.Files[0].ID != uploaded.File.ID {
		t.Fatalf("active files after restore = %#v, want uploaded file", activeFiles.Files)
	}
}

func TestFileHandlerRejectsInvalidFileID(t *testing.T) {
	router, token := newTestFileRouter(t)

	request := newAuthenticatedRequest(http.MethodDelete, "/files/not-a-number", nil, token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !strings.Contains(response.Body.String(), "invalid file id") {
		t.Fatalf("unexpected response body: %s", response.Body.String())
	}
}

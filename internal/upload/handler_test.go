package upload

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const testJWTSecret = "test-secret"

func newTestHandlerRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()

	gin.SetMode(gin.TestMode)

	handler := NewHandler(newTestService(t))
	router := gin.New()
	protected := router.Group("")
	protected.Use(middleware.Auth(testJWTSecret))
	protected.POST("/uploads/init", handler.Init)

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

func newAuthenticatedRequest(method string, target string, body *bytes.Reader, token string) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestHandlerInitUpload(t *testing.T) {
	router, token := newTestHandlerRouter(t)

	requestBody := []byte(`{
		"original_name":"video.mp4",
		"content_type":"video/mp4",
		"file_size":25,
		"chunk_size":10,
		"file_hash":"file-hash"
	}`)
	request := newAuthenticatedRequest(http.MethodPost, "/uploads/init", bytes.NewReader(requestBody), token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("init status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}

	var result struct {
		Upload Task `json:"upload"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode init response: %v", err)
	}
	if result.Upload.ID == "" {
		t.Fatal("expected upload ID")
	}
	if result.Upload.TotalChunks != 3 {
		t.Fatalf("total chunks = %d, want 3", result.Upload.TotalChunks)
	}
	if result.Upload.Status != StatusUploading {
		t.Fatalf("status = %q, want %q", result.Upload.Status, StatusUploading)
	}
}

func TestHandlerInitUploadRejectsInvalidFileSize(t *testing.T) {
	router, token := newTestHandlerRouter(t)

	requestBody := []byte(`{
		"original_name":"video.mp4",
		"file_size":0,
		"chunk_size":10
	}`)
	request := newAuthenticatedRequest(http.MethodPost, "/uploads/init", bytes.NewReader(requestBody), token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("init status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

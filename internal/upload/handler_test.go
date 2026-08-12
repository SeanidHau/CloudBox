package upload

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const testJWTSecret = "test-secret"

func newTestHandlerRouter(t *testing.T) (*gin.Engine, string) {
	return newTestHandlerRouterWithQuota(t, 1<<30)
}

func newTestHandlerRouterWithQuota(t *testing.T, quotaBytes int64) (*gin.Engine, string) {
	t.Helper()

	gin.SetMode(gin.TestMode)

	handler := NewHandler(newTestServiceWithQuota(t, quotaBytes))
	router := gin.New()
	protected := router.Group("")
	protected.Use(middleware.Auth(testJWTSecret))
	protected.POST("/uploads/init", handler.Init)
	protected.GET("/uploads", handler.ListUploading)
	protected.PUT("/uploads/:id/chunks/:number", handler.UploadChunk)
	protected.GET("/uploads/:id", handler.GetStatus)
	protected.POST("/uploads/:id/complete", handler.Complete)
	protected.DELETE("/uploads/:id", handler.Cancel)

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

func TestHandlerInitUploadRejectsQuotaExceeded(t *testing.T) {
	router, token := newTestHandlerRouterWithQuota(t, 5)

	requestBody := []byte(`{
		"original_name":"video.mp4",
		"file_size":6,
		"chunk_size":6
	}`)
	request := newAuthenticatedRequest(http.MethodPost, "/uploads/init", bytes.NewReader(requestBody), token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("init over quota status = %d, want %d: %s", response.Code, http.StatusConflict, response.Body.String())
	}
}

func TestHandlerInitUploadValidatesParentFolder(t *testing.T) {
	router, token := newTestHandlerRouter(t)

	invalidParentBody := []byte(`{
		"parent_id":0,
		"original_name":"video.mp4",
		"file_size":10,
		"chunk_size":10
	}`)
	invalidParentRequest := newAuthenticatedRequest(http.MethodPost, "/uploads/init", bytes.NewReader(invalidParentBody), token)
	invalidParentResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidParentResponse, invalidParentRequest)
	if invalidParentResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid parent status = %d, want %d: %s", invalidParentResponse.Code, http.StatusBadRequest, invalidParentResponse.Body.String())
	}

	missingParentBody := []byte(`{
		"parent_id":999,
		"original_name":"video.mp4",
		"file_size":10,
		"chunk_size":10
	}`)
	missingParentRequest := newAuthenticatedRequest(http.MethodPost, "/uploads/init", bytes.NewReader(missingParentBody), token)
	missingParentResponse := httptest.NewRecorder()
	router.ServeHTTP(missingParentResponse, missingParentRequest)
	if missingParentResponse.Code != http.StatusNotFound {
		t.Fatalf("missing parent status = %d, want %d: %s", missingParentResponse.Code, http.StatusNotFound, missingParentResponse.Body.String())
	}
}

func TestHandlerUploadChunk(t *testing.T) {
	router, token := newTestHandlerRouter(t)
	task := initializeUpload(t, router, token)

	request := newAuthenticatedRequest(
		http.MethodPut,
		"/uploads/"+task.ID+"/chunks/0",
		bytes.NewReader([]byte("0123456789")),
		token,
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}

	var result struct {
		Chunk Chunk `json:"chunk"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode chunk response: %v", err)
	}
	if result.Chunk.Number != 0 || result.Chunk.Size != 10 {
		t.Fatalf("chunk = %#v, want number 0 and size 10", result.Chunk)
	}
}

func TestHandlerUploadChunkValidatesRequest(t *testing.T) {
	router, token := newTestHandlerRouter(t)
	task := initializeUpload(t, router, token)

	testCases := []struct {
		name   string
		target string
		body   string
		want   int
	}{
		{
			name:   "invalid number",
			target: "/uploads/" + task.ID + "/chunks/not-a-number",
			body:   "0123456789",
			want:   http.StatusBadRequest,
		},
		{
			name:   "missing task",
			target: "/uploads/missing/chunks/0",
			body:   "0123456789",
			want:   http.StatusNotFound,
		},
		{
			name:   "wrong chunk size",
			target: "/uploads/" + task.ID + "/chunks/1",
			body:   "short",
			want:   http.StatusBadRequest,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := newAuthenticatedRequest(
				http.MethodPut,
				testCase.target,
				bytes.NewReader([]byte(testCase.body)),
				token,
			)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != testCase.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, testCase.want, response.Body.String())
			}
		})
	}
}

func TestHandlerGetUploadStatus(t *testing.T) {
	router, token := newTestHandlerRouter(t)
	task := initializeUpload(t, router, token)

	for _, chunk := range []struct {
		number int
		body   string
	}{
		{number: 1, body: "abcdefghij"},
		{number: 0, body: "0123456789"},
	} {
		request := newAuthenticatedRequest(
			http.MethodPut,
			"/uploads/"+task.ID+"/chunks/"+strconv.Itoa(chunk.number),
			bytes.NewReader([]byte(chunk.body)),
			token,
		)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("upload chunk %d status = %d, want %d: %s", chunk.number, response.Code, http.StatusCreated, response.Body.String())
		}
	}

	request := newAuthenticatedRequest(http.MethodGet, "/uploads/"+task.ID, bytes.NewReader(nil), token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status request = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var result UploadStatus
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if result.Upload.ID != task.ID || len(result.Chunks) != 2 {
		t.Fatalf("status = %#v, want task with two chunks", result)
	}
	if result.Chunks[0].Number != 0 || result.Chunks[1].Number != 1 {
		t.Fatalf("chunk order = %d, %d, want 0, 1", result.Chunks[0].Number, result.Chunks[1].Number)
	}

	missingRequest := newAuthenticatedRequest(http.MethodGet, "/uploads/missing", bytes.NewReader(nil), token)
	missingResponse := httptest.NewRecorder()
	router.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d: %s", missingResponse.Code, http.StatusNotFound, missingResponse.Body.String())
	}
}

func TestHandlerListsOnlyCurrentUsersUploadingTasks(t *testing.T) {
	router, token := newTestHandlerRouter(t)
	first := initializeUpload(t, router, token)
	second := initializeUpload(t, router, token)

	request := newAuthenticatedRequest(http.MethodGet, "/uploads", bytes.NewReader(nil), token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var result struct {
		Uploads []Task `json:"uploads"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(result.Uploads) != 2 {
		t.Fatalf("upload count = %d, want 2", len(result.Uploads))
	}
	ids := map[string]bool{result.Uploads[0].ID: true, result.Uploads[1].ID: true}
	if !ids[first.ID] || !ids[second.ID] {
		t.Fatalf("uploads = %#v, want both current-user tasks", result.Uploads)
	}
}

func TestHandlerCompleteUpload(t *testing.T) {
	router, token := newTestHandlerRouter(t)
	task := initializeUpload(t, router, token)

	for _, chunk := range []struct {
		number int
		body   string
	}{
		{number: 0, body: "0123456789"},
		{number: 1, body: "abcdefghij"},
		{number: 2, body: "12345"},
	} {
		request := newAuthenticatedRequest(
			http.MethodPut,
			"/uploads/"+task.ID+"/chunks/"+strconv.Itoa(chunk.number),
			bytes.NewReader([]byte(chunk.body)),
			token,
		)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("upload chunk %d status = %d, want %d: %s", chunk.number, response.Code, http.StatusCreated, response.Body.String())
		}
	}

	request := newAuthenticatedRequest(http.MethodPost, "/uploads/"+task.ID+"/complete", bytes.NewReader(nil), token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("complete status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}

	var result struct {
		File struct {
			ID   int64 `json:"id"`
			Size int64 `json:"size"`
		} `json:"file"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode complete response: %v", err)
	}
	if result.File.ID == 0 || result.File.Size != 25 {
		t.Fatalf("file = %#v, want persisted file with size 25", result.File)
	}
}

func TestHandlerCompleteUploadValidatesTask(t *testing.T) {
	router, token := newTestHandlerRouter(t)

	missingRequest := newAuthenticatedRequest(http.MethodPost, "/uploads/missing/complete", bytes.NewReader(nil), token)
	missingResponse := httptest.NewRecorder()
	router.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing complete status = %d, want %d: %s", missingResponse.Code, http.StatusNotFound, missingResponse.Body.String())
	}

	task := initializeUpload(t, router, token)
	incompleteRequest := newAuthenticatedRequest(http.MethodPost, "/uploads/"+task.ID+"/complete", bytes.NewReader(nil), token)
	incompleteResponse := httptest.NewRecorder()
	router.ServeHTTP(incompleteResponse, incompleteRequest)
	if incompleteResponse.Code != http.StatusConflict {
		t.Fatalf("incomplete complete status = %d, want %d: %s", incompleteResponse.Code, http.StatusConflict, incompleteResponse.Body.String())
	}
}

func TestHandlerCancelUpload(t *testing.T) {
	router, token := newTestHandlerRouter(t)
	task := initializeUpload(t, router, token)

	request := newAuthenticatedRequest(http.MethodDelete, "/uploads/"+task.ID, bytes.NewReader(nil), token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("cancel status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
	}

	repeatedRequest := newAuthenticatedRequest(http.MethodDelete, "/uploads/"+task.ID, bytes.NewReader(nil), token)
	repeatedResponse := httptest.NewRecorder()
	router.ServeHTTP(repeatedResponse, repeatedRequest)
	if repeatedResponse.Code != http.StatusConflict {
		t.Fatalf("repeated cancel status = %d, want %d: %s", repeatedResponse.Code, http.StatusConflict, repeatedResponse.Body.String())
	}

	missingRequest := newAuthenticatedRequest(http.MethodDelete, "/uploads/missing", bytes.NewReader(nil), token)
	missingResponse := httptest.NewRecorder()
	router.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing cancel status = %d, want %d: %s", missingResponse.Code, http.StatusNotFound, missingResponse.Body.String())
	}
}

func initializeUpload(t *testing.T, router *gin.Engine, token string) Task {
	t.Helper()

	requestBody := []byte(`{
		"original_name":"video.mp4",
		"content_type":"video/mp4",
		"file_size":25,
		"chunk_size":10
	}`)
	request := newAuthenticatedRequest(http.MethodPost, "/uploads/init", bytes.NewReader(requestBody), token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("initialize upload status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}

	var result struct {
		Upload Task `json:"upload"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	return result.Upload
}

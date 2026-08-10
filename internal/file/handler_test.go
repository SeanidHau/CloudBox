package file

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	jobmodule "github.com/SeanidHau/CloudBox/internal/job"
	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const testJWTSecret = "test-secret"

func newTestFileRouter(t *testing.T) (*gin.Engine, string) {
	return newTestFileRouterWithQuota(t, testStorageQuotaBytes)
}

func newTestFileRouterWithQuota(t *testing.T, quotaBytes int64) (*gin.Engine, string) {
	t.Helper()

	gin.SetMode(gin.TestMode)

	service := newTestServiceWithStorageQuota(t, &fakeStorage{}, quotaBytes)
	handler := NewHandler(service)

	router := gin.New()
	protected := router.Group("")
	protected.Use(middleware.Auth(testJWTSecret))
	protected.POST("/files", handler.Upload)
	protected.POST("/files/instant", handler.InstantUpload)
	protected.GET("/files", handler.ListActive)
	protected.GET("/files/trash", handler.ListDeleted)
	protected.GET("/files/:id/download", handler.Download)
	protected.DELETE("/files/:id/permanent", handler.PermanentlyDelete)
	protected.DELETE("/files/:id", handler.SoftDelete)
	protected.POST("/files/:id/restore", handler.Restore)
	protected.PATCH("/files/:id/move", handler.MoveActive)
	protected.PATCH("/files/:id/rename", handler.RenameActive)
	protected.POST("/folders", handler.CreateFolder)
	protected.GET("/folders", handler.ListFolders)
	protected.PATCH("/folders/:id/rename", handler.RenameFolder)
	protected.PATCH("/folders/:id/move", handler.MoveFolder)
	protected.DELETE("/folders/:id", handler.DeleteFolder)
	protected.GET("/storage", handler.GetStorageUsage)

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

func TestFileHandlerEnqueueVerification(t *testing.T) {
	gin.SetMode(gin.TestMode)

	storage := &fakeStorage{}
	queue := &fakeJobEnqueuer{
		job: &jobmodule.Job{ID: "verify-job", Status: jobmodule.StatusQueued},
	}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		storage,
		testStorageQuotaBytes,
		WithJobEnqueuer(queue),
	)
	uploaded, err := service.Upload(1, "verify.txt", "text/plain", strings.NewReader("verify content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}

	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.UserIDKey, int64(1))
		c.Next()
	})
	router.POST("/files/:id/verify", handler.EnqueueVerification)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/files/"+strconv.FormatInt(uploaded.ID, 10)+"/verify",
		nil,
	))
	if response.Code != http.StatusAccepted {
		t.Fatalf("enqueue status = %d, want %d: %s", response.Code, http.StatusAccepted, response.Body.String())
	}

	var result struct {
		Job jobmodule.Job `json:"job"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode enqueue response: %v", err)
	}
	if result.Job.ID != "verify-job" || queue.userID != 1 {
		t.Fatalf("enqueue result/user = %#v/%d, want verify-job/1", result.Job, queue.userID)
	}
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

	rangeRequest := newAuthenticatedRequest(http.MethodGet, filePath+"/download", nil, token)
	rangeRequest.Header.Set("Range", "bytes=6-13")
	rangeResponse := httptest.NewRecorder()
	router.ServeHTTP(rangeResponse, rangeRequest)

	if rangeResponse.Code != http.StatusPartialContent {
		t.Fatalf("range download status = %d, want %d", rangeResponse.Code, http.StatusPartialContent)
	}
	if rangeResponse.Body.String() != "cloudbox" {
		t.Fatalf("range download content = %q, want %q", rangeResponse.Body.String(), "cloudbox")
	}
	if rangeResponse.Header().Get("Content-Range") != "bytes 6-13/14" {
		t.Fatalf("content range = %q, want %q", rangeResponse.Header().Get("Content-Range"), "bytes 6-13/14")
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

func TestFileHandlerPermanentlyDelete(t *testing.T) {
	router, token := newTestFileRouter(t)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "permanent.txt")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write([]byte("permanent content")); err != nil {
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

	filePath := "/files/" + strconv.FormatInt(uploaded.File.ID, 10)

	// 活跃文件尚可恢复，不能绕过回收站直接永久删除。
	activeDeleteRequest := newAuthenticatedRequest(http.MethodDelete, filePath+"/permanent", nil, token)
	activeDeleteResponse := httptest.NewRecorder()
	router.ServeHTTP(activeDeleteResponse, activeDeleteRequest)
	if activeDeleteResponse.Code != http.StatusNotFound {
		t.Fatalf("active permanent delete status = %d, want %d", activeDeleteResponse.Code, http.StatusNotFound)
	}

	softDeleteRequest := newAuthenticatedRequest(http.MethodDelete, filePath, nil, token)
	softDeleteResponse := httptest.NewRecorder()
	router.ServeHTTP(softDeleteResponse, softDeleteRequest)
	if softDeleteResponse.Code != http.StatusOK {
		t.Fatalf("soft delete status = %d, want %d", softDeleteResponse.Code, http.StatusOK)
	}

	permanentDeleteRequest := newAuthenticatedRequest(http.MethodDelete, filePath+"/permanent", nil, token)
	permanentDeleteResponse := httptest.NewRecorder()
	router.ServeHTTP(permanentDeleteResponse, permanentDeleteRequest)
	if permanentDeleteResponse.Code != http.StatusNoContent {
		t.Fatalf("permanent delete status = %d, want %d: %s", permanentDeleteResponse.Code, http.StatusNoContent, permanentDeleteResponse.Body.String())
	}
	if permanentDeleteResponse.Body.Len() != 0 {
		t.Fatalf("permanent delete response body = %q, want empty", permanentDeleteResponse.Body.String())
	}

	trashRequest := newAuthenticatedRequest(http.MethodGet, "/files/trash", nil, token)
	trashResponse := httptest.NewRecorder()
	router.ServeHTTP(trashResponse, trashRequest)
	if trashResponse.Code != http.StatusOK {
		t.Fatalf("trash status = %d, want %d", trashResponse.Code, http.StatusOK)
	}

	var trash struct {
		Files []UserFile `json:"files"`
	}
	if err := json.Unmarshal(trashResponse.Body.Bytes(), &trash); err != nil {
		t.Fatalf("decode trash response: %v", err)
	}
	if len(trash.Files) != 0 {
		t.Fatalf("trash files = %#v, want empty", trash.Files)
	}

	repeatDeleteRequest := newAuthenticatedRequest(http.MethodDelete, filePath+"/permanent", nil, token)
	repeatDeleteResponse := httptest.NewRecorder()
	router.ServeHTTP(repeatDeleteResponse, repeatDeleteRequest)
	if repeatDeleteResponse.Code != http.StatusNotFound {
		t.Fatalf("repeat permanent delete status = %d, want %d", repeatDeleteResponse.Code, http.StatusNotFound)
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

func TestFileHandlerInstantUpload(t *testing.T) {
	router, token := newTestFileRouter(t)

	const content = "same content"

	var uploadBody bytes.Buffer
	writer := multipart.NewWriter(&uploadBody)
	part, err := writer.CreateFormFile("file", "original.txt")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	uploadRequest := newAuthenticatedRequest(http.MethodPost, "/files", &uploadBody, token)
	uploadRequest.Header.Set("Content-Type", writer.FormDataContentType())
	uploadResponse := httptest.NewRecorder()
	router.ServeHTTP(uploadResponse, uploadRequest)
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d: %s", uploadResponse.Code, http.StatusCreated, uploadResponse.Body.String())
	}

	hash := sha256.Sum256([]byte(content))
	instantBody, err := json.Marshal(instantUploadRequest{
		OriginalName: "instant-copy.txt",
		FileHash:     hex.EncodeToString(hash[:]),
	})
	if err != nil {
		t.Fatalf("encode instant upload request: %v", err)
	}

	instantRequest := newAuthenticatedRequest(http.MethodPost, "/files/instant", bytes.NewReader(instantBody), token)
	instantRequest.Header.Set("Content-Type", "application/json")
	instantResponse := httptest.NewRecorder()
	router.ServeHTTP(instantResponse, instantRequest)

	if instantResponse.Code != http.StatusCreated {
		t.Fatalf("instant upload status = %d, want %d: %s", instantResponse.Code, http.StatusCreated, instantResponse.Body.String())
	}

	folderBody, err := json.Marshal(createFolderRequest{Name: "documents"})
	if err != nil {
		t.Fatalf("encode folder request: %v", err)
	}
	folderRequest := newAuthenticatedRequest(http.MethodPost, "/folders", bytes.NewReader(folderBody), token)
	folderRequest.Header.Set("Content-Type", "application/json")
	folderResponse := httptest.NewRecorder()
	router.ServeHTTP(folderResponse, folderRequest)
	if folderResponse.Code != http.StatusCreated {
		t.Fatalf("create folder status = %d, want %d: %s", folderResponse.Code, http.StatusCreated, folderResponse.Body.String())
	}

	var folderResult struct {
		Folder Folder `json:"folder"`
	}
	if err := json.Unmarshal(folderResponse.Body.Bytes(), &folderResult); err != nil {
		t.Fatalf("decode folder response: %v", err)
	}

	instantInFolderBody, err := json.Marshal(instantUploadRequest{
		ParentID:     &folderResult.Folder.ID,
		OriginalName: "folder-copy.txt",
		FileHash:     hex.EncodeToString(hash[:]),
	})
	if err != nil {
		t.Fatalf("encode folder instant upload request: %v", err)
	}
	instantInFolderRequest := newAuthenticatedRequest(http.MethodPost, "/files/instant", bytes.NewReader(instantInFolderBody), token)
	instantInFolderRequest.Header.Set("Content-Type", "application/json")
	instantInFolderResponse := httptest.NewRecorder()
	router.ServeHTTP(instantInFolderResponse, instantInFolderRequest)
	if instantInFolderResponse.Code != http.StatusCreated {
		t.Fatalf("folder instant upload status = %d, want %d: %s", instantInFolderResponse.Code, http.StatusCreated, instantInFolderResponse.Body.String())
	}

	var instantInFolderResult struct {
		File UserFile `json:"file"`
	}
	if err := json.Unmarshal(instantInFolderResponse.Body.Bytes(), &instantInFolderResult); err != nil {
		t.Fatalf("decode folder instant upload response: %v", err)
	}
	if instantInFolderResult.File.ParentID == nil || *instantInFolderResult.File.ParentID != folderResult.Folder.ID {
		t.Fatalf("instant file parent ID = %v, want %d", instantInFolderResult.File.ParentID, folderResult.Folder.ID)
	}

	missingBody, err := json.Marshal(instantUploadRequest{
		OriginalName: "missing.txt",
		FileHash:     "missing-hash",
	})
	if err != nil {
		t.Fatalf("encode missing instant upload request: %v", err)
	}

	missingRequest := newAuthenticatedRequest(http.MethodPost, "/files/instant", bytes.NewReader(missingBody), token)
	missingRequest.Header.Set("Content-Type", "application/json")
	missingResponse := httptest.NewRecorder()
	router.ServeHTTP(missingResponse, missingRequest)

	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing hash status = %d, want %d: %s", missingResponse.Code, http.StatusNotFound, missingResponse.Body.String())
	}

	missingFolderID := int64(999)
	missingFolderBody, err := json.Marshal(instantUploadRequest{
		ParentID:     &missingFolderID,
		OriginalName: "missing-folder.txt",
		FileHash:     hex.EncodeToString(hash[:]),
	})
	if err != nil {
		t.Fatalf("encode missing folder instant upload request: %v", err)
	}
	missingFolderRequest := newAuthenticatedRequest(http.MethodPost, "/files/instant", bytes.NewReader(missingFolderBody), token)
	missingFolderRequest.Header.Set("Content-Type", "application/json")
	missingFolderResponse := httptest.NewRecorder()
	router.ServeHTTP(missingFolderResponse, missingFolderRequest)
	if missingFolderResponse.Code != http.StatusNotFound {
		t.Fatalf("missing folder status = %d, want %d: %s", missingFolderResponse.Code, http.StatusNotFound, missingFolderResponse.Body.String())
	}
}

func TestFileHandlerFolderLifecycle(t *testing.T) {
	router, token := newTestFileRouter(t)

	createFolder := func(requestBody createFolderRequest) (*Folder, int) {
		body, err := json.Marshal(requestBody)
		if err != nil {
			t.Fatalf("encode folder request: %v", err)
		}

		request := newAuthenticatedRequest(http.MethodPost, "/folders", bytes.NewReader(body), token)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		if response.Code != http.StatusCreated {
			return nil, response.Code
		}

		var result struct {
			Folder Folder `json:"folder"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode folder response: %v", err)
		}

		return &result.Folder, response.Code
	}

	root, status := createFolder(createFolderRequest{Name: "documents"})
	if status != http.StatusCreated || root.ParentID != nil {
		t.Fatalf("root folder status = %d, folder = %#v", status, root)
	}

	child, status := createFolder(createFolderRequest{ParentID: &root.ID, Name: "photos"})
	if status != http.StatusCreated || child.ParentID == nil || *child.ParentID != root.ID {
		t.Fatalf("child folder status = %d, folder = %#v", status, child)
	}

	rootListRequest := newAuthenticatedRequest(http.MethodGet, "/folders", nil, token)
	rootListResponse := httptest.NewRecorder()
	router.ServeHTTP(rootListResponse, rootListRequest)
	if rootListResponse.Code != http.StatusOK {
		t.Fatalf("root list status = %d, want %d: %s", rootListResponse.Code, http.StatusOK, rootListResponse.Body.String())
	}

	var rootList struct {
		Folders []Folder `json:"folders"`
	}
	if err := json.Unmarshal(rootListResponse.Body.Bytes(), &rootList); err != nil {
		t.Fatalf("decode root folder list: %v", err)
	}
	if len(rootList.Folders) != 1 || rootList.Folders[0].ID != root.ID {
		t.Fatalf("root folders = %#v, want documents", rootList.Folders)
	}

	childListRequest := newAuthenticatedRequest(http.MethodGet, "/folders?parent_id="+strconv.FormatInt(root.ID, 10), nil, token)
	childListResponse := httptest.NewRecorder()
	router.ServeHTTP(childListResponse, childListRequest)
	if childListResponse.Code != http.StatusOK {
		t.Fatalf("child list status = %d, want %d: %s", childListResponse.Code, http.StatusOK, childListResponse.Body.String())
	}

	var childList struct {
		Folders []Folder `json:"folders"`
	}
	if err := json.Unmarshal(childListResponse.Body.Bytes(), &childList); err != nil {
		t.Fatalf("decode child folder list: %v", err)
	}
	if len(childList.Folders) != 1 || childList.Folders[0].ID != child.ID {
		t.Fatalf("child folders = %#v, want photos", childList.Folders)
	}

	_, duplicateStatus := createFolder(createFolderRequest{Name: "documents"})
	if duplicateStatus != http.StatusConflict {
		t.Fatalf("duplicate folder status = %d, want %d", duplicateStatus, http.StatusConflict)
	}

	_, emptyStatus := createFolder(createFolderRequest{Name: "   "})
	if emptyStatus != http.StatusBadRequest {
		t.Fatalf("empty folder status = %d, want %d", emptyStatus, http.StatusBadRequest)
	}

	invalidParentRequest := newAuthenticatedRequest(http.MethodGet, "/folders?parent_id=0", nil, token)
	invalidParentResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidParentResponse, invalidParentRequest)
	if invalidParentResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid parent status = %d, want %d", invalidParentResponse.Code, http.StatusBadRequest)
	}

	missingParentRequest := newAuthenticatedRequest(http.MethodGet, "/folders?parent_id=999", nil, token)
	missingParentResponse := httptest.NewRecorder()
	router.ServeHTTP(missingParentResponse, missingParentRequest)
	if missingParentResponse.Code != http.StatusNotFound {
		t.Fatalf("missing parent status = %d, want %d", missingParentResponse.Code, http.StatusNotFound)
	}
}

func TestFileHandlerUploadsIntoFolder(t *testing.T) {
	router, token := newTestFileRouter(t)

	folderBody, err := json.Marshal(createFolderRequest{Name: "documents"})
	if err != nil {
		t.Fatalf("encode folder request: %v", err)
	}
	folderRequest := newAuthenticatedRequest(http.MethodPost, "/folders", bytes.NewReader(folderBody), token)
	folderRequest.Header.Set("Content-Type", "application/json")
	folderResponse := httptest.NewRecorder()
	router.ServeHTTP(folderResponse, folderRequest)
	if folderResponse.Code != http.StatusCreated {
		t.Fatalf("create folder status = %d, want %d: %s", folderResponse.Code, http.StatusCreated, folderResponse.Body.String())
	}

	var folderResult struct {
		Folder Folder `json:"folder"`
	}
	if err := json.Unmarshal(folderResponse.Body.Bytes(), &folderResult); err != nil {
		t.Fatalf("decode folder response: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("parent_id", strconv.FormatInt(folderResult.Folder.ID, 10)); err != nil {
		t.Fatalf("write parent ID: %v", err)
	}
	part, err := writer.CreateFormFile("file", "report.txt")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write([]byte("folder content")); err != nil {
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

	var uploadResult struct {
		File UserFile `json:"file"`
	}
	if err := json.Unmarshal(uploadResponse.Body.Bytes(), &uploadResult); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploadResult.File.ParentID == nil || *uploadResult.File.ParentID != folderResult.Folder.ID {
		t.Fatalf("file parent ID = %v, want %d", uploadResult.File.ParentID, folderResult.Folder.ID)
	}

	rootListRequest := newAuthenticatedRequest(http.MethodGet, "/files", nil, token)
	rootListResponse := httptest.NewRecorder()
	router.ServeHTTP(rootListResponse, rootListRequest)
	if rootListResponse.Code != http.StatusOK {
		t.Fatalf("root list status = %d, want %d", rootListResponse.Code, http.StatusOK)
	}

	var rootList struct {
		Files []UserFile `json:"files"`
	}
	if err := json.Unmarshal(rootListResponse.Body.Bytes(), &rootList); err != nil {
		t.Fatalf("decode root file list: %v", err)
	}
	if len(rootList.Files) != 0 {
		t.Fatalf("root files = %#v, want no files", rootList.Files)
	}

	folderListRequest := newAuthenticatedRequest(
		http.MethodGet,
		"/files?parent_id="+strconv.FormatInt(folderResult.Folder.ID, 10),
		nil,
		token,
	)
	folderListResponse := httptest.NewRecorder()
	router.ServeHTTP(folderListResponse, folderListRequest)
	if folderListResponse.Code != http.StatusOK {
		t.Fatalf("folder list status = %d, want %d", folderListResponse.Code, http.StatusOK)
	}

	var folderList struct {
		Files []UserFile `json:"files"`
	}
	if err := json.Unmarshal(folderListResponse.Body.Bytes(), &folderList); err != nil {
		t.Fatalf("decode folder file list: %v", err)
	}
	if len(folderList.Files) != 1 || folderList.Files[0].ID != uploadResult.File.ID {
		t.Fatalf("folder files = %#v, want report.txt", folderList.Files)
	}

	invalidListRequest := newAuthenticatedRequest(http.MethodGet, "/files?parent_id=0", nil, token)
	invalidListResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidListResponse, invalidListRequest)
	if invalidListResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid list status = %d, want %d", invalidListResponse.Code, http.StatusBadRequest)
	}

	missingListRequest := newAuthenticatedRequest(http.MethodGet, "/files?parent_id=999", nil, token)
	missingListResponse := httptest.NewRecorder()
	router.ServeHTTP(missingListResponse, missingListRequest)
	if missingListResponse.Code != http.StatusNotFound {
		t.Fatalf("missing folder list status = %d, want %d", missingListResponse.Code, http.StatusNotFound)
	}

	var invalidBody bytes.Buffer
	invalidWriter := multipart.NewWriter(&invalidBody)
	// 请求格式正确，但目录 ID 非法，验证处理器会在保存文件前拒绝它。
	if err := invalidWriter.WriteField("parent_id", "0"); err != nil {
		t.Fatalf("write invalid parent ID: %v", err)
	}
	invalidPart, err := invalidWriter.CreateFormFile("file", "invalid.txt")
	if err != nil {
		t.Fatalf("create invalid multipart file: %v", err)
	}
	if _, err := invalidPart.Write([]byte("invalid parent")); err != nil {
		t.Fatalf("write invalid multipart file: %v", err)
	}
	if err := invalidWriter.Close(); err != nil {
		t.Fatalf("close invalid multipart writer: %v", err)
	}

	invalidRequest := newAuthenticatedRequest(http.MethodPost, "/files", &invalidBody, token)
	invalidRequest.Header.Set("Content-Type", invalidWriter.FormDataContentType())
	invalidResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid upload status = %d, want %d", invalidResponse.Code, http.StatusBadRequest)
	}
}

func TestFileHandlerMovesActiveFile(t *testing.T) {
	router, token := newTestFileRouter(t)

	folderBody, err := json.Marshal(createFolderRequest{Name: "documents"})
	if err != nil {
		t.Fatalf("encode folder request: %v", err)
	}
	folderRequest := newAuthenticatedRequest(http.MethodPost, "/folders", bytes.NewReader(folderBody), token)
	folderRequest.Header.Set("Content-Type", "application/json")
	folderResponse := httptest.NewRecorder()
	router.ServeHTTP(folderResponse, folderRequest)
	if folderResponse.Code != http.StatusCreated {
		t.Fatalf("create folder status = %d, want %d: %s", folderResponse.Code, http.StatusCreated, folderResponse.Body.String())
	}

	var folderResult struct {
		Folder Folder `json:"folder"`
	}
	if err := json.Unmarshal(folderResponse.Body.Bytes(), &folderResult); err != nil {
		t.Fatalf("decode folder response: %v", err)
	}

	var uploadBody bytes.Buffer
	writer := multipart.NewWriter(&uploadBody)
	part, err := writer.CreateFormFile("file", "report.txt")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write([]byte("content")); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	uploadRequest := newAuthenticatedRequest(http.MethodPost, "/files", &uploadBody, token)
	uploadRequest.Header.Set("Content-Type", writer.FormDataContentType())
	uploadResponse := httptest.NewRecorder()
	router.ServeHTTP(uploadResponse, uploadRequest)
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d: %s", uploadResponse.Code, http.StatusCreated, uploadResponse.Body.String())
	}

	var uploadResult struct {
		File UserFile `json:"file"`
	}
	if err := json.Unmarshal(uploadResponse.Body.Bytes(), &uploadResult); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}

	moveBody, err := json.Marshal(moveFileRequest{ParentID: &folderResult.Folder.ID})
	if err != nil {
		t.Fatalf("encode move request: %v", err)
	}
	moveRequest := newAuthenticatedRequest(http.MethodPatch, "/files/"+strconv.FormatInt(uploadResult.File.ID, 10)+"/move", bytes.NewReader(moveBody), token)
	moveRequest.Header.Set("Content-Type", "application/json")
	moveResponse := httptest.NewRecorder()
	router.ServeHTTP(moveResponse, moveRequest)
	if moveResponse.Code != http.StatusOK {
		t.Fatalf("move status = %d, want %d: %s", moveResponse.Code, http.StatusOK, moveResponse.Body.String())
	}

	var moveResult struct {
		File UserFile `json:"file"`
	}
	if err := json.Unmarshal(moveResponse.Body.Bytes(), &moveResult); err != nil {
		t.Fatalf("decode move response: %v", err)
	}
	if moveResult.File.ParentID == nil || *moveResult.File.ParentID != folderResult.Folder.ID {
		t.Fatalf("moved parent ID = %v, want %d", moveResult.File.ParentID, folderResult.Folder.ID)
	}

	rootRequest := newAuthenticatedRequest(http.MethodPatch, "/files/"+strconv.FormatInt(uploadResult.File.ID, 10)+"/move", bytes.NewReader([]byte(`{}`)), token)
	rootRequest.Header.Set("Content-Type", "application/json")
	rootResponse := httptest.NewRecorder()
	router.ServeHTTP(rootResponse, rootRequest)
	if rootResponse.Code != http.StatusOK {
		t.Fatalf("move to root status = %d, want %d: %s", rootResponse.Code, http.StatusOK, rootResponse.Body.String())
	}

	invalidRequest := newAuthenticatedRequest(http.MethodPatch, "/files/"+strconv.FormatInt(uploadResult.File.ID, 10)+"/move", bytes.NewReader([]byte(`{"parent_id":0}`)), token)
	invalidRequest.Header.Set("Content-Type", "application/json")
	invalidResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid parent status = %d, want %d", invalidResponse.Code, http.StatusBadRequest)
	}

	missingFolderRequest := newAuthenticatedRequest(http.MethodPatch, "/files/"+strconv.FormatInt(uploadResult.File.ID, 10)+"/move", bytes.NewReader([]byte(`{"parent_id":999}`)), token)
	missingFolderRequest.Header.Set("Content-Type", "application/json")
	missingFolderResponse := httptest.NewRecorder()
	router.ServeHTTP(missingFolderResponse, missingFolderRequest)
	if missingFolderResponse.Code != http.StatusNotFound {
		t.Fatalf("missing folder status = %d, want %d", missingFolderResponse.Code, http.StatusNotFound)
	}

	deleteRequest := newAuthenticatedRequest(http.MethodDelete, "/files/"+strconv.FormatInt(uploadResult.File.ID, 10), nil, token)
	deleteResponse := httptest.NewRecorder()
	router.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d", deleteResponse.Code, http.StatusOK)
	}

	deletedMoveRequest := newAuthenticatedRequest(http.MethodPatch, "/files/"+strconv.FormatInt(uploadResult.File.ID, 10)+"/move", bytes.NewReader([]byte(`{}`)), token)
	deletedMoveRequest.Header.Set("Content-Type", "application/json")
	deletedMoveResponse := httptest.NewRecorder()
	router.ServeHTTP(deletedMoveResponse, deletedMoveRequest)
	if deletedMoveResponse.Code != http.StatusNotFound {
		t.Fatalf("move deleted file status = %d, want %d", deletedMoveResponse.Code, http.StatusNotFound)
	}
}

func TestFileHandlerRenamesActiveFile(t *testing.T) {
	router, token := newTestFileRouter(t)

	var uploadBody bytes.Buffer
	writer := multipart.NewWriter(&uploadBody)
	part, err := writer.CreateFormFile("file", "draft.txt")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write([]byte("content")); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	uploadRequest := newAuthenticatedRequest(http.MethodPost, "/files", &uploadBody, token)
	uploadRequest.Header.Set("Content-Type", writer.FormDataContentType())
	uploadResponse := httptest.NewRecorder()
	router.ServeHTTP(uploadResponse, uploadRequest)
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d: %s", uploadResponse.Code, http.StatusCreated, uploadResponse.Body.String())
	}

	var uploadResult struct {
		File UserFile `json:"file"`
	}
	if err := json.Unmarshal(uploadResponse.Body.Bytes(), &uploadResult); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}

	renameBody, err := json.Marshal(renameFileRequest{OriginalName: "final.txt"})
	if err != nil {
		t.Fatalf("encode rename request: %v", err)
	}
	renameRequest := newAuthenticatedRequest(http.MethodPatch, "/files/"+strconv.FormatInt(uploadResult.File.ID, 10)+"/rename", bytes.NewReader(renameBody), token)
	renameRequest.Header.Set("Content-Type", "application/json")
	renameResponse := httptest.NewRecorder()
	router.ServeHTTP(renameResponse, renameRequest)
	if renameResponse.Code != http.StatusOK {
		t.Fatalf("rename status = %d, want %d: %s", renameResponse.Code, http.StatusOK, renameResponse.Body.String())
	}

	var renameResult struct {
		File UserFile `json:"file"`
	}
	if err := json.Unmarshal(renameResponse.Body.Bytes(), &renameResult); err != nil {
		t.Fatalf("decode rename response: %v", err)
	}
	if renameResult.File.OriginalName != "final.txt" {
		t.Fatalf("renamed file = %#v, want final.txt", renameResult.File)
	}
	if renameResult.File.StoragePath != uploadResult.File.StoragePath {
		t.Fatalf("storage path = %q, want unchanged %q", renameResult.File.StoragePath, uploadResult.File.StoragePath)
	}

	emptyRequest := newAuthenticatedRequest(http.MethodPatch, "/files/"+strconv.FormatInt(uploadResult.File.ID, 10)+"/rename", bytes.NewReader([]byte(`{"original_name":"   "}`)), token)
	emptyRequest.Header.Set("Content-Type", "application/json")
	emptyResponse := httptest.NewRecorder()
	router.ServeHTTP(emptyResponse, emptyRequest)
	if emptyResponse.Code != http.StatusBadRequest {
		t.Fatalf("empty name status = %d, want %d", emptyResponse.Code, http.StatusBadRequest)
	}

	deleteRequest := newAuthenticatedRequest(http.MethodDelete, "/files/"+strconv.FormatInt(uploadResult.File.ID, 10), nil, token)
	deleteResponse := httptest.NewRecorder()
	router.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d", deleteResponse.Code, http.StatusOK)
	}

	deletedRequest := newAuthenticatedRequest(http.MethodPatch, "/files/"+strconv.FormatInt(uploadResult.File.ID, 10)+"/rename", bytes.NewReader(renameBody), token)
	deletedRequest.Header.Set("Content-Type", "application/json")
	deletedResponse := httptest.NewRecorder()
	router.ServeHTTP(deletedResponse, deletedRequest)
	if deletedResponse.Code != http.StatusNotFound {
		t.Fatalf("rename deleted file status = %d, want %d", deletedResponse.Code, http.StatusNotFound)
	}
}

func TestFileHandlerRenamesFolder(t *testing.T) {
	router, token := newTestFileRouter(t)

	createFolder := func(name string) Folder {
		body, err := json.Marshal(createFolderRequest{Name: name})
		if err != nil {
			t.Fatalf("encode folder request: %v", err)
		}
		request := newAuthenticatedRequest(http.MethodPost, "/folders", bytes.NewReader(body), token)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("create folder status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
		}

		var result struct {
			Folder Folder `json:"folder"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode folder response: %v", err)
		}
		return result.Folder
	}

	documents := createFolder("documents")
	_ = createFolder("music")

	renameBody, err := json.Marshal(renameFolderRequest{Name: "work"})
	if err != nil {
		t.Fatalf("encode rename request: %v", err)
	}
	renameRequest := newAuthenticatedRequest(http.MethodPatch, "/folders/"+strconv.FormatInt(documents.ID, 10)+"/rename", bytes.NewReader(renameBody), token)
	renameRequest.Header.Set("Content-Type", "application/json")
	renameResponse := httptest.NewRecorder()
	router.ServeHTTP(renameResponse, renameRequest)
	if renameResponse.Code != http.StatusOK {
		t.Fatalf("rename status = %d, want %d: %s", renameResponse.Code, http.StatusOK, renameResponse.Body.String())
	}

	var renameResult struct {
		Folder Folder `json:"folder"`
	}
	if err := json.Unmarshal(renameResponse.Body.Bytes(), &renameResult); err != nil {
		t.Fatalf("decode rename response: %v", err)
	}
	if renameResult.Folder.Name != "work" {
		t.Fatalf("renamed folder = %#v, want work", renameResult.Folder)
	}

	emptyRequest := newAuthenticatedRequest(http.MethodPatch, "/folders/"+strconv.FormatInt(documents.ID, 10)+"/rename", bytes.NewReader([]byte(`{"name":"   "}`)), token)
	emptyRequest.Header.Set("Content-Type", "application/json")
	emptyResponse := httptest.NewRecorder()
	router.ServeHTTP(emptyResponse, emptyRequest)
	if emptyResponse.Code != http.StatusBadRequest {
		t.Fatalf("empty name status = %d, want %d", emptyResponse.Code, http.StatusBadRequest)
	}

	duplicateRequest := newAuthenticatedRequest(http.MethodPatch, "/folders/"+strconv.FormatInt(documents.ID, 10)+"/rename", bytes.NewReader([]byte(`{"name":"music"}`)), token)
	duplicateRequest.Header.Set("Content-Type", "application/json")
	duplicateResponse := httptest.NewRecorder()
	router.ServeHTTP(duplicateResponse, duplicateRequest)
	if duplicateResponse.Code != http.StatusConflict {
		t.Fatalf("duplicate name status = %d, want %d", duplicateResponse.Code, http.StatusConflict)
	}

	missingRequest := newAuthenticatedRequest(http.MethodPatch, "/folders/999/rename", bytes.NewReader(renameBody), token)
	missingRequest.Header.Set("Content-Type", "application/json")
	missingResponse := httptest.NewRecorder()
	router.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing folder status = %d, want %d", missingResponse.Code, http.StatusNotFound)
	}
}

func TestFileHandlerMovesFolder(t *testing.T) {
	router, token := newTestFileRouter(t)

	createFolder := func(parentID *int64, name string) Folder {
		body, err := json.Marshal(createFolderRequest{ParentID: parentID, Name: name})
		if err != nil {
			t.Fatalf("encode folder request: %v", err)
		}
		request := newAuthenticatedRequest(http.MethodPost, "/folders", bytes.NewReader(body), token)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("create folder status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
		}

		var result struct {
			Folder Folder `json:"folder"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode folder response: %v", err)
		}
		return result.Folder
	}

	documents := createFolder(nil, "documents")
	photos := createFolder(&documents.ID, "photos")
	archive := createFolder(nil, "archive")

	moveBody, err := json.Marshal(moveFolderRequest{ParentID: &archive.ID})
	if err != nil {
		t.Fatalf("encode move request: %v", err)
	}
	moveRequest := newAuthenticatedRequest(http.MethodPatch, "/folders/"+strconv.FormatInt(documents.ID, 10)+"/move", bytes.NewReader(moveBody), token)
	moveRequest.Header.Set("Content-Type", "application/json")
	moveResponse := httptest.NewRecorder()
	router.ServeHTTP(moveResponse, moveRequest)
	if moveResponse.Code != http.StatusOK {
		t.Fatalf("move status = %d, want %d: %s", moveResponse.Code, http.StatusOK, moveResponse.Body.String())
	}

	var moveResult struct {
		Folder Folder `json:"folder"`
	}
	if err := json.Unmarshal(moveResponse.Body.Bytes(), &moveResult); err != nil {
		t.Fatalf("decode move response: %v", err)
	}
	if moveResult.Folder.ParentID == nil || *moveResult.Folder.ParentID != archive.ID {
		t.Fatalf("moved parent ID = %v, want %d", moveResult.Folder.ParentID, archive.ID)
	}

	cycleBody, err := json.Marshal(moveFolderRequest{ParentID: &photos.ID})
	if err != nil {
		t.Fatalf("encode cycle request: %v", err)
	}
	cycleRequest := newAuthenticatedRequest(http.MethodPatch, "/folders/"+strconv.FormatInt(documents.ID, 10)+"/move", bytes.NewReader(cycleBody), token)
	cycleRequest.Header.Set("Content-Type", "application/json")
	cycleResponse := httptest.NewRecorder()
	router.ServeHTTP(cycleResponse, cycleRequest)
	if cycleResponse.Code != http.StatusConflict {
		t.Fatalf("cycle move status = %d, want %d: %s", cycleResponse.Code, http.StatusConflict, cycleResponse.Body.String())
	}

	invalidRequest := newAuthenticatedRequest(http.MethodPatch, "/folders/"+strconv.FormatInt(documents.ID, 10)+"/move", bytes.NewReader([]byte(`{"parent_id":0}`)), token)
	invalidRequest.Header.Set("Content-Type", "application/json")
	invalidResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid parent status = %d, want %d", invalidResponse.Code, http.StatusBadRequest)
	}
}

func TestFileHandlerDeletesFolder(t *testing.T) {
	router, token := newTestFileRouter(t)

	createFolder := func(parentID *int64, name string) Folder {
		body, err := json.Marshal(createFolderRequest{ParentID: parentID, Name: name})
		if err != nil {
			t.Fatalf("encode folder request: %v", err)
		}
		request := newAuthenticatedRequest(http.MethodPost, "/folders", bytes.NewReader(body), token)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("create folder status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
		}

		var result struct {
			Folder Folder `json:"folder"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode folder response: %v", err)
		}
		return result.Folder
	}

	empty := createFolder(nil, "empty")
	deleteRequest := newAuthenticatedRequest(http.MethodDelete, "/folders/"+strconv.FormatInt(empty.ID, 10), nil, token)
	deleteResponse := httptest.NewRecorder()
	router.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d: %s", deleteResponse.Code, http.StatusNoContent, deleteResponse.Body.String())
	}

	missingRequest := newAuthenticatedRequest(http.MethodDelete, "/folders/"+strconv.FormatInt(empty.ID, 10), nil, token)
	missingResponse := httptest.NewRecorder()
	router.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing folder status = %d, want %d", missingResponse.Code, http.StatusNotFound)
	}

	parent := createFolder(nil, "parent")
	_ = createFolder(&parent.ID, "child")
	nonEmptyRequest := newAuthenticatedRequest(http.MethodDelete, "/folders/"+strconv.FormatInt(parent.ID, 10), nil, token)
	nonEmptyResponse := httptest.NewRecorder()
	router.ServeHTTP(nonEmptyResponse, nonEmptyRequest)
	if nonEmptyResponse.Code != http.StatusConflict {
		t.Fatalf("non-empty folder status = %d, want %d: %s", nonEmptyResponse.Code, http.StatusConflict, nonEmptyResponse.Body.String())
	}

	invalidRequest := newAuthenticatedRequest(http.MethodDelete, "/folders/0", nil, token)
	invalidResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid folder status = %d, want %d", invalidResponse.Code, http.StatusBadRequest)
	}
}

func TestFileHandlerGetsStorageUsage(t *testing.T) {
	router, token := newTestFileRouter(t)

	var uploadBody bytes.Buffer
	writer := multipart.NewWriter(&uploadBody)
	part, err := writer.CreateFormFile("file", "usage.txt")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write([]byte("hello")); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	uploadRequest := newAuthenticatedRequest(http.MethodPost, "/files", &uploadBody, token)
	uploadRequest.Header.Set("Content-Type", writer.FormDataContentType())
	uploadResponse := httptest.NewRecorder()
	router.ServeHTTP(uploadResponse, uploadRequest)
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d: %s", uploadResponse.Code, http.StatusCreated, uploadResponse.Body.String())
	}

	usageRequest := newAuthenticatedRequest(http.MethodGet, "/storage", nil, token)
	usageResponse := httptest.NewRecorder()
	router.ServeHTTP(usageResponse, usageRequest)
	if usageResponse.Code != http.StatusOK {
		t.Fatalf("storage status = %d, want %d: %s", usageResponse.Code, http.StatusOK, usageResponse.Body.String())
	}

	var result struct {
		Storage StorageUsage `json:"storage"`
	}
	if err := json.Unmarshal(usageResponse.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode storage response: %v", err)
	}
	if result.Storage.UsedBytes != 5 {
		t.Fatalf("used bytes = %d, want 5", result.Storage.UsedBytes)
	}
	if result.Storage.QuotaBytes != testStorageQuotaBytes {
		t.Fatalf("quota bytes = %d, want %d", result.Storage.QuotaBytes, testStorageQuotaBytes)
	}
}

func TestFileHandlerRejectsUploadOverQuota(t *testing.T) {
	router, token := newTestFileRouterWithQuota(t, 5)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "too-large.txt")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write([]byte("123456")); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := newAuthenticatedRequest(http.MethodPost, "/files", &body, token)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("upload over quota status = %d, want %d: %s", response.Code, http.StatusConflict, response.Body.String())
	}
}

package file

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

type createFolderRequest struct {
	ParentID *int64 `json:"parent_id"`
	Name     string `json:"name"`
}

type moveFileRequest struct {
	ParentID *int64 `json:"parent_id"`
}

type instantUploadRequest struct {
	ParentID     *int64 `json:"parent_id"`
	OriginalName string `json:"original_name"`
	FileHash     string `json:"file_hash"`
}

type renameFileRequest struct {
	OriginalName string `json:"original_name"`
}

type renameFolderRequest struct {
	Name string `json:"name"`
}

type moveFolderRequest struct {
	ParentID *int64 `json:"parent_id"`
}

func (h *Handler) Search(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}
	filter := SearchFilter{Query: strings.TrimSpace(c.Query("q")), Kind: strings.TrimSpace(c.Query("kind"))}
	if filter.Kind != "" && filter.Kind != "image" && filter.Kind != "video" && filter.Kind != "other" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid search kind"})
		return
	}
	if since := c.Query("since"); since != "" {
		parsed, err := time.Parse(time.RFC3339, since)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid search time"})
			return
		}
		filter.CreatedAfter = parsed
	}
	if before := c.Query("before"); before != "" {
		parsed, err := time.Parse(time.RFC3339, before)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid search time"})
			return
		}
		filter.CreatedBefore = parsed
	}
	files, err := h.service.SearchActive(userID, filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search files"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

func (h *Handler) Upload(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	header, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}

	src, err := header.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to open uploaded file"})
		return
	}
	defer src.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	var parentID *int64

	if value := c.PostForm("parent_id"); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid parent id"})
			return
		}

		parentID = &id
	}

	savedFile, err := h.service.UploadIntoFolder(
		userID,
		parentID,
		header.Filename,
		contentType,
		src,
	)
	if errors.Is(err, ErrOriginalNameRequired) || errors.Is(err, ErrContentRequired) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFolderNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrStorageQuotaExceeded) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upload file"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"file": savedFile,
	})
}

func (h *Handler) ListActive(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	var parentID *int64

	if value, exists := c.GetQuery("parent_id"); exists {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid parent id"})
			return
		}

		parentID = &id
	}

	files, err := h.service.ListActiveInFolder(userID, parentID)
	if errors.Is(err, ErrFolderNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list active files"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"files": files,
	})
}

func (h *Handler) ListDeleted(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	files, err := h.service.ListDeleted(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list trash"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"files": files,
	})
}

func (h *Handler) Download(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	fileID, err := parseFileID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return
	}

	userFile, reader, err := h.service.OpenForDownload(userID, fileID)
	if errors.Is(err, ErrFileNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	if errors.Is(err, ErrFileScanIncomplete) {
		c.JSON(http.StatusLocked, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFileInfected) {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to open file"})
		return
	}
	defer reader.Close()

	c.Header("Content-Disposition", `attachment; filename="`+userFile.OriginalName+`"`)
	c.Header("Content-Type", userFile.ContentType)
	http.ServeContent(c.Writer, c.Request, userFile.OriginalName, userFile.CreatedAt, reader)
}

func (h *Handler) DownloadThumbnail(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	fileID, err := parseFileID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return
	}

	preview, reader, err := h.service.OpenThumbnailForDownload(userID, fileID)
	if errors.Is(err, ErrFilePreviewNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "file preview not found"})
		return
	}
	if errors.Is(err, ErrFileScanIncomplete) {
		c.JSON(http.StatusLocked, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFileInfected) {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to open preview file"})
		return
	}
	defer reader.Close()

	c.Header("Content-Disposition", `inline; filename="thumbnail.png"`)
	c.Header("Content-Type", preview.ContentType)
	http.ServeContent(c.Writer, c.Request, "thumbnail.png", preview.CreatedAt, reader)
}

func (h *Handler) Preview(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	fileID, err := parseFileID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return
	}

	file, reader, err := h.service.OpenInlinePreview(userID, fileID)
	if errors.Is(err, ErrFileNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	if errors.Is(err, ErrInlinePreviewUnsupported) {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFileScanIncomplete) {
		c.JSON(http.StatusLocked, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFileInfected) {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to open preview file"})
		return
	}
	defer reader.Close()

	c.Header("Content-Disposition", `inline; filename="`+file.OriginalName+`"`)
	c.Header("Content-Type", file.ContentType)
	http.ServeContent(c.Writer, c.Request, file.OriginalName, file.CreatedAt, reader)
}

func (h *Handler) SoftDelete(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	fileID, err := parseFileID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return
	}

	keepShares := c.Query("keep_shares") == "true"
	err = h.service.SoftDeleteWithShareOption(userID, fileID, keepShares)
	if errors.Is(err, ErrFileNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "file moved to trash",
	})
}

func (h *Handler) PermanentlyDelete(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	fileID, err := parseFileID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return
	}

	if err := h.service.PermanentlyDelete(userID, fileID); err != nil {
		if errors.Is(err, ErrFileNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to permanently delete file"})
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) Restore(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	fileID, err := parseFileID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return
	}

	err = h.service.Restore(userID, fileID)
	if errors.Is(err, ErrFileNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to restore file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "file restored",
	})
}

func (h *Handler) EnqueueVerification(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	fileID, err := parseFileID(c)
	if err != nil || fileID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return
	}

	job, err := h.service.EnqueueFileVerification(userID, fileID)
	if errors.Is(err, ErrFileNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	if errors.Is(err, ErrJobQueueUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to enqueue file verification"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"job": job,
	})
}

func parseFileID(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}

func (h *Handler) InstantUpload(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	var req instantUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}

	savedFile, err := h.service.InstantUploadIntoFolder(
		userID,
		req.ParentID,
		req.OriginalName,
		req.FileHash,
	)
	if errors.Is(err, ErrOriginalNameRequired) || errors.Is(err, ErrFileHashRequired) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFolderNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFileObjectNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "file object not found"})
		return
	}
	if errors.Is(err, ErrStorageQuotaExceeded) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create instant upload"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"file": savedFile,
	})
}

func (h *Handler) CreateFolder(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	var req createFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}

	folder, err := h.service.CreateFolder(userID, req.ParentID, req.Name)
	if errors.Is(err, ErrFolderNameRequired) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFolderNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFolderAlreadyExists) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create folder"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"folder": folder})
}

func (h *Handler) ListFolders(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	var parentID *int64

	if value, exists := c.GetQuery("parent_id"); exists {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid parent id"})
			return
		}

		parentID = &id
	}

	folders, err := h.service.ListFolders(userID, parentID)
	if errors.Is(err, ErrFolderNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list folders"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"folders": folders})
}

func (h *Handler) MoveActive(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	fileID, err := parseFileID(c)
	if err != nil || fileID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return
	}

	var req moveFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}
	if req.ParentID != nil && *req.ParentID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid parent id"})
		return
	}

	movedFile, err := h.service.MoveActive(userID, fileID, req.ParentID)
	if errors.Is(err, ErrFileNotFound) || errors.Is(err, ErrFolderNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to move file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"file": movedFile,
	})
}

func (h *Handler) RenameActive(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	fileID, err := parseFileID(c)
	if err != nil || fileID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return
	}

	var req renameFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}

	renamedFile, err := h.service.RenameActive(
		userID,
		fileID,
		req.OriginalName,
	)
	if errors.Is(err, ErrOriginalNameRequired) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFileNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rename file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"file": renamedFile,
	})
}

func (h *Handler) RenameFolder(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	folderID, err := parseFileID(c)
	if err != nil || folderID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid folder id"})
		return
	}

	var req renameFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}

	folder, err := h.service.RenameFolder(userID, folderID, req.Name)
	if errors.Is(err, ErrFolderNameRequired) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFolderNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFolderAlreadyExists) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rename folder"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"folder": folder,
	})
}

func (h *Handler) MoveFolder(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	folderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || folderID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid folder id"})
		return
	}

	var req moveFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}
	if req.ParentID != nil && *req.ParentID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid parent id"})
		return
	}

	folder, err := h.service.MoveFolder(userID, folderID, req.ParentID)
	if errors.Is(err, ErrFolderNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFolderAlreadyExists) || errors.Is(err, ErrFolderMoveCycle) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to move folder"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"folder": folder,
	})
}

func (h *Handler) DeleteFolder(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	folderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || folderID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid folder id"})
		return
	}

	err = h.service.DeleteFolder(userID, folderID)
	if errors.Is(err, ErrFolderNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrFolderNotEmpty) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete folder"})
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) GetStorageUsage(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	usage, err := h.service.GetStorageUsage(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get storage usage"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"storage": usage,
	})
}

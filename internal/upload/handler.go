package upload

import (
	"errors"
	"net/http"
	"strconv"

	filemodule "github.com/SeanidHau/CloudBox/internal/file"
	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}
type initRequest struct {
	ParentID     *int64 `json:"parent_id"`
	OriginalName string `json:"original_name"`
	ContentType  string `json:"content_type"`
	FileSize     int64  `json:"file_size"`
	ChunkSize    int64  `json:"chunk_size"`
	FileHash     string `json:"file_hash"`
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Init(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	var req initRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}

	if req.ParentID != nil && *req.ParentID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid parent id"})
		return
	}

	task, err := h.service.InitInFolder(
		userID,
		req.ParentID,
		req.OriginalName,
		req.ContentType,
		req.FileSize,
		req.ChunkSize,
		req.FileHash,
	)
	if errors.Is(err, ErrOriginalNameRequired) ||
		errors.Is(err, ErrFileSizeInvalid) ||
		errors.Is(err, ErrChunkSizeInvalid) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, filemodule.ErrFolderNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, filemodule.ErrStorageQuotaExceeded) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to initialize upload"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"upload": task,
	})
}

func (h *Handler) UploadChunk(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	chunkNumber, err := strconv.ParseInt(c.Param("number"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chunk number"})
		return
	}

	chunk, err := h.service.UploadChunk(
		userID,
		c.Param("id"),
		chunkNumber,
		c.Request.Body,
	)
	if errors.Is(err, ErrTaskNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrChunkNumberInvalid) ||
		errors.Is(err, ErrChunkSizeMismatch) ||
		errors.Is(err, ErrChunkContentRequired) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrTaskNotUploading) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upload chunk"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"chunk": chunk})
}

func (h *Handler) GetStatus(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	status, err := h.service.GetStatus(userID, c.Param("id"))
	if errors.Is(err, ErrTaskNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get upload status"})
		return
	}

	c.JSON(http.StatusOK, status)
}

func (h *Handler) Complete(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	userFile, err := h.service.Complete(userID, c.Param("id"))
	if errors.Is(err, ErrTaskNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": ErrTaskNotFound.Error()})
		return
	}
	if errors.Is(err, ErrTaskNotUploading) ||
		errors.Is(err, ErrChunksIncomplete) ||
		errors.Is(err, ErrChunkHashMismatch) ||
		errors.Is(err, ErrFileHashMismatch) ||
		errors.Is(err, filemodule.ErrStorageQuotaExceeded) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to complete upload"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"file": userFile})
}

func (h *Handler) Cancel(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	err := h.service.Cancel(userID, c.Param("id"))
	if errors.Is(err, ErrTaskNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrTaskNotUploading) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel upload"})
		return
	}

	c.Status(http.StatusNoContent)
}

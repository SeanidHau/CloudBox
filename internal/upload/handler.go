package upload

import (
	"errors"
	"net/http"

	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}
type initRequest struct {
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

	task, err := h.service.Init(
		userID,
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
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to initialize upload"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"upload": task,
	})
}

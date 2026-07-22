package file

import (
	"errors"
	"mime"
	"net/http"
	"strconv"

	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(router gin.IRouter) {
	router.POST("", h.upload)
	router.GET("", h.list)
	router.GET("/trash", h.listTrash)
	router.GET("/:id/download", h.download)
	router.DELETE("/:id", h.delete)
	router.POST("/:id/restore", h.restore)
}

func (h *Handler) upload(c *gin.Context) {
	userID := currentUserID(c)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file field"})
		return
	}

	uploadedFile, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "open upload failed"})
		return
	}
	defer uploadedFile.Close()

	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	userFile, err := h.service.Upload(c.Request.Context(), CreateFileParams{
		UserID:       userID,
		OriginalName: fileHeader.Filename,
		Size:         fileHeader.Size,
		ContentType:  contentType,
	}, uploadedFile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "upload file failed"})
		return
	}

	c.JSON(http.StatusCreated, userFile)
}

func (h *Handler) list(c *gin.Context) {
	files, err := h.service.ListActive(c.Request.Context(), currentUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list files failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"files": files})
}

func (h *Handler) listTrash(c *gin.Context) {
	files, err := h.service.ListTrash(c.Request.Context(), currentUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list trash failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"files": files})
}

func (h *Handler) download(c *gin.Context) {
	fileID, ok := parseFileID(c)
	if !ok {
		return
	}

	userFile, reader, err := h.service.OpenDownload(c.Request.Context(), currentUserID(c), fileID)
	if err != nil {
		writeFileError(c, err)
		return
	}
	defer reader.Close()

	disposition := mime.FormatMediaType("attachment", map[string]string{
		"filename": userFile.OriginalName,
	})
	headers := map[string]string{
		"Content-Disposition": disposition,
	}

	c.DataFromReader(http.StatusOK, userFile.Size, userFile.ContentType, reader, headers)
}

func (h *Handler) delete(c *gin.Context) {
	fileID, ok := parseFileID(c)
	if !ok {
		return
	}

	if err := h.service.Delete(c.Request.Context(), currentUserID(c), fileID); err != nil {
		writeFileError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) restore(c *gin.Context) {
	fileID, ok := parseFileID(c)
	if !ok {
		return
	}

	if err := h.service.Restore(c.Request.Context(), currentUserID(c), fileID); err != nil {
		writeFileError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func currentUserID(c *gin.Context) int64 {
	value, ok := c.Get(middleware.UserIDKey)
	if !ok {
		return 0
	}

	userID, ok := value.(int64)
	if !ok {
		return 0
	}
	return userID
}

func parseFileID(c *gin.Context) (int64, bool) {
	fileID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || fileID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return 0, false
	}
	return fileID, true
}

func writeFileError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrFileNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
	case errors.Is(err, ErrFileDeleted):
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "file operation failed"})
	}
}

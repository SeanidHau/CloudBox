package file

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
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

	savedFile, err := h.service.Upload(userID, header.Filename, contentType, src)
	if errors.Is(err, ErrOriginalNameRequired) || errors.Is(err, ErrContentRequired) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
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

	files, err := h.service.ListActive(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list files"})
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

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to open file"})
		return
	}
	defer reader.Close()

	c.Header("Content-Disposition", `attachment; filename="`+userFile.OriginalName+`"`)
	c.Header("Content-Type", userFile.ContentType)
	http.ServeContent(c.Writer, c.Request, userFile.OriginalName, userFile.CreatedAt, reader)
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

	err = h.service.SoftDelete(userID, fileID)
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

func parseFileID(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}

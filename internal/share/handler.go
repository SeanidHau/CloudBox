package share

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	filemodule "github.com/SeanidHau/CloudBox/internal/file"
	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

type createShareRequest struct {
	Password     string     `json:"password"`
	ExpiresAt    *time.Time `json:"expires_at"`
	MaxDownloads *int64     `json:"max_downloads"`
}

type saveShareRequest struct {
	Password string `json:"password"`
	ParentID *int64 `json:"parent_id"`
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

func (h *Handler) Create(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	fileID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || fileID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return
	}

	var req createShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}

	share, err := h.service.Create(
		userID,
		fileID,
		req.Password,
		req.ExpiresAt,
		req.MaxDownloads,
	)
	if errors.Is(err, ErrFileNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	if errors.Is(err, ErrShareExpirationInvalid) || errors.Is(err, ErrDownloadLimitInvalid) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create share"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"share": share})
}

func (h *Handler) Download(c *gin.Context) {
	file, reader, err := h.service.OpenForDownload(
		c.Param("token"),
		c.GetHeader("X-Share-Password"),
	)
	if errors.Is(err, ErrShareNotFound) || errors.Is(err, ErrFileNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
		return
	}
	if errors.Is(err, ErrSharePasswordRequired) || errors.Is(err, ErrSharePasswordInvalid) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid share password"})
		return
	}
	if errors.Is(err, ErrShareExpired) || errors.Is(err, ErrDownloadLimitReached) {
		c.JSON(http.StatusGone, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrSharedFileUnavailable) {
		c.JSON(http.StatusLocked, gin.H{"error": "shared file is not available"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to open shared file"})
		return
	}
	defer reader.Close()

	c.Header("Content-disposition", `attachment; filename="`+file.OriginalName+`"`)
	c.Header("Content-Type", file.ContentType)

	http.ServeContent(c.Writer, c.Request, file.OriginalName, time.Time{}, reader)
}

func (h *Handler) Save(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	var req saveShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}

	file, err := h.service.SaveToUserFiles(
		userID,
		c.Param("token"),
		req.Password,
		req.ParentID,
	)

	switch {
	case errors.Is(err, ErrShareNotFound), errors.Is(err, ErrFileNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
	case errors.Is(err, ErrSharePasswordRequired), errors.Is(err, ErrSharePasswordInvalid):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid share password"})
	case errors.Is(err, ErrShareExpired), errors.Is(err, ErrDownloadLimitReached):
		c.JSON(http.StatusGone, gin.H{"error": err.Error()})
	case errors.Is(err, ErrSharedFileUnavailable):
		c.JSON(http.StatusLocked, gin.H{"error": "shared file is not available"})
	case errors.Is(err, filemodule.ErrFolderNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, filemodule.ErrStorageQuotaExceeded):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save shared file"})
	default:
		c.JSON(http.StatusCreated, gin.H{"file": file})
	}
}

func (h *Handler) List(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	shares, err := h.service.List(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list shares"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"shares": shares})
}

func (h *Handler) Revoke(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	if err := h.service.Revoke(userID, c.Param("token")); err != nil {
		if errors.Is(err, ErrShareNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke share"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "share revoked"})
}

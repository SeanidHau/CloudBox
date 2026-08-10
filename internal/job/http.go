package job

import (
	"errors"
	"net/http"

	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
)

type HTTPHandler struct {
	service *Service
}

func NewHTTPHandler(service *Service) *HTTPHandler {
	return &HTTPHandler{
		service: service,
	}
}

func (h *HTTPHandler) Get(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}

	job, err := h.service.GetForUser(userID, c.Param("id"))
	if errors.Is(err, ErrInvalidJobID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrJobNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "background job not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get background job"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"job": job,
	})
}

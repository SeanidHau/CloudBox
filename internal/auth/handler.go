package auth

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

type authRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	InviteCode string `json:"invite_code"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type quotaRequest struct {
	StorageQuotaBytes int64 `json:"storage_quota_bytes"`
}
type statusRequest struct {
	Status string `json:"status"`
}

func (h *Handler) Register(c *gin.Context) {
	var req authRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}
	user, err := h.service.Register(strings.TrimSpace(req.Username), req.Password, strings.TrimSpace(req.InviteCode))
	if errors.Is(err, ErrUsernameRequired) || errors.Is(err, ErrPasswordRequired) || errors.Is(err, ErrInviteCodeRequired) || errors.Is(err, ErrInvalidInvitation) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register user"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": user})
}

func (h *Handler) Login(c *gin.Context) {
	var req authRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}
	token, user, err := h.service.Login(strings.TrimSpace(req.Username), req.Password)
	if errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrAccountDisabled) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to login"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "user": user})
}

func (h *Handler) Me(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}
	user, err := h.service.repo.FindByID(userID)
	if errors.Is(err, ErrUserNotFound) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get current user"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (h *Handler) ChangePassword(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}
	user, err := h.service.ChangeOwnPassword(userID, req.CurrentPassword, req.NewPassword)
	if errors.Is(err, ErrCurrentPasswordRequired) || errors.Is(err, ErrNewPasswordRequired) || errors.Is(err, ErrInvalidCredentials) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrUserNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to change password"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (h *Handler) ListUsers(c *gin.Context) {
	requesterID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}
	users, err := h.service.ListUsers(requesterID)
	if errors.Is(err, ErrAdminRequired) {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

func (h *Handler) SetUserQuota(c *gin.Context) {
	h.updateUser(c, func(requesterID, userID int64) (*User, error) {
		var req quotaRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			return nil, err
		}
		return h.service.SetUserQuota(requesterID, userID, req.StorageQuotaBytes)
	})
}

func (h *Handler) SetUserStatus(c *gin.Context) {
	h.updateUser(c, func(requesterID, userID int64) (*User, error) {
		var req statusRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			return nil, err
		}
		return h.service.SetUserStatus(requesterID, userID, strings.TrimSpace(req.Status))
	})
}

func (h *Handler) ResetPassword(c *gin.Context) {
	requesterID, userID, ok := adminTarget(c)
	if !ok {
		return
	}
	temporaryPassword, user, err := h.service.ResetPassword(requesterID, userID)
	if errors.Is(err, ErrAdminRequired) {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrUserNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reset password"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user, "temporary_password": temporaryPassword})
}

func (h *Handler) RevokeAllUserShares(c *gin.Context) {
	requesterID, userID, ok := adminTarget(c)
	if !ok {
		return
	}
	revoked, err := h.service.RevokeAllUserShares(requesterID, userID)
	if errors.Is(err, ErrAdminRequired) {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrUserNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke user shares"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": revoked})
}

func (h *Handler) CreateInvitation(c *gin.Context) {
	requesterID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}
	invitation, err := h.service.CreateInvitation(requesterID)
	if errors.Is(err, ErrAdminRequired) {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create invitation"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"invitation": invitation})
}

func (h *Handler) ListInvitations(c *gin.Context) {
	requesterID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return
	}
	invitations, err := h.service.ListInvitations(requesterID)
	if errors.Is(err, ErrAdminRequired) {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list invitations"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"invitations": invitations})
}

func (h *Handler) RevokeInvitation(c *gin.Context) {
	requesterID, invitationID, ok := adminTarget(c)
	if !ok {
		return
	}
	invitation, err := h.service.RevokeInvitation(requesterID, invitationID)
	if errors.Is(err, ErrAdminRequired) {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrInvitationNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke invitation"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"invitation": invitation})
}

func (h *Handler) updateUser(c *gin.Context, update func(requesterID, userID int64) (*User, error)) {
	requesterID, userID, ok := adminTarget(c)
	if !ok {
		return
	}
	user, err := update(requesterID, userID)
	if errors.Is(err, ErrAdminRequired) {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrInvalidQuota) || errors.Is(err, ErrCannotManageSelf) || errors.Is(err, ErrCannotDisableLastAdmin) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, ErrUserNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func adminTarget(c *gin.Context) (int64, int64, bool) {
	requesterID, ok := middleware.CurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
		return 0, 0, false
	}
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return 0, 0, false
	}
	return requesterID, userID, true
}

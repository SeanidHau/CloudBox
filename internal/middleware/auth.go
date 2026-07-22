package middleware

import (
	"net/http"
	"strings"

	"github.com/SeanidHau/CloudBox/internal/auth"
	"github.com/gin-gonic/gin"
)

const (
	UserIDKey   = "user_id"
	UsernameKey = "username"
)

func Auth(authService *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		tokenValue, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || strings.TrimSpace(tokenValue) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header"})
			return
		}

		claims, err := authService.ParseToken(strings.TrimSpace(tokenValue))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		c.Set(UserIDKey, claims.UserID)
		c.Set(UsernameKey, claims.Username)
		c.Next()
	}
}

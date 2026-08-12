package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const UserIDKey = "user_id"

const MustChangePasswordKey = "must_change_password"

type SessionValidator func(userID int64, sessionVersion int64) error

func Auth(jwtSecret string, validators ...SessionValidator) gin.HandlerFunc {
	var validateSession SessionValidator
	if len(validators) > 0 {
		validateSession = validators[0]
	}

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header"})
			c.Abort()
			return
		}

		tokenText := parts[1]

		token, err := jwt.Parse(tokenText, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims"})
			c.Abort()
			return
		}

		userIDFloat, ok := claims["user_id"].(float64)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
			c.Abort()
			return
		}
		sessionVersion, ok := claims["session_version"].(float64)
		if validateSession != nil && (!ok || validateSession(int64(userIDFloat), int64(sessionVersion)) != nil) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}
		mustChangePassword, _ := claims["must_change_password"].(bool)

		c.Set(UserIDKey, int64(userIDFloat))
		c.Set(MustChangePasswordKey, mustChangePassword)
		c.Next()
	}
}

func RequirePasswordChanged() gin.HandlerFunc {
	return func(c *gin.Context) {
		mustChange, _ := c.Get(MustChangePasswordKey)
		if required, _ := mustChange.(bool); required {
			c.JSON(http.StatusPreconditionRequired, gin.H{"error": "password change is required"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func CurrentUserID(c *gin.Context) (int64, bool) {
	value, exists := c.Get(UserIDKey)
	if !exists {
		return 0, false
	}

	userID, ok := value.(int64)
	return userID, ok
}

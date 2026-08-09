package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	RequestIDKey    = "request_id"
	RequestIDHeader = "X-Request-ID"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := uuid.NewString()

		c.Set(RequestIDKey, requestID)
		c.Header(RequestIDHeader, requestID)

		c.Next()
	}
}

func CurrentRequestID(c *gin.Context) (string, bool) {
	value, exists := c.Get(RequestIDKey)
	if !exists {
		return "", false
	}

	requestID, ok := value.(string)
	return requestID, ok
}

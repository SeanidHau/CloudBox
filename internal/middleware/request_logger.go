package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

func RequestLogger(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}

	return func(c *gin.Context) {
		startAt := time.Now()

		c.Next()

		requestID, _ := CurrentRequestID(c)
		attributes := []any{
			"request_id", requestID,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(startAt).Milliseconds(),
		}

		if userID, ok := CurrentUserID(c); ok {
			attributes = append(attributes, "user_id", userID)
		}

		logger.Info("HTTP request completed", attributes...)
	}
}

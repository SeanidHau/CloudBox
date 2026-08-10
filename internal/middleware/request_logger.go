package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
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

		spanContext := trace.SpanContextFromContext(c.Request.Context())
		if spanContext.IsValid() {
			attributes = append(
				attributes,
				"trace_id", spanContext.TraceID().String(),
				"span_id", spanContext.SpanID().String(),
			)
		}

		if userID, ok := CurrentUserID(c); ok {
			attributes = append(attributes, "user_id", userID)
		}

		logger.Info("HTTP request completed", attributes...)
	}
}

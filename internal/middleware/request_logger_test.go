package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

func TestRequestLoggerWritesRequestFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	router := gin.New()
	router.Use(RequestID(), RequestLogger(logger))
	router.GET("/files/:id", func(c *gin.Context) {
		c.Set(UserIDKey, int64(42))
		c.Status(http.StatusCreated)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/files/7", nil))

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode JSON log: %v; output: %s", err, output.String())
	}

	if entry["msg"] != "HTTP request completed" {
		t.Fatalf("message = %#v, want HTTP request completed", entry["msg"])
	}
	if entry["request_id"] != response.Header().Get(RequestIDHeader) {
		t.Fatalf("request ID = %#v, want response header %q", entry["request_id"], response.Header().Get(RequestIDHeader))
	}
	if entry["method"] != http.MethodGet {
		t.Fatalf("method = %#v, want %q", entry["method"], http.MethodGet)
	}
	if entry["path"] != "/files/7" {
		t.Fatalf("path = %#v, want /files/7", entry["path"])
	}
	if entry["status"] != float64(http.StatusCreated) {
		t.Fatalf("status = %#v, want %d", entry["status"], http.StatusCreated)
	}
	if entry["user_id"] != float64(42) {
		t.Fatalf("user ID = %#v, want 42", entry["user_id"])
	}
	if latency, ok := entry["latency_ms"].(float64); !ok || latency < 0 {
		t.Fatalf("latency_ms = %#v, want non-negative number", entry["latency_ms"])
	}
}

func TestRequestLoggerOmitsUserIDWhenUnauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	router := gin.New()
	router.Use(RequestID(), RequestLogger(logger))
	router.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode JSON log: %v; output: %s", err, output.String())
	}
	if _, exists := entry["user_id"]; exists {
		t.Fatalf("unauthenticated log contains user ID: %#v", entry["user_id"])
	}
}

func TestRequestLoggerWritesTraceFieldsWhenSpanContextExists(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1},
		SpanID:  trace.SpanID{2},
	})

	router := gin.New()
	router.Use(RequestLogger(logger))
	router.GET("/health", func(c *gin.Context) {
		// The logger reads the SpanContext from the request context after c.Next.
		c.Request = c.Request.WithContext(trace.ContextWithSpanContext(c.Request.Context(), spanContext))
		c.Status(http.StatusOK)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode JSON log: %v; output: %s", err, output.String())
	}
	if entry["trace_id"] != spanContext.TraceID().String() {
		t.Fatalf("trace ID = %#v, want %q", entry["trace_id"], spanContext.TraceID())
	}
	if entry["span_id"] != spanContext.SpanID().String() {
		t.Fatalf("span ID = %#v, want %q", entry["span_id"], spanContext.SpanID())
	}
}

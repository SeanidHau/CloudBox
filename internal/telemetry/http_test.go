package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestHTTPTracingRecordsServerSpanAndContinuesRemoteTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})

	router := gin.New()
	router.Use(HTTPTracing())
	router.GET("/files/:id", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	request := httptest.NewRequest(http.MethodGet, "/files/42", nil)
	otel.GetTextMapPropagator().Inject(
		trace.ContextWithRemoteSpanContext(context.Background(), parent),
		propagation.HeaderCarrier(request.Header),
	)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}

	span := spans[0]
	if span.Name() != "GET /files/:id" {
		t.Fatalf("span name = %q, want GET /files/:id", span.Name())
	}
	if span.SpanKind() != trace.SpanKindServer {
		t.Fatalf("span kind = %s, want server", span.SpanKind())
	}
	if span.SpanContext().TraceID() != parent.TraceID() {
		t.Fatalf("trace ID = %s, want continued trace %s", span.SpanContext().TraceID(), parent.TraceID())
	}
	if span.Parent().SpanID() != parent.SpanID() {
		t.Fatalf("parent span ID = %s, want %s", span.Parent().SpanID(), parent.SpanID())
	}

	assertSpanAttribute(t, span, "http.request.method", http.MethodGet)
	assertSpanAttribute(t, span, "url.path", "/files/42")
	assertSpanAttribute(t, span, "http.response.status_code", int64(http.StatusCreated))
}

func TestHTTPTracingMarksRecoveredServerError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)

	router := gin.New()
	// Tracing wraps Recovery so it observes the final 500 response status.
	router.Use(HTTPTracing(), gin.Recovery())
	router.GET("/panic", func(c *gin.Context) {
		panic("test panic")
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if spans[0].Status().Code != codes.Error {
		t.Fatalf("span status = %s, want error", spans[0].Status().Code)
	}
}

func assertSpanAttribute(t *testing.T, span sdktrace.ReadOnlySpan, key string, want any) {
	t.Helper()

	for _, attribute := range span.Attributes() {
		if string(attribute.Key) != key {
			continue
		}

		switch expected := want.(type) {
		case string:
			if attribute.Value.AsString() != expected {
				t.Fatalf("attribute %q = %q, want %q", key, attribute.Value.AsString(), expected)
			}
		case int64:
			if attribute.Value.AsInt64() != expected {
				t.Fatalf("attribute %q = %d, want %d", key, attribute.Value.AsInt64(), expected)
			}
		}

		return
	}

	t.Fatalf("span attribute %q not found", key)
}

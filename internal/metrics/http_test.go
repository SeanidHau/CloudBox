package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestHTTPMetricsRecordsRouteTemplateAndCompletesInFlight(t *testing.T) {
	gin.SetMode(gin.TestMode)

	registry := prometheus.NewRegistry()
	metrics := NewHTTPMetrics(registry)

	router := gin.New()
	router.Use(metrics.Middleware())
	router.GET("/files/:id", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/files/42", nil))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}

	labels := map[string]string{
		"method": http.MethodGet,
		"path":   "/files/:id",
		"status": "201",
	}

	requestMetric := findMetric(t, registry, "cloudbox_http_requests_total", labels)
	if value := requestMetric.GetCounter().GetValue(); value != 1 {
		t.Fatalf("request count = %v, want 1", value)
	}

	durationMetric := findMetric(t, registry, "cloudbox_http_request_duration_seconds", labels)
	if count := durationMetric.GetHistogram().GetSampleCount(); count != 1 {
		t.Fatalf("duration sample count = %d, want 1", count)
	}

	inFlightMetric := findMetric(t, registry, "cloudbox_http_requests_in_flight", nil)
	if value := inFlightMetric.GetGauge().GetValue(); value != 0 {
		t.Fatalf("in-flight requests = %v, want 0", value)
	}
}

func TestHTTPMetricsUsesUnmatchedPathForMissingRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	registry := prometheus.NewRegistry()
	metrics := NewHTTPMetrics(registry)

	router := gin.New()
	router.Use(metrics.Middleware())

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing/42", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}

	findMetric(t, registry, "cloudbox_http_requests_total", map[string]string{
		"method": http.MethodGet,
		"path":   "unmatched",
		"status": "404",
	})
}

func TestHTTPMetricsRecordsRecoveredPanicAsServerError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	registry := prometheus.NewRegistry()
	metrics := NewHTTPMetrics(registry)

	router := gin.New()
	// Metrics must wrap Recovery so the deferred observation sees status 500.
	router.Use(metrics.Middleware(), gin.Recovery())
	router.GET("/panic", func(c *gin.Context) {
		panic("test panic")
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}

	findMetric(t, registry, "cloudbox_http_requests_total", map[string]string{
		"method": http.MethodGet,
		"path":   "/panic",
		"status": "500",
	})
}

func findMetric(
	t *testing.T,
	registry *prometheus.Registry,
	name string,
	expectedLabels map[string]string,
) *dto.Metric {
	t.Helper()

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	for _, family := range metricFamilies {
		if family.GetName() != name {
			continue
		}

		for _, metric := range family.GetMetric() {
			if metricLabelsMatch(metric.GetLabel(), expectedLabels) {
				return metric
			}
		}
	}

	t.Fatalf("metric %q with labels %#v not found", name, expectedLabels)
	return nil
}

func metricLabelsMatch(labels []*dto.LabelPair, expected map[string]string) bool {
	if len(labels) != len(expected) {
		return false
	}

	for _, label := range labels {
		value, exists := expected[label.GetName()]
		if !exists || value != label.GetValue() {
			return false
		}
	}

	return true
}

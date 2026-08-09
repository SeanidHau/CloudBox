package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

type HTTPMetrics struct {
	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	requestInFlight prometheus.Gauge
}

func NewHTTPMetrics(registerer prometheus.Registerer) *HTTPMetrics {
	metrics := &HTTPMetrics{
		requestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "cloudbox",
				Subsystem: "http",
				Name:      "requests_total",
				Help:      "Total number of completed HTTP requests.",
			},
			[]string{"method", "path", "status"},
		),
		requestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "cloudbox",
				Subsystem: "http",
				Name:      "request_duration_seconds",
				Help:      "HTTP request duration in seconds.",
			},
			[]string{"method", "path", "status"},
		),
		requestInFlight: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "cloudbox",
				Subsystem: "http",
				Name:      "requests_in_flight",
				Help:      "Current number of in-flight HTTP requests.",
			},
		),
	}

	registerer.MustRegister(
		metrics.requestsTotal,
		metrics.requestDuration,
		metrics.requestInFlight,
	)

	return metrics
}

func (m *HTTPMetrics) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		m.requestInFlight.Inc()
		defer m.requestInFlight.Dec()

		c.Next()

		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}

		status := strconv.Itoa(c.Writer.Status())
		labels := []string{c.Request.Method, path, status}

		m.requestsTotal.WithLabelValues(labels...).Inc()
		m.requestDuration.WithLabelValues(labels...).Observe(time.Since(startedAt).Seconds())
	}
}

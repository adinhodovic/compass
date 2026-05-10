// Package metrics holds the Prometheus instrumentation shared by the
// server and registry packages. Metrics are registered against the
// default `prometheus.DefaultRegisterer` so a single `/metrics` handler
// from `Handler()` exposes everything.
package metrics

import (
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	HTTPRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "compass",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "HTTP requests handled, partitioned by route pattern, method, and status.",
		},
		[]string{"route", "method", "status"},
	)

	HTTPDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "compass",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request latency, partitioned by route pattern.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"route"},
	)

	SourceRefreshTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "compass",
			Subsystem: "source",
			Name:      "refresh_total",
			Help:      "Source refresh attempts, partitioned by source name and outcome (success|error).",
		},
		[]string{"source", "outcome"},
	)

	SourceRefreshDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "compass",
			Subsystem: "source",
			Name:      "refresh_duration_seconds",
			Help:      "Source refresh latency.",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		},
		[]string{"source"},
	)

	SourceServices = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "compass",
			Subsystem: "source",
			Name:      "services",
			Help:      "Number of services currently exposed by each source after the most recent refresh.",
		},
		[]string{"source"},
	)

	// SourceLastSuccessTimestamp is the Unix timestamp of the most recent
	// successful refresh per source. Pair with `time()` in a Prometheus alert
	// to page on stale sources, e.g. `time() - compass_source_last_success_timestamp_seconds > 600`.
	SourceLastSuccessTimestamp = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "compass",
			Subsystem: "source",
			Name:      "last_success_timestamp_seconds",
			Help:      "Unix timestamp (seconds) of the last successful refresh per source.",
		},
		[]string{"source"},
	)
)

// Handler is the HTTP handler that exposes the Prometheus metrics.
func Handler() http.Handler {
	return promhttp.Handler()
}

// ObserveSourceRefresh records the outcome and latency of a single source
// refresh attempt. If err is nil the outcome is "success", `count` is
// recorded as the gauge value, and the last-success timestamp is bumped;
// otherwise outcome is "error" and the gauges are left untouched (so
// alerting on stale `last_success_timestamp_seconds` keeps working).
func ObserveSourceRefresh(source string, count int, dur time.Duration, err error) {
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	SourceRefreshTotal.WithLabelValues(source, outcome).Inc()
	SourceRefreshDuration.WithLabelValues(source).Observe(dur.Seconds())
	if err == nil {
		SourceServices.WithLabelValues(source).Set(float64(count))
		SourceLastSuccessTimestamp.WithLabelValues(source).SetToCurrentTime()
	}
}

// NormalizeRoute collapses dynamic path segments down to fixed labels so
// we don't blow up Prometheus cardinality with one series per service ID
// or page slug. Returns "other" for anything unexpected.
func NormalizeRoute(path string) string {
	switch {
	case path == "/" || path == "":
		return "/"
	case path == "/services":
		return "/services"
	case strings.HasPrefix(path, "/services/"):
		return "/services/:id"
	case strings.HasPrefix(path, "/pages/"):
		return "/pages/*"
	case strings.HasPrefix(path, "/static/"):
		return "/static/*"
	case path == "/health":
		return "/health"
	case path == "/debug":
		return "/debug"
	case path == "/metrics":
		return "/metrics"
	case path == "/manifest.webmanifest":
		return "/manifest.webmanifest"
	default:
		return "other"
	}
}

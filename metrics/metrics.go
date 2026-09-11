// Package metrics provides Prometheus instrumentation for the REST service:
// a Metrics struct holding the collectors, a Register method that builds a
// DEDICATED registry, and Middleware that wraps an httprouter.Handle to record
// per-request counts, an in-flight gauge, and a latency histogram. The wrapped
// handlers stay oblivious to Prometheus — this middleware is the only place that
// touches it. This file is resource- and module-agnostic.
package metrics

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics bundles the three collectors the middleware records into.
type Metrics struct {
	TotalRequests  prometheus.Counter       // monotonic count of all handled requests
	ReqDuration    *prometheus.HistogramVec // request latency, labelled by method + path
	ActiveRequests *prometheus.GaugeVec     // in-flight requests, labelled by method + path
}

// NewMetrics constructs the collectors with their names, help text, and labels.
func NewMetrics() *Metrics {
	return &Metrics{
		TotalRequests: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "gateway_requests_total",
			Help: "Total number of HTTP requests",
		}),
		ReqDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gateway_requests_duration_seconds",
			Help:    "Request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
			[]string{"method", "path"}),
		ActiveRequests: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gateway_active_requests",
			Help: "Number of in-flight requests",
		},
			[]string{"method", "path"}),
	}
}

// Register creates a dedicated registry and registers every collector on it. The
// returned registry is served at /metrics via promhttp in main — using a private
// registry (not the global default) keeps the exposition surface explicit.
func (m *Metrics) Register() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(m.TotalRequests)
	reg.MustRegister(m.ReqDuration)
	reg.MustRegister(m.ActiveRequests)
	return reg
}

// Middleware wraps next, recording the in-flight gauge (incremented on entry,
// decremented on exit via defer), the latency histogram (observed on exit), and
// the total-request counter — all before delegating to the real handler. Wrap
// each BUSINESS route with this; do not wrap /metrics itself.
func Middleware(m *Metrics, next httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		labels := prometheus.Labels{"method": r.Method, "path": r.URL.Path}

		m.ActiveRequests.With(labels).Inc()
		defer m.ActiveRequests.With(labels).Dec()

		timer := prometheus.NewTimer(prometheus.ObserverFunc(func(d float64) {
			m.ReqDuration.With(labels).Observe(d)
		}))
		defer timer.ObserveDuration()

		m.TotalRequests.Inc()
		next(w, r, p)
	}
}

// Package metrics exposes Prometheus instrumentation for RelayForge.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry *prometheus.Registry

	httpRequests  *prometheus.CounterVec
	httpDuration  *prometheus.HistogramVec
	jobClaims     *prometheus.CounterVec
	jobSuccesses  *prometheus.CounterVec
	jobFailures   *prometheus.CounterVec
	jobRetries    *prometheus.CounterVec
	queueDepth    *prometheus.GaugeVec
	onlineWorkers *prometheus.GaugeVec
	leaseReaped   prometheus.Counter
	outboxBacklog prometheus.Gauge
}

func New() *Metrics {
	registry := prometheus.NewRegistry()
	m := &Metrics{
		registry: registry,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relayforge_http_requests_total",
			Help: "HTTP requests handled by the API.",
		}, []string{"method", "route", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "relayforge_http_request_duration_seconds",
			Help:    "HTTP request latency by route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route", "status"}),
		jobClaims: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relayforge_job_claims_total",
			Help: "Jobs claimed by workers.",
		}, []string{"project_id"}),
		jobSuccesses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relayforge_job_successes_total",
			Help: "Job executions that succeeded.",
		}, []string{"project_id"}),
		jobFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relayforge_job_failures_total",
			Help: "Job executions that failed.",
		}, []string{"project_id"}),
		jobRetries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relayforge_job_retries_total",
			Help: "Job executions scheduled for retry.",
		}, []string{"project_id"}),
		queueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "relayforge_queue_depth",
			Help: "Pending and scheduled jobs per project.",
		}, []string{"project_id"}),
		onlineWorkers: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "relayforge_online_workers",
			Help: "Online workers per project.",
		}, []string{"project_id"}),
		leaseReaped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relayforge_leases_reaped_total",
			Help: "Expired worker leases reaped by the API.",
		}),
		outboxBacklog: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "relayforge_outbox_backlog",
			Help: "Pending and processing outbox messages.",
		}),
	}
	registry.MustRegister(
		m.httpRequests,
		m.httpDuration,
		m.jobClaims,
		m.jobSuccesses,
		m.jobFailures,
		m.jobRetries,
		m.queueDepth,
		m.onlineWorkers,
		m.leaseReaped,
		m.outboxBacklog,
	)
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Middleware records request counts and latency. Route labels use the ServeMux
// pattern so path parameters do not explode cardinality.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		status := strconv.Itoa(recorder.status)
		m.httpRequests.WithLabelValues(r.Method, route, status).Inc()
		m.httpDuration.WithLabelValues(r.Method, route, status).Observe(time.Since(start).Seconds())
	})
}

func (m *Metrics) RecordClaim(projectID string) {
	m.jobClaims.WithLabelValues(projectID).Inc()
}

func (m *Metrics) RecordSuccess(projectID string) {
	m.jobSuccesses.WithLabelValues(projectID).Inc()
}

func (m *Metrics) RecordFailure(projectID string) {
	m.jobFailures.WithLabelValues(projectID).Inc()
}

func (m *Metrics) RecordRetry(projectID string) {
	m.jobRetries.WithLabelValues(projectID).Inc()
}

func (m *Metrics) ResetQueueDepth() {
	m.queueDepth.Reset()
}

func (m *Metrics) SetQueueDepth(projectID string, depth float64) {
	m.queueDepth.WithLabelValues(projectID).Set(depth)
}

func (m *Metrics) ResetOnlineWorkers() {
	m.onlineWorkers.Reset()
}

func (m *Metrics) SetOnlineWorkers(projectID string, online float64) {
	m.onlineWorkers.WithLabelValues(projectID).Set(online)
}

func (m *Metrics) RecordLeaseReaped(count int) {
	m.leaseReaped.Add(float64(count))
}

func (m *Metrics) SetOutboxBacklog(backlog float64) {
	m.outboxBacklog.Set(backlog)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

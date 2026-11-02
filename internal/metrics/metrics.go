// Package metrics measures the app for Prometheus: requests per route
// (count and duration), database connection pools, the job queue, and the
// Go runtime. Handler serves the measurements, with Go's profiler, on a
// separate address (METRICS_ADDR) that must only be reachable from inside
// your network.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/ukharwa/kickstart/internal/db"
	"github.com/ukharwa/kickstart/internal/metrics/store"
)

// Metrics holds the app's measurements.
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// New returns Metrics measuring the database d among the rest.
func New(d *db.DB, logger *slog.Logger) *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP requests, by route pattern and status code.",
		}, []string{"route", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "How long HTTP requests take, by route pattern.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
	}
	m.registry.MustRegister(
		m.requests, m.duration,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewDBStatsCollector(d.Write, "write"),
		&jobsCollector{db: d, logger: logger},
	)
	if d.Read != d.Write { // a separate pool for reads
		m.registry.MustRegister(collectors.NewDBStatsCollector(d.Read, "read"))
	}
	return m
}

// Middleware counts and times requests by the route pattern that served
// them (such as "POST /login"), which keeps the number of
// series small. Put it last in the middleware stack, next to the router:
// the router records the pattern on the request it is given.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		m.requests.WithLabelValues(route, strconv.Itoa(rec.status)).Inc()
		m.duration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	})
}

// statusRecorder notes the status code a handler sends.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the real writer.
func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Handler serves the measurements at /metrics and Go's profiler under
// /debug/pprof/. Never expose it publicly: profiles reveal the program's
// internals.
func (m *Metrics) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

// Start serves Handler on addr until ctx is done.
func (m *Metrics) Start(ctx context.Context, addr string, logger *slog.Logger) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("metrics: listen on %s: %w", addr, err)
	}
	// No write timeout: a CPU profile takes 30 seconds to collect.
	srv := &http.Server{Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	go func() {
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics server", "err", err)
		}
	}()
	logger.Info("metrics listening", "addr", ln.Addr().String())
	return nil
}

// jobsCollector reports the job queue's length and its failed jobs, counted
// in the database when Prometheus asks.
type jobsCollector struct {
	db     *db.DB
	logger *slog.Logger
}

var (
	queuedDesc = prometheus.NewDesc("jobs_queued", "Background jobs waiting to run or running.", nil, nil)
	failedDesc = prometheus.NewDesc("jobs_failed", "Background jobs that failed on every attempt (kept 30 days).", nil, nil)
)

func (c *jobsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- queuedDesc
	ch <- failedDesc
}

func (c *jobsCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q := store.New(c.db.Read)
	for desc, count := range map[*prometheus.Desc]func(context.Context) (int64, error){
		queuedDesc: q.CountQueuedJobs,
		failedDesc: q.CountFailedJobs,
	} {
		n, err := count(ctx)
		if err != nil {
			c.logger.WarnContext(ctx, "count jobs for metrics", "err", err)
			ch <- prometheus.NewInvalidMetric(desc, err)
			continue
		}
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(n))
	}
}

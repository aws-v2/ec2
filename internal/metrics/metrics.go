package metrics

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	reqTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total", Help: "HTTP requests.",
	}, []string{"method", "route", "status"})

	reqDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "http_request_duration_seconds", Help: "HTTP latency.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})
)

func init() { prometheus.MustRegister(reqTotal, reqDuration) }

// GinMiddleware records count and latency per route template.
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched" // keeps 404 scans from creating unlimited label values
		}
		reqTotal.WithLabelValues(c.Request.Method, route, strconv.Itoa(c.Writer.Status())).Inc()
		reqDuration.WithLabelValues(c.Request.Method, route).Observe(time.Since(start).Seconds())
	}
}

// RegisterDB exports connection pool stats. Call after the DB is opened.
func RegisterDB(db *sql.DB, name string) {
	prometheus.MustRegister(collectors.NewDBStatsCollector(db, name))
}

// Serve runs a metrics-only server on its own port, protected by an API key.
// It refuses to start without a key, so the endpoint can never be exposed open.
func Serve(addr, apiKey string) {
	if apiKey == "" {
		slog.Error("metrics server NOT started: METRICS_API_KEY is empty")
		return
	}
	want := sha256.Sum256([]byte(apiKey))

	authed := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := sha256.Sum256([]byte(r.Header.Get("X-Api-Key")))
			if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", authed(promhttp.Handler()))

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("metrics server NOT started: cannot bind", "addr", addr, "err", err)
		return
	}
	slog.Info("metrics server started", "addr", addr)

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server stopped", "err", err)
		}
	}()
}
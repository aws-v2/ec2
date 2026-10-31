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

	instancesByStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ec2_instances", Help: "Instances by status and profile.",
	}, []string{"status", "profile"})

	instanceCreateTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ec2_instance_create_total", Help: "Instance create attempts.",
	}, []string{"profile", "result"})

	instanceCreateDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ec2_instance_create_duration_seconds",
		Help:    "Time to provision a VM.",
		Buckets: []float64{5, 10, 20, 30, 60, 120, 300},
	}, []string{"profile"})

	amiBuildTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ec2_ami_build_total", Help: "AMI build attempts.",
	}, []string{"result"})

	amiBuildDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "ec2_ami_build_duration_seconds",
		Help:    "Time to build an AMI.",
		Buckets: []float64{30, 60, 120, 300, 600, 1200},
	})

	amiBuildProgress = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ec2_ami_build_progress_percent", Help: "AMI build progress percentage.",
	}, []string{"ami_id"})

	gatewayPorts = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ec2_gateway_ports", Help: "Gateway ports by status.",
	}, []string{"status"})

	agentCallErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ec2_agent_call_errors_total", Help: "Failed calls to host agents.",
	}, []string{"action", "host"})
)

func init() {
	prometheus.MustRegister(
		reqTotal,
		reqDuration,
		instancesByStatus,
		instanceCreateTotal,
		instanceCreateDuration,
		amiBuildTotal,
		amiBuildDuration,
		amiBuildProgress,
		gatewayPorts,
		agentCallErrors,
	)
}

// RecordInstanceCreate records the result and duration of an instance provisioning attempt.
func RecordInstanceCreate(profile, result string, duration time.Duration) {
	instanceCreateTotal.WithLabelValues(labelOrUnknown(profile), result).Inc()
	instanceCreateDuration.WithLabelValues(labelOrUnknown(profile)).Observe(duration.Seconds())
}

// RecordAMIBuild records the result and duration of an AMI build.
func RecordAMIBuild(result string, duration time.Duration) {
	amiBuildTotal.WithLabelValues(result).Inc()
	amiBuildDuration.Observe(duration.Seconds())
}

// SetAMIBuildProgress updates the current progress of an AMI build.
func SetAMIBuildProgress(amiID string, progress float64) {
	amiBuildProgress.WithLabelValues(labelOrUnknown(amiID)).Set(progress)
}

// ClearAMIBuildProgress removes the per-build series once the build finishes.
func ClearAMIBuildProgress(amiID string) {
	amiBuildProgress.DeleteLabelValues(labelOrUnknown(amiID))
}

// RecordAgentCallError records a failed call to a host agent.
func RecordAgentCallError(action, host string) {
	agentCallErrors.WithLabelValues(labelOrUnknown(action), labelOrUnknown(host)).Inc()
}

func labelOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

// RefreshDatabaseGauges refreshes instance and gateway-port counts from PostgreSQL.
// Both queries complete before replacing the previous gauge values.
func RefreshDatabaseGauges(db *sql.DB) error {
	type instanceCount struct {
		status  string
		profile string
		count   float64
	}
	var instanceCounts []instanceCount
	rows, err := db.Query(`
		SELECT COALESCE(status, 'unknown'), COALESCE(image_profile, 'unknown'), COUNT(*)
		FROM instances
		GROUP BY status, image_profile
	`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var count instanceCount
		if err := rows.Scan(&count.status, &count.profile, &count.count); err != nil {
			rows.Close()
			return err
		}
		instanceCounts = append(instanceCounts, count)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	type portCount struct {
		status string
		count  float64
	}
	var portCounts []portCount
	rows, err = db.Query(`
		SELECT COALESCE(status, 'unknown'), COUNT(*)
		FROM gateway_ports
		GROUP BY status
	`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var count portCount
		if err := rows.Scan(&count.status, &count.count); err != nil {
			rows.Close()
			return err
		}
		portCounts = append(portCounts, count)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	instancesByStatus.Reset()
	for _, count := range instanceCounts {
		instancesByStatus.WithLabelValues(count.status, count.profile).Set(count.count)
	}
	gatewayPorts.Reset()
	for _, count := range portCounts {
		gatewayPorts.WithLabelValues(count.status).Set(count.count)
	}
	return nil
}

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

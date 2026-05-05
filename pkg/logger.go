package pkg

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.Logger

// Init sets up the global logger
func Init(serviceName string) {
	env := getEnv("APP_ENV", "dev")

	var config zap.Config

	switch env {
	case "prod":
		config = zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zap.WarnLevel)

	case "staging":
		config = zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)

	default: // dev
		config = zap.NewDevelopmentConfig()
		config.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	}

	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	logger, err := config.Build(
		zap.Fields(
			zap.String("service", serviceName),
			zap.String("env", env),
		),
	)

	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}

	Log = logger
}

// Sync flushes logs (call on shutdown)
func Sync() {
	if Log != nil {
		_ = Log.Sync()
	}
}
func WithScope(scope string) *zap.Logger {
	return Log.With(
		zap.String("scope", scope),
	)
}
// WithRequest adds request-scoped logging context
func WithRequest(traceID string) *zap.Logger {
	return Log.With(
		zap.String("trace_id", traceID),
	)
}

// helper
func getEnv(key, fallback string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	return val
}
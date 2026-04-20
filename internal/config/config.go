package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	// Database
	DB DBConfig

	// NATS
	NATS NATSConfig

	// Server
	Server ServerConfig

	// Libvirt
	Libvirt LibvirtConfig

	// Profiles
	Profile string

	// MinIO
	MinIO MinIOConfig
}

type LibvirtConfig struct {
	URI       string
	ImagesDir string
}

type DBConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

type NATSConfig struct {
	URL           string
	User          string
	Password      string
	SubjectPrefix string
}

type ServerConfig struct {
	Port        string
	ServiceName string
	HTTPPort    int
}

type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		DB: DBConfig{
			Host:            getEnv("DB_HOST", "localhost"),
			Port:            getEnvInt("DB_PORT", 5432),
			User:            getEnv("DB_USER", "root"),
			Password:        getEnv("DB_PASSWORD", "root"),
			Database:        getEnv("DB_NAME", "ec2_db"), // Changed from network_db to ec2 to match context
			SSLMode:         getEnv("DB_SSLMODE", "disable"),
			MaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: getEnvDuration("DB_CONN_MAX_LIFETIME", 5*time.Minute),
			ConnMaxIdleTime: getEnvDuration("DB_CONN_MAX_IDLE_TIME", 10*time.Minute),
		},
		NATS: NATSConfig{
			URL:      getEnv("NATS_URL", "nats://localhost:4222"),
			User:     getEnv("NATS_USER", "auth-server"),
			Password: getEnv("NATS_PASSWORD", "auth-secret"),
			SubjectPrefix: getEnv("NATS_SUBJECT_PREFIX", "dev.v1"),
		},
		Server: ServerConfig{
			Port:        getEnv("PORT", "8088"),
			ServiceName: getEnv("SERVICE_NAME", "ec2-service"),
			HTTPPort:    getEnvInt("HTTP_PORT", 8088),
		},
		Libvirt: LibvirtConfig{
			URI:       getEnv("LIBVIRT_URI", "qemu:///system"),
			ImagesDir: getEnv("IMAGES_DIR", "/var/lib/libvirt/images"),
		},
		Profile: strings.ToLower(getEnv("APP_PROFILE", "dev")),
		MinIO: MinIOConfig{
			Endpoint:  getEnv("MINIO_ENDPOINT", "localhost:9000"),
			AccessKey: getEnv("MINIO_ACCESS_KEY", "minioadmin"),
			SecretKey: getEnv("MINIO_SECRET_KEY", "minioadmin123"),
			UseSSL:    getEnv("MINIO_USE_SSL", "false") == "true",
		},
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}

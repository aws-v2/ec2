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
SystemUserId string
	// Profiles
	Profile string

	// MinIO
	MinIO MinIOConfig

	AgentUrl         string
	MigrationsDir    string
	PublicKey        string
	PrivateKey       string
	AgentPort        int
	AgentUrlParts    string
	ProfileBaseImage map[string]string
	ENV              string
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
	ChannelBinding  string // "" locally, "require" for Neon
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

func LoadKeyPair() []string {
	return []string{
		`-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACCb9O27Iw1d0uTJS8QZ1Hd1l54D7tWNJgchEqh+fF7xyAAAAJA1N0zJNTdM
yQAAAAtzc2gtZWQyNTUxOQAAACCb9O27Iw1d0uTJS8QZ1Hd1l54D7tWNJgchEqh+fF7xyA
AAAED5fX1U0S1jTMBuVxVvMzWrRERHpS6qEcTQafq8+9Mga5v07bsjDV3S5MlLxBnUd3WX
ngPu1Y0mByESqH58XvHIAAAAC2VjMi1zZXJ2aWNlAQI=
-----END OPENSSH PRIVATE KEY-----`,
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJv07bsjDV3S5MlLxBnUd3WXngPu1Y0mByESqH58XvHI ec2-service",
	}

}
func Load() (*Config, error) {
	_ = godotenv.Load()
	profileBaseImage := make(map[string]string)

	profileBaseImage["vanilla"] = "rds-template"
	profileBaseImage["ai-worker"] = "ubuntu-22.04"
	profileBaseImage["gamelift"] = "ubuntu-22.04"
	profileBaseImage["games"] = "ubuntu-22.04"
	profileBaseImage["workers"] = "rds-template"
	profileBaseImage["lambda"] = "ubuntu-22.04"
	profileBaseImage["s3"] = "rds-template"
	profileBaseImage["rds"] = "rds-template"

	cfg := &Config{
		SystemUserId:"00000000-0000-0000-0000-000000000000",
		AgentUrlParts: getEnv("API_GATEWAY", "http://localhost:8080"),
		AgentPort:     getEnvInt("AGENT_PORT", 9030),
		PrivateKey:    LoadKeyPair()[0],
		ENV:           getEnv("ENV", "dev"),
		PublicKey:     LoadKeyPair()[1],
		AgentUrl:      getEnv("AGENT_URL", "ws://localhost:9030/terminal"),
		MigrationsDir: getEnv("MIGRATIONS_PATH", "./migrations"),
		DB: DBConfig{
			Host:            getEnv("DB_HOST", "localhost"),
			Port:            getEnvInt("DB_PORT", 5432),
			User:            getEnv("DB_USER", "root"),
			Password:        getEnv("DB_PASSWORD", "root"),
			Database:        getEnv("DB_NAME", "ec2_db1"),
			SSLMode:         getEnv("DB_SSLMODE", "disable"),
			ChannelBinding:  getEnv("DB_CHANNEL_BINDING", ""), // e.g. "require" for Neon
			MaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: getEnvDuration("DB_CONN_MAX_LIFETIME", 5*time.Minute),
			ConnMaxIdleTime: getEnvDuration("DB_CONN_MAX_IDLE_TIME", 10*time.Minute),
		},
// 		DB_HOST=ep-purple-feather-aypga34j-pooler.c-5.us-east-2.aws.neon.tech
// DB_PORT=5432
// DB_USER=neondb_owner
// DB_PASSWORD=npg_EHvDpaNKS73u
// DB_NAME=ec2_db
// DB_SSLMODE=require
// DB_CHANNEL_BINDING=require
		NATS: NATSConfig{
			URL:           getEnv("NATS_URL", "nats://localhost:4222"),
			User:          getEnv("NATS_USER", "auth-server"),
			Password:      getEnv("NATS_PASSWORD", "auth-secret"),
			SubjectPrefix: getEnv("NATS_PREFIX", "dev.v1"),
		},
		Server: ServerConfig{
			Port:        getEnv("PORT", "8088"),
			ServiceName: getEnv("SERVICE_NAME", "ec2-service"),
			HTTPPort:    getEnvInt("HTTP_PORT", 8088),
		},
		Libvirt: LibvirtConfig{
			URI:       getEnv("LIBVIRT_URI", "qemu:///system"), //this particular url works indev but not in staging
			ImagesDir: getEnv("IMAGES_DIR", "/var/lib/libvirt/images"),
		},
		Profile: strings.ToLower(getEnv("APP_PROFILE", "dev")),
		MinIO: MinIOConfig{
			Endpoint:  getEnv("MINIO_ENDPOINT", "localhost:9000"),
			AccessKey: getEnv("MINIO_ACCESS_KEY", "minioadmin"),
			SecretKey: getEnv("MINIO_SECRET_KEY", "minioadmin123"),
			UseSSL:    getEnv("MINIO_USE_SSL", "false") == "true",
		},
		ProfileBaseImage: profileBaseImage,
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

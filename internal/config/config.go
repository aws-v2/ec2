
// internal/config/config.go
package config

import "os"

type Config struct {
	PostgresConn string
	ServerPort   string
	ProxmoxURL   string
	ProxmoxNode  string
	ProxmoxToken string
}

func Load() *Config {
	return &Config{
		PostgresConn: getEnv("POSTGRES_CONN", "postgres://postgres:postgres@localhost:5432/ec2clone?sslmode=disable"),
		ServerPort:   getEnv("SERVER_PORT", "8085"),
		ProxmoxURL:   getEnv("PROXMOX_URL", "https://proxmox.local:8006"),
		ProxmoxNode:  getEnv("PROXMOX_NODE", "pve"),
		ProxmoxToken: getEnv("PROXMOX_TOKEN", ""),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
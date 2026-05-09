package host

import (
	"time"
)

type Host struct {
	ID            string    `json:"id"`
	Hostname      string    `json:"hostname"`
	IP            string    `json:"ip"`
	CPUTotal      int       `json:"cpu_total"`
	CPUUsed       int       `json:"cpu_used"`
	RAMTotal      int       `json:"ram_total"` // in MB
	RAMFree       int       `json:"ram_free"`  // in MB
	DiskTotal     int       `json:"disk_total"` // in GB
	DiskFree      int       `json:"disk_free"`  // in GB
	Status        string    `json:"status"`    // active, inactive, maintenance
	LastHeartbeat time.Time `json:"last_heartbeat"`
	CreatedAt     time.Time `json:"created_at"`
}

type HeartbeatRequest struct {
	HostID    string `json:"host_id"`
	Hostname  string `json:"hostname"`
	IP        string `json:"ip"`
	CPUTotal  int    `json:"cpu_total"`
	CPUUsed   int    `json:"cpu_used"`
	RAMTotal  int    `json:"ram_total"`
	RAMFree   int    `json:"ram_free"`
	DiskTotal int    `json:"disk_total"`
	DiskFree  int    `json:"disk_free"`
}

type Repository interface {
	Update(host *Host) error
	GetBestHosts(limit int) ([]*Host, error)
	GetByID(id string) (*Host, error)
}

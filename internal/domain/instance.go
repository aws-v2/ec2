package domain

import "time"

type InstanceStatus string

const (
	StatusPending    InstanceStatus = "pending"
	StatusRunning    InstanceStatus = "running"
	StatusStopped    InstanceStatus = "stopped"
	StatusTerminated InstanceStatus = "terminated"
	WaitForShutdown  InstanceStatus = "wait_for_shutdown"
	StatusRestarting InstanceStatus = "Restarting"
)

type Instance struct {
	ID           string         `json:"id" db:"id"`
	VMName       string         `json:"vm_name" db:"vm_name"`
	Image        string         `json:"image" db:"image"`
	CPU          int            `json:"cpu" db:"cpu"`
	RAM          int            `json:"ram" db:"ram"`
	SSHKey       string         `json:"ssh_key" db:"ssh_key"`
	Status       InstanceStatus `json:"status" db:"status"`
	IP           string         `json:"ip" db:"ip"`
	PublicIP     string         `json:"public_ip" db:"public_ip"`
	ProxmoxID    int            `json:"proxmox_id" db:"proxmox_id"`
	RootVolumeID string         `json:"root_volume_id" db:"root_volume_id"`
	StorageSize  int            `json:"storage_size" db:"storage_size"`
	StorageType  string         `json:"storage_type" db:"storage_type"`
	DeviceName   string         `json:"device_name" db:"device_name"`
	CreatedAt    time.Time      `json:"created_at" db:"created_at"`
	UserID       string         `json:"user_id" db:"user_id"`
}

type CreateInstanceRequest struct {
	Image  string `json:"image" binding:"required"`
	CPU    int    `json:"cpu" binding:"required"`
	RAM    int    `json:"ram" binding:"required"`
	SSHKey string `json:"ssh_key" binding:"required"`
}

type InstanceStatusCheck struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Details   string    `json:"details"`
	UpdatedAt time.Time `json:"updated_at"`
}

type InstanceMetrics struct {
	CPUUsage       float64   `json:"cpu_usage"`
	RAMUsageMB     int       `json:"ram_usage_mb"`
	RAMTotalMB     int       `json:"ram_total_mb"`
	NetworkInKbps  float64   `json:"network_in_kbps"`
	NetworkOutKbps float64   `json:"network_out_kbps"`
	DiskReadIOPS   int       `json:"disk_read_iops"`
	DiskWriteIOPS  int       `json:"disk_write_iops"`
	UptimeSeconds  int       `json:"uptime_seconds"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type InstanceTag struct {
	Key   string `json:"key" db:"key"`
	Value string `json:"value" db:"value"`
}
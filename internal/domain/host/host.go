package host

import (
	"context"
	"time"
)

type Host struct {
	HostType string `json:"hosttype"`
	ID            string    `json:"id"`
	Hostname      string    `json:"hostname"`
	IP            string    `json:"ip"`
	CPUTotal      float64       `json:"cpu_total"`
	CPUUsed       float64       `json:"cpu_used"`
	RAMTotal      float64       `json:"ram_total"` // in MB
	RAMFree       float64       `json:"ram_free"`  // in MB
	DiskTotal     float64       `json:"disk_total"` // in GB
	DiskFree      float64       `json:"disk_free"`  // in GB
	Status        string    `json:"status"`    // active, inactive, maintenance
	SSHUser       string    `json:"ssh_user"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	CreatedAt     time.Time `json:"created_at"`
	AvailableTemplates []string  `json:"available_templates"`
	SSHPrivateKey      string    `json:"ssh_private_key"`
}

type DomainStats struct {
	VMID      string  `json:"vmid"`
	HostID    string  `json:"hostid"`
	CreatedAt time.Time `json:"created_at"`
	Name      string  `json:"name"`
	State     string  `json:"state"`
	CPUUsed   float64 `json:"cpu_used"`   // Changed to float64
	Memory    uint64  `json:"memory_kb"`  // Changed to uint64
	DiskRead  uint64  `json:"disk_read"`  // Changed to uint64
	DiskWrite uint64  `json:"disk_write"` // Changed to uint64
	NetRx     uint64  `json:"net_rx"`     // Changed to uint64
	NetTx     uint64  `json:"net_tx"`     // Changed to uint64
}

type VMActionTarget struct {
	VMID   string
	HostIP string
	Action string // "sleep" or "terminate"
}
type HeartbeatRequest struct {
	HostType string `json:"hosttype"`
	HostID             string        `json:"host_id"`
	Hostname           string        `json:"hostname"`
	IP                 string        `json:"ip"`
	CPUTotal           float64       `json:"cpu_total"`
	CPUUsed            float64       `json:"cpu_used"`
	RAMTotal           float64       `json:"ram_total"`
	RAMFree            float64       `json:"ram_free"`
	DiskTotal          float64       `json:"disk_total"`
	DiskFree           float64       `json:"disk_free"`
	AvailableTemplates []string      `json:"available_templates"`
	SSHPrivateKey      string        `json:"ssh_private_key"`
	SSHUser            string        `json:"ssh_user"`
	VMs                []DomainStats `json:"vms"`
}

// type DomainStats struct {
// 	Name      string  `json:"name"`
// 	State     string  `json:"state"`
// 	CPUUsed   float64 `json:"cpu_used"`   // % or time
// 	Memory    uint64  `json:"memory_kb"`  // KB
// 	DiskRead  uint64  `json:"disk_read"`  // bytes
// 	DiskWrite uint64  `json:"disk_write"` // bytes
// 	NetRx     uint64  `json:"net_rx"`     // bytes
// 	NetTx     uint64  `json:"net_tx"`     // bytes
// }

type Repository interface {
	Update(host *Host) error
	GetBestHosts(limit int) ([]*Host, error)
	GetBestHostsByType(limit int, targetType string) ([]*Host, error)
	GetByID(id string) (*Host, error)
	ListAll(ctx context.Context) ([]Host, error)
}



type MetricsRepository interface {
	// Ins
	Insert(ctx context.Context, hostID string, metric DomainStats) error
	GetVMsRequiringAction(ctx context.Context, sleepDuration, terminateDuration time.Duration) ([]VMActionTarget, error)
}

type RolloutUpdateRequest struct {
	UserID string `json:"user_id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Bucket string `json:"bucket"`
	FileName string `json:"file_name"`
}

type UpdateResult struct {
	HostID string `json:"host_id"`
	Addr   string `json:"addr"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

type RolloutSummary struct {
	Total   int            `json:"total"`
	OK      int            `json:"ok"`
	Failed  int            `json:"failed"`
	Version string         `json:"version"`
	Results []UpdateResult `json:"results"`
	S3Error string `json:"error"`
}
type AgentUpdatePayload struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`

}




type GetDownloadURLRequest struct{
	UserID    string `json:"user_id"`
	AssetType string `json:"asset_type"`
	Key       string `json:"key"`
}

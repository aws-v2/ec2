package host

import (
	"context"
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
	SSHUser       string    `json:"ssh_user"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	CreatedAt     time.Time `json:"created_at"`
	AvailableTemplates []string  `json:"available_templates"`
	SSHPrivateKey      string    `json:"ssh_private_key"`
}

type HeartbeatRequest struct {
	HostID             string   `json:"host_id"`
	Hostname           string   `json:"hostname"`
	IP                 string   `json:"ip"`
	CPUTotal           int      `json:"cpu_total"`
	CPUUsed            int      `json:"cpu_used"`
	RAMTotal           int      `json:"ram_total"`
	RAMFree            int      `json:"ram_free"`
	DiskTotal          int      `json:"disk_total"`
	DiskFree           int      `json:"disk_free"`
	AvailableTemplates []string `json:"available_templates"`
	SSHPrivateKey      string   `json:"ssh_private_key"`
	SSHUser       string    `json:"ssh_user"`

}

type Repository interface {
	Update(host *Host) error
	GetBestHosts(limit int) ([]*Host, error)
	GetByID(id string) (*Host, error)
	ListAll(ctx context.Context) ([]Host, error)
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

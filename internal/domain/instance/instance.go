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

// InstanceLifecycleEvent represents the event published to NATS for Network Service integration.
type InstanceLifecycleEvent struct {
	CorrelationID string                  `json:"correlation_id"`
	InstanceID    string                  `json:"instance_id"`
	EventType     string                  `json:"event_type"`
	Timestamp     string                  `json:"timestamp"`
	Payload       InstanceLifecyclePayload `json:"payload"`
	AgentURL    string           `json:"agent_url,omitempty"`
	SessionID string `json:"session_id"`

}

type InstanceLifecyclePayload struct {
	IPAddress   string            `json:"ip_address"`
	VPCID       string            `json:"vpc_id"`
	ServicePort int               `json:"service_port"`
	Metadata    InstanceMetadata `json:"metadata"`
	AgentWS     string            `json:"agent_ws,omitempty"`

}

type InstanceMetadata struct {
	InstanceType string `json:"instance_type"`
	AMIID        string `json:"ami_id"`
}

const (
	EventInstanceStarted = "INSTANCE_STARTED"
	EventInstanceStopped = "INSTANCE_STOPPED"
	EventHealthUpdate    = "HEALTH_UPDATE"
	EventProvisioningProgress = "PROVISIONING_PROGRESS"
	EventInstanceError = "INSTANCE_ERROR"
	 EventInstanceProvisioned = "INSTANCE_PROVISIONED"
)

type ProvisioningProgressEvent struct {
	InstanceID string `json:"instance_id"`
	EventType  string `json:"event_type"`
	Stage      string `json:"stage"`
	Message    string `json:"message"`
	Timestamp  string `json:"timestamp"`
	 Data       any    `json:"data,omitempty"`
}

type Instance struct {
	ID           string         `json:"id" db:"id"`
	VMName       string         `json:"vm_name" db:"vm_name"`
	Image        string         `json:"image" db:"image"`
	CPU          int            `json:"cpu" db:"cpu"`
	RAM          int            `json:"ram" db:"ram"`
	PublicSSHKey       string         `json:"public_ssh_key" db:"public_sshkey"`
	PrivateSshKey       string         `json:"private_ssh_key" db:"private_sshkey"`
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
	VPCID        string         `json:"vpc_id" db:"vpc_id"`
	HostID       string         `json:"host_id" db:"host_id"`
	SSH        string         `json:"ssh" db:"ssh"`
}

type ProvisionInstanceEvent struct 
{
	Profile    string            `json:"profile"`
	Specs      map[string]int    `json:"specs"`
	UserID     string            `json:"user_id"`
	StorageARN string            `json:"storage_arn"`
	Manifest GameManifest `json:"manifest"`
	SessionID string `json:"session_id"`
}



type ProvisionedRequesFinishedResponse struct{
	VMID string `json:"vm_id"`
	AgentURL  string `json:"agent_url"`

}

type GameManifest struct {
	Name        string     `json:"name"`
	Version     string     `json:"version"`
	HeadlessBin string     `json:"headless_bin"`
	MainScene   string     `json:"main_scene"`
	PlayerNode  string     `json:"player_node"`
	SyncNodes   []SyncNode `json:"sync_nodes"`
	Parameters map[string]string `json:"parameters"`

}
type SyncNode struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type InstanceSpecs struct {
	CPU int `json:"cpu"`
	RAM int `json:"ram"`
}


type CreateInstanceRequest struct {
	Image      string            `json:"image" binding:"required"`
	CPU        int               `json:"cpu" binding:"required"`
	RAM        int               `json:"ram" binding:"required"`
	Profile    string            `json:"profile"`
	Manifest GameManifest `json:"manifest"`
	ARN string `json:"arn"`
	SessionID string `json:"session_id"`
	Assets []AssetConfigs `json:"assets,omitempty"`
	

}
type AssetConfigs struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type CreateInstanceResponse struct {
	Image      string            `json:"image" binding:"required"`
	CPU        int               `json:"cpu" binding:"required"`
	RAM        int               `json:"ram" binding:"required"`
	SSHKey     string            `json:"ssh_key" binding:"required"`
	VPCID      string            `json:"vpc_id"`
	Profile    string            `json:"profile"`
	Parameters map[string]string `json:"parameters"`
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

// InstanceInfo holds the connection details the terminal agent needs to open
// an SSH session to a VM. It is stored in the domain so that both the
// application layer and the repository interface can reference it without
// creating an import cycle.
type InstanceInfo struct {
	// AgentURL is the base URL of the agent (http/https/ws/wss).
	// Example: "http://10.201.129.253:9030"
	AgentURL string

	// SSH target fields forwarded verbatim to the agent's open_terminal message.
	VMIP      string
	VMSSHPort int    // 0 → agent defaults to 22
	SSHUser   string
	SSHKey    string // private key PEM content
	VMHost		string
	
}




 

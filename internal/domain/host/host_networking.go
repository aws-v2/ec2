package host

import domain "ec2-api/internal/domain/instance"

type NetworkReconcileRequest struct {
	VPCID   string        `json:"vpc_id"`
	VMID   string        `json:"vm_id"`

	Bridge  BridgeConfig  `json:"bridge"`
	IP      string        `json:"ip"`
	Gateway string        `json:"gateway"`
	Assets  []domain.Asset `json:"assets,omitempty"`
	SessionID string `json:"session_id"`

}

type AssetConfig struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Path   string `json:"path"` // Path on the HOST
	Target string `json:"target"` // Path inside the GUEST (optional, can be inferred or added)
	SHA256 string `json:"sha256"`
	
}

type BridgeConfig struct {
	Name    string `json:"name"`
	Gateway string `json:"gateway"`
	CIDR    string `json:"cidr"`
	Config  string `json:"config"`
}





type GatewayHost struct{
	ID string 
	StartPort int
	EndPort int

}
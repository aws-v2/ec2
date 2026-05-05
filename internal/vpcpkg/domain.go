package vpcpkg

import "time"

const (
	VPCStatusActive  = "active"
	VPCStatusPending = "pending"
	VPCStatusDeleted = "deleted"
)

// VPC represents a tenant-scoped isolated virtual network.
type VPC struct {
	ID         string    `db:"id"`
	Name       string    `db:"name"`
	CIDRBlock  string    `db:"cidr_block"`
	BridgeName string    `db:"bridge_name"`
	GatewayIP  string    `db:"gateway_ip"`
	TenantID   string    `db:"tenant_id"`
	Status     string    `db:"status"`
	IsDefault  bool      `db:"is_default"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

// NetworkAllocation records which IP has been assigned to an instance within a VPC.
type NetworkAllocation struct {
	ID          string    `db:"id"`
	VPCID       string    `db:"vpc_id"`
	InstanceID  string    `db:"instance_id"`
	IPAddress   string    `db:"ip_address"`
	AllocatedAt time.Time `db:"allocated_at"`
}

// InstanceNetwork is the resolved networking detail returned to the caller
// after a successful allocation — everything needed to build cloud-init.
type InstanceNetwork struct {
	PrivateIP  string
	Gateway    string
	BridgeName string
	VPCID      string
}
package vpcpkg

import "context"

// Repository defines all persistence operations for VPCs and IP allocations.
// The concrete implementation lives in internal/infra/postgres/vpc_repository.go.
type Repository interface {
	// VPC operations
	GetDefaultVPC(ctx context.Context, tenantID string) (*VPC, error)
	GetVPCByID(ctx context.Context, vpcID string) (*VPC, error)
	ListVPCsByTenant(ctx context.Context, tenantID string) ([]*VPC, error)
	CreateVPC(ctx context.Context, vpc *VPC) error
	UpdateVPCStatus(ctx context.Context, vpcID, status string) error
	DeleteVPC(ctx context.Context, vpcID string) error

	// CIDR allocation — must run inside a serializable transaction to prevent races
	GetUsedCIDRs(ctx context.Context) ([]string, error)

	// IP allocation within a VPC
	GetAllocatedIPs(ctx context.Context, vpcID string) ([]string, error)
	AllocateIP(ctx context.Context, alloc *NetworkAllocation) error
	ReleaseIP(ctx context.Context, instanceID string) error
	GetAllocationByInstance(ctx context.Context, instanceID string) (*NetworkAllocation, error)

	// Guard: count running instances in a VPC before allowing deletion
	CountActiveInstances(ctx context.Context, vpcID string) (int, error)
}
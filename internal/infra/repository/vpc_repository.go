package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	vpc "ec2-api/internal/vpcpkg"
	"github.com/jmoiron/sqlx"
)

type VPCRepository struct {
	db *sqlx.DB
}

func NewVPCRepository(db *sqlx.DB) *VPCRepository {
	return &VPCRepository{db: db}
}

// ─── VPC CRUD ─────────────────────────────────────────────────────────────────

func (r *VPCRepository) GetDefaultVPC(ctx context.Context, tenantID string) (*vpc.VPC, error) {
	var v vpc.VPC
	err := r.db.GetContext(ctx, &v, `
		SELECT id, name, cidr_block, bridge_name, gateway_ip, tenant_id, host_id, status, is_default, created_at, updated_at
		FROM vpcs
		WHERE tenant_id = $1 AND is_default = true AND status != 'deleted'
		LIMIT 1
	`, tenantID)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *VPCRepository) GetVPCByID(ctx context.Context, vpcID string) (*vpc.VPC, error) {
	var v vpc.VPC
	err := r.db.GetContext(ctx, &v, `
		SELECT id, name, cidr_block, bridge_name, gateway_ip, tenant_id, host_id, status, is_default, created_at, updated_at
		FROM vpcs
		WHERE id = $1 AND status != 'deleted'
	`, vpcID)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *VPCRepository) ListVPCsByTenant(ctx context.Context, tenantID string) ([]*vpc.VPC, error) {
	var rows []*vpc.VPC
	err := r.db.SelectContext(ctx, &rows, `
		SELECT id, name, cidr_block, bridge_name, gateway_ip, tenant_id, host_id, status, is_default, created_at, updated_at
		FROM vpcs
		WHERE tenant_id = $1 AND status != 'deleted'
		ORDER BY created_at ASC
	`, tenantID)
	return rows, err
}

func (r *VPCRepository) CreateVPC(ctx context.Context, v *vpc.VPC) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO vpcs (id, name, cidr_block, bridge_name, gateway_ip, tenant_id, host_id, status, is_default, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`,
		v.ID, v.Name, v.CIDRBlock, v.BridgeName, v.GatewayIP,
		v.TenantID, v.HostID, v.Status, v.IsDefault, v.CreatedAt, v.UpdatedAt,
	)
	return err
}

func (r *VPCRepository) UpdateVPCStatus(ctx context.Context, vpcID, status string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE vpcs SET status = $1, updated_at = $2 WHERE id = $3
	`, status, time.Now(), vpcID)
	return err
}

func (r *VPCRepository) UpdateVPCHost(ctx context.Context, vpcID, hostID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE vpcs SET host_id = $1, updated_at = $2 WHERE id = $3
	`, hostID, time.Now(), vpcID)
	return err
}

func (r *VPCRepository) DeleteVPC(ctx context.Context, vpcID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE vpcs SET status = 'deleted', updated_at = $1 WHERE id = $2
	`, time.Now(), vpcID)
	return err
}

// ─── CIDR ALLOCATION ──────────────────────────────────────────────────────────

// GetUsedCIDRs returns all non-deleted CIDR blocks.
// The caller (service.go) runs AllocateNextCIDR against this list.
// For full safety under concurrent creates, wrap the caller in a DB-level
// serializable transaction — or add a unique constraint on cidr_block (already
// enforced by the UNIQUE index on the column).
func (r *VPCRepository) GetUsedCIDRs(ctx context.Context) ([]string, error) {
	var cidrs []string
	err := r.db.SelectContext(ctx, &cidrs, `
		SELECT cidr_block FROM vpcs WHERE status != 'deleted'
	`)
	return cidrs, err
}

// ─── IP ALLOCATION ────────────────────────────────────────────────────────────

func (r *VPCRepository) GetAllocatedIPs(ctx context.Context, vpcID string) ([]string, error) {
	var ips []string
	err := r.db.SelectContext(ctx, &ips, `
		SELECT ip_address FROM network_allocations WHERE vpc_id = $1
	`, vpcID)
	return ips, err
}

func (r *VPCRepository) AllocateIP(ctx context.Context, alloc *vpc.NetworkAllocation) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO network_allocations (id, vpc_id, instance_id, ip_address, allocated_at)
		VALUES ($1, $2, $3, $4, $5)
	`, alloc.ID, alloc.VPCID, alloc.InstanceID, alloc.IPAddress, alloc.AllocatedAt)
	return err
}

func (r *VPCRepository) ReleaseIP(ctx context.Context, instanceID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM network_allocations WHERE instance_id = $1
	`, instanceID)
	return err
}

func (r *VPCRepository) GetAllocationByInstance(ctx context.Context, instanceID string) (*vpc.NetworkAllocation, error) {
	var a vpc.NetworkAllocation
	err := r.db.GetContext(ctx, &a, `
		SELECT id, vpc_id, instance_id, ip_address, allocated_at
		FROM network_allocations
		WHERE instance_id = $1
	`, instanceID)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ─── GUARD ────────────────────────────────────────────────────────────────────

func (r *VPCRepository) CountActiveInstances(ctx context.Context, vpcID string) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count, `
		SELECT COUNT(*) FROM instances
		WHERE vpc_id = $1 AND status NOT IN ('terminated', 'failed')
	`, vpcID)
	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("count active instances: %w", err)
	}
	return count, nil
}
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	dto "ec2-api/internal/domain/dto"
	domain "ec2-api/internal/domain/instance"

	"ec2-api/internal/interfaces"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type instanceRepository struct { // lowercase, unexported
	db *sqlx.DB
}

func (r *instanceRepository) CreateScalingPolicy(ctx context.Context, userID string, req *domain.ScalingPolicyRequest) error {
	query := `
		INSERT INTO ec2_scaling_policies (
			id, user_id, name, policy_type,
			min_capacity, max_capacity, target_value,
			scale_in_cooldown, scale_out_cooldown,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	now := time.Now()
	_, err := r.db.ExecContext(ctx, query,
		uuid.New().String(), userID, req.Name, req.PolicyType,
		req.MinCapacity, req.MaxCapacity, req.TargetValue,
		req.ScaleInCooldown, req.ScaleOutCooldown,
		now, now,
	)
	if err != nil {
		return fmt.Errorf("CreateScalingPolicy: %w", err)
	}
	return nil
}

func (r *instanceRepository) GetScalingPolicies(ctx context.Context, userID string) ([]domain.ScalingPolicy, error) {
	query := `
		SELECT
			id, user_id, name, policy_type,
			min_capacity, max_capacity, target_value,
			scale_in_cooldown, scale_out_cooldown,
			created_at, updated_at
		FROM ec2_scaling_policies
		WHERE user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("GetScalingPolicies: %w", err)
	}
	defer rows.Close()

	var policies []domain.ScalingPolicy
	for rows.Next() {
		var p domain.ScalingPolicy
		if err := rows.Scan(
			&p.ID, &p.UserID, &p.Name, &p.PolicyType,
			&p.MinCapacity, &p.MaxCapacity, &p.TargetValue,
			&p.ScaleInCooldown, &p.ScaleOutCooldown,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("GetScalingPolicies scan: %w", err)
		}
		policies = append(policies, p)
	}
	return policies, rows.Err()
}

func (r *instanceRepository) UpdateScalingPolicy(ctx context.Context, userID, policyID string, req *domain.UpdateScalingPolicyRequest) error {
	query := `
		UPDATE ec2_scaling_policies
		SET
			name               = COALESCE($1, name),
			min_capacity       = COALESCE($2, min_capacity),
			max_capacity       = COALESCE($3, max_capacity),
			target_value       = COALESCE($4, target_value),
			scale_in_cooldown  = COALESCE($5, scale_in_cooldown),
			scale_out_cooldown = COALESCE($6, scale_out_cooldown),
			updated_at         = $7
		WHERE id = $8 AND user_id = $9
	`
	args := []any{
		req.Name, req.MinCapacity, req.MaxCapacity,
		req.TargetValue, req.ScaleInCooldown, req.ScaleOutCooldown,
		time.Now(), policyID, userID,
	}
	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("UpdateScalingPolicy: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("UpdateScalingPolicy rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("scaling policy %s not found for user %s", policyID, userID)
	}
	return nil
}

func (r *instanceRepository) DeleteScalingPolicy(ctx context.Context, userID, policyID string) error {
	query := `
		DELETE FROM ec2_scaling_policies
		WHERE id = $1 AND user_id = $2
	`
	result, err := r.db.ExecContext(ctx, query, policyID, userID)
	if err != nil {
		return fmt.Errorf("DeleteScalingPolicy: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("DeleteScalingPolicy rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("scaling policy %s not found for user %s", policyID, userID)
	}
	return nil
}
func NewInstanceRepository(db *sqlx.DB) interfaces.InstanceRepository { // return interface
	return &instanceRepository{db: db}
}
func (r *instanceRepository) Update(instance *domain.Instance) error {
	query := `
		UPDATE instances 
		SET status = $1, 
		    ip = $2, 
		    public_ip = $3, 
		    proxmox_id = $4,
		    vm_name = $5,
		    root_volume_id = $6,
		    storage_size = $7,
		    storage_type = $8,
		    device_name = $9,
		    vpc_id = $10
		WHERE id = $11
	`
	_, err := r.db.Exec(query,
		instance.Status,
		instance.IP,
		instance.PublicIP,
		instance.ProxmoxID,
		instance.VMName,
		instance.RootVolumeID,
		instance.StorageSize,
		instance.StorageType,
		instance.DeviceName,
		instance.VPCID,
		instance.ID,
	)
	return err
}
func (r *instanceRepository) Create(instance *domain.Instance) error {
	query := `INSERT INTO instances (
		id, vm_name, image, cpu, ram, 
		public_sshkey, private_sshkey,
		status, ip, public_ip, proxmox_id, 
		created_at, user_id, root_volume_id, 
		storage_size, storage_type, device_name, vpc_id
	) 
	VALUES (
		$1, $2, $3, $4, $5, 
		$6, $7,
		$8, $9, $10, $11, 
		$12, $13, $14, 
		$15, $16, $17, $18
	)`

	instance.CreatedAt = time.Now()

	_, err := r.db.Exec(
		query,
		instance.ID,
		instance.VMName,
		instance.Image,
		instance.CPU,
		instance.RAM,
		instance.PublicSSHKey,
		instance.PrivateSshKey, // ✅ new field
		instance.Status,
		instance.IP,
		instance.PublicIP,
		instance.ProxmoxID,
		instance.CreatedAt,
		instance.UserID,
		instance.RootVolumeID,
		instance.StorageSize,
		instance.StorageType,
		instance.DeviceName,
		instance.VPCID,
	)

	return err
}
func (r *instanceRepository) FindByID(id string) (*domain.Instance, error) {
	var instance domain.Instance
	query := `SELECT * FROM instances WHERE id = $1 AND status != 'terminated'`

	err := r.db.Get(&instance, query, id)
	if err == sql.ErrNoRows {
		return nil, dto.ErrInstanceNotFound
	}
	return &instance, err
}

// repository
func (r *instanceRepository) FindAll(userID string) ([]*domain.Instance, error) {
	var instances []*domain.Instance

	query := `SELECT * FROM instances WHERE status != 'terminated' AND user_id = $1 ORDER BY created_at DESC`

	err := r.db.Select(&instances, query, userID)
	if err != nil {
		return nil, err
	}

	// log.Printf("[repo] FindAll userID=%s found=%d", userID, len(instances))
	return instances, nil
}

func (r *instanceRepository) UpdateStatus(id string, status domain.InstanceStatus) error {
	query := `UPDATE instances SET status = $1 WHERE id = $2`
	result, err := r.db.Exec(query, status, id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return dto.ErrInstanceNotFound
	}
	return nil
}

func (r *instanceRepository) Delete(id string) error {
	return r.UpdateStatus(id, domain.StatusTerminated)
}

func (r *instanceRepository) GetTags(instanceID string) ([]*domain.InstanceTag, error) {
	var tags []*domain.InstanceTag
	query := `SELECT key, value FROM instance_tags WHERE instance_id = $1`
	err := r.db.Select(&tags, query, instanceID)
	return tags, err
}

func (r *instanceRepository) AddOrUpdateTag(instanceID string, tag *domain.InstanceTag) error {
	query := `
		INSERT INTO instance_tags (instance_id, key, value)
		VALUES ($1, $2, $3)
		ON CONFLICT (instance_id, key) DO UPDATE SET value = EXCLUDED.value
	`
	_, err := r.db.Exec(query, instanceID, tag.Key, tag.Value)
	return err
}

func (r *instanceRepository) DeleteTag(instanceID string, key string) error {
	query := `DELETE FROM instance_tags WHERE instance_id = $1 AND key = $2`
	_, err := r.db.Exec(query, instanceID, key)
	return err
}

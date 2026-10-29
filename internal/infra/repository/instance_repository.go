package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"ec2-api/config"
	dto "ec2-api/internal/domain/dto"
	domain "ec2-api/internal/domain/instance"

	"ec2-api/internal/interfaces"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	// "github.com/shirou/gopsutil/host"
)

type instanceRepository struct { // lowercase, unexported
	db       *sqlx.DB
	cfg      *config.Config
	hostRepo interfaces.HostRepository
}

func NewInstanceRepository(db *sqlx.DB, cfg *config.Config, hostRepo interfaces.HostRepository) interfaces.InstanceRepository { // return interface
	return &instanceRepository{db: db, cfg: cfg, hostRepo: hostRepo}
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
		    vpc_id = $10,
		    host_id = $11
		WHERE id = $12
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
		instance.HostID,
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
		storage_size, storage_type, device_name, vpc_id, host_id, session_id, public_port,image_profile
	) 
	VALUES (
		$1, $2, $3, $4, $5, 
		$6, $7,
		$8, $9, $10, $11, 
		$12, $13, $14, 
		$15, $16, $17, $18, $19, $20, $21, $22
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
		instance.HostID,
		instance.SessionID,
		instance.PublicPort,
		instance.ImageProfile,
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
func (r *instanceRepository) FindAMIsByUserID(userID string) ([]domain.AMISummary, error) {
	query := `
		SELECT id, name, COALESCE(description, ''), status,
		       COALESCE(source_instance_id, ''), created_at
		FROM golden_images
		WHERE user_id = $1
		ORDER BY created_at DESC`

	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("query amis for user %s: %w", userID, err)
	}
	defer rows.Close()

	images := make([]domain.AMISummary, 0) // empty slice, so JSON renders [] instead of null
	for rows.Next() {
		var a domain.AMISummary
		if err := rows.Scan(
			&a.ID, &a.Name, &a.Description, &a.Status,
			&a.SourceInstanceID, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ami row: %w", err)
		}
		images = append(images, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ami rows: %w", err)
	}
	return images, nil
}

func (r *instanceRepository) FindAMIByIDAndUserID(id, userID string) (*domain.Image, error) {
	query := `
		SELECT id, name, COALESCE(description, ''), user_id,
		       COALESCE(source_instance_id, ''), COALESCE(host_id, ''),
		       status,sha256, no_reboot, COALESCE(consistency, ''),
		       disk_path, COALESCE(size_bytes, 0), COALESCE(sha256, ''),
		       COALESCE(error_message, ''), created_at, updated_at
		FROM golden_images
		WHERE id = $1 AND user_id = $2`

	var img domain.Image
	err := r.db.QueryRow(query, id, userID).Scan(
		&img.ID, &img.Name, &img.Description, &img.UserID,
		&img.SourceInstanceID, &img.HostID,
		&img.Status, &img.SHA256,&img.NoReboot, &img.Consistency,
		&img.DiskPath, &img.SizeBytes, &img.SHA256,
		&img.ErrorMessage, &img.CreatedAt, &img.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("No found")
		}
		return nil, fmt.Errorf("find ami %s for user %s: %w", id, userID, err)
	}
	return &img, nil
}

func (r *instanceRepository) CreateAMI(image *domain.Image) error {
	query := `
		INSERT INTO golden_images (
			id, name, description, user_id, source_instance_id, host_id,
			status, no_reboot, consistency, disk_path, size_bytes, sha256, error_message
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12, $13
		)
		RETURNING created_at, updated_at`

	return r.db.QueryRow(
		query,
		image.ID, image.Name, image.Description, image.UserID, image.SourceInstanceID, image.HostID,
		image.Status, image.NoReboot, image.Consistency, image.DiskPath, image.SizeBytes, image.SHA256, image.ErrorMessage,
	).Scan(&image.CreatedAt, &image.UpdatedAt)
}

func (r *instanceRepository) UpdateAMI(image *domain.Image) error {
	query := `
		UPDATE golden_images SET
			name          = $1,
			description   = $2,
			status        = $3,
			consistency   = $4,
			disk_path     = $5,
			size_bytes    = $6,
			sha256        = $7,
			error_message = $8,
			progress      = $9,
			updated_at    = CURRENT_TIMESTAMP
		WHERE id = $10 AND deleted_at IS NULL
		RETURNING updated_at`

	err := r.db.QueryRow(
		query,
		image.Name, image.Description, image.Status, image.Consistency, image.DiskPath,
		image.SizeBytes, image.SHA256, image.ErrorMessage, image.Progress, image.ID,
	).Scan(&image.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("image %s not found", image.ID)
	}
	return err
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

// GetInstanceInfo returns the connection details the terminal agent needs
// to open an SSH session to the given VM.
//
// The agent is assumed to run on the instance's public IP on port 9030
// (the same port every provisioned agent binds to). Adjust the port
// constant below if your deployment uses a different one.
func (r *instanceRepository) GetInstanceInfo(instanceID, userID string) (*domain.InstanceInfo, error) {
	const agentPort = 9030

	instance, err := r.FindByID(instanceID)

	if err != nil {
		return nil, fmt.Errorf("GetInstanceInfo: %w, forinstance %s", err, instanceID)
	}
	// Ownership check.
	if instance.UserID != userID {
		return nil, fmt.Errorf("GetInstanceInfo: instance %s not found for user %s", instanceID, userID)
	}

	host, err := r.hostRepo.GetByID(instance.HostID)

	agentHost := host.IP
	if agentHost == "" {
		agentHost = instance.IP // fall back to private IP
	}

	return &domain.InstanceInfo{
		// VMHost:  fmt.Sprintf("http://%s:%d", agentHost, agentPort),
		VMHost: host.ID,
		// AgentURL:  r.cfg.AgentUrl,
		AgentURL: fmt.Sprintf("ws://%s:%d", agentHost, agentPort),

		VMIP:      instance.IP,
		VMSSHPort: 22,
		SSHUser:   host.SSHUser,
		SSHKey:    instance.PrivateSshKey,
	}, nil
}

func (r *instanceRepository) ReleaseLambdaVM(ctx context.Context, vmId string) (*domain.Instance, error) {
	query := `
		UPDATE instances
		SET status = $1
		WHERE id = $2
		RETURNING *
	`

	var instance domain.Instance

	err := r.db.GetContext(
		ctx,
		&instance,
		query,
		domain.StatusRunning,
		vmId,
	)

	if err == sql.ErrNoRows {
		return nil, dto.ErrInstanceNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("ReleaseLambdaVM: %w", err)
	}

	return &instance, nil
}



func (r *instanceRepository) FindUnreachableVms(ctx context.Context) ([]*domain.Instance, error) {
	query := `
		SELECT id, vm_name, ip , host_id
		FROM instances 
		WHERE (status = 'running' OR status = 'active')
		AND (vm_name LIKE 'sagemaker%' OR vm_name LIKE 'lambda%')
	`


	var instances []*domain.Instance
	err := r.db.SelectContext(ctx, &instances, query)
	if err != nil {
		return nil, fmt.Errorf("FindUnreachableVms: %w", err)
	}

	return instances, nil
}
func (r *instanceRepository) FindAndMarkWarmInstanceInUse(ctx context.Context, profile string) (*domain.Instance, error) {
	query := `
		UPDATE instances 
		SET status = $1
		WHERE id = (
			SELECT id FROM instances 
			WHERE image_profile = $2 AND (status = 'running' OR status = 'active') 
			ORDER BY created_at ASC 
			LIMIT 1 
			FOR UPDATE SKIP LOCKED
		)
		RETURNING *
	`

	var instance domain.Instance
	err := r.db.GetContext(ctx, &instance, query, domain.StatusInUse, profile)

			log.Printf("ERR %#v\n", err)
	
	if err == sql.ErrNoRows {
		return nil, dto.ErrInstanceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("FindAndMarkWarmInstanceInUse: %w", err)
	}
			log.Printf("INSTANCE Before return  %+v\n", instance)

	return &instance, nil
}

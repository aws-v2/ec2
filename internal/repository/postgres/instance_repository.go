package postgres

import (
	"database/sql"
	"time"

	"ec2-api/internal/domain"

	"github.com/jmoiron/sqlx"
)

type instanceRepository struct { // lowercase, unexported
	db *sqlx.DB
}

func NewInstanceRepository(db *sqlx.DB) domain.InstanceRepository { // return interface
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
	query := `INSERT INTO instances (id, vm_name, image, cpu, ram, ssh_key, status, ip, public_ip, proxmox_id, created_at, user_id, root_volume_id, storage_size, storage_type, device_name, vpc_id) 
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`

	instance.CreatedAt = time.Now()
	_, err := r.db.Exec(query, instance.ID, instance.VMName, instance.Image, instance.CPU, instance.RAM,
		instance.SSHKey, instance.Status, instance.IP, instance.PublicIP, instance.ProxmoxID, instance.CreatedAt, instance.UserID,
		instance.RootVolumeID, instance.StorageSize, instance.StorageType, instance.DeviceName, instance.VPCID)
	return err
}

func (r *instanceRepository) FindByID(id string) (*domain.Instance, error) {
	var instance domain.Instance
	query := `SELECT * FROM instances WHERE id = $1 AND status != 'terminated'`

	err := r.db.Get(&instance, query, id)
	if err == sql.ErrNoRows {
		return nil, domain.ErrInstanceNotFound
	}
	return &instance, err
}

func (r *instanceRepository) FindAll(userID string) ([]*domain.Instance, error) {
	var instances []*domain.Instance
	query := `SELECT * FROM instances WHERE status != 'terminated' AND user_id = $1 ORDER BY created_at DESC`
	err := r.db.Select(&instances, query, userID)
	return instances, err
}

func (r *instanceRepository) UpdateStatus(id string, status domain.InstanceStatus) error {
	query := `UPDATE instances SET status = $1 WHERE id = $2`
	result, err := r.db.Exec(query, status, id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return domain.ErrInstanceNotFound
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

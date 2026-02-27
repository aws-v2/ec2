// repositories/postgres/volume_repository.go
package postgres

import (
	"database/sql"
	"fmt"

	"github.com/Qarani-m/ec2-api/internal/domain"
	"github.com/jmoiron/sqlx"
)

type VolumeRepository struct {
	db *sqlx.DB
}

func NewVolumeRepository(db *sqlx.DB) *VolumeRepository {
	return &VolumeRepository{db: db}
}

func (r *VolumeRepository) Create(volume *domain.Volume) error {
	query := `
		INSERT INTO volumes (volume_name, name, size, format, type, availability_zone, status, attached_to)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at
	`

	err := r.db.QueryRow(query,
		volume.VolumeName,
		volume.Name,
		volume.Size,
		volume.Format,
		volume.Type,
		volume.AvailabilityZone,
		volume.Status,
		nullString(volume.AttachedTo),
	).Scan(&volume.ID, &volume.CreatedAt, &volume.UpdatedAt)
	
	return err
}

func (r *VolumeRepository) FindByID(id int) (*domain.Volume, error) {
	var volume domain.Volume

	fmt.Println(id)

query := `
	SELECT id, volume_name, name, size, format, type, availability_zone, status,
	       COALESCE(device_path, '') as device_path,
	       COALESCE(attached_to, '') as attached_to,
	       created_at, updated_at
	FROM volumes WHERE id = $1
`

	
	err := r.db.Get(&volume, query, id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &volume, nil
}

func (r *VolumeRepository) FindAll() ([]*domain.Volume, error) {
	var volumes []*domain.Volume
	query := `
		SELECT id, volume_name, name, size, format, type, availability_zone, status, 
		       COALESCE(attached_to, '') as attached_to, created_at, updated_at 
		FROM volumes 
		ORDER BY created_at DESC
	`
	
	err := r.db.Select(&volumes, query)
	if err != nil {
		return nil, err
	}
	return volumes, nil
}

func (r *VolumeRepository) FindByInstanceID(instanceID string) ([]*domain.Volume, error) {
	var volumes []*domain.Volume
	query := `
		SELECT COALESCE(attached_to, '') as attached_to, 
		       COALESCE(device_path, '') as device_path,
		       created_at, updated_at 
		FROM volumes WHERE attached_to = $1
	`
	
	err := r.db.Select(&volumes, query, instanceID)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Query: SELECT ... WHERE attached_to = '%s'\n", instanceID)
fmt.Printf("Returned %d volumes\n", len(volumes))
	return volumes, nil
}

func (r *VolumeRepository) Update(volume *domain.Volume) error {
 
	query := `
		UPDATE volumes 
		SET volume_name = $2, name = $3, size = $4, format = $5, type = $6, 
		    availability_zone = $7, status = $8, attached_to = $9, 
		    device_path = $10, updated_at = now()
		WHERE id = $1
		RETURNING updated_at
	`
	err := r.db.QueryRow(query,
		volume.ID,
		volume.VolumeName,
		volume.Name,
		volume.Size,
		volume.Format,
		volume.Type,
		volume.AvailabilityZone,
		volume.Status,
		nullString(volume.AttachedTo),
		nullString(volume.DevicePath),
	).Scan(&volume.UpdatedAt)
	
	if err == sql.ErrNoRows {
		return fmt.Errorf("volume not found: %d", volume.ID)
	}
	return err
}

func (r *VolumeRepository) UpdateSize(id int, newSize int) error {
	query := `UPDATE volumes SET size = $1, updated_at = now() WHERE id = $2`
	_, err := r.db.Exec(query, newSize, id)
	return err
}

func (r *VolumeRepository) Delete(volume *domain.Volume) error {
	query := `DELETE FROM volumes WHERE id = $1`
	result, err := r.db.Exec(query, volume.ID)
	if err != nil {
		return err
	}
	
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("volume not found: %d", volume.ID)
	}
	return nil
}

func (r *VolumeRepository) GetTags(volumeID int) ([]*domain.VolumeTag, error) {
	var tags []*domain.VolumeTag
	query := `SELECT key, value FROM volume_tags WHERE volume_id = $1`
	err := r.db.Select(&tags, query, volumeID)
	return tags, err
}

func (r *VolumeRepository) AddOrUpdateTag(volumeID int, tag *domain.VolumeTag) error {
	query := `
		INSERT INTO volume_tags (volume_id, key, value)
		VALUES ($1, $2, $3)
		ON CONFLICT (volume_id, key) DO UPDATE SET value = EXCLUDED.value
	`
	_, err := r.db.Exec(query, volumeID, tag.Key, tag.Value)
	return err
}

func (r *VolumeRepository) DeleteTag(volumeID int, key string) error {
	query := `DELETE FROM volume_tags WHERE volume_id = $1 AND key = $2`
	_, err := r.db.Exec(query, volumeID, key)
	return err
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}




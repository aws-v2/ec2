// repositories/postgres/volume_repository.go
package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	domain "ec2-api/internal/domain/instance"

	"github.com/google/uuid"
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
		INSERT INTO volumes (volume_name, name, size, format, type, availability_zone, status, attached_to, user_id, internal_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
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
		volume.UserID,
		volume.InternalName,
	).Scan(&volume.ID, &volume.CreatedAt, &volume.UpdatedAt)

	return err
}

func (r *VolumeRepository) FindByID(id int) (*domain.Volume, error) {
	var volume domain.Volume

	fmt.Println(id)

	query := `
SELECT id, volume_name, name, size, format, type, availability_zone, status,
       sha256,
       COALESCE(device_path, '') as device_path,
       COALESCE(attached_to, '') as attached_to,
       COALESCE(reserved_to, '') as reserved_to,
       reserved_at,
       reservation_expires_at,
       COALESCE(internal_name, '') as internal_name,
       COALESCE(latest_reference, '') as latest_reference,
       COALESCE(user_id, '') as user_id,
       created_at, updated_at
FROM volumes WHERE id = $1`

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
		FROM volumes where status not in ('DELETED')
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
		    device_path = $10, reserved_to = $11, reserved_at = $12,
		    reservation_expires_at = $13, sha256 = $14, latest_reference = $15,
		    updated_at = now()
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
		nullString(volume.ReservedTo),
		volume.ReservedAt,
		volume.ReservationExpiresAt,
		nullString(volume.SHA256),
		nullString(volume.LatestReference),
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


func (r *VolumeRepository) FindByAllVolumeSnapshot() ([]*domain.VolumeSnapshot, error) {


    var snapshots []*domain.VolumeSnapshot

    query := `
        SELECT
            id,
            volume_id,
            name,
            description,
            status,
            size,
            snapshot_path,
            sha256,
            storage_key,
            created_at,
            updated_at
        FROM volume_snapshots
        ORDER BY created_at DESC
    `

    err := r.db.Select(&snapshots, query)
    if err != nil {
        log.Printf("This one was called %v", err)
    }

    return snapshots, err

}
func (r *VolumeRepository) FindByVolumeSnapshotByVolumeID(
    volumeID *int,
) ([]*domain.VolumeSnapshot, error) {

    var snapshots []*domain.VolumeSnapshot

    query := `
        SELECT
            id,
            volume_id,
            name,
            description,
            status,
            size,
            snapshot_path,
            sha256,
            storage_key,
            created_at,
            updated_at
        FROM volume_snapshots
        WHERE volume_id = $1
        ORDER BY created_at DESC
    `

    err := r.db.Select(&snapshots, query, *volumeID)
    if err != nil {
        log.Printf("This one was called %v", err)
    }

    return snapshots, err
}
func (r *VolumeRepository) DeleteVolumeSnapshot(
	snapshotID string,
) error {

	query := `DELETE FROM volume_snapshots WHERE id = $1`

	result, err := r.db.Exec(query, snapshotID)
	if err != nil {
		return fmt.Errorf("failed to delete volume snapshot %s: %w", snapshotID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected for volume snapshot %s: %w", snapshotID, err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("volume snapshot %s not found", snapshotID)
	}

	return nil
}
func (r *VolumeRepository) FindVolumeSnapshotByID(
	id string,
) (*domain.VolumeSnapshot, error) {

	query := `
		SELECT id, volume_id, name, description, status, size,
		       COALESCE(snapshot_path, '') as snapshot_path,
		       COALESCE(sha256, '') as sha256,
		       COALESCE(storage_key, '') as storage_key,
		       created_at, updated_at
		FROM volume_snapshots
		WHERE id = $1
	`

	row := r.db.QueryRow(query, id)

	var snap domain.VolumeSnapshot
	err := row.Scan(
		&snap.ID,
		&snap.VolumeID,
		&snap.Name,
		&snap.Description,
		&snap.Status,
		&snap.Size,
		&snap.SnapshotPath,
		&snap.SHA256,
		&snap.StorageKey,
		&snap.CreatedAt,
		&snap.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find volume snapshot by id %s: %w", id, err)
	}

	return &snap, nil
}




func (r *VolumeRepository) UpdateVolumeSnapshot(snapshot *domain.VolumeSnapshot) error {
	query := `
		UPDATE volume_snapshots
		SET status = $2, snapshot_path = $3, storage_key = $4, sha256 = $5, updated_at = now()
		WHERE id = $1
		RETURNING updated_at
	`
	err := r.db.QueryRow(query,
		snapshot.ID,
		snapshot.Status,
		snapshot.SnapshotPath,
		snapshot.StorageKey,
		snapshot.SHA256,
	).Scan(&snapshot.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("volume snapshot not found: %s", snapshot.ID)
	}
	return err
}

func (r *VolumeRepository) UpdateVolumeStatus(
    volumeId int,
    status domain.VolumeStatus,
) error {

    query := `
        UPDATE volumes
        SET
            status = $1,
            updated_at = CURRENT_TIMESTAMP
        WHERE id = $2
    `

    result, err := r.db.Exec(
        query,
        status,
        volumeId,
    )
    if err != nil {
        return fmt.Errorf(
            "failed to update volume snapshot status: %w",
            err,
        )
    }

    rowsAffected, err := result.RowsAffected()
    if err != nil {
        return fmt.Errorf(
            "failed to check volume snapshot update: %w",
            err,
        )
    }

    if rowsAffected == 0 {
        return fmt.Errorf(
            "volume snapshot not found: %s",
            volumeId,
        )
    }

    return nil
}


func (r *VolumeRepository) UpdateVolumeSnapshotStatus(
    snapshotID uuid.UUID,
    status domain.VolumeSnapshotStatus,
) error {

    query := `
        UPDATE volume_snapshots
        SET
            status = $1,
            updated_at = CURRENT_TIMESTAMP
        WHERE id = $2
    `

    result, err := r.db.Exec(
        query,
        status,
        snapshotID,
    )
    if err != nil {
        return fmt.Errorf(
            "failed to update volume snapshot status: %w",
            err,
        )
    }

    rowsAffected, err := result.RowsAffected()
    if err != nil {
        return fmt.Errorf(
            "failed to check volume snapshot update: %w",
            err,
        )
    }

    if rowsAffected == 0 {
        return fmt.Errorf(
            "volume snapshot not found: %s",
            snapshotID,
        )
    }

    return nil
}




func (r *VolumeRepository) CreateVolumeSnapshot(
    snapshot *domain.VolumeSnapshot,
) error {

    query := `
        INSERT INTO volume_snapshots (
            id,
            volume_id,
            name,
            description,
            status,
            size,
            snapshot_path,
            sha256,
            storage_key,
            created_at,
            updated_at
        )
        VALUES (
            $1,
            $2,
            $3,
            $4,
            $5,
            $6,
            $7,
            $8,
            $9,
            NOW(),
            NOW()
        )
        RETURNING created_at, updated_at
    `

    err := r.db.QueryRow(
        query,
        snapshot.ID,
        snapshot.VolumeID,
        snapshot.Name,
        snapshot.Description,
        snapshot.Status,
        snapshot.Size,
        snapshot.SnapshotPath,
        snapshot.SHA256,
        snapshot.StorageKey,
    ).Scan(
        &snapshot.CreatedAt,
        &snapshot.UpdatedAt,
    )

    if err != nil {
        return fmt.Errorf("failed to create volume snapshot: %w", err)
    }

    return nil
}
func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}


func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}
package postgres

import (
	"database/sql"
	"time"

	domain "ec2-api/internal/domain/instance"

	"ec2-api/internal/interfaces"

	"github.com/jmoiron/sqlx"
)

type snapshotRepository struct {
	db *sqlx.DB
}

func NewSnapshotRepository(db *sqlx.DB) interfaces.SnapshotRepository {
	return &snapshotRepository{db: db}
}

func (r *snapshotRepository) GetDB() *sqlx.DB {
	return r.db
}
func (r *snapshotRepository) Create(snapshot *domain.Snapshot) error {
	query := `INSERT INTO vm_snapshots (vm_name, snapshot_name, description, user_id, disk_only, status, created_at) 
	          VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`

	snapshot.CreatedAt = time.Now()

	err := r.db.QueryRow(query, snapshot.InstanceID, snapshot.Name, snapshot.Description,
		snapshot.UserID, snapshot.DiskOnly, snapshot.Status, snapshot.CreatedAt).Scan(&snapshot.ID)
	return err
}

func (r *snapshotRepository) FindByID(id string) (*domain.Snapshot, error) {
	var snapshot domain.Snapshot
	query := `SELECT * FROM vm_snapshots WHERE id = $1`

	err := r.db.Get(&snapshot, query, id)
	if err == sql.ErrNoRows {
		return nil, domain.ErrSnapshotNotFound
	}
	return &snapshot, err
}

func (r *snapshotRepository) FindAll(userID string ) ([]*domain.Snapshot, error) {
	var snapshots []*domain.Snapshot
	query := `SELECT * FROM vm_snapshots where user_id=$1 ORDER BY created_at DESC`
	err := r.db.Select(&snapshots, query, userID)
	return snapshots, err
}

func (r *snapshotRepository) FindByInstanceID(instanceID string) ([]*domain.Snapshot, error) {
	var snapshots []*domain.Snapshot
	query := `SELECT * FROM vm_snapshots WHERE vm_name = $1 ORDER BY created_at DESC`
	err := r.db.Select(&snapshots, query, instanceID)
	return snapshots, err
}

func (r *snapshotRepository) FindByVolumeID(volumeID int) ([]*domain.VolumeSnapshot, error) {
	var snapshots []*domain.VolumeSnapshot
	query := `SELECT id, volume_id, name, description, status, created_at, size FROM vm_snapshots WHERE volume_id = $1 ORDER BY created_at DESC`
	err := r.db.Select(&snapshots, query, volumeID)
	return snapshots, err
}

func (r *snapshotRepository) UpdateStatus(id string, status domain.SnapshotStatus) error {
	query := `UPDATE vm_snapshots SET status = $1 WHERE id = $2`
	_, err := r.db.Exec(query, status, id)
	return err
}

func (r *snapshotRepository) Delete(id string) error {
	query := `DELETE FROM vm_snapshots WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}

package postgres

import (
	"database/sql"
	"time"

	"ec2-api/internal/domain"
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
	query := `INSERT INTO snapshots (instance_id, volume_id, name, description, status, created_at, size, user_id) 
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`

	snapshot.CreatedAt = time.Now()

	err := r.db.QueryRow(query, snapshot.InstanceID, snapshot.VolumeID, snapshot.Name, snapshot.Description,
		snapshot.Status, snapshot.CreatedAt, snapshot.Size, snapshot.UserID).Scan(&snapshot.ID)
	return err
}

func (r *snapshotRepository) FindByID(id int) (*domain.Snapshot, error) {
	var snapshot domain.Snapshot
	query := `SELECT * FROM snapshots WHERE id = $1`

	err := r.db.Get(&snapshot, query, id)
	if err == sql.ErrNoRows {
		return nil, domain.ErrSnapshotNotFound
	}
	return &snapshot, err
}

func (r *snapshotRepository) FindAll() ([]*domain.Snapshot, error) {
	var snapshots []*domain.Snapshot
	query := `SELECT * FROM snapshots ORDER BY created_at DESC`
	err := r.db.Select(&snapshots, query)
	return snapshots, err
}

func (r *snapshotRepository) FindByInstanceID(instanceID string) ([]*domain.Snapshot, error) {
	var snapshots []*domain.Snapshot
	query := `SELECT * FROM snapshots WHERE instance_id = $1 ORDER BY created_at DESC`
	err := r.db.Select(&snapshots, query, instanceID)
	return snapshots, err
}

func (r *snapshotRepository) FindByVolumeID(volumeID int) ([]*domain.VolumeSnapshot, error) {
	var snapshots []*domain.VolumeSnapshot
	query := `SELECT id, volume_id, name, description, status, created_at, size FROM snapshots WHERE volume_id = $1 ORDER BY created_at DESC`
	err := r.db.Select(&snapshots, query, volumeID)
	return snapshots, err
}

func (r *snapshotRepository) UpdateStatus(id int, status domain.SnapshotStatus) error {
	query := `UPDATE snapshots SET status = $1 WHERE id = $2`
	_, err := r.db.Exec(query, status, id)
	return err
}

func (r *snapshotRepository) Delete(id int) error {
	query := `DELETE FROM snapshots WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}

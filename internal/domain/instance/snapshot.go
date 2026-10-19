package domain

import (
	"errors"
	"time"
)

var (
	ErrSnapshotNotFound = errors.New("snapshot not found")
)

type SnapshotStatus string

const (
	SnapshotStatusPending  SnapshotStatus = "pending"
	SnapshotStatusReady    SnapshotStatus = "ready"
	SnapshotStatusDeleting SnapshotStatus = "deleting"
	SnapshotStatusFailed   SnapshotStatus = "failed"
)

type Snapshot struct {
	ID          string  `json:"id" db:"id"`
	InstanceID  *string `json:"instance_id" db:"instance_id"`
	VMName   string   `json:"vm_name" db:"vm_name"`
	VolumeID    *int           `json:"volume_id" db:"volume_id"`
	Name        string         `json:"name" db:"name"`
	SnapshotName        string         `json:"snapshot_name" db:"snapshot_name"`
	Description string         `json:"description" db:"description"`
	Status      SnapshotStatus `json:"status" db:"status"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
	Size        int            `json:"size" db:"size"`
	UserID      string         `json:"user_id" db:"user_id"`
	DiskOnly    bool           `json:"disk_only" db:"disk_only"`
}

type CreateSnapshotRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	UserID      string         `json:"user_id" db:"user_id"`

}


 
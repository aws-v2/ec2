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
	ID          int            `json:"id" db:"id"`
	InstanceID  *string        `json:"instance_id" db:"instance_id"`
	VolumeID    *int           `json:"volume_id" db:"volume_id"`
	Name        string         `json:"name" db:"name"`
	Description string         `json:"description" db:"description"`
	Status      SnapshotStatus `json:"status" db:"status"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
	Size        int            `json:"size" db:"size"`
	UserID      string         `json:"user_id" db:"user_id"`
}

type VolumeSnapshot struct {
	ID          int            `json:"id" db:"id"`
	VolumeID    *int           `json:"volume_id" db:"volume_id"`
	Name        string         `json:"name" db:"name"`
	Description string         `json:"description" db:"description"`
	Status      SnapshotStatus `json:"status" db:"status"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
	Size        int            `json:"size" db:"size"`
	UserID      string         `json:"user_id" db:"user_id"`
}

type CreateSnapshotRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type SnapshotRepository interface {
	Create(snapshot *Snapshot) error
	FindByID(id int) (*Snapshot, error)
	FindAll() ([]*Snapshot, error)
	FindByInstanceID(instanceID string) ([]*Snapshot, error)
	FindByVolumeID(volumeID int) ([]*VolumeSnapshot, error)
	UpdateStatus(id int, status SnapshotStatus) error
	Delete(id int) error
}

type SnapshotService interface {
	CreateSnapshot(instanceID string, req *CreateSnapshotRequest) (*Snapshot, error)
	CreateVolumeSnapshot(volumeID int, req *CreateSnapshotRequest) (*VolumeSnapshot, error)
	GetSnapshot(id int) (*Snapshot, error)
	ListSnapshots() ([]*Snapshot, error)
	ListSnapshotsByInstance(instanceID string) ([]*Snapshot, error)
	ListSnapshotsByVolume(volumeID int) ([]*VolumeSnapshot, error)
	DeleteSnapshot(id int) error
}

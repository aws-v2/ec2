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

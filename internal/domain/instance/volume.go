// domain/volume.go
package domain

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"database/sql"
)

type Volume struct {
	ID                   int          `json:"id" db:"id"`
	VolumeName           string       `json:"volume_name" db:"volume_name"`
	Name                 string       `json:"name" db:"name"`
	Size                 int          `json:"size" db:"size"`
	Format               string       `json:"format" db:"format"`
	Type                 string       `json:"type" db:"type"`
	AvailabilityZone     string       `json:"availability_zone" db:"availability_zone"`
	Status               VolumeStatus `json:"status" db:"status"`
	AttachedTo           string       `json:"attached_to" db:"attached_to"`
	DevicePath           string       `json:"device_path" db:"device_path"`
	CreatedAt            time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time    `json:"updated_at" db:"updated_at"`
	UserID               string       `json:"user_id" db:"user_id"`
	ReservedTo           string       `json:"reserved_to" db:"reserved_to"`
	ReservedAt           sql.NullTime `json:"reserved_at,omitempty" db:"reserved_at"`
	ReservationExpiresAt sql.NullTime `json:"reservation_expires_at,omitempty" db:"reservation_expires_at"`
	SHA256               string       `json:"sha256" db:"sha256"`
	LatestReference      string       `json:"latest_reference" db:"latest_reference"`
	InternalName         string       `json:"internal_name" db:"internal_name"`
}

type VolumeStatus string

const (
	VolumeStatusAvailable       VolumeStatus = "available"
	VolumeStatusAttached        VolumeStatus = "attached"
	VolumeStatusCreating        VolumeStatus = "creating"
	VolumeStatusDeleting        VolumeStatus = "deleting"
	VolumeStatusAttaching       VolumeStatus = "attaching"
	VolumeStatusDetaching       VolumeStatus = "detaching"
	VolumeStatusExpanding       VolumeStatus = "expanding"
	VolumeStatusExpandingFailed VolumeStatus = "expanding_failed"
	VolumeStatusReserved        VolumeStatus = "reserved"
)

type CreateVolumeRequest struct {
	Name             string `json:"name" binding:"required"`
	Size             int    `json:"size" binding:"required,min=1,max=1000"`
	Type             string `json:"type" binding:"required"`
	AvailabilityZone string `json:"az" binding:"required"`
}

type AttachVolumeRequest struct {
	InstanceID string `json:"instance_id" binding:"required"`
}

// Helper method to get storage path
// Helper method to get storage path
func (v *Volume) GetStoragePath() (string, error) {
	volumeRoot := "/var/lib/libvirt/images"
	if err := os.MkdirAll(volumeRoot, 0755); err != nil {
		return "", fmt.Errorf("failed to create volume root %s: %w", volumeRoot, err)
	}

	filename := v.VolumeName
	ext := "." + v.Format
	if !strings.HasSuffix(filename, ext) {
		filename = filename + ext
	}

	return fmt.Sprintf("%s/%s", volumeRoot, filename), nil
}

type DiskTarget struct {
	Dev string `xml:"dev,attr"`
	Bus string `xml:"bus,attr"`
}

type Disk struct {
	Type   string `xml:"type,attr"`
	Device string `xml:"device,attr"`
	Source struct {
		File string `xml:"file,attr"`
	} `xml:"source"`
	Target DiskTarget `xml:"target"`
}

type DomainDevices struct {
	Disks []Disk `xml:"disk"`
}

type DomainXML struct {
	Devices DomainDevices `xml:"devices"`
}

type VolumeTag struct {
	Key   string `json:"key" db:"key"`
	Value string `json:"value" db:"value"`
}

type ReservationRequest struct {
	ReservedTo string    `json:"reserved_to" binding:"required"`
	ExpiresAt  time.Time `json:"expires_at" binding:"required"`
}

type CreateVolumeSnapshotRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type VolumeSnapshotStatus string

const (
	VolumeSnapshotStatusPending  VolumeSnapshotStatus = "PENDING"
	VolumeSnapshotStatusCreating VolumeSnapshotStatus = "CREATING"
	VolumeSnapshotStatusReady    VolumeSnapshotStatus = "READY"
	VolumeSnapshotStatusFailed   VolumeSnapshotStatus = "FAILED"
	VolumeSnapshotStatusDeleting VolumeSnapshotStatus = "DELETING"
	VolumeSnapshotStatusDeleted  VolumeSnapshotStatus = "DELETED"
)

type VolumeSnapshot struct {
	ID           uuid.UUID            `json:"id" db:"id"`
	VolumeID     *int                 `json:"volume_id" db:"volume_id"`
	Name         string               `json:"name" db:"name"`
	Description  string               `json:"description" db:"description"`
	Status       VolumeSnapshotStatus `json:"status" db:"status"`
	Size         int                  `json:"size" db:"size"`
	SnapshotPath *string              `json:"snapshot_path,omitempty" db:"snapshot_path"`
	SHA256       *string              `json:"sha256,omitempty" db:"sha256"`
	StorageKey   *string              `json:"storage_key,omitempty" db:"storage_key"`
	CreatedAt    time.Time            `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at" db:"updated_at"`
}

type CreateVolumeFromSnapshotRequest struct {
	SnapshotID string    `json:"snapshot_id" binding:"required"`
	VolumeName string `json:"volume_name" binding:"required"`
}

// subject := fmt.Sprintf("%s.s3.task.create_presigned_url", c.natsPrefix)

type CreatePresignedURLRequest struct {
	UserID     string `json:"user_id"`
	GameID     string `json:"game_id,omitempty"`
	AssetID    string `json:"asset_id"`
	AssetType  string `json:"asset_type"`  // "game" | "template"
	AssetName  string `json:"asset_name"`  // this is the nameof the job/game/render job this asset belongs to like kalshi or ruto tracker
	BucketName string `json:"bucket_name"` // this is the nameof the job/game/render job this asset belongs to like kalshi or ruto tracker
	Key        string `json:"key"`

	Sha256 string `json:"sha256"`
}

// downloadTemplateSubj := fmt.Sprintf("%s.s3.task.create_presign_download_url", c.natsPrefix)

type CreatePresignDownloadURLRequest struct {
	UserID        string `json:"user_id"`
	CorrelationID string `json:"correlation_id"`
	FileSha256    string `json:"sha256"`
	AssetID       string `json:"asset_id"`
	FileCount     int    `json:"file_count"`
}

type DeleteFileNatsRequest struct {
	Sha256 string `json:"sha_256"`
}

type DeleteFileS3Resp struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}


	type AllSnapshots struct{
		VolumeSnapshots []*VolumeSnapshot `json:"volume_snapshots"`
		InstanceSnapshots []*Snapshot `json:"instance_snapshots"`
	}
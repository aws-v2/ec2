// domain/volume.go
package domain

import (
	"fmt"
	"time"
)

type Volume struct {
	ID               int       `json:"id" db:"id"`
	VolumeName       string    `json:"volume_name" db:"volume_name"`
	Name             string    `json:"name" db:"name"`
	Size             int       `json:"size" db:"size"`
	Format           string    `json:"format" db:"format"`
	Type             string    `json:"type" db:"type"`
	AvailabilityZone string    `json:"availability_zone" db:"availability_zone"`
	Status           string    `json:"status" json:"Lifecycle_State" db:"status"`
	AttachedTo       string    `json:"attached_to" db:"attached_to"`
	DevicePath       string    `json:"device_path" db:"device_path"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
}

const (
	VolumeStatusAvailable = "available"
	VolumeStatusAttached  = "attached"
	VolumeStatusCreating  = "creating"
	VolumeStatusDeleting  = "deleting"
	VolumeStatusAttaching = "attaching"
	VolumeStatusDetaching = "detaching"
	VolumeStatusExpanding = "expanding"
	VolumeStatusReserved  = "reserved"
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
func (v *Volume) GetStoragePath() string {
	return fmt.Sprintf("/var/lib/libvirt/images/%s.%s", v.VolumeName, v.Format)
}
 



type DiskTarget struct {
	Dev string `xml:"dev,attr"`
	Bus string `xml:"bus,attr"`
}

type Disk struct {
	Type   string     `xml:"type,attr"`
	Device string     `xml:"device,attr"`
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
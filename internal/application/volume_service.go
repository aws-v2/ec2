// application/volume_service.go
package application

import (
	"fmt"

	"ec2-api/internal/domain"
	"ec2-api/internal/interfaces"
	"ec2-api/internal/libvirt"

	"github.com/google/uuid"
)

type VolumeService struct {
	repo          interfaces.VolumeRepository
	instanceRepo  interfaces.InstanceRepository
	libvirtClient *libvirt.LibvirtClient
}

func NewVolumeService(
	repo interfaces.VolumeRepository,
	instanceRepo interfaces.InstanceRepository,
	libvirtClient *libvirt.LibvirtClient,
) *VolumeService {
	return &VolumeService{
		repo:          repo,
		instanceRepo:  instanceRepo,
		libvirtClient: libvirtClient,
	}
}

func (s *VolumeService) CreateVolume(req *domain.CreateVolumeRequest) (*domain.Volume, error) {
	volumeName := fmt.Sprintf("vol-%s", uuid.New().String()[:8])

	volume := &domain.Volume{
		VolumeName:       volumeName,
		Name:             req.Name,
		Size:             req.Size,
		Format:           "qcow2", // Internal default
		Type:             req.Type,
		AvailabilityZone: req.AvailabilityZone,
		Status:           domain.VolumeStatusCreating,
	}

	if err := s.repo.Create(volume); err != nil {
		return nil, fmt.Errorf("failed to save volume: %w", err)
	}
	go s.createVolumeAsync(volume)

	return volume, nil
}

func (s *VolumeService) createVolumeAsync(volume *domain.Volume) {
	storagePath := volume.GetStoragePath()
	err := s.libvirtClient.CreateVolume(storagePath, volume.Size)
	if err != nil {
		fmt.Printf("Failed to create volume %d: %v\n", volume.ID, err)
		volume.Status = domain.VolumeStatusDeleting
		s.repo.Update(volume)
		return
	}
	volume.Status = domain.VolumeStatusAvailable
	s.repo.Update(volume)
}

func (s *VolumeService) ListVolumes() ([]*domain.Volume, error) {
	// TODO: Fire event: volume_list_started
	volumes, err := s.repo.FindAll()
	if err != nil {
		// TODO: Fire event: volume_list_db_failed
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}

	// TODO: Fire event: volume_list_completed
	return volumes, nil
}

func (s *VolumeService) GetVolume(volumeID int) (*domain.Volume, error) {
	// TODO: Fire event: volume_get_started
	volume, err := s.repo.FindByID(volumeID)
	if err != nil {
		// TODO: Fire event: volume_get_db_failed
		return nil, fmt.Errorf("failed to get volume: %w", err)
	}
	if volume == nil {
		// TODO: Fire event: volume_not_found
		return nil, fmt.Errorf("volume not found: %d", volumeID)
	}

	// TODO: Fire event: volume_get_completed
	return volume, nil
}

func (s *VolumeService) ReserveVolume(volumeID int) (*domain.Volume, error) {
	volume, err := s.repo.FindByID(volumeID)
	if err != nil {
		return nil, fmt.Errorf("failed to find volume for reservation: %w", err)
	}
	if volume == nil {
		return nil, fmt.Errorf("volume not found: %d", volumeID)
	}

	volume.Status = domain.VolumeStatusReserved
	if err := s.repo.Update(volume); err != nil {
		return nil, fmt.Errorf("failed to update volume status to reserved: %w", err)
	}

	return volume, nil
}

func (s *VolumeService) ExpandVolume(volumeID int, newSize int) (*domain.Volume, error) {
	volume, err := s.repo.FindByID(volumeID)
	if err != nil || volume == nil {
		return nil, fmt.Errorf("volume not found: %d", volumeID)
	}

	if newSize <= volume.Size {
		return nil, fmt.Errorf("new size must be greater than current size (%d GB)", volume.Size)
	}

	volume.Status = domain.VolumeStatusExpanding
	s.repo.Update(volume)

	go func() {
		storagePath := volume.GetStoragePath()
		if err := s.libvirtClient.ResizeVolume(storagePath, newSize); err != nil {
			fmt.Printf("Failed to resize volume %d: %v\n", volume.ID, err)
			volume.Status = domain.VolumeStatusAvailable
			s.repo.Update(volume)
			return
		}

		volume.Size = newSize
		volume.Status = domain.VolumeStatusAvailable
		s.repo.Update(volume)
		s.repo.UpdateSize(volume.ID, newSize)
		fmt.Printf("✓ Volume %d expanded to %d GB\n", volume.ID, newSize)
	}()

	return volume, nil
}

func (s *VolumeService) AttachVolume(volumeID int, instanceID string) (*domain.Volume, error) {
	// Update status to attaching
	volume, err := s.repo.FindByID(volumeID)
	if err != nil || volume == nil {
		return nil, fmt.Errorf("volume not found: %d", volumeID)
	}

	if volume.Status != domain.VolumeStatusAvailable {
		return nil, fmt.Errorf("volume is not available, current status: %s", volume.Status)
	}

	instance, err := s.instanceRepo.FindByID(instanceID)
	if err != nil || instance == nil {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}

	volume.Status = domain.VolumeStatusAttaching
	s.repo.Update(volume)

	attachedDevices, err := s.libvirtClient.ListAttachedDevices(instance.VMName)
	if err != nil {
		volume.Status = domain.VolumeStatusAvailable
		s.repo.Update(volume)
		return nil, fmt.Errorf("failed to list attached devices: %w", err)
	}

	devicePath := getNextDevicePathFromLibvirt(attachedDevices)
	storagePath := volume.GetStoragePath()

	err = s.libvirtClient.AttachVolume(instance.VMName, storagePath, devicePath, volume.Format)
	if err != nil {
		volume.Status = domain.VolumeStatusAvailable
		s.repo.Update(volume)
		return nil, fmt.Errorf("failed to attach volume: %w", err)
	}

	volume.AttachedTo = instanceID
	volume.DevicePath = devicePath
	volume.Status = domain.VolumeStatusAttached
	if err := s.repo.Update(volume); err != nil {
		s.libvirtClient.DetachVolume(instance.VMName, devicePath)
		return nil, fmt.Errorf("failed to update volume status: %w", err)
	}

	return volume, nil
}

func (s *VolumeService) DetachVolume(volumeID int) (*domain.Volume, error) {
	volume, err := s.repo.FindByID(volumeID)
	if err != nil || volume == nil {
		return nil, fmt.Errorf("volume not found: %d", volumeID)
	}

	if volume.Status != domain.VolumeStatusAttached {
		return nil, fmt.Errorf("volume is not attached, current status: %s", volume.Status)
	}

	instance, err := s.instanceRepo.FindByID(volume.AttachedTo)
	if err != nil || instance == nil {
		return nil, fmt.Errorf("instance not found: %s", volume.AttachedTo)
	}

	// Safety Check: Root volume safety
	if volume.DevicePath == "/dev/vda" || volume.DevicePath == "/dev/sda1" {
		if instance.Status == domain.StatusRunning {
			return nil, fmt.Errorf("cannot detach root volume while instance is running")
		}
	}

	volume.Status = domain.VolumeStatusDetaching
	s.repo.Update(volume)

	err = s.libvirtClient.DetachVolume(instance.VMName, volume.DevicePath)
	if err != nil {
		volume.Status = domain.VolumeStatusAttached
		s.repo.Update(volume)
		return nil, fmt.Errorf("failed to detach volume: %w", err)
	}

	volume.AttachedTo = ""
	volume.DevicePath = ""
	volume.Status = domain.VolumeStatusAvailable
	if err := s.repo.Update(volume); err != nil {
		return nil, fmt.Errorf("failed to update volume status: %w", err)
	}

	return volume, nil
}

func (s *VolumeService) DeleteVolume(volumeID int, force bool) error {
	// TODO: Fire event: volume_delete_started

	// Get volume
	volume, err := s.repo.FindByID(volumeID)
	if err != nil || volume == nil {
		// TODO: Fire event: volume_not_found_for_delete
		return fmt.Errorf("volume not found: %d", volumeID)
	}

	// Check volume status
	if volume.Status == domain.VolumeStatusAttached && !force {
		// TODO: Fire event: volume_delete_failed_attached
		return fmt.Errorf("cannot delete attached volume, detach first or use force")
	}

	// If forced and attached, we should attempt to detach from libvirt first if possible
	if volume.Status == domain.VolumeStatusAttached && force && volume.AttachedTo != "" {
		instance, err := s.instanceRepo.FindByID(volume.AttachedTo)
		if err == nil && instance != nil {
			s.libvirtClient.DetachVolume(instance.VMName, volume.DevicePath)
		}
	}

	// TODO: Fire event: validation_passed_for_delete

	storagePath := volume.GetStoragePath()

	// Delete volume using libvirt
	err = s.libvirtClient.DeleteVolume(storagePath)
	if err != nil {
		// TODO: Fire event: libvirt_delete_failed
		return fmt.Errorf("failed to delete volume file: %w", err)
	}

	// TODO: Fire event: libvirt_delete_completed

	// Delete volume from database
	if err := s.repo.Delete(volume); err != nil {
		// TODO: Fire event: volume_delete_db_failed
		return fmt.Errorf("failed to delete volume from database: %w", err)
	}

	// TODO: Fire event: volume_delete_completed
	return nil
}

func getNextDevicePathFromLibvirt(used []string) string {
	usedSet := map[string]bool{}
	for _, u := range used {
		usedSet[u] = true
	}

	// Typical pattern: /dev/vda, /dev/vdb, /dev/vdc, ...
	for c := 'a'; c <= 'z'; c++ {
		path := fmt.Sprintf("/dev/vd%c", c)
		if !usedSet[path] {
			return path
		}
	}

	return "/dev/vdz"
}

func (s *VolumeService) GetTags(volumeID int) ([]*domain.VolumeTag, error) {
	return s.repo.GetTags(volumeID)
}

func (s *VolumeService) AddOrUpdateTag(volumeID int, tag *domain.VolumeTag) error {
	return s.repo.AddOrUpdateTag(volumeID, tag)
}

func (s *VolumeService) DeleteTag(volumeID int, key string) error {
	return s.repo.DeleteTag(volumeID, key)
}

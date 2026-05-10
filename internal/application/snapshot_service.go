package application

import (
	"fmt"
	"time"

	domain "ec2-api/internal/domain/instance"
	interfaces "ec2-api/internal/interfaces"
	libvirt "ec2-api/internal/infra/libvirt"
)

type SnapshotService struct {
	repo          interfaces.SnapshotRepository
	instanceRepo  interfaces.InstanceRepository
	volumeRepo    interfaces.VolumeRepository
	libvirtClient *libvirt.LibvirtClient
	hostRepo      interfaces.HostRepository
}

func NewSnapshotService(repo interfaces.SnapshotRepository, instanceRepo interfaces.InstanceRepository, volumeRepo interfaces.VolumeRepository, libvirt *libvirt.LibvirtClient, hostRepo interfaces.HostRepository) *SnapshotService {
	return &SnapshotService{
		repo:          repo,
		instanceRepo:  instanceRepo,
		volumeRepo:    volumeRepo,
		libvirtClient: libvirt,
		hostRepo:      hostRepo,
	}
}

func (s *SnapshotService) CreateVolumeSnapshot(volumeID int, req *domain.CreateSnapshotRequest) (*domain.VolumeSnapshot, error) {
	volume, err := s.volumeRepo.FindByID(volumeID)
	if err != nil || volume == nil {
		return nil, fmt.Errorf("volume not found: %d", volumeID)
	}

	snapshotName := req.Name
	if snapshotName == "" {
		snapshotName = fmt.Sprintf("snap-%d", time.Now().Unix())
	}

	snapshot := &domain.Snapshot{
		VolumeID:    &volumeID,
		Name:        snapshotName,
		Description: req.Description,
		Status:      domain.SnapshotStatusPending,
		Size:        volume.Size,
	}

	if err := s.repo.Create(snapshot); err != nil {
		return nil, fmt.Errorf("failed to save snapshot metadata: %w", err)
	}

	if volume.Status == domain.VolumeStatusAttached && volume.AttachedTo != "" {
		instance, err := s.instanceRepo.FindByID(volume.AttachedTo)
		if err == nil && instance != nil {
			var remoteHostIP, remoteHostUser string
			if instance.HostID != "" {
				host, _ := s.hostRepo.GetByID(instance.HostID)
				if host != nil {
					remoteHostIP = host.IP
					remoteHostUser = host.SSHUser
				}
			}
			if err := s.libvirtClient.CreateSnapshot(remoteHostIP, remoteHostUser, instance.VMName, snapshot.Name, snapshot.Description); err != nil {
				s.repo.UpdateStatus(snapshot.ID, domain.SnapshotStatusFailed)
				return nil, fmt.Errorf("failed to create libvirt snapshot for attached volume: %w", err)
			}
		}
	}

	s.repo.UpdateStatus(snapshot.ID, domain.SnapshotStatusReady)

	return &domain.VolumeSnapshot{
		ID:          snapshot.ID,
		VolumeID:    &volumeID,
		Name:        snapshot.Name,
		Description: snapshot.Description,
		Status:      domain.SnapshotStatusReady,
		CreatedAt:   snapshot.CreatedAt,
		Size:        snapshot.Size,
	}, nil
}

func (s *SnapshotService) CreateSnapshot(instanceID string, req *domain.CreateSnapshotRequest) (*domain.Snapshot, error) {
	instance, err := s.instanceRepo.FindByID(instanceID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}

	snapshotName := req.Name
	if snapshotName == "" {
		snapshotName = fmt.Sprintf("snap-%d", time.Now().Unix())
	}

	snapshot := &domain.Snapshot{
		InstanceID:  &instanceID,
		Name:        snapshotName,
		Description: req.Description,
		Status:      domain.SnapshotStatusPending,
	}

	if err := s.repo.Create(snapshot); err != nil {
		return nil, fmt.Errorf("failed to save snapshot metadata: %w", err)
	}

	// Fetch host IP
	var remoteHostIP, remoteHostUser string
	if instance.HostID != "" {
		host, _ := s.hostRepo.GetByID(instance.HostID)
		if host != nil {
			remoteHostIP = host.IP
			remoteHostUser = host.SSHUser
		}
	}

	// Create snapshot in Libvirt
	if err := s.libvirtClient.CreateSnapshot(remoteHostIP, remoteHostUser, instance.VMName, snapshot.Name, snapshot.Description); err != nil {
		s.repo.UpdateStatus(snapshot.ID, domain.SnapshotStatusFailed)
		return nil, fmt.Errorf("failed to create libvirt snapshot: %w", err)
	}

	s.repo.UpdateStatus(snapshot.ID, domain.SnapshotStatusReady)
	snapshot.Status = domain.SnapshotStatusReady
	return snapshot, nil
}

func (s *SnapshotService) GetSnapshot(id int) (*domain.Snapshot, error) {
	return s.repo.FindByID(id)
}

func (s *SnapshotService) ListSnapshots() ([]*domain.Snapshot, error) {
	return s.repo.FindAll()
}

func (s *SnapshotService) ListSnapshotsByInstance(instanceID string) ([]*domain.Snapshot, error) {
	return s.repo.FindByInstanceID(instanceID)
}

func (s *SnapshotService) ListSnapshotsByVolume(volumeID int) ([]*domain.VolumeSnapshot, error) {
	return s.repo.FindByVolumeID(volumeID)
}

func (s *SnapshotService) DeleteSnapshot(id int) error {
	snapshot, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	if snapshot.InstanceID == nil {
		return fmt.Errorf("snapshot is not linked to an instance")
	}

	instance, err := s.instanceRepo.FindByID(*snapshot.InstanceID)
	if err != nil {
		return err
	}

	var remoteHostIP, remoteHostUser string
	if instance.HostID != "" {
		host, _ := s.hostRepo.GetByID(instance.HostID)
		if host != nil {
			remoteHostIP = host.IP
			remoteHostUser = host.SSHUser
		}
	}

	if err := s.libvirtClient.DeleteSnapshot(remoteHostIP, remoteHostUser, instance.VMName, snapshot.Name); err != nil {
		return fmt.Errorf("failed to delete libvirt snapshot: %w", err)
	}

	return s.repo.Delete(id)
}

func (s *SnapshotService) DeleteVolumeSnapshot(id int) error {
	snapshot, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	if snapshot.VolumeID == nil {
		return fmt.Errorf("snapshot is not linked to a volume")
	}

	// Future enhancement: if volume snapshots map directly to libvirt snapshots when attached,
	// or specific disk files, cleanup logic would go here.

	// For now, we simply remove the record from the database.
	return s.repo.Delete(id)
}

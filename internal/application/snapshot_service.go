package application

import (
	"fmt"
	"log"
	"time"

	domain "ec2-api/internal/domain/instance"
	libvirt "ec2-api/internal/infra/libvirt"
	interfaces "ec2-api/internal/interfaces"

	"github.com/google/uuid"
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
		DiskOnly:    true,
	}
	snapshot.UserID = req.UserID
	snapshot.ID = uuid.New().String()

	if err := s.repo.Create(snapshot); err != nil {
		return nil, fmt.Errorf("failed to save snapshot metadata: %w", err)
	}

	// Fetch host IP
	var remoteHostIP, remoteHostUser, remoteHostKey string
	if instance.HostID != "" {
		host, _ := s.hostRepo.GetByID(instance.HostID)
		if host != nil {
			remoteHostIP = host.IP
			remoteHostUser = host.SSHUser
			remoteHostKey = host.SSHPrivateKey
		}
	}
	// Create snapshot in Libvirt
	if err := s.libvirtClient.CreateSnapshot(remoteHostIP, remoteHostUser, remoteHostKey, instance.VMName, snapshot.Name, snapshot.Description); err != nil {
		s.repo.UpdateStatus(snapshot.ID, domain.SnapshotStatusFailed)
		return nil, fmt.Errorf("failed to create libvirt snapshot: %w", err)
	}

	s.repo.UpdateStatus(snapshot.ID, domain.SnapshotStatusReady)
	snapshot.Status = domain.SnapshotStatusReady
	return snapshot, nil
}

func (s *SnapshotService) GetSnapshot(id string) (*domain.Snapshot, error) {
	return s.repo.FindByID(id)
}

func (s *SnapshotService) ListSnapshots(userID string) (*domain.AllSnapshots, error) {
	var allSnapshots domain.AllSnapshots

	volumeSnapshots, err := s.volumeRepo.FindByAllVolumeSnapshot()
	if err != nil {
		log.Printf("Failed to get all volume snapshots")
		return nil, err
	}

	allSnapshots.VolumeSnapshots = volumeSnapshots

	isntanceSnapshots, err := s.repo.FindAll(userID)
	allSnapshots.InstanceSnapshots = isntanceSnapshots
	if err != nil {
		log.Printf("Failed to get all instance snapshots")
		return nil, err
	}
	return &allSnapshots, nil
}

func (s *SnapshotService) ClearSnapshot(snapshotId string) error {
	snapshot, err := s.repo.FindByID(snapshotId)
	if err != nil {
		return fmt.Errorf("failed to find snapshot %s: %w", snapshotId, err)
	}
	if snapshot == nil {
		return fmt.Errorf("snapshot %s not found", snapshotId)
	}

	instance, err := s.instanceRepo.FindByID(*snapshot.InstanceID)
	if err != nil {
		return fmt.Errorf("failed to resolve host for vm %s: %w", snapshot.InstanceID, err)
	}

	host, err := s.hostRepo.GetByID(instance.HostID)

	if err != nil {
		return fmt.Errorf("failed to resolve host for vm %s: %w", snapshot.InstanceID, err)
	}

	if err := s.libvirtClient.DeleteSnapshot(host.IP, host.SSHUser, host.SSHPrivateKey, instance.VMName, snapshot.Name); err != nil {
		return fmt.Errorf("failed to delete snapshot on hypervisor: %w", err)
	}

	if err := s.repo.Delete(snapshot.ID); err != nil {
		log.Printf("[Service:ClearSnapshot] WARNING: libvirt snapshot deleted but DB record cleanup failed for %s/%s: %v",
			snapshot.Name, snapshot.ID, err)
		return fmt.Errorf("snapshot deleted but failed to remove record: %w", err)
	}

	return nil
}

func (s *SnapshotService) RevertSnapshot(snapshotId string) error {
	snapshot, err := s.repo.FindByID(snapshotId)
	if err != nil {
		return fmt.Errorf("failed to find snapshot %s: %w", snapshotId, err)
	}
	if snapshot == nil {
		return fmt.Errorf("snapshot %s not found", snapshotId)
	}
log.Printf("the snapshot we got; %+v", snapshot)


	instance, err := s.instanceRepo.FindByID(snapshot.VMName)
	if err != nil {
		return fmt.Errorf("failed to resolve host for vm %s: %w", snapshot.InstanceID, err)
	}

	host, err := s.hostRepo.GetByID(instance.HostID)

	if err != nil {
		return fmt.Errorf("failed to resolve host for vm %s: %w", snapshot.InstanceID, err)
	}

	if err := s.repo.UpdateStatus(snapshotId, "REVERTING"); err != nil {
		log.Printf("[Service:RevertSnapshot] failed to update snapshot status: %v", err)
	}



	if err := s.libvirtClient.RevertToSnapshot(host.IP, host.SSHUser, host.SSHPrivateKey, instance.VMName, snapshot.SnapshotName); err != nil {
		s.repo.UpdateStatus(snapshotId, "FAILED")
		return fmt.Errorf("failed to revert snapshot on hypervisor: %w", err)
	}

	if err := s.repo.UpdateStatus(snapshotId, "READY"); err != nil {
		log.Printf("[Service:RevertSnapshot] failed to reset snapshot status after revert: %v", err)
	}

	if err := s.repo.UpdateStatus(snapshot.ID, "STOPPED"); err != nil {
		log.Printf("[Service:RevertSnapshot] failed to update vm status after revert: %v", err)
	}

	return nil
}

func (s *SnapshotService) ListSnapshotsByInstance(instanceID string) (*domain.AllSnapshots, error) {
	instanceSnapshots, err := s.repo.FindByInstanceID(instanceID)

	if err != nil {
		log.Printf("Failed to get all instance snapshots")
		return nil, err
	}

	return &domain.AllSnapshots{
		InstanceSnapshots: instanceSnapshots,
	}, nil

}

func (s *SnapshotService) DeleteSnapshot(id string) error {
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

	var remoteHostIP, remoteHostUser, remoteHostKey string
	if instance.HostID != "" {
		host, _ := s.hostRepo.GetByID(instance.HostID)
		if host != nil {
			remoteHostIP = host.IP
			remoteHostUser = host.SSHUser
			remoteHostKey = host.SSHPrivateKey
		}
	}

	if err := s.libvirtClient.DeleteSnapshot(remoteHostIP, remoteHostUser, remoteHostKey, instance.VMName, snapshot.Name); err != nil {
		return fmt.Errorf("failed to delete libvirt snapshot: %w", err)
	}

	return s.repo.Delete(id)
}

func (s *SnapshotService) DeleteVolumeSnapshot(id string) error {
	// snapshot, err := s.repo.FindByID(id)
	// if err != nil {
	// 	return err
	// }

	// if snapshot.VolumeID == nil {
	// 	return fmt.Errorf("snapshot is not linked to a volume")
	// }

	// // Future enhancement: if volume snapshots map directly to libvirt snapshots when attached,
	// // or specific disk files, cleanup logic would go here.

	// // For now, we simply remove the record from the database.
	// return s.repo.Delete(id)
	return fmt.Errorf("Method not implemented")
}

package application

import (
	"context"
	dto "ec2-api/internal/domain/dto"
	domain "ec2-api/internal/domain/instance"
	libvirt "ec2-api/internal/infra/libvirt"
	messaging "ec2-api/internal/infra/messaging"
	storage "ec2-api/internal/infra/storage"
	interfaces "ec2-api/internal/interfaces"
	"ec2-api/internal/vpcpkg"

	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type InstanceService struct {
	repo          interfaces.InstanceRepository
	sgService     interfaces.SecurityGroupService
	libvirtClient *libvirt.LibvirtClient
	systemPubKey  string
	imagesDir     string
	publisher     messaging.Publisher
	minioAdapter  *storage.MinIOAdapter
	vpcService    *vpcpkg.Service   // ← replaces publisher
	hostService   *HostService
}

func NewInstanceService(
	repo interfaces.InstanceRepository,
	sgService interfaces.SecurityGroupService,
	libvirt *libvirt.LibvirtClient,
	systemPubKey string,
	imagesDir string,
	publisher messaging.Publisher,
	minioAdapter *storage.MinIOAdapter,
	vpcService *vpcpkg.Service,
	hostService *HostService,
) *InstanceService {
	s := &InstanceService{
		repo:          repo,
		sgService:     sgService,
		libvirtClient: libvirt,
		systemPubKey:  systemPubKey,
		imagesDir:     imagesDir,
		publisher:     publisher,
		minioAdapter:  minioAdapter,
		vpcService:    vpcService,
		hostService:   hostService,
	}

	return s
}

const (
	StageCloningDisk        = "CLONING_DISK"
	StageProvisioned        = "PROVISIONED"
	StageDownloadingPayload = "DOWNLOADING_PAYLOAD"
	StageUnzippingPayload   = "UNZIPPING_PAYLOAD"
	StageInjectingPayload   = "INJECTING_PAYLOAD"
	StageStartingVM         = "STARTING_VM"
	StageCompleted          = "COMPLETED"
	StageFailed             = "FAILED"
)




var imageMap = map[string]string{
	// Ubuntu LTS versions
	"ubuntu-20.04": "ubuntu-20.04.qcow2",
	"ubuntu-22.04": "ubuntu-22.04.qcow2",
	"ubuntu-24.04": "ubuntu-24.04.qcow2",
	// Debian
	"debian-11": "debian-11.qcow2",
	"debian-12": "debian-12.qcow2",
	"rocky-8":     "rocky-8.qcow2",
	"rocky-9":     "rocky-9.qcow2",
	"almalinux-8": "almalinux-8.qcow2",
	"almalinux-9": "almalinux-9.qcow2",
	"fedora-39": "fedora-39.qcow2",
	"fedora-40": "fedora-40.qcow2",
	"centos-stream-9": "centos-stream-9.qcow2",
}


// ─── SSH Key Pair ─────────────────────────────────────────────────────────────

type SSHKeyPair struct {
	PrivateKeyPEM string // stored in DB → fed to agent at terminal time
	PublicKeyAuth string // "ssh-ed25519 AAAA..." → fed to cloud-init authorized_keys
}


// CreateInstance — unchanged signature, Step 2 now calls vpcService directly.
func (s *InstanceService) CreateInstance(ctx context.Context, req *domain.CreateInstanceRequest, userID string) (*domain.Instance, error) {

	// Step 1: Validate image and acquire IAM token
	baseImagePath, instanceID, vmName, newDiskPath, instanceToken, err := s.prepareInstanceResources(req, userID)
	if err != nil {
		return nil, err
	}
 
	// Step 2: Allocate networking — direct call, no NATS
	vpcID, privateIP, gateway, bridgeName, err := s.allocateInstanceNetwork(ctx, userID, instanceID)
	if err != nil {
		return nil, err
	}

	// Step 2.5: Select best host
	bestHost, err := s.hostService.SelectBestHost()
	if err != nil {
		log.Printf("[SCHEDULER] [ERROR] Failed to select best host: %v", err)
	}
	hostID := ""
	if bestHost != nil {
		hostID = bestHost.ID
		log.Printf("[SCHEDULER] [OK] Selected host %s (%s) for instance %s", bestHost.Hostname, hostID, instanceID)
	} else {
		log.Printf("[SCHEDULER] [WARN] No active hosts found, provisioning locally")
	}
 
	// Step 3: Persist the record and launch VM creation asynchronously
	instance, err := s.persistAndLaunch(req, userID, instanceID, vmName, newDiskPath, baseImagePath, bridgeName, privateIP, gateway, vpcID, instanceToken, hostID)
	if err != nil {
		return nil, err
	}

	return instance, nil
}
 




func (s *InstanceService) GetInstance(id, userID string) (*domain.Instance, error) {
	instance, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if instance.UserID != userID && userID != "" {
		return nil, dto.ErrInstanceNotFound // or forbidden
	}
	return instance, nil
}

func (s *InstanceService) ListInstances(userID string) ([]*domain.Instance, error) {
	return s.repo.FindAll(userID)
}

func (s *InstanceService) StopInstance(id, userID string) error {
	instance, err := s.GetInstance(id, userID)
	if err != nil {
		return err
	}

	var remoteHostIP, remoteHostUser, remoteHostKey string
	if instance.HostID != "" {
		host, _ := s.hostService.GetHost(instance.HostID)
		if host != nil {
			remoteHostIP = host.IP
			remoteHostUser = host.SSHUser
			remoteHostKey = host.SSHPrivateKey
		}
	}

	if err := s.libvirtClient.StopVM(remoteHostIP, remoteHostUser, remoteHostKey, instance.VMName); err != nil {
		return err
	}

	instance.Status = domain.StatusStopped
	if err := s.repo.Update(instance); err != nil {
		return err
	}

	// Publish INSTANCE_STOPPED event
	if s.publisher != nil {
		go s.publisher.PublishInstanceEvent(domain.EventInstanceStopped, instance)
	}

	return nil
}

func (s *InstanceService) RestartInstance(instanceID, userID string) error {
	// Get the instance from the database/repository
	instance, err := s.GetInstance(instanceID, userID)
	if err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}

	var remoteHostIP, remoteHostUser, remoteHostKey string
	if instance.HostID != "" {
		host, _ := s.hostService.GetHost(instance.HostID)
		if host != nil {
			remoteHostIP = host.IP
			remoteHostUser = host.SSHUser
			remoteHostKey = host.SSHPrivateKey
		}
	}

	// Use the LibvirtClient to restart the VM
	err = s.libvirtClient.RestartVM(remoteHostIP, remoteHostUser, remoteHostKey, instance.VMName)
	if err != nil {
		return fmt.Errorf("failed to restart VM: %w", err)
	}

	// Update the status of the instance to reflect the restart
	// TODO: update the restarting in the to running
	instance.Status = domain.StatusRestarting
	if err := s.repo.Update(instance); err != nil {
		return fmt.Errorf("failed to update instance status: %w", err)
	}

	return nil
}
func (s *InstanceService) StartInstance(id, userID string) error {
	instance, err := s.GetInstance(id, userID)
	if err != nil {
		return err
	}

	var remoteHostIP, remoteHostUser, remoteHostKey string
	if instance.HostID != "" {
		host, _ := s.hostService.GetHost(instance.HostID)
		if host != nil {
			remoteHostIP = host.IP
			remoteHostUser = host.SSHUser
			remoteHostKey = host.SSHPrivateKey
		}
	}

	if err := s.libvirtClient.StartVM(remoteHostIP, remoteHostUser, remoteHostKey, instance.VMName); err != nil {
		return err
	}

	if err := s.repo.UpdateStatus(id, domain.StatusRunning); err != nil {
		return err
	}

	// Publish INSTANCE_STARTED event
	if s.publisher != nil {
		instance.Status = domain.StatusRunning
		go s.publisher.PublishInstanceEvent(domain.EventInstanceStarted, instance)
	}

	return nil
}

func (s *InstanceService) DeleteInstance(id, userID string) error {
	instance, err := s.GetInstance(id, userID)
	if err != nil {
		return err
	}

	// Use the VMName from the database to ensure consistency
	diskPath := filepath.Join(s.imagesDir, fmt.Sprintf("%s.qcow2", instance.VMName))

	var remoteHostIP, remoteHostUser, remoteHostKey string
	if instance.HostID != "" {
		host, _ := s.hostService.GetHost(instance.HostID)
		if host != nil {
			remoteHostIP = host.IP
			remoteHostUser = host.SSHUser
			remoteHostKey = host.SSHPrivateKey
		}
	}

	if err := s.libvirtClient.DeleteVM(remoteHostIP, remoteHostUser, remoteHostKey, instance.VMName); err != nil {
		return err
	}

	// Delete disk file
	if remoteHostIP != "" {
		exec.Command("ssh", "-o", "StrictHostKeyChecking=no", fmt.Sprintf("root@%s", remoteHostIP), "rm", "-f", diskPath).Run()
	} else {
		if err := os.Remove(diskPath); err != nil && !os.IsNotExist(err) {
			fmt.Printf("Warning: Failed to delete disk file %s: %v\n", diskPath, err)
		}
	}

	return s.repo.Delete(id)
}
func (s *InstanceService) GetStatusChecks(id, userID string) ([]*domain.InstanceStatusCheck, error) {
	instance, err := s.GetInstance(id, userID)
	if err != nil {
		return nil, err
	}

	// For now, return mock data as requested
	now := time.Now()
	checks := []*domain.InstanceStatusCheck{
		{
			ID:        "system_check",
			Name:      "System Status Check",
			Status:    "passed",
			Details:   "Infrastructure health is optimal. No issues detected in the underlying hardware.",
			UpdatedAt: now.Add(-time.Hour), // Mocking some time in the past
		},
		{
			ID:        "instance_check",
			Name:      "Instance Status Check",
			Status:    "passed",
			Details:   "Instance reachability check passed. The operating system is responding to network requests.",
			UpdatedAt: now.Add(-time.Minute * 55),
		},
	}

	// Update status if instance is not running
	if instance.Status != domain.StatusRunning {
		for _, check := range checks {
			check.Status = "failing"
			check.Details = "Instance is not in running state, status checks cannot be performed."
		}
	}

	return checks, nil
}

func (s *InstanceService) GetMetrics(id, userID string) (*domain.InstanceMetrics, error) {
	instance, err := s.GetInstance(id, userID)
	if err != nil {
		return nil, err
	}

	// For now, return mock data as requested
	now := time.Now()
	metrics := &domain.InstanceMetrics{
		CPUUsage:       15.4,
		RAMUsageMB:     512,
		RAMTotalMB:     instance.RAM, // Use RAM from instance
		NetworkInKbps:  45.2,
		NetworkOutKbps: 12.8,
		DiskReadIOPS:   120,
		DiskWriteIOPS:  45,
		UptimeSeconds:  36000,
		UpdatedAt:      now,
	}

	// If RAM total in instance is 0 (shouldn't happen but for safety)
	if metrics.RAMTotalMB == 0 {
		metrics.RAMTotalMB = 2048
	}

	// If instance is not running, return mostly zeroed metrics
	if instance.Status != domain.StatusRunning {
		metrics.CPUUsage = 0
		metrics.RAMUsageMB = 0
		metrics.NetworkInKbps = 0
		metrics.NetworkOutKbps = 0
		metrics.DiskReadIOPS = 0
		metrics.DiskWriteIOPS = 0
		metrics.UptimeSeconds = 0
	}

	return metrics, nil
}

// AssignVPC performs a coordinated VPC migration for an instance.
func (s *InstanceService) AssignVPC(ctx context.Context, userID, instanceID, newVPCID string) error {
	// 1. Fetch Instance
	instance, err := s.GetInstance(instanceID, userID)
	if err != nil {
		return err
	}

	oldVPCID := instance.VPCID
	if oldVPCID == newVPCID {
		return fmt.Errorf("instance is already in VPC %s", newVPCID)
	}

	log.Printf("[VPC-HOP] Starting migration for instance %s from VPC %s to %s", instanceID, oldVPCID, newVPCID)

	var remoteHostIP, remoteHostUser, remoteHostKey string
	if instance.HostID != "" {
		host, _ := s.hostService.GetHost(instance.HostID)
		if host != nil {
			remoteHostIP = host.IP
			remoteHostUser = host.SSHUser
			remoteHostKey = host.SSHPrivateKey
		}
	}

	// 2. Stop the Instance
	log.Printf("[VPC-HOP] Stopping VM %s on host %s (user: %s)", instance.VMName, remoteHostIP, remoteHostUser)
	if err := s.libvirtClient.StopVM(remoteHostIP, remoteHostUser, remoteHostKey, instance.VMName); err != nil {
		log.Printf("[VPC-HOP] Warning: StopVM failed: %v", err)
		// Continue anyway as it might already be stopped
	}

	// 3. Release Old Network
	if oldVPCID != "" {
		log.Printf("[VPC-HOP] Releasing old network for %s in VPC %s", instanceID, oldVPCID)
		if err := s.vpcService.ReleaseInstanceNetwork(context.Background(), instanceID); err != nil {
			log.Printf("[VPC-HOP] Warning: ReleaseInstanceNetwork failed: %v", err)
		}
	}

	// 4. Prepare New Network
	log.Printf("[VPC-HOP] Preparing new network for %s in VPC %s", instanceID, newVPCID)
	network, err := s.vpcService.AllocateInstanceNetwork(context.Background(), userID, instanceID, newVPCID)
	if err != nil {
		return fmt.Errorf("failed to prepare new network: %w", err)
	}
	privateIP := network.PrivateIP
	gateway := network.Gateway
	bridgeName := network.BridgeName

	// 5. Update Instance Metadata
	instance.VPCID = newVPCID
	instance.IP = privateIP
	// Note: bridgeName might be used in the XML reconfiguration
	if err := s.repo.Update(instance); err != nil {
		return fmt.Errorf("failed to update instance record: %w", err)
	}

	// 6. Re-configure Libvirt XML & Restart
	// We achieve this by deleting the old definition and creating a new one with same disk but new bridge
	log.Printf("[VPC-HOP] Reconfiguring VM %s with new bridge %s and IP %s", instance.VMName, bridgeName, privateIP)
	if err := s.libvirtClient.DeleteVM(remoteHostIP, remoteHostUser, remoteHostKey, instance.VMName); err != nil {
		log.Printf("[VPC-HOP] Warning: DeleteVM (undefine) failed: %v", err)
	}

	diskPath := filepath.Join(s.imagesDir, fmt.Sprintf("%s.qcow2", instance.VMName))

	combinedKeys := instance.PublicSSHKey
	if s.systemPubKey != "" {
		combinedKeys += "\n" + s.systemPubKey
	}

	_, err = s.libvirtClient.CreateAndStartVM(
		remoteHostIP,
		remoteHostUser,
		remoteHostKey,
		instance.VMName,
		diskPath,
		instance.CPU,
		instance.RAM,
		combinedKeys,
		bridgeName,
		privateIP,
		gateway,
		"",  // no metrics token needed for VPC-hop
		"",  // no profile for VPC-hop
		nil, // no parameters for VPC-hop
		"",  // no backing template for VPC-hop
	)
	if err != nil {
		return fmt.Errorf("failed to restart VM in new VPC: %w", err)
	}

	instance.Status = domain.StatusRunning
	_ = s.repo.UpdateStatus(instance.ID, domain.StatusRunning)

	log.Printf("[VPC-HOP] Successfully moved instance %s to VPC %s", instanceID, newVPCID)
	return nil
}
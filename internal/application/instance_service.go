package application

import (
	"context"
	dto "ec2-api/internal/domain/dto"
	"ec2-api/internal/domain/host"
	domain "ec2-api/internal/domain/instance"
	libvirt "ec2-api/internal/infra/libvirt"
	messaging "ec2-api/internal/infra/messaging"
	storage "ec2-api/internal/infra/storage"
	interfaces "ec2-api/internal/interfaces"
	"ec2-api/internal/vpcpkg"
	"errors"
	"net/http"

	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type InstanceService struct {
	repo          interfaces.InstanceRepository
	hostRepo      interfaces.HostRepository
	sgService     interfaces.SecurityGroupService
	libvirtClient *libvirt.LibvirtClient
	systemPubKey  string
	imagesDir     string
	publisher     *messaging.NATSPublisher
	minioAdapter  *storage.MinIOAdapter
	vpcService    *vpcpkg.Service // ← replaces publisher
	hostService   *HostService
	ec2PrivateKey string
	//  agentClient *vpcpkg.AgentClient
	httpClient *http.Client
	agentPort  int
}

func NewInstanceService(
	repo interfaces.InstanceRepository,
	hostRepo interfaces.HostRepository,
	sgService interfaces.SecurityGroupService,
	libvirt *libvirt.LibvirtClient,
	systemPubKey string,
	imagesDir string,
	publisher *messaging.NATSPublisher,
	minioAdapter *storage.MinIOAdapter,
	vpcService *vpcpkg.Service,
	hostService *HostService,
	ec2PrivateKey string,
	agentPort int,
) *InstanceService {

	return &InstanceService{
		repo:          repo,
		hostRepo:      hostRepo,
		sgService:     sgService,
		libvirtClient: libvirt,
		systemPubKey:  systemPubKey,
		imagesDir:     imagesDir,
		publisher:     publisher,
		minioAdapter:  minioAdapter,
		vpcService:    vpcService,
		hostService:   hostService,
		ec2PrivateKey: ec2PrivateKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		agentPort: agentPort,
	}
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


// ─── SSH Key Pair ─────────────────────────────────────────────────────────────

type SSHKeyPair struct {
	PrivateKeyPEM string // stored in DB → fed to agent at terminal time
	PublicKeyAuth string // "ssh-ed25519 AAAA..." → fed to cloud-init authorized_keys
}

var ErrNoActiveHosts = errors.New("no active hosts found")

// CreateInstance — unchanged signature, Step 2 now calls vpcService directly.
func (s *InstanceService) CreateInstance(ctx context.Context, req *domain.CreateInstanceRequest, userID string) (*domain.Instance, error) {


	switch req.Profile {

	case "ai-worker":
		req.Image = fmt.Sprintf("%s", req.Image)//ubuntu-22.04-green

	case "gamelift":
		req.Image = fmt.Sprintf("%s", req.Image)//ubuntu-22.04-blue
	case "rds":
		log.Printf("[SCHEDULER] rds profile",)
		
		req.Image = fmt.Sprintf("%s", req.Image)//ubuntu-22.04-yellow
	default:
		req.Image = fmt.Sprintf("%s", req.Image)//ubuntu-22.04-grey

	}
 
	// Step 1: Validate image and acquire IAM token
	baseImagePath, instanceID, vmName, newDiskPath, instanceToken, err := s.prepareInstanceResources(req, userID)
	if err != nil {
		go s.publisher.PublishInstanceEvent(req.Profile,domain.EventInstanceError, &domain.Instance{ID: instanceID}, "", req.SessionID,domain.VMStoped,req.ResourceID)
		return nil, err
	}

	// Step 2: VPC Placement & Host Selection
	// ─── NEW PLACEMENT LOGIC ──────────────────────────────────────
	
	defaultVPC, err := s.vpcService.GetOrCreateDefaultVPC(ctx, userID)
	if err != nil {
		go s.publisher.PublishInstanceEvent(req.Profile,domain.EventInstanceError, &domain.Instance{ID: instanceID}, "", req.SessionID,domain.VMStoped,req.ResourceID)
		return nil, fmt.Errorf("failed to get default VPC for user %s: %w", userID, err)
	}
	
	bestHost, err := s.hostService.SelectBestHost(req.Profile, defaultVPC.HostID)
	if err != nil {
		go s.publisher.PublishInstanceEvent(req.Profile,domain.EventInstanceError, &domain.Instance{}, "", req.SessionID,domain.VMStoped,req.ResourceID)
		log.Printf("[SCHEDULER] [ERROR] Failed to select best host: %v", err)
		return nil, err
	}

	hostID := ""
	if bestHost != nil {
		hostID = bestHost.ID
		log.Printf("[SCHEDULER] [OK] Selected host %s (%s) for instance %s with thsi ip: %s", bestHost.Hostname, hostID, instanceID, bestHost.IP)
	} else {
		go s.publisher.PublishInstanceEvent(req.Profile,domain.EventInstanceError, &domain.Instance{}, "", req.SessionID,domain.VMStoped,req.ResourceID)
		log.Printf("[SCHEDULER] [WARN] No active hosts found, provisioning locally")
		return nil, ErrNoActiveHosts
	}

	// Always bind the VPC to the actively selected compute host lock-in
	if bestHost.ID != defaultVPC.HostID {
		if err := s.vpcService.UpdateVPCHost(ctx, defaultVPC.ID, bestHost.ID); err != nil {
			log.Printf("[SCHEDULER] [WARN] Failed to update VPC HostID lock: %v", err)
		}
	}

	// ─── END PLACEMENT LOGIC ──────────────────────────────────────

	// Step 2.5: Allocate networking — direct call, no NATS
	vpcID, privateIP, gateway, bridgeName, err := s.allocateInstanceNetwork(ctx, userID, instanceID, defaultVPC)
	if err != nil {
		go s.publisher.PublishInstanceEvent(req.Profile,domain.EventInstanceError, &domain.Instance{ID: instanceID}, "", req.SessionID,domain.VMStoped,req.ResourceID)
		return nil, err
	}

	log.Printf("[SCHEDULER] [OK] Selected host %s (%s) for instance %s", bestHost.Hostname, hostID, instanceID)

	// --------------

	// Step 3: Persist the record and launch VM creation asynchronously
	instance, err := s.persistAndLaunch(req, userID, instanceID, vmName, newDiskPath, baseImagePath, bridgeName, privateIP, gateway, vpcID, instanceToken, bestHost)
	if err != nil {
		go s.publisher.PublishInstanceEvent(req.Profile,domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.VMStoped,req.ResourceID)
		return nil, err
	}
	instance.HostID = hostID
	_ = s.repo.Update(instance)

	return instance, nil
}

// type InsatanceDetailsResponse struct{
// 	Instance domain.Instance `json:"instance"`
// 	HostIP string `json:"host_ip"`

// }

func (s *InstanceService) GetInstance(id, userID string) (*domain.Instance, error) {
	instance, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if instance.UserID != userID && userID != "" {
		return nil, dto.ErrInstanceNotFound // or forbidden
	}
	host, err := s.hostRepo.GetByID(instance.HostID)
	if err != nil {
		return nil, err
	}
	instance.HostID = host.IP
	return instance, nil
}

func (s *InstanceService) ListInstances(userID string) ([]*domain.Instance, error) {
	return s.repo.FindAll(userID)
}

func (s *InstanceService) StopInstance(id, userID string, sessionID string) error {
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
		go s.publisher.PublishInstanceEvent(instance.ImageProfile, domain.EventInstanceError, instance, "", sessionID,domain.VMStoped,"")
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
func (s *InstanceService) StartInstance(id, userID string, sessionID string) error {
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
		go s.publisher.PublishInstanceEvent(instance.ImageProfile,domain.EventInstanceStarted, instance, "", sessionID,domain.VMStarted,"")
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
func (s *InstanceService) AssignVPC(
	ctx context.Context,
	userID,
	instanceID,
	newVPCID string,
) error {

	// ---------------------------------------------------
	// Load instance
	// ---------------------------------------------------

	instance, err := s.GetInstance(instanceID, userID)
	if err != nil {
		return err
	}

	oldVPCID := instance.VPCID

	if oldVPCID == newVPCID {
		return fmt.Errorf(
			"instance already in vpc %s",
			newVPCID,
		)
	}

	// ---------------------------------------------------
	// Resolve host
	// ---------------------------------------------------

	var remoteHostIP string
	var remoteHostUser string

	if instance.HostID != "" {
		host, _ := s.hostService.GetHost(instance.HostID)

		if host != nil {
			remoteHostIP = host.IP
			remoteHostUser = host.SSHUser
		}
	}

	if remoteHostUser == "" {
		remoteHostUser = "root"
	}

	// ---------------------------------------------------
	// Stop VM
	// ---------------------------------------------------

	err = s.libvirtClient.StopVM(
		remoteHostIP,
		remoteHostUser,
		s.ec2PrivateKey,
		instance.VMName,
	)

	if err != nil {
		log.Printf("[VPC] stop vm failed: %v", err)
	}

	// ---------------------------------------------------
	// Release old network
	// ---------------------------------------------------

	if oldVPCID != "" {
		_ = s.vpcService.ReleaseInstanceNetwork(
			context.Background(),
			instance.ID,
		)
	}

	// ---------------------------------------------------
	// Allocate new network
	// ---------------------------------------------------

	network, err := s.vpcService.AllocateInstanceNetwork(
		ctx,
		userID,
		instance.ID,
		newVPCID,
	)

	if err != nil {
		return err
	}

	privateIP := network.PrivateIP
	gateway := network.Gateway
	bridgeName := network.BridgeName

	// ---------------------------------------------------
	// Reconcile new network
	// ---------------------------------------------------

	_, _ = s.ReconcileNetwork(
		instance.ImageProfile,
		remoteHostIP,
		host.NetworkReconcileRequest{
			VPCID: newVPCID,
			Bridge: host.BridgeConfig{
				Name:    bridgeName,
				Gateway: gateway,
			},
			IP:      privateIP,
			Gateway: gateway,
			VMID: instanceID,
		},
	)

	// ---------------------------------------------------
	// Build cloud init ISO
	// ---------------------------------------------------

	combinedKeys := instance.PublicSSHKey

	if s.systemPubKey != "" {
		combinedKeys += "\n" + s.systemPubKey
	}

	isoPath, cleanupFn, err := s.libvirtClient.CreateCloudInitISO(
		instance.VMName,
		combinedKeys,
		privateIP,
		gateway,
		"",
		"default",
		map[string]string{},
	)

	if err != nil {
		return err
	}

	defer cleanupFn()

	// ---------------------------------------------------
	// Transfer ISO
	// ---------------------------------------------------

	err = s.transferOverlayToHost(
		remoteHostIP,
		remoteHostUser,
		s.ec2PrivateKey,
		isoPath,
		isoPath,
	)

	if err != nil {
		return err
	}

	// ---------------------------------------------------
	// VM paths
	// ---------------------------------------------------

	diskPath := filepath.Join(
		s.imagesDir,
		fmt.Sprintf("%s.qcow2", instance.VMName),
	)

	// ---------------------------------------------------
	// Delete old libvirt definition
	// ---------------------------------------------------

	_ = s.libvirtClient.DeleteVM(
		remoteHostIP,
		remoteHostUser,
		s.ec2PrivateKey,
		instance.VMName,
	)

	// ---------------------------------------------------
	// Start VM
	// ---------------------------------------------------

	_, err = s.libvirtClient.CreateAndStartVM(
		remoteHostIP,
		remoteHostUser,
		s.ec2PrivateKey,

		instance.VMName,

		diskPath,
		isoPath,

		instance.CPU,
		instance.RAM,

		bridgeName,
		privateIP,
		gateway,
		"default",
	)

	if err != nil {
		return err
	}

	// ---------------------------------------------------
	// Persist
	// ---------------------------------------------------

	instance.VPCID = newVPCID
	instance.IP = privateIP
	instance.Status = domain.StatusRunning

	if err := s.repo.Update(instance); err != nil {
		return err
	}

	log.Printf(
		"[VPC] instance %s moved to %s",
		instance.ID,
		newVPCID,
	)

	return nil
}

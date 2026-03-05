package application

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Qarani-m/ec2-api/internal/domain"
	"github.com/Qarani-m/ec2-api/internal/libvirt"
	"github.com/Qarani-m/ec2-api/pkg/messaging"
	"github.com/google/uuid"
)

type InstanceService struct {
	repo          domain.InstanceRepository
	sgService     domain.SecurityGroupService
	libvirtClient *libvirt.LibvirtClient
	systemPubKey  string
	imagesDir     string
	publisher     messaging.Publisher
}

var imageMap = map[string]string{
	// Ubuntu LTS versions
	"ubuntu-20.04": "ubuntu-20.04.qcow2",
	"ubuntu-22.04": "ubuntu-22.04.qcow2",
	"ubuntu-24.04": "ubuntu-24.04.qcow2",
	// Debian
	"debian-11": "debian-11.qcow2",
	"debian-12": "debian-12.qcow2",
	// CentOS alternatives (Amazon Linux equivalent)
	"rocky-8":     "rocky-8.qcow2",
	"rocky-9":     "rocky-9.qcow2",
	"almalinux-8": "almalinux-8.qcow2",
	"almalinux-9": "almalinux-9.qcow2",
	// Fedora (latest releases)
	"fedora-39": "fedora-39.qcow2",
	"fedora-40": "fedora-40.qcow2",
	// CentOS Stream
	"centos-stream-9": "centos-stream-9.qcow2",
}

// Map of image download URLs
var imageURLMap = map[string]string{
	// Ubuntu cloud images
	"ubuntu-20.04": "https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-amd64.img",
	"ubuntu-22.04": "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img",
	"ubuntu-24.04": "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img",

	// Debian cloud images
	"debian-11": "https://cloud.debian.org/images/cloud/bullseye/latest/debian-11-generic-amd64.qcow2",
	"debian-12": "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-generic-amd64.qcow2",

	// Rocky Linux (CentOS/Amazon Linux alternative)
	"rocky-8": "https://download.rockylinux.org/pub/rocky/8/images/x86_64/Rocky-8-GenericCloud-Base.latest.x86_64.qcow2",
	"rocky-9": "https://download.rockylinux.org/pub/rocky/9/images/x86_64/Rocky-9-GenericCloud-Base.latest.x86_64.qcow2",

	// AlmaLinux (CentOS/Amazon Linux alternative)
	"almalinux-8": "https://repo.almalinux.org/almalinux/8/cloud/x86_64/images/AlmaLinux-8-GenericCloud-latest.x86_64.qcow2",
	"almalinux-9": "https://repo.almalinux.org/almalinux/9/cloud/x86_64/images/AlmaLinux-9-GenericCloud-latest.x86_64.qcow2",

	// Fedora cloud images
	"fedora-39": "https://download.fedoraproject.org/pub/fedora/linux/releases/39/Cloud/x86_64/images/Fedora-Cloud-Base-39-1.5.x86_64.qcow2",
	"fedora-40": "https://download.fedoraproject.org/pub/fedora/linux/releases/40/Cloud/x86_64/images/Fedora-Cloud-Base-40-1.14.x86_64.qcow2",

	// CentOS Stream
	"centos-stream-9": "https://cloud.centos.org/centos/9-stream/x86_64/images/CentOS-Stream-GenericCloud-9-latest.x86_64.qcow2",
}

func NewInstanceService(repo domain.InstanceRepository, sgService domain.SecurityGroupService, libvirt *libvirt.LibvirtClient, systemPubKey string, imagesDir string, publisher messaging.Publisher) *InstanceService {
	s := &InstanceService{repo: repo, sgService: sgService, libvirtClient: libvirt, systemPubKey: systemPubKey, imagesDir: imagesDir, publisher: publisher}

	// Start background health update loop
	if publisher != nil {
		go s.startHealthUpdateLoop()
	}

	return s
}

// startHealthUpdateLoop periodically publishes HEALTH_UPDATE events for all instances.
// Why this is being implemented: The purpose is to allow the Network Service to learn
// about EC2 instances and track their health, so we can later integrate VPC/subnet
// awareness safely.
func (s *InstanceService) startHealthUpdateLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		instances, err := s.repo.FindAll("") // Find all across all users
		if err != nil {
			log.Printf("[InstanceService] Error fetching instances for health update: %v", err)
			continue
		}

		for _, instance := range instances {
			// Only publish health for running or stopped instances, but mainly we want the registry to know they exist
			// The Network Service will use this to track health.
			if (instance.Status == domain.StatusRunning || instance.Status == domain.StatusStopped) && s.publisher != nil {
				s.publisher.PublishInstanceEvent(domain.EventHealthUpdate, instance)
			}
		}
	}
}

// ensureImageExists checks if an image exists, and downloads it if it doesn't
func (s *InstanceService) ensureImageExists(imageName string, imagePath string) error {
	// Check if file exists
	if _, err := os.Stat(imagePath); err == nil {
		fmt.Printf("Image %s already exists at %s\n", imageName, imagePath)
		return nil
	}

	// Get download URL
	downloadURL, ok := imageURLMap[imageName]
	if !ok {
		return fmt.Errorf("no download URL configured for image: %s", imageName)
	}

	fmt.Printf("Image %s not found. Downloading from %s...\n", imageName, downloadURL)

	// Ensure directory exists
	dir := filepath.Dir(imagePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Download the image
	if err := s.downloadImage(downloadURL, imagePath); err != nil {
		return fmt.Errorf("failed to download image: %w", err)
	}

	fmt.Printf("Successfully downloaded %s to %s\n", imageName, imagePath)
	return nil
}

// downloadImage downloads a file from a URL to a local path
func (s *InstanceService) downloadImage(url string, destPath string) error {
	// Create temporary file
	tmpPath := destPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// Download the file
	resp, err := http.Get(url)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.Remove(tmpPath)
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Get file size for progress tracking
	fileSize := resp.ContentLength
	fmt.Printf("Downloading %d MB...\n", fileSize/(1024*1024))

	// Write to file with progress indication
	written, err := io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	fmt.Printf("Downloaded %d MB\n", written/(1024*1024))

	// Move temp file to final location
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename file: %w", err)
	}

	return nil
}







func (s *InstanceService) CreateInstance(req *domain.CreateInstanceRequest, userID string) (*domain.Instance, error) {

	// ── Step 1: Validate image ────────────────────────────────────────────────
	baseImageName, ok := imageMap[req.Image]
	if !ok {
		availableImages := make([]string, 0, len(imageMap))
		for k := range imageMap {
			availableImages = append(availableImages, k)
		}
		return nil, fmt.Errorf("image %s not found. Available: %v", req.Image, availableImages)
	}
	baseImagePath := filepath.Join(s.imagesDir, baseImageName)

	if err := s.ensureImageExists(req.Image, baseImagePath); err != nil {
		return nil, fmt.Errorf("failed to ensure image exists: %w", err)
	}

	instanceID := fmt.Sprintf("i-%s", uuid.New().String()[:8])
	vmName := fmt.Sprintf("vm-%s", instanceID)
	newDiskPath := filepath.Join(s.imagesDir, fmt.Sprintf("%s.qcow2", vmName))

	// ── Step 2: Ask Network Service to prepare networking BEFORE creating VM ──
	// This is the critical change: we get the IP, gateway, and bridge from the
	// network service first. The VM will be created with this IP baked into
	// cloud-init as a static address — no DHCP polling needed.
	var (
		vpcID      string
		privateIP  string
		gateway    string
		bridgeName string
	)

fmt.Println("---------Preparing network for instance", instanceID)



	if s.publisher != nil {
		if req.VPCID != "" {
fmt.Println("--------->>>1")

			// User specified a VPC — validate it first
			valid, err := s.publisher.ValidateVPC(userID, req.VPCID)
			if err != nil {
				log.Printf("[VPC] [ERROR] VPC validation failed for user %s, vpc %s: %v", userID, req.VPCID, err)
				return nil, fmt.Errorf("failed to validate VPC: %w", err)
			}
			if !valid {
				log.Printf("[VPC] [FAILURE] Invalid VPC %s for user %s", req.VPCID, userID)
				return nil, fmt.Errorf("invalid VPC ID: %s", req.VPCID)
			}
			vpcID = req.VPCID
			log.Printf("[VPC] [OK] Validated VPC %s for instance %s", vpcID, instanceID)
		} else {
fmt.Println("--------->>>2")

			// No VPC specified — get the default VPC
			var err error
			vpcID, bridgeName, err = s.publisher.GetDefaultVPC(userID)
			if err != nil {
				log.Printf("[VPC] [FAILURE] Failed to get default VPC for user %s: %v", userID, err)
				return nil, fmt.Errorf("failed to get default VPC: %w", err)
			}
			log.Printf("[VPC] [OK] Got default VPC %s (bridge: %s) for instance %s", vpcID, bridgeName, instanceID)
		}
fmt.Println("--------->>>3")

		// Ask network service to allocate an IP from the VPC subnet.
		// This returns the private IP, gateway, and confirms the bridge name.
		// The VM will be created with this exact IP — the network service is the
		// single source of truth for all IP addresses.
		var err error
		privateIP, gateway, bridgeName, err = s.publisher.PrepareInstanceNetwork(userID, instanceID, vpcID)
		if err != nil {
			log.Printf("[NETWORK] [ERROR] Failed to prepare network for instance %s in VPC %s: %v", instanceID, vpcID, err)
			return nil, fmt.Errorf("failed to prepare instance network: %w", err)
		}
fmt.Println("--------->>>4")
		log.Printf("[NETWORK] [OK] Network ready for instance %s — IP: %s, gateway: %s, bridge: %s",
			instanceID, privateIP, gateway, bridgeName)
	}


fmt.Println("---+++++------Preparing network for instance", instanceID)


	// ── Step 3: Save instance record with pending status and the known IP ─────
	// We save the IP now because we already know it — the network service
	// allocated it above. No need to update it again after VM creation.
	instance := &domain.Instance{
		ID:        instanceID,
		VMName:    vmName,
		Image:     req.Image,
		CPU:       req.CPU,
		RAM:       req.RAM,
		SSHKey:    req.SSHKey,
		Status:    domain.StatusPending,
		IP:        privateIP,  // ← known before VM creation
		PublicIP:  "",
		ProxmoxID: 0,
		CreatedAt: time.Now(),
		UserID:    userID,
		VPCID:     vpcID,
	}

	if err := s.repo.Create(instance); err != nil {
		// IP was allocated but instance won't be created — release it back
		if s.publisher != nil && privateIP != "" {
			if releaseErr := s.publisher.ReleaseInstanceNetwork(userID, instanceID, vpcID); releaseErr != nil {
				log.Printf("[NETWORK] [WARN] Failed to release IP for failed instance %s: %v", instanceID, releaseErr)
			}
		}
		return nil, fmt.Errorf("failed to save instance: %w", err)
	}

	// ── Step 4: Launch VM creation in background ──────────────────────────────
	// privateIP and gateway are passed in — libvirt will bake them into
	// cloud-init so the VM boots with a static IP. No DHCP polling.
	go s.createVMAsync(instance, req, baseImagePath, newDiskPath, bridgeName, privateIP, gateway)

	return instance, nil
}

func (s *InstanceService) createVMAsync(
	instance *domain.Instance,
	req *domain.CreateInstanceRequest,
	baseImagePath, newDiskPath, bridgeName, privateIP, gateway string,
) {
	absBase, _ := filepath.Abs(baseImagePath)
	absNew, _ := filepath.Abs(newDiskPath)

	// ── Step 1: Clone base disk ───────────────────────────────────────────────
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2",
		"-F", "qcow2", "-b", absBase, absNew)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[VM] Failed to clone disk for %s: %v\nOutput: %s", instance.VMName, err, string(output))
		s.markTerminatedAndReleaseNetwork(instance, newDiskPath)
		return
	}

	// ── Step 2: Validate libvirt is available ─────────────────────────────────
	if s.libvirtClient == nil {
		log.Printf("[VM] libvirt client not initialized for %s", instance.VMName)
		s.markTerminatedAndReleaseNetwork(instance, newDiskPath)
		return
	}

	combinedKeys := req.SSHKey
	if s.systemPubKey != "" {
		combinedKeys += "\n" + s.systemPubKey
	}

	log.Printf("[VM] Creating VM %s with static IP %s on bridge %s", instance.VMName, privateIP, bridgeName)

	// ── Step 3: Create and start VM with pre-allocated static IP ─────────────
	// privateIP and gateway are passed into CreateAndStartVM which writes them
	// into the cloud-init network-config. The VM boots already knowing its IP.
	// waitForVMIP is no longer needed since the IP is statically configured.
	vmID, err := s.libvirtClient.CreateAndStartVM(
		instance.VMName, absNew, req.CPU, req.RAM, combinedKeys, bridgeName, privateIP, gateway,
	)
	if err != nil {
		log.Printf("[VM] Failed to create VM %s: %v", instance.VMName, err)
		exec.Command("rm", "-f", newDiskPath).Run()
		s.markTerminatedAndReleaseNetwork(instance, "")
		return
	}

	// ── Step 4: Persist running state ────────────────────────────────────────
	// IP is already set on the instance from CreateInstance — just update
	// status, public IP, and the libvirt VM ID.
	instance.Status = domain.StatusRunning
	instance.PublicIP = s.libvirtClient.GetPublicIP(vmID)
	instance.ProxmoxID = vmID

	if err := s.repo.Update(instance); err != nil {
		log.Printf("[VM] Failed to update instance %s in DB: %v", instance.ID, err)
		s.libvirtClient.DeleteVM(instance.VMName)
		exec.Command("rm", "-f", newDiskPath).Run()
		return
	}

	// ── Step 5: Notify Network Service that instance is live ──────────────────
	// The network service already allocated the IP and recorded the reservation.
	// This event tells it the VM actually booted so it can start health monitoring.
	if s.publisher != nil {
		if err := s.publisher.PublishInstanceEvent(domain.EventInstanceStarted, instance); err != nil {
			log.Printf("[NATS] [WARN] Failed to publish INSTANCE_STARTED for %s: %v", instance.ID, err)
		}
	}

	// ── Step 6: Auto-assign default security group ────────────────────────────
	go func() {
		sg, err := s.sgService.GetSecurityGroupByName("default")
		if err == nil {
			if err := s.sgService.AssignToInstance(instance.ID, sg.ID); err != nil {
				log.Printf("[SG] Failed to auto-assign default SG to %s: %v", instance.ID, err)
			} else {
				log.Printf("[SG] ✓ Auto-assigned 'default' security group to %s", instance.ID)
			}
		} else {
			log.Printf("[SG] Could not find 'default' SG for auto-assignment: %v", err)
		}
	}()

	log.Printf("✓ VM %s created successfully (ID: %s, IP: %s, bridge: %s)",
		instance.VMName, instance.ID, privateIP, bridgeName)
}

// markTerminatedAndReleaseNetwork marks the instance as terminated and releases
// the pre-allocated IP back to the network service pool so it can be reused.
func (s *InstanceService) markTerminatedAndReleaseNetwork(instance *domain.Instance, diskPath string) {
	instance.Status = domain.StatusTerminated
	if err := s.repo.Update(instance); err != nil {
		log.Printf("[VM] Failed to mark instance %s as terminated: %v", instance.ID, err)
	}
	if diskPath != "" {
		exec.Command("rm", "-f", diskPath).Run()
	}
	// Release the IP back to the subnet pool
	if s.publisher != nil && instance.VPCID != "" {
		if err := s.publisher.ReleaseInstanceNetwork(instance.UserID, instance.ID, instance.VPCID); err != nil {
			log.Printf("[NETWORK] [WARN] Failed to release network for terminated instance %s: %v", instance.ID, err)
		}
	}
}







// markTerminated is a helper to set instance status to terminated and persist it.
func (s *InstanceService) markTerminated(instance *domain.Instance, diskPath string) {
	instance.Status = domain.StatusTerminated
	if err := s.repo.Update(instance); err != nil {
		log.Printf("[VM] Failed to mark instance %s as terminated: %v", instance.ID, err)
	}
	if diskPath != "" {
		exec.Command("rm", "-f", diskPath).Run()
	}
}

func (s *InstanceService) GetInstance(id, userID string) (*domain.Instance, error) {
	instance, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if instance.UserID != userID && userID != "" {
		return nil, domain.ErrInstanceNotFound // or forbidden
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

	if err := s.libvirtClient.StopVM(instance.VMName); err != nil {
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

	// Use the LibvirtClient to restart the VM
	err = s.libvirtClient.RestartVM(instance.VMName)
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

	if err := s.libvirtClient.StartVM(instance.VMName); err != nil {
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

	if err := s.libvirtClient.DeleteVM(instance.VMName); err != nil {
		return err
	}

	// Delete disk file
	if err := os.Remove(diskPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("Warning: Failed to delete disk file %s: %v\n", diskPath, err)
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

func (s *InstanceService) GetTags(id, userID string) ([]*domain.InstanceTag, error) {
	if _, err := s.GetInstance(id, userID); err != nil {
		return nil, err
	}
	return s.repo.GetTags(id)
}

func (s *InstanceService) AddOrUpdateTag(id, userID string, tag *domain.InstanceTag) error {
	if _, err := s.GetInstance(id, userID); err != nil {
		return err
	}
	return s.repo.AddOrUpdateTag(id, tag)
}

func (s *InstanceService) DeleteTag(id, userID string, key string) error {
	if _, err := s.GetInstance(id, userID); err != nil {
		return err
	}
	return s.repo.DeleteTag(id, key)
}

// MoveInstanceVPC moves an existing instance to another VPC.
func (s *InstanceService) MoveInstanceVPC(instanceID, userID, targetVPCID string) error {
	// 1. Validate instance ownership and existence
	instance, err := s.repo.FindByID(instanceID)
	if err != nil {
		return err
	}
	if instance.UserID != userID {
		return fmt.Errorf("unauthorized: instance does not belong to user")
	}

	// 2. Validate target VPC ownership if publisher is available
	if s.publisher != nil {
		valid, err := s.publisher.ValidateVPC(userID, targetVPCID)
		if err != nil {
			return fmt.Errorf("failed to validate target VPC: %w", err)
		}
		if !valid {
			return fmt.Errorf("invalid target VPC ID: %s", targetVPCID)
		}

		// 3. Detach from current VPC if it has one
		if instance.VPCID != "" {
			if err := s.publisher.DetachResource(userID, instanceID, instance.VPCID); err != nil {
				log.Printf("[VPC] [WARNING] Detach from VPC %s failed for instance %s: %v", instance.VPCID, instanceID, err)
			}
		}

		// 4. Attach to new VPC
		if _, err := s.publisher.AttachResource(userID, instanceID, targetVPCID); err != nil {
			return fmt.Errorf("failed to attach to new VPC: %w", err)
		}
	}

	// 5. Update local database
	oldVPC := instance.VPCID
	instance.VPCID = targetVPCID
	if err := s.repo.Update(instance); err != nil {
		return fmt.Errorf("failed to update instance record: %w", err)
	}

	log.Printf("[VPC] [SUCCESS] Moved instance %s from VPC %s to %s (user: %s)", instanceID, oldVPC, targetVPCID, userID)

	// Why this is being implemented:
	// This allows EC2 instances to participate in tenant-specific VPCs, enabling multi-instance
	// and multi-service isolation, while still preserving default behavior for single-instance tenants.

	return nil
}

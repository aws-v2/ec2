package application

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Qarani-m/ec2-api/internal/domain"
	"github.com/Qarani-m/ec2-api/internal/libvirt"
	"github.com/google/uuid"
)

type InstanceService struct {
	repo          domain.InstanceRepository
	sgService     domain.SecurityGroupService
	libvirtClient *libvirt.LibvirtClient
	systemPubKey  string
	imagesDir     string
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

func NewInstanceService(repo domain.InstanceRepository, sgService domain.SecurityGroupService, libvirt *libvirt.LibvirtClient, systemPubKey string, imagesDir string) *InstanceService {
	return &InstanceService{repo: repo, sgService: sgService, libvirtClient: libvirt, systemPubKey: systemPubKey, imagesDir: imagesDir}
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
	// TODO: Fire event: instance_creation_started
	// Validate image exists in map
	baseImageName, ok := imageMap[req.Image]
	if !ok {
		// ... (keep current error handling)
		availableImages := make([]string, 0, len(imageMap))
		for k := range imageMap {
			availableImages = append(availableImages, k)
		}
		return nil, fmt.Errorf("image %s not found. Available: %v", req.Image, availableImages)
	}
	baseImagePath := filepath.Join(s.imagesDir, baseImageName)

	// TODO: Fire event: image_validation_passed
	// Ensure the base image exists (download if needed)
	if err := s.ensureImageExists(req.Image, baseImagePath); err != nil {
		// TODO: Fire event: image_download_failed
		return nil, fmt.Errorf("failed to ensure image exists: %w", err)
	}
	// TODO: Fire event: image_ready
	// Generate instance details
	instanceID := fmt.Sprintf("i-%s", uuid.New().String()[:8])
	vmName := fmt.Sprintf("vm-%s", instanceID)
	newDiskPath := filepath.Join(s.imagesDir, fmt.Sprintf("%s.qcow2", vmName))
	// TODO: Fire event: instance_id_generated (instance ID, VM name)
	// Create instance record immediately with pending status
	instance := &domain.Instance{
		ID:        instanceID,
		VMName:    vmName,
		Image:     req.Image,
		CPU:       req.CPU,
		RAM:       req.RAM,
		SSHKey:    req.SSHKey,
		Status:    domain.StatusPending,
		IP:        "",
		PublicIP:  "",
		ProxmoxID: 0,
		CreatedAt: time.Now(),
		UserID:    userID,
	}
	if err := s.repo.Create(instance); err != nil {
		// TODO: Fire event: instance_db_create_failed
		return nil, fmt.Errorf("failed to save instance: %w", err)
	}
	// TODO: Fire event: instance_db_created (instance details)
	// Start VM creation in background
	go s.createVMAsync(instance, req, baseImagePath, newDiskPath)
	// TODO: Fire event: vm_creation_queued (instance ID)
	fmt.Println(instance)
	return instance, nil
}

func (s *InstanceService) createVMAsync(instance *domain.Instance, req *domain.CreateInstanceRequest, baseImagePath, newDiskPath string) {
	// TODO: Fire event: vm_creation_async_started (instance ID, VM name)
	// Clone disk
	// TODO: Fire event: disk_cloning_started (source path, destination path)
	absBase, _ := filepath.Abs(baseImagePath)
	absNew, _ := filepath.Abs(newDiskPath)
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2",
		"-F", "qcow2", "-b", absBase, absNew)
	if output, err := cmd.CombinedOutput(); err != nil {
		// TODO: Fire event: disk_cloning_failed (instance ID, error)
		fmt.Printf("Failed to clone disk for %s: %v\nOutput: %s\n", instance.VMName, err, string(output))
		instance.Status = domain.StatusTerminated
		s.repo.Update(instance)
		// TODO: Fire event: instance_terminated (instance ID, reason: disk_clone_failed)
		return
	}
	// TODO: Fire event: disk_cloning_completed (instance ID, disk path, disk size)
	// Create and start VM
	// TODO: Fire event: vm_libvirt_creation_started (instance ID, VM name, CPU, RAM)
	if s.libvirtClient == nil {
		fmt.Printf("Failed to create VM %s: libvirt client is not initialized\n", instance.VMName)
		instance.Status = domain.StatusTerminated
		s.repo.Update(instance)
		return
	}
	combinedKeys := req.SSHKey
	if s.systemPubKey != "" {
		combinedKeys += "\n" + s.systemPubKey
	}
	vmID, ip, err := s.libvirtClient.CreateAndStartVM(instance.VMName, absNew, req.CPU, req.RAM, combinedKeys)
	if err != nil {
		// TODO: Fire event: vm_libvirt_creation_failed (instance ID, error)
		fmt.Printf("Failed to create VM %s: %v\n", instance.VMName, err)
		// Cleanup disk on failure
		exec.Command("rm", "-f", newDiskPath).Run()
		// TODO: Fire event: disk_cleanup_completed (instance ID, disk path)
		instance.Status = domain.StatusTerminated
		s.repo.Update(instance)
		// TODO: Fire event: instance_terminated (instance ID, reason: vm_creation_failed)
		return
	}
	// TODO: Fire event: vm_started (instance ID, VM ID, IP address)
	// Update instance with VM details
	instance.Status = domain.StatusRunning
	instance.IP = ip
	instance.PublicIP = s.libvirtClient.GetPublicIP(vmID)
	instance.ProxmoxID = vmID
	// TODO: Fire event: instance_status_updating (instance ID, status: running)
	if err := s.repo.Update(instance); err != nil {
		// TODO: Fire event: instance_db_update_failed (instance ID, error)
		fmt.Printf("Failed to update instance %s: %v\n", instance.ID, err)
		// Cleanup VM on DB failure
		s.libvirtClient.DeleteVM(instance.VMName)
		// TODO: Fire event: vm_cleanup_completed (instance ID, VM ID)
		exec.Command("rm", "-f", newDiskPath).Run()
		// TODO: Fire event: disk_cleanup_completed (instance ID, disk path)
		return
	}

	// Auto-assign default security group
	go func() {
		sg, err := s.sgService.GetSecurityGroupByName("default")
		if err == nil {
			if err := s.sgService.AssignToInstance(instance.ID, sg.ID); err != nil {
				fmt.Printf("Warning: Failed to auto-assign default SG to %s: %v\n", instance.ID, err)
			} else {
				fmt.Printf("✓ Auto-assigned 'default' security group to %s\n", instance.ID)
			}
		} else {
			fmt.Printf("Warning: Could not find 'default' security group for auto-assignment: %v\n", err)
		}
	}()
	// TODO: Fire event: instance_running (instance ID, VM name, IP, public IP)
	// TODO: Fire event: vm_creation_completed (instance ID, duration, final status)
	fmt.Printf("✓ VM %s created successfully (ID: %s, IP: %s)\n", instance.VMName, instance.ID, ip)
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

	instance.Status = domain.WaitForShutdown
	return s.repo.Update(instance)
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

	return s.repo.UpdateStatus(id, domain.StatusRunning)
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

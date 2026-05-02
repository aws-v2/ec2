package application

import (
	"archive/zip"
	"context"
	"ec2-api/internal/domain"
	"ec2-api/internal/interfaces"
	"ec2-api/internal/libvirt"
	"ec2-api/internal/storage"
	"ec2-api/pkg/messaging"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// "context"
// "fmt"
// "io"
// "log"
// "net/http"
// "os"
// "os/exec"
// "path/filepath"
// "time"

// "archive/zip"
// "ec2-api/internal/domain"
// "ec2-api/internal/infrastructure/storage"
// "ec2-api/internal/interfaces"
// "ec2-api/internal/libvirt"
// "ec2-api/pkg/messaging"
// "strings"

// "github.com/google/uuid"

type InstanceService struct {
	repo          interfaces.InstanceRepository
	sgService     interfaces.SecurityGroupService
	libvirtClient *libvirt.LibvirtClient
	systemPubKey  string
	imagesDir     string
	publisher     messaging.Publisher
	minioAdapter  *storage.MinIOAdapter
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

func NewInstanceService(repo interfaces.InstanceRepository, sgService interfaces.SecurityGroupService, libvirt *libvirt.LibvirtClient, systemPubKey string, imagesDir string, publisher messaging.Publisher, minioAdapter *storage.MinIOAdapter) *InstanceService {
	s := &InstanceService{repo: repo, sgService: sgService, libvirtClient: libvirt, systemPubKey: systemPubKey, imagesDir: imagesDir, publisher: publisher, minioAdapter: minioAdapter}

	// Start background health update loop
	if publisher != nil {
		go s.startHealthUpdateLoop()
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

	// ── Step 1b: Request IAM token for the metrics agent ──────────────────────
	var instanceToken string
	if s.publisher != nil {
		var err error
		instanceToken, err = s.publisher.RequestInstanceToken(userID, instanceID)
		if err != nil {
			log.Printf("[IAM] [ERROR] Failed to get instance token for %s: %v", instanceID, err)
			return nil, fmt.Errorf("failed to get instance token: %w %s", err,s.publisher)
		}
		log.Printf("[IAM] [OK] Received instance token for %s", instanceID)
	}

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
		IP:        privateIP, // ← known before VM creation
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
	go s.createVMAsync(instance, req, baseImagePath, newDiskPath, bridgeName, privateIP, gateway, instanceToken)

	return instance, nil
}

func (s *InstanceService) createVMAsync(
	instance *domain.Instance,
	req *domain.CreateInstanceRequest,
	baseImagePath, newDiskPath, bridgeName, privateIP, gateway, instanceToken string,
) {
	profile := req.Profile
	params := req.Parameters
	absBase, _ := filepath.Abs(baseImagePath)
	absNew, _ := filepath.Abs(newDiskPath)

	// ── Step 1: Clone base disk ───────────────────────────────────────────────
	s.publishProgress(instance.ID, StageCloningDisk, "Cloning base image to new instance disk...")
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2",
		"-F", "qcow2", "-b", absBase, absNew)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[VM] Failed to clone disk for %s: %v\nOutput: %s", instance.VMName, err, string(output))
		s.publishProgress(instance.ID, StageFailed, fmt.Sprintf("Failed to clone disk: %v", err))
		s.markTerminatedAndReleaseNetwork(instance, newDiskPath)
		return
	}

	// ── Step 1.5: Payload Injection (Pre-mounted disk modification) ──────────
	// For gamelift, we still use host-side injection for legacy compatibility.
	// For ai-worker, we now use SageMaker-like simulation where the VM handles its own preparation.
	if profile == "gamelift" {
		if err := s.injectPayloadIntoDisk(instance, profile, params, absNew); err != nil {
			log.Printf("[VM] [%s] Injection failed for %s: %v", profile, instance.VMName, err)
			s.publishProgress(instance.ID, StageFailed, fmt.Sprintf("Failed to inject payload: %v", err))
			s.markTerminatedAndReleaseNetwork(instance, absNew)
			return
		}
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
	s.publishProgress(instance.ID, StageStartingVM, "Defining and starting the virtual machine...")
	vmID, err := s.libvirtClient.CreateAndStartVM(
		instance.VMName, absNew, req.CPU, req.RAM, combinedKeys, bridgeName, privateIP, gateway, instanceToken,
		profile, params,
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

	s.publishProgress(instance.ID, StageProvisioned, "Instance is now running and reachable.")
	log.Printf("✓ VM %s created successfully (ID: %s, IP: %s, bridge: %s)",
		instance.VMName, instance.ID, privateIP, bridgeName)
}

func (s *InstanceService) publishProgress(instanceID, stage, message string) {
	if s.publisher != nil {
		_ = s.publisher.PublishProvisioningProgress(instanceID, stage, message)
	}
}

func (s *InstanceService) injectPayloadIntoDisk(instance *domain.Instance, profile string, params map[string]string, diskPath string) error {
	// Case-insensitive lookup with STORAGE_ARN as fallback for DOWNLOAD_URL
	var downloadURL, headlessBin string
	for k, v := range params {
		switch strings.ToUpper(k) {
		case "DOWNLOAD_URL", "STORAGE_ARN":
			if downloadURL == "" || strings.ToUpper(k) == "DOWNLOAD_URL" {
				downloadURL = v
			}
		case "HEADLESS_BIN":
			headlessBin = v
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("missing DOWNLOAD_URL or STORAGE_ARN in parameters (keys found: %v)", getMapKeys(params))
	}

	// For gamelift, headlessBin is mandatory. For others (like ai-worker), it's optional.
	if headlessBin == "" && instance.Image == "" { // Use a better check if needed, but for now let's just use profile if we had it here
		// We don't have profile here directly easily without changing signature,
		// but we can check if it's required based on some heuristic or just allow it to be empty.
		log.Printf("[VM] No HEADLESS_BIN provided, skipping automated execution setup")
	}

	// 1. Create a workspace on host
	workdir := filepath.Join(os.TempDir(), fmt.Sprintf("inject-%s", instance.ID))
	if err := os.MkdirAll(workdir, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(workdir)

	// 2. Download payload to host
	zipPath := filepath.Join(workdir, "payload.zip")
	s.publishProgress(instance.ID, StageDownloadingPayload, "Downloading payload package to host...")

	if strings.HasPrefix(downloadURL, "arn:aws:s3:::") {
		// Parse ARN: arn:aws:s3:::bucket/key/path/file.zip
		path := strings.TrimPrefix(downloadURL, "arn:aws:s3:::")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) < 2 {
			return fmt.Errorf("invalid S3 ARN format: %s", downloadURL)
		}
		bucket := parts[0]
		key := parts[1]

		if s.minioAdapter == nil {
			return fmt.Errorf("MinIO adapter not initialized, cannot handle S3 ARN")
		}

		log.Printf("[VM] Downloading from MinIO: bucket=%s, key=%s", bucket, key)
		if err := s.minioAdapter.DownloadFile(context.Background(), bucket, key, zipPath); err != nil {
			return fmt.Errorf("failed to download from MinIO: %w", err)
		}
	} else {
		if err := s.downloadImage(downloadURL, zipPath); err != nil {
			return fmt.Errorf("failed to download payload: %w", err)
		}
	}

	// 3. Unzip on host
	s.publishProgress(instance.ID, StageUnzippingPayload, "Extracting payload...")
	extractDir := filepath.Join(workdir, "payload")
	if err := s.unzip(zipPath, extractDir); err != nil {
		return fmt.Errorf("failed to unzip: %w", err)
	}

	// 4. Multi-stage guestmount injection
	s.publishProgress(instance.ID, StageInjectingPayload, "Injecting payload files into instance disk...")
	mountDir := filepath.Join(workdir, "mount")
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		return err
	}

	// Wait for disk to be settled
	time.Sleep(1 * time.Second)

	// Mount
	log.Printf("[VM] Mounting %s to %s", diskPath, mountDir)
	cmd := exec.Command("guestmount", "-a", diskPath, "-i", mountDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("guestmount failed: %w\nOutput: %s", err, string(out))
	}
	// 5. Create directory structure inside mount
	targetDir := filepath.Join(mountDir, "opt", "game", "run")
	if profile == "ai-worker" {
		targetDir = filepath.Join(mountDir, "opt", "inference", "run")
	}

	if err := exec.Command("mkdir", "-p", targetDir).Run(); err != nil {
		return fmt.Errorf("failed to create target dir: %w", err)
	}

	// 6. Copy files
	log.Printf("[VM] Copying files to %s", targetDir)
	cpCmd := exec.Command("cp", "-r", extractDir+"/.", targetDir)
	if out, err := cpCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy failed: %w\nOutput: %s", err, string(out))
	}

	// 7. Ensure binary is executable inside the mount (if provided)
	if headlessBin != "" {
		binPath := filepath.Join(targetDir, headlessBin)
		_ = exec.Command("chmod", "+x", binPath).Run()
	}

	// 8. Unmount and cleanup
	log.Printf("[VM] Unmounting %s...", mountDir)
	var lastUnmountErr error
	for i := 0; i < 5; i++ {
		unmountCmd := exec.Command("guestunmount", mountDir)
		if err := unmountCmd.Run(); err == nil {
			lastUnmountErr = nil
			break
		} else {
			lastUnmountErr = err
			log.Printf("[Gamelift] Unmount attempt %d failed, retrying...", i+1)
			time.Sleep(500 * time.Millisecond)
		}
	}

	if lastUnmountErr != nil {
		log.Printf("[Gamelift] Persistent unmount failure for %s: %v", mountDir, lastUnmountErr)
	}

	// Final rest to ensure FUSE/QEMU releases the file handle
	time.Sleep(2 * time.Second)

	log.Printf("[VM] Injection successful for %s", instance.VMName)
	return nil
}

func (s *InstanceService) unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
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

	// 2. Stop the Instance
	log.Printf("[VPC-HOP] Stopping VM %s", instance.VMName)
	if err := s.libvirtClient.StopVM(instance.VMName); err != nil {
		log.Printf("[VPC-HOP] Warning: StopVM failed: %v", err)
		// Continue anyway as it might already be stopped
	}

	// 3. Release Old Network
	if oldVPCID != "" && s.publisher != nil {
		log.Printf("[VPC-HOP] Releasing network in VPC %s", oldVPCID)
		if err := s.publisher.ReleaseInstanceNetwork(userID, instanceID, oldVPCID); err != nil {
			log.Printf("[VPC-HOP] Warning: ReleaseInstanceNetwork failed: %v", err)
		}
	}

	// 4. Prepare New Network
	var privateIP, gateway, bridgeName string
	if s.publisher != nil {
		log.Printf("[VPC-HOP] Preparing network in VPC %s", newVPCID)
		privateIP, gateway, bridgeName, err = s.publisher.PrepareInstanceNetwork(userID, instanceID, newVPCID)
		if err != nil {
			return fmt.Errorf("failed to prepare new network in VPC %s: %w", newVPCID, err)
		}
	}

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
	if err := s.libvirtClient.DeleteVM(instance.VMName); err != nil {
		log.Printf("[VPC-HOP] Warning: DeleteVM (undefine) failed: %v", err)
	}

	diskPath := filepath.Join(s.imagesDir, fmt.Sprintf("%s.qcow2", instance.VMName))

	combinedKeys := instance.SSHKey
	if s.systemPubKey != "" {
		combinedKeys += "\n" + s.systemPubKey
	}

	_, err = s.libvirtClient.CreateAndStartVM(
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
	)
	if err != nil {
		return fmt.Errorf("failed to restart VM in new VPC: %w", err)
	}

	instance.Status = domain.StatusRunning
	_ = s.repo.UpdateStatus(instance.ID, domain.StatusRunning)

	log.Printf("[VPC-HOP] Successfully moved instance %s to VPC %s", instanceID, newVPCID)
	return nil
}

func (s *InstanceService) CreateScalingPolicy(ctx context.Context, userID string, req *domain.ScalingPolicyRequest) error {
	return s.repo.CreateScalingPolicy(ctx, userID, req)
}

func (s *InstanceService) GetScalingPolicies(ctx context.Context, userID string) ([]domain.ScalingPolicy, error) {
	return s.repo.GetScalingPolicies(ctx, userID)
}

func (s *InstanceService) UpdateScalingPolicy(ctx context.Context, userID, policyID string, req *domain.UpdateScalingPolicyRequest) error {
	return s.repo.UpdateScalingPolicy(ctx, userID, policyID, req)
}

func (s *InstanceService) DeleteScalingPolicy(ctx context.Context, userID, policyID string) error {
	return s.repo.DeleteScalingPolicy(ctx, userID, policyID)
}
// ── Scaling Enforcement ──────────────────────────────────────────────────

// EnforceScaling receives a requested scale action from the Metrics Service and attempts to execute it.
func (s *InstanceService) EnforceScaling(ctx context.Context, event *domain.ScaleEvent) error {
	log.Printf("[SCALER] Enforcing scale action: %s for target: %s (tenant: %s)", event.Action, event.Policy.TargetID, event.Policy.UserID)

	// In the future, target_type could be "asg", and we'd look up the ASG details here.
	// For now, if the target is an individual instance, we act on that instance directly.
	if event.Policy.PolicyType != "instance" {
		return fmt.Errorf("unsupported target_type: %s", event.Policy.PolicyType)
	}

	targetInstance, err := s.GetInstance(event.Policy.TargetID, event.UserID)
	if err != nil {
		return fmt.Errorf("failed to fetch target instance %s: %w", event.Policy.TargetID, err)
	}

	switch event.Action {
	case domain.ScaleOutAction:
		return s.handleScaleOut(targetInstance, event.Policy.MaxCapacity)
	case domain.ScaleInAction:
		return s.handleScaleIn(targetInstance)
	default:
		return fmt.Errorf("unknown scale action: %s", event.Action)
	}
}

func (s *InstanceService) handleScaleOut(baseInstance *domain.Instance, maxInstances int) error {
	log.Printf("[SCALER] Initiating scale-out based on instance %s (VPC: %s)", baseInstance.ID, baseInstance.VPCID)

	// Check if max limit is reached
	if maxInstances > 0 {
		instances, err := s.ListInstances(baseInstance.UserID)
		if err != nil {
			return fmt.Errorf("failed to list instances to enforce scale limit: %w", err)
		}

		currentCount := 0
		for _, inst := range instances {
			if inst.Status == domain.StatusRunning || inst.Status == domain.StatusPending {
				if inst.VPCID == baseInstance.VPCID && inst.Image == baseInstance.Image {
					currentCount++
				}
			}
		}

		if currentCount >= maxInstances {
			log.Printf("[SCALER] [WARNING] Scale-out blocked. Current instances (%d) reached or exceeded max limit (%d)", currentCount, maxInstances)
			return nil
		}
	}

	req := &domain.CreateInstanceRequest{
		Image:  baseInstance.Image,
		CPU:    baseInstance.CPU,
		RAM:    baseInstance.RAM,
		SSHKey: baseInstance.SSHKey,
		VPCID:  baseInstance.VPCID,
	}

	_, err := s.CreateInstance(req, baseInstance.UserID)
	return err
}

func (s *InstanceService) handleScaleIn(baseInstance *domain.Instance) error {
	log.Printf("[SCALER] Initiating scale-in for target instance %s", baseInstance.ID)

	instances, err := s.ListInstances(baseInstance.UserID)
	if err != nil {
		return fmt.Errorf("failed to list instances for scale-in: %w", err)
	}

	var activeReplicas []*domain.Instance
	for _, inst := range instances {
		if (inst.Status == domain.StatusRunning || inst.Status == domain.StatusPending) && inst.ID != baseInstance.ID {
			if inst.VPCID == baseInstance.VPCID && inst.Image == baseInstance.Image {
				activeReplicas = append(activeReplicas, inst)
			}
		}
	}

	if len(activeReplicas) == 0 {
		log.Printf("[SCALER] [WARNING] Scale-in blocked. No replicas to terminate.")
		return nil
	}

	targetToTerminate := activeReplicas[len(activeReplicas)-1]
	return s.DeleteInstance(targetToTerminate.ID, targetToTerminate.UserID)
}

// HandleProvision handles a request to provision a new VM based on a profile.
func (s *InstanceService) HandleProvision(ctx context.Context, event *domain.ProvisionInstanceEvent) error {
	log.Printf("[PROVISIONER] Provisioning VM for profile: %s", event.Profile)

	// Merge flat fields into Parameters for backward compatibility
	if event.Parameters == nil {
		event.Parameters = make(map[string]string)
	}
	if event.StorageARN != "" && event.Parameters["STORAGE_ARN"] == "" {
		event.Parameters["STORAGE_ARN"] = event.StorageARN
	}
	if event.HeadlessBin != "" && event.Parameters["HEADLESS_BIN"] == "" {
		event.Parameters["HEADLESS_BIN"] = event.HeadlessBin
	}

	// Create a provision request
	req := &domain.CreateInstanceRequest{
		Image:      "ubuntu-22.04", // Default base image
		CPU:        event.Specs.CPU,
		RAM:        event.Specs.RAM,
		SSHKey:     "", // System key will be added automatically
		Profile:    event.Profile,
		Parameters: event.Parameters,
	}

	// Ensure sensible defaults if specs are empty
	if req.CPU == 0 {
		req.CPU = 2
	}
	if req.RAM == 0 {
		req.RAM = 4096
	}

	// Use the provided user ID or "system"
	userID := event.UserID
	if userID == "" {
		userID = "system"
	}

	_, err := s.CreateInstance(req, userID)
	if err != nil {
		return fmt.Errorf("failed to create instance for profile %s: %w", event.Profile, err)
	}

	return nil
}

func getMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

package application

import (
	"bytes"
	"context"
	dto "ec2-api/internal/domain/dto"
	"ec2-api/internal/domain/host"
	domain "ec2-api/internal/domain/instance"
	libvirt "ec2-api/internal/infra/libvirt"
	messaging "ec2-api/internal/infra/messaging"
	storage "ec2-api/internal/infra/storage"
	interfaces "ec2-api/internal/interfaces"
	"ec2-api/internal/vpcpkg"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/google/uuid"
	libvirtgo "libvirt.org/go/libvirt"

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
	// TODO: i should probably remvoethis ,
	// it was added here to facilitate working with this line
	// if _, err := s.AddForwardingRule(
	// in the n the intance_lifecycle.go, ie. if the ENV is dev we skip
	// calling the function it requires an intenet connection
	// Aki pesa wewe :)
	ENV                 string
	profileBaseImage    map[string]string
	assetServiceBaseURL string
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
	profileBaseImage map[string]string,
	ENV string,
	assetServiceBaseURL string,
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
		agentPort:           agentPort,
		profileBaseImage:    profileBaseImage,
		ENV:                 ENV,
		assetServiceBaseURL: assetServiceBaseURL,
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

func (s *InstanceService) resolveCustomImage(userID, amiID, hostip string) (string, error) {
	image, err := s.repo.FindAMIByIDAndUserID(amiID, userID)
	if err != nil {
		return "", err // dto.ErrAMINotFound if missing or owned by someone else
	}

	if image.Status != domain.ImageStatusAvailable {
		return "", fmt.Errorf("image %s is not available (status: %s)", amiID, image.Status)
	}

	if image.SHA256 == "" {
		return "", fmt.Errorf("image %s has no sha256 recorded", amiID)
	}

	url, err := s.publisher.FetchAgentPresignedURL(userID, "version", "file_name", image.SHA256)
	if err != nil {
		return "", fmt.Errorf("get presigned url for image %s: %w", amiID, err)
	}
	if url == "" {
		return "", fmt.Errorf("presigned url for image %s is empty", amiID)
	}
	log.Printf("returned s3 url: %+v", url)
	fullUrl := fmt.Sprintf("%s%s", s.assetServiceBaseURL, url)

	s.DownloadFileViaAgent(hostip, fullUrl, "/var/lib/libvirt/images", image.ID)

	return image.ID, nil
}
func (s *InstanceService) DownloadFileViaAgent(hostIP, fileURL, destination, filename string) error {
	reqBody := HostActionRequest{
		Type: "download_file",
		Params: map[string]string{
			"url":         fileURL,
			"destination": destination,
			"filename":    filename,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal host action request: %w", err)
	}

	url := fmt.Sprintf("http://%s:9030/host-actions", hostIP)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build host action request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("X-Agent-Key", l.agentAPIKey)

	// Generous timeout for now since we're downloading synchronously and
	// waiting for the agent's confirmation before proceeding to attach.
	// This will need to become async once volumes are large enough that
	// blocking the whole attach request on a download isn't viable.
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach agent on host %s: %w", hostIP, err)
	}
	defer resp.Body.Close()

	var result HostActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode agent response: %w", err)
	}

	if resp.StatusCode != http.StatusAccepted || result.Status != "success" {
		return fmt.Errorf("agent reported failure downloading file: %s", result.Message)
	}

	return nil
}
func resolveImage(profile string, profileBaseImage map[string]string) string {
	// "vanilla":"ubuntu-22.04.18"

	imagePrefix := profileBaseImage[profile]

	switch profile {
	case "ai-worker":
		return fmt.Sprintf("%s", imagePrefix)
	case "gamelift":
		return fmt.Sprintf("%s", imagePrefix)
	case "rds":
		return fmt.Sprintf("%s", imagePrefix)
	case "lambdas":
		return fmt.Sprintf("%s", imagePrefix)
	case "vanilla":
		return fmt.Sprintf("%s", imagePrefix)
	default:
		return fmt.Sprintf("%s", imagePrefix)
	}
}

// 8.4.4.4.12
type PrepareInstanceResources struct {
	baseImagePath string
	instanceID    string
	vmName        string
	newDiskPath   string
	instanceToken string
}

// CreateInstance — unchanged signature, Step 2 now calls vpcService directly.
func (s *InstanceService) DownloadPem(ctx context.Context, instanceID string) (string, error) {
	instance, err := s.repo.FindByID(instanceID)

	if err != nil {
		return "", fmt.Errorf("failed to get download pem forisntance  %s: %w", instanceID, err)
	}
	return instance.PrivateSshKey, nil
}
func (s *InstanceService) CreateInstance(ctx context.Context, req *domain.CreateInstanceRequest, userID string) (*domain.Instance, error) {

	log.Printf("[SCHEDULER][] %d profile", req.Specs.CPU)

	instanceID := fmt.Sprintf(uuid.New().String()[:8])

	vmName := fmt.Sprintf("%s-vm-%s", req.Profile, instanceID)

	instanceID = fmt.Sprintf("%s-i-%s", req.Profile, vmName)

	// Step 2: VPC Placement & Host Selection
	// ─── NEW PLACEMENT LOGIC ──────────────────────────────────────

	defaultVPC, err := s.vpcService.GetOrCreateDefaultVPC(ctx, userID)
	if err != nil {
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{ID: instanceID}, "", req.SessionID, domain.VMStoped, req.ResourceID)
		return nil, fmt.Errorf("failed to get default VPC for user %s: %w", userID, err)
	}

	bestHost, gatewayHost, err := s.hostService.SelectBestHost(req.Profile, defaultVPC.HostID)
	// beshHost== the host where the vms will be running
	// gatewayHost == the public vps that is available
	if err != nil {
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.VMStoped, req.ResourceID)
		log.Printf("[SCHEDULER] [ERROR] Failed to select best host: %v", err)
		return nil, err
	}

	hostID := ""
	if bestHost != nil {
		hostID = bestHost.ID
		log.Printf("[SCHEDULER] [OK] Selected host %s (%s) for instance %s with thsi ip: %s", bestHost.Hostname, hostID, instanceID, bestHost.IP)
	} else {
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.VMStoped, req.ResourceID)
		log.Printf("[SCHEDULER] [WARN] No active hosts found, provisioning locally for profile:%s", req.Profile)
		return nil, ErrNoActiveHosts
	}

	// Always bind the VPC to the actively selected compute host lock-in
	if bestHost.ID != defaultVPC.HostID {
		if err := s.vpcService.UpdateVPCHost(ctx, defaultVPC.ID, bestHost.ID); err != nil {
			log.Printf("[SCHEDULER] [WARN] Failed to update VPC HostID lock: %v", err)
		}
	}

	// ─── END PLACEMENT LOGIC ──────────────────────────────────────

	if req.Profile == "custom_ami" {
		if req.CustomAMIID == "" {
			return nil, fmt.Errorf("custom_ami_id is required when profile is custom_ami")
		}

		image, err := s.resolveCustomImage(userID, req.CustomAMIID, bestHost.IP)

		if err != nil {
			if errors.Is(err, fmt.Errorf("AMI not found")) {
				return nil, fmt.Errorf("AMI not found") // let the handler map this to 404
			}
			return nil, fmt.Errorf("resolve custom image %s: %w", req.CustomAMIID, err)
		}

		// here

		req.Image = image

	} else {
		req.Image = resolveImage(req.Profile, s.profileBaseImage)
	}

	// Step 1: Validate image and acquire IAM token
	// TODO: this shouldbe removed, all it dos is ensure the iage exists froma mapno the acutal host
	// checkup,to actually dothat we need host selection as number 1 step
	baseImagePath, newDiskPath, instanceToken, err := s.prepareInstanceResources(req, userID, instanceID, vmName, bestHost)
	if err != nil {
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{ID: instanceID}, "", req.SessionID, domain.VMStoped, req.ResourceID)
		return nil, err
	}

	// TODO:
	// Vm name is reassigned here because i dont want to fuck areound and find out
	// therefor

	vmName = req.Name

	fmt.Printf("The choosen base-image----->: %s", baseImagePath)

	// Step 2: VPC Placement & Host Selection
	// // ─── NEW PLACEMENT LOGIC ──────────────────────────────────────

	// defaultVPC, err := s.vpcService.GetOrCreateDefaultVPC(ctx, userID)
	// if err != nil {
	// 	go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{ID: instanceID}, "", req.SessionID, domain.VMStoped, req.ResourceID)
	// 	return nil, fmt.Errorf("failed to get default VPC for user %s: %w", userID, err)
	// }

	// bestHost, gatewayHost, err := s.hostService.SelectBestHost(req.Profile, defaultVPC.HostID)
	// // beshHost== the host where the vms will be running
	// // gatewayHost == the public vps that is available
	// if err != nil {
	// 	go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.VMStoped, req.ResourceID)
	// 	log.Printf("[SCHEDULER] [ERROR] Failed to select best host: %v", err)
	// 	return nil, err
	// }

	// hostID := ""
	// if bestHost != nil {
	// 	hostID = bestHost.ID
	// 	log.Printf("[SCHEDULER] [OK] Selected host %s (%s) for instance %s with thsi ip: %s", bestHost.Hostname, hostID, instanceID, bestHost.IP)
	// } else {
	// 	go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.VMStoped, req.ResourceID)
	// 	log.Printf("[SCHEDULER] [WARN] No active hosts found, provisioning locally for profile:%s", req.Profile)
	// 	return nil, ErrNoActiveHosts
	// }

	// // Always bind the VPC to the actively selected compute host lock-in
	// if bestHost.ID != defaultVPC.HostID {
	// 	if err := s.vpcService.UpdateVPCHost(ctx, defaultVPC.ID, bestHost.ID); err != nil {
	// 		log.Printf("[SCHEDULER] [WARN] Failed to update VPC HostID lock: %v", err)
	// 	}
	// }

	// // ─── END PLACEMENT LOGIC ──────────────────────────────────────

	// Step 2.5: Allocate networking — direct call, no NATS
	vpcID, privateIP, gateway, bridgeName, err := s.allocateInstanceNetwork(ctx, userID, instanceID, defaultVPC)
	if err != nil {
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{ID: instanceID}, "", req.SessionID, domain.VMStoped, req.ResourceID)
		return nil, err
	}

	log.Printf("[SCHEDULER] [OK] Selected host %s (%s) for instance %s", bestHost.Hostname, hostID, instanceID)
	log.Printf("[SCHEDULER] [OK] Stelected host hostname: %s hostip (%s) for hosttype %s", gatewayHost.Hostname, gatewayHost.IP, gatewayHost.HostType)

	// --------------

	// Step 3: Persist the record and launch VM creation asynchronously
	instance, err := s.persistAndLaunch(ctx, req, userID, instanceID, vmName, newDiskPath, baseImagePath, bridgeName, privateIP, gateway, vpcID, instanceToken, bestHost, gatewayHost)
	if err != nil {
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.VMStoped, req.ResourceID)
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
	instance.HostID = host.ID
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
		go s.publisher.PublishInstanceEvent(instance.ImageProfile, domain.EventInstanceError, instance, "", sessionID, domain.VMStoped, "")
	}

	return nil
}

type hostConn struct{ IP, User, Key string }

func (s *InstanceService) resolveHost(instance *domain.Instance) hostConn {
	var h hostConn
	if instance.HostID != "" {
		if host, _ := s.hostService.GetHost(instance.HostID); host != nil {
			h = hostConn{IP: host.IP, User: host.SSHUser, Key: host.SSHPrivateKey}
		}
	}
	return h
}

// ListAMIs returns the summaries of all images owned by the user, newest first.
func (s *InstanceService) ListAMIs(userID string) ([]domain.AMISummary, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("user id is required")
	}
	return s.repo.FindAMIsByUserID(userID)
}

// CreateAMIImage validates the request, records a pending image and builds it in the background.
func (s *InstanceService) CreateAMIImage(req domain.AMIImageRequest) (*domain.Image, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("image name is required")
	}

	instance, err := s.GetInstance(req.InstanceID, req.UserID)
	if err != nil {
		return nil, err
	}
	if instance.Status != domain.StatusRunning && instance.Status != domain.StatusStopped {
		return nil, fmt.Errorf("instance must be running or stopped to create an image (current: %s)", instance.Status)
	}

	img := &domain.Image{
		ID:               uuid.NewString(),
		Name:             req.Name,
		Description:      req.Description,
		UserID:           req.UserID,
		SourceInstanceID: instance.ID,
		NoReboot:         req.NoReboot,
		Status:           domain.ImageStatusPending,
		DiskPath:         fmt.Sprintf("/var/lib/libvirt/images/amis/%s.qcow2", uuid.NewString()),
	}
	if err := s.repo.CreateAMI(img); err != nil {
		return nil, err
	}

	host := s.resolveHost(instance)

	go func() {
		started := time.Now()
		live := req.NoReboot && instance.Status == domain.StatusRunning
		amiLogf(img.ID, "build queued: name=%q instance=%s no_reboot=%t mode=%s",
			img.Name, instance.ID, req.NoReboot, map[bool]string{true: "live", false: "offline"}[live])

		var buildErr error
		if live {
			log.Printf("Building Live AmI image,")
			buildErr = s.buildAMILive(host, instance, img)
		} else {
			log.Printf("Building Offline AmI image,")

			buildErr = s.buildAMIOffline(host, instance, img)
		}

		if buildErr != nil {
			amiLogf(img.ID, "FAILED after %s: %v", time.Since(started).Round(time.Millisecond), buildErr)
			img.Status = domain.ImageStatusFailed
			img.ErrorMessage = buildErr.Error()
			if dErr := s.libvirtClient.DeleteFile(host.IP, host.User, host.Key, img.DiskPath); dErr != nil {
				amiLogf(img.ID, "WARN cleanup of partial image failed: %v", dErr)
			}
		} else {
			amiLogf(img.ID, "AVAILABLE after %s", time.Since(started).Round(time.Millisecond))
			img.Status = domain.ImageStatusAvailable
		}

		if err := s.repo.UpdateAMI(img); err != nil {
			amiLogf(img.ID, "ERROR failed to persist final status=%s: %v", img.Status, err)
		}
	}()

	return img, nil
}

// helper: one prefix for every AMI log line
func amiLogf(imgID, format string, args ...any) {
	log.Printf("[AMI %s] "+format, append([]any{imgID}, args...)...)
}

// diskNameFromInstanceID turns "rds-i-rds-vm-6c555602" into "rds-vm-6c555602".
// If the ID has no "-i-" separator, it is returned unchanged.
func diskNameFromInstanceID(instanceID string) string {
	if _, after, found := strings.Cut(instanceID, "-i-"); found && after != "" {
		return after
	}
	return instanceID
}

// instanceDiskPath builds the full qcow2 path for an instance.
func instanceDiskPath(imagesDir, instanceID string) string {
	return filepath.Join(imagesDir, diskNameFromInstanceID(instanceID)+".qcow2")
}

// buildAMIOffline: no_reboot == false. Stop, copy, start again.
func (s *InstanceService) buildAMIOffline(host hostConn, instance *domain.Instance, img *domain.Image) error {
	start := time.Now()
	wasRunning := instance.Status == domain.StatusRunning
	// srcDisk := s.hostService.Cfg.Libvirt.ImagesDir+"/"+instance.ID+".qcow2" // the VM's qcow2 file, NOT the images directory
	srcDisk := instanceDiskPath(s.hostService.Cfg.Libvirt.ImagesDir, instance.ID)
	amiLogf(img.ID, "offline build started: vm=%s host=%q wasRunning=%t src=%s dst=%s start =%v",
		instance.VMName, host.IP, wasRunning, srcDisk, img.DiskPath, start)


	if wasRunning {
		t := time.Now()
		amiLogf(img.ID, "stopping vm %s", instance.VMName)
		if err := s.libvirtClient.StopVM(host.IP, host.User, host.Key, instance.VMName); err != nil {
			amiLogf(img.ID, "ERROR stop vm failed: %v", err)
			return fmt.Errorf("stop vm: %w", err)
		}

		amiLogf(img.ID, "waiting for vm %s to shut off (timeout 2m)", instance.VMName)
		if err := s.libvirtClient.WaitForState(host.IP, host.User, host.Key, instance.VMName, "shut off", 2*time.Minute); err != nil {
			amiLogf(img.ID, "ERROR wait for shutdown failed after %s: %v", time.Since(t).Round(time.Millisecond), err)
			return fmt.Errorf("wait for shutdown: %w", err)
		}
		amiLogf(img.ID, "vm %s is shut off (took %s)", instance.VMName, time.Since(t).Round(time.Millisecond))

		instance.Status = domain.StatusStopped
		if err := s.repo.Update(instance); err != nil {
			amiLogf(img.ID, "WARN failed to save instance status=stopped: %v", err)
		}

		// Always bring the VM back, even if the copy fails.
		defer func() {
			amiLogf(img.ID, "restarting vm %s", instance.VMName)
			if err := s.libvirtClient.StartVM(host.IP, host.User, host.Key, instance.VMName); err != nil {
				amiLogf(img.ID, "ERROR failed to restart vm %s: %v (MANUAL ACTION NEEDED)", instance.VMName, err)
				return
			}
			instance.Status = domain.StatusRunning
			if err := s.repo.Update(instance); err != nil {
				amiLogf(img.ID, "WARN vm restarted but failed to save status=running: %v", err)
			}
			amiLogf(img.ID, "vm %s restarted", instance.VMName)
		}()
	} else {
		amiLogf(img.ID, "vm %s already stopped, skipping shutdown", instance.VMName)
	}
		return nil

	// // convert flattens any backing file (template) into a standalone, compressed image
	// t := time.Now()
	// amiLogf(img.ID, "copying disk (qemu-img convert) %s -> %s", srcDisk, img.DiskPath)
	// if err := s.libvirtClient.ConvertDisk(host.IP, host.User, host.Key, srcDisk, img.DiskPath); err != nil {
	// 	amiLogf(img.ID, "ERROR disk copy failed after %s: %v", time.Since(t).Round(time.Millisecond), err)
	// 	return fmt.Errorf("copy disk: %w", err)
	// }
	// amiLogf(img.ID, "disk copy done (took %s)", time.Since(t).Round(time.Millisecond))

	// // we update the ami status from building to ready

	// if err := s.finalizeImage(host, img); err != nil {
	// 	amiLogf(img.ID, "ERROR finalize failed: %v", err)
	// 	return err
	// }
	// amiLogf(img.ID, "offline build finished: size=%d bytes sha256=%s total=%s",
	// 	img.SizeBytes, img.SHA256, time.Since(start).Round(time.Millisecond))
	// return nil
}

// buildAMILive: no_reboot == true. VM keeps running.
func (s *InstanceService) buildAMILive(host hostConn, instance *domain.Instance, img *domain.Image) (err error) {
	start := time.Now()
	const diskTarget = "vda"
	srcDisk := s.hostService.Cfg.Libvirt.ImagesDir + "/" + instance.ID // the VM's qcow2 file, NOT the images directory

	// srcDisk := instance.DiskPath // the VM's qcow2 file, NOT the images directory
	overlay := fmt.Sprintf("/var/lib/libvirt/images/amis/%s.overlay.qcow2", img.ID)

	amiLogf(img.ID, "live build started: vm=%s host=%q disk=%s src=%s overlay=%s dst=%s",
		instance.VMName, host.IP, diskTarget, srcDisk, overlay, img.DiskPath)

	// 1. Disk-only external snapshot: writes now go to the overlay, base becomes read-only.
	//    Tries --quiesce (needs qemu-guest-agent); falls back to a crash-consistent snapshot.
	t := time.Now()
	amiLogf(img.ID, "creating disk-only snapshot (trying quiesce)")
	quiesced, err := s.libvirtClient.CreateDiskOnlySnapshot(
		host.IP, host.User, host.Key, instance.VMName, diskTarget, overlay, true)
	if err != nil {
		amiLogf(img.ID, "ERROR snapshot failed after %s: %v", time.Since(t).Round(time.Millisecond), err)
		return fmt.Errorf("create snapshot: %w", err)
	}
	img.Consistency = map[bool]string{true: "filesystem", false: "crash"}[quiesced]
	if quiesced {
		amiLogf(img.ID, "snapshot created with guest-agent quiesce (filesystem-consistent) in %s", time.Since(t).Round(time.Millisecond))
	} else {
		amiLogf(img.ID, "WARN snapshot created WITHOUT quiesce (crash-consistent only; is qemu-guest-agent running?) in %s", time.Since(t).Round(time.Millisecond))
	}

	// 2. Whatever happens next, merge the overlay back so the VM is left on its original disk.
	defer func() {
		t := time.Now()
		amiLogf(img.ID, "merging overlay back (blockcommit --active --pivot)")
		if cErr := s.libvirtClient.BlockCommitActive(
			host.IP, host.User, host.Key, instance.VMName, diskTarget); cErr != nil {
			amiLogf(img.ID, "ERROR blockcommit failed after %s: %v. VM is still writing to overlay %s (MANUAL ACTION: virsh blockcommit %s %s --active --pivot)",
				time.Since(t).Round(time.Millisecond), cErr, overlay, instance.VMName, diskTarget)
			if err == nil {
				err = fmt.Errorf("blockcommit: %w", cErr)
			}
			return // keep overlay: the VM is still writing to it
		}
		amiLogf(img.ID, "blockcommit pivoted back to base disk (took %s)", time.Since(t).Round(time.Millisecond))

		if dErr := s.libvirtClient.DeleteFile(host.IP, host.User, host.Key, overlay); dErr != nil {
			amiLogf(img.ID, "WARN failed to delete overlay %s: %v", overlay, dErr)
		} else {
			amiLogf(img.ID, "overlay %s deleted", overlay)
		}
	}()

	// 3. Copy the frozen base disk (nothing writes to it any more).
	t = time.Now()
	amiLogf(img.ID, "copying frozen base disk (qemu-img convert) %s -> %s", srcDisk, img.DiskPath)
	if err = s.libvirtClient.ConvertDisk(host.IP, host.User, host.Key, srcDisk, img.DiskPath); err != nil {
		amiLogf(img.ID, "ERROR disk copy failed after %s: %v", time.Since(t).Round(time.Millisecond), err)
		return fmt.Errorf("copy disk: %w", err)
	}
	amiLogf(img.ID, "disk copy done (took %s)", time.Since(t).Round(time.Millisecond))

	if err = s.finalizeImage(host, img); err != nil {
		amiLogf(img.ID, "ERROR finalize failed: %v", err)
		return err
	}
	amiLogf(img.ID, "live build finished: consistency=%s size=%d bytes sha256=%s total=%s",
		img.Consistency, img.SizeBytes, img.SHA256, time.Since(start).Round(time.Millisecond))

	return nil
}

// finalizeImage records size and checksum, and generalizes the image if needed.
func (s *InstanceService) finalizeImage(host hostConn, img *domain.Image) error {
	size, sum, err := s.libvirtClient.StatAndHash(host.IP, host.User, host.Key, img.DiskPath)
	if err != nil {
		return fmt.Errorf("stat image: %w", err)
	}
	img.SizeBytes = size
	img.SHA256 = sum
	s3Key := fmt.Sprintf("ami/%s", img.Name)

	// here get the presigned upload url,
	// then delete i from the current host,

	uploadReq := domain.CreatePresignedURLRequest{
		UserID:     img.UserID,
		GameID:     img.ID,
		AssetID:    img.ID,
		AssetType:  "ec2-amis",
		AssetName:  img.Name,
		BucketName: "ec2-amis",
		Key:        s3Key,
		Sha256:     sum,
	}

	presignedPath, err := s.publisher.PublishCreateUploadURL(uploadReq)
	if err != nil {
		log.Printf("failed to get presigned upload url for snapshot %s: %v", img.ID, err)
		return err
	}
	log.Printf("returned s3 url: %+v", presignedPath)
	fullUrl := fmt.Sprintf("%s%s", s.assetServiceBaseURL, presignedPath)

	storagePath, err := img.GetStoragePath()
	if err != nil {
		log.Printf("Volume path error: %v", err)
		return err
	}

	err = s.UploadAndDeleteFileViaAgent(host.IP, storagePath, fullUrl, "")
	if err != nil {
		log.Printf("Failed to uplaod and delete the volume %s from host", img.Name)
		return err
	}

	return nil
}
func (s *InstanceService) UploadAndDeleteFileViaAgent(hostIP, filePath, presignedURL, extra string) error {
	params := map[string]string{
		"path": filePath,
		"url":  presignedURL,
	}

	if extra != "" {
		params["extras"] = extra

		log.Printf("The e*xtras: %v", extra)

	}

	reqBody := HostActionRequest{
		Type:   "upload_and_delete",
		Params: params,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal host action request: %w", err)
	}

	url := fmt.Sprintf("http://%s:9030/host-actions", hostIP)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build host action request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("X-Agent-Key", l.agentAPIKey)

	// Large files take a while to upload — give it plenty of room.
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach agent on host %s: %w", hostIP, err)
	}
	defer resp.Body.Close()

	var result HostActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode agent response: %w", err)
	}

	if resp.StatusCode != http.StatusOK || result.Status != "success" {
		return fmt.Errorf("agent reported failure uploading file: %s", result.Message)
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
		go s.publisher.PublishInstanceEvent(instance.ImageProfile, domain.EventInstanceStarted, instance, "", sessionID, domain.VMStarted, "")
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

// TODO: Remove this function, its replacement is down below
// func (s *InstanceService) GetStatusChecks(id, userID string) ([]*domain.InstanceStatusCheck, error) {
// 	instance, err := s.GetInstance(id, userID)
// 	if err != nil {
// 		return nil, err
// 	}

// 	// For now, return mock data as requested
// 	now := time.Now()
// 	checks := []*domain.InstanceStatusCheck{
// 		{
// 			ID:        "system_check",
// 			Name:      "System Status Check",
// 			Status:    "passed",
// 			Details:   "Infrastructure health is optimal. No issues detected in the underlying hardware.",
// 			UpdatedAt: now.Add(-time.Hour), // Mocking some time in the past
// 		},
// 		{
// 			ID:        "instance_check",
// 			Name:      "Instance Status Check",
// 			Status:    "passed",
// 			Details:   "Instance reachability check passed. The operating system is responding to network requests.",
// 			UpdatedAt: now.Add(-time.Minute * 55),
// 		},
// 	}

// 	// Update status if instance is not running
// 	if instance.Status != domain.StatusRunning {
// 		for _, check := range checks {
// 			check.Status = "failing"
// 			check.Details = "Instance is not in running state, status checks cannot be performed."
// 		}
// 	}

// 	return checks, nil
// }

type checkResult struct {
	Status  string // "passed" | "failing" | "insufficient-data"
	Details string
}

const (
	checkTimeout = 4 * time.Second
)

func (s *InstanceService) GetStatusChecks(id, userID string) ([]*domain.InstanceStatusCheck, error) {

	instance, err := s.GetInstance(id, userID)
	if err != nil {
		return nil, err
	}
	log.Printf("Running status checks for instance: %+v with id: %s", instance.HostID, instance.VMName)

	now := time.Now()

	// Run both checks concurrently — they're independent and each has its own I/O timeout.
	sysCh := make(chan checkResult, 1)
	instCh := make(chan checkResult, 1)

	go func() { sysCh <- s.runSystemStatusCheck(instance) }()
	go func() { instCh <- s.runInstanceStatusCheck(instance) }()

	sysResult := <-sysCh
	instResult := <-instCh

	return []*domain.InstanceStatusCheck{
		{
			ID:        "system_check",
			Name:      "System Status Check",
			Status:    sysResult.Status,
			Details:   sysResult.Details,
			UpdatedAt: now,
		},
		{
			ID:        "instance_check",
			Name:      "Instance Status Check",
			Status:    instResult.Status,
			Details:   instResult.Details,
			UpdatedAt: now,
		},
	}, nil
}

// System Status Check: is the host + hypervisor itself healthy?
func (s *InstanceService) runSystemStatusCheck(instance *domain.Instance) checkResult {
	if instance.HostID == "" {
		return checkResult{"insufficient-data", "Instance has no assigned host."}
	}

	host, err := s.hostRepo.GetByID(instance.HostID)
	if err != nil || host == nil {
		log.Printf("[runSystemStatusCheck] Get host by id returned an error %v for host id: %v ", err, instance.HostID)
		return checkResult{"insufficient-data", "Could not resolve the host running this instance."}
	}

	// 1. Is the host reachable at all? (Tailscale IP, so this is an overlay-network check)
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	if err := tcpProbe(ctx, host.IP, "22"); err != nil {
		return checkResult{"failing", fmt.Sprintf("Host %s is unreachable: %v", host.IP, err)}
	}

	// 2. Is libvirtd on that host actually responding, not just the network?
	conn, err := s.libvirtClient.ConnectToHost(host.IP, host.SSHUser, host.SSHPrivateKey)
	if err != nil {
		return checkResult{"failing", fmt.Sprintf("libvirtd on host %s is not responding: %v", host.IP, err)}
	}
	defer conn.Close()

	if alive, err := conn.IsAlive(); err != nil || !alive {
		return checkResult{"failing", "libvirt connection to host is not alive."}
	}

	// 3. Does libvirt itself (not your DB) report the domain as running?
	dom, err := conn.LookupDomainByName(instance.VMName)
	if err != nil {
		return checkResult{"failing", fmt.Sprintf("Hypervisor has no record of domain %s.", instance.VMName)}
	}
	defer dom.Free()

	state, _, err := dom.GetState()
	if err != nil {
		return checkResult{"failing", "Could not read domain state from hypervisor."}
	}
	if state != libvirtgo.DOMAIN_RUNNING {
		return checkResult{"failing", fmt.Sprintf("Hypervisor reports domain state: %v.", state)}
	}

	return checkResult{"passed", "Host is reachable, libvirtd is responsive, and the hypervisor reports the domain as running."}
}

// Instance Status Check: is the guest OS itself alive and reachable?
func (s *InstanceService) runInstanceStatusCheck(instance *domain.Instance) checkResult {
	if instance.IP == "" && instance.PublicIP == "" {
		return checkResult{"insufficient-data", "Instance has no known IP address to check."}
	}

	ip := instance.IP

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()

	// TCP handshake against a port the guest OS itself must be up to answer.
	// SSH (22) is a reasonable default; use whatever port your images guarantee is open.
	if err := tcpProbe(ctx, ip, "22"); err != nil {
		return checkResult{"failing", fmt.Sprintf("Guest OS is not responding on the network: %v", err)}
	}

	// Optional but strong: if the image has qemu-guest-agent, ping it directly.
	// This proves the guest kernel is scheduling, not just that TCP/IP is up.
	// if s.libvirtClient != nil {
	// 	if err := s.libvirtClient.GuestAgentPing(instance.HostID, instance.VMName); err == nil {
	// 		return checkResult{"passed", "Guest OS is reachable and responded to a guest-agent ping."}
	// 	}
	// 	// Guest agent not installed/responding isn't necessarily a failure —
	// 	// don't downgrade the check just because the agent is absent.
	// }

	return checkResult{"passed", "Guest OS is reachable over the network."}
}

func tcpProbe(ctx context.Context, host, port string) error {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	conn.Close()
	return nil
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
	params map[string]any,
	rdsParticulars map[string]string,
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
			VMID:    instanceID,
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
		params,
		rdsParticulars,
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

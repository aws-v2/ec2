package application

import (
	"bytes"
	"context"
	"ec2-api/internal/domain/host"
	domain "ec2-api/internal/domain/instance"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"ec2-api/internal/vpcpkg"
	"strings"

	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)
var imageMap = map[string]string{
	// Ubuntu LTS versions
	"ubuntu-20.04":       "ubuntu-20.04.qcow2",
	"ubuntu-22.04-blue":  "ubuntu-22.04.qcow2",
	"ubuntu-22.04-green": "ubuntu-22.04.qcow2",
	"ubuntu-22.04-grey":  "ubuntu-22.04.qcow2",
	"ubuntu-22.04":  "ubuntu-22.04.qcow2",
	"ubuntu-24.04":       "ubuntu-24.04.qcow2",
	// Debian
	"debian-11":       "debian-11.qcow2",
	"debian-12":       "debian-12.qcow2",
	"rocky-8":         "rocky-8.qcow2",
	"rocky-9":         "rocky-9.qcow2",
	"almalinux-8":     "almalinux-8.qcow2",
	"almalinux-9":     "almalinux-9.qcow2",
	"fedora-39":       "fedora-39.qcow2",
	"fedora-40":       "fedora-40.qcow2",
	"centos-stream-9": "centos-stream-9.qcow2",
}

// prepareInstanceResources validates the requested image, ensures the base disk
// exists on disk, 
// generates the instance/VM identifiers, and 
// fetches an IAM
// token that the in-VM metrics agent will use to authenticate.

// TODO: put the returns ina struct
// type InstanceResourceResponse struct{
	
// }

func (s *InstanceService) prepareInstanceResources(req *domain.CreateInstanceRequest, userID string) (
	baseImagePath, instanceID, vmName, newDiskPath, instanceToken string, err error,
) {
	// Checks if we have that base image in servers files 
	baseImageName, ok := imageMap[req.Image]
	if !ok {
		available := make([]string, 0, len(imageMap))
		for k := range imageMap {
			available = append(available, k)
		}
		return "", "", "", "", "", fmt.Errorf("image %s not found. Available: %v", req.Image, available)
	}
	// s.imagesDir maps to this entry in config/config.go
	// ImagesDir: getEnv("IMAGES_DIR", "/var/lib/libvirt/images")
	// TODO: add an endpoint for the agentto download the base image if issues arise ,
	baseImagePath = filepath.Join(s.imagesDir, baseImageName) 
	if err = s.EnsureImageExists(req.Image, baseImagePath); err != nil {
		return "", "", "", "", "", fmt.Errorf("failed to ensure image exists: %w", err)
	}

	instanceID = fmt.Sprintf("i-%s", uuid.New().String()[:8])
	vmName = fmt.Sprintf("vm-%s", req.Name)
	newDiskPath = filepath.Join(s.imagesDir, fmt.Sprintf("%s.qcow2", vmName))

	// ai-worker VMs do not need an IAM token — the IAM service may not even
	// be subscribed on this NATS subject, so skip the request entirely to
	// avoid a 5-second timeout that would abort provisioning before any
	// lifecycle event is published.
	if s.publisher != nil && req.Profile != "ai-worker" {
		instanceToken, err = s.publisher.RequestInstanceToken(userID, instanceID)
		if err != nil {
			// Non-fatal: log and continue with an empty token so the VM can
			// still be provisioned and the INSTANCE_PROVISIONED event fires.
			log.Printf("[IAM] [WARN] Failed to get instance token for %s (profile=%s): %v — continuing without token", instanceID, req.Profile, err)
			instanceToken = ""
		} else {
			log.Printf("[IAM] [OK] Received instance token for %s", instanceID)
		}
	} else if req.Profile == "ai-worker" {
		log.Printf("[IAM] [SKIP] Skipping IAM token for ai-worker instance %s", instanceID)
	}

	return baseImagePath, instanceID, vmName, newDiskPath, instanceToken, nil
}

// allocateInstanceNetwork now calls vpcService directly instead of NATS.
func (s *InstanceService) allocateInstanceNetwork(ctx context.Context, userID, instanceID string, defaultVPC *vpcpkg.VPC) (
	vpcID, privateIP, gateway, bridgeName string, err error,
) {
	log.Printf("[NETWORK] Preparing network for instance %s in VPC %s", instanceID, defaultVPC.ID)

	// Allocate an IP within that VPC
	network, err := s.vpcService.AllocateInstanceNetwork(ctx, userID, instanceID, defaultVPC.ID)
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to allocate network for instance %s: %w", instanceID, err)
	}

	log.Printf("[NETWORK] [OK] Network ready for instance %s — IP: %s, gateway: %s, bridge: %s",
		instanceID, network.PrivateIP, network.Gateway, network.BridgeName)

	return network.VPCID, network.PrivateIP, network.Gateway, network.BridgeName, nil
}

// persistAndLaunch generates the SSH key pair, writes the instance record to
// the DB with the pre-allocated IP, then fires off async VM creation.
// If the DB write fails the pre-allocated IP is released back to the pool.
func (s *InstanceService) persistAndLaunch(
	req *domain.CreateInstanceRequest,
	userID, instanceID, vmName, newDiskPath, baseImagePath,
	bridgeName, privateIP, gateway, vpcID, instanceToken string, bestHost *host.Host,
) (*domain.Instance, error) {
	keyPair, err := GenerateSSHKeyPair()
	if err != nil {
		log.Printf("[SSH] [ERROR] Failed to generate SSH key pair for instance %s: %v", instanceID, err)
		return nil, fmt.Errorf("failed to generate SSH key pair: %w", err)
	}



	instance := &domain.Instance{
		ID:            instanceID,
		VMName:        vmName,
		Image:         req.Image,
		CPU:           req.CPU,
		RAM:           req.RAM,
		PublicSSHKey:  keyPair.PublicKeyAuth,
		PrivateSshKey: keyPair.PrivateKeyPEM,
		Status:        domain.StatusPending,
		IP:            privateIP,
		PublicIP:      "",
		ProxmoxID:     0,
		CreatedAt:     time.Now(),
		UserID:        userID,
		VPCID:         vpcID,
		SessionID:     req.SessionID,
	}
		if req.Profile != "" {
		if req.Profile == "gamelift" {
			instance.StorageSize = 5
		}else if req.Profile == "ai-worker" {
			instance.StorageSize = 5
		}else{
			instance.StorageSize = 5
		}



	}

	if err := s.repo.Create(instance); err != nil {
		if s.vpcService != nil && privateIP != "" {
			if releaseErr := s.vpcService.ReleaseInstanceNetwork(context.Background(), instanceID); releaseErr != nil {
				log.Printf("[NETWORK] [WARN] Failed to release IP for failed instance %s: %v", instanceID, releaseErr)
			}
		}
		go s.publisher.PublishInstanceEvent(domain.EventInstanceError, &domain.Instance{}, "", req.SessionID)
		return nil, fmt.Errorf("failed to save instance: %w", err)
	}

	// Extract assets from manifest parameters
	assets := []domain.AssetConfigs{}
	if req.Manifest.Parameters != nil {
		if url, ok := req.Manifest.Parameters["ASSET_URL"]; ok {
			asset := domain.AssetConfigs{
				Name:   req.Manifest.Name,
				URL:    url,
				Path:   req.Manifest.Parameters["ASSET_PATH"],
				SHA256: req.Manifest.Parameters["ASSET_SHA256"],
			}
			if asset.Path == "" {
				asset.Path = fmt.Sprintf("/var/lib/assets/%s", asset.Name)
			}
			assets = append(assets, asset)
		}
	}

	// Reconcile network + asset download on agent.
	// This is SYNCHRONOUS — we must not launch the VM until the agent
	// confirms assets are on disk. ReconcileNetwork now returns a real
	// error when assets are present and the call fails.
	if _, err := s.ReconcileNetwork(bestHost.IP, host.NetworkReconcileRequest{
		VPCID: vpcID,
		Bridge: host.BridgeConfig{
			Name:    bridgeName,
			Gateway: gateway,
			CIDR:    "",
		},
		IP:      privateIP,
		Gateway: gateway,
		Assets:  assets,
		SessionID: req.SessionID,
		VMID: instanceID,

	}); err != nil {
		log.Printf("[NETWORK] reconcile failed for instance %s: %v", instanceID, err)
		go s.publisher.PublishInstanceEvent(domain.EventInstanceError, &domain.Instance{}, "", req.SessionID)
		s.markTerminatedAndReleaseNetwork(instance, newDiskPath)
		return nil, fmt.Errorf("network reconcile failed: %w", err)
	}

assetPath := "<none>"
if len(assets) > 0 {
	assetPath = assets[0].Path
}

log.Printf(
	"[NETWORK] reconcile complete for instance %s — asset path: %s",
	instanceID,
	assetPath,
)



	go s.createVMAsync(instance, req, baseImagePath, newDiskPath, bridgeName, privateIP, gateway, instanceToken, keyPair, assets)

	instance.PrivateSshKey = keyPair.PrivateKeyPEM
	return instance, nil
}



func (s *InstanceService) ReconcileNetwork(agentIP string, req host.NetworkReconcileRequest) (bool, error) {

	url := fmt.Sprintf("http://%s:9030/reconcile/network", agentIP)

	body, err := json.Marshal(req)
	if err != nil {
		go s.publisher.PublishInstanceEvent(domain.EventInstanceError, &domain.Instance{}, "", req.SessionID)
		log.Printf("[NETWORK] marshal error: %v", err)
		return false, err
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		go s.publisher.PublishInstanceEvent(domain.EventInstanceError, &domain.Instance{}, "", req.SessionID)
		log.Printf("[NETWORK] request build error: %v", err)
		return false, err
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Use a long timeout — asset downloads can be large.
	// The default client timeout (if set) may be too short and cause
	// a silent false-nil return that races with InjectAssets.
	reconcileClient := &http.Client{
		Timeout: 10 * time.Minute,
	}

	resp, err := reconcileClient.Do(httpReq)
	if err != nil {
		go s.publisher.PublishInstanceEvent(domain.EventInstanceError, &domain.Instance{}, "", req.SessionID)
		log.Printf("[NETWORK] agent call failed: %v", err)
		return false, fmt.Errorf("agent reconcile call failed: %w", err) // 👈 now a real error
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[NETWORK] agent returned %d: %s", resp.StatusCode, string(body))
		return false, fmt.Errorf("agent reconcile returned %d: %s", resp.StatusCode, string(body)) // 👈 real error
	}

	log.Printf("[NETWORK] reconciliation successful for %s", agentIP)
	return true, nil
}


func (s *InstanceService) buildOverlay(
    instanceID string,
    baseImagePath string,
    overlayPath string,
    diskSizeGB int,
) error {
    size := fmt.Sprintf("%dG", diskSizeGB)
    if diskSizeGB <= 0 {
        size = "20G"
    }

    cmd := exec.Command(
        "qemu-img", "create",
        "-f", "qcow2",
        "-F", "qcow2",
        "-b", baseImagePath,
        overlayPath,
        size,
    )
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"[overlay][%s] failed: %w output=%s",
			instanceID, err, string(output),
		)
	}

	return nil
}

func (s *InstanceService) transferOverlayToHost(
	hostIP string,
	hostUser string,
	privateKey string,
	localOverlayPath string,
	remoteOverlayPath string,
) error {

	formattedKey := strings.ReplaceAll(privateKey, "\\n", "\n")
	formattedKey = strings.TrimSpace(formattedKey) + "\n" // 👈 fix

	keyFile, err := os.CreateTemp("", "id_rsa_*")
	if err != nil {
		return err
	}
	defer os.Remove(keyFile.Name())

	if _, err := keyFile.Write([]byte(formattedKey)); err != nil {
		return err
	}

	keyFile.Close()

	if err := os.Chmod(keyFile.Name(), 0600); err != nil {
		return err
	}

	fileInfo, err := os.Stat(localOverlayPath)
	if err != nil {
		return fmt.Errorf("failed to stat overlay file: %w", err)
	}
	sizeMB := float64(fileInfo.Size()) / (1024 * 1024)
	log.Printf("[transferOverlayToHost] transferring %s -> %s@%s:%s | size: %.2f MB (%d bytes)",
		localOverlayPath, hostUser, hostIP, remoteOverlayPath, sizeMB, fileInfo.Size())

	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "BatchMode=yes",
		"-i", keyFile.Name(),
		localOverlayPath,
		fmt.Sprintf("%s@%s:%s", hostUser, hostIP, remoteOverlayPath),
	}

	cmd := exec.Command("scp", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("scp failed: %w output=%s", err, string(output))
	}

	return nil
}

func (s *InstanceService) createVMAsync(
	instance *domain.Instance,
	req *domain.CreateInstanceRequest,
	baseImagePath string,
	newDiskPath string,
	bridgeName string,
	privateIP string,
	gateway string,
	instanceToken string,
	keyPair *SSHKeyPair,
	assets []domain.AssetConfigs,
) {
	// ---------------------------------------------------
	// Resolve host
	// ---------------------------------------------------

	var remoteHostIP string
	var remoteHostUser string

	if instance.HostID != "" {
		host, err := s.hostService.GetHost(instance.HostID)
		if err == nil && host != nil {
			remoteHostIP = host.IP
			remoteHostUser = host.SSHUser
		}
	}

	if remoteHostUser == "" {
		remoteHostUser = "root"
	}

	// ---------------------------------------------------
	// Absolute paths
	// ---------------------------------------------------

	absBase, _ := filepath.Abs(baseImagePath)
	absOverlay, _ := filepath.Abs(newDiskPath)

	// ---------------------------------------------------
	// STEP 1 — BUILD OVERLAY
	// ---------------------------------------------------

	log.Printf("[VM] building overlay %s", absOverlay)

	err := s.buildOverlay(instance.ID, absBase, absOverlay, instance.StorageSize)
	if err != nil {
		log.Printf("[VM] overlay creation failed: %v", err)
		go s.publisher.PublishInstanceEvent(domain.EventInstanceError, instance, "", req.SessionID)

		s.markTerminatedAndReleaseNetwork(instance, absOverlay)
		return
	}

	// ---------------------------------------------------
	// STEP 2 — BUILD CLOUD INIT ISO
	// ---------------------------------------------------

	keys := []string{}

	for _, key := range []string{
		keyPair.PublicKeyAuth,
		s.systemPubKey,
	} {
		key = strings.TrimSpace(key)

		if key != "" {
			keys = append(keys, key)
		}
	}

	combinedKeys := strings.Join(keys, "\n")

	isoPath, cleanupFn, err := s.libvirtClient.CreateCloudInitISO(
		instance.VMName,
		combinedKeys,
		privateIP,
		gateway,
		instanceToken,
		req.Profile,
		req.Manifest.Parameters,
	)

	if err != nil {
		log.Printf("[VM] cloud-init creation failed: %v", err)
		go s.publisher.PublishInstanceEvent(domain.EventInstanceError, instance, "", req.SessionID)

		s.markTerminatedAndReleaseNetwork(instance, absOverlay)
		return
	}

	defer cleanupFn()

	log.Printf("[VM] cloud-init iso created %s", isoPath)

	// ---------------------------------------------------
	// STEP 3 — TRANSFER OVERLAY
	// ---------------------------------------------------

	err = s.transferOverlayToHost(
		remoteHostIP,
		remoteHostUser,
		s.ec2PrivateKey,
		absOverlay,
		absOverlay,
	)

	if err != nil {
		log.Printf("[VM] overlay transfer failed*: %v", err)
		go s.publisher.PublishInstanceEvent(domain.EventInstanceError, instance, "", req.SessionID)

		s.markTerminatedAndReleaseNetwork(instance, absOverlay)
		return
	}

	log.Printf("[VM] overlay transferred")

	// ---------------------------------------------------
	// STEP 4 — TRANSFER CLOUD INIT ISO
	// ---------------------------------------------------

	err = s.transferOverlayToHost(
		remoteHostIP,
		remoteHostUser,
		s.ec2PrivateKey,
		isoPath,
		isoPath,
	)

	if err != nil {
		log.Printf("[VM] iso transfer failed: %v", err)
		go s.publisher.PublishInstanceEvent(domain.EventInstanceError, instance, "", req.SessionID)

		s.markTerminatedAndReleaseNetwork(instance, absOverlay)
		return
	}

	log.Printf("[VM] iso transferred")

	// ---------------------------------------------------
	// STEP 4.5 — INJECT ASSETS
	// ---------------------------------------------------

	if len(assets) > 0 {
		err = s.libvirtClient.InjectAssets(
			remoteHostIP,
			remoteHostUser,
			s.ec2PrivateKey,
			absOverlay,
			assets,
		)

		if err != nil {
			log.Printf("[VM] asset injection failed: %v", err)
			go s.publisher.PublishInstanceEvent(domain.EventInstanceError, instance, "", req.SessionID)

			s.markTerminatedAndReleaseNetwork(instance, absOverlay)
			return
		}

		log.Printf("[VM] assets injected successfully")
	}

	// ---------------------------------------------------
	// STEP 5 — START VM
	// ---------------------------------------------------

	vmID, err := s.libvirtClient.CreateAndStartVM(
		remoteHostIP,
		remoteHostUser,
		s.ec2PrivateKey,

		instance.VMName,

		absOverlay,
		isoPath,

		req.CPU,
		req.RAM,

		bridgeName,
		privateIP,
		gateway,
		req.Profile,
	)

	if err != nil {
		log.Printf("[VM] failed to start vm: %v", err)
		go s.publisher.PublishInstanceEvent(domain.EventInstanceError, &domain.Instance{}, "", req.SessionID)

		s.markTerminatedAndReleaseNetwork(instance, absOverlay)
		return
	}

	// ---------------------------------------------------
	// STEP 6 — UPDATE INSTANCE
	// ---------------------------------------------------


	instance.Status = domain.StatusRunning
	instance.ProxmoxID = vmID

	if err := s.repo.Update(instance); err != nil {
		go s.publisher.PublishInstanceEvent(domain.EventInstanceError, &domain.Instance{}, "", req.SessionID)

		log.Printf("[VM] failed to update instance: %v", err)
		return
	}

	log.Printf(
		"[VM] instance %s running on host %s",
		instance.ID,
		remoteHostIP,
	)

	log.Printf(
		"---------------------------------",
	)

	agentWS := fmt.Sprintf("ws://%s:%d", remoteHostIP, s.agentPort)

	if err := s.publisher.PublishInstanceEvent(domain.EventInstanceProvisioned, instance, agentWS, req.SessionID); err != nil {
		log.Printf("[VM] failed to publish provisioned event: %v", err)
	}

	// Also publish INSTANCE_STARTED so lifecycle monitors know the VM is active and has an IP.
	if err := s.publisher.PublishInstanceEvent(domain.EventInstanceStarted, instance, agentWS, req.SessionID); err != nil {
		log.Printf("[VM] failed to publish started event: %v", err)
	}

}



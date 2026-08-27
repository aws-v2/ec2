package application

import (
	"bytes"
	"context"
	"ec2-api/internal/domain/host"
	domain "ec2-api/internal/domain/instance"
	"ec2-api/internal/vpcpkg"
	"encoding/json"
	"io"
	"net/http"
	"os"
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
	"ubuntu-20.04":                     "ubuntu-20.04.qcow2",
	"ubuntu-22.04-blue":                "ubuntu-22.04.qcow2",
	"ubuntu-22.04-green":               "ubuntu-22.04.qcow2",
	"ubuntu-22.04-grey":                "ubuntu-22.04.qcow2",
	"ubuntu-22.04:version:sha256-blue": "ubuntu-22.04.qcow2",
	"ubuntu-22.04":                     "ubuntu-22.04.qcow2",
	"ubuntu-24.04":                     "ubuntu-24.04.qcow2",
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
	// TODO: migrate thenamingof keys in this map to this new scheme,
	// due tochangin this  baseImageName, ok := imageMap[req.Profile]  in the prepareInstanceResources from
	// baseImageName, ok := imageMap[req.Image]

	"lambda":    "lambda-template.qcow2",
	"rds":       "rds-v4.qcow2",
	"vanilla":   "rds-template.qcow2",
	"ai-worker": "ubuntu-22.04.qcow2",
	"games":     "ubuntu-22.04.qcow2",
	"gamelift":  "ubuntu-22.04.qcow2",
	"worker":    "rds-template.qcow2",
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
	baseImageName, ok := imageMap[req.Profile]
	if !ok {
		available := make([]string, 0, len(imageMap))
		for k := range imageMap {
			available = append(available, k)
		}
		return "", "", "", "", "", fmt.Errorf("image %s not found. Available: %v", req.Image, available)
	}
	// TODO: add an endpoint for the agentto download the base image if issues arise on the host
	// and the baseimage is not found,
	baseImagePath = filepath.Join(s.imagesDir, baseImageName)
	if err = s.EnsureImageExists(req.Image, baseImagePath); err != nil {
		return "", "", "", "", "", fmt.Errorf("failed to ensure image exists: %w", err)
	}

	instanceID = fmt.Sprintf(uuid.New().String()[:8])
 
	vmName = fmt.Sprintf("%s-vm-%s",req.Profile, instanceID)
	instanceID= fmt.Sprintf("%s-i-%s",req.Profile, vmName) 
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
	ctx context.Context,
	req *domain.CreateInstanceRequest,
	userID, instanceID, vmName, newDiskPath, baseImagePath,
	bridgeName, privateIP, gateway, vpcID, instanceToken string, bestHost *host.Host, publicGateway *host.Host,
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
		CPU:           req.Specs.CPU,
		RAM:           req.Specs.RAM,
		PublicSSHKey:  keyPair.PublicKeyAuth,
		PrivateSshKey: keyPair.PrivateKeyPEM,
		Status:        domain.StatusPending,
		IP:            privateIP,
		PublicIP:      publicGateway.IP,
		PublicPort:      publicGateway.IP,
		ProxmoxID:     0,
		CreatedAt:     time.Now(),
		UserID:        userID,
		VPCID:         vpcID,
		SessionID:     req.SessionID,
		StorageSize:   req.Specs.Storage,
		ImageProfile: req.Profile,
	}

	fmt.Printf("\n1:the instance specs are as follows cpu: %v ram:%v storage:%v  \n", req.Specs.CPU, req.Specs.RAM, instance.StorageSize)

	if err := s.repo.Create(instance); err != nil {
		if s.vpcService != nil && privateIP != "" {
			if releaseErr := s.vpcService.ReleaseInstanceNetwork(context.Background(), instanceID); releaseErr != nil {
				log.Printf("[NETWORK] [WARN] Failed to release IP for failed instance %s: %v", instanceID, releaseErr)
			}
		}
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.RepoCreateRecord, req.ResourceID)
		return nil, fmt.Errorf("failed to save instance: %w", err)
	}
	log.Printf("[NETWORK-] check log %v", publicGateway.ID)

	gatewayServer, serr := s.hostRepo.GetByGatewayHost(ctx, publicGateway.ID)
	if serr != nil {
		log.Printf("[NETWORK] gateway reconcile failed for instance %v: %v", gatewayServer, serr)

		return nil, fmt.Errorf("Failed to get the gateway server: %w", serr)

	}
	log.Printf("[NETWORK] gateway reconcile values %v", gatewayServer)
	// Extract assets from manifest parameters
	assets := []domain.Asset{}
	if req.Assets != nil {
		assets = req.Assets

	}

	var forwardingPort int
	forwardingPort =req.ForwardingPort
	switch req.Profile {
	case "rds":
		forwardingPort = 5432
	case "gamelift":
		forwardingPort = 9030
	case "ai-worker":
		forwardingPort = 9030
	case "lambda":
		forwardingPort = 9087
	default:
		forwardingPort = 22
	}

	// Reconcile network + asset download on agent.
	// This is SYNCHRONOUS — we must not launch the VM until the agent
	// confirms assets are on disk. ReconcileNetwork now returns a real
	// error when assets are present and the call fails.
	var reqForward = host.AddForwardingRuleRequest{
		ExternalPort: gatewayServer.Port,
		TargetIP:     instance.IP,
		TargetPort:   forwardingPort,
		Protocol:     "tcp",
	}
	if _, err := s.ReconcileNetwork(req.Profile, bestHost.IP, host.NetworkReconcileRequest{
		VPCID: vpcID,
		Bridge: host.BridgeConfig{
			Name:    bridgeName,
			Gateway: gateway,
			CIDR:    "",
		},
		IP:        privateIP,
		Gateway:   gateway,
		Assets:    assets,
		SessionID: req.SessionID,
		VMID:      instanceID,
		// ExternalPort:publicGateway,
		ForwardingRule: reqForward,
	}); err != nil {
		log.Printf("[NETWORK] reconcile failed for instance %s: %v", instanceID, err)
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.RepoReConcile, req.ResourceID)
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

	// get an external port withthis logic,
	// get all from the gateway_ports table whosoe status is available
	//select the recent,

	// if strings.ToLower(s.ENV) != "dev" {

	if _, err := s.AddForwardingRule(
		publicGateway.IP,
		bestHost.IP, //vm_ip

		gatewayServer.Port,
		forwardingPort,
		req.Profile,
		req.SessionID,
	); err != nil {
		log.Printf("[NETWORK] reconcile failed for instance %s: %v", instanceID, err)
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.RepoReConcile, req.ResourceID)
		s.markTerminatedAndReleaseNetwork(instance, newDiskPath)
		return nil, fmt.Errorf("network add a forwading rul failed: %w", err)
	}
	// }

	log.Printf("\n\nBefore updating the port status %v \n\n", gatewayServer)

	updatedPortsCount, err := s.hostRepo.UpdatePortStatus(ctx, "TAKEN", gatewayServer.GatewayID, instanceID, gatewayServer.Port)
	if updatedPortsCount <= 0 {
		return nil, fmt.Errorf("could not update the port status of the gateway%v", err)

	}
	log.Printf("Port status updated")
	go s.createVMAsync(instance, req, baseImagePath, newDiskPath, bridgeName, privateIP, gateway, instanceToken, keyPair, assets, bestHost)

	instance.PrivateSshKey = keyPair.PrivateKeyPEM
	instance.GatewayIP = publicGateway.IP
	instance.GatewayPort = gatewayServer.Port
	return instance, nil
}

// type AddForwardingRuleRequest struct {
// 	ExternalPort int    `json:"external_port"`
// 	TargetIP     string `json:"target_ip"`
// 	TargetPort   int    `json:"target_port"`
// 	Protocol     string `json:"protocol"` // tcp/udp
// }

type RemoveForwardingRuleRequest struct {
	ExternalPort int    `json:"external_port"`
	TargetIP     string `json:"target_ip"`
	TargetPort   int    `json:"target_port"`
	Protocol     string `json:"protocol"`
}

func (s *InstanceService) RemoveForwardingRule(
	gatewayIP string,
	targetIP string,
	targetPort int,
	externalPort int,
	profile, SessionID string,
) (bool, error) {

	req := RemoveForwardingRuleRequest{
		ExternalPort: externalPort,
		TargetIP:     targetIP,
		TargetPort:   targetPort,
		Protocol:     "tcp",
	}

	url := fmt.Sprintf("http://%s:9030/gateway/remove", gatewayIP)

	body, err := json.Marshal(req)
	if err != nil {
		go s.publisher.PublishInstanceEvent(profile, domain.EventInstanceError, &domain.Instance{}, "", SessionID, domain.RepoCreateRecord, "")
		log.Printf("[NETWORK] marshal error: %v", err)
		return false, err
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		go s.publisher.PublishInstanceEvent(profile, domain.EventInstanceError, &domain.Instance{}, "", SessionID, domain.NetworkReconcile, "")
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
		go s.publisher.PublishInstanceEvent(profile, domain.EventInstanceError, &domain.Instance{}, "", SessionID, domain.NetworkReconcile, "")
		log.Printf("[NETWORK] agent call failed: %v", err)
		return false, fmt.Errorf("agent reconcile call failed: %w", err) // 👈 now a real error
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[NETWORK1] agent returned %d: %s", resp.StatusCode, string(body))
		return false, fmt.Errorf("agent reconcile returned %d: %s", resp.StatusCode, string(body)) // 👈 real error
	}

	log.Printf("[NETWORK] reconciliation successful for %s", gatewayIP)
	return true, nil
}

func (s *InstanceService) AddForwardingRule(
	gatewayIP string,
	targetIP string, //esstt host ip
	targetPort int,
	externalPort int,
	profile, SessionID string,

) (bool, error) {

	req := host.AddForwardingRuleRequest{
		ExternalPort: targetPort, // gateway port
		TargetIP:     targetIP,     //vmport
		TargetPort:   externalPort,
		Protocol:     "tcp",
	}
	// 	req := AddForwardingRuleRequest{
	// 	ExternalPort: externalPort, // gateway port
	// 	TargetIP:     vmIP,     //vmport
	// 	TargetPort:   3333,
	// 	Protocol:     "tcp",
	// }

	url := fmt.Sprintf("http://%s:9030/gateway/add", gatewayIP)

	body, err := json.Marshal(req)
	if err != nil {
		go s.publisher.PublishInstanceEvent(profile, domain.EventInstanceError, &domain.Instance{}, "", SessionID, domain.RepoCreateRecord, "")
		log.Printf("[NETWORK] marshal error: %v", err)
		return false, err
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		go s.publisher.PublishInstanceEvent(profile, domain.EventInstanceError, &domain.Instance{}, "", SessionID, domain.NetworkReconcile, "")
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
		go s.publisher.PublishInstanceEvent(profile, domain.EventInstanceError, &domain.Instance{}, "", SessionID, domain.NetworkReconcile, "")
		log.Printf("[NETWORK] agent call failed: %v", err)
		return false, fmt.Errorf("agent reconcile call failed: %w", err) // 👈 now a real error
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[NETWORK3] agent returned %d: %s", resp.StatusCode, string(body))
		return false, fmt.Errorf("agent reconcile returned %d: %s", resp.StatusCode, string(body)) // 👈 real error
	}

	log.Printf("[NETWORK] reconciliation successful for %s", gatewayIP)
	// TODO: HErE we shuldmark the port "TAKEN"
	return true, nil
}

func (s *InstanceService) ReconcileNetwork(profile, agentIP string, req host.NetworkReconcileRequest) (bool, error) {

	url := fmt.Sprintf("http://%s:9030/reconcile/network", agentIP)

	body, err := json.Marshal(req)
	if err != nil {
		go s.publisher.PublishInstanceEvent(profile, domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.RepoCreateRecord, "")
		log.Printf("[NETWORK] marshal error: %v", err)
		return false, err
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		go s.publisher.PublishInstanceEvent(profile, domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.NetworkReconcile, "")
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
		go s.publisher.PublishInstanceEvent(profile, domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.NetworkReconcile, "")
		log.Printf("[NETWORK] agent call failed: %v", err)
		return false, fmt.Errorf("agent reconcile call failed: %w", err) // 👈 now a real error
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[NETWORK2] agent returned %d: %s", resp.StatusCode, string(body))
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

	fmt.Printf("Thedisk size that reaches this function is  %d", diskSizeGB)

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
	assets []domain.Asset,
	bestHost *host.Host,
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
	fmt.Printf("\n2:the instance specs are as follows cpu: %v ram:%v storage:%v  \n", instance.CPU, instance.RAM, instance.StorageSize)

	log.Printf("[VM] building overlay %s", absOverlay)

	err := s.buildOverlay(instance.ID, absBase, absOverlay, instance.StorageSize)
	if err != nil {
		log.Printf("[VM] overlay creation failed: %v", err)
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, instance, "", req.SessionID, domain.CreateOverlay, req.ResourceID)

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

	// TODO: remove this
	// in the near future
	// params := make(map[string]string)
	params := req.EnvParams

	
	isoPath, cleanupFn, err := s.libvirtClient.CreateCloudInitISO(
		instance.VMName,
		combinedKeys,
		privateIP,
		gateway,
		instanceToken,
		req.Profile,
		params,
	)

	if err != nil {
		log.Printf("[VM] cloud-init creation failed: %v", err)
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, instance, "", req.SessionID, domain.BuildCloudInit, req.ResourceID)

		s.markTerminatedAndReleaseNetwork(instance, absOverlay)
		return
	}

	defer cleanupFn()

	log.Printf("[VM] cloud-init iso created %s", isoPath)
	log.Printf("[VM] cloud-init iso created remotehost ip %s remote user %s len of pk %d", remoteHostIP, remoteHostUser,len(s.ec2PrivateKey))

	// ---------------------------------------------------
	// STEP 3 — TRANSFER OVERLAY
	// ---------------------------------------------------
		// remoteHostIP="192.168.122.1"

	// s.ec2PrivateKey = bestHost.SSHPrivateKey
	err = s.transferOverlayToHost(
		remoteHostIP,
		remoteHostUser,
		s.ec2PrivateKey,
		absOverlay,
		absOverlay,
	)

	if err != nil {
		log.Printf("[VM] overlay transfer failed*: %v", err)
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, instance, "", req.SessionID, domain.TransferOverlay, req.ResourceID)

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
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, instance, "", req.SessionID, domain.TransferOverlay, req.ResourceID)

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

			req.Profile,
			req.ResourceID,
		)

		if err != nil {
			log.Printf("[VM] asset injection failed: %v", err)
			go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, instance, "", req.SessionID, domain.InjectOverlay, req.ResourceID)

			s.markTerminatedAndReleaseNetwork(instance, absOverlay)
			return
		}

		log.Printf("[VM] assets injected successfully")
	}

	fmt.Printf("\n 1: the cpu: %d the ram:%d \n", req.Specs.CPU, req.Specs.RAM)

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

		req.Specs.CPU,
		req.Specs.RAM,

		bridgeName,
		privateIP,
		gateway,
		req.Profile,
	)

	if err != nil {
		log.Printf("[VM] failed to start vm: %v", err)
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.StartVM, req.ResourceID)

		s.markTerminatedAndReleaseNetwork(instance, absOverlay)
		return
	}

	// ---------------------------------------------------
	// STEP 6 — UPDATE INSTANCE
	// ---------------------------------------------------

	instance.Status = domain.StatusRunning
	instance.ProxmoxID = vmID

	if err := s.repo.Update(instance); err != nil {
		go s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceError, &domain.Instance{}, "", req.SessionID, domain.RepoUpadateRecord, req.ResourceID)

		log.Printf("[VM] failed to update instance: %v", err)
		return
	}

	log.Printf(
		"[VM] instance %s running on host %s",
		instance.ID,
		remoteHostIP,
	)

	log.Printf(
		"-----------------------8----------%s", req.ResourceID)

	agentWS := fmt.Sprintf("ws://%s:%d", remoteHostIP, s.agentPort)

	if err := s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceProvisioned, instance, agentWS, req.SessionID, domain.VMProvisioned, req.ResourceID); err != nil {
		log.Printf("[VM] failed to publish provisioned event: %v", err)
	}

	// Also publish INSTANCE_STARTED so lifecycle monitors know the VM is active and has an IP.
	if err := s.publisher.PublishInstanceEvent(req.Profile, domain.EventInstanceStarted, instance, agentWS, req.SessionID, domain.VMStarted, req.ResourceID); err != nil {
		log.Printf("[VM] failed to publish started event: %v", err)
	}

}






/*

i ahve this situation, 
i am using the same machine as my gateway server and the 
provisioning host, 
i ahve it register itself in the dabae with hosttype gateway and also 
so i ahve this 

ec2_db1=# select id, hostname,hosttype from hosts;
                     id                      |              hostname               | hosttype 
---------------------------------------------+-------------------------------------+----------
 gateway-18:60:24:4f:4a:13:root:6d617274696e | 18:60:24:4f:4a:13:root:6d617274696e | gateway
 grey-18:60:24:4f:4a:13:root:6d617274696e    | 18:60:24:4f:4a:13:root:6d617274696e | grey
 18:60:24:4f:4a:13:root:6d617274696e         | 18:60:24:4f:4a:13:root:6d617274696e | hybrid


 now issue comes in in the forwarding rule 
 when creating an rds vm(the ip address of the vm is 10.0.5.121) we pick the randomport 40093,which means
 whatever traffic we get on our machine on port port forward it to the vm,
 these ae the two forwarding rules tha





*/
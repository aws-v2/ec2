package application

import (
	domain "ec2-api/internal/domain/instance"

	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// prepareInstanceResources validates the requested image, ensures the base disk
// exists on disk, generates the instance/VM identifiers, and fetches an IAM
// token that the in-VM metrics agent will use to authenticate.
func (s *InstanceService) prepareInstanceResources(req *domain.CreateInstanceRequest, userID string) (
	baseImagePath, instanceID, vmName, newDiskPath, instanceToken string, err error,
) {
	baseImageName, ok := imageMap[req.Image]
	if !ok {
		available := make([]string, 0, len(imageMap))
		for k := range imageMap {
			available = append(available, k)
		}
		return "", "", "", "", "", fmt.Errorf("image %s not found. Available: %v", req.Image, available)
	}

	baseImagePath = filepath.Join(s.imagesDir, baseImageName)
	if err = s.EnsureImageExists(req.Image, baseImagePath); err != nil {
		return "", "", "", "", "", fmt.Errorf("failed to ensure image exists: %w", err)
	}

	instanceID = fmt.Sprintf("i-%s", uuid.New().String()[:8])
	vmName = fmt.Sprintf("vm-%s", instanceID)
	newDiskPath = filepath.Join(s.imagesDir, fmt.Sprintf("%s.qcow2", vmName))

	if s.publisher != nil {
		instanceToken, err = s.publisher.RequestInstanceToken(userID, instanceID)
		if err != nil {
			log.Printf("[IAM] [ERROR] Failed to get instance token for %s: %v", instanceID, err)
			return "", "", "", "", "", fmt.Errorf("failed to get instance token: %w", err)
		}
		log.Printf("[IAM] [OK] Received instance token for %s", instanceID)
	}

	return baseImagePath, instanceID, vmName, newDiskPath, instanceToken, nil
}

// allocateInstanceNetwork resolves the VPC (user-supplied or default) and asks
// the network service to pre-allocate an IP, gateway, and bridge. The IP is
// baked into cloud-init as a static address — no DHCP polling required.
func (s *InstanceService) allocateInstanceNetwork(req *domain.CreateInstanceRequest, userID, instanceID string) (
	vpcID, privateIP, gateway, bridgeName string, err error,
) {
	if s.publisher == nil {
		return "", "", "", "", nil
	}

	log.Printf("[NETWORK] Preparing network for instance %s", instanceID)

	if req.VPCID != "" {
		valid, err := s.publisher.ValidateVPC(userID, req.VPCID)
		if err != nil {
			log.Printf("[VPC] [ERROR] VPC validation failed for user %s, vpc %s: %v", userID, req.VPCID, err)
			return "", "", "", "", fmt.Errorf("failed to validate VPC: %w", err)
		}
		if !valid {
			log.Printf("[VPC] [FAILURE] Invalid VPC %s for user %s", req.VPCID, userID)
			return "", "", "", "", fmt.Errorf("invalid VPC ID: %s", req.VPCID)
		}
		vpcID = req.VPCID
		log.Printf("[VPC] [OK] Validated VPC %s for instance %s", vpcID, instanceID)
	} else {
		vpcID, bridgeName, err = s.publisher.GetDefaultVPC(userID)
		if err != nil {
			log.Printf("[VPC] [FAILURE] Failed to get default VPC for user %s: %v", userID, err)
			return "", "", "", "", fmt.Errorf("failed to get default VPC: %w", err)
		}
		log.Printf("[VPC] [OK] Got default VPC %s (bridge: %s) for instance %s", vpcID, bridgeName, instanceID)
	}

	privateIP, gateway, bridgeName, err = s.publisher.PrepareInstanceNetwork(userID, instanceID, vpcID)
	if err != nil {
		log.Printf("[NETWORK] [ERROR] Failed to prepare network for instance %s in VPC %s: %v", instanceID, vpcID, err)
		return "", "", "", "", fmt.Errorf("failed to prepare instance network: %w", err)
	}

	log.Printf("[NETWORK] [OK] Network ready for instance %s — IP: %s, gateway: %s, bridge: %s",
		instanceID, privateIP, gateway, bridgeName)

	return vpcID, privateIP, gateway, bridgeName, nil
}




// persistAndLaunch generates the SSH key pair, writes the instance record to
// the DB with the pre-allocated IP, then fires off async VM creation.
// If the DB write fails the pre-allocated IP is released back to the pool.
func (s *InstanceService) persistAndLaunch(
	req *domain.CreateInstanceRequest,
	userID, instanceID, vmName, newDiskPath, baseImagePath,
	bridgeName, privateIP, gateway, vpcID, instanceToken string,
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
	}

	if err := s.repo.Create(instance); err != nil {
		if s.publisher != nil && privateIP != "" {
			if releaseErr := s.publisher.ReleaseInstanceNetwork(userID, instanceID, vpcID); releaseErr != nil {
				log.Printf("[NETWORK] [WARN] Failed to release IP for failed instance %s: %v", instanceID, releaseErr)
			}
		}
		return nil, fmt.Errorf("failed to save instance: %w", err)
	}

	go s.createVMAsync(instance, req, baseImagePath, newDiskPath, bridgeName, privateIP, gateway, instanceToken)

	// Return the full key so the caller can hand it back to the user once
	instance.PrivateSshKey = keyPair.PrivateKeyPEM
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

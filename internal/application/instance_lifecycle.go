package application

import (
	"context"
	domain "ec2-api/internal/domain/instance"
	"strings"

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

// allocateInstanceNetwork now calls vpcService directly instead of NATS.
func (s *InstanceService) allocateInstanceNetwork(ctx context.Context, userID, instanceID string) (
	vpcID, privateIP, gateway, bridgeName string, err error,
) {
	log.Printf("[NETWORK] Preparing network for instance %s", instanceID)
 
	// Get or create the default VPC for this user
	defaultVPC, err := s.vpcService.GetOrCreateDefaultVPC(ctx, userID)
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to get default VPC for user %s: %w", userID, err)
	}
	log.Printf("[VPC] [OK] Got default VPC %s (bridge: %s) for instance %s", defaultVPC.ID, defaultVPC.BridgeName, instanceID)
 
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
		return nil, fmt.Errorf("failed to save instance,:::::::::::: %w", err)
	}

	go s.createVMAsync(instance, req, baseImagePath, newDiskPath, bridgeName, privateIP, gateway, instanceToken,keyPair)

	// Return the full key so the caller can hand it back to the user once
	instance.PrivateSshKey = keyPair.PrivateKeyPEM
	return instance, nil
}



func (s *InstanceService) createVMAsync(
	instance *domain.Instance,
	req *domain.CreateInstanceRequest,
	baseImagePath, newDiskPath, bridgeName, privateIP, gateway, instanceToken string,
	keyPair *SSHKeyPair,
) {
	profile := req.Profile
	manifest := req.Manifest
	arn := req.ARN

	absBase, _ := filepath.Abs(baseImagePath)
	absNew, _ := filepath.Abs(newDiskPath)

	var remoteHostIP string
	if instance.HostID != "" {
		if host, err := s.hostService.GetHost(instance.HostID); err == nil && host != nil {
			remoteHostIP = host.IP
			log.Printf("[VM] Targeted for remote host %s (IP: %s)", instance.HostID, remoteHostIP)
		}
	}

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
		if err := s.injectPayloadIntoDisk(instance, profile, manifest, arn,absNew); err != nil {
			log.Printf("[VM] [%s] Injection failed for9 %s: %v", profile, instance.VMName, err)
			s.publishProgress(instance.ID, StageFailed, fmt.Sprintf("Failed to inject payload: %v", err))
			s.markTerminatedAndReleaseNetwork(instance, absNew)
			return
		}
	}


	// ── Step 1.6: Bake Standalone Image ──────────────────────────────────────
	if remoteHostIP != "" {
		s.publishProgress(instance.ID, StageCloningDisk, "Baking standalone image for remote host transfer...")
		bakedDisk := absNew + ".baked"
		convertCmd := exec.Command("qemu-img", "convert", "-O", "qcow2", absNew, bakedDisk)
		if output, err := convertCmd.CombinedOutput(); err != nil {
			log.Printf("[VM] Failed to bake disk for %s: %v\nOutput: %s", instance.VMName, err, string(output))
			s.publishProgress(instance.ID, StageFailed, fmt.Sprintf("Failed to bake disk: %v", err))
			s.markTerminatedAndReleaseNetwork(instance, absNew)
			return
		}
		// Replace original with baked standalone disk
		exec.Command("mv", bakedDisk, absNew).Run()
		log.Printf("[VM] Standalone baked disk ready for transfer: %s", absNew)
	}

	// ── Step 2: Validate libvirt is available ─────────────────────────────────
	if s.libvirtClient == nil {
		log.Printf("[VM] libvirt client not initialized for %s", instance.VMName)
		s.markTerminatedAndReleaseNetwork(instance, newDiskPath)
		return
	}
// // ── Step 1.75: Generate SSH key pair for instance ─────────────────────────
// keyPair, err := GenerateSSHKeyPair()



// if err != nil {
//     log.Printf("[VM] Failed to generate SSH key pair for %s: %v", instance.VMName, err)
//     s.publishProgress(instance.ID, StageFailed, "Failed to generate SSH keys")
//     s.markTerminatedAndReleaseNetwork(instance, newDiskPath)
//     return
// }


 

	log.Printf("[VM] Creating VM %s with static IP %s on bridge %s", instance.VMName, privateIP, bridgeName)

	// ── Step 3: Create and start VM with pre-allocated static IP ─────────────
	// privateIP and gateway are passed into CreateAndStartVM which writes them
	// into the cloud-init network-config. The VM boots already knowing its IP.
	// waitForVMIP is no longer needed since the IP is statically configured.
	s.publishProgress(instance.ID, StageStartingVM, "Defining and starting the virtual machine...")
	keys := []string{}
for _, key := range []string{keyPair.PublicKeyAuth, s.systemPubKey} {
    key = strings.TrimSpace(key)
    key = strings.ReplaceAll(key, "\n", "")
    key = strings.ReplaceAll(key, "\r", "")
    if key != "" {
        keys = append(keys, key)
    }
}

// ✅ Build a newline-joined string — matches what createCloudInitISO expects
combinedKeys := strings.Join(keys, "\n")

log.Printf("[VM] Creating VM %s with static IP %s on bridge %s", instance.VMName, privateIP, bridgeName)





vmID, err := s.libvirtClient.CreateAndStartVM(
    remoteHostIP, instance.VMName, absNew, req.CPU, req.RAM, combinedKeys, bridgeName, // ✅ correct var
    privateIP, gateway, instanceToken, profile, manifest.Parameters,
)
	if err != nil {
		log.Printf("[VM] Failed to create VM %s: %v", instance.VMName, err)
		if remoteHostIP != "" {
			exec.Command("ssh", "-o", "StrictHostKeyChecking=no", fmt.Sprintf("root@%s", remoteHostIP), "rm", "-f", newDiskPath).Run()
		} else {
			exec.Command("rm", "-f", newDiskPath).Run()
		}
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
		s.libvirtClient.DeleteVM(remoteHostIP, instance.VMName)
		if remoteHostIP != "" {
			exec.Command("ssh", "-o", "StrictHostKeyChecking=no", fmt.Sprintf("root@%s", remoteHostIP), "rm", "-f", newDiskPath).Run()
		} else {
			exec.Command("rm", "-f", newDiskPath).Run()
		}
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

	// s.publishProgress(instance.ID, StageProvisioned, "Instance is now running and reachable.")

s.publishProgress(instance.ID, StageProvisioned, "Instance is now running and reachable.", domain.ProvisionedRequesFinishedResponse{
    VMID:     "vm-hardcoded-001",   // hardcoded for now
    AgentURL: "http://10.0.0.1:90", // hardcoded for now
})


	log.Printf("✓ VM %s created successfully (ID: %s, IP: %s, bridge: %s)",
		instance.VMName, instance.ID, privateIP, bridgeName)
}

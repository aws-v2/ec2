package application

import (
	"context"
	domain "ec2-api/internal/domain/instance"
	"os"
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
	bridgeName, privateIP, gateway, vpcID, instanceToken, hostID string,
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
		HostID:        hostID, // set before goroutine starts to avoid race condition
	}






	if err := s.repo.Create(instance); err != nil {
		if s.vpcService != nil && privateIP != "" {
			if releaseErr := s.vpcService.ReleaseInstanceNetwork(context.Background(), instanceID); releaseErr != nil {
				log.Printf("[NETWORK] [WARN] Failed to release IP for failed instance %s: %v", instanceID, releaseErr)
			}
		}
		return nil, fmt.Errorf("failed to save instance,:::::::::::: %w", err)
	}

	go s.createVMAsync(instance, req, baseImagePath, newDiskPath, bridgeName, privateIP, gateway, instanceToken, keyPair)

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

	// remoteHostIP, remoteHostUser, remoteHostKey will be resolved AFTER phase 1
	// (scheduling has already written instance.HostID to the DB by then).

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


	// Note: Template check (whether to use overlay or bake) is done AFTER phase 1
	// in the consolidated host-resolve block just before CreateAndStartVM.
	// This ensures instance.HostID is set by the scheduler.

	log.Printf("-------------------end of phase one-----")

	// ── Step 2: Validate libvirt is available ─────────────────────────────────
	if s.libvirtClient == nil {
		log.Printf("[VM] libvirt client not initialized for %s", instance.VMName)
		s.markTerminatedAndReleaseNetwork(instance, newDiskPath)
		return
	}
// // ── Step 1.75: Generate SSH key pair for instance ─────────────────────────
// keyPair, err := GenerateSSHKeyPair()
 


 

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





	// ── Resolve host details (done here so HostID is set after scheduling) ────
var remoteHostIP, remoteHostUser, remoteHostKey string
var backingTemplate string

log.Printf("[VM] Starting remote host resolution for instance=%s hostID=%s image=%s",
	instance.ID,
	instance.HostID,
	instance.Image,
)

if instance.HostID != "" {
	log.Printf("[VM] Looking up host record for hostID=%s", instance.HostID)

	h, err := s.hostService.GetHost(instance.HostID)
	if err != nil {
		log.Printf("[VM] [ERROR] Failed to lookup host %s: %v",
			instance.HostID,
			err,
		)
	} else if h == nil {
		log.Printf("[VM] [WARN] Host lookup returned nil for hostID=%s",
			instance.HostID,
		)
	} else {

		log.Printf("[VM] Host record found:")
		log.Printf("[VM]   HostID=%s", h.ID)
		log.Printf("[VM]   IP=%s", h.IP)
		log.Printf("[VM]   SSHUser=%s", h.SSHUser)
		log.Printf("[VM]   AvailableTemplates=%v", h.AvailableTemplates)

		remoteHostIP = h.IP
		remoteHostUser = h.SSHUser
		remoteHostKey = h.SSHPrivateKey

		if remoteHostIP == "" {
			log.Printf("[VM] [WARN] Host IP is empty")
		}

		if remoteHostUser == "" {
			log.Printf("[VM] [WARN] SSH user is empty")
		}

		if remoteHostKey != "" {
			log.Printf("[VM] SSH private key found for host=%s", instance.HostID)
		} else {
			log.Printf("[VM] [WARN] No SSH private key found for host=%s", instance.HostID)
		}

		log.Printf("[VM] Checking template availability on host=%s for image=%s",
			instance.HostID,
			instance.Image,
		)

		foundTemplate := false

		for _, t := range h.AvailableTemplates {
			log.Printf("[VM] Comparing host template=%s against requested image=%s",
				t,
				instance.Image,
			)

			if t == instance.Image {
				backingTemplate = instance.Image
				foundTemplate = true

				log.Printf("[VM] Template match found")
				log.Printf("[VM] Using backing template=%s", backingTemplate)
				log.Printf("[VM] Overlay-only transfer enabled")
				break
			}
		}

		if !foundTemplate {
			log.Printf("[VM] No matching template found on host=%s",
				instance.HostID,
			)
			log.Printf("[VM] Full image transfer will be required")
		}
	}
} else {
	log.Printf("[VM] [WARN] Instance has no HostID assigned")
}

log.Printf("[VM] Remote host resolution completed")
log.Printf("[VM] Final remote config:")
log.Printf("[VM]   remoteHostIP=%s", remoteHostIP)
log.Printf("[VM]   remoteHostUser=%s", remoteHostUser)

if backingTemplate != "" {
	log.Printf("[VM]   backingTemplate=%s", backingTemplate)
} else {
	log.Printf("[VM]   backingTemplate=<none>")
}



























	if remoteHostUser == "" {
		remoteHostUser = "x6617274696" // Global fallback
	}

	// If Agent has no template, bake a full standalone image before transfer.
	if remoteHostIP != "" && backingTemplate == "" {
		log.Printf("[VM] Host has no template for %s — baking standalone image for transfer", instance.Image)
		s.publishProgress(instance.ID, StageCloningDisk, "Baking standalone image for remote host transfer...")
		bakedDisk := absNew + ".baked"

		var lastConvertErr error
		var bakeOut []byte
		for i := 0; i < 99; i++ {
			convertCmd := exec.Command("qemu-img", "convert", "-U", "-O", "qcow2", absNew, bakedDisk)
			bakeOut, lastConvertErr = convertCmd.CombinedOutput()
			if lastConvertErr == nil {
				break
			}
			log.Printf("[VM] Bake attempt %d failed for %s, retrying... Error: %v", i+1, instance.VMName, lastConvertErr)
			time.Sleep(1 * time.Second)
		}
		if lastConvertErr != nil {
			log.Printf("[VM] Failed to bake disk for %s: %v\nOutput: %s", instance.VMName, lastConvertErr, string(bakeOut))
			s.publishProgress(instance.ID, StageFailed, fmt.Sprintf("Failed to bake disk: %v", lastConvertErr))
			s.markTerminatedAndReleaseNetwork(instance, absNew)
			return
		}
		exec.Command("mv", bakedDisk, absNew).Run()
		log.Printf("[VM] Standalone baked disk ready for transfer: %s", absNew)
	}

	vmID, err := s.libvirtClient.CreateAndStartVM(
		remoteHostIP, remoteHostUser, remoteHostKey, instance.VMName, absNew, req.CPU, req.RAM, combinedKeys, bridgeName,
		privateIP, gateway, instanceToken, profile, manifest.Parameters, backingTemplate,
	)
	if err != nil {
		log.Printf("[VM] Failed to create VM %s: %v", instance.VMName, err)
		if remoteHostIP != "" {
			args := []string{"-o", "StrictHostKeyChecking=no", "-o", "BatchMode=yes", "-o", "PreferredAuthentications=publickey"}
			if remoteHostKey != "" {
				f, err := os.CreateTemp("", "id_rsa_cleanup_*")
				if err == nil {
					f.WriteString(remoteHostKey)
					f.Close()
					os.Chmod(f.Name(), 0600)
					args = append(args, "-i", f.Name())
					defer os.Remove(f.Name())
				}
			}
			exec.Command("ssh", append(args, fmt.Sprintf("%s@%s", remoteHostUser, remoteHostIP), "rm", "-f", absNew)...).Run()
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
		s.libvirtClient.DeleteVM(remoteHostIP, remoteHostUser, remoteHostKey, instance.VMName)
		if remoteHostIP != "" {
			// Effort-only cleanup
			exec.Command("ssh", "-o", "StrictHostKeyChecking=no", fmt.Sprintf("%s@%s", remoteHostUser, remoteHostIP), "rm", "-f", newDiskPath).Run()
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

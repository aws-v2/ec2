package application

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	domain "ec2-api/internal/domain/instance"


	"golang.org/x/crypto/ssh"
)

// GenerateSSHKeyPair creates an ed25519 key pair.
// Call this once at instance creation time and persist both keys to the DB.
func GenerateSSHKeyPair() (*SSHKeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}

	// Marshal private key to OpenSSH PEM format
	privPEM, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	privPEMStr := string(pem.EncodeToMemory(privPEM))

	// Marshal public key to authorized_keys format ("ssh-ed25519 AAAA...")
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("derive public key: %w", err)
	}
	pubAuthStr := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))

	return &SSHKeyPair{
		PrivateKeyPEM: privPEMStr,
		PublicKeyAuth: pubAuthStr,
	}, nil
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

func (s *InstanceService) publishProgress(instanceID, stage, message string) {
	if s.publisher != nil {
		_ = s.publisher.PublishProvisioningProgress(instanceID, stage, message)
	}
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
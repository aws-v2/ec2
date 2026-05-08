package application

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"log"
	"os/exec"
	"strings"

	domain "ec2-api/internal/domain/instance"


	"golang.org/x/crypto/ssh"
)

// GenerateSSHKeyPair creates an ed25519 key pair.
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

	// ✅ Declare BEFORE any use
	privPEMStr := string(pem.EncodeToMemory(privPEM))

	// Marshal public key to authorized_keys format
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

// min helper if you're on Go < 1.21
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
 
func (s *InstanceService) publishProgress(instanceID, stage, message string, payload ...any) {
    if s.publisher != nil {
        _ = s.publisher.PublishProvisioningProgress(instanceID, stage, message, payload...)
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
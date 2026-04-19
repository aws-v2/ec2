package application

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"ec2-api/internal/domain"

	"golang.org/x/crypto/ssh"
)

type SSHKeyService struct {
	repo             domain.SSHKeyRepository
	systemKeyService *SystemKeyService
	keysDir          string
}

func NewSSHKeyService(repo domain.SSHKeyRepository, systemKeyService *SystemKeyService, keysDir string) *SSHKeyService {
	return &SSHKeyService{
		repo:             repo,
		systemKeyService: systemKeyService,
		keysDir:          keysDir,
	}
}

func (s *SSHKeyService) CreateSSHKey(req *domain.CreateSSHKeyRequest) (*domain.SSHKey, error) {
	var publicKey string
	var privateKey string

	if req.PublicKey == "" {
		// Generate RSA key
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("failed to generate private key: %w", err)
		}

		// Private Key PEM
		privBlock := &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(priv),
		}
		privateKey = string(pem.EncodeToMemory(privBlock))

		// Public Key
		pub, err := ssh.NewPublicKey(&priv.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("failed to generate public key: %w", err)
		}
		publicKey = string(ssh.MarshalAuthorizedKey(pub))
	} else {
		publicKey = req.PublicKey
	}

	key := &domain.SSHKey{
		Name:       req.Name,
		PublicKey:  publicKey,
		PrivateKey: privateKey,
	}

	if err := s.repo.Create(key); err != nil {
		return nil, err
	}

	// Save private key to disk if generated
	if privateKey != "" {
		keyPath := filepath.Join(s.keysDir, req.Name+".pem")
		if err := os.WriteFile(keyPath, []byte(privateKey), 0600); err != nil {
			return nil, fmt.Errorf("failed to save private key to disk: %w", err)
		}
	}

	return key, nil
}

func (s *SSHKeyService) GetSSHKey(id int) (*domain.SSHKey, error) {
	return s.repo.FindByID(id)
}

func (s *SSHKeyService) GetSSHKeyByName(name string) (*domain.SSHKey, error) {
	return s.repo.FindByName(name)
}

func (s *SSHKeyService) ListSSHKeys() ([]*domain.SSHKey, error) {
	return s.repo.FindAll()
}

func (s *SSHKeyService) DeleteSSHKey(id int) error {
	key, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	if err := s.repo.Delete(id); err != nil {
		return err
	}

	// Remove file if exists
	keyPath := filepath.Join(s.keysDir, key.Name+".pem")
	_ = os.Remove(keyPath)

	return nil
}

func (s *SSHKeyService) GetPrivateKeyByName(name string) ([]byte, error) {
	// 1. Check if it's a file on disk
	keyPath := filepath.Join(s.keysDir, name+".pem")
	if _, err := os.Stat(keyPath); err == nil {
		return os.ReadFile(keyPath)
	}

	// 2. Fallback to system key if appropriate
	return s.systemKeyService.GetPrivateKey()
}

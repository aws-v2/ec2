package application

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

type SystemKeyService struct {
	privateKeyPath string
	publicKeyPath  string
}

func NewSystemKeyService(baseDir string) *SystemKeyService {
	return &SystemKeyService{
		privateKeyPath: filepath.Join(baseDir, "id_rsa"),
		publicKeyPath:  filepath.Join(baseDir, "id_rsa.pub"),
	}
}

func (s *SystemKeyService) EnsureKeys() error {
	if _, err := os.Stat(s.privateKeyPath); err == nil {
		return nil
	}

	// Generate RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	// Save Private Key
	privFile, err := os.Create(s.privateKeyPath)
	if err != nil {
		return err
	}
	defer privFile.Close()

	privBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	if err := pem.Encode(privFile, privBlock); err != nil {
		return err
	}

	// Generate Public Key
	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return err
	}

	pubBytes := ssh.MarshalAuthorizedKey(pub)
	if err := os.WriteFile(s.publicKeyPath, pubBytes, 0644); err != nil {
		return err
	}

	return nil
}

func (s *SystemKeyService) GetPrivateKey() ([]byte, error) {
	return os.ReadFile(s.privateKeyPath)
}

func (s *SystemKeyService) GetPublicKeyString() (string, error) {
	bytes, err := os.ReadFile(s.publicKeyPath)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

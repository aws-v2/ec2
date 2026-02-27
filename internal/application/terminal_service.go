package application

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/Qarani-m/ec2-api/internal/domain"
	"golang.org/x/crypto/ssh"
)

type TerminalService struct {
	instanceService  *InstanceService
	systemKeyService *SystemKeyService
}

func NewTerminalService(is *InstanceService, sks *SystemKeyService) *TerminalService {
	return &TerminalService{
		instanceService:  is,
		systemKeyService: sks,
	}
}

type SSHSession struct {
	Client  *ssh.Client
	Session *ssh.Session
	In      io.WriteCloser
	Out     io.Reader
}

func (s *TerminalService) CreateSSHSession(instanceID, userID string) (*SSHSession, error) {
	instance, err := s.instanceService.GetInstance(instanceID, userID)
	if err != nil {
		return nil, err
	}

	if instance.Status != domain.StatusRunning || instance.IP == "" {
		return nil, fmt.Errorf("instance is not running or has no IP")
	}

	privKeyBytes, err := s.systemKeyService.GetPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get system private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(privKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: "ubuntu", // Default user
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // For demo/local use
		Timeout:         10 * time.Second,            // Fail fast if unreachable
	}

	// Connect to instance
	addr := net.JoinHostPort(instance.IP, "22")
	fmt.Printf("[SSH] Dialing %s...\n", addr)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		fmt.Printf("[SSH] Dial failed for %s: %v\n", addr, err)
		return nil, fmt.Errorf("failed to dial SSH: %w", err)
	}
	fmt.Printf("[SSH] Connected to %s\n", addr)

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to create SSH session: %w", err)
	}

	// Request PTY
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("failed to request pty: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	if err := session.Shell(); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("failed to start shell: %w", err)
	}

	return &SSHSession{
		Client:  client,
		Session: session,
		In:      stdin,
		Out:     io.MultiReader(stdout, stderr),
	}, nil
}

func (s *SSHSession) Close() {
	if s.Session != nil {
		s.Session.Close()
	}
	if s.Client != nil {
		s.Client.Close()
	}
}

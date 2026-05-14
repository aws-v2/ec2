package application

import (
	domain "ec2-api/internal/domain/host"
	"log"
	"strings"
	"sync"
	"time"
)

type HostService struct {
	repo domain.Repository
	mu   sync.Mutex
	lastSelectionOffset int
	 ec2PublicKey string // loaded from env/config at startup
}

type HeartbeatResponse struct {
    EC2PublicKey string `json:"ec2_public_key"`
}

func NewHostService(repo domain.Repository, ec2PublicKey string) *HostService {
	return &HostService{
		repo: repo,
		ec2PublicKey: ec2PublicKey,
	}
}

func (s *HostService) GetHost(id string) (*domain.Host, error) {
	return s.repo.GetByID(id)
}

func (s *HostService) HandleHeartbeat(req domain.HeartbeatRequest) (*HeartbeatResponse, error) {
    sshUser := req.SSHUser
    parts := strings.Split(req.Hostname, ":")
    if len(parts) >= 7 && parts[6] != "root" && parts[6] != "" {
        log.Printf("[host-service] Extracted SSH user '%s' from heartbeat hostname", parts[6])
        sshUser = parts[6]
    }

    host := &domain.Host{
        ID:                 req.HostID,
        Hostname:           req.Hostname,
        IP:                 req.IP,
        SSHUser:            sshUser,
        CPUTotal:           req.CPUTotal,
        CPUUsed:            req.CPUUsed,
        RAMTotal:           req.RAMTotal,
        RAMFree:            req.RAMFree,
        DiskTotal:          req.DiskTotal,
        DiskFree:           req.DiskFree,
        AvailableTemplates: req.AvailableTemplates,
        SSHPrivateKey:      req.SSHPrivateKey,
        Status:             "active",
        LastHeartbeat:      time.Now(),
    }

    if err := s.repo.Update(host); err != nil {
        return nil, err
    }

    return &HeartbeatResponse{
        EC2PublicKey: s.ec2PublicKey,
    }, nil
}

func (s *HostService) SelectBestHost() (*domain.Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Get Top 10 best hosts
	hosts, err := s.repo.GetBestHosts(10)
	if err != nil {
		return nil, err
	}

	if len(hosts) == 0 {
		return nil, nil // No active hosts
	}

	// 2. Round Robin among the results
	s.lastSelectionOffset = (s.lastSelectionOffset + 1) % len(hosts)
	return hosts[s.lastSelectionOffset], nil
}

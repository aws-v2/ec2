package application

import (
	domain "ec2-api/internal/domain/host"
	"log"
	"sync"
	"time"
)

type HostService struct {
	repo domain.Repository
	mu   sync.Mutex
	lastSelectionOffset int
}

func NewHostService(repo domain.Repository) *HostService {
	return &HostService{
		repo: repo,
	}
}

func (s *HostService) GetHost(id string) (*domain.Host, error) {
	return s.repo.GetByID(id)
}

func (s *HostService) HandleHeartbeat(req domain.HeartbeatRequest) error {
	log.Printf("[host-service] handling heartbeat for host %s, cpu: %d, ram: %d, storage: %d", req.HostID, req.CPUTotal, req.RAMTotal, req.DiskTotal)
	host := &domain.Host{
		ID:            req.HostID,
		Hostname:      req.Hostname,
		IP:            req.IP,
		CPUTotal:      req.CPUTotal,
		CPUUsed:       req.CPUUsed,
		RAMTotal:      req.RAMTotal,
		RAMFree:       req.RAMFree,
		DiskTotal:     req.DiskTotal,
		DiskFree:      req.DiskFree,
		Status:        "active",
		LastHeartbeat: time.Now(),
	}
	return s.repo.Update(host)
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

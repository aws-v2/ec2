package application

import (
	"context"
	domain "ec2-api/internal/domain/host"
	messaging "ec2-api/internal/infra/messaging"
	"encoding/json"
	"fmt"
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
	publisher      *messaging.NATSPublisher


}

type HeartbeatResponse struct {
    EC2PublicKey string `json:"ec2_public_key"`
}

func NewHostService(repo domain.Repository, ec2PublicKey string,
	publisher  *messaging.NATSPublisher,
    
    ) *HostService {
	return &HostService{
		repo: repo,
		ec2PublicKey: ec2PublicKey,
				publisher:     publisher,
	}
}

func (s *HostService) GetHost(id string) (*domain.Host, error) {
	return s.repo.GetByID(id)
}


func (s *HostService) AddTemplate(
	ctx context.Context,
	userID, correlationID, templateARN, extension string,
) (string, error) {

	if extension == "" {
		extension = ".zip"
	}

	req := struct {
		TemplateID    string `json:"template_id"`
		UserID        string `json:"user_id"`
		ARN           string `json:"arn"`
		Extension     string `json:"extension"`
	}{
		TemplateID: templateARN,
		UserID:     userID,
		ARN:        templateARN,
		Extension:  extension,
	}

	url, err := s.publisher.PublishCreateUploadURL(req)
	if err != nil {
		return "", fmt.Errorf("failed to create upload url: %w", err)
	}

	return url, nil
}


func (s *HostService) GetHostTemplates(ctx context.Context, userID, correlationID string) (string, error) {

	templateARN := "default"

	payload := map[string]string{
		"user_id":        userID,
		"correlation_id": correlationID,
		"template_arn":   templateARN,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// ONLY responsibility: call publisher
	url, err := s.publisher.PublishDownloadTemplateURL(data)
	if err != nil {
		return "", fmt.Errorf("failed to get presigned url: %w", err)
	}

	return url, nil
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

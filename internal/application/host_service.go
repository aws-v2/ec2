package application

import (
	"bytes"
	"context"
	domain "ec2-api/internal/domain/host"
	agentDomain "ec2-api/internal/domain/instance"
	messaging "ec2-api/internal/infra/messaging"
	"ec2-api/internal/interfaces"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type HostService struct {
	repo                domain.Repository
	mu                  sync.Mutex
	lastSelectionOffset int
	ec2PublicKey        string // loaded from env/config at startup
	publisher           *messaging.NATSPublisher
	agentUrlParts       string
	rolloutRepo         interfaces.RolloutRepository
	metricsRepo         domain.MetricsRepository
}

type HeartbeatResponse struct {
	EC2PublicKey string `json:"ec2_public_key"`
}

func NewHostService(repo domain.Repository, ec2PublicKey string,
	publisher *messaging.NATSPublisher, agentUrlParts string, rolloutRepo interfaces.RolloutRepository, metricsRepo domain.MetricsRepository) *HostService {
	return &HostService{
		repo:          repo,
		ec2PublicKey:  ec2PublicKey,
		agentUrlParts: agentUrlParts,
		publisher:     publisher,
		rolloutRepo:   rolloutRepo,
		metricsRepo:   metricsRepo,
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
		TemplateID string `json:"template_id"`
		UserID     string `json:"user_id"`
		ARN        string `json:"arn"`
		Extension  string `json:"extension"`
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

func (s *HostService) RolloutAgentUpdate(ctx context.Context, req domain.RolloutUpdateRequest) (*domain.RolloutSummary, error) {
	hosts, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch hosts: %w", err)
	}

	presignedURL, err := s.publisher.FetchAgentPresignedURL(req.UserID, req.Version, req.FileName, req.SHA256)
	if err != nil {
		return nil, fmt.Errorf("fetch agent presigned url: %w", err)
	}

	if req.SHA256 == "" {
		req.SHA256 = "sha256-test"
	}

	payload := domain.AgentUpdatePayload{
		Version: req.Version,
		URL:     fmt.Sprintf("%s%s", s.agentUrlParts, presignedURL),
		SHA256:  req.SHA256,
	}

	if err := s.validateS3URL(payload.URL); err != nil {
		return nil, fmt.Errorf("s3 pre-flight failed, rollout aborted: %w for this url: %s", err, payload.URL)
	}

	// --- Save rollout metadata ---
	rolloutID := uuid.New()
	if err := s.rolloutRepo.CreateRollout(ctx, agentDomain.AgentRollout{
		ID:          rolloutID,
		Version:     req.Version,
		FileName:    req.FileName,
		SHA256:      req.SHA256,
		URL:         payload.URL,
		InitiatedBy: req.UserID,
		Total:       len(hosts),
	}); err != nil {
		return nil, fmt.Errorf("save rollout metadata: %w", err)
	}

	// --- Insert pending status row for every host upfront ---
	for _, host := range hosts {
		_ = s.rolloutRepo.InsertUpdateStatus(ctx, agentDomain.AgentUpdateStatus{
			ID:        uuid.New(),
			RolloutID: rolloutID,
			HostID:    host.ID,
			HostIP:    host.IP,
		})
	}

	rolloutCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		counter atomic.Int64
		s3Err   atomic.Pointer[error]
	)

	results := make(chan domain.UpdateResult, len(hosts))

	for _, host := range hosts {
		go func(host domain.Host) {
			n := counter.Add(1)
			if n%10 == 0 {
				if err := s.validateS3URL(payload.URL); err != nil {
					s3Err.CompareAndSwap(nil, &err)
					cancel()
				}
			}

			if rolloutCtx.Err() != nil {
				results <- domain.UpdateResult{HostID: host.ID, Addr: host.IP, OK: false, Error: "rollout aborted: s3 became unreachable mid-flight"}
				return
			}

			err := s.pushUpdateToAgent(host.IP, payload)
			res := domain.UpdateResult{HostID: host.ID, Addr: host.IP, OK: err == nil}

			if err != nil {
				res.Error = err.Error()
				hostUUID, err1 := uuid.Parse(host.ID)
				if err1 != nil {
					return
				}
				// push failed — mark immediately, no need to wait for agent ping
				_ = s.rolloutRepo.UpdateStatusByHostAndVersion(ctx, hostUUID, req.Version, "failed")
			}
			// if OK — stays "pending" until agent pings back

			results <- res
		}(host)
	}

	summary := &domain.RolloutSummary{Total: len(hosts), Version: req.Version}
	for range hosts {
		r := <-results
		if r.OK {
			summary.OK++
		} else {
			summary.Failed++
		}
		summary.Results = append(summary.Results, r)
	}

	if p := s3Err.Load(); p != nil {
		summary.S3Error = (*p).Error()
	}

	// Persist final counts
	_ = s.rolloutRepo.UpdateRolloutSummary(ctx, rolloutID, summary.OK, summary.Failed, summary.S3Error)

	return summary, nil
}

func (s *HostService) validateS3URL(url string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build s3 request: %w", err)
	}

	req.Header.Set("Range", "bytes=0-0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("s3 unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode >= 500 {
		return fmt.Errorf("s3 unavailable: %d", resp.StatusCode)
	}

	return nil
}

func (s *HostService) pushUpdateToAgent(hostIP string, payload domain.AgentUpdatePayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("http://%s:9030/update", hostIP)
	log.Printf("[HostService] pushing update to agent at %s", url)

	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post to agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("agent returned %d", resp.StatusCode)
	}

	return nil
}

func (s *HostService) SendPowerAction(ctx context.Context, hostIP string, vmID string, action string) error {
	req := struct {
		VMID   string `json:"vm_id"`
		Action string `json:"action"`
	}{
		VMID:   vmID,
		Action: action,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal power request: %w", err)
	}

	url := fmt.Sprintf("http://%s:9030/vm/power", hostIP)
	log.Printf("[HostService] sending power action %s to VM %s at %s", action, vmID, url)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create power request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send power action: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("agent returned %d", resp.StatusCode)
	}

	return nil
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

	url, err := s.publisher.PublishDownloadTemplateURL(data)
	if err != nil {
		return "", fmt.Errorf("failed to get presigned url: %w", err)
	}

	return url, nil
}

func (s *HostService) HandleHeartbeat(req domain.HeartbeatRequest) (*HeartbeatResponse, error) {
	log.Printf("[host-service] Heartbeat from host=\x1b[32m%s\x1b[0m ip=%s cpu_used=%.2f%% ram_free=%.2f vms_count=%d", 
		req.HostID, req.IP, req.CPUUsed, req.RAMFree, len(req.VMs))

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
		log.Printf("[host-service] Failed to database update host %s: %v", req.HostID, err)
		return nil, err
	}

	for _, vm := range req.VMs {
		metric := domain.DomainStats{
			VMID:      vm.VMID,
			HostID:    req.HostID,
			Name:      vm.Name,
			CPUUsed:   vm.CPUUsed,
			Memory:    vm.Memory,
			DiskRead:  vm.DiskRead,
			DiskWrite: vm.DiskWrite,
			NetRx:     vm.NetRx,
			NetTx:     vm.NetTx,
			State:     vm.State,
			CreatedAt: time.Now(),
		}
		log.Printf("  -> [vm-met8rics] name=%-15s state=%-10s cpu=%.2f ram=%d disk(r/w)=%d/%d net(rx/tx)=%d/%d",
			metric.Name, metric.State, metric.CPUUsed, metric.Memory, metric.DiskRead, metric.DiskWrite, metric.NetRx, metric.NetTx)
		go func(hID string, m domain.DomainStats) {
			if err := s.metricsRepo.Insert(context.Background(), hID, m); err != nil {
				log.Printf("[host-service] Failed to insert metrics for VM %s: %v", m.VMID, err)
			}
		}(host.ID, metric)
	}

	return &HeartbeatResponse{
		EC2PublicKey: s.ec2PublicKey,
	}, nil
}

func (s *HostService) SelectBestHost() (*domain.Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	hosts, err := s.repo.GetBestHosts(10)
	if err != nil {
		return nil, err
	}

	if len(hosts) == 0 {
		return nil, nil
	}

	s.lastSelectionOffset = (s.lastSelectionOffset + 1) % len(hosts)
	return hosts[s.lastSelectionOffset], nil
}

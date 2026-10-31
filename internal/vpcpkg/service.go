package vpcpkg

import (
	"bytes"
	"context"
	"database/sql"
	"ec2-api/internal/interfaces"
	ec2metrics "ec2-api/internal/metrics"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Service is the single entry point the InstanceService will call.
// It owns all VPC business logic and coordinates the repository + provisioner.
type Service struct {
	repo     Repository
	hostRepo interfaces.HostRepository

	provisioner *Provisioner
}

func NewVpcService(repo Repository, provisioner *Provisioner, hostRepo interfaces.HostRepository) *Service {
	return &Service{repo: repo, provisioner: provisioner, hostRepo: hostRepo}
}

// GetOrCreateDefaultVPC returns the tenant's default VPC, creating it if needed.
// This replaces the old NATS call to the external network service.
func (s *Service) GetOrCreateDefaultVPC(ctx context.Context, tenantID string) (*VPC, error) {
	vpc, err := s.repo.GetDefaultVPC(ctx, tenantID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to query default VPC: %w", err)
	}
	if vpc != nil {
		return vpc, nil
	}

	log.Printf("[VPC] No default VPC for tenant %s — creating one", tenantID)
	return s.createVPC(ctx, tenantID, tenantID+"-default", true)
}

// CreateVPC provisions a new non-default VPC for a tenant.
// Called when the user explicitly creates a VPC from the API.
func (s *Service) CreateVPC(ctx context.Context, tenantID, name string) (*VPC, error) {
	return s.createVPC(ctx, tenantID, name, false)
}

func (s *Service) FindOrphanBridges(ctx context.Context, candidates []string) ([]string, error) {
	return s.repo.FindOrphanBridges(ctx, candidates)
}

// AllocateInstanceNetwork resolves the VPC and assigns a free IP to the instance.
// Returns all data needed to build cloud-init network config.
func (s *Service) AllocateInstanceNetwork(ctx context.Context, tenantID, instanceID, vpcID string) (*InstanceNetwork, error) {
	vpc, err := s.repo.GetVPCByID(ctx, vpcID)
	if err != nil {
		return nil, fmt.Errorf("VPC %s not found: %w", vpcID, err)
	}

	// Get already-used IPs so we don't collide
	usedIPs, err := s.repo.GetAllocatedIPs(ctx, vpcID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch allocated IPs for VPC %s: %w", vpcID, err)
	}

	ip, err := NextAvailableIP(vpc.CIDRBlock, usedIPs)
	if err != nil {
		er := fmt.Errorf("IP exhaustion in VPC %s: %w", vpcID, err)
		log.Printf("Error: %v", er)
		return nil, er
	}

	alloc := &NetworkAllocation{
		ID:          uuid.NewString(),
		VPCID:       vpcID,
		InstanceID:  instanceID,
		IPAddress:   ip,
		AllocatedAt: time.Now(),
	}
	if err := s.repo.AllocateIP(ctx, alloc); err != nil {
		return nil, fmt.Errorf("failed to reserve IP %s for instance %s: %w", ip, instanceID, err)
	}

	log.Printf("[VPC] Allocated IP %s in VPC %s for instance %s", ip, vpcID, instanceID)

	return &InstanceNetwork{
		PrivateIP:  ip,
		Gateway:    vpc.GatewayIP,
		BridgeName: vpc.BridgeName,
		VPCID:      vpcID,
	}, nil
}

// ReleaseInstanceNetwork frees the IP assigned to a terminated instance.
func (s *Service) ReleaseInstanceNetwork(ctx context.Context, instanceID string) error {
	if err := s.repo.ReleaseIP(ctx, instanceID); err != nil {
		return fmt.Errorf("failed to release IP for instance %s: %w", instanceID, err)
	}
	log.Printf("[VPC] Released IP for instance %s", instanceID)
	return nil
}

// UpdateVPCHost locks the VPC ownership to a new host ID
func (s *Service) UpdateVPCHost(ctx context.Context, vpcID, hostID string) error {
	return s.repo.UpdateVPCHost(ctx, vpcID, hostID)
}

// DeleteVPC tears down the VPC. Blocked if any instances are still running in it.
func (s *Service) DeleteVPC(ctx context.Context, vpcID string) error {
	count, err := s.repo.CountActiveInstances(ctx, vpcID)
	if err != nil {
		return fmt.Errorf("failed to check active instances: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("VPC %s still has %d active instance(s) — terminate them first", vpcID, count)
	}

	if err := s.repo.DeleteVPC(ctx, vpcID); err != nil {
		return fmt.Errorf("failed to delete VPC record %s: %w", vpcID, err)
	}

	return nil
}

// ListVPCs returns all VPCs owned by a tenant.
func (s *Service) ListVPCs(ctx context.Context, tenantID string) ([]*VPC, error) {
	return s.repo.ListVPCsByTenant(ctx, tenantID)
}

// ─── internal ────────────────────────────────────────────────────────────────

const maxCIDRAttempts = 64

func (s *Service) createVPC(ctx context.Context, tenantID, name string, isDefault bool) (*VPC, error) {
	// CIDR allocation must be serialized — get all existing CIDRs first
	usedCIDRs, err := s.repo.GetUsedCIDRs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch used CIDRs: %w", err)
	}

	// cidr, err := AllocateNextCIDR(usedCIDRs)
	// if err != nil {
	// 	return nil, err
	// }
	var cidr string
	for attempt := 0; attempt < maxCIDRAttempts; attempt++ {
		candidate, err := AllocateNextCIDR(usedCIDRs)
		if err != nil {
			return nil, err
		}

		free, reason, err := s.cidrFreeOnAllHosts(ctx, candidate)
		if err != nil {
			return nil, fmt.Errorf("cannot verify CIDR %s, refusing to allocate: %w", candidate, err)
		}
		if free {
			cidr = candidate
			break
		}

		log.Printf("[VPC] CIDR %s is in use on a host (%s), trying next", candidate, reason)
		usedCIDRs = append(usedCIDRs, candidate)
	}
	if cidr == "" {
		return nil, fmt.Errorf("no free CIDR found after %d attempts", maxCIDRAttempts)
	}

	gateway, err := GatewayFromCIDR(cidr)
	if err != nil {
		return nil, err
	}

	vpcID := uuid.NewString()
	bridge := BridgeNameFromVPCID(vpcID)

	vpc := &VPC{
		ID:         vpcID,
		Name:       name,
		CIDRBlock:  cidr,
		BridgeName: bridge,
		GatewayIP:  gateway,
		TenantID:   tenantID,
		Status:     VPCStatusPending,
		IsDefault:  isDefault,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Persist first with status=pending so the CIDR is reserved
	if err := s.repo.CreateVPC(ctx, vpc); err != nil {
		return nil, fmt.Errorf("failed to persist VPC record: %w", err)
	}

	// Mark active immediately since host agent handles physical bridging at reconcile
	if err := s.repo.UpdateVPCStatus(ctx, vpc.ID, VPCStatusActive); err != nil {
		return nil, fmt.Errorf("failed to mark VPC active: %w", err)
	}
	vpc.Status = VPCStatusActive

	log.Printf("[VPC] Created VPC %s (CIDR: %s, bridge: %s, default: %v)", vpc.ID, cidr, bridge, isDefault)
	return vpc, nil
}

func (s *Service) cidrFreeOnAllHosts(ctx context.Context, cidr string) (bool, string, error) {
	hosts, err := s.hostRepo.ListAll(ctx) // adapt to your host repo; must return distinct IPs
	if err != nil {
		return false, "", fmt.Errorf("list hosts: %w", err)
	}
	for _, host := range hosts {
		free, reason, err := s.checkCIDROnHost(ctx, host.IP, cidr)
		if err != nil {
			return false, "", fmt.Errorf("host %s: %w", host.IP, err)
		}
		if !free {
			return false, fmt.Sprintf("host %s: %s", host.IP, reason), nil
		}
	}
	return true, "", nil
}

func (s *Service) checkCIDROnHost(ctx context.Context, hostIP, cidr string) (free bool, reason string, retErr error) {
	defer func() {
		if retErr != nil {
			ec2metrics.RecordAgentCallError("cidr_check", hostIP)
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	body, err := json.Marshal(map[string]string{"cidr": cidr})
	if err != nil {
		return false, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("http://%s:9030/cidr-check", hostIP), bytes.NewReader(body))
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("agent unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return false, "", fmt.Errorf("agent returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var out struct {
		Free      bool     `json:"free"`
		Conflicts []string `json:"conflicts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, "", fmt.Errorf("decode agent response: %w", err)
	}
	return out.Free, strings.Join(out.Conflicts, "; "), nil
}

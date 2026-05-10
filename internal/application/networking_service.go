package application

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"time"

	domain "ec2-api/internal/domain/instance"
	interfaces "ec2-api/internal/interfaces"
	messaging "ec2-api/internal/infra/messaging"
	libvirt  "ec2-api/internal/infra/libvirt")

type NetworkingService struct {
	ipRepo       interfaces.IPRepository
	sgRepo       interfaces.SecurityGroupRepository
	instanceRepo interfaces.InstanceRepository
	libvirt      *libvirt.LibvirtClient
	publisher    messaging.Publisher
}

func NewNetworkingService(ipRepo interfaces.IPRepository, sgRepo interfaces.SecurityGroupRepository, instanceRepo interfaces.InstanceRepository, libvirt *libvirt.LibvirtClient, publisher messaging.Publisher) *NetworkingService {
	return &NetworkingService{
		ipRepo:       ipRepo,
		sgRepo:       sgRepo,
		instanceRepo: instanceRepo,
		libvirt:      libvirt,
		publisher:    publisher,
	}
}

// IP Service implementation
func (s *NetworkingService) AllocateIP(req *domain.AllocateIPRequest) (*domain.IPAllocation, error) {
	// Simple random public IP for prototype
	publicIP := fmt.Sprintf("1.1.1.%d", rand.Intn(254)+1)

	var privateIP string
	if req.InstanceID != nil && *req.InstanceID != "" {
		instance, err := s.instanceRepo.FindByID(*req.InstanceID)
		if err == nil && instance.IP != "" {
			privateIP = instance.IP
		}
	}

	if privateIP == "" {
		// Generate random private IP for simulation
		privateIP = fmt.Sprintf("10.0.0.%d", rand.Intn(254)+1)
	}

	ip := &domain.IPAllocation{
		InstanceID: req.InstanceID,
		PublicIP:   publicIP,
		PrivateIP:  &privateIP,
		Status:     "allocated",
	}

	if err := s.ipRepo.Create(ip); err != nil {
		return nil, fmt.Errorf("failed to save IP allocation: %w", err)
	}

	return ip, nil
}

func (s *NetworkingService) ReleaseIP(id int) error {
	return s.ipRepo.Delete(id)
}

func (s *NetworkingService) ListIPs() ([]*domain.IPAllocation, error) {
	return s.ipRepo.FindAll()
}

// Security Group Service implementation
func (s *NetworkingService) CreateSecurityGroup(req *domain.CreateSecurityGroupRequest) (*domain.SecurityGroup, error) {
	sg := &domain.SecurityGroup{
		Name:        req.Name,
		Description: req.Description,
		Rules:       []domain.SecurityGroupRule{},
	}

	if err := s.sgRepo.Create(sg); err != nil {
		return nil, fmt.Errorf("failed to save security group: %w", err)
	}

	return sg, nil
}

func (s *NetworkingService) GetSecurityGroup(id int) (*domain.SecurityGroup, error) {
	return s.sgRepo.FindByID(id)
}

func (s *NetworkingService) GetSecurityGroupByName(name string) (*domain.SecurityGroup, error) {
	return s.sgRepo.FindByName(name)
}

func (s *NetworkingService) ListSecurityGroups() ([]*domain.SecurityGroup, error) {
	return s.sgRepo.FindAll()
}

func (s *NetworkingService) UpdateSecurityGroup(id int, req *domain.CreateSecurityGroupRequest) (*domain.SecurityGroup, error) {
	sg, err := s.sgRepo.FindByID(id)
	if err != nil {
		return nil, err
	}

	sg.Name = req.Name
	sg.Description = req.Description

	if err := s.sgRepo.Update(sg); err != nil {
		return nil, fmt.Errorf("failed to update security group: %w", err)
	}

	return sg, nil
}

func (s *NetworkingService) DeleteSecurityGroup(id int) error {
	return s.sgRepo.Delete(id)
}

// Rule operations
func (s *NetworkingService) AddRule(sgID int, req *domain.AddRuleRequest) (*domain.SecurityGroupRule, error) {
	rule := &domain.SecurityGroupRule{
		SecurityGroupID: sgID,
		Type:            req.Type,
		Protocol:        req.Protocol,
		FromPort:        req.FromPort,
		ToPort:          req.ToPort,
		SourceDestCIDR:  req.SourceDestCIDR,
		Description:     req.Description,
	}

	if err := s.sgRepo.AddRule(rule); err != nil {
		return nil, fmt.Errorf("failed to add security group rule: %w", err)
	}

	return rule, nil
}

func (s *NetworkingService) RemoveRule(sgID int, ruleID int) error {
	// Optional: verify rule belongs to sgID
	return s.sgRepo.RemoveRule(ruleID)
}

// Assignment operations
func (s *NetworkingService) AssignToInstance(instanceID string, sgID int) error {
	return s.sgRepo.AddInstanceToGroup(instanceID, sgID)
}

func (s *NetworkingService) RemoveFromInstance(instanceID string, sgID int) error {
	return s.sgRepo.RemoveInstanceFromGroup(instanceID, sgID)
}

func (s *NetworkingService) ListForInstance(instanceID string) ([]*domain.SecurityGroup, error) {
	return s.sgRepo.GetGroupsForInstance(instanceID)
}

func (s *NetworkingService) SeedDefaultSecurityGroup() error {
	sg, err := s.sgRepo.FindByName("default")
	if err != nil && err != domain.ErrSecurityGroupNotFound {
		return err
	}

	if err == domain.ErrSecurityGroupNotFound {
		sg = &domain.SecurityGroup{
			Name:        "default",
			Description: "Default security group allowing SSH access",
		}
		if err := s.sgRepo.Create(sg); err != nil {
			return fmt.Errorf("failed to create default security group: %w", err)
		}
	}

	// Add port 22 rule if no rules exist
	rules, err := s.sgRepo.GetRules(sg.ID)
	if err != nil {
		return err
	}

	if len(rules) == 0 {
		port22 := 22
		_, err := s.AddRule(sg.ID, &domain.AddRuleRequest{
			Type:           "inbound",
			Protocol:       "tcp",
			FromPort:       &port22,
			ToPort:         &port22,
			SourceDestCIDR: "0.0.0.0/0",
			Description:    "Allow SSH",
		})
		if err != nil {
			return fmt.Errorf("failed to add default SSH rule: %w", err)
		}
	}

	return nil
}

func (s *NetworkingService) ListVPCs(ctx context.Context, tenantID string) ([]domain.VPC, error) {
	if s.publisher == nil {
		return nil, fmt.Errorf("network service publisher is not configured")
	}
	return s.publisher.ListVPCs(tenantID)
}

// CreateVPC dispatches a request to create a VPC
func (s *NetworkingService) CreateVPC(ctx context.Context, tenantID, vpcName string) error {
	if s.publisher == nil {
		return fmt.Errorf("network service publisher is not configured")
	}
	return s.publisher.CreateVPC(tenantID, vpcName, tenantID)
}

func (s *NetworkingService) AssignVPC(tenantID, instanceID, vpcID string) error {
	if s.publisher == nil {
		return fmt.Errorf("NATS publisher not initialized")
	}

	// 1. Get instance details
	instance, err := s.instanceRepo.FindByID(instanceID)
	if err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}

	if instance.UserID != tenantID {
		return fmt.Errorf("unauthorized: instance does not belong to tenant")
	}

	oldVPCID := instance.VPCID

	// 2. Stop the Instance
	fmt.Printf("[NetworkingService] Stopping instance %s for VPC migration\n", instanceID)
	if err := s.libvirt.StopVM("", instance.VMName); err != nil {
		fmt.Printf("[NetworkingService] Warning: StopVM failed for %s: %v\n", instanceID, err)
	}

	// Wait a bit for stop to complete (simple approach)
	time.Sleep(2 * time.Second)

	// 3. Release Old IP
	if oldVPCID != "" {
		fmt.Printf("[NetworkingService] Releasing old network for %s in VPC %s\n", instanceID, oldVPCID)
		if err := s.publisher.ReleaseInstanceNetwork(tenantID, instanceID, oldVPCID); err != nil {
			fmt.Printf("[NetworkingService] Warning: ReleaseInstanceNetwork failed: %v\n", err)
		}
	}

	// 4. Prepare New IP
	fmt.Printf("[NetworkingService] Preparing new network for %s in VPC %s\n", instanceID, vpcID)
	privateIP, gateway, bridgeName, err := s.publisher.PrepareInstanceNetwork(tenantID, instanceID, vpcID)
	if err != nil {
		return fmt.Errorf("failed to prepare new network: %w", err)
	}

	// 5. Update Database
	instance.VPCID = vpcID
	instance.IP = privateIP
	if err := s.instanceRepo.Update(instance); err != nil {
		return fmt.Errorf("failed to update instance record: %w", err)
	}

	// 6. Reconfigure & Start
	// We need to undefine and redefine with new bridge and new cloud-init
	fmt.Printf("[NetworkingService] Reconfiguring and restarting instance %s\n", instanceID)
	if err := s.libvirt.DeleteVM("", instance.VMName); err != nil {
		fmt.Printf("[NetworkingService] Warning: DeleteVM (undefine) failed: %v\n", err)
	}

	// Determine disk path (logic similar to InstanceService)
	// For production we should store disk path in DB, but here we follow the convention
	diskPath := filepath.Join(s.libvirt.GetImagesDir(), fmt.Sprintf("%s.qcow2", instance.VMName))

	// Get tags/ssh keys if needed, for prototype we assume we have them or can get them
	// Instance struct has SSHKey
	_, err = s.libvirt.CreateAndStartVM(
		"", // remoteHostIP not available in NetworkingService
		instance.VMName,
		diskPath,
		instance.CPU,
		instance.RAM,
		instance.PublicSSHKey,
		bridgeName,
		privateIP,
		gateway,
		"",  // no metrics token needed for VPC migration
		"",  // no profile for VPC migration
		nil, // no parameters for VPC migration
	)
	if err != nil {
		return fmt.Errorf("failed to restart VM in new VPC: %w", err)
	}

	fmt.Printf("[NetworkingService] Successfully moved instance %s to VPC %s with IP %s\n", instanceID, vpcID, privateIP)
	return nil
}

package application

import (
	"fmt"
	"math/rand"

	"github.com/Qarani-m/ec2-api/internal/domain"
	"github.com/Qarani-m/ec2-api/internal/libvirt"
)

type NetworkingService struct {
	ipRepo       domain.IPRepository
	sgRepo       domain.SecurityGroupRepository
	instanceRepo domain.InstanceRepository
	libvirt      *libvirt.LibvirtClient
}

func NewNetworkingService(ipRepo domain.IPRepository, sgRepo domain.SecurityGroupRepository, instanceRepo domain.InstanceRepository, libvirt *libvirt.LibvirtClient) *NetworkingService {
	return &NetworkingService{
		ipRepo:       ipRepo,
		sgRepo:       sgRepo,
		instanceRepo: instanceRepo,
		libvirt:      libvirt,
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

package domain

import (
	"errors"
	"time"
)

var (
	ErrIPNotFound            = errors.New("IP allocation not found")
	ErrSecurityGroupNotFound = errors.New("security group not found")
)

// VPC represents a Virtual Private Cloud
type VPC struct {
	ID          string    `json:"id" db:"id"`
	TenantID    string    `json:"tenant_id" db:"tenant_id"`
	Name        string    `json:"name" db:"name"`
	CIDR        string    `json:"cidr" db:"cidr"`
	Status      string    `json:"status" db:"status"`
	BridgeName  string    `json:"bridge_name" db:"bridge_name"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	IsDefault   bool      `json:"is_default" db:"is_default"`
}

// ResourceNetworkAssignment represents an assignment of a resource to a VPC
type ResourceNetworkAssignment struct {
	ResourceARN string `json:"resource_arn"`
	VPCID       string `json:"vpc_id"`
	PrivateIP   string `json:"private_ip"`
	Status      string `json:"status"`
}

// IP Allocation
type IPAllocation struct {
	ID           int       `json:"id" db:"id"`
	InstanceID   *string   `json:"instance_id" db:"instance_id"`
	PublicIP     string    `json:"public_ip" db:"public_ip"`
	PrivateIP    *string   `json:"private_ip" db:"private_ip"`
	PortMappings string    `json:"port_mappings" db:"port_mappings"`
	Status       string    `json:"status" db:"status"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UserID       string    `json:"user_id" db:"user_id"`
}

type AllocateIPRequest struct {
	InstanceID *string `json:"instance_id"`
}

// Security Group
type SecurityGroupRule struct {
	ID              int       `json:"id" db:"id"`
	SecurityGroupID int       `json:"security_group_id" db:"security_group_id"`
	Type            string    `json:"type" db:"type"` // 'inbound' or 'outbound'
	Protocol        string    `json:"protocol" db:"protocol"`
	FromPort        *int      `json:"from_port" db:"from_port"`
	ToPort          *int      `json:"to_port" db:"to_port"`
	SourceDestCIDR  string    `json:"source_dest_cidr" db:"source_dest_cidr"`
	Description     string    `json:"description" db:"description"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
}

type SecurityGroup struct {
	ID          int                 `json:"id" db:"id"`
	Name        string              `json:"name" db:"name"`
	Description string              `json:"description" db:"description"`
	Rules       []SecurityGroupRule `json:"rules"` // Now structured
	CreatedAt   time.Time           `json:"created_at" db:"created_at"`
	UserID      string              `json:"user_id" db:"user_id"`
}

type CreateSecurityGroupRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

type AddRuleRequest struct {
	Type           string  `json:"type" binding:"required,oneof=inbound outbound"`
	Protocol       string  `json:"protocol" binding:"required"`
	FromPort       *int    `json:"from_port"`
	ToPort         *int    `json:"to_port"`
	SourceDestCIDR string  `json:"source_dest_cidr" binding:"required"`
	Description    string  `json:"description"`
}

// Repositories
type IPRepository interface {
	Create(ip *IPAllocation) error
	FindByID(id int) (*IPAllocation, error)
	FindAll() ([]*IPAllocation, error)
	Update(ip *IPAllocation) error
	Delete(id int) error
}

type SecurityGroupRepository interface {
	Create(sg *SecurityGroup) error
	FindByID(id int) (*SecurityGroup, error)
	FindByName(name string) (*SecurityGroup, error)
	FindAll() ([]*SecurityGroup, error)
	Update(sg *SecurityGroup) error
	Delete(id int) error

	// Rules
	AddRule(rule *SecurityGroupRule) error
	RemoveRule(ruleID int) error
	GetRules(sgID int) ([]SecurityGroupRule, error)

	// Instance Associations
	AddInstanceToGroup(instanceID string, sgID int) error
	RemoveInstanceFromGroup(instanceID string, sgID int) error
	GetGroupsForInstance(instanceID string) ([]*SecurityGroup, error)
}

// Services
type IPService interface {
	AllocateIP(req *AllocateIPRequest) (*IPAllocation, error)
	ReleaseIP(id int) error
	ListIPs() ([]*IPAllocation, error)
}

type SecurityGroupService interface {
	CreateSecurityGroup(req *CreateSecurityGroupRequest) (*SecurityGroup, error)
	GetSecurityGroup(id int) (*SecurityGroup, error)
	GetSecurityGroupByName(name string) (*SecurityGroup, error)
	ListSecurityGroups() ([]*SecurityGroup, error)
	UpdateSecurityGroup(id int, req *CreateSecurityGroupRequest) (*SecurityGroup, error)
	DeleteSecurityGroup(id int) error

	// Rule operations
	AddRule(sgID int, req *AddRuleRequest) (*SecurityGroupRule, error)
	RemoveRule(sgID int, ruleID int) error

	// Assignment operations
	AssignToInstance(instanceID string, sgID int) error
	RemoveFromInstance(instanceID string, sgID int) error
	ListForInstance(instanceID string) ([]*SecurityGroup, error)

	// Seeding
	SeedDefaultSecurityGroup() error
}

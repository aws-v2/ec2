package interfaces

import (
	"context"
	"ec2-api/internal/domain"
)

type InstanceRepository interface {
	Create(instance *domain.Instance) error
	FindByID(id string) (*domain.Instance, error)
	FindAll(userID string) ([]*domain.Instance, error)
	UpdateStatus(id string, status domain.InstanceStatus) error
	Delete(id string) error
	Update(instance *domain.Instance) error
	GetTags(instanceID string) ([]*domain.InstanceTag, error)
	AddOrUpdateTag(instanceID string, tag *domain.InstanceTag) error
	DeleteTag(instanceID string, key string) error
	CreateScalingPolicy(ctx context.Context, userID string, req *domain.ScalingPolicyRequest) error
	GetScalingPolicies(ctx context.Context, userID string) ([]domain.ScalingPolicy, error)
	UpdateScalingPolicy(ctx context.Context, userID, policyID string, req *domain.UpdateScalingPolicyRequest) error
	DeleteScalingPolicy(ctx context.Context, userID, policyID string) error
}

type VolumeRepository interface {
	Create(volume *domain.Volume) error
	FindByID(id int) (*domain.Volume, error)
	FindAll() ([]*domain.Volume, error)
	FindByInstanceID(instanceID string) ([]*domain.Volume, error)
	Update(volume *domain.Volume) error
	UpdateSize(id int, newSize int) error
	Delete(volume *domain.Volume) error

	GetTags(volumeID int) ([]*domain.VolumeTag, error)
	AddOrUpdateTag(volumeID int, tag *domain.VolumeTag) error
	DeleteTag(volumeID int, key string) error
}



type TemplateRepository interface {
	Create(template *domain.Template) error
	FindByID(id int) (*domain.Template, error)
	FindByName(name string) (*domain.Template, error)
	FindAll() ([]*domain.Template, error)
	UpdateStatus(id int, status domain.TemplateStatus) error
	Delete(id int) error
}

type TemplateService interface {
	CreateTemplate(req *domain.CreateTemplateRequest) (*domain.Template, error)
	GetTemplate(id int) (*domain.Template, error)
	ListTemplates() ([]*domain.Template, error)
	DeleteTemplate(id int) error
}

type SnapshotRepository interface {
	Create(snapshot *domain.Snapshot) error
	FindByID(id int) (*domain.Snapshot, error)
	FindAll() ([]*domain.Snapshot, error)
	FindByInstanceID(instanceID string) ([]*domain.Snapshot, error)
	FindByVolumeID(volumeID int) ([]*domain.VolumeSnapshot, error)
	UpdateStatus(id int, status domain.SnapshotStatus) error
	Delete(id int) error
}

type SnapshotService interface {
	CreateSnapshot(instanceID string, req *domain.CreateSnapshotRequest) (*domain.Snapshot, error)
	CreateVolumeSnapshot(volumeID int, req *domain.CreateSnapshotRequest) (*domain.VolumeSnapshot, error)
	GetSnapshot(id int) (*domain.Snapshot, error)
	ListSnapshots() ([]*domain.Snapshot, error)
	ListSnapshotsByInstance(instanceID string) ([]*domain.Snapshot, error)
	ListSnapshotsByVolume(volumeID int) ([]*domain.VolumeSnapshot, error)
	DeleteSnapshot(id int) error
}


// Repositories
type IPRepository interface {
	Create(ip *domain.IPAllocation) error
	FindByID(id int) (*domain.IPAllocation, error)
	FindAll() ([]*domain.IPAllocation, error)
	Update(ip *domain.IPAllocation) error
	Delete(id int) error
}

type SecurityGroupRepository interface {
	Create(sg *domain.SecurityGroup) error
	FindByID(id int) (*domain.SecurityGroup, error)
	FindByName(name string) (*domain.SecurityGroup, error)
	FindAll() ([]*domain.SecurityGroup, error)
	Update(sg *domain.SecurityGroup) error
	Delete(id int) error

	// Rules
	AddRule(rule *domain.SecurityGroupRule) error
	RemoveRule(ruleID int) error
	GetRules(sgID int) ([]domain.SecurityGroupRule, error)

	// Instance Associations
	AddInstanceToGroup(instanceID string, sgID int) error
	RemoveInstanceFromGroup(instanceID string, sgID int) error
	GetGroupsForInstance(instanceID string) ([]*domain.SecurityGroup, error)
}

// Services
type IPService interface {
	AllocateIP(req *domain.AllocateIPRequest) (*domain.IPAllocation, error)
	ReleaseIP(id int) error
	ListIPs() ([]*domain.IPAllocation, error)
}

type SecurityGroupService interface {
	CreateSecurityGroup(req *domain.CreateSecurityGroupRequest) (*domain.SecurityGroup, error)
	GetSecurityGroup(id int) (*domain.SecurityGroup, error)
	GetSecurityGroupByName(name string) (*domain.SecurityGroup, error)
	ListSecurityGroups() ([]*domain.SecurityGroup, error)
	UpdateSecurityGroup(id int, req *domain.CreateSecurityGroupRequest) (*domain.SecurityGroup, error)
	DeleteSecurityGroup(id int) error

	// Rule operations
	AddRule(sgID int, req *domain.AddRuleRequest) (*domain.SecurityGroupRule, error)
	RemoveRule(sgID int, ruleID int) error

	// Assignment operations
	AssignToInstance(instanceID string, sgID int) error
	RemoveFromInstance(instanceID string, sgID int) error
	ListForInstance(instanceID string) ([]*domain.SecurityGroup, error)

	// Seeding
	SeedDefaultSecurityGroup() error
}




// FleetRepository defines methods for fetching fleet-wide data
type FleetRepository interface {
	GetOverview(userID string) (*domain.FleetOverview, error)
	GetEvents(userID string, limit int) ([]*domain.FleetEvent, error)
	LogEvent(event *domain.FleetEvent) error
}

// LambdaRepository placeholder for future implementation
type LambdaRepository interface {
	CountActive(userID string) (int, error)
}



type SSHKeyRepository interface {
	Create(key *domain.SSHKey) error
	FindByID(id int) (*domain.SSHKey, error)
	FindByName(name string) (*domain.SSHKey, error)
	FindAll() ([]*domain.SSHKey, error)
	Delete(id int) error
}

type SSHKeyService interface {
	CreateSSHKey(req *domain.CreateSSHKeyRequest) (*domain.SSHKey, error)
	GetSSHKey(id int) (*domain.SSHKey, error)
	GetSSHKeyByName(name string) (*domain.SSHKey, error)
	ListSSHKeys() ([]*domain.SSHKey, error)
	DeleteSSHKey(id int) error
}

package interfaces

import (
	"context"
	hostdomain "ec2-api/internal/domain/host"
	domain "ec2-api/internal/domain/instance"

	"github.com/google/uuid"
)

type HostRepository interface {
	Update(host *hostdomain.Host) error
	GetBestHosts(limit int) ([]*hostdomain.Host, error)
	GetByID(id string) (*hostdomain.Host, error)
	ListAll(ctx context.Context) ([]hostdomain.Host, error)

}


type RolloutRepository interface {
    CreateRollout(ctx context.Context, r domain.AgentRollout) error
    UpdateRolloutSummary(ctx context.Context, rolloutID uuid.UUID, ok, failed int, s3Err string) error

    InsertUpdateStatus(ctx context.Context, s domain.AgentUpdateStatus) error
    UpdateStatusByHostAndVersion(ctx context.Context, hostID uuid.UUID, version, status string) error // called on agent ping
    GetStatusByRollout(ctx context.Context, rolloutID uuid.UUID) ([]domain.AgentUpdateStatus, error)
}

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
	GetInstanceInfo(instanceID, userID string) (*domain.InstanceInfo, error)
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
type SSHKeyRepository interface {
	Create(key *domain.SSHKey) error
	FindByID(id int) (*domain.SSHKey, error)
	FindByName(name string) (*domain.SSHKey, error)
	FindAll() ([]*domain.SSHKey, error)
	Delete(id int) error
}


// LambdaRepository placeholder for future implementation
type LambdaRepository interface {
	CountActive(userID string) (int, error)
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

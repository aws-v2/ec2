package interfaces

import domain "ec2-api/internal/domain/instance"
import dto "ec2-api/internal/domain/dto"


type TemplateService interface {
	CreateTemplate(req *domain.CreateTemplateRequest) (*domain.Template, error)
	GetTemplate(id int) (*domain.Template, error)
	ListTemplates() ([]*domain.Template, error)
	DeleteTemplate(id int) error
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
type FleetRepository interface {
	GetOverview(userID string) (*dto.FleetOverview, error)
	GetEvents(userID string, limit int) ([]*dto.FleetEvent, error)
	LogEvent(event *dto.FleetEvent) error
}

type SSHKeyService interface {
	CreateSSHKey(req *domain.CreateSSHKeyRequest) (*domain.SSHKey, error)
	GetSSHKey(id int) (*domain.SSHKey, error)
	GetSSHKeyByName(name string) (*domain.SSHKey, error)
	ListSSHKeys() ([]*domain.SSHKey, error)
	DeleteSSHKey(id int) error
}

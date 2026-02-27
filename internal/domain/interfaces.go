package domain



type InstanceRepository interface {
	Create(instance *Instance) error
	FindByID(id string) (*Instance, error)
	FindAll(userID string) ([]*Instance, error)
	UpdateStatus(id string, status InstanceStatus) error
	Delete(id string) error
	Update(instance *Instance) error
	GetTags(instanceID string) ([]*InstanceTag, error)
	AddOrUpdateTag(instanceID string, tag *InstanceTag) error
	DeleteTag(instanceID string, key string) error
}





type VolumeRepository interface {
    Create(volume *Volume) error
    FindByID(id int) (*Volume, error)
    FindAll() ([]*Volume, error)
    FindByInstanceID(instanceID string) ([]*Volume, error)
    Update(volume *Volume) error
    UpdateSize(id int, newSize int) error
    Delete(volume *Volume) error

    GetTags(volumeID int) ([]*VolumeTag, error)
    AddOrUpdateTag(volumeID int, tag *VolumeTag) error
    DeleteTag(volumeID int, key string) error
}
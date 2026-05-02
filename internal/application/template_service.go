package application

import (
	"fmt"

	"ec2-api/internal/domain"
	"ec2-api/internal/interfaces"
	"ec2-api/internal/libvirt"
)

type TemplateService struct {
	templateRepo interfaces.TemplateRepository
	instanceRepo interfaces.InstanceRepository
	libvirt      *libvirt.LibvirtClient
}

func NewTemplateService(templateRepo interfaces.TemplateRepository, instanceRepo interfaces.InstanceRepository, libvirt *libvirt.LibvirtClient) *TemplateService {
	return &TemplateService{
		templateRepo: templateRepo,
		instanceRepo: instanceRepo,
		libvirt:      libvirt,
	}
}

func (s *TemplateService) CreateTemplate(req *domain.CreateTemplateRequest) (*domain.Template, error) {
	var instanceID *string
	var image string
	var cpu, ram int

	if req.InstanceID != "" {
		// Verify instance exists
		instance, err := s.instanceRepo.FindByID(req.InstanceID)
		if err != nil {
			return nil, fmt.Errorf("instance not found: %w", err)
		}
		instanceID = &req.InstanceID
		image = instance.Image
		cpu = instance.CPU
		ram = instance.RAM
	}

	template := &domain.Template{
		InstanceID:  instanceID,
		Name:        req.Name,
		Description: req.Description,
		Image:       image,
		CPU:         cpu,
		RAM:         ram,
		Status:      domain.TemplateStatusReady, // For prototype, we mark it as ready immediately
	}

	if err := s.templateRepo.Create(template); err != nil {
		return nil, fmt.Errorf("failed to save template: %w", err)
	}

	return template, nil
}

func (s *TemplateService) GetTemplate(id int) (*domain.Template, error) {
	return s.templateRepo.FindByID(id)
}

func (s *TemplateService) ListTemplates() ([]*domain.Template, error) {
	return s.templateRepo.FindAll()
}

func (s *TemplateService) DeleteTemplate(id int) error {
	return s.templateRepo.Delete(id)
}

package domain

import (
	"errors"
	"time"
)

var (
	ErrTemplateNotFound = errors.New("template not found")
)

type TemplateStatus string

const (
	TemplateStatusPending TemplateStatus = "pending"
	TemplateStatusReady   TemplateStatus = "ready"
	TemplateStatusFailed  TemplateStatus = "failed"
)

type Template struct {
	ID          int            `json:"id" db:"id"`
	InstanceID  *string        `json:"instance_id" db:"instance_id"`
	Name        string         `json:"name" db:"name"`
	Description string         `json:"description" db:"description"`
	Image       string         `json:"image" db:"image"`
	CPU         int            `json:"cpu" db:"cpu"`
	RAM         int            `json:"ram" db:"ram"`
	Status      TemplateStatus `json:"status" db:"status"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
}

type CreateTemplateRequest struct {
	InstanceID  string `json:"instance_id"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

type TemplateRepository interface {
	Create(template *Template) error
	FindByID(id int) (*Template, error)
	FindByName(name string) (*Template, error)
	FindAll() ([]*Template, error)
	UpdateStatus(id int, status TemplateStatus) error
	Delete(id int) error
}

type TemplateService interface {
	CreateTemplate(req *CreateTemplateRequest) (*Template, error)
	GetTemplate(id int) (*Template, error)
	ListTemplates() ([]*Template, error)
	DeleteTemplate(id int) error
}

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
	UserID      string         `json:"user_id" db:"user_id"`
}

type CreateTemplateRequest struct {
	InstanceID  string `json:"instance_id"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}


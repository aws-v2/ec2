package domain

import (
	"errors"
	"time"
)

var (
	ErrSSHKeyNotFound = errors.New("ssh key not found")
)

type SSHKey struct {
	ID         int       `json:"id" db:"id"`
	Name       string    `json:"name" db:"name"`
	PublicKey  string    `json:"public_key" db:"public_key"`
	PrivateKey string    `json:"private_key,omitempty" db:"-"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UserID     string    `json:"user_id" db:"user_id"`
}

type CreateSSHKeyRequest struct {
	Name      string `json:"name" binding:"required"`
	PublicKey string `json:"public_key"`
}


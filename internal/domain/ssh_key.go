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

type SSHKeyRepository interface {
	Create(key *SSHKey) error
	FindByID(id int) (*SSHKey, error)
	FindByName(name string) (*SSHKey, error)
	FindAll() ([]*SSHKey, error)
	Delete(id int) error
}

type SSHKeyService interface {
	CreateSSHKey(req *CreateSSHKeyRequest) (*SSHKey, error)
	GetSSHKey(id int) (*SSHKey, error)
	GetSSHKeyByName(name string) (*SSHKey, error)
	ListSSHKeys() ([]*SSHKey, error)
	DeleteSSHKey(id int) error
}

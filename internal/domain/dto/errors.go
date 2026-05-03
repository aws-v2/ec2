package domain

import "errors"

var (
	ErrInstanceNotFound = errors.New("instance not found")
	ErrProxmoxFailed    = errors.New("proxmox operation failed")
	ErrMissingSSHKey    = errors.New("missing ssh key")
	ErrMissingSSHUser   = errors.New("missing ssh user")
	ErrMissingVMIP      = errors.New("missing vm ip")
	ErrMissingVMSSHPort = errors.New("missing vm ssh port")
	ErrMissingAgentURL  = errors.New("missing agent url")
)
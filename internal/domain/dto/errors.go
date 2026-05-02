package domain

import "errors"

var (
	ErrInstanceNotFound = errors.New("instance not found")
	ErrProxmoxFailed    = errors.New("proxmox operation failed")
)
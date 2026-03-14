//go:build windows

package libvirt

import (
	"fmt"
)

type LibvirtClient struct {
	imagesDir string
}

func NewLibvirtClient(uri string, imagesDir string) (*LibvirtClient, error) {
	fmt.Printf("[Libvirt-Stub] Initialized in mock mode for Windows (ImagesDir: %s)\n", imagesDir)
	return &LibvirtClient{imagesDir: imagesDir}, nil
}

func (l *LibvirtClient) Close() error {
	return nil
}

func (l *LibvirtClient) GetImagesDir() string {
	return l.imagesDir
}

func (l *LibvirtClient) CreateAndStartVM(vmName, diskPath string, cpu, ram int, sshKey, bridgeName, privateIP, gateway, instanceToken string) (int, error) {
	return 0, fmt.Errorf("libvirt operations are not supported on Windows")
}

func (l *LibvirtClient) RestartVM(vmName string) error {
	return fmt.Errorf("libvirt operations are not supported on Windows")
}

func (l *LibvirtClient) StopVM(vmName string) error {
	return fmt.Errorf("libvirt operations are not supported on Windows")
}

func (l *LibvirtClient) StartVM(vmName string) error {
	return fmt.Errorf("libvirt operations are not supported on Windows")
}

func (l *LibvirtClient) DeleteVM(vmName string) error {
	return fmt.Errorf("libvirt operations are not supported on Windows")
}

func (l *LibvirtClient) GetPublicIP(vmID int) string {
	return "127.0.0.1"
}

func (l *LibvirtClient) EnsureDefaultNetwork() error {
	return nil
}

func (l *LibvirtClient) CreateSnapshot(vmName, snapshotName, description string) error {
	return fmt.Errorf("libvirt operations are not supported on Windows")
}

func (l *LibvirtClient) DeleteSnapshot(vmName, snapshotName string) error {
	return fmt.Errorf("libvirt operations are not supported on Windows")
}

func (l *LibvirtClient) RestoreSnapshot(vmName, snapshotName string) error {
	return fmt.Errorf("libvirt operations are not supported on Windows")
}

// Volume Operations

func (l *LibvirtClient) CreateVolume(volumePath string, sizeGB int) error {
	return fmt.Errorf("libvirt operations are not supported on Windows")
}

func (l *LibvirtClient) GetUsedDeviceNames(vmName string) (map[string]bool, error) {
	return make(map[string]bool), nil
}

func (l *LibvirtClient) AttachVolume(vmName, volumePath, devicePath, format string) error {
	return fmt.Errorf("libvirt operations are not supported on Windows")
}

func (l *LibvirtClient) DetachVolume(vmName, devicePath string) error {
	return fmt.Errorf("libvirt operations are not supported on Windows")
}

func (l *LibvirtClient) DeleteVolume(volumePath string) error {
	return fmt.Errorf("libvirt operations are not supported on Windows")
}

func (l *LibvirtClient) ResizeVolume(volumePath string, newSizeGB int) error {
	return fmt.Errorf("libvirt operations are not supported on Windows")
}

func (l *LibvirtClient) ListAttachedDevicePaths(vmName string) ([]string, error) {
	return []string{}, nil
}

func (l *LibvirtClient) ListAttachedDevices(vmName string) ([]string, error) {
	return []string{}, nil
}

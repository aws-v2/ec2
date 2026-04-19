//go:build !windows

package libvirt

import (
	"encoding/xml"
	"fmt"
	"os/exec"

	"ec2-api/internal/domain"

	libvirtgo "libvirt.org/go/libvirt"
)

func (l *LibvirtClient) CreateVolume(volumePath string, sizeGB int) error {
	// Create qcow2 volume using qemu-img
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", volumePath, fmt.Sprintf("%dG", sizeGB))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create volume: %w, output: %s", err, string(output))
	}
	return nil
}

func (l *LibvirtClient) GetUsedDeviceNames(vmName string) (map[string]bool, error) {
	domain, err := l.conn.LookupDomainByName(vmName)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup domain: %w", err)
	}
	defer domain.Free()

	xmlDesc, err := domain.GetXMLDesc(0)
	if err != nil {
		return nil, fmt.Errorf("failed to get domain XML: %w", err)
	}

	type Disk struct {
		Target struct {
			Dev string `xml:"dev,attr"`
		} `xml:"target"`
	}
	type Domain struct {
		Disks []Disk `xml:"devices>disk"`
	}

	var d Domain
	if err := xml.Unmarshal([]byte(xmlDesc), &d); err != nil {
		return nil, fmt.Errorf("failed to parse domain XML: %w", err)
	}

	used := make(map[string]bool)
	for _, disk := range d.Disks {
		used["/dev/"+disk.Target.Dev] = true
	}
	return used, nil
}

// devicePath=/dev/vde

func (l *LibvirtClient) AttachVolume(vmName, volumePath, devicePath, format string) error {
	fmt.Printf("=== AttachVolume Debug ===\n")
	fmt.Printf("vmName: %s\n", vmName)
	fmt.Printf("volumePath: %s\n", volumePath)
	fmt.Printf("devicePath: %s\n", devicePath)
	fmt.Printf("format: %s\n", format)
	l.ListAttachedDevicePaths("vm-i-b30b614f")
	domain, err := l.conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain: %w", err)
	}
	defer domain.Free()

	// Extract device name from path (e.g., vdb from /dev/vdb)
	deviceName := devicePath[5:] // Remove "/dev/"
	fmt.Printf("deviceName: %s\n", deviceName)

	// Build disk XML for attachment
	diskXML := fmt.Sprintf(`
    <disk type='file' device='disk'>
      <driver name='qemu' type='%s'/>
      <source file='%s'/>
      <target dev='%s' bus='virtio'/>
    </disk>`, format, volumePath, deviceName)

	fmt.Printf("diskXML:\n%s\n", diskXML)

	// Attach disk persistently (survives reboot)
	err = domain.AttachDeviceFlags(diskXML, libvirtgo.DOMAIN_DEVICE_MODIFY_CONFIG|libvirtgo.DOMAIN_DEVICE_MODIFY_LIVE)
	if err != nil {
		return fmt.Errorf("failed to attach volume: %w", err)
	}

	fmt.Printf("=== Attach Successful ===\n")
	return nil
}

func (l *LibvirtClient) DetachVolume(vmName, devicePath string) error {
	domain, err := l.conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain: %w", err)
	}
	defer domain.Free()

	// Extract device name from path
	deviceName := devicePath[5:]

	// Build disk XML for detachment (must match the attached disk)
	diskXML := fmt.Sprintf(`
    <disk type='file' device='disk'>
      <target dev='%s' bus='virtio'/>
    </disk>`, deviceName)

	// Detach disk persistently
	err = domain.DetachDeviceFlags(diskXML, libvirtgo.DOMAIN_DEVICE_MODIFY_CONFIG|libvirtgo.DOMAIN_DEVICE_MODIFY_LIVE)
	if err != nil {
		return fmt.Errorf("failed to detach volume: %w", err)
	}

	return nil
}

func (l *LibvirtClient) DeleteVolume(volumePath string) error {
	// Delete the volume file
	cmd := exec.Command("rm", "-f", volumePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete volume: %w, output: %s", err, string(output))
	}
	return nil
}

func (l *LibvirtClient) ResizeVolume(volumePath string, newSizeGB int) error {
	cmd := exec.Command("qemu-img", "resize", volumePath, fmt.Sprintf("%dG", newSizeGB))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to resize volume: %w, output: %s", err, string(output))
	}
	return nil
}

func (l *LibvirtClient) ListAttachedDevicePaths(vmName string) ([]string, error) {
	dom, err := l.conn.LookupDomainByName(vmName)
	if err != nil {
		return nil, fmt.Errorf("failed to find domain %s: %w", vmName, err)
	}
	defer dom.Free()

	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return nil, fmt.Errorf("failed to get domain XML: %w", err)
	}

	var domXML domain.DomainXML
	if err := xml.Unmarshal([]byte(xmlDesc), &domXML); err != nil {
		return nil, fmt.Errorf("failed to parse domain XML: %w", err)
	}

	var devices []string
	for _, disk := range domXML.Devices.Disks {
		if disk.Device == "disk" && disk.Target.Dev != "" {
			devices = append(devices, fmt.Sprintf("/dev/%s", disk.Target.Dev))
		}
	}

	return devices, nil
}

// Disk XML snippet (simplified)
type diskXML struct {
	Target struct {
		Dev string `xml:"dev,attr"`
		Bus string `xml:"bus,attr"`
	} `xml:"target"`
}

type domainXML struct {
	Disks []diskXML `xml:"devices>disk"`
}

func (s *LibvirtClient) ListAttachedDevices(vmName string) ([]string, error) {
	domain, err := s.conn.LookupDomainByName(vmName)
	if err != nil {
		return nil, fmt.Errorf("failed to find domain %s: %w", vmName, err)
	}
	defer domain.Free()

	xmlDesc, err := domain.GetXMLDesc(0)
	if err != nil {
		return nil, fmt.Errorf("failed to get XML for domain %s: %w", vmName, err)
	}

	var dom domainXML
	if err := xml.Unmarshal([]byte(xmlDesc), &dom); err != nil {
		return nil, fmt.Errorf("failed to parse domain XML: %w", err)
	}

	var devices []string
	for _, d := range dom.Disks {
		if d.Target.Dev != "" {
			devices = append(devices, fmt.Sprintf("/dev/%s", d.Target.Dev))
		}
	}

	return devices, nil
}

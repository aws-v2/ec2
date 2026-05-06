package application

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	domain "ec2-api/internal/domain/instance"
)




// Map of image download URLs
var imageURLMap = map[string]string{
	// Ubuntu cloud images
	"ubuntu-20.04": "https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-amd64.img",
	"ubuntu-22.04": "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img",
	"ubuntu-24.04": "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img",

	// Debian cloud images
	"debian-11": "https://cloud.debian.org/images/cloud/bullseye/latest/debian-11-generic-amd64.qcow2",
	"debian-12": "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-generic-amd64.qcow2",

	// Rocky Linux (CentOS/Amazon Linux alternative)
	"rocky-8": "https://download.rockylinux.org/pub/rocky/8/images/x86_64/Rocky-8-GenericCloud-Base.latest.x86_64.qcow2",
	"rocky-9": "https://download.rockylinux.org/pub/rocky/9/images/x86_64/Rocky-9-GenericCloud-Base.latest.x86_64.qcow2",
	// AlmaLinux (CentOS/Amazon Linux alternative)
	"almalinux-8": "https://repo.almalinux.org/almalinux/8/cloud/x86_64/images/AlmaLinux-8-GenericCloud-latest.x86_64.qcow2",
	"almalinux-9": "https://repo.almalinux.org/almalinux/9/cloud/x86_64/images/AlmaLinux-9-GenericCloud-latest.x86_64.qcow2",

	// Fedora cloud images
	"fedora-39": "https://download.fedoraproject.org/pub/fedora/linux/releases/39/Cloud/x86_64/images/Fedora-Cloud-Base-39-1.5.x86_64.qcow2",
	"fedora-40": "https://download.fedoraproject.org/pub/fedora/linux/releases/40/Cloud/x86_64/images/Fedora-Cloud-Base-40-1.14.x86_64.qcow2",

	// CentOS Stream
	"centos-stream-9": "https://cloud.centos.org/centos/9-stream/x86_64/images/CentOS-Stream-GenericCloud-9-latest.x86_64.qcow2",
}

// ensureImageExists checks if an image exists, and downloads it if it doesn't
func (s *InstanceService) EnsureImageExists(imageName string, imagePath string) error {
	// Check if file exists
	if _, err := os.Stat(imagePath); err == nil {
		fmt.Printf("Image %s already exists at %s\n", imageName, imagePath)
		return nil
	}

	// Get download URL
	downloadURL, ok := imageURLMap[imageName]
	if !ok {
		return fmt.Errorf("no download URL configured for image: %s", imageName)
	}

	fmt.Printf("Image %s not found. Downloading from %s...\n", imageName, downloadURL)

	// Ensure directory exists
	dir := filepath.Dir(imagePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Download the image
	if err := s.downloadImage(downloadURL, imagePath); err != nil {
		return fmt.Errorf("failed to download image: %w", err)
	}

	fmt.Printf("Successfully downloaded %s to %s\n", imageName, imagePath)
	return nil
}

// downloadImage downloads a file from a URL to a local path
func (s *InstanceService) downloadImage(url string, destPath string) error {
	// Create temporary file
	tmpPath := destPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// Download the file
	resp, err := http.Get(url)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.Remove(tmpPath)
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Get file size for progress tracking
	fileSize := resp.ContentLength
	fmt.Printf("Downloading %d MB...\n", fileSize/(1024*1024))

	// Write to file with progress indication
	written, err := io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	fmt.Printf("Downloaded %d MB\n", written/(1024*1024))

	// Move temp file to final location
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename file: %w", err)
	}

	return nil
}




func (s *InstanceService) injectPayloadIntoDisk(instance *domain.Instance, profile string, params map[string]string, diskPath string) error {
	// Case-insensitive lookup with STORAGE_ARN as fallback for DOWNLOAD_URL
	var downloadURL, headlessBin string
	for k, v := range params {
		switch strings.ToUpper(k) {
		case "DOWNLOAD_URL", "STORAGE_ARN":
			if downloadURL == "" || strings.ToUpper(k) == "DOWNLOAD_URL" {
				downloadURL = v
			}
		case "HEADLESS_BIN":
			headlessBin = v
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("missing DOWNLOAD_URL or STORAGE_ARN in parameters (keys found: %v)", getMapKeys(params))
	}

	// For gamelift, headlessBin is mandatory. For others (like ai-worker), it's optional.
	if headlessBin == "" && instance.Image == "" { // Use a better check if needed, but for now let's just use profile if we had it here
		// We don't have profile here directly easily without changing signature,
		// but we can check if it's required based on some heuristic or just allow it to be empty.
		log.Printf("[VM] No HEADLESS_BIN provided, skipping automated execution setup")
	}

	// 1. Create a workspace on host
	workdir := filepath.Join(os.TempDir(), fmt.Sprintf("inject-%s", instance.ID))
	if err := os.MkdirAll(workdir, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(workdir)

	// 2. Download payload to host
	zipPath := filepath.Join(workdir, "payload.zip")
	s.publishProgress(instance.ID, StageDownloadingPayload, "Downloading payload package to host...")

	if strings.HasPrefix(downloadURL, "arn:aws:s3:::") {
		// Parse ARN: arn:aws:s3:::bucket/key/path/file.zip
		path := strings.TrimPrefix(downloadURL, "arn:aws:s3:::")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) < 2 {
			return fmt.Errorf("invalid S3 ARN format: %s", downloadURL)
		}
		bucket := parts[0]
		key := parts[1]

		if s.minioAdapter == nil {
			return fmt.Errorf("MinIO adapter not initialized, cannot handle S3 ARN")
		}

		log.Printf("[VM] Downloading from MinIO: bucket=%s, key=%s", bucket, key)
		if err := s.minioAdapter.DownloadFile(context.Background(), bucket, key, zipPath); err != nil {
			return fmt.Errorf("failed to download from MinIO: %w", err)
		}
	} else {
		if err := s.downloadImage(downloadURL, zipPath); err != nil {
			return fmt.Errorf("failed to download payload: %w", err)
		}
	}

	// 3. Unzip on host
	s.publishProgress(instance.ID, StageUnzippingPayload, "Extracting payload...")
	extractDir := filepath.Join(workdir, "payload")
	if err := s.unzip(zipPath, extractDir); err != nil {
		return fmt.Errorf("failed to unzip: %w", err)
	}

	// 4. Multi-stage guestmount injection
	s.publishProgress(instance.ID, StageInjectingPayload, "Injecting payload files into instance disk...")
	
	mountDir := filepath.Join(workdir, "mount")
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		return err
	}

	// Wait for disk to be settled
	time.Sleep(1 * time.Second)

	// Mount
	log.Printf("[VM] Mounting %s to %s", diskPath, mountDir)
	cmd := exec.Command("guestmount", "-a", diskPath, "-i", mountDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("guestmount failed: %w\nOutput: %s", err, string(out))
	}
	// 5. Create directory structure inside mount
	targetDir := filepath.Join(mountDir, "opt", "game", "run")
	if profile == "ai-worker" {
		targetDir = filepath.Join(mountDir, "opt", "inference", "run")
	}

	if err := exec.Command("mkdir", "-p", targetDir).Run(); err != nil {
		return fmt.Errorf("failed to create target dir: %w", err)
	}

	// 6. Copy files
	log.Printf("[VM] Copying files to %s", targetDir)
	cpCmd := exec.Command("cp", "-r", extractDir+"/.", targetDir)
	if out, err := cpCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy failed: %w\nOutput: %s", err, string(out))
	}

	// 7. Ensure binary is executable inside the mount (if provided)
	if headlessBin != "" {
		binPath := filepath.Join(targetDir, headlessBin)
		_ = exec.Command("chmod", "+x", binPath).Run()
	}

	// 8. Unmount and cleanup
	log.Printf("[VM] Unmounting %s...", mountDir)
	var lastUnmountErr error
	for i := 0; i < 5; i++ {
		unmountCmd := exec.Command("guestunmount", mountDir)
		if err := unmountCmd.Run(); err == nil {
			lastUnmountErr = nil
			break
		} else {
			lastUnmountErr = err
			log.Printf("[Gamelift] Unmount attempt %d failed, retrying...", i+1)
			time.Sleep(500 * time.Millisecond)
		}
	}

	if lastUnmountErr != nil {
		log.Printf("[Gamelift] Persistent unmount failure for %s: %v", mountDir, lastUnmountErr)
	}

	// Final rest to ensure FUSE/QEMU releases the file handle
	time.Sleep(2 * time.Second)

	log.Printf("[VM] Injection successful for %s", instance.VMName)
	return nil
}





func (s *InstanceService) unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}



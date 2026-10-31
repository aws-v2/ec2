// application/volume_service.go
package application

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	domain "ec2-api/internal/domain/instance"

	libvirt "ec2-api/internal/infra/libvirt"
	messaging "ec2-api/internal/infra/messaging"
	interfaces "ec2-api/internal/interfaces"
	ec2metrics "ec2-api/internal/metrics"

	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"

	"github.com/google/uuid"
)

type VolumeService struct {
	repo                interfaces.VolumeRepository
	instanceRepo        interfaces.InstanceRepository
	hostRepo            interfaces.HostRepository
	publisher           *messaging.NATSPublisher
	assetServiceBaseURL string
	natsPrefix          string
	libvirtClient       *libvirt.LibvirtClient
	// hostService       application.HostService
}

func NewVolumeService(
	repo interfaces.VolumeRepository,
	instanceRepo interfaces.InstanceRepository,
	libvirtClient *libvirt.LibvirtClient,
	hostRepo interfaces.HostRepository,
	natsPrefix string,
	publisher *messaging.NATSPublisher,
	assetServiceBaseURL string,
	// hostService       application.HostService,

) *VolumeService {
	return &VolumeService{
		repo:                repo,
		instanceRepo:        instanceRepo,
		libvirtClient:       libvirtClient,
		hostRepo:            hostRepo,
		publisher:           publisher,
		assetServiceBaseURL: assetServiceBaseURL,
		natsPrefix:          natsPrefix,
		// hostService: hostService,
	}
}

func (s *VolumeService) CreateVolume(req *domain.CreateVolumeRequest, userId string) (*domain.Volume, error) {
	volumeName := fmt.Sprintf("vol-%s", uuid.New().String()[:8])

	volume := &domain.Volume{
		VolumeName:       volumeName,
		Name:             req.Name,
		UserID:           userId,
		Size:             req.Size,
		Format:           "qcow2", // Internal default
		Type:             req.Type,
		AvailabilityZone: req.AvailabilityZone,
		Status:           domain.VolumeStatusCreating,
		InternalName:     volumeName,
	}

	if err := s.repo.Create(volume); err != nil {
		return nil, fmt.Errorf("failed to save volume: %w", err)
	}
	// volume.VolumeName = fmt.Sprintf("%s.%s", volume.VolumeName, volume.Format)
	go s.createVolumeAsync(volume, userId, req.Size)

	return volume, nil
}

func (s *VolumeService) createVolumeAsync(volume *domain.Volume, userId string, size int) {
	storagePath, volPathErr := volume.GetStoragePath()
	if volPathErr != nil {
		log.Printf("Volume path error: %v", volPathErr)
		return
	}

	bestHosts, err := s.hostRepo.GetBestHosts(1)
	if err != nil {
		log.Printf("Failed to get best host to create a  volume via the agent: %v", err)

		return
	}
	if len(bestHosts) == 0 {
		log.Printf("Best house for creating volume is len(0)")

		return
	}

	host := bestHosts[0]

	checksum, err := s.CreateVolumeViaAgent(host.IP, storagePath, size)

	if err != nil {
		log.Printf("Failed to cerate volume via the agent: %v", err)
		return
	}

	log.Printf("Checksum calculated: %v", checksum)
	volume.SHA256 = checksum

	timestamp := time.Now().Format("2006-01-02T15-04-05")
	volumename := fmt.Sprintf("%s-%s.qcow2", volume.VolumeName, timestamp)

	volume.VolumeName = volumename

	s3Key := fmt.Sprintf("volumes/%s", volume.VolumeName)
	assetName := fmt.Sprintf("%s", volume.VolumeName)

	volumeData := domain.CreatePresignedURLRequest{
		UserID:     userId,
		GameID:     volume.VolumeName,
		AssetID:    volume.VolumeName,
		AssetType:  "ec2-volume",
		AssetName:  assetName,
		BucketName: "ec2-storage-volumes",
		Key:        s3Key,
		Sha256:     checksum,
	}

	log.Printf("Volume data for s3 url: %+v", volumeData)

	presignedPath, err := s.publisher.PublishCreateUploadURL(volumeData)
	if err != nil {
		log.Printf("failed to get presigned upload url for volume %d: %v", volume.ID, err)
		volume.Status = domain.VolumeStatusDeleting
		s.repo.Update(volume)
		return
	}
	log.Printf("Returned s3 url: %+v", presignedPath)

	fullUrl := fmt.Sprintf("%s%s", s.assetServiceBaseURL, presignedPath)

	if err := uploadFileToPresignedURL(fullUrl, storagePath); err != nil {
		log.Printf("failed to upload volume %d to s3: %v", volume.ID, err)
		volume.Status = domain.VolumeStatusDeleting
		s.repo.Update(volume)
		return
	}

	log.Printf("volume %d uploaded to s3 successfully at key %s", volume.ID, s3Key)
	volume.SHA256 = checksum
	volume.Status = domain.VolumeStatusAvailable
	volume.LatestReference = checksum

	if err := s.repo.Update(volume); err != nil {
		log.Printf("failed to update volume %d after upload: %v", volume.ID, err)
	}

	err = s.UploadAndDeleteFileViaAgent(host.IP, storagePath, fullUrl, "")
	if err != nil {
		log.Printf("Failed to uplaod and delete the volume %s from host", volume.Name)
		return
	}
}
func (l *VolumeService) CreateVolumeViaAgent(hostIP, volumePath string, sizeGB int) (checksum string, retErr error) {
	defer func() {
		if retErr != nil {
			ec2metrics.RecordAgentCallError("volume_create", hostIP)
		}
	}()

	reqBody := HostActionRequest{
		Type: "create_volume",
		Params: map[string]string{
			"volume_path": volumePath,
			"size_gb":     strconv.Itoa(sizeGB),
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal host action request: %w", err)
	}

	url := fmt.Sprintf("http://%s:9030/host-actions", hostIP)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to build host action request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("X-Agent-Key", l.agentAPIKey)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to reach agent on host %s: %w", hostIP, err)
	}
	defer resp.Body.Close()

	var result HostActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode agent response: %w", err)
	}

	if resp.StatusCode != http.StatusOK || result.Status != "success" {
		return "", fmt.Errorf("agent reported failure creating volume: %s", result.Message)
	}

	return result.Checksum, nil
}

// uploadFileToPresignedURL streams the file at path directly into the
// HTTP request body via PUT, without loading the whole file into memory.
func uploadFileToPresignedURL(url, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file for upload: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, url, f)
	if err != nil {
		return fmt.Errorf("failed to build upload request: %w", err)
	}
	req.ContentLength = info.Size()
	req.Header.Set("Content-Type", "application/octet-stream")

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// CalculateSHA256File hashes a file by streaming it, without loading
// the whole file into memory — needed for large volume/disk files.
func CalculateSHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file for hashing: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("failed to hash file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *VolumeService) ListVolumes() ([]*domain.Volume, error) {
	// TODO: Fire event: volume_list_started
	volumes, err := s.repo.FindAll()
	if err != nil {
		// TODO: Fire event: volume_list_db_failed
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}

	// TODO: Fire event: volume_list_completed
	return volumes, nil
}

func (s *VolumeService) GetVolume(volumeID int) (*domain.Volume, error) {
	// TODO: Fire event: volume_get_started
	volume, err := s.repo.FindByID(volumeID)
	if err != nil {
		// TODO: Fire event: volume_get_db_failed
		return nil, fmt.Errorf("failed to get volume: %w", err)
	}
	if volume == nil {
		// TODO: Fire event: volume_not_found
		return nil, fmt.Errorf("volume not found: %d", volumeID)
	}

	// TODO: Fire event: volume_get_completed
	return volume, nil
}
func (s *VolumeService) DeleteVolumeSnapshot(snapshotID string) error {

	volumeSnapshot, err := s.repo.FindVolumeSnapshotByID(snapshotID)
	if err != nil {
		return fmt.Errorf("snapshot not found: %w", err)
	}
	// send delete call to s3 then delete it from db
	resp, err := s.publisher.PublishDeleteFile(domain.DeleteFileNatsRequest{
		Sha256: *volumeSnapshot.SHA256,
	})
	if err != nil || resp.Code == 500 {
		log.Printf("failed to delete volume file from s3 %d: %v", volumeSnapshot.ID, err)

		return err
	}
	log.Printf("Returned s3 url: %+v", resp.Message)
	err = s.repo.DeleteVolumeSnapshot(snapshotID)

	return nil

}
func (s *VolumeService) ListSnapshotsByVolume(volumeID int) ([]*domain.VolumeSnapshot, error) {
	return s.repo.FindByVolumeSnapshotByVolumeID(&volumeID)
}

// TODO:HIGH
func (s *VolumeService) CreateVolumeFromSnapshot(req *domain.CreateVolumeFromSnapshotRequest, userId string) (*domain.Volume, error) {
	snapshot, err := s.repo.FindVolumeSnapshotByID(req.SnapshotID)

	if err != nil {
		return nil, fmt.Errorf("snapshot not found: %w", err)
	}
	if snapshot.Status != domain.VolumeSnapshotStatusReady {
		return nil, fmt.Errorf("cannot create volume from snapshot with status: %s", snapshot.Status)
	}
	volumeName := fmt.Sprintf("vol-%s", uuid.New().String()[:8])

	volume := &domain.Volume{
		Name:             req.VolumeName,
		VolumeName:       volumeName,
		Size:             snapshot.Size,
		Format:           "qcow2",
		Type:             "gp3",
		AvailabilityZone: "us-east-1a",
		UserID:           userId,
		Status:           domain.VolumeStatusCreating,
	}

	return volume, nil
}

func (s *VolumeService) CreateVolumeSnapshot(
	volumeID *int,
	req *domain.CreateVolumeSnapshotRequest,
	userId string,
) (*domain.VolumeSnapshot, error) {

	volume, err := s.repo.FindByID(*volumeID)
	if err != nil || volume == nil {
		return nil, fmt.Errorf("volume not found: %d", volumeID)
	}

	snapshotName := req.Name
	if snapshotName == "" {
		snapshotName = fmt.Sprintf("snap-%d", time.Now().Unix())
	}

	snapshot := &domain.VolumeSnapshot{
		VolumeID:    volumeID,
		ID:          uuid.New(),
		Name:        snapshotName,
		Description: req.Description,
		Status:      domain.VolumeSnapshotStatusPending,
		Size:        volume.Size,

		// Will be populated when the physical snapshot is created.
		SnapshotPath: nil,
		StorageKey:   nil,
		SHA256:       nil,
	}

	if err := s.repo.CreateVolumeSnapshot(snapshot); err != nil {
		return nil, fmt.Errorf("failed to save snapshot metadata: %w", err)
	}

	// For now, only handle volumes attached to a VM.
	if volume.AttachedTo != "" {
		log.Printf("Volume is currently attached")

		// Get the VM/instance the volume is attached to.
		instance, err := s.instanceRepo.FindByID(volume.AttachedTo)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to get instance %s: %w",
				volume.AttachedTo,
				err,
			)
		}

		if instance == nil {
			return nil, fmt.Errorf(
				"instance not found: %s",
				volume.AttachedTo,
			)
		}

		if instance.HostID == "" {
			return nil, fmt.Errorf(
				"instance %s has no host assigned",
				instance.ID,
			)
		}

		// Get the host where the VM is running.
		host, err := s.hostRepo.GetByID(instance.HostID)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to get host %s: %w",
				instance.HostID,
				err,
			)
		}

		if host == nil {
			return nil, fmt.Errorf(
				"host not found: %s",
				instance.HostID,
			)
		}

		// Get the volume's backing file path.
		// volumePath, err := volume.GetStoragePath()
		volumePath, err := volume.GetStoragePath()
		if err != nil {
			return nil, fmt.Errorf(
				"failed to get storage path for volume %s: %w",
				volume.VolumeName,
				err,
			)
		}
		// volumePath = fmt.Sprintf("%s/%s", volumePath, volume.VolumeName)

		log.Printf(
			"[VolumeService:CreateVolumeSnapshot] volume=%s attached_to=%s host=%s volume_path=%s",
			volume.VolumeName,
			instance.ID,
			instance.HostID,
			volumePath,
		)
		snapshotPath := fmt.Sprintf(
			"/var/lib/libvirt/images/snapshot-%s.qcow2",
			snapshot.ID,
		)
		err = s.libvirtClient.CreateAttachedVolumeSnapshot(
			host.IP,
			host.SSHUser,
			host.SSHPrivateKey,
			instance.VMName,
			volumePath,
			snapshotPath,
		)
		if err != nil {
			s.repo.UpdateVolumeSnapshotStatus(
				snapshot.ID,
				domain.VolumeSnapshotStatusFailed,
			)

			return nil, fmt.Errorf(
				"failed to create physical volume snapshot: %w",
				err,
			)
		}
		s.repo.UpdateVolumeSnapshotStatus(
			snapshot.ID,
			domain.VolumeSnapshotStatusReady,
		)

		checksum, err := s.CalculateChecksumViaAgent(host.IP, snapshotPath)
		if err != nil {
			log.Printf("failed to compute sha256 for snapshot %d via agent: %v", snapshot.ID, err)
			s.repo.UpdateVolumeSnapshotStatus(snapshot.ID, domain.VolumeSnapshotStatusFailed)
			return nil, fmt.Errorf("failed to checksum snapshot file: %w", err)
		}

		log.Printf("checksum calculated: %v", checksum)

		s3Key := fmt.Sprintf("snapshots/%s", volume.VolumeName)

		volumeSnapshotData := domain.CreatePresignedURLRequest{
			UserID:     userId,
			GameID:     snapshot.ID.String(),
			AssetID:    snapshot.ID.String(),
			AssetType:  "ec2-volume-snapshot",
			AssetName:  snapshot.Name,
			BucketName: "ec2-volume-snapshots",
			Key:        s3Key,
			Sha256:     checksum,
		}

		presignedPath, err := s.publisher.PublishCreateUploadURL(volumeSnapshotData)
		if err != nil {
			log.Printf("failed to get presigned upload url for snapshot %s: %v", snapshot.ID, err)
			s.repo.UpdateVolumeSnapshotStatus(snapshot.ID, domain.VolumeSnapshotStatusFailed)
			return nil, err
		}
		log.Printf("returned s3 url: %+v", presignedPath)

		fullUrl := fmt.Sprintf("%s%s", s.assetServiceBaseURL, presignedPath)

		if err := s.UploadAndDeleteFileViaAgent(host.IP, snapshotPath, fullUrl, ""); err != nil {
			log.Printf("failed to upload snapshot %s to s3 via agent: %v", snapshot.ID, err)
			s.repo.UpdateVolumeSnapshotStatus(snapshot.ID, domain.VolumeSnapshotStatusFailed)
			return nil, err
		}

		log.Printf("snapshot %s uploaded to s3 successfully at key %s", snapshot.ID, s3Key)

		snapshot.SHA256 = &checksum
		snapshot.StorageKey = &s3Key
		snapshot.Status = domain.VolumeSnapshotStatusReady

		if err := s.repo.UpdateVolumeSnapshot(snapshot); err != nil {
			log.Printf("failed to persist snapshot %s metadata after upload: %v", snapshot.ID, err)
			return nil, fmt.Errorf("failed to update snapshot record: %w", err)
		}

		return snapshot, nil

	}

	log.Printf("Volume not attached ")

	bestHosts, err := s.hostRepo.GetBestHosts(1)
	if err != nil {
		s.repo.UpdateVolumeSnapshotStatus(snapshot.ID, domain.VolumeSnapshotStatusFailed)
		return nil, fmt.Errorf("failed to find an available host for snapshot staging: %w", err)
	}
	if len(bestHosts) == 0 {
		s.repo.UpdateVolumeSnapshotStatus(snapshot.ID, domain.VolumeSnapshotStatusFailed)
		return nil, fmt.Errorf("no available hosts found for snapshot staging")
	}

	host := bestHosts[0]

	log.Printf("staging detached volume %s snapshot on host %s", volume.VolumeName, host.IP)

	presignedURL, err := s.publisher.FetchAgentPresignedURL(
		userId,
		"version1",
		volume.LatestReference,
		volume.SHA256,
	)
	if err != nil {
		log.Printf("could not get presigned download url for volume %d: %v", volume.ID, err)
		volume.Status = domain.VolumeStatusAvailable
		s.repo.Update(volume)
		return nil, fmt.Errorf("failed to get presigned download url: %w", err)
	}
	log.Printf("presigned download url for volume %d: %s", volume.ID, presignedURL)

	storagePath, _ := volume.GetStoragePath()
	destinationDir := filepath.Dir(storagePath)
	presignedURL = fmt.Sprintf("%s%s", s.assetServiceBaseURL, presignedURL)
	log.Printf("Destination Dr1: %s", destinationDir)

	if err := s.DownloadVolumeFileViaAgent(host.IP, presignedURL, destinationDir, volume.VolumeName); err != nil {
		log.Printf("agent failed to download volume %d to host %s: %v", volume.ID, host.IP, err)
		// volume.Status = domain.VolumeStatusAvailable
		// s.repo.Update(volume)
		return nil, fmt.Errorf("failed to download volume to host for templating : %w", err)
	}

	log.Printf("agent confirmed volume %d downloaded successfully to host %s", volume.ID, host.IP)

	if err != nil {
		log.Printf("failed to get presigned upload url for volume %d: %v", volume.ID, err)
		volume.Status = domain.VolumeStatusDeleting
		s.repo.Update(volume)
		return nil, err
	}

	// here we now call the
	// CreateNotAttachedVolumeSnapshot
	// continue from here upto where the snpshotis create succesfully

	snapshotPath := fmt.Sprintf("%s/snapshot-%s.qcow2", destinationDir, snapshot.ID)

	log.Printf("Destination Dr1: %s", snapshotPath)
	log.Printf("Destination storage Dr2: %s", storagePath)

	checksum, err := s.ConvertImageViaAgent(host.IP, storagePath, snapshotPath, "qcow2")

	if err != nil {
		s.repo.UpdateVolumeSnapshotStatus(snapshot.ID, domain.VolumeSnapshotStatusFailed)
		return nil, fmt.Errorf("failed to convert downloaded volume to snapshot: %w", err)
	}

	log.Printf("Image converted succesfully: with sha: %s", checksum)

	timestamp := time.Now().Format("2006-01-02T15-04-05")
	s3Key := fmt.Sprintf("snapshots/%s-%s.qcow2", snapshotName, timestamp)

	volumeSnapshotData := domain.CreatePresignedURLRequest{
		UserID:     userId,
		GameID:     snapshot.ID.String(),
		AssetID:    snapshot.ID.String(),
		AssetType:  "ec2-volume-snapshot",
		AssetName:  snapshot.Name,
		BucketName: "ec2-volume-snapshots",
		Key:        s3Key,
		Sha256:     checksum,
	}

	presignedPath, err := s.publisher.PublishCreateUploadURL(volumeSnapshotData)
	if err != nil {
		log.Printf("failed to get presigned upload url for snapshot %s: %v", snapshot.ID, err)
		s.repo.UpdateVolumeSnapshotStatus(snapshot.ID, domain.VolumeSnapshotStatusFailed)
		return nil, err
	}
	log.Printf("returned s3 url: %+v", presignedPath)

	fullUrl := fmt.Sprintf("%s%s", s.assetServiceBaseURL, presignedPath)

	if err := s.UploadAndDeleteFileViaAgent(host.IP, snapshotPath, fullUrl, storagePath); err != nil {
		log.Printf("failed to upload snapshot %s to s3 via agent: %v", snapshot.ID, err)
		s.repo.UpdateVolumeSnapshotStatus(snapshot.ID, domain.VolumeSnapshotStatusFailed)
		return nil, err
	}

	log.Printf("snapshot %s uploaded to sn3 successfully at key %s", snapshot.ID, s3Key)

	snapshot.SHA256 = &checksum
	snapshot.Status = domain.VolumeSnapshotStatusReady

	s.repo.UpdateVolumeSnapshot(snapshot)

	return &domain.VolumeSnapshot{
		ID:           snapshot.ID,
		VolumeID:     volumeID,
		Name:         snapshot.Name,
		Description:  snapshot.Description,
		Status:       snapshot.Status,
		CreatedAt:    snapshot.CreatedAt,
		Size:         snapshot.Size,
		SnapshotPath: snapshot.SnapshotPath,
		StorageKey:   snapshot.StorageKey,
		SHA256:       snapshot.SHA256,
	}, nil
}
func (l *VolumeService) ConvertImageViaAgent(hostIP, sourcePath, destPath, format string) (checksum string, retErr error) {
	defer func() {
		if retErr != nil {
			ec2metrics.RecordAgentCallError("volume_convert", hostIP)
		}
	}()

	reqBody := HostActionRequest{
		Type: "convert_image",
		Params: map[string]string{
			"source_path": sourcePath,
			"dest_path":   destPath,
			"format":      format,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal host action request: %w", err)
	}

	url := fmt.Sprintf("http://%s:9030/host-actions", hostIP)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to build host action request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("X-Agent-Key", l.agentAPIKey)

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to reach agent on host %s: %w", hostIP, err)
	}
	defer resp.Body.Close()

	var result HostActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode agent response: %w", err)
	}

	if resp.StatusCode != http.StatusOK || result.Status != "success" {
		return "", fmt.Errorf("agent reported failure converting image: %s", result.Message)
	}

	return result.Checksum, nil
}
func (s *VolumeService) ReleaseVolumeReservation(volumeID int) (*domain.Volume, error) {
	volume, err := s.repo.FindByID(volumeID)
	if err != nil {
		return nil, fmt.Errorf("failed to find volume for reservation: %w", err)
	}
	if volume == nil {
		return nil, fmt.Errorf("volume not found: %d", volumeID)
	}

	if volume.Status != domain.VolumeStatusReserved {
		if volume.Status == domain.VolumeStatusAttached {
			return nil, fmt.Errorf("volume is currently attached to: %s", volume.AttachedTo)
		}
		return nil, fmt.Errorf("volume not currently reserved, current status: %s", volume.Status)
	}

	volume.Status = domain.VolumeStatusAvailable
	volume.ReservedTo = ""
	volume.ReservedAt = sql.NullTime{}
	volume.ReservationExpiresAt = sql.NullTime{}

	if err := s.repo.Update(volume); err != nil {
		return nil, fmt.Errorf("failed to update volume status to available: %w", err)
	}

	return volume, nil
}

func (s *VolumeService) ReserveVolume(volumeID int, req domain.ReservationRequest) (*domain.Volume, error) {
	volume, err := s.repo.FindByID(volumeID)
	if err != nil {
		return nil, fmt.Errorf("failed to find volume for reservation: %w", err)
	}
	if volume == nil {
		return nil, fmt.Errorf("volume not found: %d", volumeID)
	}

	if volume.Status != domain.VolumeStatusAvailable {
		return nil, fmt.Errorf("cannot reserve a volume that is currently: %s", volume.Status)
	}

	volume.Status = domain.VolumeStatusReserved
	volume.ReservedTo = req.ReservedTo
	volume.ReservedAt = sql.NullTime{Time: time.Now(), Valid: true}
	volume.ReservationExpiresAt = sql.NullTime{Time: req.ExpiresAt, Valid: true}

	if err := s.repo.Update(volume); err != nil {
		return nil, fmt.Errorf("failed to update volume status to reserved: %w", err)
	}

	return volume, nil
}
func (l *VolumeService) UploadAndDeleteFileViaAgent(hostIP, filePath, presignedURL, extra string) (retErr error) {
	defer func() {
		if retErr != nil {
			ec2metrics.RecordAgentCallError("upload_and_delete", hostIP)
		}
	}()

	params := map[string]string{
		"path": filePath,
		"url":  presignedURL,
	}

	if extra != "" {
		params["extras"] = extra

		log.Printf("The e*xtras: %v", extra)

	}

	reqBody := HostActionRequest{
		Type:   "upload_and_delete",
		Params: params,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal host action request: %w", err)
	}

	url := fmt.Sprintf("http://%s:9030/host-actions", hostIP)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build host action request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("X-Agent-Key", l.agentAPIKey)

	// Large files take a while to upload — give it plenty of room.
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach agent on host %s: %w", hostIP, err)
	}
	defer resp.Body.Close()

	var result HostActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode agent response: %w", err)
	}

	if resp.StatusCode != http.StatusOK || result.Status != "success" {
		return fmt.Errorf("agent reported failure uploading file: %s", result.Message)
	}

	return nil
}
func (s *VolumeService) ExpandVolume(volumeID int, newSize int, userId string) (*domain.Volume, error) {
	volume, err := s.repo.FindByID(volumeID)
	if err != nil || volume == nil {
		return nil, fmt.Errorf("volume not found: %d", volumeID)
	}

	if newSize <= volume.Size {
		return nil, fmt.Errorf("new size must be greater than current size (%d GB)", volume.Size)
	}

	go func() {
		if volume.AttachedTo == "" {

			log.Printf("Volume not attached ")

			bestHosts, err := s.hostRepo.GetBestHosts(1)

			volume.Status = domain.VolumeStatusExpanding
			s.repo.Update(volume)

			if err != nil {
				s.repo.UpdateVolumeStatus(volume.ID, domain.VolumeStatusExpanding)
				return
			}
			if len(bestHosts) == 0 {
				s.repo.UpdateVolumeStatus(volume.ID, domain.VolumeStatusExpandingFailed)
				return
			}

			host := bestHosts[0]

			log.Printf("staging detached volume %s snapshot on host %s", volume.VolumeName, host.IP)

			presignedURL, err := s.publisher.FetchAgentPresignedURL(
				userId,
				"version1",
				volume.LatestReference,
				volume.SHA256,
			)
			if err != nil {
				log.Printf("could not get presigned download url for volume %d: %v", volume.ID, err)
				volume.Status = domain.VolumeStatusAvailable
				s.repo.Update(volume)
				return
			}
			log.Printf("presigned download url for volume %d: %s", volume.ID, presignedURL)

			storagePath, _ := volume.GetStoragePath()
			destinationDir := filepath.Dir(storagePath)
			presignedURL = fmt.Sprintf("%s%s", s.assetServiceBaseURL, presignedURL)
			log.Printf("Destination Dr1: %s", destinationDir)

			if err := s.DownloadVolumeFileViaAgent(host.IP, presignedURL, destinationDir, volume.VolumeName); err != nil {
				log.Printf("agent failed to download volume %d to host %s: %v", volume.ID, host.IP, err)
				volume.Status = domain.VolumeStatusAvailable
				s.repo.Update(volume)
				return
			}

			log.Printf("agent confirmed volume %d downloaded successfully to host %s", volume.ID, host.IP)

			if err != nil {
				log.Printf("failed to get presigned upload url for volume %d: %v", volume.ID, err)
				volume.Status = domain.VolumeStatusDeleting
				s.repo.Update(volume)
				return
			}

			// // here we now call the
			// // CreateNotAttachedVolumeSnapshot
			// // continue from here upto where the snpshotis create succesfully

			// snapshotPath := fmt.Sprintf("%s/snapshot-%s.qcow2", destinationDir, snapshot.ID)

			// log.Printf("Destination Dr1: %s", snapshotPath)
			// log.Printf("Destination storage Dr2: %s", storagePath)

			checksum, err := s.ResizeImageViaAgent(host.IP, storagePath, newSize)

			if err != nil {
				s.repo.UpdateVolumeStatus(volume.ID, domain.VolumeStatusExpandingFailed)
				return
			}

			log.Printf("Image resized succesfully")

			timestamp := time.Now().Format("2006-01-02T15-04-05")
			s3Key := fmt.Sprintf("snapshots/%s-%s.qcow2", volume.VolumeName, timestamp)

			volumeSnapshotData := domain.CreatePresignedURLRequest{
				UserID:     userId,
				GameID:     volume.VolumeName,
				AssetID:    volume.VolumeName,
				AssetType:  "ec2-volume-snapshot",
				AssetName:  volume.VolumeName,
				BucketName: "ec2-volume-snapshots",
				Key:        s3Key,
				Sha256:     checksum,
			}

			presignedPath, err := s.publisher.PublishCreateUploadURL(volumeSnapshotData)
			if err != nil {
				log.Printf("failed to get presigned upload url for snapshot %s: %v", volumeID, err)
				s.repo.UpdateVolumeStatus(volume.ID, domain.VolumeStatusExpandingFailed)
				return
			}
			log.Printf("returned s3 url: %+v", presignedPath)

			fullUrl := fmt.Sprintf("%s%s", s.assetServiceBaseURL, presignedPath)

			if err := s.UploadAndDeleteFileViaAgent(host.IP, storagePath, fullUrl, storagePath); err != nil {
				log.Printf("failed to upload snapshot %s to s3 via agent: %v", volume.ID, err)
				s.repo.UpdateVolumeStatus(volume.ID, domain.VolumeStatusExpandingFailed)
				return
			}

			log.Printf("volume %s uploaded to sn3 successfully at key %s", volume.ID, s3Key)
			volume.Status = domain.VolumeStatusAvailable
			// s.repo.UpdateVolumeStatus(
			// 	volume.ID,
			// 	domain.VolumeStatusAvailable,
			// )

			volume.Size = newSize
			volume.SHA256 = checksum
			volume.LatestReference = s3Key
			volume.Status = domain.VolumeStatusAvailable
			s.repo.Update(volume)

		}

		if volume.AttachedTo != "" {

			log.Printf("Volume IIIIISSSS attached ")

			instance, err := s.instanceRepo.FindByID(volume.AttachedTo)
			if err != nil || instance == nil {
				s.repo.UpdateVolumeStatus(volume.ID, domain.VolumeStatusExpandingFailed)
				return
			}

			host, err := s.hostRepo.GetByID(instance.HostID)
			if err != nil || host == nil {
				s.repo.UpdateVolumeStatus(volume.ID, domain.VolumeStatusExpandingFailed)
				return
			}

			storagePath, err := volume.GetStoragePath()
			if err != nil {
				s.repo.UpdateVolumeStatus(volume.ID, domain.VolumeStatusExpandingFailed)
				return
			}

			volume.Status = domain.VolumeStatusExpanding
			s.repo.Update(volume)

			if err := s.libvirtClient.ResizeAttachedVolume(
				host.IP, host.SSHUser, host.SSHPrivateKey,
				instance.VMName, storagePath, newSize,
			); err != nil {
				log.Printf("failed to resize attached volume %d: %v", volume.ID, err)
				s.repo.UpdateVolumeStatus(volume.ID, domain.VolumeStatusExpandingFailed)
				return
			}

			volume.Size = newSize
			volume.Status = domain.VolumeStatusAttached
			s.repo.Update(volume)

			log.Printf("volume %d resized live to %dGB while attached", volume.ID, newSize)
			return
		}
		// storagePath, volPathErr := volume.GetStoragePath()
		// if volPathErr != nil {
		// 	log.Printf("Volume path error: %v", volPathErr)
		// 	return
		// }
		// if err := s.libvirtClient.ResizeVolume(storagePath, newSize); err != nil {
		// 	fmt.Printf("Failed to resize volume %d: %v\n", volume.ID, err)
		// 	volume.Status = domain.VolumeStatusAvailable
		// 	s.repo.Update(volume)
		// 	return
		// }

		// volume.Size = newSize
		// volume.Status = domain.VolumeStatusAvailable
		// s.repo.Update(volume)
		// s.repo.UpdateSize(volume.ID, newSize)
		// fmt.Printf("✓ Volume %d expanded to %d GB\n", volume.ID, newSize)
	}()

	return volume, nil
}

func (s *VolumeService) AttachVolume(volumeID int, instanceID, userId string) (*domain.Volume, error) {
	volume, err := s.repo.FindByID(volumeID)
	if err != nil || volume == nil {
		return nil, fmt.Errorf("volume not found: %v", err)
	}

	if volume.Status != domain.VolumeStatusAvailable {
		return nil, fmt.Errorf("volume is not available, current status: %s", volume.Status)
	}

	instance, err := s.instanceRepo.FindByID(instanceID)
	if err != nil || instance == nil {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}

	host, err := s.hostRepo.GetByID(instance.HostID)
	if err != nil || host == nil {
		return nil, fmt.Errorf("failed to find host for instance %s: %v", instanceID, err)
	}

	volume.Status = domain.VolumeStatusAttaching
	log.Printf("volume %d status -> attaching (sha256=%s, latest_reference=%s)", volume.ID, volume.SHA256, volume.LatestReference)
	s.repo.Update(volume)

	presignedURL, err := s.publisher.FetchAgentPresignedURL(
		userId,
		"version1",
		volume.LatestReference,
		volume.SHA256,
	)
	if err != nil {
		log.Printf("could not get presigned download url for volume %d: %v", volume.ID, err)
		volume.Status = domain.VolumeStatusAvailable
		s.repo.Update(volume)
		return nil, fmt.Errorf("failed to get presigned download url: %w", err)
	}
	log.Printf("presigned download url for volume %d: %s", volume.ID, presignedURL)

	storagePath, _ := volume.GetStoragePath()
	destinationDir := filepath.Dir(storagePath)
	presignedURL = fmt.Sprintf("%s%s", s.assetServiceBaseURL, presignedURL)
	log.Printf("Destination Dr %d: %s", destinationDir)

	if err := s.DownloadVolumeFileViaAgent(host.IP, presignedURL, destinationDir, volume.VolumeName); err != nil {
		log.Printf("agent failed to download volume %d to host %s: %v", volume.ID, host.IP, err)
		volume.Status = domain.VolumeStatusAvailable
		s.repo.Update(volume)
		return nil, fmt.Errorf("failed to download volume to host: %w", err)
	}

	log.Printf("agent confirmed volume %d downloaded successfully to host %s", volume.ID, host.IP)

	if err != nil {
		log.Printf("failed to get presigned upload url for volume %d: %v", volume.ID, err)
		volume.Status = domain.VolumeStatusDeleting
		s.repo.Update(volume)
		return nil, err
	}

	storagePath, volPathErr := volume.GetStoragePath()
	if volPathErr != nil {
		log.Printf("Volume path error: %v", volPathErr)
		return nil, fmt.Errorf("Volume path err: %v", volPathErr)
	}

	log.Printf("the declared storge path: %s vs th agent name %s", storagePath, volume.VolumeName)
	attachedDevices, err := s.libvirtClient.ListAttachedDevices(instance.VMName)

	devicePath := getNextDevicePathFromLibvirt(attachedDevices)

	err = s.libvirtClient.AttachVolume(instance.VMName, storagePath, devicePath, volume.Format, volume.Name)
	if err != nil {
		volume.Status = domain.VolumeStatusAvailable
		s.repo.Update(volume)
		return nil, fmt.Errorf("failed to attach volume: %w", err)
	}

	volume.AttachedTo = instanceID
	volume.DevicePath = devicePath
	volume.Status = domain.VolumeStatusAttached
	if err := s.repo.Update(volume); err != nil {
		s.libvirtClient.DetachVolume(instance.VMName, devicePath)
		return nil, fmt.Errorf("failed to update volume status: %w", err)
	}

	return volume, nil
}
func (s *VolumeService) ResizeImageViaAgent(hostIP, volumePath string, newSizeGB int) (checksum string, retErr error) {
	defer func() {
		if retErr != nil {
			ec2metrics.RecordAgentCallError("volume_resize", hostIP)
		}
	}()

	reqBody := HostActionRequest{
		Type: "Resize_volume",
		Params: map[string]string{
			"volume_path": volumePath,
			"new_size_gb": strconv.Itoa(newSizeGB),
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal host action request: %w", err)
	}

	url := fmt.Sprintf("http://%s:9030/host-actions", hostIP)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to build host action request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("X-Agent-Key", l.agentAPIKey)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to reach agent on host %s: %w", hostIP, err)
	}
	defer resp.Body.Close()

	var result HostActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode agent response: %w", err)
	}

	if resp.StatusCode != http.StatusOK || result.Status != "success" {
		return "", fmt.Errorf("agent reported failure resizing volume: %s", result.Message)
	}

	return result.Checksum, nil
}
func (l *VolumeService) DownloadVolumeFileViaAgent(hostIP, fileURL, destination, filename string) (retErr error) {
	defer func() {
		if retErr != nil {
			ec2metrics.RecordAgentCallError("download_file", hostIP)
		}
	}()

	reqBody := HostActionRequest{
		Type: "download_file",
		Params: map[string]string{
			"url":         fileURL,
			"destination": destination,
			"filename":    filename,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal host action request: %w", err)
	}

	url := fmt.Sprintf("http://%s:9030/host-actions", hostIP)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build host action request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("X-Agent-Key", l.agentAPIKey)

	// Generous timeout for now since we're downloading synchronously and
	// waiting for the agent's confirmation before proceeding to attach.
	// This will need to become async once volumes are large enough that
	// blocking the whole attach request on a download isn't viable.
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach agent on host %s: %w", hostIP, err)
	}
	defer resp.Body.Close()

	var result HostActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode agent response: %w", err)
	}

	if resp.StatusCode != http.StatusAccepted || result.Status != "success" {
		return fmt.Errorf("agent reported failure downloading file: %s", result.Message)
	}

	return nil
}
func (s *VolumeService) DetachVolume(volumeID int, userId string) (*domain.Volume, error) {
	volume, err := s.repo.FindByID(volumeID)
	if err != nil || volume == nil {
		return nil, fmt.Errorf("volume not found: %d", volumeID)
	}

	if volume.Status != domain.VolumeStatusAttached {
		return nil, fmt.Errorf("volume is not attached, current status: %s", volume.Status)
	}

	instance, err := s.instanceRepo.FindByID(volume.AttachedTo)
	if err != nil || instance == nil {
		return nil, fmt.Errorf("instance not found: %s", volume.AttachedTo)
	}

	// Safety Check: Root volume safety
	if volume.DevicePath == "/dev/vda" || volume.DevicePath == "/dev/sda1" {
		if instance.Status == domain.StatusRunning {
			return nil, fmt.Errorf("cannot detach root volume while instance is running")
		}
	}

	volume.Status = domain.VolumeStatusDetaching
	s.repo.Update(volume)

	err = s.libvirtClient.DetachVolume(instance.VMName, volume.DevicePath)
	if err != nil {
		volume.Status = domain.VolumeStatusAttached
		s.repo.Update(volume)
		return nil, fmt.Errorf("failed to detach volume: %w", err)
	}

	storagePath, volPathErr := volume.GetStoragePath()
	if volPathErr != nil {
		return nil, fmt.Errorf("failed to get volume path: %w", volPathErr)
	}
	// 7777
	checksum, err := CalculateSHA256File(storagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get volume checksum: %w", err)
	}

	if checksum != volume.SHA256 {
		log.Printf("Checksum calculated: %v", checksum)
		volume.SHA256 = checksum

		timestamp := time.Now().Format("2006-01-02T15-04-05")
		volumename := fmt.Sprintf("%s-%s.qcow2", volume.VolumeName, timestamp)

		s3Key := fmt.Sprintf("volumes/%s", volumename)
		assetName := fmt.Sprintf("%s-volume", volumename)

		volumeData := domain.CreatePresignedURLRequest{
			UserID:     userId,
			GameID:     volumename,
			AssetID:    volumename,
			AssetType:  "ec2-volume",
			AssetName:  assetName,
			BucketName: "ec2-volumes",
			Key:        s3Key,
			Sha256:     checksum,
		}

		log.Printf("Volume data for s3 url: %+v", volumeData)

		presignedPath, err := s.publisher.PublishCreateUploadURL(volumeData)
		if err != nil {
			log.Printf("failed to get presigned upload url for volume %d: %v", volume.ID, err)
			volume.Status = domain.VolumeStatusDeleting
			s.repo.Update(volume)
			return nil, err
		}
		log.Printf("Returned s3 url: %+v", presignedPath)

		fullUrl := fmt.Sprintf("%s%s", s.assetServiceBaseURL, presignedPath)

		if err := uploadFileToPresignedURL(fullUrl, storagePath); err != nil {
			log.Printf("failed to upload volume %d to s3: %v", volume.ID, err)
			volume.Status = domain.VolumeStatusDeleting
			s.repo.Update(volume)
			return nil, err
		}

		log.Printf("volume %d uploaded to s3 successfully at key %s", volume.ID, s3Key)
		volume.LatestReference = s3Key
	}

	volume.AttachedTo = ""
	volume.DevicePath = ""
	volume.Status = domain.VolumeStatusAvailable

	if err := s.repo.Update(volume); err != nil {
		return nil, fmt.Errorf("failed to update volume status: %w", err)
	}

	host, err := s.hostRepo.GetByID(instance.HostID)
	if err != nil {
		log.Printf("could not get the host where the volume is at: %v", err)
		return nil, err
	}

	if err := s.DeleteVolumeFileViaAgent(host.IP, storagePath); err != nil {
		log.Printf("failed to delete local volume file for volume %d after s3 backup: %v", volume.ID, err)
		return nil, fmt.Errorf("failed to clean up volume file on host: %w", err)
	}

	log.Printf("deleted volume %d file at %s on host %s after successful s3 backup", volume.ID, storagePath, host.IP)

	return volume, nil
}

type HostActionRequest struct {
	Type   string            `json:"type"`
	Params map[string]string `json:"params"`
}

type HostActionResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
	Checksum string `json:"checksum,omitempty"`
}

func (l *VolumeService) CalculateChecksumViaAgent(hostIP, filePath string) (checksum string, retErr error) {
	defer func() {
		if retErr != nil {
			ec2metrics.RecordAgentCallError("calculate_checksum", hostIP)
		}
	}()

	reqBody := HostActionRequest{
		Type: "calculate_checksum",
		Params: map[string]string{
			"file_path": filePath,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal host action request: %w", err)
	}

	url := fmt.Sprintf("http://%s:9030/host-actions", hostIP)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to build host action request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("X-Agent-Key", l.agentAPIKey)

	// Hashing a large file can take a while — give it room.
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to reach agent on host %s: %w", hostIP, err)
	}
	defer resp.Body.Close()

	var result HostActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode agent response: %w", err)
	}

	if resp.StatusCode != http.StatusOK || result.Status != "success" {
		return "", fmt.Errorf("agent reported failure calculating checksum: %s", result.Message)
	}

	return result.Checksum, nil
}
func (s *VolumeService) DeleteVolumeFileViaAgent(hostIP, storagePath string) (retErr error) {
	defer func() {
		if retErr != nil {
			ec2metrics.RecordAgentCallError("delete_file", hostIP)
		}
	}()

	reqBody := HostActionRequest{
		Type: "delete_file",
		Params: map[string]string{
			"file_path": storagePath,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal host action request: %w", err)
	}

	url := fmt.Sprintf("http://%s:9030/host-actions", hostIP)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build host action request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("X-Agent-Key", l.agentAPIKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach agent on host %s: %w", hostIP, err)
	}
	defer resp.Body.Close()

	var result HostActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode agent response: %w", err)
	}

	if resp.StatusCode != http.StatusAccepted || result.Status != "success" {
		return fmt.Errorf("agent reported failure deleting %s: %s", storagePath, result.Message)
	}

	return nil
}
func (s *VolumeService) DeleteVolume(volumeID int, force bool) error {
	// TODO: Fire event: volume_delete_started

	// Get volume
	volume, err := s.repo.FindByID(volumeID)
	if err != nil || volume == nil {
		// TODO: Fire event: volume_not_found_for_delete
		return fmt.Errorf("volume not found: %d", volumeID)
	}

	// Check volume status
	if volume.Status == domain.VolumeStatusAttached && !force {
		// TODO: Fire event: volume_delete_failed_attached
		return fmt.Errorf("cannot delete attached volume, detach first or use force")
	}

	// If forced and attached, we should attempt to detach from libvirt first if possible
	if volume.Status == domain.VolumeStatusAttached && force && volume.AttachedTo != "" {

		vol, err := s.DetachVolume(volume.ID, volume.UserID)

		if err != nil {
			log.Printf("Attached volume could not be detouched succefullly aborting with err :%v", err)
			return err
		}

		// send sha256 to s3 to delete the file, s3 deletes the file, then we update the database

		resp, err := s.publisher.PublishDeleteFile(domain.DeleteFileNatsRequest{
			Sha256: vol.SHA256,
		})
		if err != nil || resp.Code == 500 {
			log.Printf("failed to delete volume file from s3 %d: %v", volume.ID, err)

			return err
		}
		log.Printf("Returned s3 url: %+v", resp.Message)
		volume.Status = domain.VolumeStatus(domain.VolumeSnapshotStatusDeleted)
		s.repo.Update(volume)
		return nil
	}

	resp, err := s.publisher.PublishDeleteFile(domain.DeleteFileNatsRequest{
		Sha256: volume.SHA256,
	})
	if err != nil || resp.Code == 500 {
		log.Printf("failed to delete volume file from s3 %d: %v", volume.ID, err)

		return err
	}
	log.Printf("Returned s3 url: %+v", resp.Message)
	volume.Status = domain.VolumeStatus(domain.VolumeSnapshotStatusDeleted)
	s.repo.Update(volume)
	return nil

	// TODO: Fire event: validation_passed_for_delete

	// storagePath, volPathErr := volume.GetStoragePath()
	// if volPathErr != nil {
	// 	log.Printf("Volume path error: %v", volPathErr)
	// 	return fmt.Errorf("Vollume path error: %v", volPathErr)
	// }
	// // Delete volume using libvirt
	// err = s.libvirtClient.DeleteVolume(storagePath)
	// if err != nil {
	// 	// TODO: Fire event: libvirt_delete_failed
	// 	return fmt.Errorf("failed to delete volume file: %w", err)
	// }

	// // TODO: Fire event: libvirt_delete_completed

	// // Delete volume from database
	// if err := s.repo.Delete(volume); err != nil {
	// 	// TODO: Fire event: volume_delete_db_failed
	// 	return fmt.Errorf("failed to delete volume from database: %w", err)
	// }

	// // TODO: Fire event: volume_delete_completed
	// return nil
	return nil
}

func getNextDevicePathFromLibvirt(used []string) string {
	usedSet := map[string]bool{}
	for _, u := range used {
		usedSet[u] = true
	}

	// Typical pattern: /dev/vda, /dev/vdb, /dev/vdc, ...
	for c := 'a'; c <= 'z'; c++ {
		path := fmt.Sprintf("/dev/vd%c", c)
		if !usedSet[path] {
			return path
		}
	}

	return "/dev/vdz"
}

func (s *VolumeService) GetTags(volumeID int) ([]*domain.VolumeTag, error) {
	return s.repo.GetTags(volumeID)
}

func (s *VolumeService) AddOrUpdateTag(volumeID int, tag *domain.VolumeTag) error {
	return s.repo.AddOrUpdateTag(volumeID, tag)
}

func (s *VolumeService) DeleteTag(volumeID int, key string) error {
	return s.repo.DeleteTag(volumeID, key)
}

# Expanding a Volume: Attached vs. Detached Flow

Expanding a volume increases its size without recreating it or losing its data. Like [snapshot creation](../volumes-attaching-detaching-volumes), the approach splits into two different techniques depending on whether the volume is currently attached to a running VM or sitting detached in S3.

## The request, end to end

```
Client
  │  POST /volumes/42/expand  { "new_size": 50 }
  ▼
VolumeHandler.ExpandVolume
  │
  ▼
VolumeService.ExpandVolume
  │  Validates the new size, then immediately returns — the actual
  │  resize work happens in a background goroutine.
  │
  └─► go func() {
        ├─ Volume is detached ──► stage on a host, download, resize the
        │                         file directly, re-upload to S3
        └─ Volume is attached ──► resize the live block device via
                                   libvirt, no download needed at all
      }()
```

## The HTTP handler

```go
func (h *VolumeHandler) ExpandVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	userId := c.GetString("userID")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}

	type ExpandRequest struct {
		NewSize int `json:"new_size" binding:"required,min=1"`
	}

	var req ExpandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid request body"))
		return
	}

	volume, err := h.service.ExpandVolume(volumeID, req.NewSize, userId)
	if err != nil {
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to expand volume"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusAccepted, "Storage_Expansion_Cycles_Started", volume)
}
```

`ExpandRequest` is defined locally, right inside the handler function — this is a Go idiom for a request shape that's only ever used in exactly one place; there's no need to make it a shared type in the `domain` package if nothing else references it. `binding:"required,min=1"` means Gin rejects the request outright if `new_size` is missing or non-positive, before the handler's own logic ever runs.

`202 Accepted` reflects the same asynchronous pattern used for volume creation and snapshotting — the client is told the operation has *started*, not that it has *finished*, and should check the volume's `status` field afterward to know when it's actually done.

## The service function: validate, then hand off to a goroutine

```go
func (s *VolumeService) ExpandVolume(volumeID int, newSize int, userId string) (*domain.Volume, error) {
	volume, err := s.repo.FindByID(volumeID)
	if err != nil || volume == nil {
		return nil, fmt.Errorf("volume not found: %d", volumeID)
	}

	if newSize <= volume.Size {
		return nil, fmt.Errorf("new size must be greater than current size (%d GB)", volume.Size)
	}

	go func() {
		... // the actual resize work
	}()

	return volume, nil
}
```

Only two checks happen synchronously, before the function returns: does the volume exist, and is the requested size actually larger than the current one (volumes can only grow, never shrink). Everything else — figuring out where the volume's data lives, moving it if necessary, actually resizing it, and pushing the result back to S3 — happens inside a `go func() { ... }()`, a goroutine that keeps running after `ExpandVolume` has already returned to its caller.

## Path 1: the volume is detached

```go
if volume.AttachedTo == "" {
	bestHosts, err := s.hostRepo.GetBestHosts(1)
	volume.Status = domain.VolumeStatusExpanding
	s.repo.Update(volume)

	if err != nil {
		s.repo.UpdateVolumeStatus(volume.ID, domain.VolumeStatusExpanding)
		return
	}
	...
	host := bestHosts[0]

	presignedURL, err := s.publisher.FetchAgentPresignedURL(userId, "version1", volume.LatestReference, volume.SHA256)
	...
	storagePath, _ := volume.GetStoragePath()
	destinationDir := filepath.Dir(storagePath)
	presignedURL = fmt.Sprintf("%s%s", s.assetServiceBaseURL, presignedURL)

	if err := s.DownloadVolumeFileViaAgent(host.IP, presignedURL, destinationDir, volume.VolumeName); err != nil {
		...
	}
```

A detached volume has no host of its own — its data lives only in S3 — so the first step is the same as detached snapshotting: pick a host with capacity (`GetBestHosts`), get a presigned download URL for the volume's current S3-backed data, and have that host's agent download it. This is the identical download mechanism described in the [attach documentation](../volumes-attaching-detaching-volumes), just used here to stage the file somewhere temporarily rather than to permanently attach it anywhere.

```go
	checksum, err := s.ResizeImageViaAgent(host.IP, storagePath, newSize)
	...
```

With the file now sitting on the staging host, `ResizeImageViaAgent` asks that host's agent to run the actual resize — under the hood, this is a `qemu-img resize` call against the downloaded qcow2 file, growing its virtual size to `newSize` gigabytes. This is safe to do directly on the file precisely *because* the volume is detached — nothing is reading from or writing to it concurrently, so there's no risk of corrupting a disk something else has open.

```go
	timestamp := time.Now().Format("2006-01-02T15-04-05")
	s3Key := fmt.Sprintf("snapshots/%s-%s.qcow2", volume.VolumeName, timestamp)

	volumeSnapshotData := domain.CreatePresignedURLRequest{ ... }
	presignedPath, err := s.publisher.PublishCreateUploadURL(volumeSnapshotData)
	...
	fullUrl := fmt.Sprintf("%s%s", s.assetServiceBaseURL, presignedPath)

	if err := s.UploadAndDeleteFileViaAgent(host.IP, storagePath, fullUrl, storagePath); err != nil {
		...
	}

	volume.Size = newSize
	volume.SHA256 = checksum
	volume.LatestReference = s3Key
	volume.Status = domain.VolumeStatusAvailable
	s.repo.Update(volume)
}
```

Once resized, the file is checksummed (the checksum comes back from `ResizeImageViaAgent` itself), uploaded to S3 under a fresh key, and the local staged copy is cleaned up. On success, three fields on the volume record change together: `Size` reflects the new capacity, `SHA256` and `LatestReference` are updated to point at this newly resized file — exactly mirroring how a [detach](../volumes-attaching-detaching-volumes) updates these same two fields whenever a volume's data changes.

## Path 2: the volume is attached

```go
if volume.AttachedTo != "" {
	instance, err := s.instanceRepo.FindByID(volume.AttachedTo)
	...
	host, err := s.hostRepo.GetByID(instance.HostID)
	...
	storagePath, err := volume.GetStoragePath()
	...

	volume.Status = domain.VolumeStatusExpanding
	s.repo.Update(volume)

	if err := s.libvirtClient.ResizeAttachedVolume(
		host.IP, host.SSHUser, host.SSHPrivateKey,
		instance.VMName, storagePath, newSize,
	); err != nil {
		...
	}

	volume.Size = newSize
	volume.Status = domain.VolumeStatusAttached
	s.repo.Update(volume)
}
```

This path is much simpler than the detached one, precisely because nothing needs to move anywhere — the volume's file is already on the correct host, already attached to the correct VM. The instance and host are resolved purely to know which vm_host and which domain to direct the libvirt call at.

### The libvirt live resize call

```go
func (l *LibvirtClient) ResizeAttachedVolume(
	remoteHostIP, remoteHostUser, remoteHostKey string,
	vmName string,
	volumePath string,
	newSizeGB int,
) error {
	conn, cleanup, err := l.getConnection(remoteHostIP, remoteHostUser, remoteHostKey)
	...
	domain, err := conn.LookupDomainByName(vmName)
	...

	newSizeBytes := uint64(newSizeGB) * 1024 * 1024 * 1024

	if err := domain.BlockResize(volumePath, newSizeBytes, libvirt.DOMAIN_BLOCK_RESIZE_BYTES); err != nil {
		return fmt.Errorf("failed to resize attached volume %s: %w", volumePath, err)
	}

	return nil
}
```

`newSizeGB * 1024 * 1024 * 1024` converts gigabytes into raw bytes — `BlockResize`'s unit depends on which flag is passed, and using `DOMAIN_BLOCK_RESIZE_BYTES` makes the call unambiguous: the second argument is always interpreted as an exact byte count, rather than needing to remember whether some other call expects kibibytes.

`BlockResize` is libvirt's purpose-built API for growing (or shrinking, though this system never does that) a disk that's actively attached to a running VM. Unlike a plain `qemu-img resize` — which would be unsafe to run directly against a file a running VM has open — `BlockResize` coordinates the change through QEMU itself, which knows the disk is in use and can safely extend it live, with the guest's virtio driver picking up the new size without any downtime.

**An important limitation worth calling out explicitly: `BlockResize` only grows the block device itself — it does not resize the filesystem living on it.** After this call succeeds, the guest OS sees a bigger disk, but any filesystem already created on it (ext4, xfs, etc.) still reports its old size until something inside the guest explicitly grows it too — typically `growpart` to extend the partition table (if one exists) followed by `resize2fs` (ext4) or `xfs_growfs` (xfs) to grow the filesystem into the newly available space. This is a manual step for the user to perform inside their instance after an expand completes; it isn't something this API does on their behalf.

## Related documentation

- [Attaching a Volume](../volumes-attaching-detaching-volumes)
- [Detaching a Volume](../volumes-attaching-detaching-volumes)
- [Creating Volume Snapshots](../volumes-attaching-detaching-volumes)
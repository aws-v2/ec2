# Detaching a Volume: Request Flow, Libvirt Internals & S3 Backup

This document walks through everything that happens between a client calling `POST /volumes/:id/detach` and the volume ending up safely backed up in S3 with its local copy removed from the host — mirroring the [Attaching a Volume](../volumes-attaching-detaching-volumes) document, but for the reverse direction.

## The request, end to end

```
Client
  │  POST /volumes/42/detach
  ▼
VolumeHandler.DetachVolume        (HTTP layer)
  │
  ▼
VolumeService.DetachVolume        (business logic — validates, orchestrates)
  │  1. Load volume + the instance it's attached to
  │  2. Refuse to detach a running root volume
  │  3. Mark volume "detaching"
  │  4. Ask libvirt to unplug the disk from the running VM
  │  5. Checksum the now-unattached file
  │  6. If the data changed since the last backup, re-upload it to S3
  │     and record the new checksum/S3 key
  │  7. Mark volume "available", clear its attachment info
  │  8. Delete the now-backed-up local copy from the vm_host
  ▼
Volume is now fully detached, its data safely in S3, and no longer
taking up disk space on any host.
```

## The HTTP handler

```go
func (h *VolumeHandler) DetachVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	userId := c.GetString("userID")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		log.Printf("[Handler:DetachVolume] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}

	volume, err := h.service.DetachVolume(volumeID, userId)
	if err != nil {
		log.Printf("[Handler:DetachVolume] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to detach volume"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Volume detached successfully", volume)
}
```

This follows the exact same pattern as `AttachVolume`'s handler: parse the volume ID from the URL, read the authenticated caller's ID from Gin's context, delegate everything else to the service layer, and translate the result into the standard `{code, message, data}` response envelope described in the attach documentation. There's no request body here — unlike attach, detach needs no additional information beyond which volume to detach.

## The service function

```go
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
```

Same validation-first pattern as attach: the volume must exist, and it must actually be in the `attached` state — you can't detach a volume that's `available`, `reserved`, or already `detaching`. `volume.AttachedTo` stores the instance ID it's currently plugged into, and that instance record is fetched, since the next steps need to know which VM (`instance.VMName`) to tell libvirt to detach from.

```go
	// Safety Check: Root volume safety
	if volume.DevicePath == "/dev/vda" || volume.DevicePath == "/dev/sda1" {
		if instance.Status == domain.StatusRunning {
			return nil, fmt.Errorf("cannot detach root volume while instance is running")
		}
	}
```

A guard against a genuinely destructive mistake: `/dev/vda` and `/dev/sda1` are the conventional device paths for a VM's **boot disk** — the disk containing its operating system. Detaching that from a running instance would be equivalent to yanking the hard drive out of a computer while it's running; the instance would almost certainly crash or hang immediately. This check refuses the operation outright if the instance is currently running and the volume being detached looks like a root disk.

```go
	volume.Status = domain.VolumeStatusDetaching
	s.repo.Update(volume)

	err = s.libvirtClient.DetachVolume(instance.VMName, volume.DevicePath)
	if err != nil {
		volume.Status = domain.VolumeStatusAttached
		s.repo.Update(volume)
		return nil, fmt.Errorf("failed to detach volume: %w", err)
	}
```

Same pattern as attach: mark the transitional state (`detaching`) and persist it immediately, so no other operation can act on this volume mid-flight. If the actual libvirt detach call fails, the status rolls back to `attached` — the volume genuinely never left the VM, so the database should keep reflecting that.

## The libvirt detach call

```go
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
```

This mirrors the attach call almost exactly, in reverse. `LookupDomainByName` gets a handle to the running VM. `devicePath[5:]` strips the leading `"/dev/"` from something like `/dev/vdc`, leaving just `vdc` — the same device name libvirt uses internally to identify the disk slot.

Notice the detach XML is **much smaller** than the attach XML — it only needs `<target dev='vdc' bus='virtio'/>`, with no `<driver>`, `<source>`, or `<serial>` elements at all. This is intentional and important to understand: **libvirt's detach XML doesn't describe a new disk — it's a lookup key** used to identify *which already-attached disk* to remove. Libvirt matches this fragment against the VM's current device list (found via the target `dev` and bus) and, once it finds the matching device, removes that entire device regardless of what its other properties (source file, format, serial) actually were. You don't need to restate the file path or format to detach it — you only need enough information to unambiguously identify *which* disk you mean.

`DetachDeviceFlags` takes the same two flags as attach, with the same meaning in reverse: `DOMAIN_DEVICE_MODIFY_LIVE` removes it from the running VM immediately, and `DOMAIN_DEVICE_MODIFY_CONFIG` also removes it from the VM's persisted configuration, so it doesn't reappear the next time the VM boots.

## After the disk is unplugged: checksumming and backing up to S3

This is the part specific to how this system manages volume storage, and it's worth understanding why it exists at all: **once a volume is detached, its data no longer needs to occupy space on any vm_host.** The volume's data is only required to live on a host while something is actively using it. So immediately after a successful detach, the service checks whether the volume's contents changed while it was attached, and if so, pushes the updated data back to S3 — the same place a fresh volume's data is served from the next time it's attached anywhere.

```go
	storagePath, volPathErr := volume.GetStoragePath()
	if volPathErr != nil {
		return nil, fmt.Errorf("failed to get volume path: %w", volPathErr)
	}

	checksum, err := CalculateSHA256File(storagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get volume checksum: %w", err)
	}
```

`GetStoragePath()` gives the local path to the qcow2 file that was just unplugged from the VM (it's still sitting on the vm_host's disk at this point — detaching a device doesn't delete its backing file, it only disconnects it from the VM). `CalculateSHA256File` computes a fresh hash of that file's current contents.

```go
	if checksum != volume.SHA256 {
		log.Printf("Checksum calculated: %v", checksum)
		volume.SHA256 = checksum
```

`volume.SHA256` holds the checksum from the **last time this volume's data was known to be backed up** — either from its original creation, or from a previous detach cycle. Comparing the freshly computed checksum against this recorded value answers a simple but important question: **did anything actually get written to this disk while it was attached?** If the VM only read from the volume and never wrote to it, the checksum will be identical, and there's no need to re-upload unchanged data. If the two differ, the guest wrote *something* — new files, modified files, filesystem metadata changes from normal use — and that new state needs to be preserved.

```go
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

		presignedPath, err := s.publisher.PublishCreateUploadURL(volumeData)
		if err != nil {
			log.Printf("failed to get presigned upload url for volume %d: %v", volume.ID, err)
			volume.Status = domain.VolumeStatusDeleting
			s.repo.Update(volume)
			return nil, err
		}

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
```

When a re-upload is needed, a fresh, timestamped S3 object key is generated (`volumes/vol-a1b2c3d4-2026-09-24T14-30-00.qcow2`) rather than reusing the previous key — every detach that actually changed data produces a new object in S3, rather than overwriting the previous one in place. The service then requests a presigned upload URL for that key (the same asset-service pattern used elsewhere for uploads) and pushes the file's bytes to it.

Once the upload succeeds, two fields on the volume record are what actually matter going forward:

- **`volume.SHA256`** is updated to the freshly computed checksum — this becomes the new baseline for the *next* detach's comparison.
- **`volume.LatestReference`** is updated to the new S3 key — this is what the attach flow reads from (`FetchAgentPresignedURL(userId, "version1", volume.LatestReference, volume.SHA256)`, per the attach documentation) the next time this volume needs to be downloaded onto a host.

If the checksum comparison found no change at all, this entire block is skipped — `SHA256` and `LatestReference` are left exactly as they were, since the data already sitting in S3 under the existing reference is still perfectly valid and there's nothing new to preserve.

## Finalizing: clearing attachment state and cleaning up the host

```go
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

	log.Printf("deleted volume %d file at %s on host %s after successful s3 backup", volume.ID, storagePath, host.ID)

	return volume, nil
}
```

With the data now safely represented in S3 (either freshly re-uploaded, or already correct from before), the volume's `AttachedTo` and `DevicePath` are cleared and its status is set back to `available` — this is the point at which the volume becomes reservable or attachable again by anything else. Only after that database update succeeds does the function ask the agent running on the vm_host to delete the local qcow2 file — since the data is durably in S3, the local copy on this particular host is no longer needed and can be reclaimed.

## Related documentation

- [Attaching a Volume](../volumes-attaching-detaching-volumes)
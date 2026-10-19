# Creating Volume Snapshots: Attached vs. Detached Flow

Creating a snapshot means capturing a volume's data at a point in time, independent of the volume itself. This is the largest and most involved operation in the volumes system, because **how** a snapshot is captured depends entirely on whether the volume is currently plugged into a running VM or sitting idle in S3 — these are two fundamentally different problems, solved by two different code paths inside the same function.

## Why two paths at all?

- **An attached volume's authoritative data lives on a running VM's disk**, actively being read from and written to. Capturing it means asking libvirt to make a live copy of that disk *while the VM keeps running*, without pausing it or touching its original file.
- **A detached volume's authoritative data lives in S3**, not on any host at all. There's no running VM and no live disk to copy from — the data first has to be brought onto some host before anything resembling "copying it" is even possible.

Both paths converge on the same outcome: a new qcow2 file representing the volume's data at that moment, checksummed and uploaded to S3 as a `VolumeSnapshot` record. But the road to get there is different enough that they're worth understanding separately.

## The request, end to end

```
Client
  │  POST /volumes/42/snapshots  { "name": "before-migration" }
  ▼
VolumeHandler.CreateVolumeSnapshot
  │
  ▼
VolumeService.CreateVolumeSnapshot
  │
  ├─ Volume is attached ──────────────► live block-copy on the vm_host,
  │                                     via libvirt, while the VM keeps running
  │
  └─ Volume is detached ──────────────► pick a staging host, download the
                                         volume's S3 data onto it, convert
                                         it into a snapshot file there
                │
                ▼
      Either way: checksum the resulting file (via the agent), request a
      presigned S3 upload URL, upload it, record the snapshot as READY
```

## The HTTP handler

```go
func (h *VolumeHandler) CreateVolumeSnapshot(c *gin.Context) {
	userId := c.GetString("userID")
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}

	var req domain.CreateVolumeSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// If body is empty, just use default values
		req = domain.CreateVolumeSnapshotRequest{}
	}

	snapshot, err := h.service.CreateVolumeSnapshot(&volumeID, &req, userId)
	if err != nil {
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to create snapshot"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusAccepted, "Snapshot creation started", snapshot)
}
```

Both `name` and `description` on the request are optional — if the client sends no body at all (or a body that fails to bind), the handler falls back to an empty `CreateVolumeSnapshotRequest{}` rather than rejecting the request. `202 Accepted` signals that snapshot creation is a longer-running operation than a typical request/response — the client gets back a `VolumeSnapshot` record immediately, and should check its `status` field to know when the actual data capture has finished.

## Setting up the snapshot record before any real work happens

```go
snapshot := &domain.VolumeSnapshot{
	VolumeID:    volumeID,
	ID:          uuid.New(),
	Name:        snapshotName,
	Description: req.Description,
	Status:      domain.VolumeSnapshotStatusPending,
	Size:        volume.Size,
	SnapshotPath: nil,
	StorageKey:   nil,
	SHA256:       nil,
}

if err := s.repo.CreateVolumeSnapshot(snapshot); err != nil {
	return nil, fmt.Errorf("failed to save snapshot metadata: %w", err)
}
```

A database row is created for this snapshot **before** any actual data is copied anywhere, with status `PENDING`. This means a client polling `GET /volumes/:id/snapshots` can see a snapshot "in progress" immediately, and if the whole operation later fails partway through, there's already a row to mark `FAILED` rather than the failure vanishing with nothing to show for the attempt.

## Path 1: the volume is attached

```go
if volume.AttachedTo != "" {
	instance, err := s.instanceRepo.FindByID(volume.AttachedTo)
	...
	host, err := s.hostRepo.GetByID(instance.HostID)
	...
	volumePath, err := volume.GetStoragePath()
	...
	snapshotPath := fmt.Sprintf("/var/lib/libvirt/images/snapshot-%s.qcow2", snapshot.ID)

	err = s.libvirtClient.CreateAttachedVolumeSnapshot(
		host.IP, host.SSHUser, host.SSHPrivateKey,
		instance.VMName, volumePath, snapshotPath,
	)
	...
}
```

The instance and host records are resolved so the code knows *which machine* to actually talk to — libvirt operations are always directed at a specific vm_host over the network. `snapshotPath` is a brand-new file path, named using the snapshot's own UUID, distinct from the volume's own file — this is the destination the live copy will be written to.

### The libvirt live block-copy

```go
func (l *LibvirtClient) CreateAttachedVolumeSnapshot(
	remoteHostIP, remoteHostUser, remoteHostKey string,
	vmName, volumePath, snapshotPath string,
) error {
	conn, cleanup, err := l.getConnection(remoteHostIP, remoteHostUser, remoteHostKey)
	...
	domain, err := conn.LookupDomainByName(vmName)
	...

	if info, err := domain.GetBlockJobInfo(volumePath, 0); err == nil && info != nil && info.Type != libvirt.DOMAIN_BLOCK_JOB_TYPE_UNKNOWN {
		log.Printf("found stale block job on %s (type=%v), aborting before retry", volumePath, info.Type)
		if err := domain.BlockJobAbort(volumePath, 0); err != nil {
			return fmt.Errorf("failed to clear stale block job on %s: %w", volumePath, err)
		}
	}

	destXML := fmt.Sprintf(`
		<disk type='file'>
			<driver name='qemu' type='qcow2'/>
			<source file='%s'/>
		</disk>`, snapshotPath)

	if err := domain.BlockCopy(
		volumePath, destXML,
		&libvirt.DomainBlockCopyParameters{},
		libvirt.DOMAIN_BLOCK_COPY_TRANSIENT_JOB,
	); err != nil {
		return fmt.Errorf("failed to create volume snapshot for %s: %w", volumePath, err)
	}
	...
}
```

**Clearing a stale block job first.** Libvirt tracks an active "block job" per disk while a copy is in progress. If a previous attempt failed partway through and left one registered, libvirt will refuse to start a new one on the same disk — this check looks for exactly that situation and cleans it up before proceeding, so a single earlier failure doesn't permanently jam this volume's ability to be snapshotted again.

**`destXML`** describes only the *destination* of the copy — a new file, in qcow2 format, at `snapshotPath`. Unlike the disk-attachment XML used elsewhere, there's no `<target dev>` here, because this disk is never being plugged into any VM — it's purely a file libvirt is told to write a copy into.

**`BlockCopy(volumePath, destXML, ...)`** is libvirt's live disk-copy primitive: while the VM keeps running and keeps using `volumePath` completely normally, QEMU streams that disk's current contents into a brand-new file at `snapshotPath`, byte for byte. The guest experiences no downtime and, if things are done correctly (see below), never even knows the copy happened.

**`DOMAIN_BLOCK_COPY_TRANSIENT_JOB`** tells libvirt not to require special persistent-domain bookkeeping for this job — without it, some libvirt/QEMU version combinations reject the operation against a normally-configured (non-transient) VM with an unhelpful "domain is not transient" error.

```go
	for {
		info, err := domain.GetBlockJobInfo(volumePath, 0)
		if err != nil {
			return fmt.Errorf("failed to get block job status: %w", err)
		}
		if info == nil || info.End == 0 || info.Cur >= info.End {
			break
		}
		time.Sleep(2 * time.Second)
	}

	if err := domain.BlockJobAbort(volumePath, 0); err != nil {
		return fmt.Errorf("failed to finalize block copy for %s: %w", volumePath, err)
	}

	return nil
}
```

`BlockCopy` starts the job and returns immediately — it does not wait for the copy to actually finish. This loop polls `GetBlockJobInfo` every two seconds, comparing how far the copy has progressed (`Cur`) against its total size (`End`), until the copy has fully caught up.

**The final `BlockJobAbort(volumePath, 0)` — with no pivot flag — is the crucial, easy-to-get-wrong step.** Libvirt's block-copy mechanism was originally designed for *live storage migration*: moving a running VM's disk to new storage and then switching the VM over to using the new file (a "pivot"). If this call passed `DOMAIN_BLOCK_JOB_ABORT_PIVOT`, the VM's live disk would be swapped to `snapshotPath`, and the *original* volume file would become the orphaned one — exactly backwards from what a snapshot needs. Passing plain `0` here means "the copy is done, stop tracking the job, but do not touch which file the VM is actually using" — the VM keeps running on its original, untouched disk, and `snapshotPath` is left behind as an independent, complete, standalone copy.

### Checksumming and uploading the attached-volume snapshot

```go
checksum, err := s.CalculateChecksumViaAgent(host.IP, snapshotPath)
...
s3Key := fmt.Sprintf("snapshots/%s", volume.VolumeName)

volumeSnapshotData := domain.CreatePresignedURLRequest{ ... Key: s3Key, Sha256: checksum, ... }

presignedPath, err := s.publisher.PublishCreateUploadURL(volumeSnapshotData)
...
fullUrl := fmt.Sprintf("%s%s", s.assetServiceBaseURL, presignedPath)

if err := s.UploadAndDeleteFileViaAgent(host.IP, snapshotPath, fullUrl, ""); err != nil {
	...
}

snapshot.SHA256 = &checksum
snapshot.StorageKey = &s3Key
snapshot.Status = domain.VolumeSnapshotStatusReady

if err := s.repo.UpdateVolumeSnapshot(snapshot); err != nil {
	...
}

return snapshot, nil
```

`CalculateChecksumViaAgent` hashes the newly created snapshot file — this call goes to the agent running on the vm_host, not to a local file read, because **the control plane itself has no direct filesystem access to any vm_host**; every file operation on a host is done by asking that host's own agent to do it over HTTP. This is the same reason volume downloads and deletions are routed through the agent rather than done directly.

Once a checksum is in hand, the flow is the same upload pattern used throughout this system: request a presigned S3 URL for the target key, then tell the agent to upload the local file to that URL. `UploadAndDeleteFileViaAgent` appears to also remove the local file once the upload succeeds — its last argument here is an empty string, which (based on how it's used differently in the detached path below) looks like an optional *second* file path to also delete after a successful upload. In the attached case, there's nothing else to clean up — the snapshot file itself is deleted by the upload, and the original volume file must obviously be left alone since the VM is still using it.

## Path 2: the volume is detached

```go
bestHosts, err := s.hostRepo.GetBestHosts(1)
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

Since a detached volume has no running VM and no host it's currently associated with, the very first problem is: **there's nothing to copy from yet.** `GetBestHosts(1)` asks the host registry (which tracks CPU/RAM/disk metrics reported by each host's agent) for the single best-suited host to temporarily stage this work on — essentially borrowing capacity on some host, since the volume itself has no host of its own right now.

`FetchAgentPresignedURL` and `DownloadVolumeFileViaAgent` are the exact same download mechanism used when [attaching a volume](../volumes-attaching-detaching-volumes) — the volume's S3-backed data (`volume.LatestReference`, `volume.SHA256`) is fetched down onto the chosen staging host, landing at the volume's normal storage path there.

```go
snapshotPath := fmt.Sprintf("%s/snapshot-%s.qcow2", destinationDir, snapshot.ID)

checksum, err := s.ConvertImageViaAgent(host.IP, storagePath, snapshotPath, "qcow2")
...
```

With the volume's raw data now sitting on the staging host, `ConvertImageViaAgent` produces the actual snapshot file — a copy (or format conversion) of the downloaded volume, written out under the snapshot's own UUID-based filename, distinct from the downloaded original.

```go
timestamp := time.Now().Format("2006-01-02T15-04-05")
s3Key := fmt.Sprintf("snapshots/%s-%s.qcow2", snapshotName, timestamp)

volumeSnapshotData := domain.CreatePresignedURLRequest{ ... }
presignedPath, err := s.publisher.PublishCreateUploadURL(volumeSnapshotData)
...
fullUrl := fmt.Sprintf("%s%s", s.assetServiceBaseURL, presignedPath)

if err := s.UploadAndDeleteFileViaAgent(host.IP, snapshotPath, fullUrl, storagePath); err != nil {
	...
}

s.repo.UpdateVolumeSnapshotStatus(snapshot.ID, domain.VolumeSnapshotStatusReady)

return &domain.VolumeSnapshot{ ... }, nil
```

The same request-a-URL, upload pattern follows. Here, `UploadAndDeleteFileViaAgent`'s fourth argument is `storagePath` — the *downloaded, staged copy* of the original volume — rather than an empty string. This makes sense given the purpose of that argument: once the snapshot file has been safely uploaded to S3, the staged download that produced it is no longer needed either, and both temporary files on the staging host should be cleaned up together.

## Related documentation

- [Attaching a Volume](../volumes-attaching-detaching-volumes)
- [Detaching a Volume](../volumes-attaching-detaching-volumes)
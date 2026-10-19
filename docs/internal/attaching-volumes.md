# Attaching a Volume: Request Flow & Libvirt Internals

This document walks through everything that happens between a client calling `POST /volumes/:id/attach` and a fresh block device showing up inside the guest VM — the HTTP layer, the service logic, the libvirt call that actually plugs the disk in, and the steps needed inside the guest to use it.

## The request, end to end

```
Client
  │  POST /volumes/42/attach  { "instance_id": "i-0123..." }
  ▼
VolumeHandler.AttachVolume        (HTTP layer — parses request, calls service)
  │
  ▼
VolumeService.AttachVolume        (business logic — validates, orchestrates)
  │  1. Load volume + instance + host records from the database
  │  2. Mark volume "attaching"
  │  3. Ask the asset service for a presigned download URL for this
  │     volume's S3-backed data
  │  4. Tell the host's agent to download that file onto the vm_host
  │  5. Ask libvirt to plug the downloaded disk into the running VM
  │  6. Mark volume "attached", record which device it landed on
  ▼
LibvirtClient.AttachVolume        (talks to libvirtd on the vm_host)
  │  Builds a small XML document describing the disk, hands it to
  │  libvirt, which tells QEMU to hot-plug it into the running VM
  ▼
Guest VM
     Kernel detects a new virtio-blk device, exposes it as /dev/vdX
```

## The HTTP handler

```go
func (h *VolumeHandler) AttachVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	userId := c.GetString("userID")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		log.Printf("[Handler:AttachVolume] invalid id: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}
	var req domain.AttachVolumeRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Handler:AttachVolume] Bad request: %v", err)
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid request body"))
		return
	}
	volume, err := h.service.AttachVolume(volumeID, req.InstanceID, userId)
	if err != nil {
		log.Printf("[Handler:AttachVolume] Service call error: %v", err)
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to attach volume"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Volume attached successfully", volume)
}
```

Step by step:

1. **`c.Param("id")`** pulls the `:id` segment out of the URL (`/volumes/42/attach` → `"42"`), as a string. `strconv.Atoi` converts it to an `int`; if the caller sent something non-numeric, this fails immediately with a `400 Bad Request` before anything else runs.
2. **`c.GetString("userID")`** reads a value out of Gin's per-request context — not out of the request body or URL. This value was placed there earlier in the request's life by an authentication middleware, after it validated the caller's JWT/API key. By the time this handler runs, "who is making this request" has already been decided elsewhere; the handler just reads the answer.
3. **`c.ShouldBindJSON(&req)`** reads the request body, expects it to be JSON, and tries to populate `req` (a `domain.AttachVolumeRequest`) from it:
   ```go
   type AttachVolumeRequest struct {
       InstanceID string `json:"instance_id" binding:"required"`
   }
   ```
   The `binding:"required"` tag means Gin's validator rejects the request with an error if `instance_id` is missing or empty — you never reach the handler's own logic with an empty `InstanceID`.

4. **`h.service.AttachVolume(...)`** is where all the real work happens — the handler itself does no business logic, it just translates HTTP concepts (URL params, JSON body, status codes) into a plain Go function call, and translates the result back into an HTTP response.
5. Whatever the service returns is handed to `vpcpkg.RespondSucces` or `vpcpkg.RespondError`.

## The response envelope

Every response from this API — success or failure — has the same shape:

```go
type apiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}
```

```go
func RespondSucces(c *gin.Context, statusCode int, message string, data interface{}) {
	c.Writer.Header().Set("Version", "1.0.0")
	c.Writer.Header().Set("Content-Type", "application/json")

	c.JSON(statusCode, apiResponse{
		Code:    statusCode,
		Message: message,
		Data:    data,
	})
}
```

- **`Code`** duplicates the HTTP status code inside the JSON body itself. This is redundant with the actual HTTP status line, but it means a client that only inspects the parsed JSON (rather than the transport-level status) can still branch on success/failure without a second lookup.
- **`Message`** is a short, human-readable string — `"Volume attached successfully"` on success, or the error text (`err.Error()`) on failure. It's meant for logs, debugging, or a toast/notification in a UI — not for programmatic branching (use `Code` for that).
- **`Data`** is `interface{}` — Go's "this can be absolutely anything" type. On success it holds whatever the handler passed in (here, the updated `*domain.Volume`); `json:"data,omitempty"` means if `Data` is `nil` (as it always is on the error path, since `RespondError` never sets it), the `data` key is dropped from the JSON entirely rather than appearing as `"data": null`.

`RespondError` is the same shape, minus `Data`:

```go
func RespondError(c *gin.Context, statusCode int, err error) {
	c.Writer.Header().Set("Version", "1.0.0")
	c.Writer.Header().Set("Content-Type", "application/json")

	c.JSON(statusCode, apiResponse{
		Code:    statusCode,
		Message: err.Error(),
	})
	return
}
```

A successful attach therefore returns:

```json
{
  "code": 200,
  "message": "Volume attached successfully",
  "data": {
    "id": 42,
    "volume_name": "vol-a1b2c3d4",
    "status": "attached",
    "attached_to": "i-0123456789abcdef0",
    "device_path": "/dev/vde",
    ...
  }
}
```

## The service function

```go
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
	...
```

**Validation first, before anything is touched.** Three lookups happen before a single byte moves anywhere: the volume must exist and be `available` (not already attached, not mid-reservation, etc.), the target instance must exist, and that instance's physical host must be resolvable. Any of these failing returns an error immediately — nothing has been mutated yet, so there's nothing to undo.

```go
	volume.Status = domain.VolumeStatusAttaching
	log.Printf("volume %d status -> attaching (sha256=%s, latest_reference=%s)", volume.ID, volume.SHA256, volume.LatestReference)
	s.repo.Update(volume)
```

The volume is marked `attaching` and persisted immediately. This matters for concurrency: once this write lands, any other request trying to attach or reserve the same volume will fail the `VolumeStatusAvailable` check above — the volume is now "claimed" for the duration of this operation.

```go
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
```

This is the key architectural fact about how volumes work here: **a detached volume's data lives in S3, not on any vm_host.** `LatestReference` is the S3 object key, `SHA256` is its checksum. To attach the volume anywhere, its data first has to come *back* from S3 onto the specific host the target instance runs on. This call asks the asset service for a temporary, signed URL that grants download access to that specific object without needing long-lived S3 credentials on this service.

Notice the failure-handling pattern here, repeated at every step of this function: if something goes wrong, the volume's status is rolled back to `available` and the error is returned. This makes the operation safely retryable — a client that gets an error can just call attach again, rather than the volume being stuck in `attaching` forever.

```go
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
```

The control plane doesn't download anything itself — it sends an HTTP request to a small agent process running *on the target vm_host* `DownloadVolumeFileViaAgent`  (over the internal Tailscale network) telling it "download this URL, save it here." This indirection exists because the control plane (where this Go service runs) typically doesn't have direct filesystem access to every vm_host — the agent does the actual download locally on the machine that needs the file.

```go
	storagePath, volPathErr := volume.GetStoragePath()
	...
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
```

With the file now sitting on the correct vm_host, `ListAttachedDevices` asks libvirt what devices the target VM already has, and `getNextDevicePathFromLibvirt` picks the next free slot (`/dev/vdb`, `/dev/vdc`, etc.) that isn't already taken. `LibvirtClient.AttachVolume` — covered in detail below — is the call that actually hot-plugs the disk into the running VM. On success, the database is updated to reflect the new `attached` state; if *that* final write fails, the code attempts to detach the disk it just attached, so the VM's real state and the database's recorded state don't drift apart.

## What libvirt actually is

If you've never touched libvirt before: it's a management layer that sits between your code and a hypervisor (in this setup, QEMU/KVM). Instead of your program shelling out to `qemu-system-x86_64` with a huge list of command-line flags, or hand-editing running VM state, you talk to `libvirtd` — a daemon running on the host — through its API, and it translates your requests into the right low-level operations against QEMU.

**libvirt-go** (imported here as `libvirtgo`) is a set of Go bindings for that API — Go function calls that, under the hood, make calls into libvirt's C library. You get regular Go types and error handling instead of writing C or shelling out to the `virsh` command-line tool.

A few libvirt vocabulary terms used directly in this code:

- **Domain** — libvirt's word for a virtual machine. `conn.LookupDomainByName(vmName)` finds the VM by its name and gives you a `Domain` object you can operate on. This is not related to the `domain` package alias used elsewhere in this codebase for your own Go types — it's an unfortunate but common naming collision between "libvirt's concept of a VM" and "your application's domain/business-logic layer."
- **Connection** — `l.conn` is an open connection to a specific `libvirtd` daemon (potentially on a remote host, since your setup manages multiple vm_hosts over the network). Everything you do with libvirt goes through a connection.
- **Device hot-plug** — attaching a disk to a *running* VM without rebooting it. This is what `AttachDeviceFlags` does; the guest OS detects the new hardware live, the same way plugging in a USB drive on a real machine doesn't require a restart.

## Deep dive: building the disk XML

```go
diskXML := fmt.Sprintf(`
    <disk type='file' device='disk'>
      <driver name='qemu' type='%s'/>
      <source file='%s'/>
      <target dev='%s' bus='virtio'/>
      <serial>%s</serial>
    </disk>`, format, volumePath, deviceName, volumeSerial)
```

This is the part worth understanding really well, because it's the actual instruction libvirt acts on — everything before this point in the code was just gathering the information needed to build this one string.

Libvirt's entire configuration language is XML. Rather than a Go struct or a set of function parameters, you describe *what you want* as an XML document, and libvirt parses and validates it. This particular fragment describes one disk device. Going element by element:

**`<disk type='file' device='disk'>`**
The outer element. `type='file'` tells libvirt the disk's backing storage is a regular file on the filesystem (as opposed to, say, a raw block device, an iSCSI target, or a network-attached volume — libvirt supports several storage backends, and this is a plain qcow2 file sitting in `/var/lib/libvirt/images/`). `device='disk'` says this is a generic disk, as opposed to `device='cdrom'` (which you're already using elsewhere for cloud-init ISOs) or `device='floppy'`.

**`<driver name='qemu' type='%s'/>`**
`name='qemu'` tells libvirt which driver backend handles this disk — QEMU's own disk emulation, standard for a QEMU/KVM setup. `type='%s'` is filled in with `format` (`"qcow2"` in your case) — this tells QEMU what *file format* the backing file is in, so it knows how to interpret it (qcow2 supports things like copy-on-write and internal snapshots; QEMU also supports `raw`, `vmdk`, and others, but this system standardizes on qcow2 everywhere).

**`<source file='%s'/>`**
The actual path to the backing file on the vm_host's filesystem — filled in with `volumePath`, e.g. `/var/lib/libvirt/images/vol-a1b2c3d4.qcow2`. This is the file the agent just finished downloading from S3 moments earlier in the flow.

**`<target dev='%s' bus='virtio'/>`**
This is where it gets interesting, and where the bug discussed below comes from. `dev='%s'` (filled with `deviceName`, e.g. `vdc`) is libvirt's **label** for this device slot — it's what shows up if you inspect the VM's configuration with `virsh dumpxml`. `bus='virtio'` specifies the virtual hardware bus the disk is attached to. Virtio is a paravirtualized device standard designed specifically for virtual machines — it's much faster than emulating a real physical disk controller, because the guest OS's virtio driver and the hypervisor cooperate directly rather than the hypervisor faking real hardware behavior in software. This is why your devices show up as `/dev/vdX` inside the guest (the `v` prefix is the Linux kernel's convention for virtio block devices) rather than `/dev/sdX` (SCSI/SATA-style naming).

**Important nuance**: `target dev='vdc'` is a *hint/label* for libvirt's own bookkeeping — it is **not a binding contract with the guest kernel** about what device name will actually appear inside the VM. The guest kernel independently assigns device names based on the order it discovers virtio devices on the PCI bus at boot/hotplug time. Usually these line up, but they can diverge — which is exactly the bug covered in the last section of this document.

**`<serial>%s</serial>`**
This is the fix for that divergence. A serial number is a stable identifier for the disk that survives regardless of what device name (`vdb`, `vdc`, etc.) the guest kernel happens to assign it. Once set, the guest exposes a symlink at `/dev/disk/by-id/virtio-<serial>` that always points at the correct device — so software inside the guest (or a human running commands) can reference the disk by something that never changes, instead of a name that can shift between attach/detach cycles.

The full XML, with real values substituted, looks like this:

```xml
<disk type='file' device='disk'>
  <driver name='qemu' type='qcow2'/>
  <source file='/var/lib/libvirt/images/vol-a1b2c3d4.qcow2'/>
  <target dev='vdc' bus='virtio'/>
  <serial>my-volume-name</serial>
</disk>
```

Once built, this string is handed to libvirt:

```go
err = domain.AttachDeviceFlags(diskXML, libvirtgo.DOMAIN_DEVICE_MODIFY_CONFIG|libvirtgo.DOMAIN_DEVICE_MODIFY_LIVE)
```

`AttachDeviceFlags` sends this XML to libvirtd and asks it to attach the described device to the given domain. The two flags, combined with a bitwise OR, tell libvirt to apply this change in **two places at once**:

- **`DOMAIN_DEVICE_MODIFY_LIVE`** — apply it to the *running* VM right now, live, no reboot needed.
- **`DOMAIN_DEVICE_MODIFY_CONFIG`** — also persist it into the VM's saved configuration, so the disk is still there the next time the VM boots (without this flag alone, a live-only change would vanish on the next restart).

Using both together is what makes this a durable hot-plug: the guest sees the disk immediately, and it survives a reboot.

## Inside the guest: making the new disk usable

Once `AttachDeviceFlags` succeeds, the guest kernel discovers the new virtio-blk device. From here, using it is a normal Linux disk-setup process:

**1. Confirm the device appeared**

```bash
lsblk
```

Look for a new entry with no mount point — this is the volume that just showed up. If this is the volume's first-ever attachment, it appears with no filesystem; if it was previously formatted, used, detached, and is now being reattached, it already has one.

**2. Check whether it needs formatting**

```bash
sudo blkid /dev/vdc
```

No output means no filesystem exists yet — this is a brand-new volume. Output like `UUID="..." TYPE="ext4"` means it already has data; skip formatting entirely, or you'll destroy it.

**3. Format it (first attachment only)**

```bash
sudo mkfs.ext4 /dev/vdc
```

Never run this on a volume that's been used before.

**4. Create a mount point and mount it**

```bash
sudo mkdir -p /mnt/data
sudo mount /dev/vdc /mnt/data
```

Verify:

```bash
df -h /mnt/data
```

**5. Make it durable across reboots**

Get the filesystem's UUID (stable, unlike the device name — see the next section for why this matters):

```bash
sudo blkid /dev/vdc
```

Add it to `/etc/fstab`:

```
UUID=f52aa09d-642d-4da8-bf88-9c7fad989144  /mnt/data  ext4  defaults,nofail  0  2
```

`nofail` means the VM still boots normally even if this volume happens to be detached at boot time, rather than hanging waiting for a device that isn't there.

## The device-naming bug: why `/dev/vdb` becomes `/dev/vdc`, and why the data is never actually lost

This is the exact issue flagged earlier in this document's XML deep dive: **`target dev='vdb'` in the libvirt XML is a label for libvirt's bookkeeping — the guest kernel decides the real device name independently**, based on the order it enumerates virtio devices on the PCI bus. That enumeration order depends on which PCI slot the device ends up attached to, which libvirt allocates automatically unless a specific slot is pinned.

In practice: attach a volume the first time, and it might come up as `/dev/vdb` inside the guest. Detach it. Reattach it later — libvirt might ask for the same `target dev='vdb'` again, but if the PCI slot assignment differs this time around (which can happen even with no code changes, purely due to how libvirt allocates addresses), the guest kernel can enumerate that same disk as `/dev/vdc` instead.

**The data is completely unaffected by this.** The qcow2 file on the vm_host — the actual bytes making up the filesystem and its contents — was never touched by the device-naming change. What actually goes wrong is almost always downstream of this: a `/etc/fstab` entry, a mount script, or a human typing a command that hardcodes `/dev/vdb` will now point at a device that doesn't exist (or that's a *different* disk entirely), producing symptoms that look like data loss — "my filesystem is empty," "mount says wrong fs type," "directory not writable" — when the real cause is a stale reference to the wrong device name.

**How this is addressed going forward:**

1. **The `<serial>` element** in the disk XML gives the guest a stable path at `/dev/disk/by-id/virtio-<serial>`, independent of whatever `/dev/vdX` name the kernel happens to assign.
2. **Mounting by filesystem UUID** (via `/etc/fstab`, as shown above) is the more universal fix — a UUID is generated when the filesystem is created and never changes, regardless of which device node or PCI slot it ends up on.

The practical rule: **never treat `/dev/vdX` as a permanent identifier for a volume.** Always resolve the correct device via its UUID (`blkid`) or its serial-based `/dev/disk/by-id/` path before trusting that you're reading from or writing to the right disk — especially important right after any detach/reattach cycle.
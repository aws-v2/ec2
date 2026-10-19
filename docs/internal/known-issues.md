# Known Issues

This page aggregates every bug, race condition, inconsistency, and design gap surfaced while implementing and documenting the volumes feature. It's organized by category rather than by endpoint, since several of the most important issues here are *patterns* that show up in more than one place — fixing the pattern once matters more than patching each occurrence individually.

Each item is marked **Open** or **Fixed**. Severity is a rough guide to how urgently each should be addressed, not a strict SLA.

## Concurrency & race conditions

### `ExpandVolume` returns a pointer that a goroutine mutates concurrently
**Severity: High · Status: Open**

`VolumeService.ExpandVolume` starts a background goroutine and immediately returns the same `*domain.Volume` pointer to its caller, which the HTTP handler then serializes to JSON. The goroutine subsequently mutates that same struct's fields (`Status`, `Size`, `SHA256`, `LatestReference`) with no synchronization. This is an unsynchronized concurrent read/write on shared memory — confirmed under Go's race detector (`go test -race` or `GORACE=...`), not just a theoretical concern. In practice, the client's `202` response can show stale or in-progress data, and the underlying memory access is undefined behavior.

**Fix direction:** either don't return the same pointer the goroutine will mutate (return a snapshot/copy taken before the goroutine starts), or make the async contract explicit and have the client always re-fetch via `GetVolume` rather than trusting the initial response body's mutable fields.

### Reservations use read-then-write instead of an atomic conditional update
**Severity: High · Status: Open**

Both `ReserveVolume` and `ReleaseVolumeReservation` do `FindByID` → check `Status` → `Update`, rather than a single atomic `UPDATE ... WHERE status = 'available'`. Two concurrent reserve requests for the same volume can both pass the status check before either writes, and both "succeed" — the second silently overwrites the first's `ReservedTo`/`ReservedAt`. This defeats the entire purpose of reservations, which exist specifically to prevent this kind of double-claim.

**Fix direction:** a conditional `UPDATE` that only succeeds if the row still matches the expected prior status, checking rows-affected to detect a lost race, matching the same pattern already needed for safe concurrent `AttachVolume` calls.

## Status / state-machine bugs

### Wrong recovery status on host-lookup failure during expand
**Severity: High · Status: Open**

In `ExpandVolume`'s detached branch, if `GetBestHosts` fails, the code calls `s.repo.UpdateVolumeStatus(volume.ID, domain.VolumeStatusExpanding)` — re-setting the *same* in-progress status instead of `VolumeStatusExpandingFailed`. The volume is left permanently reporting "expanding," since nothing will ever revisit or retry it. This exact class of bug was also caught and fixed once already in an earlier iteration of this same function, and reappeared.

### Misleading status on detach's S3-upload failure
**Severity: Medium · Status: Open**

If the S3 re-upload during `DetachVolume` fails (presigned URL request or the upload itself), the code sets `volume.Status = domain.VolumeStatusDeleting` — but nothing is being deleted. At this point the volume has already been physically detached from the VM, yet `AttachedTo` and `DevicePath` are never cleared in this failure path, leaving the database in an inconsistent state: status implies deletion, but attachment fields still reference the old instance/device.

### `ExpandVolume`'s two branches are independent `if`s, not `if/else`
**Severity: Low · Status: Open**

```go
if volume.AttachedTo == "" { ... }
if volume.AttachedTo != "" { ... }
```

Harmless today only because nothing inside the first branch ever sets `AttachedTo`. A future edit that touches `AttachedTo` inside the detached branch would silently cause both branches to execute for the same request.

## Data integrity & storage bugs

### Snapshot S3 key collision on attached volumes
**Severity: High · Status: Open**

`CreateVolumeSnapshot`'s attached-volume path builds its S3 key as `fmt.Sprintf("snapshots/%s", volume.VolumeName)` — using only the volume's name, with no snapshot ID or timestamp. Every snapshot ever taken of the same volume overwrites the previous one in S3. A user who takes two snapshots and expects two independent restore points actually has one, silently.

**Fix direction:** `fmt.Sprintf("snapshots/%s.qcow2", snapshot.ID)`, matching the pattern already used correctly elsewhere (e.g. the detached-volume snapshot path includes a timestamp, though see the next item).

### Detached-volume snapshots never persist their checksum or storage key
**Severity: High · Status: Open**

The detached branch of `CreateVolumeSnapshot` calls `s.repo.UpdateVolumeSnapshotStatus(snapshot.ID, Ready)` — which only updates the `status` column — but never calls `s.repo.UpdateVolumeSnapshot(snapshot)` with the computed `SHA256`/`StorageKey` set. The snapshot ends up marked `READY` in the database with `sha256` and `storage_key` left `NULL`, even though the actual file is sitting correctly in S3. Anything that later tries to resolve this snapshot's location (e.g. `CreateVolumeFromSnapshot`) would find nothing usable.

### Expand's resized volume gets uploaded into the snapshots bucket
**Severity: Medium · Status: Open**

`ExpandVolume`'s detached path uploads the resized volume using `AssetType: "ec2-volume-snapshot"`, `BucketName: "ec2-volume-snapshots"`, and an S3 key under `snapshots/...` — even though this is the volume's own canonical data being updated, not a snapshot. This looks like a copy-paste carryover from the snapshot code. It will likely still function (since `LatestReference` just points at wherever it landed), but it pollutes the snapshots bucket with objects that were never created via `CreateVolumeSnapshot`, which would corrupt any future "list all snapshots" or bucket-lifecycle logic that assumes everything there came from an explicit snapshot request.

### Detach can leak a local file with no cleanup path
**Severity: Medium · Status: Open**

In `DetachVolume`, the host lookup (`s.hostRepo.GetByID(instance.HostID)`) happens *after* the volume has already been marked `available` and its S3 backup completed. If that lookup fails, the function returns an error, but the database already says the volume is `available` — nothing will ever revisit this volume to clean up its now-orphaned local qcow2 file on that host. It sits there permanently, since `available` volumes have no further trigger that would delete it.

### Local file is deleted after detach with no verification a backup actually exists
**Severity: Medium · Status: Open**

When a detach's checksum comparison finds no change, the upload step is (correctly) skipped — but the function still unconditionally deletes the local file at the end, trusting that `volume.LatestReference` already points at a valid S3 object. If that assumption is ever wrong (for instance, a silently-failed upload during original volume creation), this is a real data-loss path with no safety net checking before the delete.

### `CreateVolumeFromSnapshot` doesn't persist the volume it creates
**Severity: Critical · Status: Open**

```go
func (s *VolumeService) CreateVolumeFromSnapshot(req *domain.CreateVolumeFromSnapshotRequest, userId string) (*domain.Volume, error) {
	...
	volume := &domain.Volume{ ... }
	return volume, nil
}
```

This builds a `Volume` struct and returns it directly — there is no `s.repo.Create(volume)` call, and no async provisioning is kicked off (contrast with plain `CreateVolume`, which calls `go s.createVolumeAsync(volume, userId)`). The handler responds `201 Created` describing a volume that was never written to the database and has no backing file anywhere. This endpoint does not currently work.

### `CreateVolumeFromSnapshot`'s handler reads the wrong context key
**Severity: High · Status: Open**

```go
userId := c.GetString("userId")  // lowercase "d"
```

Every other handler in this file reads `c.GetString("userID")` (capital `D`), matching the key the auth middleware actually sets. This means `userId` here is always an empty string, silently — no error, just wrong data flowing into whatever eventually uses it.

## Repository / query bugs

### Incomplete column selection in `FindAll` and `FindByInstanceID`
**Severity: Medium · Status: Open**

`VolumeRepository.FindByInstanceID`'s query selects only `attached_to`, `device_path`, `created_at`, `updated_at` — omitting `id`, `volume_name`, `size`, `format`, `type`, `availability_zone`, and `status`. `FindAll` similarly omits `device_path`. Any caller of either function receives `domain.Volume` structs with those fields silently zero-valued, not reflecting the actual row. Only `FindByID` currently selects the full column set correctly.

### Possible orphaned `name` column
**Severity: Low · Status: Open — needs confirmation**

`VolumeRepository.Create` inserts into both `volume_name` and `name`. It's unclear whether `name` is a legacy duplicate that should be removed in a migration, or intentionally distinct from `volume_name` (e.g. a user-facing display name vs. an internal identifier). Worth confirming before extending this table further.

## Code duplication & copy-paste drift

Several of the bugs above trace back to the same root cause: **the same multi-step sequence (get presigned URL → build full URL → call the agent → handle failure by setting a status and returning) is hand-copied across `CreateVolume`, `DetachVolume`, `ExpandVolume`, and both branches of `CreateVolumeSnapshot`**, with small per-copy variations in bucket name, key format, and asset type. When one copy is updated, the others aren't automatically kept in sync, which is exactly how the snapshot key collision and the snapshot-bucket mislabeling in expand happened.

Other duplicated patterns worth consolidating:

- **`strconv.Atoi(c.Param("id"))` plus its error handling** is repeated, nearly verbatim, at the top of every single volume handler.
- **The status-rollback-on-failure pattern** (`volume.Status = X; s.repo.Update(volume)`) is copy-pasted at nearly every failure branch across every service function, which is exactly how the wrong-status-on-failure bugs above happened — a shared helper (`s.failVolume(volume, status)`) would make it impossible to typo the wrong status enum in one call site while getting it right everywhere else.
- **Device-name extraction** (`devicePath[5:]`) is duplicated in both `LibvirtClient.AttachVolume` and `LibvirtClient.DetachVolume`, in both cases with no bounds check — an empty or malformed `devicePath` would panic rather than return a clear error.
- **Resolving an instance's host** (`instanceRepo.FindByID` then `hostRepo.GetByID`) is repeated across attach, detach, expand, and snapshot creation — a single `resolveInstanceHost(instanceID) (*Instance, *Host, error)` helper would remove four copies of the same two-step lookup and its error handling.
- **Agent-helper return signatures have drifted inconsistently.** `ResizeImageViaAgent` and `ConvertImageViaAgent` are both called as if they return `(string, error)` (a checksum plus an error) in some call sites, while their originally documented design returns only `error`. Whether these were deliberately extended to compute and return a checksum, or whether this is a mismatch that doesn't actually compile, needs to be confirmed — and either way, the agent-calling functions would benefit from a shared naming/signature convention so this kind of drift is caught immediately rather than discovered while reading the code closely.
- **Copy-pasted log messages that no longer match their context** — e.g. `ExpandVolume` logging `"staging detached volume %s snapshot on host %s"` (this is an expand operation, not a snapshot), and more than one upload success log reading `"uploaded to sn3 successfully"` instead of `"s3"`. Harmless on their own, but a reliable sign of copy-paste-without-review, and a hint that the surrounding logic was copied at the same time.

## Safety & validation gaps

### `volume.Name` used as the libvirt disk serial
**Severity: Medium · Status: Open**

`AttachVolume`'s libvirt call passes the volume's free-form, user-supplied display name (`volume.Name`) as the disk's `<serial>` value. This field has no guarantee of being short or free of spaces/special characters, while virtio's serial field has historically had practical length limits in some QEMU/kernel combinations. `volume.VolumeName` or `volume.InternalName` (the system-generated, guaranteed-safe identifiers) would be a safer choice.

### Ignored error from `ListAttachedDevices`
**Severity: Low · Status: Open**

```go
attachedDevices, err := s.libvirtClient.ListAttachedDevices(instance.VMName)
devicePath := getNextDevicePathFromLibvirt(attachedDevices)
```

`err` is never checked. If this call fails, `attachedDevices` could be incomplete or nil, and `getNextDevicePathFromLibvirt` would proceed on bad data — potentially handing back a device path that's already in use, surfacing as a confusing failure later inside the actual attach call instead of a clear error here.

### No automatic reservation expiry
**Severity: Low · Status: Open — design gap, not a bug**

`ReservationExpiresAt` is faithfully recorded on every reservation, but nothing in the system reads it back. There is no scheduled sweep and no check-on-read that treats an expired reservation as implicitly released. Right now `expires_at` is purely informational — a caller must notice a reservation is stale and explicitly release it.

## Resolved issues (kept for history)

These were caught and fixed during this project and are included here so the reasoning behind current behavior isn't lost.

- **Markdown frontmatter parser corrupted any doc containing a table.** `parseMarkdownWithFrontmatter` split on the substring `"---"` anywhere in the file, so a markdown table separator (`|---|---|`) was misread as a frontmatter delimiter, silently discarding everything before the table. Fixed by requiring `---` to appear as a standalone line, and only at the very start of the file.
- **Device names (`/dev/vdb` → `/dev/vdc`) are not stable across detach/reattach cycles.** This is expected libvirt/QEMU behavior (PCI enumeration order, not the `target dev` XML label, determines the guest's device name) rather than a bug to fix — addressed by adding a `<serial>` element to the disk XML and by always mounting via filesystem UUID rather than a raw device path. See [Attaching a Volume](../volumes-attaching-detaching-volumes) for the full explanation.
- **`ReservedAt`/`ReservationExpiresAt` as plain `time.Time` failed to scan `NULL` values.** Fixed by changing both fields to `sql.NullTime`, which correctly represents "not currently reserved" as a genuine `NULL` rather than a zero-value timestamp.
- **The `Volume.Status` field being a plain `string` allowed silent typos against the typed `VolumeStatus` constants.** Left as an open recommendation to convert `Status` to the named `domain.VolumeStatus` type throughout, for compile-time checking — not yet done everywhere.
- **The API gateway's docs endpoints never forwarded a caller's real role**, because the `isPublicEndpoint` check short-circuited before any credential was validated. Fixed by introducing a distinct "optionally authenticated" path for docs routes, which validates a credential if one is present and only falls back to an anonymous role when none is given or it's invalid.
- **A controller living inside the gateway application bypassed the gateway's own auth filter chain entirely**, because Spring's annotated-controller dispatch takes priority over Gateway's route-matching for any path with a direct `@GetMapping` match. Fixed by having that controller resolve the caller's role inline rather than relying on a header the filter chain was never going to set for it. This is a pattern worth remembering for any future controller added directly to the gateway app.
- **The gateway's docs-manifest poller failed to deserialize any field**, because the polled service wrapped its response in a `{code, message, data}` envelope that the poller's DTO didn't account for. Fixed by deserializing into a generic envelope wrapper and unwrapping `data` before caching.

## Cross-references

- [Overview & Architecture](../volumes-architecture)
- [Attaching a Volume](../volumes-attaching-detaching-volumes)
- [Detaching a Volume](../volumes-attaching-detaching-volumes)
- [Creating Volume Snapshots](../volumes-attaching-detaching-volumes)
- [Expanding a Volume](../volumes-attaching-detaching-volumes)
- [Tags & Reservations](../volumes-attaching-detaching-volumes)
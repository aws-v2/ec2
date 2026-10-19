# Tags & Reservations

Tags and reservations are the two simplest operations in the volumes API — neither touches libvirt, an agent, or S3. Both work purely against the database, which is why their handlers and service functions are much shorter than attach, detach, snapshot, or expand.

## Reservations

### What a reservation is for

A reservation lets you hold a specific `available` volume for a specific instance or workflow **before** the actual attach happens. This matters when provisioning is a multi-step process — for example, deciding which volume to use, then creating an instance, then attaching the volume to it — and you need to guarantee nothing else claims that volume in the time between those steps.

A reservation records:
- **who** it's held for (`ReservedTo`)
- **when** it was made (`ReservedAt`)
- **when it expires** (`ReservationExpiresAt`)

Only an `available` volume can be reserved, and a reservation can only be released while the volume is actually `reserved`.

### Reserve a volume

```http
POST /api/v1/ec2/volumes/{id}/reserve
Content-Type: application/json

{
  "reserved_to": "i-0123456789abcdef0",
  "expires_at": "2026-09-24T18:00:00Z"
}
```

```go
func (h *VolumeHandler) ReserveVolume(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}
	var req domain.ReservationRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid request body"))
		return
	}

	volume, err := h.service.ReserveVolume(volumeID, req)
	if err != nil {
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to reserve volume"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Storage_Reservation_Created", volume)
}
```

```go
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
```

The logic is a straightforward guard-then-write: confirm the volume is `available`, then set four fields at once — `Status` becomes `reserved`, `ReservedTo` records the caller-supplied identifier for who this is held for, and `ReservedAt`/`ReservationExpiresAt` are stamped with the current time and the caller-supplied expiry. `ReservedAt` and `ReservationExpiresAt` are `sql.NullTime` rather than plain `time.Time`, since an unreserved volume needs these columns to be genuinely `NULL` in the database, not a zero-value timestamp that could be mistaken for a real date.

Returns `200 OK` with the volume now in `reserved` status.

### Release a reservation

```http
POST /api/v1/ec2/volumes/{id}/release-reservation
```

```go
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
```

The mirror image of reserving: confirm the volume is actually `reserved` (with a slightly more specific error message if it's already `attached` — since that's a common and informative reason a release might fail), then clear all three reservation fields back to their empty/`NULL` state and set `Status` back to `available`. Once this returns, the volume is immediately eligible to be reserved or attached by anyone.

Returns `200 OK` with the volume back in `available` status.

## Tags

Tags are simple key/value pairs attached to a volume for your own organization, cost tracking, or automation — for example, `environment=production` or `owner=team-a`. Unlike reservations, tags don't affect a volume's `status` or any other behavior in the system at all; they're pure metadata.

### List a volume's tags

```http
GET /api/v1/ec2/volumes/{id}/tags
```

```go
func (h *VolumeHandler) ListTags(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}
	tags, err := h.service.GetTags(volumeID)
	if err != nil {
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to get tags"))
		return
	}

	if tags == nil {
		tags = []*domain.VolumeTag{}
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Tags retrieved successfully", tags)
}
```

Returns `200 OK` with an array of tags:

```json
{
  "code": 200,
  "message": "Tags retrieved successfully",
  "data": [
    { "key": "environment", "value": "production" },
    { "key": "owner", "value": "team-a" }
  ]
}
```

The `if tags == nil { tags = []*domain.VolumeTag{} }` check exists because a database query with no matching rows in Go typically returns a `nil` slice, not an empty one. Without this check, a volume with zero tags would serialize to `"data": null` in the response instead of `"data": []` — a needless inconsistency for API clients, who would otherwise have to treat "no tags" and "an error occurred" as looking similar in the response shape.

### Add or update a tag

```http
POST /api/v1/ec2/volumes/{id}/tags
Content-Type: application/json

{
  "key": "environment",
  "value": "production"
}
```

```go
func (h *VolumeHandler) AddTag(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}
	var tag domain.VolumeTag
	if err := c.ShouldBindJSON(&tag); err != nil {
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid tag data"))
		return
	}

	if err := h.service.AddOrUpdateTag(volumeID, &tag); err != nil {
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to add or update tag"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Tag added/updated successfully", nil)
}
```

If a tag with the given `key` already exists on the volume, its `value` is overwritten — there's no separate "update" endpoint, since add and update are the same operation from the database's point of view (an upsert). The response carries no `data` — if you need to see the resulting tag list, call `GET /volumes/:id/tags` afterward.

### Remove a tag

```http
DELETE /api/v1/ec2/volumes/{id}/tags/{key}
```

```go
func (h *VolumeHandler) DeleteTag(c *gin.Context) {
	volumeIDStr := c.Param("id")
	volumeID, err := strconv.Atoi(volumeIDStr)
	if err != nil {
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("invalid volume id"))
		return
	}
	key := c.Param("key")
	if key == "" {
		vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("tag key is required"))
		return
	}

	if err := h.service.DeleteTag(volumeID, key); err != nil {
		vpcpkg.RespondError(c, http.StatusInternalServerError, fmt.Errorf("failed to delete tag"))
		return
	}

	vpcpkg.RespondSucces(c, http.StatusOK, "Tag deleted successfully", nil)
}
```

`key` comes from the URL path itself (`:key`), not the request body — since a delete only needs to identify *which* tag to remove, there's nothing else to submit.

### The service layer for tags is intentionally thin

```go
func (s *VolumeService) GetTags(volumeID int) ([]*domain.VolumeTag, error) {
	return s.repo.GetTags(volumeID)
}

func (s *VolumeService) AddOrUpdateTag(volumeID int, tag *domain.VolumeTag) error {
	return s.repo.AddOrUpdateTag(volumeID, tag)
}

func (s *VolumeService) DeleteTag(volumeID int, key string) error {
	return s.repo.DeleteTag(volumeID, key)
}
```

Unlike attach, detach, snapshot, or expand — which all involve validation, status transitions, and coordination with libvirt/agents/S3 — tag operations have no business rules to enforce beyond "does this volume exist," which isn't even checked here (it's implicitly enforced by the database's foreign key on `volume_tags.volume_id`, which rejects a tag for a volume ID that doesn't exist). These functions exist purely to keep the service layer as the single entry point every handler goes through, rather than handlers calling the repository directly — a consistent layering rule even when, as here, there's nothing to add on top of the repository call.

## Related documentation

- [Overview & Architecture](../volumes-architecture)
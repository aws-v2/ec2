# Volumes: Overview & Architecture

Volumes are persistent block storage devices you can create, attach to an instance, detach, resize, snapshot, tag, and reserve — independently of any single instance's lifecycle.

## What is a volume?

A volume behaves like a virtual hard disk. You create it with a given size and format, then attach it to a running instance to use it as a regular block device. A volume exists independently of any instance — detaching it does not delete its data, and it can later be reattached to the same or a different instance.

## Volume lifecycle

A volume moves through the following states over its lifetime:

| Status | Meaning |
|---|---|
| `creating` | The volume record has been created and its backing storage is being provisioned. |
| `available` | The volume exists and is not currently attached to any instance. |
| `reserved` | The volume has been held for a specific instance/workflow ahead of an attach — see [Reservations](#reservations) below. |
| `attaching` | An attach operation is in progress. |
| `attached` | The volume is attached to an instance and in use. |
| `detaching` | A detach operation is in progress. |
| `expanding` | A resize operation is in progress. |

A volume can only be **attached** while it is `available`, can only be **reserved** while it is `available`, and can only be **expanded** to a larger size than it currently has.

## Core concepts

### Snapshots

A snapshot captures the state of a volume's data at a point in time, independent of the volume itself. You can create a snapshot of a volume whether or not it's currently attached to an instance, and later use that snapshot as the source for a brand-new volume — for example, to give several instances the same starting data.

Creating a snapshot from an *attached* volume captures its data live, without needing to detach it or interrupt the instance using it.

### Reservations

A reservation lets you hold a specific `available` volume for a specific instance or workflow before the actual attach happens — useful when a multi-step provisioning process (create instance → attach volume → boot) needs to guarantee nothing else claims that volume in between steps.

A reservation records who it's held for and when it expires. If the reserving workflow never completes, the reservation can be released, returning the volume to `available` for anyone to use.

### Tags

Tags are key/value pairs you attach to a volume for your own organization, cost tracking, or automation — for example, `environment=production` or `owner=team-a`.

### Expanding a volume

A volume's size can be increased after creation, letting you grow storage without recreating the volume or migrating data. Volumes cannot be shrunk. Expansion works whether the volume is currently attached or detached.

## API Reference

All endpoints below require authentication and are scoped under `/api/v1/ec2/volumes`.

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/volumes` | Create a new volume. |
| `GET` | `/volumes` | List all volumes belonging to your account. |
| `GET` | `/volumes/:id` | Get details of a specific volume. |
| `POST` | `/volumes/:id/reserve` | Reserve an available volume for later use. |
| `POST` | `/volumes/:id/release-reservation` | Release a volume's reservation, returning it to `available`. |
| `POST` | `/volumes/:id/attach` | Attach a volume to an instance. |
| `POST` | `/volumes/:id/detach` | Detach a volume from its current instance. |
| `POST` | `/volumes/:id/expand` | Increase a volume's size. |
| `DELETE` | `/volumes/:id` | Delete a volume. |
| `POST` | `/volumes/:id/snapshots` | Create a snapshot of a volume. |
| `GET` | `/volumes/:id/snapshots` | List snapshots for a volume. |
| `DELETE` | `/volumes/:id/snapshot` | Delete a volume snapshot. |
| `POST` | `/volumes/from-snapshot` | Create a new volume from an existing, ready snapshot. |
| `GET` | `/volumes/:id/tags` | List a volume's tags. |
| `POST` | `/volumes/:id/tags` | Add or update a tag on a volume. |
| `DELETE` | `/volumes/:id/tags/:key` | Remove a tag from a volume. |

### Create a volume

```http
POST /api/v1/ec2/volumes
Content-Type: application/json

{
  "name": "my-data-volume",
  "size": 20,
  "type": "gp3",
  "availability_zone": "us-east-1a"
}
```

Returns `202 Accepted` with the created volume record. Volume creation is asynchronous — check the volume's `status` field to see when it transitions from `creating` to `available`.

### Reserve a volume

```http
POST /api/v1/ec2/volumes/{id}/reserve
Content-Type: application/json

{
  "reserved_to": "i-0123456789abcdef0",
  "expires_at": "2026-09-24T18:00:00Z"
}
```

Returns `200 OK` with the volume in `reserved` status. Only an `available` volume can be reserved. If the reservation is never confirmed by an attach, it should be explicitly released — a reservation that's simply forgotten will eventually expire based on `expires_at`, but releasing it explicitly frees the volume immediately.

### Release a reservation

```http
POST /api/v1/ec2/volumes/{id}/release-reservation
```

Returns `200 OK` with the volume back in `available` status. Fails if the volume isn't currently `reserved` — for example, if it's already been attached, or was never reserved to begin with.

### Attach a volume to an instance

```http
POST /api/v1/ec2/volumes/{id}/attach
Content-Type: application/json

{
  "instance_id": "i-0123456789abcdef0"
}
```

Returns `200 OK` with the updated volume record once the attach operation completes, with `status` set to `attached`.

> **Note:** The device name your instance's operating system assigns to the volume (e.g. `/dev/vdb`) is not guaranteed to remain the same across future detach/reattach cycles. See [Attaching & Detaching Volumes](../volumes-attaching-detaching-volumes) for how to reliably locate and mount the volume inside your instance.

### Detach a volume

```http
POST /api/v1/ec2/volumes/{id}/detach
```

Returns `200 OK` with the volume back in the `available` state. Always unmount the volume from inside your instance before detaching, to avoid data corruption from in-flight writes.

### Expand a volume

```http
POST /api/v1/ec2/volumes/{id}/expand
Content-Type: application/json

{
  "new_size": 50
}
```

`new_size` must be larger than the volume's current size. Returns `202 Accepted` — expansion is asynchronous and works whether the volume is attached or detached. After expansion completes, you'll typically still need to grow the filesystem inside the instance to make use of the new space.

### Create a snapshot

```http
POST /api/v1/ec2/volumes/{id}/snapshots
Content-Type: application/json

{
  "name": "before-migration",
  "description": "Snapshot before running the data migration"
}
```

Both fields are optional — if `name` is omitted, one is generated automatically. Returns `202 Accepted` with the created snapshot record. Snapshot creation works the same way whether the volume is currently attached or not.

### Create a volume from a snapshot

```http
POST /api/v1/ec2/volumes/from-snapshot
Content-Type: application/json

{
  "snapshot_id": "b3f1c2e4-...",
  "volume_name": "restored-data-volume"
}
```

Returns `201 Created` with the new volume, pre-populated with the snapshot's data. The source snapshot must be in `READY` status.

### Tag a volume

```http
POST /api/v1/ec2/volumes/{id}/tags
Content-Type: application/json

{
  "key": "environment",
  "value": "production"
}
```

Adding a tag with a key that already exists on the volume updates its value.

### Deleting a volume

```http
DELETE /api/v1/ec2/volumes/{id}?force=true
```

By default, deletion is blocked if the volume is currently `attached`. Passing `force=true` overrides this: the volume is detached from its instance first, then its storage and database record are removed. Use `force` carefully — it can result in an instance losing access to a disk it still expects to be present.

## Related documentation

- [Attaching & Detaching Volumes](../volumes-attaching-detaching-volumes)
- [Snapshots](../volumes-attaching-detaching-volumes)
- [Expanding Volumes](../volumes-attaching-detaching-volumes)
- [Tags & Reservations](../volumes-attaching-detaching-volumes)
- [Known Issues](../volumes-attaching-detaching-volumes)
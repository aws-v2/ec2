# EBS Volumes

Elastic Block Storage (EBS) volumes provide persistent, high-performance block-level storage for EC2 instances. Unlike root disks, EBS volumes persist independently of instance lifecycle — they survive instance stops, restarts, and can be detached and reattached.

## Key Concepts

| Term | Description |
|---|---|
| **Volume** | A virtual disk that can be attached to an EC2 instance. |
| **Attachment** | The process of linking a volume to an instance via a device path (e.g., `/dev/vdb`). |
| **Expansion** | Increasing the size of an existing volume without data loss. |
| **Snapshot** | A backup copy of a volume at a point in time. |

## Volume States

| State | Description |
|---|---|
| `available` | Newly created, not attached to any instance. |
| `attached` | Actively mounted to an instance. |
| `detached` | Was attached, now unattached (same as available). |
| `reserved` | Logically reserved for a scheduled attachment. |
| `deleting` | Volume is being decommissioned. |

## Storage Types

| Type | Description | Recommended For |
|---|---|---|
| **SSD (gp2)** | General purpose SSD, low latency | Web servers, app data |
| **SSD (io1)** | Provisioned IOPS SSD, high throughput | Databases, high-traffic apps |
| **HDD (st1)** | Throughput-optimized HDD | Sequential workloads, log storage |
| **HDD (sc1)** | Cold storage HDD, low cost | Archive, infrequently accessed data |

## API Reference

### Create a Volume

```bash
POST /api/v1/ec2/volumes
{
  "name": "data-volume-01",
  "size": 50,
  "volume_type": "gp2"
}
```

### List Volumes

```bash
GET /api/v1/ec2/volumes
```

### Get Volume Details

```bash
GET /api/v1/ec2/volumes/:id
```

### Attach a Volume to an Instance

```bash
POST /api/v1/ec2/volumes/:id/attach
{
  "instance_id": "vm-abc123"
}
```

### Detach a Volume

```bash
POST /api/v1/ec2/volumes/:id/detach
```

### Expand a Volume

```bash
POST /api/v1/ec2/volumes/:id/expand
{
  "new_size": 100
}
```

> ⚠️ After expansion, you must resize the filesystem on the instance:
> ```bash
> sudo resize2fs /dev/vdb
> ```

### Delete a Volume

```bash
DELETE /api/v1/ec2/volumes/:id
```

Use `?force=true` to force-delete an attached volume:

```bash
DELETE /api/v1/ec2/volumes/:id?force=true
```

## Volume Tags

Organize your volumes with custom metadata tags:

```bash
# Add a tag
POST /api/v1/ec2/volumes/:id/tags
{ "key": "environment", "value": "production" }

# List tags
GET /api/v1/ec2/volumes/:id/tags

# Delete a tag
DELETE /api/v1/ec2/volumes/:id/tags/:key
```

## Best Practices

- **Snapshot before expanding** — Always take a snapshot before resizing a volume.
- **Use the right type** — SSD for IOPS-sensitive apps, HDD for sequential bulk access.
- **Label volumes clearly** — Use tags like `project`, `environment`, and `owner`.
- **Avoid force-delete** — Always detach cleanly before deleting to prevent data corruption.

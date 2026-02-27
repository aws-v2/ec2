# Snapshots

Snapshots are point-in-time backups of EC2 instances or EBS volumes. They allow you to restore a previous state, create new instances from a baseline, or migrate data between environments.

## Snapshot Types

| Type | Source | Use Case |
|---|---|---|
| **Instance Snapshot** | A running EC2 instance (via libvirt domain snapshot) | Full VM state backup, rollback |
| **Volume Snapshot** | An attached or detached EBS volume | Data backup, cloning |

## Instance Snapshots

Instance snapshots capture the full state of a virtual machine including memory, CPU state, and all attached disks at the time of the snapshot.

### Create an Instance Snapshot

```bash
POST /api/v1/ec2/instances/:id/snapshot
{
  "name": "pre-upgrade-snapshot",
  "description": "Backup before OS upgrade"
}
```

### List Instance Snapshots

```bash
GET /api/v1/ec2/snapshots?instance_id=<instance_id>
```

### Get a Specific Snapshot

```bash
GET /api/v1/ec2/snapshots/:id
```

### Delete an Instance Snapshot

```bash
DELETE /api/v1/ec2/snapshots/:id
```

## Volume Snapshots

Volume snapshots capture the block-level data of an EBS volume. They are useful for creating backups of persistent data stores independent of the instance.

### Create a Volume Snapshot

```bash
POST /api/v1/ec2/volumes/:id/snapshots
{
  "name": "pg-data-backup",
  "description": "PostgreSQL data directory backup"
}
```

### List Volume Snapshots

```bash
GET /api/v1/ec2/volumes/:id/snapshots
```

### Delete a Volume Snapshot

```bash
DELETE /api/v1/ec2/volumes/:id/snapshot
```

> The `:id` in the delete path is the **snapshot ID**, not the volume ID.

## Snapshot States

| State | Description |
|---|---|
| `pending` | Snapshot creation has been triggered. Data is being captured. |
| `ready` | Snapshot is complete and available for use. |
| `failed` | Snapshot creation encountered an error. |

## Snapshot vs Backup

| Aspect | Snapshot | Full Backup |
|---|---|---|
| Speed | Incremental, fast | Full copy, slower |
| Storage | Metadata + delta | Full disk image |
| Restore | Fast (revert domain) | Slower (image copy) |
| Data included | VM or volume state | Disk data only |

## Best Practices

- **Before updates** — Always snapshot before OS upgrades or major config changes.
- **Name meaningfully** — Use descriptive names like `pre-migration-2026-02-27`.
- **Schedule regularly** — Set up external cron jobs or automation to snapshot critical instances daily.
- **Clean up old snapshots** — Snapshots consume metadata storage. Delete obsolete ones.

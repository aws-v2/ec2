---
title: "Volumes Overview & API Reference"
description: "Comprehensive public guide to persistent block storage volumes in EC2 compute, including attachment lifecycle, snapshots, volume expansion, tags, and API reference."
icon: "hard-drive"
tags: ["volumes", "storage", "block-store", "snapshots", "attaching"]
---

# Persistent Block Storage Volumes

## Overview

A **Volume** is a persistent, raw block storage device that exists independently of any individual virtual machine instance. Unlike instance root disks, volumes persist data beyond the lifecycle of an instance. You can create a volume, attach it to a running instance, format and mount it as a filesystem, write data, cleanly unmount and detach it, and later reattach it to the same or a different instance without data loss.

Volumes are ideal for workloads requiring durable, high-performance, re-attachable storage such as databases, application data repositories, media storage, and stateful application state.

---

## Core Characteristics & Concepts

1. **Independent Lifecycle**: Volumes exist as standalone resources. Deleting or terminating an instance does not automatically delete attached persistent volumes unless explicitly configured.
2. **Dynamic Device Naming**: Inside guest virtual machines, attached volumes are presented as virtio block devices (for example, `/dev/vdb`, `/dev/vdc`). Because device names can vary across attachments depending on host PCI enumeration order, **always identify and mount volumes using their filesystem UUID** or persistent disk identifiers rather than relying on `/dev/vdX` device paths.
3. **Point-in-Time Snapshots**: You can create point-in-time snapshots of any volume. Snapshots are stored durably and can be used to create new volumes or back up critical state.
4. **Online & Offline Volume Expansion**: Volumes can be expanded to larger capacities while attached to a running instance or while idle, without requiring volume recreation.

---

## Volume States & Lifecycle

A volume transitions through specific statuses during its lifecycle:

```
                  ┌──────────────┐
                  │  available   │ ◄──────────┐
                  └──────┬───────┘            │
                         │                    │
          ┌──────────────┼──────────────┐     │
          ▼              ▼              ▼     │
   ┌────────────┐ ┌────────────┐ ┌──────────┐ │
   │ attaching  │ │  reserved  │ │expanding │ │
   └─────┬──────┘ └─────┬──────┘ └────┬─────┘ │
         │              │             │       │
         ▼              │             └───────┤
   ┌────────────┐       │                     │
   │  attached  │◄──────┘                     │
   └─────┬──────┘                             │
         │                                    │
         ▼                                    │
   ┌────────────┐                             │
   │ detaching  │─────────────────────────────┘
   └────────────┘
```

* **`available`**: The volume is idle and ready to be attached to an instance or reserved.
* **`attaching`**: An attachment operation is currently in progress.
* **`attached`**: The volume is actively connected to an instance and available as a block device inside the guest OS.
* **`detaching`**: A detachment operation is in progress, synchronizing and backing up volume state.
* **`reserved`**: The volume is temporarily locked for a specific target instance or multi-step provisioning workflow.
* **`expanding`**: A volume storage expansion operation is in progress.

---

## Attaching and Mounting Volumes: Guest Guide

### Step 1: Attach via API
Call the attach endpoint to connect an available volume to a target instance:

```http
POST /api/v1/ec2/volumes/42/attach
Content-Type: application/json

{
  "instance_id": "i-0123456789abcdef0"
}
```

Upon success, the volume status becomes `attached`, and the response indicates the initial target device path (e.g., `/dev/vdc`).

---

### Step 2: Identify the Block Device inside the Guest
SSH into your instance and list all block devices:

```bash
lsblk
```

Example output:

```
NAME    MAJ:MIN RM  SIZE RO TYPE MOUNTPOINTS
vda     252:0    0   20G  0 disk
├─vda1  252:1    0 19.9G  0 part /
└─vda15 252:15   0  106M  0 part /boot/efi
vdc     252:32   0   50G  0 disk
```

In this output, `vdc` is the newly attached 50GB block device.

---

### Step 3: Check for Existing Filesystem Signature
Before formatting, check if the volume already contains a filesystem:

```bash
sudo blkid /dev/vdc
```

* **If `blkid` returns a UUID and TYPE** (e.g., `UUID="f52aa09d-642d-4da8-bf88-9c7fad989144" TYPE="ext4"`), **the volume already has data**. Do NOT format it, or you will erase existing data! Skip to Step 5.
* **If `blkid` returns no output**, the volume is new and unformatted. Proceed to Step 4.

---

### Step 4: Format New Volume (First Use Only)
Format a brand-new volume with an `ext4` filesystem:

```bash
sudo mkfs.ext4 /dev/vdc
```

> **Warning:** Run `mkfs` only once when initializing a brand-new volume. Never run `mkfs` on a volume that already contains data.

---

### Step 5: Retrieve Filesystem UUID & Mount Volume
Obtain the volume's stable UUID:

```bash
sudo blkid /dev/vdc
```

Output:
```
/dev/vdc: UUID="f52aa09d-642d-4da8-bf88-9c7fad989144" BLOCK_SIZE="4096" TYPE="ext4"
```

Create a mount point directory and mount using the UUID:

```bash
sudo mkdir -p /mnt/data
sudo mount UUID=f52aa09d-642d-4da8-bf88-9c7fad989144 /mnt/data
```

Verify the mount:

```bash
df -h /mnt/data
```

---

### Step 6: Configure Persistent Boot Mount (`/etc/fstab`)
To ensure the volume mounts automatically on system boot (or after reattachment), add an entry to `/etc/fstab` referencing the UUID:

```
UUID=f52aa09d-642d-4da8-bf88-9c7fad989144  /mnt/data  ext4  defaults,nofail  0  2
```

> **Important:** Always include the `nofail` option. This allows the guest operating system to boot normally even if the volume is detached or unattached during boot.

Test your `/etc/fstab` configuration without rebooting:

```bash
sudo mount -a
```

---

### Step 7: Safe Detachment Flow
Before calling the API to detach a volume, always unmount the filesystem inside the guest to flush write buffers and prevent data corruption:

```bash
sudo umount /mnt/data
```

Then invoke the API detach endpoint:

```http
POST /api/v1/ec2/volumes/42/detach
```

---

## Volume Snapshots

Volume snapshots capture a point-in-time copy of a volume's block state.

### Creating a Snapshot
You can take a snapshot of either an **attached** volume (live capture while the instance is running) or a **detached** volume:

```http
POST /api/v1/ec2/volumes/42/snapshots
Content-Type: application/json

{
  "name": "daily-backup-2026-10-01",
  "description": "Pre-upgrade database snapshot"
}
```

Response (`202 Accepted`):
```json
{
  "code": 202,
  "message": "Snapshot creation started",
  "data": {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "volume_id": 42,
    "name": "daily-backup-2026-10-01",
    "status": "PENDING",
    "size": 50
  }
}
```

### Listing & Polling Snapshots
Poll the volume's snapshots to check when the status transitions from `PENDING` to `READY`:

```http
GET /api/v1/ec2/volumes/42/snapshots
```

---

## Volume Expansion

Volumes can be grown to a larger size without losing data. Volumes can only be expanded, never shrunk.

### Requesting Expansion
To expand a volume, submit a request specifying the desired `new_size` in GB:

```http
POST /api/v1/ec2/volumes/42/expand
Content-Type: application/json

{
  "new_size": 100
}
```

Response (`202 Accepted`):
```json
{
  "code": 202,
  "message": "Storage_Expansion_Cycles_Started",
  "data": {
    "id": 42,
    "size": 50,
    "status": "expanding"
  }
}
```

### Guest Operating System Filesystem Resizing
Once the volume status returns to `attached` (or `available`), the physical block device has grown, but the filesystem inside the guest must be resized to utilize the new space.

#### For `ext4` Filesystems:
```bash
sudo resize2fs /dev/vdc
```

#### For `XFS` Filesystems:
```bash
sudo xfs_growfs /mnt/data
```

Verify expanded capacity with `df -h /mnt/data`.

---

## Reservations & Tags

### Volume Reservations
If your automation provisions instances in multiple steps, you can reserve an available volume to prevent other operations from claiming it before attachment:

```http
POST /api/v1/ec2/volumes/42/reserve
Content-Type: application/json

{
  "reserved_to": "i-0123456789abcdef0",
  "expires_at": "2026-10-01T20:00:00Z"
}
```

To release a reservation manually:
```http
POST /api/v1/ec2/volumes/42/release-reservation
```

### Volume Metadata Tagging
Organize and categorize your volumes using key-value tags:

* **List tags**: `GET /api/v1/ec2/volumes/42/tags`
* **Add or update tag**: `POST /api/v1/ec2/volumes/42/tags` with `{"key": "env", "value": "production"}`
* **Delete tag**: `DELETE /api/v1/ec2/volumes/42/tags/env`

---

## Complete API Reference

| HTTP Method | Route | Description |
|---|---|---|
| `POST` | `/api/v1/ec2/volumes` | Create a new volume |
| `GET` | `/api/v1/ec2/volumes` | List all volumes |
| `GET` | `/api/v1/ec2/volumes/:id` | Get details for a specific volume |
| `POST` | `/api/v1/ec2/volumes/:id/attach` | Attach volume to an instance |
| `POST` | `/api/v1/ec2/volumes/:id/detach` | Detach volume from an instance |
| `POST` | `/api/v1/ec2/volumes/:id/expand` | Expand volume storage capacity |
| `POST` | `/api/v1/ec2/volumes/:id/snapshots` | Create a volume snapshot |
| `GET` | `/api/v1/ec2/volumes/:id/snapshots` | List snapshots for a volume |
| `DELETE` | `/api/v1/ec2/volumes/:id/snapshot` | Delete a volume snapshot |
| `POST` | `/api/v1/ec2/volumes/:id/reserve` | Reserve an available volume |
| `POST` | `/api/v1/ec2/volumes/:id/release-reservation` | Release a volume reservation |
| `GET` | `/api/v1/ec2/volumes/:id/tags` | List metadata tags |
| `POST` | `/api/v1/ec2/volumes/:id/tags` | Add or update a tag |
| `DELETE` | `/api/v1/ec2/volumes/:id/tags/:key` | Remove a metadata tag |
| `DELETE` | `/api/v1/ec2/volumes/:id` | Delete an unattached volume |

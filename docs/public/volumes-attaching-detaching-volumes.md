---
title: "Attaching and Detaching Volumes Guide"
description: "Step-by-step guide for attaching volumes to EC2 instances, block device discovery, UUID mounting, /etc/fstab persistence, and safe detachment."
icon: "link"
tags: ["volumes", "attaching", "detaching", "mounting", "fstab"]
---

# Attaching and Detaching Volumes

This guide explains how to attach a volume to an instance, mount it inside the guest, and safely detach it later without losing data.

## Overview

A volume is a persistent block storage device that exists independently of any instance. You can attach a volume to an instance, use it like a regular disk, detach it, and reattach it later — to the same instance or a different one. Detaching a volume does not delete its data; the data lives on the volume itself, not on the instance.

## Before you begin

You'll need:

- A volume that has already been created and is in the `available` state.
- A running instance to attach it to.
- SSH access to the instance.

## Step 1: Attach the volume

Attach the volume to your instance using the console, CLI, or API (see [API Architecture](../api-architexture) for request details). Once attached, the volume's status changes to `attached`, and it is exposed to the instance as a new block device.

> **Important:** The device name your instance assigns to a newly attached volume (for example, `/dev/vdb`, `/dev/vdc`, `/dev/vdd`) is **not guaranteed to be the same every time you attach it**. This is normal behavior for virtio-based block devices and applies whether this is the volume's first attachment or a later reattachment. Never assume a volume will always come back on the same device path — always identify it by its filesystem UUID instead (see Step 2).

## Step 2: Find the new device inside the instance

SSH into your instance and list the block devices:

```bash
lsblk
```

Example output:

```
NAME    MAJ:MIN RM  SIZE RO TYPE MOUNTPOINTS
vda     252:0    0   10G  0 disk
├─vda1  252:1    0  9.9G  0 part /
└─vda15 252:15   0  106M  0 part /boot/efi
vdc     252:32   0    2G  0 disk
```

Here, `vdc` is the newly attached volume — it's the disk with no partitions and no mount point yet. Compare against the output before you attached the volume if you're unsure which one is new.

## Step 3: Check if the volume already has a filesystem

If this is a **brand-new** volume you're attaching for the first time, it has no filesystem yet and needs to be formatted (Step 4).

If this volume was **previously formatted and used**, it already has a filesystem — do not format it again, or you will destroy your data. Check with:

```bash
sudo blkid /dev/vdc
```

- If this prints something like `UUID="..." TYPE="ext4"`, the volume already has a filesystem. Skip to Step 5.
- If it prints nothing, the volume is unformatted. Continue to Step 4.

## Step 4: Format the volume (first use only)

```bash
sudo mkfs.ext4 /dev/vdc
```

Replace `/dev/vdc` with your actual device name from Step 2.

> **Warning:** Only run this command once per volume, the first time you use it. Running it again on a volume that already has data will erase that data.

## Step 5: Get the volume's UUID

Every filesystem has a UUID that stays constant regardless of which device name (`/dev/vdb`, `/dev/vdc`, etc.) the volume is attached as. Get it with:

```bash
sudo blkid /dev/vdc
```

Example output:

```
/dev/vdc: UUID="f52aa09d-642d-4da8-bf88-9c7fad989144" BLOCK_SIZE="4096" TYPE="ext4"
```

Save this UUID — you'll use it for mounting instead of the device path.

## Step 6: Create a mount point and mount the volume

```bash
sudo mkdir -p /mnt/data
sudo mount UUID=f52aa09d-642d-4da8-bf88-9c7fad989144 /mnt/data
```

Verify it mounted correctly:

```bash
df -h /mnt/data
```

## Step 7: Make the mount persist across reboots

Edit `/etc/fstab` and add a line using the UUID, not the device path:

```
UUID=f52aa09d-642d-4da8-bf88-9c7fad989144  /mnt/data  ext4  defaults,nofail  0  2
```

The `nofail` option matters: if the volume is ever missing when the instance boots (for example, if it was detached), the instance will still boot normally instead of hanging or dropping into a recovery shell.

Test the fstab entry without rebooting:

```bash
sudo mount -a
df -h /mnt/data
```

If this runs with no errors, the entry is correct.

## Detaching a volume

Before detaching, unmount it cleanly from inside the instance to avoid data corruption from in-flight writes:

```bash
sudo umount /mnt/data
```

Then detach the volume using the console, CLI, or API. Once detached, the volume's status changes back to `available`, and it can be attached to another instance (or the same one) later.

## Reattaching a volume

When you reattach a volume, always treat it as if it might appear on a **different device name** than before:

1. Run `lsblk` to find the new device.
2. Run `sudo blkid /dev/vdX` to confirm it's the volume you expect (match the UUID against what you recorded earlier).
3. Mount it using the UUID:
   ```bash
   sudo mount UUID=f52aa09d-642d-4da8-bf88-9c7fad989144 /mnt/data
   ```

Because Step 7 already added a UUID-based `/etc/fstab` entry, in most cases you can simply run:

```bash
sudo mount -a
```

and it will find and mount the correct volume automatically, regardless of which device name it was assigned.

## Common issues

### "Directory is not writable" after reattaching

This usually means `/mnt/data` is still referencing an old, now-nonexistent device from a previous attachment (for example, the mount table still points at `/dev/vdb`, but the volume actually came back as `/dev/vdc`). Fix it by unmounting and remounting using the current device or, preferably, the UUID:

```bash
sudo umount /mnt/data
sudo mount UUID=<your-volume-uuid> /mnt/data
```

### Filesystem appears to be missing after detach/reattach

Your data is very unlikely to actually be gone — it almost always means the wrong device is mounted, or the volume was mistakenly reformatted. Check the actual device list with `lsblk` and confirm the UUID with `blkid` before concluding data is lost.

### I don't remember the volume's UUID

If you have any device that might be the volume, run:

```bash
sudo blkid /dev/vdX
```

for each disk listed by `lsblk` until you find the UUID you're looking for. If you recorded the UUID when you first formatted the volume (recommended), check your own notes or your `/etc/fstab` on the original instance.

## Best practices

- **Always mount by UUID, never by device path.** Device paths (`/dev/vdb`, `/dev/vdc`) can change between attachments.
- **Record the UUID immediately after formatting** a volume, before you forget it.
- **Use `nofail` in `/etc/fstab`** so a missing volume doesn't block your instance from booting.
- **Unmount before detaching** to avoid data corruption.
- **Never format a volume you didn't just create** — always check with `blkid` first.
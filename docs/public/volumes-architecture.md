---
title: "Volumes System Architecture"
description: "High-level architectural overview of the persistent block storage volume subsystem, component interactions, state transitions, and snapshot architecture."
icon: "layers"
tags: ["architecture", "volumes", "storage", "block-device"]
---

# Volumes Subsystem Architecture

## Overview

The **Volumes Subsystem** provides persistent, high-performance block storage for virtual machine instances. The architecture decouples storage management from compute node execution, allowing volumes to exist independently, migrate between compute hosts, scale on demand, and produce durable point-in-time snapshots.

---

## Subsystem Components

```
┌─────────────────────────────────────────────────────────┐
│                    API Gateway / Control Plane          │
└────────────────────────────┬────────────────────────────┘
                             │
            ┌────────────────┴────────────────┐
            ▼                                 ▼
┌────────────────────────┐       ┌────────────────────────┐
│  Volume Orchestration  │       │  Snapshot Orchestration│
└───────────┬────────────┘       └───────────┬────────────┘
            │                                 │
            ├─────────────────────────────────┘
            ▼
┌─────────────────────────────────────────────────────────┐
│                    Storage Abstraction                  │
└───────────┬─────────────────────────────────┬───────────┘
            ▼                                 ▼
┌────────────────────────┐       ┌────────────────────────┐
│ Virtualization Engine  │       │ Durable Object Storage │
│ (Host Block Controller)│       │ (Backup & Snapshot)    │
└────────────────────────┘       └────────────────────────┘
```

1. **API Control Plane**: Validates user HTTP requests, manages authentication and authorization, enforces volume quotas, and handles state persistence.
2. **Volume Orchestration Service**: Orchestrates volume status transitions (`available`, `attaching`, `attached`, `detaching`, `reserved`, `expanding`), coordinates block device hot-plugging, and performs storage expansion.
3. **Snapshot Orchestration Service**: Manages point-in-time data captures for attached and detached volumes, tracking snapshot metadata and completion status.
4. **Virtualization Host Engine**: Manages physical hypervisor block attachments, exposing volumes as paravirtualized virtio block devices (`/dev/vdX`) inside guest virtual machines.
5. **Durable Object Storage**: Stores detached volume backing files and point-in-time volume snapshots off-host for high durability and cross-host mobility.

---

## Key Architectural Principles

### 1. Storage-Compute Decoupling
Volumes are treated as independent first-class resources. When an instance is stopped or terminated, attached persistent volumes automatically detach and return to the `available` state, leaving their data fully preserved in durable storage.

### 2. Live Hot-Plugging & Detaching
Volumes can be attached to or detached from running virtual machines without requiring instance reboots. 
* **Attaching**: The control plane dynamically allocates a free block slot on the host and hot-plugs the volume into the running VM's virtual PCI bus.
* **Detaching**: The control plane safely disconnects the block device from the hypervisor and synchronizes changed data blocks to durable object storage.

### 3. Dual-Path Snapshot Architecture
Snapshot capture adapts based on the volume's current state:
* **Attached Volumes**: Snapshots perform a live, zero-downtime block copy of the active block device while the guest VM continues running uninterrupted.
* **Detached Volumes**: Snapshots stage the volume data from durable object storage, generate a point-in-time qcow2 snapshot image, and save it to snapshot storage.

### 4. Asynchronous Storage Expansion
Volume expansion accepts requested size increases immediately (returning `202 Accepted`) and processes the expansion asynchronously. 
* **Live Expansion**: Attached volumes undergo live hypervisor block resize operations, allowing the guest kernel to immediately detect the larger device size without downtime.
* **Offline Expansion**: Detached volume images are expanded prior to next attachment.

---

## Security & Isolation

* **Tenant Isolation**: Volumes are bound to specific tenant accounts and tenant virtual private networks.
* **Data Security**: Volume data in transit and at rest is protected, and host storage paths are isolated per instance boundary.
* **Safe Device Access**: Guest instances interact only with paravirtualized block controllers, preventing direct access to physical host storage hardware.

---

## Related Documentation

* [Volumes Overview & API Reference](volumes)
* [Attaching & Detaching Volumes Guide](volumes-attaching-detaching-volumes)
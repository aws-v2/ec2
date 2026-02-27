# EC2 Overview

Elastic Compute Cloud (EC2) on Serwin allows you to provision, manage, and scale virtual machines (instances) in a cloud environment. EC2 abstracts the underlying hardware, giving you full control over your compute resources via a simple API and web interface.

## Key Concepts

| Concept | Description |
|---|---|
| **Instance** | A virtual machine running on shared or dedicated infrastructure. |
| **Image (AMI)** | A pre-configured OS disk image used to boot instances (e.g., `ubuntu-22.04`). |
| **Instance Type** | A combination of vCPU and RAM defining the compute capacity. |
| **SSH Key** | A cryptographic key pair used to authenticate securely to an instance. |
| **Security Group** | A virtual firewall controlling inbound and outbound network traffic. |
| **Volume** | Elastic Block Storage (EBS) providing persistent block-level storage. |
| **Snapshot** | A point-in-time backup of an instance or a volume. |
| **Template** | A reusable launch configuration for creating identical instances. |

## Supported OS Images

Serwin EC2 supports a range of Linux distributions for root disk images:

- `ubuntu-22.04` — Ubuntu 22.04 LTS (Jammy Jellyfish)
- `ubuntu-20.04` — Ubuntu 20.04 LTS (Focal Fossa)
- `debian-12` — Debian 12 (Bookworm)
- `centos-9` — CentOS Stream 9
- `fedora-38` — Fedora 38

Images are stored as raw QCOW2 disk files and cloned on instance creation.

## Architecture Overview

```
┌────────────────────────────────────────────┐
│             EC2 API Server                 │
│                                            │
│  ┌────────┐  ┌──────────┐  ┌───────────┐  │
│  │ Router │→ │ Handlers │→ │ Services  │  │
│  └────────┘  └──────────┘  └─────┬─────┘  │
│                                  │         │
│         ┌────────────────────────┤         │
│         ▼                        ▼         │
│   ┌──────────┐           ┌────────────┐   │
│   │ Postgres  │           │  Libvirt   │   │
│   │    DB    │           │  (QEMU/KVM)│   │
│   └──────────┘           └────────────┘   │
└────────────────────────────────────────────┘
```

- **API Server** — A Go application using the Gin web framework, exposing RESTful endpoints under `/api/v1/ec2`.
- **PostgreSQL** — Persistent storage for all resource metadata (instances, volumes, snapshots, keys, etc.).
- **Libvirt / QEMU-KVM** — The virtualization backend that runs and manages virtual machine domains.

## Root Disk Types

| Type | Description | Recommended For |
|---|---|---|
| **SSD (virtio)** | Fast solid-state backed disk | General purpose, databases |
| **HDD (ide)** | Slower rotational-style disk | Archive, low-cost storage |

## API Base URL

All EC2 API calls are made to:

```
http://<host>:8080/api/v1/ec2
```

# EC2 Service — Internal Documentation

> **Service name:** `ec2-service`
> **Owned by:** Serwin Systems Platform Team
> **Last updated:** 2026-05-05

---

## Table of Contents

1. [Overview](#1-overview)
2. [What is the EC2 Service?](#2-what-is-the-ec2-service)
3. [Instance Lifecycle](#3-instance-lifecycle)
4. [Provisioning Pipeline](#4-provisioning-pipeline)
5. [Instance Profiles](#5-instance-profiles)
6. [SSH Key Management](#6-ssh-key-management)
7. [Disk Management](#7-disk-management)
8. [Cloud-Init](#8-cloud-init)
9. [Service Dependencies](#9-service-dependencies)
10. [NATS Messaging](#10-nats-messaging)
11. [Instance Identity & Naming](#11-instance-identity--naming)
12. [API Reference](#12-api-reference)
13. [Data Model](#13-data-model)
14. [Configuration Reference](#14-configuration-reference)
15. [Troubleshooting](#15-troubleshooting)

---

## 1. Overview

The **EC2 Service** is the core compute provisioning engine of Serwin Systems. It is the equivalent of AWS EC2 — responsible for the full lifecycle of virtual machine instances from creation through termination.

It orchestrates KVM/libvirt virtual machines on bare-metal hypervisor hosts, integrating with the Networking Service for IP allocation, the IAM Service for instance identity tokens, NATS for async event streaming, and cloud-init for in-guest configuration at first boot.

```
                        ┌──────────────────────────────────────┐
                        │            EC2 Service               │
                        │                                      │
  REST API ────────────►│  Instance Manager                    │
                        │   ├── CreateInstance                 │
                        │   ├── ListInstances                  │
                        │   ├── TerminateInstance              │
                        │   └── GetInstance                    │
                        │                                      │
                        │  VM Provisioner (Async)              │
                        │   ├── Clone base disk                │
                        │   ├── Inject payload (gamelift)      │
                        │   ├── Generate SSH key pair          │
                        │   ├── Build cloud-init ISO           │
                        │   └── Define + start libvirt domain  │
                        └──────────────┬───────────────────────┘
                                       │
              ┌────────────────────────┼────────────────────────┐
              ▼                        ▼                        ▼
     Network Service            IAM Service              NATS / JetStream
   (VPC + IP + bridge)     (instance identity         (lifecycle events,
                               token via NATS)          progress updates)
```

---

## 2. What is the EC2 Service?

### Responsibility

The EC2 Service owns everything between "a user requests a VM" and "the VM is reachable on the network." Specifically:

- **Instance record management** — creating, updating, and querying instance records in PostgreSQL
- **VM provisioning** — cloning base disk images, building cloud-init ISOs, defining and starting libvirt domains
- **SSH key pair generation** — generating a unique ed25519 keypair per instance, stored in the DB and injected into the VM via cloud-init
- **Profile-based customization** — different VM profiles (vanilla, gamelift, ai-worker) result in different cloud-init payloads being injected
- **Security group assignment** — auto-assigning the `default` security group on instance creation
- **Lifecycle event publishing** — emitting NATS events at every stage of provisioning so the frontend and other services can track progress in real time

### What it does NOT own

| Concern | Owned by |
|---------|----------|
| IP allocation / VPC management | Network Service |
| User authentication / JWT validation | IAM Service |
| iptables / firewall rule enforcement | Security Group Service |
| S3 storage volumes | S3 Service |
| Metrics collection inside the VM | Metrics Agent (in-guest) |
| Game session management | GameLift Service |

---

## 3. Instance Lifecycle

An instance moves through the following states from creation to termination:

```
                     ┌─────────────┐
    POST /instances  │             │
  ──────────────────►│   PENDING   │
                     │             │
                     └──────┬──────┘
                            │  Provisioner picks up job (async goroutine)
                            ▼
                     ┌─────────────┐
                     │  CLONING    │  qemu-img create (disk clone)
                     └──────┬──────┘
                            │
                            ▼
                     ┌─────────────┐
                     │  STARTING   │  libvirt domain define + create
                     └──────┬──────┘
                            │
                            ▼
                     ┌─────────────┐
                     │   RUNNING   │  VM booted, IP reachable
                     └──────┬──────┘
                            │  DELETE /instances/:id
                            ▼
                     ┌─────────────┐
                     │ TERMINATING │  libvirt domain destroy + undefine
                     └──────┬──────┘
                            │
                            ▼
                     ┌─────────────┐
                     │ TERMINATED  │  IP lease released, disk deleted
                     └─────────────┘

   Error at any stage ──► FAILED
```

### State Descriptions

| State | Description |
|-------|-------------|
| `PENDING` | Instance record created in DB, provisioner not yet started |
| `CLONING` | Base disk image is being cloned via `qemu-img` |
| `STARTING` | libvirt domain is being defined and started |
| `RUNNING` | VM is live, reachable on its allocated private IP |
| `TERMINATING` | Shutdown in progress, resources being cleaned up |
| `TERMINATED` | All resources released. Record kept for audit |
| `FAILED` | Provisioning failed at some stage. Error reason stored in DB |

---

## 4. Provisioning Pipeline

Instance creation is a **two-phase operation**:

- **Phase 1 (synchronous, ~50ms):** Validate request, allocate network resources, generate SSH keys, write instance record to DB, return `202 Accepted` with instance ID.
- **Phase 2 (asynchronous goroutine):** Heavy lifting — disk clone, VM definition, cloud-init ISO creation, boot.

This means the API returns immediately while the VM is being provisioned in the background. The frontend polls `GET /instances` and receives real-time progress via NATS lifecycle events.

### Full Pipeline Detail

```
POST /api/v1/ec2/instances
         │
         ▼
┌─────────────────────────────────┐
│ 1. Validate request             │
│    - CPU / RAM limits           │
│    - Profile is valid           │
│    - Base image exists on disk  │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 2. Request instance IAM token   │
│    NATS: dev.v1.iam.token.      │
│          generate               │
│    Returns: signed JWT for the  │
│    instance to auth with other  │
│    services from inside the VM  │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 3. Prepare network              │
│    - GetOrCreateDefaultVPC      │
│    - EnsureNetworkOnHost        │
│    - AllocateIP (atomic lease)  │
│    Returns: IP, gateway, bridge │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 4. Generate SSH key pair        │
│    - ed25519 via crypto/rand    │
│    - PrivateKeyPEM → stored DB  │
│    - PublicKeyAuth → cloud-init │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 5. Write instance to DB         │
│    Status: PENDING              │
│    Fields: IP, keys, profile,   │
│            vpc_id, token...     │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 6. Return 202 Accepted          │  ◄── API response to caller
│    { instance_id, status, ip }  │
└────────────────┬────────────────┘
                 │
    ┌────────────┘  async goroutine spawned
    ▼
┌─────────────────────────────────┐
│ 7. Clone base disk              │
│    qemu-img create -f qcow2     │
│    -F qcow2 -b base.qcow2       │
│    new-disk.qcow2               │
│    (copy-on-write, fast)        │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 8. Payload injection (gamelift) │
│    Host-side disk mount +       │
│    write game binary/config     │
│    into the disk before boot    │
│    (skipped for other profiles) │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 9. Build cloud-init ISO         │
│    Three files:                 │
│    - user-data  (SSH keys,      │
│                  users, runcmd) │
│    - meta-data  (instance-id,   │
│                  hostname)      │
│    - network-config (static IP, │
│                  gateway)       │
│    Written to tmpdir, packed    │
│    into ISO via genisoimage     │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 10. Define + start libvirt VM   │
│     - Build domain XML          │
│     - DomainDefineXML           │
│     - domain.Create()           │
│     - Attach cloud-init ISO     │
│     - Attach disk               │
│     - Attach bridge NIC         │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 11. Update instance in DB       │
│     Status: RUNNING             │
│     ProxmoxID: libvirt domain   │
│                ID               │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 12. Publish INSTANCE_STARTED    │
│     NATS event                  │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 13. Auto-assign default SG      │
│     (async goroutine)           │
└─────────────────────────────────┘
```

---

## 5. Instance Profiles

A **profile** defines what software environment is set up inside the VM at first boot. The profile is passed as part of the `CreateInstanceRequest` and controls:

- Which `write_files` blocks are injected into cloud-init `user-data`
- Which `runcmd` commands run at first boot
- Whether host-side disk injection is performed before the VM starts

### Available Profiles

#### `vanilla`

A plain Ubuntu 22.04 instance with no additional software. Suitable for general-purpose compute.

- No host-side disk injection
- Minimal `runcmd` (update packages, configure hostname)
- SSH access only via generated keypair + user's keys

#### `gamelift`

A game server hosting profile. Uses **host-side disk injection** (`injectPayloadIntoDisk`) to write game binaries and config into the disk before the VM boots. This is for legacy compatibility — new game profiles should prefer the `ai-worker` self-preparation model.

- Host-side injection runs in Step 8 of the pipeline
- `runcmd` launches the game server process on boot
- Params map (`req.Parameters`) carries game-specific config (e.g. `{{GAME_PORT}}`, `{{SESSION_ID}}`)
- Template placeholders in `user-data` are replaced at ISO build time

#### `ai-worker`

A machine learning inference worker profile, modelled after AWS SageMaker. The VM handles its own preparation — it downloads model weights, sets up the inference server, and registers with the metrics service **from inside the VM** using the instance IAM token.

- No host-side disk injection
- `runcmd` uses `__IAM_TOKEN__` and `__INSTANCE_ID__` placeholders to authenticate against internal services
- Designed for long-running inference workloads

### Profile Placeholder Substitution

All profiles support dynamic value injection via `user-data` template placeholders:

| Placeholder | Replaced with |
|-------------|---------------|
| `__INSTANCE_ID__` | The instance ID (e.g. `i-b99bba33`) |
| `__IAM_TOKEN__` | The signed JWT issued by IAM Service |
| `__GATEWAY_IP__` | The VPC gateway IP (e.g. `10.0.1.1`) |
| `{{KEY}}` | Any key from `req.Parameters` map, uppercased |

---

## 6. SSH Key Management

Every instance gets a **unique ed25519 keypair** generated at creation time.

### Generation

```
ed25519.GenerateKey(crypto/rand.Reader)
    │
    ├── Private key → MarshalPrivateKey → OpenSSH PEM format
    │       Stored in instances.private_key_pem (DB)
    │       Returned to user via API after creation
    │
    └── Public key → ssh.MarshalAuthorizedKey → authorized_keys format
            Injected into cloud-init user-data
            Also stored in instances.public_key_auth (DB)
```

### Key Injection into VM

Two keys are combined and injected into the VM via cloud-init `ssh-authorized-keys`:

1. **Instance keypair public key** — generated uniquely per instance
2. **System public key** (`s.systemPubKey`) — the platform's master key, allows ops team SSH access to any instance

```yaml
# user-data fragment (rendered at ISO build time)
users:
  - name: ubuntu
    ssh-authorized-keys:
      - ssh-ed25519 AAAAC3Nz...  # instance keypair
      - ssh-rsa AAAAB3Nz...      # system key
```

Keys are sanitised before injection — whitespace and embedded newlines are stripped to prevent malformed `authorized_keys` entries.

### Key Security

- Private keys are stored encrypted at rest in PostgreSQL
- Private keys are **never logged** — only the first 64 characters of the PEM (the header line) are written to logs for debugging
- The system private key lives in the EC2 service config and is never stored in the DB

---

## 7. Disk Management

### Base Images

Base images are pre-built qcow2 disk images stored on the hypervisor host at:

```
/var/lib/libvirt/images/{image-name}.qcow2
```

Example:
```
/var/lib/libvirt/images/ubuntu-22.04.qcow2
```

The EC2 service checks for base image existence before starting provisioning. If the image is missing, the request fails fast with a meaningful error rather than failing deep in the async pipeline.

```
Image ubuntu-22.04 already exists at /var/lib/libvirt/images/ubuntu-22.04.qcow2
```

### Instance Disk (Copy-on-Write Clone)

Each instance gets its own disk cloned from the base image using qemu-img's **copy-on-write (COW)** backing file mechanism:

```bash
qemu-img create -f qcow2 -F qcow2 \
  -b /var/lib/libvirt/images/ubuntu-22.04.qcow2 \
  /var/lib/libvirt/images/vm-i-b99bba33.qcow2
```

This means:
- The clone is created in **milliseconds** regardless of base image size
- The base image is never modified
- The instance disk only stores the delta (writes) from the base
- Multiple instances share the same base image data on disk

### Disk Cleanup on Termination

When an instance is terminated, its disk is deleted:

```go
exec.Command("rm", "-f", newDiskPath).Run()
```

The base image is never touched.

---

## 8. Cloud-Init

Cloud-init is the standard Linux first-boot configuration system. The EC2 service builds a **cloud-init ISO** (also called a "seed ISO" or "config drive") and attaches it to the VM as a virtual CD-ROM. On first boot, the VM reads this ISO and applies the configuration.

### ISO Contents

```
cloudinit.iso
├── user-data        # Users, SSH keys, packages, files, run commands
├── meta-data        # Instance ID and hostname
└── network-config   # Static IP configuration (Netplan v2 format)
```

### `user-data`

Controls the in-guest OS configuration:

```yaml
#cloud-config
ssh_pwauth: true
users:
  - name: ubuntu
    plain_text_passwd: "ubuntu"
    sudo: ['ALL=(ALL) NOPASSWD:ALL']
    shell: /bin/bash
    lock_passwd: false
    ssh-authorized-keys:
      - ssh-ed25519 AAAAC3Nz...  # instance key
      - ssh-rsa AAAAB3Nz...      # system key

write_files:
  # Profile-specific files written into the VM filesystem

runcmd:
  # Profile-specific commands run on first boot
```

### `network-config`

Configures the VM's NIC with the pre-allocated static IP (Netplan v2 format). **DHCP is not used.**

```yaml
version: 2
ethernets:
  ens3:
    addresses:
      - 10.0.1.19/24
    gateway4: 10.0.1.1
    nameservers:
      addresses: [8.8.8.8, 1.1.1.1]
```

### `meta-data`

Minimal instance identity data:

```yaml
instance-id: vm-i-b99bba33
local-hostname: vm-i-b99bba33
```

### ISO Creation

The three files are written to a temp directory and packed into an ISO using `genisoimage` (or `mkisofs`) with the `-V cidata` label that cloud-init recognises:

```bash
genisoimage -output /var/lib/libvirt/images/vm-i-b99bba33-cloudinit.iso \
            -V cidata -r -J \
            user-data meta-data network-config
```

Temp files are cleaned up after the ISO is created via a `defer cleanupFn()`.

---

## 9. Service Dependencies

| Dependency | How Used | Failure Behaviour |
|------------|----------|-------------------|
| **Network Service** | VPC creation, IP allocation | Instance creation aborted, 500 returned |
| **IAM Service** | Instance token via NATS RPC | Instance creation aborted if token not received |
| **libvirt / KVM** | VM define, start, stop, delete | Instance provisioning fails, status → FAILED |
| **PostgreSQL** | All instance records, IP leases, SG assignments | Service cannot operate |
| **NATS / JetStream** | Progress events, inter-service RPC | Degraded mode — VM still created but events not published |
| **Security Group Service** | Auto-assign default SG | Non-fatal — instance still runs, SG assigned on retry |

---

## 10. NATS Messaging

### Subjects

| Subject | Direction | Purpose |
|---------|-----------|---------|
| `{env}.v1.ec2.instance.lifecycle` | Publish | Real-time provisioning progress |
| `{env}.v1.iam.token.generate` | Request/Reply | Get instance IAM token |

Where `{env}` is `dev`, `staging`, or `prod`.

### Lifecycle Event Stages

Events are published to `{env}.v1.ec2.instance.lifecycle` at each stage. The frontend subscribes to these to drive the real-time progress UI.

| Stage constant | Message |
|----------------|---------|
| `StageCloningDisk` | `Cloning base image to new instance disk...` |
| `StageStartingVM` | `Defining and starting the virtual machine...` |
| `StageProvisioned` | `Instance is now running and reachable.` |
| `StageFailed` | Error message from the failing step |

### IAM Token Request

Before provisioning starts, the EC2 service makes a synchronous NATS request to the IAM service:

```
Subject:  dev.v1.iam.token.generate
Payload:  { user_id, instance_id, correlation_id }
Reply:    { token: "<signed JWT>" }
Timeout:  5s
```

The returned token is baked into the cloud-init `user-data` as `__IAM_TOKEN__`. The in-guest agent uses it to authenticate against the metrics service and other internal APIs.

---

## 11. Instance Identity & Naming

### Instance ID

Instance IDs are generated at creation time as `i-` followed by 8 random hex characters:

```
i-b99bba33
i-286783a2
```

This matches the AWS EC2 instance ID format for compatibility.

### VM Name

The libvirt domain name is derived by prepending `vm-` to the instance ID:

```
Instance ID:  i-b99bba33
VM Name:      vm-i-b99bba33
```

This is used as:
- The libvirt domain name
- The hostname inside the VM (set via `meta-data`)
- The disk image filename prefix (`vm-i-b99bba33.qcow2`)
- The cloud-init ISO filename prefix (`vm-i-b99bba33-cloudinit.iso`)

The instance ID can always be recovered from the VM name by stripping the `vm-` prefix.

---

## 12. API Reference

### `POST /api/v1/ec2/instances`

Launch a new instance.

**Request body:**

```json
{
  "image": "ubuntu-22.04",
  "cpu": 2,
  "ram": 2048,
  "profile": "vanilla",
  "parameters": {
    "game_port": "7777"
  }
}
```

**Response:** `202 Accepted`

```json
{
  "instance_id": "i-b99bba33",
  "status": "PENDING",
  "private_ip": "10.0.1.19"
}
```

---

### `GET /api/v1/ec2/instances`

List all instances for the authenticated user.

**Response:** `200 OK`

```json
[
  {
    "instance_id": "i-b99bba33",
    "status": "RUNNING",
    "private_ip": "10.0.1.19",
    "public_ip": null,
    "profile": "vanilla",
    "created_at": "2026-05-05T12:19:26Z"
  }
]
```

---

### `GET /api/v1/ec2/instances/:id`

Get a single instance by ID.

---

### `DELETE /api/v1/ec2/instances/:id`

Terminate an instance. Destroys the libvirt domain, deletes the disk, releases the IP lease.

**Response:** `204 No Content`

---

## 13. Data Model

### `instances`

| Column | Type | Description |
|--------|------|-------------|
| `id` | VARCHAR | Instance ID (`i-b99bba33`) |
| `vm_name` | VARCHAR | libvirt domain name (`vm-i-b99bba33`) |
| `user_id` | UUID | Owning user |
| `status` | ENUM | `PENDING`, `RUNNING`, `TERMINATED`, `FAILED`, ... |
| `profile` | VARCHAR | `vanilla`, `gamelift`, `ai-worker` |
| `private_ip` | VARCHAR | Allocated VPC IP |
| `public_ip` | VARCHAR | Elastic IP if assigned, else null |
| `proxmox_id` | INT | libvirt domain ID (misnamed, legacy) |
| `vpc_id` | UUID | FK → `vpcs.id` |
| `cpu` | INT | vCPU count |
| `ram` | INT | RAM in MB |
| `disk_path` | VARCHAR | Absolute path to instance disk image |
| `private_key_pem` | TEXT | OpenSSH PEM private key (encrypted at rest) |
| `public_key_auth` | TEXT | `authorized_keys` format public key |
| `instance_token` | TEXT | IAM JWT for this instance |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

---

## 14. Configuration Reference

| Env Var | Description |
|---------|-------------|
| `LIBVIRT_URI` | libvirt connection URI (default: `qemu:///system`) |
| `IMAGES_DIR` | Base directory for disk images (default: `/var/lib/libvirt/images`) |
| `SYSTEM_PUBLIC_KEY` | Platform SSH public key injected into every VM |
| `NATS_URL` | NATS server URL |
| `EC2_DB_DSN` | PostgreSQL DSN |
| `PROFILE` | Active config profile (`dev`, `staging`, `prod`) |
| `IAM_TOKEN_TIMEOUT` | Timeout for IAM token NATS request (default: `5s`) |

---

## 15. Troubleshooting

### Instance stuck in `PENDING`

The async provisioner goroutine may have panicked silently. Check service logs for the instance ID around the creation timestamp. Look for `[VM]` prefixed log lines.

---

### `Failed to define domain: domain already exists`

A previous failed provisioning run left a half-defined libvirt domain. Clean it up:

```bash
virsh undefine vm-i-b99bba33
virsh destroy vm-i-b99bba33  # if still running
```

---

### VM boots but SSH does not work

1. Check that the cloud-init ISO was created and attached:
   ```bash
   virsh dumpxml vm-i-b99bba33 | grep -A3 "cdrom"
   ```

2. Mount and inspect the ISO:
   ```bash
   mkdir /tmp/ci && mount -o loop /var/lib/libvirt/images/vm-i-b99bba33-cloudinit.iso /tmp/ci
   cat /tmp/ci/user-data
   umount /tmp/ci
   ```

3. Confirm the `ssh-authorized-keys` block contains both the instance key and system key.

---

### `Failed to clone disk: exit status 1`

The base image path does not exist or has wrong permissions:

```bash
ls -lh /var/lib/libvirt/images/ubuntu-22.04.qcow2
# Should be readable by the libvirt/qemu user
chown libvirt-qemu:libvirt-qemu /var/lib/libvirt/images/ubuntu-22.04.qcow2
```

---

### Instance token not received (IAM timeout)

```
[NATS] Failed to get instance token for i-b99bba33: nats: timeout
```

- Check that the IAM service is running and subscribed to `dev.v1.iam.token.generate`
- Check NATS connectivity between EC2 service and IAM service
- Increase `IAM_TOKEN_TIMEOUT` if network latency is high

---

*For questions or changes, open a PR against `docs/internal/ec2-service.md` in the `serwin-systems` monorepo.*
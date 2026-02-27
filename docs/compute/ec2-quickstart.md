# Quickstart: Launch an Instance

Get your first EC2 instance running on Serwin in minutes by following these steps.

## Step 1: Choose an Image

Select the operating system image for your instance. The image defines what OS is pre-installed on the root disk.

Popular choices:
- `ubuntu-22.04` — Ubuntu 22.04 LTS (recommended for most workloads)
- `debian-12` — Debian 12 Bookworm

## Step 2: Choose an Instance Type

Select the compute profile that matches your workload:

| Type | vCPU | RAM | Use Case |
|---|---|---|---|
| `t2.micro` | 1 | 1 GB | Dev/test, low traffic |
| `t2.small` | 1 | 2 GB | Light web apps |
| `t2.medium` | 2 | 4 GB | Medium web apps |
| `c3.large` | 2 | 4 GB | CPU-intensive tasks |
| `m3.large` | 2 | 8 GB | General purpose |
| `r3.large` | 2 | 16 GB | Memory-intensive tasks |

## Step 3: Create an SSH Key

SSH keys are required to authenticate to your instance. You can generate one via the API:

```bash
# Generate a new key pair (returns private key in response)
curl -X POST http://localhost:8080/api/v1/ec2/ssh-keys \
  -H "Content-Type: application/json" \
  -d '{"name": "my-key"}'
```

> **Save the private key from the response!** It is only shown once. Download it from:
> `GET /api/v1/ec2/ssh-keys/my-key/download`

## Step 4: Launch the Instance

```bash
curl -X POST http://localhost:8080/api/v1/ec2/instances \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-first-instance",
    "image": "ubuntu-22.04",
    "instance_type": "t2.micro",
    "ssh_key_name": "my-key"
  }'
```

The API will respond with an instance object including your instance's `id` and current `status`.

## Step 5: Wait for Instance to be Running

Poll the instance status until it reaches `running`:

```bash
curl http://localhost:8080/api/v1/ec2/instances/<id>
```

The `status` field will transition from `pending` → `running`.

## Step 6: Connect via SSH

Once the instance is running, connect using the downloaded private key:

```bash
chmod 600 my-key.pem
ssh -i my-key.pem ubuntu@<public-ip>
```

Replace `<public-ip>` with the IP address from the instance details response.

## What's Next?

- Attach additional **EBS Volumes** for persistent storage
- Configure **Security Groups** to expose specific ports
- Take a **Snapshot** as a backup
- Create a **Launch Template** to reuse this configuration

# Instance Types

Serwin EC2 offers a range of instance types organized into compute families. Choose the type that best matches your application's resource requirements.

## Instance Families

### General Purpose

Balanced compute, memory, and networking resources for a broad range of workloads including web servers, development environments, and small databases.

### Compute Optimized

High ratio of vCPU to memory, ideal for compute-bound applications like batch processing, media transcoding, and high-performance computing.

### Memory Optimized

High ratio of memory to vCPU, suited for in-memory databases, real-time analytics, and large caching layers.

## Available Instance Types

| Name | Family | vCPU | RAM (GB) | Use Case |
|---|---|---|---|---|
| `t2.nano` | General Purpose | 1 | 0.5 | Micro services, testing |
| `t2.micro` | General Purpose | 1 | 1 | Dev/test, very low traffic |
| `t2.small` | General Purpose | 1 | 2 | Light web applications |
| `t2.medium` | General Purpose | 2 | 4 | Medium-traffic web apps |
| `t2.large` | General Purpose | 2 | 8 | Production web servers |
| `t2.xlarge` | General Purpose | 4 | 16 | Multi-service applications |
| `c3.large` | Compute Optimized | 2 | 4 | Batch processing, CI/CD |
| `c3.xlarge` | Compute Optimized | 4 | 8 | Video transcoding, HPC |
| `c3.2xlarge` | Compute Optimized | 8 | 16 | High-throughput computation |
| `m3.large` | General Purpose | 2 | 8 | General production workloads |
| `m3.xlarge` | General Purpose | 4 | 16 | Balanced workloads |
| `r3.large` | Memory Optimized | 2 | 16 | In-memory caching |
| `r3.xlarge` | Memory Optimized | 4 | 32 | Large in-memory databases |
| `r3.2xlarge` | Memory Optimized | 8 | 64 | Real-time analytics |

## Choosing the Right Instance Type

1. **Start small** — Use `t2.micro` for development and testing.
2. **Scale up** — Move to `t2.medium` or `m3.large` for production web apps.
3. **Go specialized** — Use `c3.*` for CPU-intensive jobs, `r3.*` for memory-heavy workloads.

## Specifying Instance Type on Launch

```bash
POST /api/v1/ec2/instances
{
  "name": "web-server-01",
  "image": "ubuntu-22.04",
  "instance_type": "t2.medium",
  "ssh_key_name": "my-key"
}
```

## Instance Resource Limits

By default, each account is limited to:

- **10 running instances** per region
- **100 vCPUs** total across all instances
- **500 GB** of EBS storage per region

Contact support to increase these limits.

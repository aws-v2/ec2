# Metrics & Status Checks

Serwin EC2 provides real-time monitoring of instance health and resource usage. Monitor CPU, memory, network, and disk metrics directly via the API or UI.

## Available Metrics

| Metric | Unit | Description |
|---|---|---|
| `cpu_usage` | Percentage (%) | CPU utilization across all vCPUs |
| `ram_usage` | Percentage (%) | Memory utilization relative to total RAM |
| `network_in` | KB/s | Network bytes received per second |
| `network_out` | KB/s | Network bytes sent per second |
| `disk_read` | IOPS | Disk read operations per second |
| `disk_write` | IOPS | Disk write operations per second |

## Fetching Instance Metrics

```bash
GET /api/v1/ec2/instances/:id/metrics
```

**Response:**
```json
{
  "data": {
    "instance_id": "vm-abc123",
    "cpu_usage": 23.5,
    "ram_usage": 61.2,
    "network_in": 1024,
    "network_out": 512,
    "disk_read": 45,
    "disk_write": 12,
    "timestamp": "2026-02-27T09:00:00Z"
  }
}
```

## Metrics Polling

Metrics are collected at a **30-second** interval from the hypervisor. The API returns the most recent sample. For historical metrics, use a dedicated time-series solution (e.g., Prometheus + Grafana).

## Status Checks

Status checks provide a binary health indicator for your instance. Two checks run automatically on every `running` instance:

### System Status Check

Validates that the underlying host (hypervisor, network, hardware) is functioning correctly.

| Result | Meaning |
|---|---|
| `passed` | Host infrastructure is healthy |
| `failed` | Possible hypervisor or hardware issue — contact support |

### Instance Status Check

Validates that the VM itself is responding to network checks (ICMP ping or TCP connection).

| Result | Meaning |
|---|---|
| `passed` | Instance is reachable and healthy |
| `failed` | Instance OS may have crashed or be unresponsive |

## Fetching Status Checks

```bash
GET /api/v1/ec2/instances/:id/status-checks
```

**Response:**
```json
{
  "data": {
    "instance_id": "vm-abc123",
    "system_check": "passed",
    "instance_check": "passed",
    "overall": "passed",
    "checked_at": "2026-02-27T09:00:00Z"
  }
}
```

## Alerting

The current platform does not include built-in alerting. To set up alerts:

1. Poll `GET /instances/:id/metrics` from your monitoring system.
2. Parse `cpu_usage`, `ram_usage`, etc.
3. Trigger alerts using your existing observability stack (PagerDuty, Grafana, etc.).

## Common Troubleshooting

| Symptom | Likely Cause | Action |
|---|---|---|
| High CPU (>90%) | Process spike or stuck job | SSH in and run `top` |
| High RAM (>90%) | Memory leak or undersized instance | Restart app or resize instance |
| Instance check failed | OS crash | Stop and start the instance |
| System check failed | Hypervisor issue | Contact support |

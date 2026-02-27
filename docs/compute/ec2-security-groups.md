# Security Groups

Security Groups act as virtual firewalls for your EC2 instances, controlling what network traffic is allowed in and out. Every instance must be associated with at least one security group.

## How Security Groups Work

```
Internet
   │
   ▼
┌──────────────────────┐
│   Security Group     │
│  ┌────────────────┐  │
│  │ Inbound Rules  │  │  ← What traffic can reach your instance
│  └────────────────┘  │
│  ┌─────────────────┐ │
│  │ Outbound Rules  │ │  ← What traffic your instance can send
│  └─────────────────┘ │
└──────────┬───────────┘
           │
           ▼
     EC2 Instance
```

Security groups are **stateful** — if you allow inbound traffic on a port, responses are automatically allowed out.

## Default Security Group

Every new EC2 instance is automatically assigned the `default` security group, which has the following inbound rule pre-configured:

| Protocol | Port | Source | Description |
|---|---|---|---|
| TCP | 22 | 0.0.0.0/0 | SSH access from anywhere |

> You can modify or replace the default security group at any time.

## Rule Properties

| Field | Description | Example |
|---|---|---|
| **Protocol** | Network protocol | `tcp`, `udp`, `icmp` |
| **Port / Port Range** | Port or range to match | `80`, `8000-8999`, `443` |
| **CIDR** | IP address range in CIDR notation | `0.0.0.0/0` (all), `192.168.1.0/24` |
| **Description** | Optional label for the rule | `HTTP from internet` |

## Common Security Group Rules

| Use Case | Protocol | Port | CIDR |
|---|---|---|---|
| SSH access | TCP | 22 | Your IP/32 |
| HTTP web server | TCP | 80 | 0.0.0.0/0 |
| HTTPS web server | TCP | 443 | 0.0.0.0/0 |
| PostgreSQL | TCP | 5432 | VPC CIDR |
| Redis | TCP | 6379 | VPC CIDR |
| All ICMP (ping) | ICMP | -1 | 0.0.0.0/0 |

## API Reference

### Create a Security Group

```bash
POST /api/v1/ec2/security-groups
{
  "name": "web-sg",
  "description": "Security group for web servers"
}
```

### List Security Groups

```bash
GET /api/v1/ec2/security-groups
```

### Add an Inbound Rule

```bash
POST /api/v1/ec2/security-groups/:id/rules
{
  "type": "inbound",
  "protocol": "tcp",
  "port": 443,
  "cidr": "0.0.0.0/0",
  "description": "HTTPS from internet"
}
```

### Remove a Rule

```bash
DELETE /api/v1/ec2/security-groups/:id/rules/:ruleId
```

### Assign a Security Group to an Instance

```bash
POST /api/v1/ec2/instances/:id/security-groups
{
  "security_group_id": 2
}
```

### Remove a Security Group from an Instance

```bash
DELETE /api/v1/ec2/instances/:id/security-groups/:sgId
```

## Security Best Practices

- **Restrict SSH** — Only allow port 22 from your specific IP address (`your.ip/32`), not `0.0.0.0/0`.
- **Principle of least privilege** — Only open ports your application actually needs.
- **Use separate groups** — Use different security groups for web, app, and database tiers.
- **Label rules** — Always add a description to help track why a rule exists.

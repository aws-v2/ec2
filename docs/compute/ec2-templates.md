# Launch Templates

Launch Templates are reusable configuration blueprints for EC2 instances. Instead of specifying all parameters every time you launch an instance, you define them once in a template and reference it whenever launching.

## Why Use Templates?

- **Consistency** — Ensure all instances in a fleet use identical configuration.
- **Speed** — Launch pre-defined instance types without manual configuration.
- **GitOps-friendly** — Templates can be version-controlled and deployed programmatically.
- **Automation** — Used by Fleet Console for bulk instance provisioning.

## Template Fields

| Field | Type | Description |
|---|---|---|
| `name` | string | Unique display name for the template |
| `description` | string | Human-readable description of the template's purpose |
| `image` | string | OS image slug (e.g., `ubuntu-22.04`) |
| `cpu` | int | Number of virtual CPUs |
| `ram` | int | Memory in MB |
| `instance_id` | string (optional) | Source instance to copy config from |

## Creating a Template

### Option 1: Manual Configuration

Specify all configuration fields explicitly:

```bash
POST /api/v1/ec2/templates
{
  "name": "web-server-standard",
  "description": "Standard web server configuration",
  "image": "ubuntu-22.04",
  "cpu": 2,
  "ram": 4096
}
```

### Option 2: From an Existing Instance

Capture the configuration of a running instance by providing its ID. The template will automatically inherit the instance's `image`, `cpu`, and `ram` values:

```bash
POST /api/v1/ec2/templates
{
  "name": "production-clone",
  "description": "Clone of production web server",
  "instance_id": "vm-abc123"
}
```

## Listing Templates

```bash
GET /api/v1/ec2/templates
```

## Getting Template Details

```bash
GET /api/v1/ec2/templates/:id
```

**Response:**
```json
{
  "data": {
    "id": 1,
    "name": "web-server-standard",
    "description": "Standard web server configuration",
    "image": "ubuntu-22.04",
    "cpu": 2,
    "ram": 4096,
    "status": "ready",
    "created_at": "2026-02-27T07:10:00Z"
  }
}
```

## Deleting a Template

```bash
DELETE /api/v1/ec2/templates/:id
```

## Template Status

| Status | Description |
|---|---|
| `ready` | Template is validated and available for launching. |
| `pending` | Template is being processed (e.g., linked to a live instance). |
| `error` | Template failed validation. |

## Best Practices

- **One template per environment tier** — Separate templates for dev, staging, and production.
- **Version templates** — Use naming conventions like `web-server-v1`, `web-server-v2`.
- **Review before deleting** — Ensure no active automation references the template before deletion.

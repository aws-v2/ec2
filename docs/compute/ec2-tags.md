# Tags

Tags are key-value metadata that you attach to EC2 resources (currently instances and volumes). They help you organize, search, filter, and manage resources at scale.

## What Are Tags?

A tag consists of a **key** and a **value**, both strings:

```json
{ "key": "environment", "value": "production" }
```

Tags are flexible — you define the schema. Common tag strategies:

| Key | Example Value | Purpose |
|---|---|---|
| `environment` | `production`, `staging`, `dev` | Identify the deployment environment |
| `project` | `api-backend`, `data-pipeline` | Group by project or service |
| `owner` | `alice`, `team-infra` | Track resource ownership |
| `cost-center` | `eng-001` | Cost allocation and billing |
| `version` | `v2.1.0` | Associate with a software release |

## Instance Tags API

### Add or Update a Tag

```bash
POST /api/v1/ec2/instances/:id/tags
{
  "key": "environment",
  "value": "production"
}
```

If the key already exists, the value is updated.

### List All Tags for an Instance

```bash
GET /api/v1/ec2/instances/:id/tags
```

**Response:**
```json
{
  "data": [
    { "key": "environment", "value": "production" },
    { "key": "project", "value": "api-backend" }
  ]
}
```

### Delete a Tag

```bash
DELETE /api/v1/ec2/instances/:id/tags/:key
```

## Volume Tags API

Volume tagging follows the same pattern as instance tags:

### Add or Update a Volume Tag

```bash
POST /api/v1/ec2/volumes/:id/tags
{
  "key": "purpose",
  "value": "postgres-data"
}
```

### List Volume Tags

```bash
GET /api/v1/ec2/volumes/:id/tags
```

### Delete a Volume Tag

```bash
DELETE /api/v1/ec2/volumes/:id/tags/:key
```

## Tagging Best Practices

- **Establish a tagging standard** — Define required tags (e.g., `environment`, `owner`) for all resources.
- **Automate tagging** — Apply tags at creation time via API payloads or automation scripts.
- **Use lowercase and hyphens** — Consistent key naming like `cost-center` avoids confusion.
- **Don't store sensitive data in tags** — Tags are visible to all users with API access.
- **Clean up tags** — Remove outdated tags when resources change ownership or environment.

## Bulk Tag Operations

The current API supports per-resource tagging. For bulk operations across many resources, use a script:

```bash
# Tag all instances in a project group (example shell script)
INSTANCE_IDS=$(curl -s http://localhost:8080/api/v1/ec2/instances | jq -r '.data[].id')

for id in $INSTANCE_IDS; do
  curl -X POST http://localhost:8080/api/v1/ec2/instances/$id/tags \
    -H "Content-Type: application/json" \
    -d '{"key": "managed-by", "value": "automation"}'
done
```

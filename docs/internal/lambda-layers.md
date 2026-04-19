---
title: Layers
description: Package and share dependencies across Lambda functions using Layers.
icon: layers
lastUpdated: 2026-02-27
tags:
  - Lambda
  - Layers
  - Dependencies
---

# Layers

A **Lambda Layer** is an archive containing libraries, custom runtimes, data, or configuration files that you can share across multiple functions. Instead of bundling dependencies into every deployment package, you attach layers to functions and Lambda merges them into the execution environment.

## What Layers Are

Layers solve the **dependency duplication** problem in Lambda:

- Without layers: each function carries its own copy of shared libraries (e.g. `numpy`, `boto3`, `lodash`), bloating every deployment package.
- With layers: shared libraries live in a single versioned layer. Functions reference the layer — no duplication, fast updates.

Layers are extracted to `/opt` in the execution environment. The runtime's module search path automatically includes `/opt/python`, `/opt/nodejs/node_modules`, etc., so imports work transparently.

## Attaching a Layer to a Function

### Via the Console

1. Go to **Lambda → Functions → [your function] → Configuration → Layers**
2. Click **Add Layer**
3. Select a layer and version from the list
4. Click **Save**

### Via the API

Include layer ARNs in your function registration or config update:

```bash
curl -X PATCH https://api.serwin.io/api/v1/lambda/functions/my-function/config \
  -H "Authorization: Bearer <YOUR_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "layers": [
      "arn:serwin:lambda:eu-north-1:123abc:layer:data-utils:4",
      "arn:serwin:lambda:eu-north-1:123abc:layer:numpy-layer:1"
    ]
  }'
```

## Layer Versioning

Every time you publish a layer, Lambda creates a new **immutable version**. Functions reference a specific version ARN — updating the layer does not automatically update functions that use it. To adopt a new layer version, update the function configuration to reference the new version ARN.

```
arn:serwin:lambda:eu-north-1:123abc:layer:my-layer:1   ← version 1
arn:serwin:lambda:eu-north-1:123abc:layer:my-layer:2   ← version 2 (new)
```

Old layer versions continue to work indefinitely unless explicitly deleted.

## Creating a Layer

Package your dependencies and upload as a zip:

```bash
# Python example
pip install requests -t ./python/
zip -r layer.zip python/

curl -X POST https://api.serwin.io/api/v1/lambda/layers \
  -H "Authorization: Bearer <YOUR_API_KEY>" \
  -F "name=requests-layer" \
  -F "runtime=python3.11" \
  -F "file=@layer.zip"
```

## Size Limits

| Limit | Value |
|-------|-------|
| Max unzipped size (function + all layers) | **250 MB** |
| Max layers per function | **5** |
| Max layer zip size (per layer) | **50 MB** compressed |

> **Tip:** If your total unzipped size approaches 250 MB, consider using a container image runtime instead of a zip-based deployment. Container images support up to **10 GB**.

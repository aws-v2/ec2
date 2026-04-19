---
title: Function Lifecycle
description: Understand the states, deployment process, versioning, and cold start behavior of Lambda functions.
icon: server
lastUpdated: 2026-02-27
tags:
  - Lambda
  - Lifecycle
  - Deployment
---

# Function Lifecycle

Every Lambda function moves through a defined set of states from creation to invocation. Understanding the lifecycle helps you build reliable, predictable serverless applications.

## Function States

| State | Description |
|-------|-------------|
| **Pending** | The function has been registered. Lambda is validating the code and preparing the runtime environment. |
| **Active** | The function is ready to receive invocations. |
| **Inactive** | The function exists but cannot be invoked. This happens after extended periods with no invocations, or when manually disabled. |
| **Failed** | Deployment or validation encountered an unrecoverable error. The function must be redeployed. |

A function transitions from **Pending → Active** automatically once validation completes. If validation fails (e.g. invalid runtime, corrupt artifact), it moves to **Failed**.

## Deployment Process

When you create or update a function, Lambda executes the following pipeline:

1. **Upload** — Your code artifact (file, zip, or container image reference) is transferred to Serwin storage.
2. **Validation** — Lambda verifies the artifact format, checks runtime compatibility, and scans for basic execution errors.
3. **Activation** — The function record is updated to **Active** and becomes invocable. Existing warm execution environments are recycled.

> **Code updates** follow the same pipeline. During the transition, in-flight invocations complete against the previous version. New invocations are held until activation completes (typically under 5 seconds).

## Versioning and Aliases

Lambda supports **immutable versions** and mutable **aliases**:

- **Version** — A snapshot of your function code and configuration at a point in time. Published versions cannot be modified. Referenced by a numeric suffix (e.g. `:3`).
- **Alias** — A pointer to a specific version. Aliases are mutable and can be updated to redirect traffic to a new version without changing downstream integrations (e.g. `$LATEST`, `production`, `staging`).

```
arn:serwin:lambda:eu-north-1:123abc:function:hello-world:3      ← version
arn:serwin:lambda:eu-north-1:123abc:function:hello-world:prod   ← alias
```

## Cold Start and the Initialization Phase

When Lambda creates a new execution environment (cold start), it runs through two phases before executing your handler:

1. **Init Phase** — Lambda downloads your code, initializes the runtime, and runs all code **outside** your handler function (global imports, connections, configuration). This runs once per environment lifetime.

2. **Invoke Phase** — Your handler function is called with the event and context objects.

```python
import boto3  # ← runs during Init phase (once)

def handler(event, context):  # ← runs during Invoke phase (every call)
    return {"statusCode": 200}
```

To minimize cold start impact:
- Keep global initialization lightweight
- Cache SDK clients and database connections at the module level (they persist across warm invocations)
- Keep your deployment package small to reduce download time

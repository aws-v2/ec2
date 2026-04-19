---
title: Environment Variables
description: Configure, manage, and secure environment variables for your Lambda functions.
icon: settings
lastUpdated: 2026-02-27
tags:
  - Lambda
  - Environment Variables
  - Configuration
---

# Environment Variables

Environment variables let you pass configuration, secrets, and runtime parameters to your Lambda function without hardcoding them in your source code.

## How Environment Variables Work in Lambda

When Lambda invokes your function, it injects all configured environment variables into the runtime process. They are accessible via the standard OS mechanism in each language:

```python
# Python
import os
db_url = os.environ["DATABASE_URL"]
```

```javascript
// Node.js
const dbUrl = process.env.DATABASE_URL;
```

```go
// Go
import "os"
dbUrl := os.Getenv("DATABASE_URL")
```

Environment variables are **loaded once** when the execution environment initializes. Updating variables requires redeployment or a configuration update — changes do not affect running environments.

## Setting Environment Variables

### Via the Console

1. Navigate to **Lambda → Functions → [your function]**
2. Click the **Configuration** tab
3. Under **Environment Variables**, click **Edit**
4. Add key-value pairs and click **Save**

### Via the API

Send a `PATCH` request to the function config endpoint:

```bash
curl -X PATCH https://api.serwin.io/api/v1/lambda/functions/my-function/config \
  -H "Authorization: Bearer <YOUR_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "env": {
      "DATABASE_URL": "postgres://user:pass@host:5432/db",
      "LOG_LEVEL": "info"
    }
  }'
```

## Reserved Variable Names

The following variable names are reserved by the Lambda runtime and **cannot** be set by users:

| Variable | Value | Description |
|----------|-------|-------------|
| `FUNCTION_NAME` | Function name | The name of the executing function |
| `FUNCTION_VERSION` | Version string | The deployed version identifier |
| `MEMORY_LIMIT_MB` | e.g. `128` | Configured memory limit in MB |
| `REGION` | e.g. `eu-north-1` | Region where the function runs |
| `PAYLOAD` | JSON string | The raw invocation event payload |
| `RUNTIME_API` | Internal URL | Lambda runtime API endpoint (internal use) |

Attempting to set a reserved variable will result in a `400 Bad Request` response with an error describing the conflict.

## Size Limits

The total size of all environment variable keys and values combined must not exceed **4 KB**. This limit applies to the serialized string representation (`KEY=VALUE\n...`).

## Encrypting Sensitive Variables

For secrets and API keys, Serwin Lambda automatically encrypts environment variables at rest using **AES-256**. However, values are **visible in plaintext** in the console and API responses to authorized users.

For more robust secret management:
- Use **Serwin Secrets Manager** and fetch secrets at runtime from within your handler (not at init time, to avoid cold start latency).
- Limit IAM permissions so that only the function's execution role can access specific secrets.

> **Best Practice:** Never log environment variables in your handler. Use structured logging with explicit field selection to avoid accidental secret exposure in log streams.

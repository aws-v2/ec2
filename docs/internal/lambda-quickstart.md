---
title: "Quickstart: Deploy a Function"
description: Deploy your first serverless function on Serwin Lambda in minutes.
icon: cpu
lastUpdated: 2026-02-27
tags:
  - Lambda
  - Quickstart
  - Deploy
---

# Quickstart: Deploy a Function

This guide walks you through creating, deploying, and invoking your first Lambda function.

## Step 1: Create a Function

In the Serwin console, navigate to **Lambda → Functions** and click **Create Function**.

Provide a name (e.g. `hello-world`) and select a runtime (e.g. **Python 3.11**).

## Step 2: Write Your Handler

The handler is the entry point Lambda calls on each invocation. It receives an `event` object (the trigger payload) and a `context` object (runtime metadata).

```python
def handler(event, context):
    return {
        "statusCode": 200,
        "body": "Hello from Lambda!"
    }
```

Save this as `handler.py` (or `main.py`, depending on your runtime configuration).

## Step 3: Deploy the Function

Upload your file via the console or using the API:

```bash
curl -X POST https://api.serwin.io/api/v1/lambda/functions \
  -H "Authorization: Bearer <YOUR_API_KEY>" \
  -F "name=hello-world" \
  -F "execution.kind=python" \
  -F "resources.cpu=1" \
  -F "resources.memory=128" \
  -F "file=@handler.py"
```

A successful response confirms deployment:

```json
{
  "message": "function registered successfully",
  "name": "hello-world"
}
```

## Step 4: Add an HTTP Trigger

Navigate to your function's **Triggers** tab and click **Add Trigger**. Select **HTTP Trigger**, configure the path (e.g. `/hello`) and method (`GET`), then save.

Lambda will generate a public URL like:

```
https://<region>.lambda.serwin.io/hello-world/hello
```

## Step 5: Invoke the Function

Invoke via the API:

```bash
curl -X POST https://api.serwin.io/api/v1/lambda/functions/hello-world/invoke \
  -H "Authorization: Bearer <YOUR_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"key": "value"}'
```

Or trigger via HTTP if you configured an HTTP trigger:

```bash
curl https://<region>.lambda.serwin.io/hello-world/hello
```

## Step 6: View Logs

Every invocation generates a log stream. Logs are available in the **Logs** tab of your function or via the monitoring endpoint:

```bash
curl https://api.serwin.io/api/v1/lambda/functions/hello-world/metrics \
  -H "Authorization: Bearer <YOUR_API_KEY>"
```

Each log entry captures the invocation timestamp, duration, status, and any `print()` / `console.log()` output from your handler.

> **Note:** Logs are retained for **30 days** by default. Configure longer retention in the function settings.

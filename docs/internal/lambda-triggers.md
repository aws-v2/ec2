---
title: HTTP Triggers (API Gateway)
description: Map HTTP routes to Lambda functions using the API Gateway trigger integration.
icon: server
lastUpdated: 2026-02-27
tags:
  - Lambda
  - Triggers
  - API Gateway
  - HTTP
---

# HTTP Triggers (API Gateway)

An **HTTP Trigger** (also called an API Gateway trigger) connects an HTTP route to your Lambda function. When a request arrives at the configured path and method, Lambda is invoked with the request details as the event payload and your function's return value is sent back as the HTTP response.

## How It Works

```
Client → HTTP Request → API Gateway → Lambda Invoke → Your Handler → Response
```

The API Gateway translates the incoming HTTP request into a structured **event object** and passes it to your handler. Your handler's return value is mapped back to an HTTP response.

## Configuring an HTTP Trigger

### Via the Console

1. Open your function → **Triggers** tab → **Add Trigger**
2. Select **HTTP Trigger**
3. Configure:
   - **Path** — e.g. `/greet` or `/users/{id}`
   - **Method** — `GET`, `POST`, `PUT`, `DELETE`, `PATCH`, or `ANY`
   - **Auth** — `None` (public) or `API Key`
4. Click **Save**

### Via the API

```bash
curl -X POST https://api.serwin.io/api/v1/lambda/functions/my-function/triggers \
  -H "Authorization: Bearer <YOUR_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "http",
    "path": "/greet",
    "method": "POST",
    "auth": "none"
  }'
```

## Event Object Shape

When your function is triggered via HTTP, the `event` parameter contains the full request details:

```json
{
  "method": "POST",
  "path": "/greet",
  "headers": {
    "content-type": "application/json",
    "authorization": "Bearer ..."
  },
  "queryStringParameters": {
    "name": "Alice"
  },
  "body": "{\"greeting\": \"hello\"}",
  "isBase64Encoded": false
}
```

## Parsing the Event in Your Handler

```python
import json

def handler(event, context):
    # Parse JSON body
    body = json.loads(event.get("body", "{}"))
    name = event.get("queryStringParameters", {}).get("name", "World")

    return {
        "statusCode": 200,
        "headers": {"Content-Type": "application/json"},
        "body": json.dumps({"message": f"Hello, {name}!", "received": body})
    }
```

## Response Format

Your handler must return a dict/object with the following structure for HTTP triggers:

| Field | Required | Description |
|-------|----------|-------------|
| `statusCode` | ✅ | HTTP status code (200, 201, 400, etc.) |
| `body` | ✅ | Response body as a string (JSON-encode objects) |
| `headers` | ❌ | Map of response headers |
| `isBase64Encoded` | ❌ | Set to `true` for binary responses |

> **Note:** If your handler returns a non-conforming response (missing `statusCode`), API Gateway will return a `502 Bad Gateway`.

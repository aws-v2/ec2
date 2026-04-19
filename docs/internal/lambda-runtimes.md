---
title: Runtimes
description: Supported language runtimes, versioning policy, and custom runtime support in Serwin Lambda.
icon: cpu
lastUpdated: 2026-02-27
tags:
  - Lambda
  - Runtimes
  - Languages
---

# Runtimes

Lambda executes your code inside a managed **runtime** — a language-specific environment that handles invocation protocol, I/O, and process lifecycle. Serwin Lambda supports several first-party runtimes and allows you to bring your own.

## Supported Runtimes

| Runtime | Language | Handler Format | Status |
|---------|----------|----------------|--------|
| `python3.11` | Python 3.11 | `module.handler` | ✅ GA |
| `nodejs20` | Node.js 20 LTS | `index.handler` | ✅ GA |
| `java21` | Java 21 (Corretto) | `com.example.Handler::handleRequest` | ✅ GA |
| `go1.21` | Go 1.21 | Binary entrypoint | ✅ GA |
| `ruby3.2` | Ruby 3.2 | `handler.handler` | ✅ GA |
| `custom` | Any | User-defined bootstrap | ✅ GA |

### Python 3.11

The default and recommended runtime for most workloads. Packages can be bundled in a zip with a `requirements.txt` or provided via Lambda Layers.

```python
def handler(event, context):
    return {"statusCode": 200, "body": "Hello from Python!"}
```

### Node.js 20

Ideal for I/O-intensive workloads and API backends. Uses the standard Node.js module system.

```javascript
exports.handler = async (event, context) => {
    return { statusCode: 200, body: "Hello from Node.js!" };
};
```

### Go 1.21

Go functions are compiled to a static binary and uploaded directly. No runtime overhead from an interpreter.

```go
func Handler(ctx context.Context, event map[string]interface{}) (string, error) {
    return "Hello from Go!", nil
}
```

### Java 21

Java functions are packaged as a JAR or ZIP containing compiled `.class` files. Cold starts are longer due to JVM initialization.

### Ruby 3.2

Ruby functions follow the same handler convention as Python. The `Gemfile` and bundled gems can be included in the deployment package.

## Runtime Versioning and Deprecation Policy

Serwin follows a structured deprecation schedule:

1. **Active** — Full support, receives security patches.
2. **Deprecated** — Announced 6 months before EOL. Functions continue to work but deployments emit a deprecation warning.
3. **End-of-Life** — Runtime is removed. Functions using the EOL runtime cannot be updated (but existing deployments continue for a 90-day grace period).

> Check the [Serwin Status Page](https://status.serwin.io) for upcoming deprecation announcements.

## Custom Runtime

If your language or version isn't listed, you can provide a **custom runtime** using a `bootstrap` executable at the root of your deployment package. Lambda will invoke `bootstrap` instead of a managed runtime process.

Your `bootstrap` must implement the **Lambda Runtime API** — a simple HTTP interface for polling events and posting responses.

```bash
# Minimal bootstrap (shell example)
#!/bin/sh
while true; do
  EVENT=$(curl -sf "http://${AWS_LAMBDA_RUNTIME_API}/2018-06-01/runtime/invocation/next")
  RESPONSE=$(echo "$EVENT" | your-handler)
  curl -X POST "http://${AWS_LAMBDA_RUNTIME_API}/2018-06-01/runtime/invocation/$(echo $REQUEST_ID)/response" \
    -d "$RESPONSE"
done
```

Set `execution.kind` to `custom` when registering a function with a custom runtime.

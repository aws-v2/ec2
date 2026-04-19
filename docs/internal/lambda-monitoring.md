---
title: Logs & Monitoring
description: Monitor invocations, view logs, track errors, and set up alerts for Lambda functions.
icon: shield
lastUpdated: 2026-02-27
tags:
  - Lambda
  - Monitoring
  - Logs
  - Metrics
---

# Logs & Monitoring

Lambda automatically captures metrics and logs for every invocation. Use the monitoring dashboard or API to diagnose issues, track performance, and set up proactive alerts.

## Key Metrics

| Metric | Description |
|--------|-------------|
| **Invocation Count** | Total number of times the function was invoked in the selected time window |
| **Duration** | Billed execution time in milliseconds (from handler start to return) |
| **Error Rate** | Percentage of invocations that ended in an error (any non-successful status) |
| **Throttle Rate** | Percentage of invocations rejected due to concurrency limits |
| **Cold Start Rate** | Percentage of invocations that required a new environment initialization |
| **Memory Usage** | Peak memory consumed during execution (MB) |

Retrieve metrics via the API:

```bash
curl https://api.serwin.io/api/v1/lambda/functions/my-function/metrics \
  -H "Authorization: Bearer <YOUR_API_KEY>"
```

```json
{
  "data": {
    "functionName": "my-function",
    "period": "1h",
    "invocations": 1240,
    "errors": 3,
    "avgDurationMs": 87,
    "throttles": 0,
    "p99DurationMs": 412
  }
}
```

## Viewing Logs

Lambda writes **log streams** — one per execution environment. Each stream contains all `print()` / `console.log()` output and runtime errors for invocations handled by that environment.

### In the Console

1. Go to **Lambda → Functions → [your function] → Logs tab**
2. Select a time range and filter by invocation ID or error status
3. Click a stream to view individual log entries

### Log Entry Structure

```
[2026-02-27T09:15:04Z] START RequestId: abc-123 Version: $LATEST
[2026-02-27T09:15:04Z] Processing order ORD-001
[2026-02-27T09:15:04Z] END RequestId: abc-123
[2026-02-27T09:15:04Z] REPORT RequestId: abc-123 Duration: 87.43 ms  Billed: 88 ms  Memory: 54 MB
```

## Error Types

| Error Type | Cause | Resolution |
|------------|-------|------------|
| **Timeout** | Handler exceeded the configured max execution time | Increase timeout or optimize handler logic |
| **Out of Memory** | Handler exceeded configured memory limit | Increase memory limit (also increases vCPU) |
| **Runtime Error** | Unhandled exception in handler code | Check logs for stack trace, fix the bug |
| **Handler Crash** | Process exited unexpectedly (SIGSEGV, OOM killer) | Check for infinite loops, memory leaks |
| **Init Error** | Crash during the initialization (Init) phase | Ensure module-level code doesn't throw |
| **Throttle** | Concurrent invocations exceeded the limit | Request a concurrency limit increase |

## Setting Up Alerts

Configure alerts to notify you when error rate or throttle rate exceeds a threshold:

```bash
curl -X POST https://api.serwin.io/api/v1/lambda/functions/my-function/alerts \
  -H "Authorization: Bearer <YOUR_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "metric": "error_rate",
    "threshold": 5,
    "comparison": "GREATER_THAN",
    "period": "5m",
    "notification": {
      "channel": "email",
      "destination": "ops@example.com"
    }
  }'
```

Supported channels: `email`, `webhook`, `pagerduty`.

> **Best Practice:** Set an alert on `error_rate > 1%` and `p99_duration > 1000ms` for production functions. This catches most regressions within minutes of deployment.

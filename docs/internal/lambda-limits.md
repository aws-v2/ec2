---
title: Quotas & Limits
description: Default service quotas for Serwin Lambda and how to request increases.
icon: shield
lastUpdated: 2026-02-27
tags:
  - Lambda
  - Limits
  - Quotas
---

# Quotas & Limits

Serwin Lambda enforces service quotas to ensure fair resource allocation across all customers. Most limits can be increased on request.

## Default Quotas

| Limit | Default Value |
|-------|---------------|
| **Max execution timeout** | 15 minutes |
| **Max memory per function** | 10,240 MB |
| **Max deployment package (zipped)** | 50 MB |
| **Max deployment package (unzipped)** | 250 MB |
| **Max container image size** | 10 GB |
| **Concurrent executions (account)** | 1,000 |
| **Concurrent executions (per function)** | 100 (unreserved pool) |
| **Max environment variables size** | 4 KB |
| **Max layers per function** | 5 |
| **Max layer size (unzipped)** | 250 MB (shared with function) |
| **Max function name length** | 64 characters |
| **Max invocation payload (sync)** | 6 MB |
| **Max invocation payload (async)** | 256 KB |
| **Max response payload (sync)** | 6 MB |

## Concurrency Explained

**Concurrency** is the number of function invocations running simultaneously at any point in time. Lambda automatically manages concurrency scaling:

- If 100 requests arrive simultaneously, Lambda spins up 100 execution environments (subject to the concurrency limit).
- Requests beyond the concurrent limit are **throttled** — they receive a `429 TooManyRequestsException`.
- You can reserve concurrency for a specific function to prevent it from consuming the shared pool.

### Reserved vs Unreserved Concurrency

| Type | Description |
|------|-------------|
| **Reserved** | Dedicated concurrency for a function. Guarantees availability. |
| **Unreserved** | Shared pool. Function uses what's available up to the regional limit. |

## Timeout Behavior

When a function exceeds its configured timeout:

1. Lambda sends a `SIGTERM` signal to the process.
2. If the process doesn't exit within 2 seconds, Lambda sends `SIGKILL`.
3. The invocation is marked as a **timeout error**.
4. The metric `errors` is incremented and the error type is logged as `Task timed out`.

## How to Request Limit Increases

To request a quota increase, submit a support ticket:

1. Navigate to **Account → Support → New Request**
2. Select **Service Quota Increase**
3. Choose **Lambda** and specify:
   - The limit type (e.g. Concurrent Executions)
   - The requested value
   - The business justification
4. Submit — typical response time is **1–2 business days**

> **Note:** Memory limit increases above 10,240 MB and timeout increases beyond 15 minutes are not available and cannot be requested. For workloads exceeding these bounds, consider **EC2** or **Fargate** as alternatives.

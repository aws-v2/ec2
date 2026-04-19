---
title: Lambda Overview
description: Run code without provisioning or managing servers — event-driven serverless execution on Serwin.
icon: cpu
lastUpdated: 2026-02-27
tags:
  - Lambda
  - Serverless
  - Functions
---

# Lambda Overview

Serwin Lambda lets you run code in response to events without provisioning or managing servers. You upload your function, configure a trigger, and Lambda handles the rest — scaling automatically from a few requests per day to thousands per second.

## Key Concepts

| Concept | Description |
|---------|-------------|
| **Function** | The unit of deployment. A function is your code, packaged for Lambda to execute. |
| **Handler** | The entry point within your function code that Lambda calls when invoked. |
| **Trigger** | An event source that invokes your function (e.g. HTTP request, queue message, schedule). |
| **Event** | A JSON document passed to your handler containing the event data from the trigger. |
| **Runtime** | The language execution environment (Python 3.11, Node.js 20, etc.) that runs your handler. |

## Cold Start vs Warm Start

When Lambda receives a request, it must first prepare the execution environment for your function.

**Cold Start** occurs when Lambda initializes a new execution environment from scratch:

1. Downloads your function code/container image
2. Starts the runtime process
3. Runs any initialization code outside your handler
4. Executes your handler

Cold starts typically add **100ms–2s** of latency and happen when:
- Your function is invoked for the first time
- A new concurrency slot is needed during a traffic spike
- The function has been idle for an extended period

**Warm Start** occurs when an existing execution environment handles the request. Lambda reuses the environment (process, memory, temp files) so only your handler code runs. Warm starts are significantly faster — usually under **10ms** of overhead.

> **Tip:** To reduce cold starts, keep your deployment package small, avoid heavy initialization logic at module load time, and use **Provisioned Concurrency** for latency-sensitive workloads.

## When to Use Lambda vs EC2

| Scenario | Lambda | EC2 |
|----------|--------|-----|
| Short-lived tasks (< 15 min) | ✅ Ideal | Overkill |
| Event-driven workloads | ✅ Ideal | Complex setup |
| Unpredictable / spiky traffic | ✅ Auto-scales | Manual scaling |
| Long-running processes (> 15 min) | ❌ Not supported | ✅ Ideal |
| Stateful applications, persistent disk | ❌ Ephemeral only | ✅ Ideal |
| Custom OS / kernel configuration | ❌ Not supported | ✅ Ideal |
| Always-on services | More expensive | ✅ Cost-effective |

Lambda is best for **glue code**, **API backends**, **data processing pipelines**, and **event-driven automation**. Use EC2 when you need persistent state, long-running processes, or full OS control.

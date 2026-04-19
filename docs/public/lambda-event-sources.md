---
title: Event Sources
description: Connect Lambda functions to S3, message queues, scheduled crons, and other event sources.
icon: server
lastUpdated: 2026-02-27
tags:
  - Lambda
  - Event Sources
  - S3
  - Queues
  - Cron
---

# Event Sources

Lambda functions can be triggered by a variety of **event sources** beyond HTTP requests. Event source mappings automatically poll or listen to external services and invoke your function when new data or events arrive.

## S3 Event Source

Trigger your function whenever an object is uploaded (or deleted) in an S3 bucket.

**Use cases:** image processing, log parsing, ETL pipelines, virus scanning on upload.

### Configuration

```bash
curl -X POST https://api.serwin.io/api/v1/lambda/functions/my-function/triggers \
  -H "Authorization: Bearer <YOUR_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "s3",
    "bucket": "my-uploads-bucket",
    "events": ["s3:ObjectCreated:*"],
    "prefix": "uploads/"
  }'
```

### Event Shape

```json
{
  "source": "s3",
  "bucket": "my-uploads-bucket",
  "key": "uploads/photo.jpg",
  "size": 204800,
  "etag": "d41d8cd98f00b204e9800998ecf8427e",
  "eventTime": "2026-02-27T09:00:00Z",
  "eventType": "ObjectCreated:Put"
}
```

## Message Queue Event Source

Poll a message queue (e.g. Serwin MQ or SQS-compatible service) and invoke your function for each batch of messages.

**Use cases:** order processing, email dispatch, background job execution.

### Configuration

```bash
curl -X POST https://api.serwin.io/api/v1/lambda/functions/my-function/triggers \
  -H "Authorization: Bearer <YOUR_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "queue",
    "queueUrl": "https://mq.serwin.io/accounts/123/queues/orders",
    "batchSize": 10,
    "visibilityTimeout": 30
  }'
```

### Event Shape

```json
{
  "source": "queue",
  "records": [
    {
      "messageId": "abc-123",
      "body": "{\"orderId\": \"ORD-001\"}",
      "attributes": { "sentTimestamp": "1709028000000" }
    }
  ]
}
```

## Scheduled (Cron) Triggers

Invoke your function on a time-based schedule using cron expressions or rate expressions.

**Use cases:** daily reports, database cleanup, health checks, cache warming.

### Configuration

```bash
curl -X POST https://api.serwin.io/api/v1/lambda/functions/my-function/triggers \
  -H "Authorization: Bearer <YOUR_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "schedule",
    "expression": "cron(0 8 * * ? *)",
    "description": "Run every day at 8:00 AM UTC"
  }'
```

Rate expressions are also supported:

```json
{ "expression": "rate(5 minutes)" }
```

### Event Shape

```json
{
  "source": "scheduler",
  "triggeredAt": "2026-02-27T08:00:00Z",
  "scheduleExpression": "cron(0 8 * * ? *)"
}
```

## Event Source Mapping and Filtering

For queue and stream-based sources, you can configure **batch size**, **starting position**, and **filter criteria** to control which events invoke your function.

### Filter Example (Queue)

Only invoke the function for messages where the `eventType` field equals `"order.created"`:

```json
{
  "type": "queue",
  "queueUrl": "https://mq.serwin.io/accounts/123/queues/orders",
  "batchSize": 5,
  "filterCriteria": {
    "filters": [
      { "pattern": "{ \"eventType\": [\"order.created\"] }" }
    ]
  }
}
```

Messages that don't match the filter are **not** forwarded to your function and remain in the queue until their retention period expires (unless explicitly deleted).

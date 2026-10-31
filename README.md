# ec2

## Prometheus metrics

The service exposes Prometheus metrics on port `7085` at `/metrics`. The endpoint
requires the `X-Api-Key` header matching `METRICS_API_KEY`; configure Prometheus
or a trusted scrape proxy to send that header. For a direct check:

```sh
curl -H "X-Api-Key: $METRICS_API_KEY" http://localhost:7085/metrics
```

EC2-specific metrics include:

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `ec2_instances` | Gauge | `status`, `profile` | Instance counts, refreshed from PostgreSQL once per minute. |
| `ec2_instance_create_total` | Counter | `profile`, `result` | Provisioning attempts, with `success` or `failed` result. |
| `ec2_instance_create_duration_seconds` | Histogram | `profile` | End-to-end instance provisioning duration. |
| `ec2_ami_build_total` | Counter | `result` | AMI builds, with `success` or `failed` result. |
| `ec2_ami_build_duration_seconds` | Histogram | — | AMI build duration. |
| `ec2_ami_build_progress_percent` | Gauge | `ami_id` | Progress of active AMI builds; the series is removed at completion. |
| `ec2_gateway_ports` | Gauge | `status` | Gateway port counts, refreshed from PostgreSQL once per minute. |
| `ec2_agent_call_errors_total` | Counter | `action`, `host` | Failed calls to host agents. |

The service also exports `http_requests_total`, `http_request_duration_seconds`,
and PostgreSQL connection-pool metrics.

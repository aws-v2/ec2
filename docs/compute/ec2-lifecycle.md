# Instance Lifecycle

EC2 instances pass through a series of states during their lifetime. Understanding these states is key to managing your compute resources effectively.

## Instance States

| State | Description |
|---|---|
| `pending` | The instance is being provisioned and initialized. Not yet accessible. |
| `running` | The instance is active and reachable. Normal operating state. |
| `stopping` | A stop action has been issued. The instance is gracefully shutting down. |
| `stopped` | The instance is powered off. Storage is preserved; compute billing pauses. |
| `terminated` | The instance has been permanently destroyed. Cannot be recovered. |
| `error` | The instance encountered a failure during startup or operation. |

## State Transition Diagram

```
         ┌─────────┐
         │ pending │ ── (boot fails) ──► error
         └────┬────┘
              │ (ready)
              ▼
         ┌─────────┐       stop        ┌─────────┐
         │ running │ ─────────────────► stopping │
         └─────────┘                   └────┬────┘
              ▲                             │
              │ start                       ▼
         ┌─────────┐                  ┌─────────┐
         │ stopped │◄─────────────────┤ stopped │
         └─────────┘                  └─────────┘
              │
              │ terminate
              ▼
        ┌────────────┐
        │ terminated │
        └────────────┘
```

## API Actions

### Start an Instance

Boots a stopped instance. Transitions: `stopped` → `pending` → `running`.

```bash
POST /api/v1/ec2/instances/:id/start
```

### Stop an Instance

Gracefully powers off a running instance. Transitions: `running` → `stopping` → `stopped`.

```bash
POST /api/v1/ec2/instances/:id/stop
```

### Restart an Instance

Performs a graceful reboot without changing state from `running`.

```bash
POST /api/v1/ec2/instances/:id/restart
```

### Terminate an Instance

**Permanently deletes** the instance and its root disk. This action is irreversible.

```bash
DELETE /api/v1/ec2/instances/:id
```

> ⚠️ **Warning**: Terminating an instance will delete its root disk. Ensure you have taken a snapshot before terminating if you need to preserve data.

## Stopped vs Terminated

| Aspect | Stopped | Terminated |
|---|---|---|
| Root disk preserved | ✅ Yes | ❌ No |
| Can be restarted | ✅ Yes | ❌ No |
| Associated volumes | Preserved | Detached |
| Recoverable | ✅ Yes | ❌ No |

## Instance Status Checks

Once an instance enters the `running` state, two health checks are performed:

- **System Check**: Validates the underlying host hypervisor system.
- **Instance Check**: Validates the VM is responding to network checks.

```bash
GET /api/v1/ec2/instances/:id/status-checks
```

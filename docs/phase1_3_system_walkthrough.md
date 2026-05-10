# Phase 1-3 Complete: What Changed and How the System Works

## What Was Broken

### Problem 1: 1.8GB Full Image Transfer (gamelift)
The `gamelift` profile was explicitly **excluded** from the template check. This meant even when the Agent had `ubuntu-22.04` cached locally, the Orchestrator would:
1. Inject the game payload into the overlay
2. Then flatten the overlay (qemu-img convert) into a full 1.8GB standalone image
3. Transfer the full 1.8GB image to the Agent

**Root cause:** `if profile != "gamelift"` guard in `instance_lifecycle.go` line ~184.

### Problem 2: Bridge Does Not Exist on Agent
The Libvirt network bridge (e.g. `vbr-90330d67-9a`) was created only on the Orchestrator during VPC allocation. When the Agent's Libvirt tried to start the VM and attach its NIC to this bridge, it didn't exist — causing:
```
virError(Code=38): Cannot get interface MTU on 'vbr-90330d67-9a': No such device
```

---

## What Was Fixed

### Fix 1 — Delta Transfer for ALL Profiles
**File:** `internal/application/instance_lifecycle.go`

Removed the `if profile != "gamelift"` guard. Now for **every** profile:
1. Check if the Agent has the base template (e.g. `ubuntu-22.04`)
2. **If yes** → send only the thin overlay (contains injected payload, ~50-100MB)
3. **If no** → bake a full standalone image (fallback for Agents without the template)

The game payload injected in Phase 1 lives in the overlay. The overlay does not contain the OS. The rebase in Phase 2 links the overlay to the Agent's local cached OS. There is no need to bundle the OS for transfer.

### Fix 2 — Remote Bridge Creation via Libvirt API
**File:** `internal/infra/libvirt/libvirt_client.go`

After connecting to the Agent's Libvirt (via `qemu+ssh://`), and before calling `DomainDefineXML`, the Orchestrator now:
1. Calls `remoteConn.LookupNetworkByName(bridgeName)`
2. If the network doesn't exist → defines it with `NetworkDefineXML` → starts it with `net.Create()`
3. If it already exists but is inactive → starts it
4. Marks it autostart so it survives Agent reboots
5. Then proceeds to define and start the VM

No changes to the Agent are needed. Everything is done through the existing libvirt+ssh tunnel.

---

## How the System Now Works (End-to-End)

```
PHASE 1 — Local Orchestration (Orchestrator)
  1. qemu-img create -f qcow2 -b ubuntu-22.04.qcow2 overlay.qcow2   # thin ~200KB
  2. guestmount overlay → inject game binary into /opt/game/run       # overlay grows to ~50MB
  3. Template check: Agent has ubuntu-22.04? YES → skip baking
  LOG: "-------------------end of phase one-----"

PHASE 2 — The Hand-off (Network)
  4. ssh mkdir -p /var/lib/libvirt/images (on Agent)
  5. rsync overlay.qcow2 → Agent (50MB instead of 1.8GB) 🚀
  6. ssh: qemu-img rebase -u -b /var/lib/libvirt/templates/ubuntu-22.04.qcow2 overlay.qcow2
     → links overlay's internal path to Agent's local golden image
  7. rsync cloud-init.iso → Agent (network config, SSH keys, startup scripts)
  LOG: "-------------------end of phase two-----"

PHASE 3 — Execution (Agent)
  8. libvirt connect: qemu+ssh://user@agent/system?keyfile=...
  9. LookupNetworkByName(vbr-xxx) → not found → NetworkDefineXML → net.Create()
     → bridge now exists on Agent
 10. DomainDefineXML(vmXML) → registers VM in Agent's Libvirt
 11. domain.Create() → QEMU boots the VM
     → reads /sbin/init: not in overlay → fetches from local golden image (no network!)
     → reads /opt/game/run: found in overlay → served instantly
 12. Cloud-init applies static IP, SSH keys, starts game server
 13. Instance status → RUNNING ✅
```

---

## Transfer Size Comparison

| Before | After |
|---|---|
| ~1,800 MB (flattened full image) | ~50-100 MB (overlay with payload only) |
| Includes full Ubuntu OS in transfer | OS stays on Agent, never transferred |
| ~40 min transfer time | ~1-2 min transfer time |

---

## Files Changed

| File | Change |
|---|---|
| `internal/application/instance_lifecycle.go` | Removed `if profile != "gamelift"` guard. All profiles now use overlay+template path. |
| `internal/infra/libvirt/libvirt_client.go` | Added bridge creation via Libvirt API after remote connect, before DomainDefineXML. |

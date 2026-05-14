# Phase 3: Execution (The Worker)

## What Phase 3 Is

After Phase 1 (local orchestration — inject game files into overlay) and Phase 2 (transfer the overlay + remote rebase) are complete, **Phase 3 is the moment the Agent actually runs the VM**.

The Agent doesn't know anything about game files, cloud-init, or overlays. It just sees:
- A disk file: `vm-i-xxxxxxxx.qcow2` (the overlay, now pointing at its local golden image)
- A cloud-init ISO: `vm-i-xxxxxxxx-cloudinit.iso` (with SSH keys, static IP, startup scripts)

Its job: **boot the VM**.

---

## Phase 3 Flow (What Should Happen)

```
Orchestrator                            Agent (100.71.223.121)
     │                                          │
     ├─── rsync delta disk ──────────────────► /var/lib/libvirt/images/vm-xxx.qcow2
     ├─── rsync cloud-init ISO ─────────────► /var/lib/libvirt/images/vm-xxx-cloudinit.iso
     ├─── ssh: qemu-img rebase ─────────────► links delta → /var/lib/libvirt/templates/ubuntu-22.04.qcow2
     ├─── [ENSURE BRIDGE EXISTS] ──────────► vbr-90330d67-9a (bridge on Agent)
     ├─── libvirt: DomainDefineXML ─────────► creates VM definition
     └─── libvirt: domain.Create() ─────────► boots the VM 🚀
```

---

## Current Status

| Step | Status |
|---|---|
| rsync delta to Agent | ✅ Working |
| rsync cloud-init ISO | ✅ Working |
| qemu-img rebase on Agent | ✅ Working |
| Ensure bridge exists on Agent | ❌ Missing — causes the error |
| DomainDefineXML | ❌ Fails (no bridge) |
| domain.Create() | ❌ Not reached |

---

## The Blocker: Bridge Does Not Exist on Agent

### Error
```
virError(Code=38, Message='Cannot get interface MTU on 'vbr-90330d67-9a': No such device')
```

### Why It Happens
The Orchestrator allocated a VPC and created a Linux bridge (`vbr-90330d67-9a`) **on itself**. The VM XML was baked referencing this bridge name. But the Agent host has never created this bridge — Libvirt on the Agent tries to attach the VM NIC to it and fails immediately.

### Why It's Phase 3
This is a **startup** problem, not a transfer problem. Phase 1 and 2 are about getting the right files to the right place. Phase 3 is about making the environment on the Agent ready to actually run the VM.

---

## Implementation Plan

### Step 1 — Create the Bridge on the Agent Before Starting the VM

In `internal/infra/libvirt/libvirt_client.go` inside `CreateAndStartVM`, **between** the ISO transfer and the Libvirt `DomainDefineXML` call, add bridge creation using the Libvirt network API (no sudo required):

```go
// ── Phase 3: Ensure bridge/network exists on Agent ────────────────────────
if bridgeName != "" {
    fmt.Printf("[Libvirt] Ensuring network bridge %s exists on Agent...\n", bridgeName)

    // Check if network already exists
    existingNet, err := conn.LookupNetworkByName(bridgeName)
    if err != nil {
        // Doesn't exist — define and start it
        netXML := fmt.Sprintf(`
<network>
  <name>%s</name>
  <bridge name='%s' stp='off' delay='0'/>
</network>`, bridgeName, bridgeName)

        newNet, err := conn.NetworkDefineXML(netXML)
        if err != nil {
            return 0, fmt.Errorf("failed to define network %s on agent: %w", bridgeName, err)
        }
        defer newNet.Free()

        if err := newNet.Create(); err != nil {
            return 0, fmt.Errorf("failed to start network %s on agent: %w", bridgeName, err)
        }
        newNet.SetAutostart(true)
        fmt.Printf("[Libvirt] Network bridge %s created and started on Agent\n", bridgeName)
    } else {
        defer existingNet.Free()
        if active, _ := existingNet.IsActive(); !active {
            existingNet.Create()
        }
        fmt.Printf("[Libvirt] Network bridge %s already exists on Agent\n", bridgeName)
    }
}
// ── End Phase 3 bridge setup ───────────────────────────────────────────────
```

> Insert this **after** the `remoteConn, err := libvirt.NewConnect(remoteURI)` call, and **before** `remoteConn.DomainDefineXML(xmlConfig)`.

### Step 2 — Where Exactly in the Code

Current code flow at the bottom of the remote section in `CreateAndStartVM`:

```go
// Connect to remote Libvirt  ← line ~163
remoteURI := fmt.Sprintf("qemu+ssh://...")
remoteConn, err := libvirt.NewConnect(remoteURI)
...

// ── INSERT BRIDGE SETUP HERE ──

domain, err := remoteConn.DomainDefineXML(xmlConfig)   ← currently failing
...
domain.Create()
```

### Step 3 — No Changes Needed to Agent

The Agent only needs:
- `libvirt-bin` running (already true — it sends heartbeats)
- The SSH user (`x6617274696`) able to connect to the Agent's Libvirt socket

The Orchestrator manages the bridge entirely through the existing libvirt+ssh connection. The Agent doesn't need any new code.

---

## After the Fix

Once the bridge is created on the Agent, the full Phase 3 sequence will complete:

1. `DomainDefineXML` → registers the VM in Agent's Libvirt
2. `domain.Create()` → QEMU boots the VM
3. VM reads `/sbin/init` → not in delta → QEMU fetches from golden image `/var/lib/libvirt/templates/ubuntu-22.04.qcow2`
4. VM reads `/opt/game/run` → found in delta → served instantly
5. Cloud-init applies static IP, SSH keys, and startup scripts
6. Instance status becomes `RUNNING` ✅

---

## Idempotency Note

The bridge creation call should be **idempotent** — if 10 VMs are provisioned to the same Agent in the same VPC, only the first one creates the bridge. All subsequent ones will find it already active via `LookupNetworkByName` and skip creation. This is handled by the `if err != nil` check above (Libvirt returns an error when the network doesn't exist).

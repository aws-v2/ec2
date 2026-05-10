# Phase 3: Remote Bridge Network Provisioning

## The Error

```
virError(Code=38, Message='Cannot get interface MTU on 'vbr-90330d67-9a': No such device')
```

Libvirt on the Agent is trying to attach the VM to the bridge `vbr-90330d67-9a`, but that bridge **does not exist** on the Agent host. The orchestrator created the bridge locally on itself, and the VM XML was baked with that bridge name — but the Agent has never seen it.

---

## Root Cause Analysis

The provisioning flow currently:
1. The Orchestrator allocates a VPC/network slot → creates a Linux bridge `vbr-<vpc-id>` **on itself**
2. The VM disk + cloud-init are transferred to the Agent
3. The VM XML references `vbr-90330d67-9a`
4. The Agent's Libvirt tries to start the VM → **fails** because the bridge doesn't exist there

---

## The Fix: Remote Bridge Creation via SSH

Before defining the VM in Libvirt on the Agent, the Orchestrator must ensure the required bridge exists on the Agent.

### Step 1: Add Bridge Setup to `CreateAndStartVM`

In `libvirt_client.go`, inside the `if remoteHostIP != ""` block, **after** the disk/ISO transfer but **before** connecting to remote Libvirt to define the VM, add:

```go
// Ensure bridge exists on remote host
fmt.Printf("[Libvirt] Ensuring bridge %s exists on remote host...\n", bridgeName)
bridgeSetupCmd := exec.Command("ssh", append(sshArgs,
    fmt.Sprintf("%s@%s", remoteHostUser, remoteHostIP),
    "bash", "-c",
    fmt.Sprintf(`
        if ! ip link show %s > /dev/null 2>&1; then
            sudo ip link add name %s type bridge
            sudo ip link set %s up
        fi
    `, bridgeName, bridgeName, bridgeName),
)...)
if output, err := bridgeSetupCmd.CombinedOutput(); err != nil {
    return 0, fmt.Errorf("failed to create bridge %s on remote host (output: %s): %w", bridgeName, string(output), err)
}
fmt.Printf("[Libvirt] Bridge %s ready on remote host\n", bridgeName)
```

> **Note:** The Agent user (`x6617274696`) needs `sudo` access for `ip link`. Either add a sudoers rule on the Agent or create the bridge as root.

### Step 2: Agent Sudoers Rule

On the Agent host, add a sudoers file so the user can create bridges without a password:

```bash
# /etc/sudoers.d/libvirt-bridge
x6617274696 ALL=(ALL) NOPASSWD: /sbin/ip link *
x6617274696 ALL=(ALL) NOPASSWD: /usr/sbin/ip link *
```

Or do it once:
```bash
echo "x6617274696 ALL=(ALL) NOPASSWD: /sbin/ip link *, /usr/sbin/ip link *" | \
  sudo tee /etc/sudoers.d/libvirt-bridge
```

---

## Alternative: Use Libvirt Network on Agent (Cleaner)

Instead of raw `ip link`, use Libvirt's own network management via the existing `getConnection()` call. After connecting to the Agent's Libvirt, define the bridge as a Libvirt network:

```go
// Check or create libvirt network for this bridge
libvirtBridgeXML := fmt.Sprintf(`
<network>
  <name>%s</name>
  <bridge name='%s' stp='off' delay='0'/>
  <ip address='%s' prefix='24'>
  </ip>
</network>`, bridgeName, bridgeName, gateway)

// Try to lookup network first
libvirtNet, err := conn.LookupNetworkByName(bridgeName)
if err != nil {
    // Network doesn't exist — create and start it
    libvirtNet, err = conn.NetworkDefineXML(libvirtBridgeXML)
    if err != nil {
        return 0, fmt.Errorf("failed to define network %s on agent: %w", bridgeName, err)
    }
}
defer libvirtNet.Free()

if active, _ := libvirtNet.IsActive(); !active {
    if err := libvirtNet.Create(); err != nil {
        return 0, fmt.Errorf("failed to start network %s on agent: %w", bridgeName, err)
    }
}
```

> This approach is cleaner because Libvirt manages the bridge lifecycle, and `getConnection()` already gives you 
> a `*libvirt.Connect` to the Agent.

---

## Where to Put the Code

File: `internal/infra/libvirt/libvirt_client.go`  
Function: `CreateAndStartVM`

Location in the function: **after** ISO rsync, **before** the `DomainDefineXML` call.

Current flow:
```
1. Create cloud-init ISO
2. ssh mkdir remote dirs           ← already done
3. rsync disk                      ← already done
4. qemu-img rebase (if delta)      ← already done
5. rsync ISO                       ← already done
6. ── INSERT BRIDGE SETUP HERE ──
7. conn.NetworkDefineXML / ip link
8. conn.DomainDefineXML            ← currently failing
9. domain.Create()
```

---

## What the Agent Needs to Have

| Requirement | Status |
|---|---|
| `qemu-img` installed | ✅ Already present (Libvirt dependency) |
| `rsync` installed | May need: `sudo apt install rsync` |
| SSH public key in `authorized_keys` | ✅ Done |
| `/var/lib/libvirt/images` writable | ✅ Done (chown fixed) |
| `/var/lib/libvirt/templates` with golden images | ✅ Required for Phase 2 rebase |
| Libvirt running and accessible | ✅ Already working (agent heartbeats) |
| Sudoers for `ip link` | ❗ Needed for bridge creation |

---

## Recommended Implementation Order

1. **Connect to Agent Libvirt first** (already happening via `getConnection`)
2. **Use Libvirt API to lookup/create the network on the Agent**  
   (`conn.LookupNetworkByName` → if not found → `conn.NetworkDefineXML` → `net.Create()`)
3. **Then define and start the VM** (`conn.DomainDefineXML` → `domain.Create()`)

This keeps everything in the Libvirt API without requiring `sudo` on the Agent, and the bridge will be persistent across reboots if you call `net.SetAutostart(true)`.

---

## Additional Note: VPC Creation on Agent

Consider adding a **new heartbeat field** where the Agent reports existing Libvirt networks:

```go
type HeartbeatRequest struct {
    // ... existing fields
    AvailableNetworks []string `json:"available_networks"` // e.g. ["vbr-90330d67-9a"]
}
```

This would let the Orchestrator know **not** to recreate networks that already exist, making future provisioning even faster.

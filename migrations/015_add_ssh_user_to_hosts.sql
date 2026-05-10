-- migration 015: Add ssh_user column to hosts table
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS ssh_user TEXT;





-- To explain the Overlay (Delta Disk) process in depth, it helps to think of it like a "Transparent Layer" placed over a "Master Book."

-- 1. The Core Concept: Parent and Child
-- A qcow2 file has a special feature called a Backing File.

-- The Parent (Golden Image): This is your 700MB ubuntu-22.04.qcow2. It is kept in a "Read-Only" state.
-- The Child (Overlay): This is a new, empty file that points to the Parent. Every time the VM tries to read something, it looks at the Child first; if it’s not there, it reads from the Parent. Every time the VM writes something, it only goes into the Child.
-- 2. The Deep-Dive Process
-- Phase 1: Local Orchestration (The "Brain")
-- On your Orchestrator server, where you have the game files:

-- Create Overlay: You run qemu-img create -f qcow2 -b ubuntu-22.04.qcow2 delta.qcow2. This file is less than 200 KB initially.
-- Inject Payload: You use guestmount to "open" delta.qcow2. You copy your 50MB of game files into it.
-- Result: delta.qcow2 is now roughly 55MB. It contains only the new game files and some filesystem metadata. It does not contain the Ubuntu OS code.
-- Phase 2: The Hand-off (The Network)
-- Transfer: You scp only the 55MB delta.qcow2 to the Agent.
-- The Metadata Problem: Inside delta.qcow2, there is a "pointer" that says: "My parent is located at /home/martin/images/ubuntu-22.04.qcow2" (the path on your Orchestrator).
-- The Fix (Rebase): On the Agent, that path doesn't exist. So, the Orchestrator sends one final command to the Agent via SSH: qemu-img rebase -u -b /var/lib/libvirt/templates/ubuntu-22.04.qcow2 delta.qcow2
-- This is "Unsafe Rebase" (-u), which just means: "Don't check anything, just change the internal text pointer to point to the Agent's local template path."


-- Phase 3: Execution (The Worker)
-- Start VM: Libvirt starts the VM using delta.qcow2 as the primary disk.
-- Transparency: When the VM boots:
-- It asks for 

-- /sbin/init
-- . QEMU sees this isn't in the delta file, so it fetches it from the local Golden Image.
-- It asks for /opt/game/run. QEMU finds this in the delta file and serves it instantly.
-- Why this addresses your "No Agent Orchestration" rule:
-- You are not asking the Agent to "manage" anything. You are just using the Agent's existing qemu-img tool (which is already there because of Libvirt) to fix a file path.

-- The Agent doesn't need to know:

-- What OS is inside.
-- What payload was injected.
-- How to "provision" a game.
-- It just sees a disk file and is told: "Your parent is over there."

-- How does this sound for a "Mental Model"?
-- Instead of sending the whole house (OS + Game) every time, you keep the foundation (Ubuntu) on the Agent and only send the furniture (the Game files) in a small box.
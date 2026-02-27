To completely remove the VMs and their associated images from your system using `virsh`, you can follow these steps:

### 1. **Shut down the VMs**

First, ensure the VMs are stopped. You can do this using `virsh shutdown` or `virsh destroy` (if the VM is not responding to shutdown).

```bash
sudo virsh shutdown vm-i-26a283c5
sudo virsh shutdown vm-i-228e6a4e
```

If they don’t shut down gracefully, you can force stop them with `destroy`:

```bash
sudo virsh destroy vm-i-26a283c5
sudo virsh destroy vm-i-228e6a4e
```

### 2. **Delete the Virtual Machines**

Once the VMs are stopped, you can delete them using `virsh undefine`. This will remove the VM definition, but **won’t delete the associated disk images**.

```bash
sudo virsh undefine vm-i-26a283c5
sudo virsh undefine vm-i-228e6a4e
```

If you want to remove the VM's configuration file as well, you can use the `--remove-all-storage` flag:

```bash
sudo virsh undefine vm-i-26a283c5 --remove-all-storage
sudo virsh undefine vm-i-228e6a4e --remove-all-storage
```

This will delete the VM configuration and also remove the associated storage (disk images).

### 3. **Delete the Disk Images**

The disk images are typically stored in `/var/lib/libvirt/images/` unless specified otherwise. To ensure that all the associated disk images are removed, you can manually delete them.

Check the disk images:

```bash
ls /var/lib/libvirt/images/
```

You should see the images associated with the VMs (e.g., `vm-i-26a283c5.qcow2`, `vm-i-228e6a4e.qcow2`). You can delete them using `rm`:

```bash
sudo rm /var/lib/libvirt/images/vm-i-26a283c5.qcow2
sudo rm /var/lib/libvirt/images/vm-i-228e6a4e.qcow2
```

Make sure that the disk images are indeed associated with these VMs before deleting them.

### 4. **Clean Up Networks (optional)**

If you've created custom networks for these VMs, you might want to clean those up as well. You can list and remove the networks:

```bash
sudo virsh net-list --all
```

To remove a network:

```bash
sudo virsh net-destroy <network_name>
sudo virsh net-undefine <network_name>
```

### 5. **Remove Cloud-init ISOs (if applicable)**

If you're using custom cloud-init ISOs for these VMs, ensure you remove them as well.

Check where your cloud-init ISOs are stored, and delete them:

```bash
sudo rm /path/to/cloud-init.iso
```

### 6. **Double-check (optional)**

Finally, you can run the following to check if everything is gone:

```bash
sudo virsh list --all
```

And verify that the images are indeed removed:

```bash
ls /var/lib/libvirt/images/
```

If everything looks good, you should be able to start fresh without any remnants of the old VMs.

---

This should effectively remove all traces of the VMs and their associated images. Let me know if you need more guidance!

sudo apt install qemu-kvm libvirt-daemon-system libvirt-clients virtinst
sudo usermod -aG libvirt $USER




sudo apt-get update
sudo apt-get install libvirt-dev pkg-config




sudo usermod -aG libvirt $USER
sudo usermod -aG kvm $USER






Setup base images: when youw ant to use this mothod
Option 1 - Pre-downloaded images: Store images locally and reference them:





# Check key services
sudo systemctl status libvirtd
sudo systemctl status systemd-machined
sudo systemctl status dbus

# Start if stopped
sudo systemctl start libvirtd systemd-machined

# Check your Go API
ps aux | grep "go run"






You can verify the VM is running:
sudo virsh list
sudo virsh domifaddr vm-i-90e2879c






https://claude.ai/chat/3fc6a949-8629-4fa1-8a08-ebc136c1b527




























Check VM Network Status
1. Wait a bit longer and check again:
bash# Wait 30-60 seconds, then check
sudo virsh domifaddr vm-i-90e2879c

# Or check DHCP leases directly
sudo virsh net-dhcp-leases default
2. Check if the VM console shows boot progress:
sudo virsh console vm-i-90e2879c
# Press Ctrl+] to exit console
3. Verify network interface is connected:
sudo virsh domiflist vm-i-90e2879c
4. Check dnsmasq DHCP logs:
sudo journalctl -u libvirtd | grep -i dhcp | tail -20
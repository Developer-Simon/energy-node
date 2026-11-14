#!/bin/bash
# verify.sh <run> <port>: boot the installed disk(s) of a finished run without the stick and check it over SSH.
# Needs the run directory from run.sh and the installer test keys in $EN_QEMU_KEY_DIR.
set -u
work=${EN_QEMU_WORK_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/energy-node-qemu}
EN_QEMU_KEY_DIR=${EN_QEMU_KEY_DIR:-$work/keys}
R=$work/runs/$1
port=$2
cmd=$(cat "$R/cmd.txt")
# drop the stick, boot from disk, add an ssh port forward
cmd=$(echo "$cmd" | sed -E 's# -drive if=none,id=stick[^ ]*##; s# -device usb-storage,bus=xhci.0,drive=stick,bootindex=0##;
	s#-nic user,model=virtio-net-pci#-nic user,model=virtio-net-pci,hostfwd=tcp:127.0.0.1:'"$port"'-:22#;
	s#-serial file:[^ ]*#-serial file:'"$R"'/verify-serial.log#; s#mon.sock#vmon.sock#')
eval "$cmd" >"$R/verify-qemu.log" 2>&1 &
qpid=$!
K=$EN_QEMU_KEY_DIR
ssho=(-i "$K/installer" -p "$port" -o "UserKnownHostsFile=$R/known_hosts" -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=5)
echo "[127.0.0.1]:$port $(cut -d' ' -f1,2 "$K/hostkey.pub")" >"$R/known_hosts"
for _ in $(seq 1 "${BOOT_WAIT:-120}"); do
	ssh "${ssho[@]}" node@127.0.0.1 true 2>/dev/null && break
	sleep 5
done
{
	echo "== host key accepted by strict known_hosts with the pre-generated key"
	ssh "${ssho[@]}" node@127.0.0.1 'echo ok'
	echo "== password login offered?"
	ssh -p "$port" -o "UserKnownHostsFile=$R/known_hosts" -o PreferredAuthentications=password -o PubkeyAuthentication=no -o BatchMode=yes node@127.0.0.1 true 2>&1 | tail -1
	ssh "${ssho[@]}" node@127.0.0.1 'set -x
		hostname; id; sudo -n true && echo SUDO_NOPASSWD_OK
		sudo passwd -S node; sudo passwd -S root
		lsblk -o NAME,SIZE,TYPE,FSTYPE,PARTTYPENAME,MOUNTPOINTS
		sudo swapon --show; head -1 /proc/meminfo; grep -v "^#" /etc/fstab; cat /etc/default/zramswap 2>/dev/null | grep -v "^#"
		cat /etc/systemd/journald.conf.d/*.conf 2>/dev/null
		systemctl is-active ssh avahi-daemon zramswap 2>/dev/null
		sudo sshd -T | grep -E "^(passwordauthentication|kbdinteractiveauthentication)"
		ls /sys/firmware/efi >/dev/null 2>&1 && echo EFI || echo BIOS
		sudo od -An -t u1 /sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c 2>/dev/null
		sudo dmesg | grep -i "secure boot" | head -2
		ls /boot/efi/EFI/* 2>/dev/null
		dpkg -l | grep -c ^ii; df -h /
		sudo grep -a "energy-node" /var/log/installer/syslog | cut -c1-200 | tail -15
		sudo poweroff' 2>&1
} >"$R/verify.txt"
sleep 15
kill $qpid 2>/dev/null
cat "$R/verify.txt"

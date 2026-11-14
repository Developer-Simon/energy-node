#!/bin/bash
# run.sh <name> <arch> <fw: bios|uefi|sb> <disk spec>...
# disk spec: sata:16G | nvme:64G | virtio:4G | usbdisk:8G
# Boots the patched stick $EN_QEMU_WORK_DIR/out/en-<arch>.iso (see build.sh),
# waits for the VM to power off (max 90 min).
set -eu
work=${EN_QEMU_WORK_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/energy-node-qemu}
name=$1 arch=$2 fw=$3
shift 3
R=$work/runs/$name
mkdir -p "$R"
cd "$R"
read -r -a display <<<"${QDISPLAY:--display none}"
common=("${display[@]}" -monitor "unix:$R/mon.sock,server,nowait"
	-serial "file:$R/serial.log" -nic "user,model=virtio-net-pci" -device "qemu-xhci,id=xhci"
	-drive "if=none,id=stick,file=$work/out/en-$arch.iso,format=raw,snapshot=on,readonly=on"
	-device "usb-storage,bus=xhci.0,drive=stick,bootindex=0")
if [ "$arch" = amd64 ]; then
	q=qemu-system-x86_64
	args=(-m 2048 -smp 2 "${common[@]}" -enable-kvm -cpu host)
	case $fw in
	bios) args+=(-machine q35) ;;
	uefi)
		cp /usr/share/edk2/ovmf/OVMF_VARS.fd vars.fd
		args+=(-machine q35
			-drive "if=pflash,format=raw,readonly=on,file=/usr/share/edk2/ovmf/OVMF_CODE.fd"
			-drive "if=pflash,format=raw,file=$R/vars.fd") ;;
	sb)
		cp /usr/share/edk2/ovmf/OVMF_VARS.secboot.fd vars.fd
		args+=(-machine "q35,smm=on" -global "driver=cfi.pflash01,property=secure,value=on"
			-drive "if=pflash,format=raw,readonly=on,file=/usr/share/edk2/ovmf/OVMF_CODE.secboot.fd"
			-drive "if=pflash,format=raw,file=$R/vars.fd") ;;
	esac
else
	q=qemu-system-aarch64
	cp /usr/share/edk2/aarch64/vars-template-pflash.raw vars.fd
	args=(-machine virt -cpu max -smp 4 -m 2048 "${common[@]}"
		-drive "if=pflash,format=raw,readonly=on,file=/usr/share/edk2/aarch64/QEMU_EFI-pflash.raw"
		-drive "if=pflash,format=raw,file=$R/vars.fd" -device virtio-gpu-pci -device "usb-kbd,bus=xhci.0")
fi
i=0
for spec in "$@"; do
	kind=${spec%%:*} size=${spec#*:}
	img=$R/disk$i-$kind.qcow2
	[ -e "$img" ] || qemu-img create -q -f qcow2 "$img" "$size"
	args+=(-drive "if=none,id=d$i,file=$img,format=qcow2")
	case $kind in
	sata) args+=(-device "ide-hd,drive=d$i,bus=ide.$((i < 6 ? i : 0))") ;;
	nvme) args+=(-device "nvme,drive=d$i,serial=nvme$i") ;;
	virtio) args+=(-device "virtio-blk-pci,drive=d$i") ;;
	usbdisk) args+=(-device "usb-storage,bus=xhci.0,drive=d$i") ;;
	esac
	i=$((i + 1))
done
echo "$q ${args[*]}" >cmd.txt
start=$(date +%s)
timeout "${TMO:-90m}" "$q" "${args[@]}" >qemu.log 2>&1 && rc=0 || rc=$?
echo "rc=$rc minutes=$((($(date +%s) - start) / 60))" | tee result.txt

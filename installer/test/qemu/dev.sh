#!/bin/bash
# dev.sh <command>: one entry point to try the Debian install stick in QEMU.
#
#   setup                  check the tools and create the test keys
#   stick [mediastick..]   build the stick (downloads the signed netinst ISO)
#   install [name] [disk..] install from the stick, with a window (default sata:16G)
#   boot [name] [port]     boot the installed disk with a window, SSH on 127.0.0.1:<port>
#   ssh [name] [cmd..]     SSH into a booted VM as the node user
#   tunnel [name]          forward the dashboard (8443 -> 443, 8080 -> 80, 8081)
#   stop [name]            power off a booted VM
#   matrix                 unattended runs C, A, F, B with checks, headless
#   clean [name]           delete the run directory (all runs when name is "all")
#
# The default name is "dev". Everything lives in $EN_QEMU_WORK_DIR, default
# ~/.cache/energy-node-qemu: test keys, ISO, stick image and one directory per
# VM. Environment: QDISPLAY (install window, default "-display gtk"), TMO (limit
# of an install, default 15m), ARCH (amd64 only for the window commands).
set -eu
here=$(cd "$(dirname "$0")" && pwd)
work=${EN_QEMU_WORK_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/energy-node-qemu}
keys=${EN_QEMU_KEY_DIR:-$work/keys}
export EN_QEMU_WORK_DIR=$work EN_QEMU_KEY_DIR=$keys
stick=$work/out/en-amd64.iso

die() { echo "dev.sh: $*" >&2; exit 1; }

need() {
	local missing=0 c
	for c in "$@"; do
		command -v "$c" >/dev/null 2>&1 || { echo "missing tool: $c" >&2; missing=1; }
	done
	[ "$missing" = 0 ] || die "install the tools above first"
}

run_dir() { echo "$work/runs/${1:-dev}"; }

running() { # running <name>: is a QEMU of this run alive?
	pgrep -f "qemu-system.*$(run_dir "$1")/" >/dev/null 2>&1
}

ssh_args() { # ssh_args <name>: options for the booted VM
	local R port
	R=$(run_dir "$1")
	[ -f "$R/port" ] || die "$1 was not booted with dev.sh boot"
	port=$(cat "$R/port")
	echo "-i $keys/installer -p $port -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -o BatchMode=yes"
}

cmd_setup() {
	need qemu-system-x86_64 qemu-img ssh ssh-keygen go
	[ -f /usr/share/edk2/ovmf/OVMF_CODE.fd ] || die "edk2 OVMF is missing (/usr/share/edk2/ovmf/OVMF_CODE.fd)"
	mkdir -p "$keys"
	if [ ! -f "$keys/installer" ]; then
		ssh-keygen -q -t ed25519 -N "" -C energy-node-installer -f "$keys/installer"
	fi
	if [ ! -f "$keys/hostkey" ]; then
		ssh-keygen -q -t ed25519 -N "" -C energy-node -f "$keys/hostkey"
	fi
	echo "keys in $keys"
	echo "host key fingerprint of every VM:"
	ssh-keygen -lf "$keys/hostkey.pub"
}

cmd_stick() {
	"$here/build.sh" amd64 "$stick" "$@"
	echo "stick: $stick"
}

cmd_install() {
	local name=${1:-dev}
	[ $# -gt 0 ] && shift
	[ -f "$stick" ] || die "no stick yet, run: dev.sh stick"
	running "$name" && die "$name is still running, run: dev.sh stop $name"
	rm -rf "$(run_dir "$name")"
	[ $# -gt 0 ] || set -- sata:16G
	[ ${#work} -le 70 ] || die "work dir path is too long for the QEMU monitor socket (108 bytes), set a shorter EN_QEMU_WORK_DIR"
	QDISPLAY=${QDISPLAY:--display gtk} TMO=${TMO:-15m} "$here/run.sh" "$name" amd64 uefi "$@"
	grep -q '^rc=0 ' "$(run_dir "$name")/result.txt" || die "the install did not power off by itself, see $(run_dir "$name")/qemu.log"
	echo "installed. Next: dev.sh boot $name"
}

cmd_boot() {
	local name=${1:-dev} port=${2:-2222} R cmd
	R=$(run_dir "$name")
	[ -f "$R/cmd.txt" ] || die "no install for $name, run: dev.sh install $name"
	running "$name" && die "$name is already running"
	ss -tln 2>/dev/null | grep -q ":$port " && die "port $port is in use, pick another: dev.sh boot $name <port>"
	cmd=$(sed -E 's# -drive if=none,id=stick[^ ]*##; s# -device usb-storage,bus=xhci.0,drive=stick,bootindex=0##;
		s#-nic user,model=virtio-net-pci#-nic user,model=virtio-net-pci,hostfwd=tcp:127.0.0.1:'"$port"'-:22#;
		s#-serial file:[^ ]*#-serial file:'"$R"'/boot-serial.log#; s#mon.sock#bmon.sock#' "$R/cmd.txt")
	[ -n "${QDISPLAY:-}" ] && cmd=${cmd/-display gtk/$QDISPLAY}
	echo "$port" >"$R/port"
	setsid -f bash -c "cd '$R' && exec $cmd" </dev/null >"$R/boot-qemu.log" 2>&1
	# shellcheck disable=SC2046
	for _ in $(seq 1 60); do
		ssh $(ssh_args "$name") -o ConnectTimeout=5 node@127.0.0.1 true 2>/dev/null && break
		sleep 2
	done
	echo "booted. SSH:        dev.sh ssh $name"
	echo "Installer connect:  host 127.0.0.1:$port, user node, key $keys/installer"
	echo "Host key (ed25519): $(ssh-keygen -lf "$keys/hostkey.pub" | cut -d' ' -f2)"
	echo "Dashboard tunnel:   dev.sh tunnel $name"
}

cmd_ssh() {
	local name=${1:-dev}
	[ $# -gt 0 ] && shift
	# shellcheck disable=SC2046
	exec ssh $(ssh_args "$name") node@127.0.0.1 "$@"
}

cmd_tunnel() {
	local name=${1:-dev}
	echo "https://localhost:8443 (self-signed) and http://localhost:8080, Ctrl-C ends"
	# shellcheck disable=SC2046
	exec ssh $(ssh_args "$name") -N -L 8443:localhost:443 -L 8080:localhost:80 -L 8081:localhost:8081 node@127.0.0.1
}

cmd_stop() {
	local name=${1:-dev} R
	R=$(run_dir "$name")
	# shellcheck disable=SC2046
	ssh $(ssh_args "$name") -o ConnectTimeout=5 node@127.0.0.1 'sudo poweroff' >/dev/null 2>&1 || true
	[ -S "$R/bmon.sock" ] && command -v socat >/dev/null 2>&1 && echo quit | socat - "UNIX-CONNECT:$R/bmon.sock" >/dev/null 2>&1 || true
	echo "stop sent to $name"
}

# Runs without a key press at the target (see README for the others).
cmd_matrix() {
	[ -f "$stick" ] || cmd_stick
	local n=0 row name fw disks port
	for row in "C uefi virtio:4G" "A bios sata:16G" "F bios sata:16G usbdisk:8G" "B sb nvme:64G"; do
		read -r name fw disks <<<"$row"
		rm -rf "$(run_dir "m$name")"
		port=$((2300 + n))
		n=$((n + 1))
		echo "== run $name ($fw, $disks)"
		# shellcheck disable=SC2086
		TMO=${TMO:-15m} "$here/run.sh" "m$name" amd64 "$fw" $disks
		"$here/verify.sh" "m$name" "$port" | tail -n 25
	done
}

cmd_clean() {
	local name=${1:-dev}
	if [ "$name" = all ]; then
		pgrep -f "qemu-system.*$work/runs/" >/dev/null 2>&1 && die "a VM is still running"
		rm -rf "$work/runs"
	else
		running "$name" && die "$name is still running, run: dev.sh stop $name"
		rm -rf "$(run_dir "$name")"
	fi
}

c=${1:-help}
[ $# -gt 0 ] && shift
case $c in
setup) cmd_setup "$@" ;;
stick) cmd_stick "$@" ;;
install) cmd_install "$@" ;;
boot) cmd_boot "$@" ;;
ssh) cmd_ssh "$@" ;;
tunnel) cmd_tunnel "$@" ;;
stop) cmd_stop "$@" ;;
matrix) cmd_matrix "$@" ;;
clean) cmd_clean "$@" ;;
help | -h | --help) sed -n '2,/^set -eu/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//' ;;
*) die "unknown command $c (dev.sh help)" ;;
esac

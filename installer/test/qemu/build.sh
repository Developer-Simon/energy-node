#!/bin/sh
# build.sh <arch> <out.iso> [mediastick flags, e.g. -ask -lang de]
#
# Patches the original Debian netinst ISO of <arch> into a stick image with
# cmd/mediastick. The ISO is read from $EN_QEMU_ISO_DIR, the installer test
# keys (installer.pub, hostkey, hostkey.pub) from $EN_QEMU_KEY_DIR.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
installer=$(cd "$here/../.." && pwd)
: "${EN_QEMU_ISO_DIR:?set EN_QEMU_ISO_DIR to the directory with the netinst ISOs}"
: "${EN_QEMU_KEY_DIR:?set EN_QEMU_KEY_DIR to the directory with installer.pub, hostkey and hostkey.pub}"
work=${EN_QEMU_WORK_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/energy-node-qemu}

arch=$1
out=$2
shift 2
case $arch in
amd64 | arm64) ;;
*) echo "build.sh: unknown arch $arch" >&2; exit 2 ;;
esac

mkdir -p "$work/bin" "$(dirname "$out")"
(cd "$installer" && GOTOOLCHAIN=auto go build -o "$work/bin/mediastick" ./cmd/mediastick)

"$work/bin/mediastick" \
	-in "$EN_QEMU_ISO_DIR/debian-13.7.0-$arch-netinst.iso" \
	-out "$out" -arch "$arch" \
	-pubkey "$EN_QEMU_KEY_DIR/installer.pub" \
	-hostkey "$EN_QEMU_KEY_DIR/hostkey" \
	"$@"

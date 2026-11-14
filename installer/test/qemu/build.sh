#!/bin/bash
# build.sh <arch> <out.iso> [mediastick flags, e.g. -ask -lang de]
#
# Patches the Debian netinst ISO of <arch> into a stick image with
# cmd/mediastick. Without $EN_QEMU_ISO_DIR the current signed ISO is downloaded
# into <work>/iso, otherwise debian-13.7.0-<arch>-netinst.iso is read from that
# directory. The installer test keys (installer.pub, hostkey, hostkey.pub) come
# from $EN_QEMU_KEY_DIR, default <work>/keys (created by dev.sh setup).
set -eu
here=$(cd "$(dirname "$0")" && pwd)
installer=$(cd "$here/../.." && pwd)
work=${EN_QEMU_WORK_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/energy-node-qemu}
keys=${EN_QEMU_KEY_DIR:-$work/keys}
[ -f "$keys/installer.pub" ] || { echo "build.sh: no test keys in $keys, run dev.sh setup" >&2; exit 2; }

arch=$1
out=$2
shift 2
case $arch in
amd64 | arm64) ;;
*) echo "build.sh: unknown arch $arch" >&2; exit 2 ;;
esac

mkdir -p "$work/bin" "$(dirname "$out")"
(cd "$installer" && GOTOOLCHAIN=auto go build -o "$work/bin/mediastick" ./cmd/mediastick)

if [ -n "${EN_QEMU_ISO_DIR:-}" ]; then
	source_iso=(-in "$EN_QEMU_ISO_DIR/debian-13.7.0-$arch-netinst.iso")
else
	source_iso=(-fetch "$work/iso")
fi
"$work/bin/mediastick" \
	"${source_iso[@]}" \
	-out "$out" -arch "$arch" \
	-pubkey "$keys/installer.pub" \
	-hostkey "$keys/hostkey" \
	"$@"

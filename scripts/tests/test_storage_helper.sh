#!/usr/bin/env bash
# Test for dashboard/energy-node-storage-helper (format, mount, unmount) with stubbed system tools
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
helper="$here/../../dashboard/energy-node-storage-helper"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

UUID="3f9c2e1a-8b7d-4c6e-9f10-2a3b4c5d6e7f"
FORMAT_UUID="0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
UNIT_NAME='mnt-energy\x2dnode\x2ddata.mount'

# Fixture table read by the stubs: NAME TYPE MOUNTPOINT UUID PARENT ("-" = leer)
export FIXTURE="$tmp/lsblk.table"
export FORMAT_UUID
export SFDISK_LOG="$tmp/sfdisk.log" MKFS_LOG="$tmp/mkfs.log" SYSTEMCTL_LOG="$tmp/systemctl.log"

mkdir -p "$tmp/bin"
cat > "$tmp/bin/lsblk" <<'SH'
#!/bin/sh
# Aufrufe des Helpers: -no PKNAME <dev>, -lnpo NAME,TYPE,MOUNTPOINT <dev>,
# -lnpo NAME,UUID, -no UUID <dev>
case "$2" in
  PKNAME)
    awk -v p="$3" '$1 == p { if ($5 != "-") print $5; exit }' "$FIXTURE" ;;
  NAME,TYPE,MOUNTPOINT)
    awk -v d="$3" -v k="${3#/dev/}" '$1 == d || $5 == k { m = ($3 == "-") ? "" : $3; print $1, $2, m }' "$FIXTURE" ;;
  NAME,UUID)
    awk '$4 != "-" { print $1, $4 }' "$FIXTURE" ;;
  UUID)
    awk -v p="$3" '$1 == p { if ($4 != "-") print $4; exit }' "$FIXTURE" ;;
  *) echo "lsblk stub: unerwartete Argumente: $*" >&2; exit 2 ;;
esac
SH
cat > "$tmp/bin/findmnt" <<'SH'
#!/bin/sh
# Nur "-no SOURCE /" wird benutzt.
awk '$3 == "/" { print $1; exit }' "$FIXTURE"
SH
cat > "$tmp/bin/sfdisk" <<'SH'
#!/bin/sh
dev=""
for arg; do dev="$arg"; done
printf '%s\n' "$*" >> "$SFDISK_LOG"
cat >> "$SFDISK_LOG"
k="${dev#/dev/}"
printf '%s1 part - - %s\n' "$dev" "$k" >> "$FIXTURE"
SH
cat > "$tmp/bin/mkfs" <<'SH'
#!/bin/sh
dev=""
for arg; do dev="$arg"; done
printf '%s\n' "$*" >> "$MKFS_LOG"
awk -v d="$dev" -v u="$FORMAT_UUID" 'BEGIN { OFS = " " } $1 == d { $4 = u } { print }' "$FIXTURE" > "$FIXTURE.new"
mv "$FIXTURE.new" "$FIXTURE"
SH
cat > "$tmp/bin/systemctl" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> "$SYSTEMCTL_LOG"
SH
cat > "$tmp/bin/udevadm" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> "$UDEVADM_LOG"
SH
export UDEVADM_LOG="$tmp/udevadm.log"
chmod +x "$tmp/bin/"*

reset() {
  rm -rf "${tmp:?}/units" "${tmp:?}/mnt" "$SFDISK_LOG" "$MKFS_LOG" "$SYSTEMCTL_LOG" "$UDEVADM_LOG"
  mkdir -p "$tmp/units"
  printf '  node-test-1 \n' > "$tmp/machine-id"
  # Systemlaufwerk mmcblk0 mit Root-Partition, dazu eine fremde Platte sda
  cat > "$FIXTURE" <<'TABLE'
/dev/mmcblk0 disk - - -
/dev/mmcblk0p2 part / - mmcblk0
/dev/sda disk - - -
TABLE
}

# run_helper ARGS... setzt rc und out
run_helper() {
  set +e
  out="$(env EN_STORAGE_UNIT_DIR="$tmp/units" \
    EN_STORAGE_MOUNT_POINT="$tmp/mnt" \
    EN_STORAGE_MACHINE_ID_FILE="$tmp/machine-id" \
    EN_STORAGE_UDEVADM="$tmp/bin/udevadm" \
    EN_STORAGE_OWNER="$(id -un)" \
    EN_STORAGE_LSBLK="$tmp/bin/lsblk" \
    EN_STORAGE_FINDMNT="$tmp/bin/findmnt" \
    EN_STORAGE_SFDISK="$tmp/bin/sfdisk" \
    EN_STORAGE_MKFS="$tmp/bin/mkfs" \
    EN_STORAGE_SYSTEMCTL="$tmp/bin/systemctl" \
    sh "$helper" "$@" 2>&1)"
  rc=$?
  set -e
}

# expect_refused CODE ARGS...: Exit 65, Code auf stdout, nichts veraendert
expect_refused() {
  code="$1"; shift
  run_helper "$@"
  [ "$rc" -eq 65 ] || fail "$* nicht mit Exit 65 abgelehnt (rc=$rc)" "$out"
  grep -qx "$code" <<<"$out" || fail "$* ohne Fehlercode $code" "$out"
  [ ! -s "$SFDISK_LOG" ] || fail "$* hat sfdisk aufgerufen"
  [ ! -s "$MKFS_LOG" ] || fail "$* hat mkfs aufgerufen"
  [ -z "$(ls -A "$tmp/units")" ] || fail "$* hat eine Unit-Datei angelegt"
}

# --- format lehnt das Laufwerk unter / ab ----------------------------------
reset
expect_refused DEVICE_IS_SYSTEM_DISK format mmcblk0

# --- format lehnt eingehaengte Partition ab --------------------------------
reset
cat >> "$FIXTURE" <<'TABLE'
/dev/sda1 part /media/usb - sda
TABLE
expect_refused DEVICE_MOUNTED format sda

# --- format lehnt Pfad, Metazeichen und leeres Argument ab -----------------
reset
expect_refused INVALID_DEVICE format ../sda
reset
expect_refused INVALID_DEVICE format 'sda;reboot'
reset
expect_refused INVALID_DEVICE format ''

# --- format legt Partition an, formatiert einmal und gibt UUID aus ---------
reset
run_helper format sda
[ "$rc" -eq 0 ] || fail "format sda schlug fehl (rc=$rc)" "$out"
[ "$out" = "$FORMAT_UUID" ] || fail "format gibt keine UUID aus" "$out"
grep -qx 'settle' "$UDEVADM_LOG" || fail "kein udevadm settle nach sfdisk"
grep -qx 'label: gpt' "$SFDISK_LOG" || fail "sfdisk ohne GPT-Layout" "$(cat "$SFDISK_LOG")"
[ "$(wc -l < "$MKFS_LOG")" -eq 1 ] || fail "mkfs nicht genau einmal aufgerufen" "$(cat "$MKFS_LOG" 2>/dev/null || true)"
grep -qF -- '-L energy-node-data /dev/sda1' "$MKFS_LOG" || fail "mkfs ohne Label oder Partition" "$(cat "$MKFS_LOG")"

# --- mount lehnt unbekannte und ungueltige UUIDs ab -----------------------
reset
cat >> "$FIXTURE" <<TABLE
/dev/sda1 part - $UUID sda
TABLE
expect_refused UNKNOWN_UUID mount 00000000-0000-4000-8000-000000000000
reset
expect_refused INVALID_UUID mount not-a-uuid

# --- mount legt Unit, Einhaengepunkt, Verzeichnisse und Kennung an --------
reset
cat >> "$FIXTURE" <<TABLE
/dev/sda1 part - $UUID sda
TABLE
run_helper mount "$UUID"
[ "$rc" -eq 0 ] || fail "mount schlug fehl (rc=$rc)" "$out"
unit="$tmp/units/$UNIT_NAME"
[ -f "$unit" ] || fail "Unit-Datei fehlt"
grep -qx 'Options=nofail,noatime' "$unit" || fail "Unit ohne nofail,noatime" "$(cat "$unit")"
grep -qx "What=/dev/disk/by-uuid/$UUID" "$unit" || fail "Unit mit falschem What" "$(cat "$unit")"
grep -qx "Where=$tmp/mnt" "$unit" || fail "Unit mit falschem Where" "$(cat "$unit")"
grep -qx 'TimeoutSec=20s' "$unit" || fail "Unit ohne TimeoutSec=20s" "$(cat "$unit")"
[ "$(stat -c %a "$tmp/mnt")" = 755 ] || fail "Einhaengepunkt nicht 0755"
if [ "$(id -u)" -eq 0 ]; then
  [ "$(stat -c %U:%G "$tmp/mnt")" = root:root ] || fail "Einhaengepunkt nicht root-eigen"
fi
[ -d "$tmp/mnt/runtime" ] || fail "runtime/ fehlt"
[ -d "$tmp/mnt/backups" ] || fail "backups/ fehlt"
[ "$(stat -c %U "$tmp/mnt/runtime")" = "$(id -un)" ] || fail "runtime/ nicht dem Betreiber gehoerend"
[ "$(cat "$tmp/mnt/.energy-node-volume")" = node-test-1 ] || fail "Kennungsdatei falsch (Maschinen-ID getrimmt)"
grep -qF "start $UNIT_NAME" "$SYSTEMCTL_LOG" || fail "Unit nicht gestartet" "$(cat "$SYSTEMCTL_LOG")"

# --- unmount stoppt und entfernt die Unit ---------------------------------
run_helper unmount
[ "$rc" -eq 0 ] || fail "unmount schlug fehl (rc=$rc)" "$out"
[ ! -e "$unit" ] || fail "Unit nach unmount noch vorhanden"
grep -qF "stop $UNIT_NAME" "$SYSTEMCTL_LOG" || fail "Unit nicht gestoppt" "$(cat "$SYSTEMCTL_LOG")"

echo "OK: $(basename "$0")"

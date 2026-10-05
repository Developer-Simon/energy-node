#!/usr/bin/env bash
# Test for scripts/bootstrap/15-system-upgrade.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/15-system-upgrade.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
# apt-get protokolliert jeden Aufruf samt DEBIAN_FRONTEND. "-s upgrade"
# simuliert und meldet je Paket aus UPGRADABLE eine Inst-Zeile.
# APT_RC laesst update scheitern, UPGRADE_RC das eigentliche upgrade.
cat > "$tmp/bin/apt-get" <<'SH'
#!/usr/bin/env bash
printf 'FRONTEND=%s apt-get %s\n' "${DEBIAN_FRONTEND:-}" "$*" >> "$APT_LOG"
case " $* " in
  *" update "*)
    # LOCKED_TIMES: so oft haelt ein anderes apt den Listen-Lock.
    if [ -n "${LOCKED_TIMES:-}" ]; then
      n="$(cat "$APT_LOG.locks" 2>/dev/null || echo 0)"
      if [ "$n" -lt "$LOCKED_TIMES" ]; then
        echo $((n + 1)) > "$APT_LOG.locks"
        echo "E: Could not get lock /var/lib/apt/lists/lock. It is held by process 812 (apt-get)" >&2
        exit 100
      fi
    fi
    exit "${APT_RC:-0}" ;;
  *" -s "*)
    [ -n "${SIM_RC:-}" ] && { echo "E: dpkg was interrupted" >&2; exit "$SIM_RC"; }
    for p in ${UPGRADABLE:-}; do printf 'Inst %s [1.0] (1.1 Debian:12/stable [armhf])\n' "$p"; done
    exit 0 ;;
  *" upgrade "*) exit "${UPGRADE_RC:-0}" ;;
esac
exit 0
SH
chmod +x "$tmp/bin/apt-get"

export PATH="$tmp/bin:$PATH"
export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_VERSION=v1.0.0 EN_SUDO=""
export EN_SELECTION="$tmp/selection.json" APT_LOG="$tmp/apt.log" EN_APT_LOCK_PAUSE=0
mkdir -p "$tmp/root/run"
flag="$tmp/root/run/reboot-required"

reset() { rm -rf "$tmp/state" "$flag" "$flag.pkgs" "$APT_LOG.locks"; : > "$APT_LOG"; rm -f "$EN_SELECTION"; }

# --- ohne Auswahl-Eintrag: gewaehlt, aktualisiert --------------------------
reset
out="$(UPGRADABLE="mosquitto libssl3" bash "$script")"
grep -qx '##STEP 15 ok' <<<"$out" || fail "kein ok-Marker" "$out"
grep -q ' update' "$APT_LOG" || fail "kein apt-get update" "$(cat "$APT_LOG")"
grep -q '^FRONTEND=noninteractive apt-get .* -y upgrade' "$APT_LOG" \
  || fail "upgrade nicht nichtinteraktiv" "$(cat "$APT_LOG")"
grep -q 'full-upgrade\|dist-upgrade' "$APT_LOG" && fail "full-upgrade statt upgrade"
grep -v -- '-o DPkg::Lock::Timeout=600' "$APT_LOG" | grep -q . \
  && fail "ein apt-get-Aufruf ohne Lock-Timeout" "$(cat "$APT_LOG")"
grep -- ' -y upgrade' "$APT_LOG" | grep -q -- '--force-confold' \
  || fail "upgrade ohne --force-confold" "$(cat "$APT_LOG")"
grep -q 'mosquitto' <<<"$out" || fail "Paketliste fehlt im Log" "$out"
[ -e "$flag" ] && fail "Neustart-Markierung ohne Kernel-Update"

# --- zweiter Lauf: Stempel --------------------------------------------------
: > "$APT_LOG"
out="$(UPGRADABLE="mosquitto" bash "$script")"
grep -qx '##STEP 15 skip bereits erledigt' <<<"$out" || fail "nicht uebersprungen" "$out"
[ -s "$APT_LOG" ] && fail "zweiter Lauf hat apt-get aufgerufen" "$(cat "$APT_LOG")"

# --- nichts zu tun: ok ohne upgrade -----------------------------------------
reset
out="$(bash "$script")"
grep -qx '##STEP 15 ok' <<<"$out" || fail "kein ok ohne Updates" "$out"
grep -q ' -y upgrade' "$APT_LOG" && fail "upgrade ohne ausstehende Pakete"

# --- Kernel-Update: Markierung und Zusatz -----------------------------------
reset
out="$(UPGRADABLE="raspberrypi-kernel libc6" bash "$script")"
grep -qx '##STEP 15 ok neustart noetig' <<<"$out" || fail "kein Neustart-Zusatz" "$out"
[ -f "$flag" ] || fail "Neustart-Markierung fehlt"
grep -qx 'raspberrypi-kernel' "$flag.pkgs" || fail "Paket fehlt in reboot-required.pkgs" "$(cat "$flag.pkgs" 2>/dev/null)"
grep -q 'libc6' "$flag.pkgs" && fail "libc6 braucht keinen Neustart"

reset
out="$(UPGRADABLE="linux-image-6.6.51+rpt-rpi-v8" bash "$script")"
grep -qx '##STEP 15 ok neustart noetig' <<<"$out" || fail "linux-image nicht erkannt" "$out"

# --- Markierung von frueher bleibt sichtbar ---------------------------------
reset
touch "$flag"
out="$(UPGRADABLE="mosquitto" bash "$script")"
grep -qx '##STEP 15 ok neustart noetig' <<<"$out" || fail "vorhandene Markierung uebersehen" "$out"

# --- abgewaehlt: nur pruefen ------------------------------------------------
reset
printf '{"steps":{"15":false}}\n' > "$EN_SELECTION"
out="$(UPGRADABLE="mosquitto libssl3" bash "$script")"
grep -qx '##STEP 15 skip nicht ausgewaehlt' <<<"$out" || fail "abgewaehlt nicht uebersprungen" "$out"
grep -q ' update' "$APT_LOG" || fail "abgewaehlt ohne Pruefung" "$(cat "$APT_LOG")"
grep -q ' -y upgrade' "$APT_LOG" && fail "abgewaehlt trotzdem aktualisiert"
grep -q '2 Pakete' <<<"$out" || fail "Zahl der Updates fehlt im Log" "$out"
[ -e "$tmp/state/steps/15" ] && fail "abgewaehlt mit Stempel"

# --- abgewaehlt und offline: kein Fehler ------------------------------------
reset
printf '{"steps":{"15":false}}\n' > "$EN_SELECTION"
set +e
out="$(APT_RC=1 bash "$script")"
rc=$?
set -e
[ "$rc" -eq 0 ] || fail "abgewaehlt und offline bricht ab" "$rc $out"
grep -qx '##STEP 15 skip nicht ausgewaehlt' <<<"$out" || fail "falscher Marker offline" "$out"

# --- gewaehlt: Fehlercodes ----------------------------------------------------
reset
set +e
out="$(APT_RC=1 bash "$script")"; rc=$?
set -e
[ "$rc" -eq 1 ] || fail "update-Fehler nicht weitergereicht" "$rc"
grep -qx '##STEP 15 fail APT_UPDATE_FAILED' <<<"$out" || fail "falscher Code fuer update" "$out"

reset
set +e
out="$(UPGRADABLE="mosquitto" UPGRADE_RC=100 bash "$script")"; rc=$?
set -e
[ "$rc" -eq 1 ] || fail "upgrade-Fehler nicht weitergereicht" "$rc"
grep -qx '##STEP 15 fail APT_UPGRADE_FAILED' <<<"$out" || fail "falscher Code fuer upgrade" "$out"
[ -e "$tmp/state/steps/15" ] && fail "Stempel trotz Fehler"

# --- Listen-Lock von apt-daily: warten statt abbrechen -----------------------
# DPkg::Lock::Timeout gilt nur fuer den dpkg-Lock, nicht fuer den Lock, den
# apt-get update auf /var/lib/apt/lists nimmt.
reset
out="$(LOCKED_TIMES=2 UPGRADABLE="mosquitto" bash "$script" 2>&1)"
grep -qx '##STEP 15 ok' <<<"$out" || fail "Lock nicht abgewartet" "$out"
[ "$(grep -c ' update' "$APT_LOG")" -eq 3 ] || fail "update nicht wiederholt" "$(cat "$APT_LOG")"

reset
set +e
out="$(LOCKED_TIMES=1000 bash "$script" 2>&1)"; rc=$?
set -e
[ "$rc" -eq 1 ] || fail "dauerhafter Lock nicht als Fehler gemeldet" "$rc"
grep -qx '##STEP 15 fail APT_UPDATE_FAILED' <<<"$out" || fail "falscher Code bei dauerhaftem Lock" "$out"
[ "$(grep -c ' update' "$APT_LOG")" -eq 60 ] || fail "nicht genau 60 Versuche" "$(grep -c ' update' "$APT_LOG")"

# --- Simulation scheitert (dpkg unterbrochen): Fehler statt "aktuell" -------
reset
set +e
out="$(SIM_RC=100 bash "$script" 2>&1)"; rc=$?
set -e
[ "$rc" -eq 1 ] || fail "Simulationsfehler nicht weitergereicht" "$rc $out"
grep -qx '##STEP 15 fail APT_UPGRADE_FAILED' <<<"$out" || fail "falscher Code fuer Simulation" "$out"
grep -q 'dpkg was interrupted' <<<"$out" || fail "apt-Fehler fehlt im Log" "$out"
[ -e "$tmp/state/steps/15" ] && fail "Stempel trotz Simulationsfehler"

echo "OK: $(basename "$0")"

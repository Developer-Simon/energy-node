#!/usr/bin/env bash
# Test for scripts/bootstrap/89-example.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/89-example.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf 'systemctl %s\n' "$*" >> "$SYSTEMCTL_LOG"
exit "${SYSTEMCTL_RC:-0}"
SH
chmod +x "$tmp/bin/systemctl"

# Ersatz fuer example_apply.py publish: loggt die Argumente und legt das
# Passwort (stdin) in eine Datei, damit der Test die Weitergabe prueft.
cat > "$tmp/publish" <<'SH'
#!/usr/bin/env bash
cat > "$PUBLISH_STDIN"
printf '%s\n' "$*" >> "$PUBLISH_LOG"
SH
chmod +x "$tmp/publish"

export PATH="$tmp/bin:$PATH"
export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_TARGET_BASE="/home/test"
export EN_BUNDLE_VERSION=v1.0.0 EN_SUDO=""
export EN_SELECTION="$tmp/selection.json" SYSTEMCTL_LOG="$tmp/systemctl.log"
export PUBLISH_LOG="$tmp/publish.log" PUBLISH_STDIN="$tmp/publish.stdin"
export EN_EXAMPLE_PUBLISH_CMD="$tmp/publish"

devices="$EN_ROOT$EN_TARGET_BASE/devices"
marker="$EN_STATE_DIR/example-created"
ALL_SELECTED='{"steps":{"89":true,"81":true,"83":true,"88":true}}'

# Leerer Knoten mit Konfiguration, ohne Auswahl und ohne Marke.
setup_node() {
  rm -rf "$EN_STATE_DIR" "$EN_ROOT"
  mkdir -p "$devices" "$EN_ROOT/etc/energy-node"
  printf 'geheim\n' > "$EN_ROOT/etc/energy-node/mqtt.pw"
  printf '{"paths":{"data_dir":"/data/energy"},"mqtt":{"host":"127.0.0.1","port":1883,"username":"energy","password_file":"/etc/energy-node/mqtt.pw"},"dashboard":{"node_device_id":"energy_node"}}\n' \
    > "$EN_ROOT/etc/energy-node/config.json"
  printf '[]\n' > "$devices/apsystems_devices.json"
  printf '[]\n' > "$devices/shelly_devices.json"
  printf '{"version":1,"settings":{},"rules":[]}\n' > "$devices/automation_rules.json"
  : > "$SYSTEMCTL_LOG"
  : > "$PUBLISH_LOG"
  rm -f "$PUBLISH_STDIN"
}

snapshot() {
  find "$EN_ROOT" -type f -print0 | sort -z | xargs -0 sha256sum
}

# --- ausdruecklich abgewaehlt: kein Beispiel, nichts geaendert --------------
setup_node
printf '{"steps":{"89":false,"81":true,"83":true,"88":true}}\n' > "$EN_SELECTION"
before="$(snapshot)"
out="$(bash "$script")"
grep -qx '##STEP 89 skip nicht ausgewaehlt' <<<"$out" || fail "abgewaehlt nicht uebersprungen" "$out"
[ "$(snapshot)" = "$before" ] || fail "abgewaehlt wurde etwas geaendert"
[ -f "$marker" ] && fail "abgewaehlt gestempelt"

# --- Eintrag fehlt in der Auswahl: Vorgabe an, Beispiel laeuft --------------
setup_node
printf '{"steps":{"81":true,"83":true,"88":true}}\n' > "$EN_SELECTION"
out="$(bash "$script")"
grep -qx '##STEP 89 ok' <<<"$out" || fail "ohne Eintrag in der Auswahl kein ok-Marker" "$out"
grep -q 'example_grid' "$devices/shelly_devices.json" || fail "Beispielgeraet fehlt ohne Eintrag in der Auswahl"

# --- Erstinstallation nachweisbar (installed-manifest.json): kein Beispiel ---
setup_node
printf '%s\n' "$ALL_SELECTED" > "$EN_SELECTION"
mkdir -p "$EN_STATE_DIR"
touch "$EN_STATE_DIR/installed-manifest.json"
before="$(cat "$devices/shelly_devices.json")"
out="$(bash "$script")"
grep -qx '##STEP 89 skip keine Erstinstallation' <<<"$out" || fail "nach frueherer Installation nicht uebersprungen" "$out"
[ "$(cat "$devices/shelly_devices.json")" = "$before" ] || fail "nach frueherer Installation wurde Geraetedatei geschrieben"
[ -s "$PUBLISH_LOG" ] && fail "nach frueherer Installation veroeffentlicht"
[ -s "$SYSTEMCTL_LOG" ] && fail "nach frueherer Installation Dienste neu gestartet"
[ -f "$marker" ] && fail "nach frueherer Installation gestempelt"

# --- ausgewaehlt, leerer Knoten: Beispiel wird angelegt --------------------
setup_node
printf '%s\n' "$ALL_SELECTED" > "$EN_SELECTION"
out="$(bash "$script")"
grep -qx '##STEP 89 ok' <<<"$out" || fail "kein ok-Marker" "$out"
grep -q 'example_grid' "$devices/shelly_devices.json" || fail "Beispielgeraet fehlt"
[ -f "$EN_ROOT/data/energy/example-seed.json" ] || fail "Seed nicht im Datenordner"
grep -qF 'outstation/energy_node/settings/simulation_active/set 1' "$PUBLISH_LOG" \
  || fail "Freigabe nicht veroeffentlicht" "$(cat "$PUBLISH_LOG")"
[ "$(cat "$PUBLISH_STDIN")" = "geheim" ] || fail "Passwort nicht an publish uebergeben"
grep -qx 'systemctl restart apsystems-ez1.service shelly-rpc.service automation.service energy-node-dashboard.service' \
  "$SYSTEMCTL_LOG" || fail "Dienste nicht neu gestartet" "$(cat "$SYSTEMCTL_LOG")"
[ -f "$marker" ] || fail "keine Marke"
[ -f "$EN_STATE_DIR/steps/89" ] || fail "kein Stempel"

# --- Gleiche Bundle-Version: der Stempel gilt --------------------------------
out="$(bash "$script")"
grep -qx '##STEP 89 skip bereits erledigt' <<<"$out" || fail "Stempel nicht beachtet" "$out"

# --- Marke vorhanden: Beispiel kommt nicht zurueck --------------------------
# Ein Update ohne Stempel (der Schritt laeuft neu) darf es nicht neu anlegen.
rm -f "$EN_STATE_DIR/steps/89"
printf '[]\n' > "$devices/shelly_devices.json"
: > "$SYSTEMCTL_LOG"
out="$(bash "$script")"
grep -qx '##STEP 89 skip Beispiel wurde schon angelegt' <<<"$out" || fail "zweiter Lauf nicht uebersprungen" "$out"
grep -q 'example_grid' "$devices/shelly_devices.json" && fail "Beispielgeraet ist wieder da"
[ -s "$SYSTEMCTL_LOG" ] && fail "zweiter Lauf hat Dienste neu gestartet"

# --- Node mit eigenen Geraeten: unangetastet --------------------------------
setup_node
printf '%s\n' "$ALL_SELECTED" > "$EN_SELECTION"
printf '[{"id":"eigen","name":"Eigen","host":"10.0.0.5"}]\n' > "$devices/shelly_devices.json"
before="$(cat "$devices/shelly_devices.json")"
out="$(bash "$script")"
grep -qx '##STEP 89 skip Node hat schon Geraete' <<<"$out" || fail "Node mit Geraeten nicht uebersprungen" "$out"
[ "$(cat "$devices/shelly_devices.json")" = "$before" ] || fail "Geraetedatei veraendert"
[ -s "$PUBLISH_LOG" ] && fail "Node mit Geraeten hat veroeffentlicht"
[ -s "$SYSTEMCTL_LOG" ] && fail "Node mit Geraeten hat Dienste neu gestartet"
[ -f "$marker" ] && fail "Node mit Geraeten gestempelt"

# --- Schritt 83 abgewaehlt: kein Beispiel --------------------------------
setup_node
printf '{"steps":{"89":true,"81":true,"83":false,"88":true}}\n' > "$EN_SELECTION"
out="$(bash "$script")"
grep -qx '##STEP 89 skip nicht ausgewaehlt' <<<"$out" || fail "lief ohne Shelly-Dienst" "$out"
grep -q 'example_grid' "$devices/shelly_devices.json" && fail "Beispiel ohne Shelly-Dienst angelegt"
[ -s "$PUBLISH_LOG" ] && fail "ohne Shelly-Dienst veroeffentlicht"

# --- Neustart scheitert: Fehlercode, keine Marke ----------------------------
setup_node
printf '%s\n' "$ALL_SELECTED" > "$EN_SELECTION"
set +e
out="$(SYSTEMCTL_RC=1 bash "$script")"
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "Neustart-Fehler ohne Abbruch"
grep -qx '##STEP 89 fail EXAMPLE_FAILED' <<<"$out" || fail "falscher Fehlercode" "$out"
[ -f "$marker" ] && fail "gescheiterter Lauf gestempelt"

echo "OK: $(basename "$0")"

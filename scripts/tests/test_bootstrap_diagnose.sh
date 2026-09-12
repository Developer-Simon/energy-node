#!/usr/bin/env bash
# Test for scripts/bootstrap/diagnose.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/diagnose.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

mkdir -p "$tmp/bin"
# ACTIVE listet die Units, die als aktiv gelten sollen.
cat > "$tmp/bin/systemctl" <<'SH'
#!/usr/bin/env bash
[ "$1" = is-active ] || exit 1
case " ${ACTIVE:-} " in *" $2 "*) echo active; exit 0 ;; esac
echo inactive; exit 3
SH
cat > "$tmp/bin/ss" <<'SH'
#!/usr/bin/env bash
printf 'State Recv-Q Send-Q Local-Address:Port\n'
for p in ${LISTENING:-}; do printf 'LISTEN 0 128 0.0.0.0:%s 0.0.0.0:*\n' "$p"; done
SH
cat > "$tmp/bin/tailscale" <<'SH'
#!/usr/bin/env bash
exit "${TS_STATUS_RC:-0}"
SH
chmod +x "$tmp/bin/systemctl" "$tmp/bin/ss" "$tmp/bin/tailscale"
export PATH="$tmp/bin:$PATH"

bundle="$tmp/bundle"
mkdir -p "$bundle"
cat > "$bundle/manifest.json" <<'JSON'
{
  "version": "v0.2.0",
  "steps": [
    { "id": "10", "optional": false },
    { "id": "83", "optional": true, "service_id": "shelly", "dir": "shelly",
      "unit": "shelly-rpc.service" }
  ]
}
JSON

export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v0.2.0

mkdir -p "$EN_STATE_DIR/steps" "$tmp/root/etc/energy-node/manifests"
printf 'bundle=v0.2.0\n' > "$EN_STATE_DIR/steps/10"
printf 'bundle=v0.1.0\n' > "$EN_STATE_DIR/steps/83"
printf '{}\n' > "$tmp/root/etc/energy-node/config.json"
printf '{"service_id":"shelly"}\n' > "$tmp/root/etc/energy-node/manifests/shelly.json"
printf '{"service_id":"tuya"}\n'   > "$tmp/root/etc/energy-node/manifests/tuya.json"

out="$(ACTIVE="mosquitto.service shelly-rpc.service" LISTENING="1883" \
       TS_STATUS_RC=0 bash "$script")"

get() { python3 -c 'import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1], {"d": d}))' "$1" <<<"$out"; }

[ "$(get 'd["bundle_version"]')" = v0.2.0 ] || fail "bundle_version falsch" "$out"
[ "$(get 'd["steps"]["10"]')" = v0.2.0 ] || fail "Stempel 10 falsch" "$out"
[ "$(get 'd["steps"]["83"]')" = v0.1.0 ] || fail "alter Stempel nicht gemeldet" "$out"
[ "$(get 'd["units"]["mosquitto.service"]')" = active ] || fail "mosquitto nicht aktiv" "$out"
[ "$(get 'd["units"]["caddy.service"]')" = inactive ] || fail "caddy nicht inaktiv" "$out"
# Die Unit aus der Schrittliste muss ohne Codeaenderung auftauchen.
[ "$(get 'd["units"]["shelly-rpc.service"]')" = active ] || fail "Dienst-Unit fehlt" "$out"
[ "$(get 'd["ports"]["1883"]')" = True ] || fail "1883 nicht als offen erkannt" "$out"
[ "$(get 'd["ports"]["8080"]')" = False ] || fail "8080 faelschlich offen" "$out"
[ "$(get 'd["config"]["config.json"]')" = True ] || fail "config.json nicht erkannt" "$out"
[ "$(get 'sorted(d["config"]["manifests"])')" = "['shelly', 'tuya']" ] || fail "Manifeste falsch" "$out"
[ "$(get 'd["tailscale"]["angemeldet"]')" = True ] || fail "tailscale nicht angemeldet" "$out"

# --- kaputter Node: trotzdem Exit 0 und vollstaendiges JSON --------------
rm -rf "$tmp/root" "$tmp/state"
set +e
out="$(ACTIVE="" LISTENING="" TS_STATUS_RC=1 bash "$script")"
rc=$?
set -e
[ "$rc" -eq 0 ] || fail "diagnose.sh bricht bei kaputtem Node ab" "$rc"
[ "$(get 'd["config"]["config.json"]')" = False ] || fail "fehlende config nicht gemeldet" "$out"
[ "$(get 'd["config"]["manifests"]')" = "[]" ] || fail "fehlende Manifeste nicht leer" "$out"
[ "$(get 'd["tailscale"]["angemeldet"]')" = False ] || fail "abgemeldet nicht erkannt" "$out"
[ "$(get 'd["steps"]')" = "{}" ] || fail "Stempel ohne Verzeichnis nicht leer" "$out"

# --- keine ##STEP-Marker --------------------------------------------------
grep -q '^##STEP' <<<"$out" && fail "diagnose.sh gibt Schritt-Marker aus" "$out"

echo "OK: $(basename "$0")"

#!/usr/bin/env bash
# Test for scripts/bootstrap/65-dashboard-config.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../bootstrap/65-dashboard-config.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

bundle="$tmp/bundle"
mkdir -p "$bundle"
cat > "$bundle/manifest.json" <<'JSON'
{
  "steps": [
    {"id": "10", "optional": false},
    {"id": "40", "optional": true, "default": true, "dashboard_key": "tailscale"},
    {"id": "70", "optional": true, "default": true},
    {"id": "81", "optional": true, "default": true, "service_id": "apsystems", "dashboard_key": "apsystems"},
    {"id": "88", "optional": true, "default": true, "service_id": "automation", "dashboard_key": "automation"}
  ]
}
JSON

export EN_STATE_DIR="$tmp/state" EN_ROOT="$tmp/root" EN_BUNDLE_DIR="$bundle"
export EN_BUNDLE_VERSION=v1.0.0 EN_SUDO=""
export EN_SELECTION="$tmp/state/selection.json"

# Der Schritt ruft am Ende "systemctl try-restart" auf (C3) - das darf in
# diesem Test niemals den echten systemd des Testrechners erreichen. Ein
# Fake-Binary vor dem echten systemctl im PATH haelt den Aufruf vollstaendig
# im Sandkasten, statt sich nur auf "2>/dev/null || true" zu verlassen.
mkdir -p "$tmp/bin"
systemctl_log="$tmp/systemctl.log"
cat > "$tmp/bin/systemctl" <<SH
#!/usr/bin/env bash
printf '%s\n' "\$*" >> "$systemctl_log"
exit 1
SH
chmod +x "$tmp/bin/systemctl"
export PATH="$tmp/bin:$PATH"

etc="$tmp/root/etc/energy-node"
mkdir -p "$etc"
printf '{"mqtt":{}}\n' > "$etc/config.json"

run() { bash "$script"; }

# --- ohne selection.json: alles an -----------------------------------------
out="$(run)"
grep -q '^##STEP 65 ok$' <<<"$out" || fail "kein ok-Marker" "$out"
python3 - "$etc/config.json" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1], encoding="utf-8"))
want = {"tailscale": True, "apsystems": True, "automation": True}
got = doc.get("installed_services")
if got != want:
    sys.exit("installed_services = %r, erwartet %r" % (got, want))
if doc.get("mqtt") != {}:
    sys.exit("fremder Schluessel 'mqtt' wurde veraendert")
PY
grep -q 'try-restart energy-node-dashboard.service' "$systemctl_log" \
  || fail "kein try-restart auf die Dashboard-Unit" "$(cat "$systemctl_log" 2>/dev/null || true)"

# --- mit selection.json: automation abgewaehlt -----------------------------
mkdir -p "$tmp/state"
printf '{"steps":{"88":false}}\n' > "$EN_SELECTION"
out="$(run)"
grep -q '^##STEP 65 ok$' <<<"$out" || fail "kein ok-Marker (2. Lauf)" "$out"
python3 - "$etc/config.json" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1], encoding="utf-8"))
want = {"tailscale": True, "apsystems": True, "automation": False}
got = doc.get("installed_services")
if got != want:
    sys.exit("installed_services = %r, erwartet %r" % (got, want))
PY

# --- kaputte selection.json: laut scheitern statt still "alles aus" --------
printf '{not valid json' > "$EN_SELECTION"
before="$(cat "$etc/config.json")"
if out="$(run 2>&1)"; then
  fail "haette an kaputter selection.json scheitern muessen" "$out"
fi
grep -q '^##STEP 65 fail SELECTION_UNREADABLE$' <<<"$out" || fail "falscher/fehlender fail-Marker" "$out"
after="$(cat "$etc/config.json")"
[ "$before" = "$after" ] || fail "config.json wurde trotz Fehlschlag veraendert" "$after"

# selection.json wieder gueltig machen, damit ein Test-Lauf danach nicht
# faelschlich weiter kaputte Zustaende hinterlaesst.
printf '{"steps":{"88":false}}\n' > "$EN_SELECTION"

echo "ok"

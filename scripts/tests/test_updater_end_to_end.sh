#!/usr/bin/env bash
# scripts/tests/test_updater_end_to_end.sh
#
# Baut ein echtes, signiertes Bundle (wie scripts/tests/bootstrap_container.sh)
# und faehrt energy-node-updater.sh dagegen: einmal unangetastet (AC 15,
# ohne den echten Dashboard-Selbst-Neustart -- der braucht ein echtes
# systemd und ist Handtest an echter Hardware, siehe unten), einmal mit
# einem nachtraeglich veraenderten manifest.json (AC 16).
#
# Die Auftrag-Steps sind bewusst nur ["50","60"]: Schritt 20 (Mosquitto)
# verlangt --user/--password-file, die energy-node-updater.sh nie mitgibt
# (ein Redeploy-Auftrag traegt laut Spec nie mqtt.pw/auth.pw) -- das ist
# eine echte, gewollte Eigenschaft des Produktionsdesigns, keine
# Testabkuerzung.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; [ -n "${2:-}" ] && printf '%s\n' "$2"; exit 1; }

# shellcheck disable=SC1091
source "$here/lib/fake_system_commands.sh"
mkdir -p "$tmp/bin"
install_fake_system_commands "$tmp/bin"
cat > "$tmp/bin/fakepip" <<'SH'
#!/usr/bin/env bash
printf 'pip %s\n' "$*" >> "$CMD_LOG"
exit 0
SH
chmod +x "$tmp/bin/fakepip"
export PATH="$tmp/bin:$PATH" CMD_LOG="$tmp/cmd.log"

case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  armv6l|armv7l) arch=armv6 ;;
  *) fail "unbekannte Testarchitektur: $(uname -m)" ;;
esac
minor="$(python3 -c 'import sys; print("%d.%d" % sys.version_info[:2])')"

# --- ein echtes, signiertes Bundle bauen (dev-Schluessel wie test_build_manifest_sign.sh) ---
key="$tmp/dev-signing-key.pem"
openssl genpkey -algorithm ed25519 -out "$key" >/dev/null 2>&1
pub="$tmp/dev-signing-key.pub.pem"
openssl pkey -in "$key" -pubout -out "$pub" >/dev/null 2>&1

printf '#!/bin/sh\n' > "$tmp/fake-dashboard"
mkdir -p "$tmp/pack/tailscale_1.62.0_arm/systemd"
printf '#!/bin/sh\n' > "$tmp/pack/tailscale_1.62.0_arm/tailscale"
printf '#!/bin/sh\n' > "$tmp/pack/tailscale_1.62.0_arm/tailscaled"
printf '[Unit]\n'    > "$tmp/pack/tailscale_1.62.0_arm/systemd/tailscaled.service"
printf 'FLAGS=""\n'  > "$tmp/pack/tailscale_1.62.0_arm/systemd/tailscaled.defaults"
tar -czf "$tmp/ts.tgz" -C "$tmp/pack" tailscale_1.62.0_arm
printf '#!/bin/sh\n' > "$tmp/fake-caddy"

dist="$tmp/dist"
bash "$repo/scripts/build/make_bundle.sh" \
  --arch "$arch" --python-minor "$minor" \
  --user pruef --base /home/pruef --out "$dist" --sign-key "$key" \
  --dashboard-binary "$tmp/fake-dashboard" \
  --tailscale-tarball "$tmp/ts.tgz" \
  --caddy-binary "$tmp/fake-caddy" \
  --skip-wheels >"$tmp/build.log" 2>&1 \
  || fail "make_bundle.sh failed to build a signed bundle" "$(cat "$tmp/build.log")"

version="$(tr -d '[:space:]' < "$repo/dashboard/VERSION")"

stage_job() {
  local jobdir="$1"
  mkdir -p "$jobdir/bundle"
  tar -xzf "${dist}/energy-node-${version}-${arch}.tar.gz" -C "$jobdir/bundle"
  # --skip-wheels leaves wheels/ with only LEER placeholder. Add a real placeholder wheel
  # without removing LEER - we only ADD files that aren't in manifest.json's files dict.
  if [ ! -d "$jobdir/bundle/wheels" ]; then
    mkdir -p "$jobdir/bundle/wheels"
  fi
  printf 'platzhalter\n' > "$jobdir/bundle/wheels/energy_node_common-0.0.0-py3-none-any.whl"
  python3 - "$jobdir/bundle/manifest.json" "$jobdir/job.json" <<'PY'
import json, sys
manifest = json.load(open(sys.argv[1]))
job = {
    "bundle_version": manifest["version"],
    "mode": "redeploy",
    "target_user": "pruef",
    "target_base": "/home/pruef",
    "steps": ["50", "60"],
}
json.dump(job, open(sys.argv[2], "w"))
PY
  mv "$jobdir/job.json" "$jobdir/pending.json"
}

export EN_ROOT="$tmp/root" EN_SUDO="" EN_PIP="$tmp/bin/fakepip"

# --- AC 15 (Form): unangetastetes, korrekt signiertes Bundle laeuft durch ---
job="$tmp/job"
stage_job "$job"
EN_UPDATER_JOB_DIR="$job" EN_UPDATER_VERIFY="$repo/scripts/bootstrap/verify_bundle.sh" \
  EN_UPDATER_PUBKEY="$pub" EN_UPDATER_STATE_DIR="$tmp/state" \
  bash "$repo/dashboard/energy-node-updater.sh" \
  || fail "updater exited non-zero on a valid job" "$(cat "$job/log" 2>/dev/null)"
grep -q '"result":"ok"' "$job/status.json" 2>/dev/null \
  || fail "an untouched, correctly signed bundle must run to completion" "$(cat "$job/status.json" 2>/dev/null)"

# --- AC 16: eine veraenderte manifest.json muss abgelehnt werden, bevor irgendein Schritt laeuft ---
job2="$tmp/job2"
stage_job "$job2"
python3 - "$job2/bundle/manifest.json" <<'PY'
import json, sys
path = sys.argv[1]
data = json.load(open(path))
data["version"] = "9.9.9-tampered"
json.dump(data, open(path, "w"))
PY

if EN_UPDATER_JOB_DIR="$job2" EN_UPDATER_VERIFY="$repo/scripts/bootstrap/verify_bundle.sh" \
   EN_UPDATER_PUBKEY="$pub" EN_UPDATER_STATE_DIR="$tmp/state2" \
   bash "$repo/dashboard/energy-node-updater.sh"; then
  fail "a tampered manifest.json must be rejected"
fi
[ ! -f "$job2/log" ] || grep -q '##STEP' "$job2/log" && fail "no step may have run against a rejected bundle" "$(cat "$job2/log")"
grep -q '"result":"rejected"' "$job2/status.json" || fail "status.json must record the rejection" "$(cat "$job2/status.json" 2>/dev/null)"

echo "OK"
echo
echo "Nicht durch diesen Test bewiesen (Handtest an echter Hardware, wie die"
echo "ARMv6-Eigenheiten in der Spec): dass energy-node-updater.path unter"
echo "echtem systemd tatsaechlich auf pending.json anspringt, und dass der"
echo "Dashboard-Prozess einen echten Neustart von energy-node-dashboard.service"
echo "mitten im Schritt 60 uebersteht und sein SSE-Strom weiterlaeuft."

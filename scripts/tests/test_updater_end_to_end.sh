#!/usr/bin/env bash
# scripts/tests/test_updater_end_to_end.sh
#
# Baut ein echtes, signiertes Bundle (wie scripts/tests/bootstrap_container.sh)
# und faehrt energy-node-updater.sh dagegen: einmal unangetastet (AC 15,
# ohne den echten Dashboard-Selbst-Neustart -- der braucht ein echtes
# systemd und ist Handtest an echter Hardware, siehe unten), einmal mit
# einem nachtraeglich veraenderten manifest.json (AC 16).
#
# Die Schrittliste des Auftrags kommt aus dem gebauten Manifest, nicht aus
# einer Liste hier: genau wie updaterhost.Host.Run sie bildet, naemlich
# jeder nicht-optionale Schritt (10, 20, 30, 50, 60, 65). Eine fest
# verdrahtete Liste ["50","60"] uebersprang frueher Schritt 20 und liess
# damit unbemerkt, dass ein echter Redeploy dort scheiterte.
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
  # Derselbe Auftrag, den updaterhost.Host.Run ohne "only" stellt: jeder
  # nicht-optionale Schritt des Manifests. target_user/target_base stehen
  # bewusst nicht drin - der Updater nimmt sie aus dem signierten Manifest.
  python3 - "$jobdir/bundle/manifest.json" "$jobdir/job.json" <<'PY'
import json, sys
manifest = json.load(open(sys.argv[1]))
steps = [s["id"] for s in manifest["steps"] if not s.get("optional")]
if not steps:
    raise SystemExit("das gebaute Manifest nennt keinen Kern-Schritt")
job = {"bundle_version": manifest["version"], "mode": "redeploy", "steps": steps}
json.dump(job, open(sys.argv[2], "w"))
PY
  mv "$jobdir/job.json" "$jobdir/pending.json"
}

export EN_ROOT="$tmp/root" EN_SUDO="" EN_PIP="$tmp/bin/fakepip"

# Ein Redeploy laeuft gegen einen bereits eingerichteten Knoten: config.json
# und mqtt.pw liegen dort seit der Erstinstallation. Genau von dort holt die
# Updater-Unit die Argumente fuer Schritt 20 - durch den Auftrag fliesst kein
# Geheimnis.
mkdir -p "$tmp/root/etc/energy-node"
cp "$repo/services/energy-node.config.json" "$tmp/root/etc/energy-node/config.json"
printf 'geheim\n' > "$tmp/root/etc/energy-node/mqtt.pw"
chmod 600 "$tmp/root/etc/energy-node/mqtt.pw"

# --- AC 15 (Form): unangetastetes, korrekt signiertes Bundle laeuft durch ---
job="$tmp/job"
stage_job "$job"
EN_UPDATER_JOB_DIR="$job" EN_UPDATER_VERIFY="$repo/scripts/bootstrap/verify_bundle.sh" \
  EN_UPDATER_PUBKEY="$pub" EN_UPDATER_STATE_DIR="$tmp/state" \
  bash "$repo/dashboard/energy-node-updater.sh" \
  || fail "updater exited non-zero on a valid job" "$(cat "$job/log" 2>/dev/null)"
grep -q '"result":"ok"' "$job/status.json" 2>/dev/null \
  || fail "an untouched, correctly signed bundle must run to completion" "$(cat "$job/status.json" 2>/dev/null)"
# Jeder Kern-Schritt muss wirklich gelaufen sein. Schritt 20 steht hier
# ausdruecklich: er war der Schritt, den die alte feste Liste ausliess und
# an dem ein echter Redeploy deshalb unbemerkt scheiterte.
for id in 10 20 30 50 60 65; do
  grep -q "##STEP ${id} ok" "$job/log" \
    || fail "step ${id} did not run to ok in a normal redeploy" "$(cat "$job/log")"
done
[ ! -e "$job/bundle" ] || fail "the bundle must leave the dashboard-writable job dir before it is verified"

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
if [ -f "$job2/log" ] && grep -q '##STEP' "$job2/log"; then
  fail "no step may have run against a rejected bundle" "$(cat "$job2/log")"
fi
grep -q '"result":"rejected"' "$job2/status.json" || fail "status.json must record the rejection" "$(cat "$job2/status.json" 2>/dev/null)"

echo "OK"
echo
echo "Nicht durch diesen Test bewiesen (Handtest an echter Hardware, wie die"
echo "ARMv6-Eigenheiten in der Spec): dass energy-node-updater.path unter"
echo "echtem systemd tatsaechlich auf pending.json anspringt, und dass der"
echo "Dashboard-Prozess einen echten Neustart von energy-node-dashboard.service"
echo "mitten im Schritt 60 uebersteht und sein SSE-Strom weiterlaeuft."

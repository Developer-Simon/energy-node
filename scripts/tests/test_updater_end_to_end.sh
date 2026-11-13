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

dist="$tmp/dist"
bash "$repo/scripts/build/make_bundle.sh" \
  --arch "$arch" --python-minor "$minor" \
  --user pruef --base /home/pruef --out "$dist" --sign-key "$key" \
  --dashboard-binary "$tmp/fake-dashboard" \
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
cp "$repo/dashboard/internal/appconfig/energy-node.config.json" "$tmp/root/etc/energy-node/config.json"
printf 'geheim\n' > "$tmp/root/etc/energy-node/mqtt.pw"
chmod 600 "$tmp/root/etc/energy-node/mqtt.pw"
# Schritt 67 leitet den Hotspot-Namen aus einer Kennung ab. Ohne WLAN-Geraet
# im Test-Sysfs ist das die machine-id.
printf 'a1b2c3d4e5f60718a9b0c1d2e3f40516\n' > "$tmp/root/etc/machine-id"

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

# --- Schritt 5: geaenderte Wheel-Festlegung wird auf dem Node geladen --------
# Ein lokaler http-Server spielt piwheels. EN_WHEEL_HOSTS_TEST erlaubt seinen
# Host; das Manifest wird neu signiert, weil es sich geaendert hat.
wheel_srv="$tmp/wheel-srv"
mkdir -p "$wheel_srv"
wheel_name="paho_mqtt-2.1.0-py3-none-any.whl"
printf 'wheel payload\n' > "$wheel_srv/$wheel_name"
wheel_sha="$(sha256sum "$wheel_srv/$wheel_name" | cut -d' ' -f1)"
python3 -u -m http.server --bind 127.0.0.1 0 --directory "$wheel_srv" >"$tmp/wheel-srv.log" 2>&1 &
wheel_srv_pid=$!
trap 'kill "${wheel_srv_pid}" 2>/dev/null || true; rm -rf "$tmp"' EXIT
wheel_port=""
for _ in $(seq 100); do
  wheel_port="$(grep -o 'port [0-9]*' "$tmp/wheel-srv.log" | head -n 1 | cut -d' ' -f2 || true)"
  [ -n "$wheel_port" ] && break
  sleep 0.1
done
[ -n "$wheel_port" ] || fail "wheel http server did not start" "$(cat "$tmp/wheel-srv.log")"

job3="$tmp/job3"
stage_job "$job3"
python3 - "$job3/bundle/manifest.json" "$wheel_name" "$wheel_port" "$wheel_sha" <<'PY'
import json, sys
path, name, port, sha = sys.argv[1:5]
data = json.load(open(path))
data["wheels"] = [{"file": name, "url": "http://127.0.0.1:%s/%s" % (port, name), "sha256": sha}]
json.dump(data, open(path, "w"), indent=2)
PY
# Wie der Bundle-Bau: Fingerabdruecke und Datei-Hashes neu berechnen, weil sich
# manifest.wheels geaendert hat (Schritt 50 haengt davon ab), dann neu signieren.
python3 - "$job3/bundle" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
head = json.loads((root / "manifest.json").read_text(encoding="utf-8"))
head.pop("files", None)
for step in head.get("steps") or []:
    step.pop("fingerprint", None)
(root / "manifest.head.json").write_text(json.dumps(head, indent=2, ensure_ascii=False), encoding="utf-8")
PY
bash -c 'source "$1"; write_bundle_manifest "$2"' _ "$repo/scripts/build/lib/manifest.sh" "$job3/bundle" \
  || fail "recomputing the bundle manifest failed"
openssl pkeyutl -sign -inkey "$key" -rawin -in "$job3/bundle/manifest.json" \
  -out "$job3/bundle/manifest.json.sig" >/dev/null 2>&1 \
  || fail "re-signing the changed manifest failed"

# Gleicher Zustandsordner wie der erste Lauf: nur Schritt 50 hat einen neuen
# Fingerabdruck, die anderen Schritte bleiben erledigt.
EN_WHEEL_HOSTS_TEST=127.0.0.1 EN_UPDATER_JOB_DIR="$job3" \
  EN_UPDATER_VERIFY="$repo/scripts/bootstrap/verify_bundle.sh" \
  EN_UPDATER_PUBKEY="$pub" EN_UPDATER_STATE_DIR="$tmp/state" \
  bash "$repo/dashboard/energy-node-updater.sh" \
  || fail "updater failed with a changed wheel pin" "$(cat "$job3/log" 2>/dev/null)"
grep -q '"result":"ok"' "$job3/status.json" 2>/dev/null \
  || fail "changed wheel pin must run to completion" "$(cat "$job3/status.json" 2>/dev/null)"
grep -q '##STEP 50 ok' "$job3/log" || fail "step 50 did not run to ok" "$(cat "$job3/log")"
cmp -s "$wheel_srv/$wheel_name" "$tmp/state/wheels/$wheel_name" \
  || fail "changed wheel pin was not downloaded to the node"
grep -q -- "$tmp/state/wheels/$wheel_name" "$CMD_LOG" \
  || fail "downloaded wheel was not handed to pip" "$(cat "$CMD_LOG")"

echo "OK"
echo
echo "Nicht durch diesen Test bewiesen (Handtest an echter Hardware, wie die"
echo "ARMv6-Eigenheiten in der Spec): dass energy-node-updater.path unter"
echo "echtem systemd tatsaechlich auf pending.json anspringt, und dass der"
echo "Dashboard-Prozess einen echten Neustart von energy-node-dashboard.service"
echo "mitten im Schritt 60 uebersteht und sein SSE-Strom weiterlaeuft."

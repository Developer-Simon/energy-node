#!/usr/bin/env bash
# scripts/tests/test_updater.sh
#
# energy-node-updater.sh laeuft ausserhalb jeder Bash-Testinfrastruktur als
# root ueber systemd; hier wird nur seine eigene Logik geprueft, mit
# Attrappen fuer verify_bundle.sh und die Bootstrap-Skripte selbst.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; exit 1; }

job="$tmp/job"
mkdir -p "$job/bundle/bootstrap"

# --- Attrappe fuer die persistente verify_bundle.sh-Kopie -----------------
verify_ok=true
verify_stub="$tmp/verify_bundle.sh"
cat > "$verify_stub" <<SH
#!/usr/bin/env bash
if [ "$verify_ok" = true ]; then echo OK; exit 0; fi
echo "FEHLER BUNDLE_SIGNATURE_INVALID"; exit 1
SH
chmod +x "$verify_stub"

pubkey="$tmp/pubkey.pem"
: > "$pubkey"

# --- Attrappen-Bootstrap-Skripte ------------------------------------------
cat > "$job/bundle/bootstrap/50-python-deps.sh" <<'SH'
#!/usr/bin/env bash
echo "##STEP 50 begin"
echo "pip install --no-index --find-links wheels/"
echo "##STEP 50 ok"
SH
cat > "$job/bundle/bootstrap/60-node-install.sh" <<'SH'
#!/usr/bin/env bash
echo "##STEP 60 begin"
echo "energy-node-dashboard installiert"
echo "##STEP 60 ok"
SH
chmod +x "$job/bundle/bootstrap"/*.sh

# --- Manifest with bundle version ------------------------------------------
cat > "$job/bundle/manifest.json" <<'JSON'
{"version":"1.5.0"}
JSON

cat > "$job/job.json" <<'JSON'
{"bundle_version":"1.5.0","mode":"redeploy","target_user":"energynode","target_base":"/home/energynode","steps":["50","60"]}
JSON
mv "$job/job.json" "$job/pending.json"

EN_UPDATER_JOB_DIR="$job" EN_UPDATER_VERIFY="$verify_stub" EN_UPDATER_PUBKEY="$pubkey" \
  EN_UPDATER_STATE_DIR="$tmp/state" \
  bash "$repo/dashboard/energy-node-updater.sh" \
  || fail "updater exited non-zero on a valid job"

[ -f "$job/current.json" ] || fail "pending.json was not claimed into current.json"
[ ! -f "$job/pending.json" ] || fail "pending.json must be gone once claimed"
grep -q '##STEP 50 ok' "$job/log" || fail "step 50 marker missing from log"
grep -q '##STEP 60 ok' "$job/log" || fail "step 60 marker missing from log"
grep -q '"result":"ok"' "$job/status.json" || fail "status.json must report ok: $(cat "$job/status.json")"

# --- zweiter Fall: die Signatur ist ungueltig -----------------------------
job2="$tmp/job2"
mkdir -p "$job2/bundle/bootstrap"
cp "$job/bundle/bootstrap"/*.sh "$job2/bundle/bootstrap/"
cp "$job/bundle/manifest.json" "$job2/bundle/manifest.json"
cat > "$job2/job.json" <<'JSON'
{"bundle_version":"1.5.0","mode":"redeploy","target_user":"energynode","target_base":"/home/energynode","steps":["50","60"]}
JSON
mv "$job2/job.json" "$job2/pending.json"

verify_ok=false
cat > "$verify_stub" <<SH
#!/usr/bin/env bash
echo "FEHLER BUNDLE_SIGNATURE_INVALID"; exit 1
SH
chmod +x "$verify_stub"

if EN_UPDATER_JOB_DIR="$job2" EN_UPDATER_VERIFY="$verify_stub" EN_UPDATER_PUBKEY="$pubkey" \
   EN_UPDATER_STATE_DIR="$tmp/state2" \
   bash "$repo/dashboard/energy-node-updater.sh"; then
  fail "updater must exit non-zero when verification fails"
fi
[ ! -f "$job2/log" ] || grep -q '##STEP' "$job2/log" && fail "no step may have run for a rejected bundle"
grep -q '"result":"rejected"' "$job2/status.json" || fail "status.json must report rejected: $(cat "$job2/status.json")"
grep -q '"code":"BUNDLE_SIGNATURE_INVALID"' "$job2/status.json" || fail "status.json must carry the fault code"

echo "OK"

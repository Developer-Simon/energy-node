#!/usr/bin/env bash
# scripts/tests/test_updater_backup.sh
#
# Prueft das Backup vor einem Versionswechsel in energy-node-updater.sh. runuser
# wird durch eine Attrappe ersetzt, die ihren Aufruf protokolliert und mit dem
# Code aus dem Testfall endet; das Dashboard-Binary liegt unter einer
# Zielbasis im Testverzeichnis.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $1"; exit 1; }

verify_stub="$tmp/verify_bundle.sh"
printf '#!/usr/bin/env bash\necho OK; exit 0\n' > "$verify_stub"
chmod +x "$verify_stub"
pubkey="$tmp/pubkey.pem"
: > "$pubkey"

# --- Attrappe fuer runuser -------------------------------------------------
# Die Vorlage bekommt den Exit-Code per sed (@RC@); jeder Aufruf landet in
# der Datei aus EN_TEST_CALLS.
mkdir -p "$tmp/bin"
cat > "$tmp/runuser.tpl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${EN_TEST_CALLS}"
exit @RC@
SH

# --- Bundle mit einem Schritt 50 und eigener Zielbasis ----------------------
fixture="$tmp/fixture"
mkdir -p "$fixture/bootstrap"
cat > "$fixture/bootstrap/50-python-deps.sh" <<'SH'
#!/usr/bin/env bash
echo "##STEP 50 begin"
echo "##STEP 50 ok"
SH
chmod +x "$fixture/bootstrap/50-python-deps.sh"

target_base="$tmp/home/energynode"
binary="$target_base/dashboard/energy-node-dashboard"

stage() {
  local dir="$1" installed="$2" with_binary="$3"
  mkdir -p "$dir/bundle" "$dir/state"
  cp -a "$fixture/." "$dir/bundle/"
  printf '{"version":"1.5.0","target_user":"energynode","target_base":"%s","steps":[{"id":"50","optional":false}]}\n' \
    "$target_base" > "$dir/bundle/manifest.json"
  if [ "$installed" != none ]; then
    printf '{"version":"%s"}\n' "$installed" > "$dir/state/installed-manifest.json"
  fi
  rm -rf "$target_base"
  if [ "$with_binary" = yes ]; then
    mkdir -p "$(dirname "$binary")"
    printf '#!/usr/bin/env bash\n' > "$binary"
    chmod +x "$binary"
  fi
  printf '{"bundle_version":"1.5.0","mode":"redeploy","steps":["50"]}\n' > "$dir/pending.json"
}

# run_case NAME INSTALLED BINARY(yes|no) RC: fuehrt den Updater aus, RC ist der
# Exit-Code der runuser-Attrappe. Ergebnis: $dir, $calls.
run_case() {
  local name="$1" installed="$2" with_binary="$3" rc="$4"
  dir="$tmp/$name"
  calls="$tmp/$name.calls"
  sed "s/@RC@/${rc}/" "$tmp/runuser.tpl" > "$tmp/bin/runuser"
  chmod +x "$tmp/bin/runuser"
  stage "$dir" "$installed" "$with_binary"
  EN_TEST_CALLS="$calls" PATH="$tmp/bin:$PATH" \
  EN_UPDATER_JOB_DIR="$dir" EN_UPDATER_VERIFY="$verify_stub" EN_UPDATER_PUBKEY="$pubkey" \
    EN_UPDATER_STATE_DIR="$dir/state" EN_UPDATER_TARGET="$tmp/absent-target.json" \
    EN_ROOT="$tmp/root" \
    bash "$repo/dashboard/energy-node-updater.sh" \
    || fail "updater exited non-zero ($name): $(cat "$dir/log" 2>/dev/null)"
}

# --- Versionswechsel, Backup gelingt -----------------------------------------
run_case ok 1.4.0 yes 0
grep -q '##BACKUP ok' "$dir/log" || fail "a successful backup must log ##BACKUP ok: $(cat "$dir/log")"
grep -qF -- "-u energynode -- $binary backup create --config $tmp/root/etc/energy-node/config.json --trigger update" "$calls" \
  || fail "backup must run as the target user with the node config and trigger update: $(cat "$calls" 2>&1)"
backup_line="$(grep -n '##BACKUP' "$dir/log" | cut -d: -f1)"
step_line="$(grep -n '##STEP 50 begin' "$dir/log" | cut -d: -f1)"
[ "$backup_line" -lt "$step_line" ] || fail "the backup must run before the first step: $(cat "$dir/log")"
grep -q '##STEP 50 ok' "$dir/log" || fail "the update must still complete"

# --- Exit-Code 3: Backup uebersprungen ---------------------------------------
run_case skipped 1.4.0 yes 3
grep -q '##BACKUP skipped' "$dir/log" || fail "exit 3 must log ##BACKUP skipped: $(cat "$dir/log")"
grep -q '##STEP 50 ok' "$dir/log" || fail "a skipped backup must not stop the update"

# --- Fehlschlag: Warnung, das Update laeuft weiter --------------------------
run_case failed 1.4.0 yes 1
grep -q 'WARN BACKUP_FAILED' "$dir/log" || fail "exit 1 must log WARN BACKUP_FAILED: $(cat "$dir/log")"
grep -q '##STEP 50 ok' "$dir/log" || fail "a failed backup must not stop the update: $(cat "$dir/log")"
grep -q '"result":"ok"' "$dir/status.json" || fail "the job must still end ok: $(cat "$dir/status.json")"

# --- Kein Binary: still uebersprungen ----------------------------------------
run_case nobinary 1.4.0 no 0
[ ! -e "$calls" ] || fail "without a binary runuser must not be called"
grep -q 'BACKUP' "$dir/log" && fail "without a binary no backup line may be logged: $(cat "$dir/log")"
grep -q '##STEP 50 ok' "$dir/log" || fail "the update must still complete without a binary"

# --- Gleiche Version oder Erstinstallation: kein Backup ----------------------
run_case sameversion 1.5.0 yes 0
[ ! -e "$calls" ] || fail "re-applying the same version must not back up"

run_case firstinstall none yes 0
[ ! -e "$calls" ] || fail "a first installation has nothing to back up"

echo "OK"

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

# --- Attrappe fuer die persistente verify_bundle.sh-Kopie -----------------
verify_stub="$tmp/verify_bundle.sh"
accept_bundle() {
  cat > "$verify_stub" <<'SH'
#!/usr/bin/env bash
echo OK; exit 0
SH
  chmod +x "$verify_stub"
}
reject_bundle() {
  cat > "$verify_stub" <<'SH'
#!/usr/bin/env bash
echo "FEHLER BUNDLE_SIGNATURE_INVALID"; exit 1
SH
  chmod +x "$verify_stub"
}
accept_bundle

pubkey="$tmp/pubkey.pem"
: > "$pubkey"

# --- Attrappen-Bootstrap-Skripte ------------------------------------------
fixture="$tmp/fixture"
mkdir -p "$fixture/bootstrap"
# Die 20er-Attrappe prueft dasselbe wie das echte scripts/bootstrap/20-mosquitto.sh:
# ohne --user/--password-file scheitert sie, noch vor jeder Skip-Pruefung.
cat > "$fixture/bootstrap/20-mosquitto.sh" <<'SH'
#!/usr/bin/env bash
echo "##STEP 20 begin"
user=""; pwfile=""
while [ $# -gt 0 ]; do
  case "$1" in
    --user) user="$2"; shift 2 ;;
    --password-file) pwfile="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -z "$user" ] || [ -z "$pwfile" ] || [ ! -f "$pwfile" ]; then
  echo "##STEP 20 fail MOSQUITTO_ARGS_MISSING"; exit 1
fi
echo "Broker eingerichtet, Benutzer $user."
echo "##STEP 20 ok"
SH
cat > "$fixture/bootstrap/50-python-deps.sh" <<'SH'
#!/usr/bin/env bash
echo "##STEP 50 begin"
echo "pip install --no-index --find-links wheels/"
echo "##STEP 50 ok"
SH
cat > "$fixture/bootstrap/60-node-install.sh" <<'SH'
#!/usr/bin/env bash
echo "##STEP 60 begin"
echo "energy-node-dashboard installiert nach ${EN_TARGET_BASE} fuer ${EN_TARGET_USER}"
echo "sudo=[${EN_SUDO-UNSET}]"
echo "##STEP 60 ok"
SH
chmod +x "$fixture/bootstrap"/*.sh

# Zielbenutzer, Zielbasis und die Schrittliste stehen im Manifest, nicht im
# Auftrag: alles, was einen root-Lauf steuert, muss von der Signatur gedeckt
# sein, und job.json ist es nicht.
cat > "$fixture/manifest.json" <<'JSON'
{"version":"1.5.0","target_user":"energynode","target_base":"/home/energynode",
 "steps":[{"id":"20","optional":false},{"id":"50","optional":false},{"id":"60","optional":false}]}
JSON

# Benutzername und Passwortpfad stehen schon auf dem Knoten - der Auftrag
# traegt keine Geheimnisse.
mkdir -p "$tmp/root/etc/energy-node"
cat > "$tmp/root/etc/energy-node/config.json" <<'JSON'
{"mqtt":{"username":"knoten","password_file":"/etc/energy-node/mqtt.pw"}}
JSON
printf 'geheim\n' > "$tmp/root/etc/energy-node/mqtt.pw"
chmod 600 "$tmp/root/etc/energy-node/mqtt.pw"

stage_job() {
  local dir="$1" steps="$2"
  mkdir -p "$dir/bundle"
  cp -a "$fixture/." "$dir/bundle/"
  printf '%s\n' "$steps" > "$dir/pending.json"
}

run_updater() {
  local dir="$1"
  EN_UPDATER_JOB_DIR="$dir" EN_UPDATER_VERIFY="$verify_stub" EN_UPDATER_PUBKEY="$pubkey" \
    EN_UPDATER_STATE_DIR="$dir/state" EN_UPDATER_TARGET="${TARGET_JSON:-$tmp/absent-target.json}" \
    EN_ROOT="$tmp/root" \
    bash "$repo/dashboard/energy-node-updater.sh"
}

# --- erster Fall: gueltiger Auftrag laeuft durch --------------------------
job="$tmp/job"
stage_job "$job" '{"bundle_version":"1.5.0","mode":"redeploy","steps":["20","50","60"]}'
run_updater "$job" || fail "updater exited non-zero on a valid job: $(cat "$job/log" 2>/dev/null)"

[ -f "$job/current.json" ] || fail "pending.json was not claimed into current.json"
[ ! -f "$job/pending.json" ] || fail "pending.json must be gone once claimed"
[ ! -e "$job/bundle" ] || fail "the bundle must leave the dashboard-writable job dir before it is verified"
[ -f "$job/state/run/bundle/manifest.json" ] || fail "the bundle was not claimed into the root-only run dir"
grep -q '##STEP 20 ok' "$job/log" || fail "step 20 marker missing from log: $(cat "$job/log")"
grep -q 'Benutzer knoten' "$job/log" || fail "step 20 did not get --user from the node's config.json: $(cat "$job/log")"
grep -q '##STEP 50 ok' "$job/log" || fail "step 50 marker missing from log"
grep -q '##STEP 60 ok' "$job/log" || fail "step 60 marker missing from log"
grep -q 'nach /home/energynode fuer energynode' "$job/log" \
  || fail "target user/base did not come from the manifest: $(cat "$job/log")"
grep -q 'sudo=\[\]' "$job/log" || fail "steps must run with EN_SUDO empty: $(cat "$job/log")"
grep -q '"result":"ok"' "$job/status.json" || fail "status.json must report ok: $(cat "$job/status.json")"

# --- zweiter Fall: die Signatur ist ungueltig -----------------------------
job2="$tmp/job2"
stage_job "$job2" '{"bundle_version":"1.5.0","mode":"redeploy","steps":["50","60"]}'
reject_bundle
if run_updater "$job2"; then
  fail "updater must exit non-zero when verification fails"
fi
if [ -f "$job2/log" ] && grep -q '##STEP' "$job2/log"; then
  fail "no step may have run for a rejected bundle: $(cat "$job2/log")"
fi
grep -q '"result":"rejected"' "$job2/status.json" || fail "status.json must report rejected: $(cat "$job2/status.json")"
grep -q '"code":"BUNDLE_SIGNATURE_INVALID"' "$job2/status.json" || fail "status.json must carry the fault code"
accept_bundle

# --- dritter Fall: eine Kennung, die das gepruefte Manifest nicht kennt ---
# Der Glob wuerde sonst aus bootstrap/ herausgreifen und root fuehrte aus,
# worauf er zeigt - genau die Umgehung von E13, die job.json erlaubt haette.
job3="$tmp/job3"
stage_job "$job3" '{"bundle_version":"1.5.0","mode":"redeploy","steps":["../../../bin/60"]}'
mkdir -p "$tmp/root/bin"
cat > "$tmp/root/bin/60-boese.sh" <<'SH'
#!/usr/bin/env bash
echo "##STEP 60 begin"
touch "$tmp/PWNED"
echo "##STEP 60 ok"
SH
if run_updater "$job3"; then
  fail "a step id outside the manifest must not run"
fi
[ ! -e "$tmp/PWNED" ] || fail "a crafted step id reached a script outside the bundle"
grep -q '"code":"STEP_NOT_IN_MANIFEST"' "$job3/status.json" \
  || fail "status.json must name the rejected step id: $(cat "$job3/status.json")"

# --- vierter Fall: ein Auftrag ohne Schritte darf nicht "ok" melden -------
job4="$tmp/job4"
stage_job "$job4" '{"bundle_version":"1.5.0","mode":"redeploy","steps":[]}'
if run_updater "$job4"; then
  fail "an empty step list must not be reported as a successful run"
fi
grep -q '"code":"JOB_NO_STEPS"' "$job4/status.json" \
  || fail "status.json must report JOB_NO_STEPS: $(cat "$job4/status.json")"

# --- fuenfter Fall: unlesbares job.json ----------------------------------
job5="$tmp/job5"
stage_job "$job5" '{"bundle_version":"1.5.0"'
if run_updater "$job5" 2>/dev/null; then
  fail "a malformed job.json must not be reported as a successful run"
fi
grep -q '"code":"JOB_MALFORMED"' "$job5/status.json" \
  || fail "status.json must report JOB_MALFORMED: $(cat "$job5/status.json")"

# --- sechster Fall: ein Fehlercode mit Sonderzeichen bleibt gueltiges JSON -
job6="$tmp/job6"
stage_job "$job6" '{"bundle_version":"1.5.0","mode":"redeploy","steps":["50"]}'
cat > "$job6/bundle/bootstrap/50-python-deps.sh" <<'SH'
#!/usr/bin/env bash
echo "##STEP 50 begin"
echo '##STEP 50 fail WHEEL"BROKEN\'
exit 1
SH
chmod +x "$job6/bundle/bootstrap/50-python-deps.sh"
if run_updater "$job6"; then
  fail "a failing step must make the updater exit non-zero"
fi
python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$job6/status.json" \
  || fail "status.json must stay parsable even for a code with quotes: $(cat "$job6/status.json")"

# --- siebter Fall: ein abgewuergter Lauf hinterlaesst trotzdem einen Status -
# Ohne ihn blieben current.json ohne status.json stehen, updaterjob.InFlight
# waere fuer immer wahr und Stage() lehnte jeden weiteren Auftrag ab.
job7="$tmp/job7"
stage_job "$job7" '{"bundle_version":"1.5.0","mode":"redeploy","steps":["50"]}'
cat > "$job7/bundle/bootstrap/50-python-deps.sh" <<'SH'
#!/usr/bin/env bash
echo "##STEP 50 begin"
kill -TERM "$PPID"
sleep 5
SH
chmod +x "$job7/bundle/bootstrap/50-python-deps.sh"
run_updater "$job7" 2>/dev/null || true
[ -f "$job7/status.json" ] || fail "an interrupted updater must still leave a terminal status"
grep -q '"code":"UPDATER_INTERRUPTED"' "$job7/status.json" \
  || fail "an interrupted run must report UPDATER_INTERRUPTED: $(cat "$job7/status.json")"

# --- Zielaufloesung (user-unabhaengige Bundles) ----------------------------
generic="$tmp/fixture-generic"
cp -a "$fixture" "$generic"
cat > "$generic/manifest.json" <<'JSON'
{"version":"1.6.0","steps":[{"id":"20","optional":false},{"id":"50","optional":false},{"id":"60","optional":false}]}
JSON
stage_generic() {
  local dir="$1" steps="$2"
  mkdir -p "$dir/bundle"
  cp -a "$generic/." "$dir/bundle/"
  printf '%s\n' "$steps" > "$dir/pending.json"
}
job_steps='{"bundle_version":"1.6.0","mode":"redeploy","steps":["60"]}'
no_steps_ran() { ! { [ -f "$1/log" ] && grep -q '##STEP' "$1/log"; }; }

# a) allgemeines Manifest + target.json: der Schritt bekommt das Ziel des Knotens
printf '{"user":"orgelbau","base":"/home/orgelbau"}\n' > "$tmp/target-orgelbau.json"
job_a="$tmp/job-a"; stage_generic "$job_a" "$job_steps"
TARGET_JSON="$tmp/target-orgelbau.json" run_updater "$job_a" \
  || fail "generic bundle + target.json must run: $(cat "$job_a/log" 2>/dev/null)"
grep -q 'nach /home/orgelbau fuer orgelbau' "$job_a/log" \
  || fail "target did not come from target.json: $(cat "$job_a/log")"

# b) allgemeines Manifest ohne target.json: TARGET_UNKNOWN, kein Schritt laeuft
job_b="$tmp/job-b"; stage_generic "$job_b" "$job_steps"
if run_updater "$job_b"; then fail "must fail without any target source"; fi
grep -q '"code":"TARGET_UNKNOWN"' "$job_b/status.json" || fail "expected TARGET_UNKNOWN: $(cat "$job_b/status.json")"
no_steps_ran "$job_b" || fail "no step may run without a target"

# c) festgelegtes Manifest (energynode) gegen target.json (orgelbau): TARGET_MISMATCH
job_c="$tmp/job-c"; stage_job "$job_c" '{"bundle_version":"1.5.0","mode":"redeploy","steps":["60"]}'
if TARGET_JSON="$tmp/target-orgelbau.json" run_updater "$job_c"; then fail "must fail on a target mismatch"; fi
grep -q '"code":"TARGET_MISMATCH"' "$job_c/status.json" || fail "expected TARGET_MISMATCH: $(cat "$job_c/status.json")"
no_steps_ran "$job_c" || fail "no step may run on a mismatch"

# d) festgelegtes Manifest, das mit target.json uebereinstimmt, laeuft
printf '{"user":"energynode","base":"/home/energynode"}\n' > "$tmp/target-default.json"
job_d="$tmp/job-d"; stage_job "$job_d" '{"bundle_version":"1.5.0","mode":"redeploy","steps":["60"]}'
TARGET_JSON="$tmp/target-default.json" run_updater "$job_d" \
  || fail "pinned bundle that agrees with target.json must run: $(cat "$job_d/log" 2>/dev/null)"

# e) ungueltige oder eingeschleuste Werte in target.json: TARGET_INVALID
for doc in '{"user":"x y","base":"/home/x"}' \
           '{"user":"a","base":"/home/a/../../etc"}' \
           '{"user":"a","base":"relative"}' \
           '{"user":"a","base":"/"}' \
           '{"user":"orgelbau\n","base":"/home/orgelbau"}' \
           '{"user":"orgelbau","base":"/home/orgelbau\n"}' \
           'not json'; do
  printf '%s\n' "$doc" > "$tmp/target-bad.json"
  job_e="$tmp/job-e"; rm -rf "$job_e"; stage_generic "$job_e" "$job_steps"
  if TARGET_JSON="$tmp/target-bad.json" run_updater "$job_e"; then fail "invalid target accepted: $doc"; fi
  grep -q '"code":"TARGET_INVALID"' "$job_e/status.json" || fail "expected TARGET_INVALID for $doc: $(cat "$job_e/status.json")"
  no_steps_ran "$job_e" || fail "no step may run for an invalid target: $doc"
done

# f) ein festgelegtes Manifest mit ungueltigem Ziel ohne target.json: TARGET_INVALID
pinned_bad="$tmp/fixture-pinned-bad"; cp -a "$fixture" "$pinned_bad"
cat > "$pinned_bad/manifest.json" <<'JSON'
{"version":"1.5.0","target_user":"x y","target_base":"/home/x",
 "steps":[{"id":"60","optional":false}]}
JSON
job_f="$tmp/job-f"; mkdir -p "$job_f/bundle"; cp -a "$pinned_bad/." "$job_f/bundle/"
printf '%s\n' '{"bundle_version":"1.5.0","mode":"redeploy","steps":["60"]}' > "$job_f/pending.json"
if run_updater "$job_f"; then fail "invalid pinned target accepted"; fi
grep -q '"code":"TARGET_INVALID"' "$job_f/status.json" || fail "expected TARGET_INVALID: $(cat "$job_f/status.json")"

echo "OK"

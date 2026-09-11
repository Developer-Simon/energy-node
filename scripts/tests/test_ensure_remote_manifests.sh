#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
# shellcheck source=/dev/null
source "${REPO_ROOT}/scripts/deploy/ensure_remote_manifests.sh"

fail() { echo "FAIL: $*" >&2; exit 1; }

# 1. Der echte services/-Baum wird vollstaendig und unter service_id-Namen gestaged.
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
stage_manifests "${REPO_ROOT}/services" "${work}" || fail "stage_manifests exit $?"

for expected in apsystems automation battery_soc shelly trucki tuya; do
  [[ -f "${work}/${expected}.json" ]] || fail "erwartete Datei fehlt: ${expected}.json"
done
count="$(find "${work}" -maxdepth 1 -name '*.json' | wc -l)"
[[ "${count}" -eq 6 ]] || fail "erwartet 6 Manifeste, gestaged ${count}"

got_id="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["service_id"])' "${work}/shelly.json")"
[[ "${got_id}" == "shelly" ]] || fail "shelly.json traegt service_id '${got_id}'"

# 2. Doppelte service_id -> Exit != 0.
dup="$(mktemp -d)"
mkdir -p "${dup}/a" "${dup}/b"
printf '{"service_id":"x"}' > "${dup}/a/manifest.json"
printf '{"service_id":"x"}' > "${dup}/b/manifest.json"
if stage_manifests "${dup}" "$(mktemp -d)" 2>/dev/null; then
  rm -rf "${dup}"; fail "doppelte service_id haette abgelehnt werden muessen"
fi
rm -rf "${dup}"

# 3. Fehlende service_id -> Exit != 0.
bad="$(mktemp -d)"
mkdir -p "${bad}/a"
printf '{"unit":"a.service"}' > "${bad}/a/manifest.json"
if stage_manifests "${bad}" "$(mktemp -d)" 2>/dev/null; then
  rm -rf "${bad}"; fail "fehlende service_id haette abgelehnt werden muessen"
fi
rm -rf "${bad}"

# 4. Leeres services/-Verzeichnis -> Exit != 0.
if stage_manifests "$(mktemp -d)" "$(mktemp -d)" 2>/dev/null; then
  fail "leeres services/-Verzeichnis haette abgelehnt werden muessen"
fi

# 5. ensure_remote_manifests im DRY_RUN ruft weder ssh noch rsync auf.
stubbin="$(mktemp -d)"
cat > "${stubbin}/ssh"   <<'EOF'
#!/bin/sh
echo "STUB-SSH $*" >> "${STUB_LOG}"
EOF
cat > "${stubbin}/rsync" <<'EOF'
#!/bin/sh
echo "STUB-RSYNC $*" >> "${STUB_LOG}"
EOF
chmod +x "${stubbin}/ssh" "${stubbin}/rsync"

STUB_LOG="$(mktemp)"
export STUB_LOG
DRY_RUN=true
TARGET_USER=someuser
RSYNC_OPTS=(--archive --dry-run)
RSYNC_SSH="ssh"
PATH="${stubbin}:${PATH}" ensure_remote_manifests "someuser@example" -o BatchMode=yes \
  || fail "ensure_remote_manifests DRY_RUN exit $?"
[[ ! -s "${STUB_LOG}" ]] || fail "DRY_RUN hat Zielzugriffe gemacht: $(cat "${STUB_LOG}")"
rm -rf "${stubbin}" "${STUB_LOG}"
unset DRY_RUN TARGET_USER RSYNC_OPTS RSYNC_SSH STUB_LOG

echo "PASS: test_ensure_remote_manifests.sh"

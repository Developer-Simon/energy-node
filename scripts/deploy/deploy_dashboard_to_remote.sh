#!/usr/bin/env bash
#
# Cross-compiles the Energy Node Dashboard for Raspberry Pi 1
# (ARMv6) and deploys binary + systemd unit to the remote node via rsync.
# Analogous in spirit to deploy_src_to_remote.sh, but ships a single
# statically linked Go binary instead of Python source, since the dashboard
# does not need a venv on the target.
#
# Usage:
#   ./deploy_dashboard_to_remote.sh [options]
#
# Options (see deploy_lib.sh for details):
#   --host HOST          Zielhost (Standard: aus secrets/deploy-target.env)
#   --user USER          Zielbenutzer (Standard: aus secrets/deploy-target.env)
#   --base PATH          Basisverzeichnis (Standard: /home/<Zielbenutzer>)
#   --dry-run, -n        Nur anzeigen, nichts uebertragen
#   --skip-restart       Dienste nicht neu starten
#   --force-config       Vorhandene /etc/energy-node/config.json ueberschreiben

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/deploy_lib.sh"
source "${SCRIPT_DIR}/ensure_remote_secrets.sh"
source "${SCRIPT_DIR}/ensure_remote_dashboard_auth.sh"
source "${SCRIPT_DIR}/ensure_remote_config.sh"

parse_deploy_args "$@"

REMOTE_DIR="${TARGET_BASE}/dashboard"
REMOTE_HOST="${SSH_TARGET}"

BINARY_NAME="energy-node-dashboard"
BUILD_DIR="$(mktemp -d)"
trap 'rm -rf "${BUILD_DIR}"' EXIT

cd "${REPO_ROOT}/dashboard"

# dashboard/VERSION holds the release triple (major.minor bumped by hand,
# patch bumped on the PR branch by the `Version bump` workflow). Builds off a branch
# other than main get a semver prerelease suffix identifying the branch and
# how many commits it has, so two builds from the same VERSION content stay
# distinguishable.
DASHBOARD_VERSION="$(tr -d '[:space:]' < VERSION)"
GIT_BRANCH="$(git -C "${REPO_ROOT}" rev-parse --abbrev-ref HEAD)"
if [[ "${GIT_BRANCH}" != "main" ]]; then
    COMMIT_COUNT="$(git -C "${REPO_ROOT}" rev-list --count HEAD)"
    BRANCH_SLUG="${GIT_BRANCH//\//-}"
    DASHBOARD_VERSION="${DASHBOARD_VERSION}-${BRANCH_SLUG}.${COMMIT_COUNT}"
fi

echo "==> Cross-compiling ${BINARY_NAME} ${DASHBOARD_VERSION} (GOOS=linux GOARCH=arm GOARM=6, CGO disabled)"
GOOS=linux GOARCH=arm GOARM=6 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-X main.buildVersion=${DASHBOARD_VERSION}" -o "${BUILD_DIR}/${BINARY_NAME}" ./cmd/dashboard

echo "==> Ensuring remote directory exists: ${REMOTE_HOST}:${REMOTE_DIR}"
ssh "${SSH_OPTS[@]}" "${REMOTE_HOST}" "mkdir -p '${REMOTE_DIR}'"

echo "==> Preflight: keine Zugangsdaten in versionierten Dateien"
"${SCRIPT_DIR}/check_tracked_secrets.sh"

echo "==> Preflight: zentrale Zugangsdaten auf dem Zielgeraet"
ensure_remote_secrets "${REMOTE_HOST}" "${SSH_OPTS[@]}"

echo "==> Preflight: Dashboard-Admin-Passwort auf dem Zielgeraet"
ensure_remote_dashboard_auth "${REMOTE_HOST}" "${SSH_OPTS[@]}"

echo "==> Preflight: zentrale Konfiguration auf dem Zielgeraet"
ensure_remote_config "${REMOTE_HOST}" "${FORCE_CONFIG}" "${SSH_OPTS[@]}"

SERVICE_INSTALLED="$(ssh "${SSH_OPTS[@]}" "${REMOTE_HOST}" \
    "if sudo test -f /etc/systemd/system/${BINARY_NAME}.service; then printf 1; else printf 0; fi")"

# Unit/System-Action-Skript/Sudoers-Regel enthalten im Repo den
# Platzhalter-Benutzer "energynode" (User=/Group=/Pfade bzw. die
# sudoers-Regel selbst) - vor dem Kopieren wird er durch den
# tatsaechlichen Zielbenutzer/-pfad ersetzt, siehe render_service_unit in
# deploy_lib.sh.
render_service_unit "${REPO_ROOT}/dashboard/${BINARY_NAME}.service" "${BUILD_DIR}/${BINARY_NAME}.service"
render_service_unit "${REPO_ROOT}/dashboard/${BINARY_NAME}-system-action" "${BUILD_DIR}/${BINARY_NAME}-system-action"
render_service_unit "${REPO_ROOT}/dashboard/${BINARY_NAME}-system-action.sudoers" "${BUILD_DIR}/${BINARY_NAME}-system-action.sudoers"

RSYNC_FILES=(
    "${BUILD_DIR}/${BINARY_NAME}"
    "${BUILD_DIR}/${BINARY_NAME}.service"
    "${BUILD_DIR}/${BINARY_NAME}-system-action"
    "${BUILD_DIR}/${BINARY_NAME}-system-action.sudoers"
)
if [[ "${SERVICE_INSTALLED}" == "0" ]]; then
    echo "==> Service is not installed; syncing binary, systemd unit, auth helper and sudoers rule"
else
    echo "==> Service is already installed; syncing binary, systemd unit, auth helper and sudoers rule"
fi

if [[ "$DRY_RUN" == true ]]; then
  echo "==> dry-run: es wird nichts uebertragen und nichts neu gestartet."
  exit 0
fi

rsync -avz --checksum -e "${RSYNC_SSH}" \
    "${RSYNC_FILES[@]}" \
    "${REMOTE_HOST}:${REMOTE_DIR}/"

if [[ "${SKIP_RESTART}" == "1" ]]; then
    echo "==> DASHBOARD_SKIP_RESTART=1, skipping remote install/restart."
    echo "==> Done (binary + runtime files synced only)."
    exit 0
fi

ssh "${SSH_OPTS[@]}" "${REMOTE_HOST}" "
    set -e
    sudo install -o root -g root -m 0644 '${REMOTE_DIR}/${BINARY_NAME}.service' /etc/systemd/system/${BINARY_NAME}.service
    if [[ '${SERVICE_INSTALLED}' == '0' ]]; then
        sudo systemctl daemon-reload
        sudo systemctl enable ${BINARY_NAME}.service
    else
        sudo systemctl daemon-reload
    fi
    sudo install -o root -g root -m 0755 '${REMOTE_DIR}/${BINARY_NAME}-system-action' /usr/local/sbin/${BINARY_NAME}-system-action
    sudo install -o root -g root -m 0440 '${REMOTE_DIR}/${BINARY_NAME}-system-action.sudoers' /etc/sudoers.d/${BINARY_NAME}-system-action
    sudo visudo -cf /etc/sudoers.d/${BINARY_NAME}-system-action
    rm -f '${REMOTE_DIR}/${BINARY_NAME}.service' '${REMOTE_DIR}/${BINARY_NAME}-system-action' '${REMOTE_DIR}/${BINARY_NAME}-system-action.sudoers'
    echo 'Restarting service'
    sudo systemctl restart ${BINARY_NAME}.service
    sudo systemctl status ${BINARY_NAME}.service --no-pager
"

echo "==> Done."

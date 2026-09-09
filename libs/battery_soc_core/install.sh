#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PYTHON="python3"
USE_USER=false
USE_EDITABLE=false
BREAK_SYSTEM_PACKAGES=false
VENV_DIR=""

usage() {
  cat <<EOF
Usage: $0 [--help] [--python PYTHON] [--venv PATH] [--user] [--editable] [--break-system-packages]

Install the battery-soc-core package from this directory.

Options:
  --help|-h                 Show this help message.
  --python PYTHON           Python interpreter to use (default: python3).
  --venv PATH               Create/activate a virtual environment at PATH.
  --user                    Install into the current user's Python site-packages.
  --editable|-e             Install the package in editable mode.
  --break-system-packages   Allow installation into a system-managed Python environment.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    --python)
      PYTHON="$2"
      shift 2
      ;;
    --venv)
      VENV_DIR="$2"
      shift 2
      ;;
    --user)
      USE_USER=true
      shift
      ;;
    --editable|-e)
      USE_EDITABLE=true
      shift
      ;;
    --break-system-packages)
      BREAK_SYSTEM_PACKAGES=true
      shift
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ -n "$VENV_DIR" ]]; then
  if [[ ! -d "$VENV_DIR" ]]; then
    "$PYTHON" -m venv "$VENV_DIR"
  fi
  PYTHON="$VENV_DIR/bin/python"
fi

cd "$SCRIPT_DIR"
# Remove stale egg-info left behind by earlier (possibly root-owned) builds.
# setuptools cannot update timestamps in a non-writable egg-info directory,
# which causes "Cannot update time stamp of directory" failures.
if [[ -d src ]] && find src -maxdepth 1 -type d -name '*.egg-info' | grep -q .; then
  echo "Removing stale egg-info directories..."
  if ! find src -maxdepth 1 -type d -name '*.egg-info' -exec rm -rf {} + 2>/dev/null; then
    echo "Stale egg-info not writable, using sudo to clean up"
    sudo find src -maxdepth 1 -type d -name '*.egg-info' -exec rm -rf {} +
  fi
fi

# Version kommt dynamisch aus VERSION (siehe pyproject.toml), gepflegt vom
# `Version bump`-Workflow wie dashboard/VERSION und services/VERSION. Ein manuelles
# Bumpen hier entfaellt damit; bei unveraendertem VERSION-Stand kann pip ein
# lokal editiertes Paket als bereits aktuell ansehen - im Zweifel
# --force-reinstall an pip uebergeben oder --editable verwenden.
PIP_CMD=("$PYTHON" -m pip install --upgrade)
if [[ "$USE_USER" == true ]]; then
  PIP_CMD+=(--user)
fi
if [[ "$BREAK_SYSTEM_PACKAGES" == true ]]; then
  PIP_CMD+=(--break-system-packages)
fi
if [[ "$USE_EDITABLE" == true ]]; then
  PIP_CMD+=(-e .)
else
  PIP_CMD+=(.)
fi

printf 'Installing battery-soc-core from %s\n' "$SCRIPT_DIR"
"${PIP_CMD[@]}"
printf 'Installation complete.\n'

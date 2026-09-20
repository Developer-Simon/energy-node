#!/usr/bin/env bash
#
# Builds (if needed) and starts the installer's graphical web UI.
#
# Usage:
#   scripts/dev/run-installer.sh [installer-args...]
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

if [ ! -x installer/installer ]; then
  ( cd installer && go build -o installer ./cmd/installer )
fi

./installer/installer "$@"

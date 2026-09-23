#!/usr/bin/env bash
#
# Builds the installer and starts it: the graphical web UI, or the developer
# CLI when the first passed argument is a subcommand.
#
# Usage:
#   scripts/dev/run-installer.sh [--no-build] [--fakehost] [args...]
#   scripts/dev/run-installer.sh [--no-build] <deploy|ensure-secrets|fetch-config|diagnose|help> [flags...]
#
#   --no-build   skip the build and start the existing binary (it is still
#                built if it does not exist yet)
#   --fakehost   build and start installer/webui/cmd/fakehost (the UI against
#                a canned backend, no Pi needed) instead of the installer
#
# All other arguments are passed on to the program that is started.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

build=1
fakehost=0
args=()
for arg in "$@"; do
  case "$arg" in
    --no-build) build=0 ;;
    --fakehost) fakehost=1 ;;
    *) args+=("$arg") ;;
  esac
done

if [ "$fakehost" = 1 ]; then
  module=installer/webui
  package=./cmd/fakehost
  binary=fakehost
else
  module=installer
  package=./cmd/installer
  binary=installer
fi

if [ "$build" = 1 ] || [ ! -x "installer/$binary" ]; then
  # installer/go.mod pins a newer Go than distro packages ship, and those
  # default to GOTOOLCHAIN=local; "auto" lets go pick the toolchain go.mod asks for.
  ( cd "$module" && GOTOOLCHAIN="${GOTOOLCHAIN:-auto}" go build -o "$OLDPWD/installer/$binary" "$package" )
fi

"./installer/$binary" ${args[@]+"${args[@]}"}

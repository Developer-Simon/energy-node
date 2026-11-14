#!/usr/bin/env bash
#
# Opens an SSH session on the Pi with the credentials from secrets/.
#
# Usage:
#   scripts/dev/ssh-pi.sh [ssh args or remote command...]
#
# Host and user come from secrets/deploy-target.env (TARGET_HOST, TARGET_USER),
# the password from secrets/system-ssh.pw (needs sshpass). An SSH key is used
# when SSH_IDENTITY points at one.
#
# PI_PROFILE=<name> reads secrets/<name>/ instead (e.g. PI_PROFILE=testpi for
# the local test Pi), with the same two file names.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
secrets="$root/secrets"
[[ -n "${PI_PROFILE:-}" ]] && secrets="$secrets/$PI_PROFILE"

# shellcheck source=/dev/null
source "$secrets/deploy-target.env"
: "${TARGET_USER:?missing in deploy-target.env}"
: "${TARGET_HOST:?missing in deploy-target.env}"

opts=(-o StrictHostKeyChecking=accept-new)

if [[ -n "${SSH_IDENTITY:-}" ]]; then
  exec ssh "${opts[@]}" -i "$SSH_IDENTITY" "$TARGET_USER@$TARGET_HOST" "$@"
fi

pw_file="$secrets/system-ssh.pw"
[[ -r "$pw_file" ]] || { echo "ssh-pi: cannot read $pw_file" >&2; exit 1; }
command -v sshpass >/dev/null || { echo "ssh-pi: sshpass is not installed" >&2; exit 1; }

exec sshpass -f "$pw_file" ssh "${opts[@]}" -o PreferredAuthentications=password,keyboard-interactive "$TARGET_USER@$TARGET_HOST" "$@"

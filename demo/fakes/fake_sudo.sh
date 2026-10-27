#!/bin/sh
# sudo shim for the demo (service logs): drops -n and runs the command as the
# current user, so the dashboard's `sudo -n <helper> ...` reaches the fake helper.
[ "${1:-}" = "-n" ] && shift
exec "$@"

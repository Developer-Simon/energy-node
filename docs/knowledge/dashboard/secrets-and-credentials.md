---
title: "Credentials and Secrets"
---

# Credentials and Secrets

Last updated: 2026-08-21

## Ground rule

No plaintext secret in a Git-tracked file. The guard
`scripts/deploy/check_tracked_secrets.sh` enforces this and is called by both
deploy scripts before anything is transferred.

## Central MQTT credentials

All eight services — the Go dashboard and the seven Python bridges — use the
same broker with the same credentials. The MQTT password lives on the target
device in a separate file:

    /etc/energy-node/mqtt.pw

The username is not a secret and is stored in the central configuration file
`/etc/energy-node/config.json` under `mqtt.username`.

**Contents of `/etc/energy-node/mqtt.pw`:**
The password itself only, with no trailing whitespace.

**Owner and permissions:** `root:energynode` with mode `0640`

**Why ownership changed:** Previously, systemd read the `*.env` files as root
and passed the values to a process started as `energynode`. With the new
central configuration file, each service reads its configuration itself on
every start. This means the process must be able to open the password files
itself, so the `energynode` group needs read access.

### First-time setup on the Pi

Both deploy scripts (`scripts/deploy/deploy_dashboard_to_remote.sh`, `scripts/deploy/deploy_src_to_remote.sh`)
check before transferring whether `/etc/energy-node/mqtt.pw` and
`/etc/energy-node/config.json` exist on the target device, via the
shared helpers `scripts/deploy/ensure_remote_secrets.sh` and `scripts/deploy/ensure_remote_config.sh`.

**For the MQTT password:**

If `/etc/energy-node/mqtt.pw` is missing:

1. If the local staging copy `secrets/mqtt.pw` is also missing (never
   versioned, thanks to the `.gitignore` entry `/secrets/`), the script
   prompts interactively for the password and creates the file locally.
2. The local copy is transferred via `scp` and installed on the target device
   in the right place with `sudo install -o root -g energynode -m 0640`.

On a repeat deploy to a fresh target device (or a second Pi), the existing
local copy `secrets/mqtt.pw` is reused without prompting again.

You can still do it manually:

    sudo mkdir -p /etc/energy-node
    sudo install -o root -g energynode -m 0640 /dev/null /etc/energy-node/mqtt.pw
    echo -n "my_mqtt_password" | sudo tee /etc/energy-node/mqtt.pw
    sudo systemctl daemon-reload
    sudo systemctl restart energy-node-dashboard.service

Check the permissions — the file must not be world-readable:

    sudo stat -c '%a %U:%G %n' /etc/energy-node/mqtt.pw
    # expected: 640 root:energynode /etc/energy-node/mqtt.pw

### Rotating the password

1. Set the new password in the broker:
   `sudo mosquitto_passwd /etc/mosquitto/passwd <user>`
2. Update the password in `/etc/energy-node/mqtt.pw`:
   `echo -n "new_password" | sudo tee /etc/energy-node/mqtt.pw`
3. Restart the broker and all services:
   `sudo systemctl restart mosquitto && sudo systemctl restart 'energy-node-*' apsystems-ez1 automation battery-soc shelly-rpc trucki-http tuya energy-node`
4. In the dashboard under Settings → MQTT, check the status: all services
   must report "connected" again.

Exactly one place has to change — that was the reason for the central
configuration file and the separate password file.

## Dashboard admin password

The admin password for dashboard access lives on the target device in a
separate file:

    /etc/energy-node-dashboard/auth.pw

**Contents:** The password itself only, with no trailing whitespace.

**Owner and permissions:** `root:energynode` with mode `0640`

The username is not a secret and is configurable via the configuration file
under `dashboard.admin_username`.

**First-time setup on the Pi:**

`scripts/deploy/deploy_dashboard_to_remote.sh` checks before transferring
whether `/etc/energy-node-dashboard/auth.pw` exists on the target device, via
the helper `scripts/deploy/ensure_remote_dashboard_auth.sh` (analogous to
`scripts/deploy/ensure_remote_secrets.sh` for the MQTT password):

1. If the local staging copy `secrets/dashboard-admin.pw` is also missing
   (never versioned, thanks to the `.gitignore` entry `/secrets/`), the script
   prompts interactively for the password and creates the file locally.
2. The local copy is transferred via `scp` and installed on the target device
   in the right place with `sudo install -o root -g energynode -m 0640`.

If the file already exists on the target device, it is left untouched — a
redeploy must not discard a password that was changed via the dashboard.

You can still do it manually:

    sudo mkdir -p /etc/energy-node-dashboard
    sudo install -o root -g energynode -m 0640 /dev/null /etc/energy-node-dashboard/auth.pw
    echo -n "my_admin_password" | sudo tee /etc/energy-node-dashboard/auth.pw

## Mosquitto bridge

`src/mosquitto-bridge.conf` is a **template** with placeholders in angle
brackets. The live version on the target device is created either through the
dashboard's bridge dialog (Settings → MQTT → Bridge, which writes to a
credential store that cannot be read back through the API) or by hand. Never
copy the live version back into the repo.

The credentials that the main system (Home Assistant) issues for this bridge
are kept locally only as a store under `secrets/hauptsystem-mqtt.env` —
deliberately without automation, for the same reason: the live version is not
written by a deploy script.

## Target user and host of the deploy scripts

The target user and host of your own target device are no longer hardcoded in
`scripts/deploy/deploy_lib.sh`; instead they live locally under
`secrets/deploy-target.env` (never versioned, thanks to the `.gitignore` entry
`/secrets/`):

    TARGET_USER=energynode
    TARGET_HOST=energy-node

`deploy_lib.sh` reads this file at the start of every deploy script, unless
`TARGET_USER`/`TARGET_HOST` are already set via environment variable. If the
file is missing and no environment variables are set either, the scripts abort
with an error message instead of continuing with a built-in default. They can
still be overridden per run via `--user`/`--host`.

## SSH access to the target device

The system login password for the user `energynode` is kept locally under
`secrets/system-ssh.pw` (never versioned, thanks to the `.gitignore` entry
`/secrets/`).

`scripts/deploy/deploy_lib.sh` reads this file at the start of every deploy
script: if it is present and `sshpass` is installed, all `ssh`, `scp`, and
`rsync` calls of the deploy scripts authenticate with it automatically,
instead of prompting for the password interactively on every run. If `sshpass`
is missing, a notice appears and the scripts prompt interactively as before.
If an SSH key is set up instead, the password goes unnoticed — `ssh`/`scp`
then behave as they did before this change.

Installing `sshpass` (Fedora): `sudo dnf install sshpass`

## Known legacy issues

The MQTT password still appears in roughly eleven older commits as part of
`mqtt.env` files. The repo has **no Git remote** and was never pushed; a
deliberate decision was therefore made against a history rewrite and against
an immediate rotation.

**Before the release as an open-source project**, a fresh fork without
inherited history is created, with a changelog of the changes following
Conventional Commits. That also settles this legacy issue.

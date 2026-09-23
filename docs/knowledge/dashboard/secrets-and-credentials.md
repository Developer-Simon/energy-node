---
title: "Credentials and Secrets"
---

# Credentials and Secrets

Last updated: 2026-09-23

## Ground rule

No plaintext secret in a Git-tracked file. The guard
`scripts/dev/check_tracked_secrets.sh` enforces this and runs in CI on every
pull request.

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

Bootstrap step 60 (`scripts/bootstrap/60-node-install.sh`) creates
`/etc/energy-node/config.json` from the bundle's template and places
`/etc/energy-node/mqtt.pw` from a password file the installer hands it. Both
are only created when missing; an existing file stays untouched.

**For the MQTT password:**

The graphical installer asks for the password on its configuration screen.
From a checkout, `installer ensure-secrets` (see
[Installer developer CLI](../installer-developer-cli.md)) does the same:

1. If the local staging copy `secrets/mqtt.pw` is missing (never versioned,
   thanks to the `.gitignore` entry `/secrets/`), it prompts interactively for
   the password and creates the file locally.
2. It reruns step 60 with that file, which installs it on the target device
   as `root:energynode` with mode `0640`.

On a repeat run against a fresh target device (or a second Pi), the existing
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

Step 60 places `/etc/energy-node-dashboard/auth.pw` the same way as the MQTT
password, and `installer ensure-secrets` covers both:

1. If the local staging copy `secrets/dashboard-admin.pw` is missing (never
   versioned, thanks to the `.gitignore` entry `/secrets/`), it prompts
   interactively for the password and creates the file locally.
2. Step 60 installs it on the target device as `root:energynode` with mode
   `0640`.

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
written by a deploy.

## Target user and host for the developer CLI

The target user and host of your own target device are not in the repository;
they live locally under `secrets/deploy-target.env` (never versioned, thanks
to the `.gitignore` entry `/secrets/`):

    TARGET_USER=energynode
    TARGET_HOST=energy-node

The installer's developer CLI reads this file for every subcommand.
`--host`/`--user`/`--base` override it per run, and `--target-env` points at a
different file.

## SSH access to the target device

The CLI tries an SSH key first (`~/.ssh/id_ed25519`, or `--identity`). Without
one it uses the system login password kept locally under
`secrets/system-ssh.pw` (never versioned, thanks to the `.gitignore` entry
`/secrets/`; `--password-file` to override). No `sshpass` is needed; the CLI
speaks SSH itself.

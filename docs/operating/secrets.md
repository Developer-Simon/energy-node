---
title: "Credentials and secrets"
redirect_from:
  - /knowledge/dashboard/secrets-and-credentials.html
---

# Credentials and secrets

Last updated: 2026-09-23

## Ground rule

No file tracked by Git may contain a plaintext secret.
`scripts/dev/check_tracked_secrets.sh` checks this in CI on every pull request.

## Central MQTT credentials

All eight services, the Go dashboard and the seven Python bridges, use the same
broker with the same credentials. The MQTT password is kept in a separate file
on the target device:

    /etc/energy-node/mqtt.pw

The username is not a secret and is stored in the central configuration file
`/etc/energy-node/config.json` under `mqtt.username`.

The file contains only the password, without trailing whitespace. It belongs to
`root:energynode` with mode `0640`. Each service reads its configuration and
password files itself on every start, so the `energynode` group needs read
access.

### First-time setup on the Pi

Bootstrap step 60 (`scripts/bootstrap/60-node-install.sh`) creates
`/etc/energy-node/config.json` from the bundle's template and places
`/etc/energy-node/mqtt.pw` from a password file the installer hands it. Both
are only created when missing. An existing file stays as it is.

The graphical installer asks for the password on its configuration screen.
From a checkout, `installer ensure-secrets` (see
[Installer developer CLI](../developing/installer-cli.md)) does the same:

1. If the local copy `secrets/mqtt.pw` is missing, it asks for the password
   and creates the file. The `.gitignore` entry `/secrets/` keeps it out of
   Git.
2. It reruns step 60 with that file, which installs it on the target device
   as `root:energynode` with mode `0640`.

When you run it again against a fresh target device or a second Pi, it reuses
the local copy `secrets/mqtt.pw` without asking again.

To do it by hand:

    sudo mkdir -p /etc/energy-node
    sudo install -o root -g energynode -m 0640 /dev/null /etc/energy-node/mqtt.pw
    echo -n "my_mqtt_password" | sudo tee /etc/energy-node/mqtt.pw
    sudo systemctl daemon-reload
    sudo systemctl restart energy-node-dashboard.service

Check the permissions. The file must not be readable by everyone:

    sudo stat -c '%a %U:%G %n' /etc/energy-node/mqtt.pw
    # expected: 640 root:energynode /etc/energy-node/mqtt.pw

### Rotating the password

1. Set the new password in the broker:
   `sudo mosquitto_passwd /etc/mosquitto/passwd <user>`
2. Update the password in `/etc/energy-node/mqtt.pw`:
   `echo -n "new_password" | sudo tee /etc/energy-node/mqtt.pw`
3. Restart the broker and all services:
   `sudo systemctl restart mosquitto && sudo systemctl restart 'energy-node-*' apsystems-ez1 automation battery-soc shelly-rpc trucki-http tuya energy-node`
4. In the dashboard under Settings → MQTT, check that all services report
   "connected" again.

The password lives in one file only, so a rotation changes one place on the
node.

## Dashboard admin password

The dashboard admin password is kept in a separate file on the target device:

    /etc/energy-node-dashboard/auth.pw

The file contains only the password, without trailing whitespace. It belongs to
`root:energynode` with mode `0640`. The username is not a secret and is set in
the configuration file under `dashboard.admin_username`.

For the first setup on the Pi, step 60 places `/etc/energy-node-dashboard/auth.pw` the same way as the MQTT
password, and `installer ensure-secrets` covers both:

1. If the local copy `secrets/dashboard-admin.pw` is missing, it asks for the
   password and creates the file. The `.gitignore` entry `/secrets/` keeps it
   out of Git.
2. Step 60 installs it on the target device as `root:energynode` with mode
   `0640`.

If the file already exists on the target device, it stays as it is, because a
redeploy must not discard a password changed in the dashboard.

To do it by hand:

    sudo mkdir -p /etc/energy-node-dashboard
    sudo install -o root -g energynode -m 0640 /dev/null /etc/energy-node-dashboard/auth.pw
    echo -n "my_admin_password" | sudo tee /etc/energy-node-dashboard/auth.pw

## Mosquitto bridge

`src/mosquitto-bridge.conf` is a template with placeholders in angle brackets.
The live version on the target device is written by the dashboard's bridge
dialog (Settings → MQTT → Bridge) or by hand. The dialog stores the
credentials where the API cannot read them back. Never copy the live version
back into the repo.

The credentials that the main system (Home Assistant) issues for this bridge
are only kept locally in `secrets/hauptsystem-mqtt.env`. Nothing automated
uses that file, because a deploy does not write the live bridge configuration
either.

## Target user and host for the developer CLI

The user and host of your own target device are not in the repository. They
are kept locally in `secrets/deploy-target.env`, which the `.gitignore` entry
`/secrets/` keeps out of Git:

    TARGET_USER=energynode
    TARGET_HOST=energy-node

The installer's developer CLI reads this file for every subcommand.
`--host`, `--user` and `--base` override it for one run, and `--target-env`
points to a different file.

## SSH access to the target device

The CLI tries an SSH key first (`~/.ssh/id_ed25519`, or `--identity`). Without
a key it uses the system login password from `secrets/system-ssh.pw`, which
the `.gitignore` entry `/secrets/` keeps out of Git. `--password-file` points to
a different file. The CLI speaks SSH itself, so you do not need `sshpass`.

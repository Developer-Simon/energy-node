# Deploying and verifying

Continues [Manual installation](index.md) from section 6.

## 6. Deploy from the development machine

The software always reaches the node as an installation package (bundle). It
holds the dashboard binary, the Python services with their wheels, units and
manifests, and the bootstrap steps that install them. The
[desktop installer](../installer.md) ships a released version or a package
file you pick. For a build of your own checkout, use the installer's developer
CLI. It builds the bundle, uploads it over SSH and runs the same steps:

```sh
scripts/dev/run-installer.sh deploy --dev-unsigned --dry-run   # look first
scripts/dev/run-installer.sh ensure-secrets --dev-unsigned     # first run: MQTT and admin passwords
scripts/dev/run-installer.sh deploy --dev-unsigned
```

The CLI reads the target host and user from the gitignored
`secrets/deploy-target.env` (`TARGET_USER`, `TARGET_HOST`) and uses
`/home/<TARGET_USER>` as the base unless you pass `--host`, `--user` or
`--base`. [Installer developer CLI](../developing/installer-cli.md) covers SSH
authentication and every flag.

The first run does the one-time setup:

1. `/etc/energy-node/mqtt.pw`: `ensure-secrets` asks for the password, keeps
   a local copy in the gitignored `secrets/`, and step 60 installs it as
   `root:energynode`, mode `0640`.
2. `/etc/energy-node-dashboard/auth.pw`: the dashboard admin password, same
   treatment.
3. `/etc/energy-node/config.json`: installed from
   `services/energy-node.config.json` if missing.

A redeploy never overwrites existing files. The dashboard may edit
`config.json`, and a deploy must not throw those edits away. Run
`deploy --force-config` (it asks for confirmation) if you want the template
back.

A full deploy runs every step and restarts every service. Restrict a run with
`--only <target>`: `dashboard`, `wheels`, or a service id (`apsystems`,
`battery-soc`, `shelly`, `trucki`, `tuya`, `automation`). That step runs even
if it already ran for this version, and its unit restarts afterwards.
`restart [--only <target>]` restarts units without deploying anything.

On every run, step 60 also writes the complete set of service manifests to
`/etc/energy-node/manifests/<service_id>.json`. The dashboard and the Python
bridges read the active services from this directory, and the Python bridges
do not start without it. The set has to match the `services` block in
`config.json`, otherwise the bridges fail to start.

### Upgrading from an earlier release (≤ 0.4)

A deploy never overwrites a live `config.json` or a deployed device file, so
some changes need manual work. Do the following once on the node, around the
first start of the new dashboard:

1. **Retire the old Python node service.** Node telemetry is now part of the
   dashboard binary as `internal/nodeagent`, and the separate service no longer
   exists. If it keeps running, it becomes a second publisher on
   `outstation/energy_node/…`.

   ```sh
   sudo systemctl disable --now energy-node.service
   sudo rm -f /etc/systemd/system/energy-node.service
   sudo systemctl daemon-reload
   rm -rf ~/energy-node          # the old service's code dir under the target base
   ```

2. **Let the dashboard migrate `config.json`.** A `schema_version 1` file has
   a top-level `node` block, a hyphenated `node.device_id` and, on very old
   nodes, `services.*.device_id` instead of `service_id`. The new dashboard
   upgrades it to version 2 on its first start. The four remaining `node.*`
   fields move to `dashboard.node_*`, `node.managed_bridges` is dropped,
   `node_device_id` becomes `"energy_node"` and every `services.*.device_id`
   is renamed to `service_id`. The dashboard writes the result back through
   its privileged system action helper (`apply-app-config`) and keeps the old
   file as `/etc/energy-node/.config.json.bak`. This needs the helper and its
   sudoers entry, which bootstrap step 60 installs (`deploy --only dashboard`).
   Without them the dashboard still runs on the migrated config, but the file
   on disk stays at version 1. The Python services reject version 1, so start
   the dashboard first and restart the bridges afterwards
   (`systemctl restart apsystems-ez1 shelly-rpc trucki-http tuya battery-soc automation`).
   A `node_device_id` other than `energy-node` or `energy_node` is left as it
   is, and the dashboard then refuses to start. Set it to `"energy_node"` by
   hand in that case.

3. **Deliver service manifests.** A node set up before this change has no
   `/etc/energy-node/manifests/`. Run `deploy --only dashboard` to create it
   (step 60 installs the manifests), then restart the Python bridges with
   `restart` or `systemctl restart apsystems-ez1 shelly-rpc trucki-http
   tuya battery-soc automation`. Without deploy access, create the six files
   by hand from `services/<name>/manifest.json`, each as `{"service_id": "<id>",
   "unit": "<unit>", "schema": "config.schema.json", "required": [...]}`.

4. **Fix `via_device` in the operator-editable device files.** In the deployed
   `battery_soc_devices.json` and `trucki_devices.json` (under
   `paths.devices_dir`, usually `~/devices/`), change every `via_device` from
   `"energy-node"` to `"energy_node"` so Home Assistant keeps linking those
   entities to the node device.

## 7. Configure devices

Edit `/etc/energy-node/config.json` for the broker, paths, log level, the
settings of each service and the dashboard settings including the `node_*`
fields. [`../operating/configuration.md`](../operating/configuration.md)
documents every field. There is no fallback to environment variables. If the
file is missing, the services do not start.

Devices live in separate JSON files under `paths.devices_dir`
(`shelly_devices.json`, `apsystems_devices.json`, `trucki_devices.json`,
`tuya_devices.json`, `battery_soc_devices.json`, `automation_rules.json`),
each with a schema next to it. The dashboard's configuration editor is the
easiest way to fill them in. It validates against the schema, keeps revisions
and, for the battery service, suggests MQTT topics and JSON keys from payloads
it has seen.

Some devices need extra first steps.

For Tuya, run `python3 -m tinytuya wizard` once to get the device ID, local
key, IP and protocol version. It needs a Tuya IoT developer account linked to
the Smart Life app. The switch is not always data point `1`, so start the
service by hand once and read the raw DPS dump in the log before you set
`datapoints.switch`.

For the APsystems EZ1, enable local mode on the inverter and enter its IP.

For Shelly, leave MQTT disabled on the devices, because the node polls them
over HTTP. Restart the service after editing `shelly_devices.json`. That file
is not reloaded while the service runs.

## 8. Dashboard access

Plain HTTP on port 8080 works right away. Admin sessions are blocked over plain
HTTP, so for HTTPS and the admin login put Caddy in front.

Install Caddy on the node first. On ARMv6, take the `armv6` or `armhf` build
from the Caddy project, since your distribution may not ship one. Then install
the repository's `Caddyfile`:

```sh
scp dashboard/Caddyfile <user>@<node>:/tmp/Caddyfile
ssh <user>@<node> "
  sudo install -o root -g root -m 0644 /tmp/Caddyfile /etc/caddy/Caddyfile &&
  sudo caddy validate --config /etc/caddy/Caddyfile &&
  sudo systemctl reload caddy"
```

`tls internal` issues a certificate from Caddy's local CA. The CA is not
trusted system wide automatically, because the Caddy service user has no sudo
rights, so browsers warn until you trust it yourself. Deploying the dashboard
does not touch the Caddy configuration.

To serve the dashboard under a sub-path behind another reverse proxy, set
`X-Forwarded-Prefix` (or `X-Ingress-Path` for Home Assistant ingress). See
[`../operating/reverse-proxy.md`](../operating/reverse-proxy.md).

## 9. Verify

```sh
systemctl is-active mosquitto tailscaled energy-node-dashboard.service
systemctl is-active apsystems-ez1 shelly-rpc trucki-http tuya battery-soc \
                    automation
journalctl -u shelly-rpc -f
mosquitto_sub -h localhost -u <mqtt-user> -P <password> -t 'outstation/#' -v
```

Then check, in this order:

1. The node appears in Home Assistant as its own device, with CPU, RAM, disk,
   throttling and Tailscale sensors.
2. The devices of every bridge appear, linked to the node via `via_device`.
3. The dashboard shows live values, and the bridges acknowledge its central
   poll interval controls.

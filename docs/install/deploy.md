# Deploying and verifying

Continues [Manual installation](index.md) from section 6.

## 6. Deploy from the development machine

The software itself always reaches the node as an installation package
(bundle): the dashboard binary, the Python services, their wheels, units and
manifests, and the bootstrap steps that install them. Two tools ship it:

- The **[desktop installer](../installer.md)** for a released version, or a
  package file you pick.
- The installer's **developer CLI** for a build of your own checkout. It
  builds the bundle, uploads it over SSH and runs the same steps:

```sh
scripts/dev/run-installer.sh deploy --dev-unsigned --dry-run   # look first
scripts/dev/run-installer.sh ensure-secrets --dev-unsigned     # first run: MQTT and admin passwords
scripts/dev/run-installer.sh deploy --dev-unsigned
```

The CLI reads the target host/user from the gitignored
`secrets/deploy-target.env` (`TARGET_USER`, `TARGET_HOST`), defaults the base
to `/home/<TARGET_USER>`, and accepts `--host`, `--user` and `--base` to
override. [Installer developer CLI](../developing/installer-cli.md)
covers SSH authentication and every flag.

On the **first** run the steps do the one-time setup:

1. `/etc/energy-node/mqtt.pw`: `ensure-secrets` asks for the password, keeps
   a local copy in the gitignored `secrets/`, and step 60 installs it as
   `root:energynode`, mode `0640`.
2. `/etc/energy-node-dashboard/auth.pw`: the dashboard admin password, same
   treatment.
3. `/etc/energy-node/config.json`: installed from
   `services/energy-node.config.json` if missing.

Existing files are **never** overwritten by a redeploy. The dashboard is
allowed to edit `config.json`, and a deploy must not throw that away. Use
`deploy --force-config` (with confirmation) if you really want the template
back.

A full deploy runs every step and restarts every service. Restrict a run with
`--only <target>`: `dashboard`, `wheels`, or a service id (`apsystems`,
`battery-soc`, `shelly`, `trucki`, `tuya`, `automation`). That step runs even
if it already ran for this version, and its unit restarts afterwards.
`restart [--only <target>]` restarts units without deploying anything.

Step 60 also delivers the complete set of service manifests to
`/etc/energy-node/manifests/<service_id>.json` on every run. The dashboard and
Python bridges read the active service scope from this directory; the Python
bridges will not start without it. The set must match the `services` block in
`config.json`, or the bridge startup will fail.

### Upgrading from an earlier release (≤ 0.4)

Some things changed that a deploy does **not** fix for you, because it
never overwrites a live `config.json` or a deployed device file. Do these
once, on the node, around the first start of the new dashboard:

1. **Retire the old Python node service.** Node telemetry now lives in the
   dashboard binary as `internal/nodeagent`; the standalone service is gone.
   Leaving it running makes it a second publisher on `outstation/energy_node/…`.

   ```sh
   sudo systemctl disable --now energy-node.service
   sudo rm -f /etc/systemd/system/energy-node.service
   sudo systemctl daemon-reload
   rm -rf ~/energy-node          # the old service's code dir under the target base
   ```

2. **Let the dashboard migrate `config.json`.** A `schema_version 1` file
   (top-level `node` block, hyphenated `node.device_id`, and — on very old
   nodes — `services.*.device_id` instead of `service_id`) is upgraded to
   version 2 the first time the new dashboard starts: the four surviving
   `node.*` fields move to `dashboard.node_*`, `node.managed_bridges` is
   dropped, `node_device_id` is normalised to `"energy_node"`, and any
   `services.*.device_id` is renamed to `service_id`. The dashboard writes
   the result back through its privileged system-action helper
   (`apply-app-config`), keeping the old file as
   `/etc/energy-node/.config.json.bak`. That needs the helper and its
   sudoers entry that bootstrap step 60 installs (`deploy --only dashboard`);
   if they are missing the dashboard still runs on the migrated config but
   the file on disk stays version 1. The Python services still reject
   version 1, so start the dashboard first, then restart the bridges
   (`systemctl restart apsystems-ez1 shelly-rpc trucki-http tuya battery-soc automation`).
   A `node_device_id` that is neither `energy-node` nor `energy_node` is left
   untouched and the dashboard then refuses to start — set it to
   `"energy_node"` by hand in that case.

3. **Deliver service manifests.** A node set up before this change has no
   `/etc/energy-node/manifests/`. Run `deploy --only dashboard` (step 60
   installs them) to create it; then restart the Python bridges
   (`restart`, or `systemctl restart apsystems-ez1 shelly-rpc trucki-http
   tuya battery-soc automation`). Without
   deploy access, create the six files by hand — each as `{"service_id": "<id>",
   "unit": "<unit>", "schema": "config.schema.json", "required": [...]}` from
   `services/<name>/manifest.json`.

4. **Fix `via_device` in the operator-editable device files.** In the deployed
   `battery_soc_devices.json` and `trucki_devices.json` (under
   `paths.devices_dir`, i.e. `~/devices/`), change every `via_device` from
   `"energy-node"` to `"energy_node"` so Home Assistant keeps linking those
   entities to the node device.

---

## 7. Configure devices

Edit `/etc/energy-node/config.json` (broker, paths, log level,
per-service settings, dashboard settings incl. the `node_*` fields) — every
field is documented in [`../operating/configuration.md`](../operating/configuration.md).
There is no environment-variable fallback: if the file is missing, services
fail to start rather than come up unconfigured.

Devices live in separate JSON files under `paths.devices_dir`
(`shelly_devices.json`, `apsystems_devices.json`, `trucki_devices.json`,
`tuya_devices.json`, `battery_soc_devices.json`, `automation_rules.json`),
each with a schema next to it. The dashboard's configuration editor is the
comfortable way to fill these in — it validates against the schema, keeps
revisions, and for the battery service it suggests the MQTT topics and JSON
keys from payloads it has actually seen.

Device-specific first steps:

- **Tuya:** run `python3 -m tinytuya wizard` once (needs a Tuya IoT
  developer account linked to the Smart Life app) to obtain device ID, local
  key, IP and protocol version. The switch's data point number is not
  necessarily `1`; start the service manually once and read the raw DPS dump
  from the log before setting `datapoints.switch`.
- **APsystems EZ1:** enable local mode on the inverter, then enter its IP.
- **Shelly:** leave MQTT disabled on the devices; the node polls them over
  HTTP. Restart the service after editing `shelly_devices.json` — there is no
  hot reload for that file.

---

## 8. Dashboard access

Direct HTTP on port 8080 works out of the box. For HTTPS and admin login
(admin sessions are blocked over plain HTTP by design), put Caddy in front:

Install Caddy on the node first — on ARMv6 take the `armv6`/`armhf` build
from the Caddy project rather than assuming your distribution ships one —
then install the repository's `Caddyfile`:

```sh
scp dashboard/Caddyfile <user>@<node>:/tmp/Caddyfile
ssh <user>@<node> "
  sudo install -o root -g root -m 0644 /tmp/Caddyfile /etc/caddy/Caddyfile &&
  sudo caddy validate --config /etc/caddy/Caddyfile &&
  sudo systemctl reload caddy"
```

`tls internal` issues a certificate from Caddy's local CA. That CA is **not**
installed system-wide automatically (the Caddy service user has no sudo
rights), so browsers will warn until you trust it explicitly. Deploying the
dashboard does not touch the Caddy configuration.

To serve the dashboard under a sub-path behind another reverse proxy, set
`X-Forwarded-Prefix` (or `X-Ingress-Path` for Home Assistant ingress) — see
[`../operating/reverse-proxy.md`](../operating/reverse-proxy.md).

---

## 9. Verify

```sh
systemctl is-active mosquitto tailscaled energy-node-dashboard.service
systemctl is-active apsystems-ez1 shelly-rpc trucki-http tuya battery-soc \
                    automation
journalctl -u shelly-rpc -f
mosquitto_sub -h localhost -u <mqtt-user> -P <password> -t 'outstation/#' -v
```

Then check, in order:

1. The node itself appears in Home Assistant as its own device, with CPU,
   RAM, disk, throttling and Tailscale sensors.
2. Every bridge's devices appear, linked to the node via `via_device`.
3. The dashboard shows live values and its central poll-interval controls
   are acknowledged by the bridges.

---


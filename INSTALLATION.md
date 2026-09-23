# Installation

How to bring up an Energy Node on a Raspberry Pi and connect it to the main
site. Written for the hard case — a **Raspberry Pi 1 Model B (ARMv6)** — so
that everything here also works on a Pi 2/3/4/5 or Zero.

The single most annoying part of an ARMv6 install is that several pieces of
software must be installed in a **deliberately outdated version first and
updated afterwards**, because their current installers no longer support the
architecture. That is [section 3](#3-packages-you-must-install-outdated-first)
and it is worth reading before you start.

> **There is a guided installer.** The [desktop installer](docs/installer.md)
> runs sections 2–8 below from your own computer: package bootstrap, broker,
> firewall, Tailscale, the dashboard, HTTPS and the device services. This page
> is the manual route, and the reference for what the installer automates and
> why.

---

## 0. What ends up where

| Where | What |
|---|---|
| Development machine | Go toolchain, Node.js, Python venv, this repository, the installer's developer CLI |
| Node — `/home/energynode/` | Python services, `devices/*.json`, the dashboard binary and its data dir |
| Node — `/etc/energy-node/` | `config.json` (the single source of truth) and `mqtt.pw` |
| Node — `/etc/energy-node-dashboard/` | `auth.pw` (dashboard admin password) |
| Node — `/etc/systemd/system/` | one unit per service |
| Node — `/etc/mosquitto/conf.d/` | broker config and the bridge to the main site |

Nothing is built on the node. The development machine builds an installation
package: the Go dashboard cross-compiled, the Python services as source, their
dependencies as wheels.

---

## 1. Prerequisites

**Hardware**

- A Raspberry Pi (Pi 1 Model B upward) with a reliable power supply — an
  undervolting Pi 1 will silently throttle; the node's own diagnostics
  expose `vcgencmd get_throttled` for exactly this reason.
- SD card, network (Ethernet or WiFi), SSH reachable from the development
  machine.

**Main site**

- Home Assistant with an MQTT broker (e.g. the Mosquitto add-on) and the
  MQTT integration enabled.
- A Tailscale account (free tier is enough) with both machines in the same
  tailnet.

**Development machine**

- Go ≥ 1.22 (`dashboard/go.mod`), Node.js (for the dashboard's JS tests),
  Python 3.9+ with a project venv, `git` and `bash`. The installer module
  pins a newer Go than the dashboard; `scripts/dev/run-installer.sh` lets Go
  fetch that toolchain itself.

```sh
git clone <your-fork> energy-node && cd energy-node
python3 -m venv .venv && .venv/bin/pip install -e libs/energy_node_common -e libs/battery_soc_core pytest
```

---

## 2. Operating system

Use a **32-bit (armhf) Raspberry Pi OS Lite** image. The 64-bit images do not
run on ARMv6 at all; if the current 32-bit image refuses to boot on a Pi 1,
fall back to **Raspberry Pi OS (Legacy, 32-bit) Lite**. The reference node
runs Python 3.11.2, which satisfies every service in this repository
(`requires-python >= 3.9`).

Flash with the Raspberry Pi Imager and preconfigure hostname, user, SSH and
WiFi there — a headless remote site is not the place to discover that SSH is
off. The root partition resizes itself on first boot; leave it alone.

```sh
ssh <user>@<node>.local
sudo apt update && sudo apt full-upgrade -y
sudo raspi-config      # timezone
```

A full `apt upgrade` on a Pi 1 takes a long while. Let it finish before
continuing — half-upgraded systems make every later error harder to read.

---

## 3. Packages you must install outdated first

Two ecosystems have dropped or restricted ARMv6 support. In each case the
*binary* still runs; only the *installer* refuses. The pattern is always the
same: **install the last version that still supports the architecture, then
let the software update itself in place.**

| Software | Why the normal path fails | What to do instead |
|---|---|---|
| Tailscale | The official install script serves no ARMv6 build past 1.62.0 | Unpack the 1.62.0 tarball by hand, then `tailscale update` |
| Python packages | Recent Raspberry Pi OS marks the system Python as externally managed (PEP 668) | Install into the system interpreter with `--break-system-packages` |

### 3.1 Tailscale — install 1.62.0, then update

```sh
cd ~
wget https://dl.tailscale.com/stable/tailscale_1.62.0_arm.tgz
tar -xzf tailscale_1.62.0_arm.tgz
cd tailscale_1.62.0_arm

sudo cp tailscale tailscaled /usr/sbin/
sudo cp systemd/tailscaled.service /etc/systemd/system/
sudo cp systemd/tailscaled.defaults /etc/default/tailscaled
```

Leave `/etc/default/tailscaled` at its default (`FLAGS=""`). Do **not** set
`--tun=userspace-networking`: it stops `tailscaled` from creating the
`tailscale0` interface and installing kernel routes, so the node cannot
route IP traffic to other tailnet peers at all. The ARMv6 kernel does not
need it — `wireguard-go` runs in userspace on every architecture and only
needs `/dev/net/tun`, which Raspberry Pi OS already provides.

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now tailscaled
sudo tailscale up
```

Open the printed login URL in any browser and approve the machine. Then
**update in place** — this is the step that gets you off the ancient version:

```sh
sudo tailscale update
tailscale status
tailscale ip -4        # the node's 100.x.x.x address
```

**Disable key expiry** for this machine in the Tailscale admin console.
Otherwise the node drops off the tailnet after ~180 days and needs an
interactive login on site.

Reach the main site over its **Tailscale IP** (`100.x.x.x`), not over a
subnet route:

- Do not pass `--accept-routes` on the node and do not advertise routes
  from it. A stale `--advertise-routes` that only takes effect once IP
  forwarding is enabled once turned the node into a silent router for a
  whole `/24`: `--accept-routes` clients sent their LAN traffic for that
  subnet through the tunnel, so `ping`/`curl` to those hosts broke while
  established SSH sessions stayed up. Clear it with
  `sudo tailscale up --reset --advertise-routes=` and confirm in the admin
  console that the route is no longer advertised.
- On the Home Assistant side, run the Tailscale add-on with
  `userspace_networking: false` so the **HA host** gets a `100.x` address.
  The Mosquitto add-on's `1883:1883` port mapping binds every interface, so
  the node reaches the broker at `<HA-Tailscale-IP>:1883` — no
  `advertise_routes`, no admin-console route approval.
- Never point the bridge at a LAN IP that could also exist on the node
  site's own network: Tailscale prefers local network membership over an
  advertised route, so the bridge would connect to the wrong local host.
  The `100.x` address has no such ambiguity.

### 3.2 Python packages on an externally managed system

Recent Raspberry Pi OS releases mark the system Python as
externally managed, so `pip install` refuses to touch it. The services here
deliberately run against the **system interpreter** — every unit starts
`/usr/bin/python3`, there is no venv on the node — so the flag is required:

```sh
sudo python3 -m pip install --break-system-packages \
    "paho-mqtt>=2.0" apsystems-ez1 tinytuya requests
```

Install what you actually need: `paho-mqtt` is required by every service,
`apsystems-ez1` only for the EZ1 bridge, `tinytuya` only for Tuya devices,
`requests` for the Shelly and Trucki bridges.

Two things to know:

- On an **older Legacy image** the `--break-system-packages` flag does not
  exist yet. Drop it there — plain `sudo python3 -m pip install …` is
  correct on those releases.
- The installer's bootstrap step 50 does this for you: it installs every
  wheel from the bundle with `--no-index`, so the node never resolves
  packages from PyPI over a possibly flaky link. The build machine fetched
  them from piwheels in advance.

### 3.3 Go and Node.js — do not install them on the node

Compiling Go on a Pi 1 is not a good use of an afternoon, and it is not
needed. The dashboard is cross-compiled on the development machine into one
statically linked ARMv6 binary with CGO disabled:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build -o energy-node-dashboard ./cmd/dashboard
```

`scripts/build/make_bundle.sh` does exactly this when it builds the
installation package.
Node.js is only used for the dashboard's browser-side tests, on the
development machine.

---

## 4. Broker and firewall on the node

```sh
sudo apt install mosquitto mosquitto-clients -y
sudo systemctl enable --now mosquitto
sudo mosquitto_passwd -c /etc/mosquitto/passwd <mqtt-user>
```

`/etc/mosquitto/conf.d/default.conf`:

```
listener 1883
allow_anonymous false
password_file /etc/mosquitto/passwd
```

```sh
sudo systemctl restart mosquitto
mosquitto_sub -h localhost -u <mqtt-user> -P <password> -t 'test/#' -v   # in one shell
mosquitto_pub -h localhost -u <mqtt-user> -P <password> -t test/x -m ok  # in another
```

Firewall:

```sh
sudo ufw allow ssh
sudo ufw allow 1883/tcp    # MQTT
sudo ufw allow 8080/tcp    # dashboard (HTTP)
sudo ufw allow 443/tcp     # dashboard (HTTPS via Caddy, see section 8)
sudo ufw enable
```

Only if you use the Shelly wake webhook (opt-in, see `docs/device-services.md`), also open its port. The installer does this only when you tick **Shelly wake webhook in the firewall** on its configuration screen (step 35, off by default, only offered while the Shelly service is selected):

```sh
sudo ufw allow 8082/tcp    # Shelly wake webhook, default webhook_port
```

---

## 5. Bridge to the main site

The bridge from the node to the main site is configured through the dashboard's **MQTT** settings page. It opens an **outgoing** connection from the node to the main site's broker and mirrors `outstation/#` in both directions. Fill in the main site's **Tailscale IP** (`100.x`, section 3.1) and the broker credentials.

Verify from the main site that `outstation/#` messages arrive (MQTT Explorer, or `mosquitto_sub -t 'outstation/#' -v`). No extra TLS is needed: Tailscale already encrypts the link.

---

## 6. Deploy from the development machine

The software itself always reaches the node as an installation package
(bundle): the dashboard binary, the Python services, their wheels, units and
manifests, and the bootstrap steps that install them. Two tools ship it:

- The **[desktop installer](docs/installer.md)** for a released version, or a
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
override. [Installer developer CLI](docs/knowledge/installer-developer-cli.md)
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
field is documented in [`docs/knowledge/configuration.md`](docs/knowledge/configuration.md).
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
[`docs/knowledge/dashboard/reverse-proxy.md`](docs/knowledge/dashboard/reverse-proxy.md).

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

## 10. Troubleshooting

| Symptom | Likely cause |
|---|---|
| Node cannot ping or route to any other tailnet peer | `--tun=userspace-networking` is set in `/etc/default/tailscaled` — remove it (section 3.1) |
| A client's traffic to the main site's LAN breaks after IP forwarding is enabled, SSH stays up | Node still advertises a subnet route — `sudo tailscale up --reset --advertise-routes=` (section 3.1) |
| Node drops off the tailnet after months | Key expiry was not disabled in the admin console |
| `pip` refuses with "externally-managed-environment" | Missing `--break-system-packages` (section 3.2) |
| A service starts, then dies immediately | `/etc/energy-node/config.json` or `mqtt.pw` missing or unreadable for group `energynode` — fail-closed by design |
| Services run an old version of the shared package | The wheel was installed for a different interpreter than the unit's `/usr/bin/python3`; reinstall and `systemctl restart` the services |
| Devices show as unavailable in Home Assistant, no errors in the log | Check the bridge really connected to the broker; the bridges log paho-internal callback exceptions only because `enable_logger()` is on |
| Dashboard reachable but admin login rejected | Admin sessions require HTTPS — use the Caddy endpoint, not port 8080 |
| Sporadic freezes / throttling on a Pi 1 | Power supply. Watch the node's own undervoltage sensor |

More background on the data model and the individual services is under
[`docs/knowledge/`](docs/knowledge/).

---
title: "Manual installation"
anchor_moves:
  31-tailscale--install-1620-then-update: index.html#31-tailscale-install-1620-then-update
  33-go-and-nodejs--do-not-install-them-on-the-node: index.html#33-go-and-nodejs-do-not-install-them-on-the-node
---

# Manual installation

This page sets up an Energy Node on a Raspberry Pi and connects it to the main
site. It is written for the hardest case, a Raspberry Pi 1 Model B (ARMv6), so
everything here also works on a Pi 2, 3, 4, 5 or Zero.

On ARMv6 some software has to be installed in an old version first and updated
afterwards, because the current installers no longer support the architecture.
[Section 3](#3-packages-you-must-install-outdated-first) covers this. Read it
before you start.

> The [desktop installer](../installer.md) runs sections 2 to 8 from your own
> computer: package bootstrap, broker, firewall, Tailscale, the dashboard,
> HTTPS and the device services. This page is the manual route and explains
> what the installer automates and why.

## 0. What ends up where

| Where | What |
|---|---|
| Development machine | Go toolchain, Node.js, Python venv, this repository, the installer's developer CLI |
| Node, `/home/energynode/` | Python services, `devices/*.json`, the dashboard binary and its data dir |
| Node, `/etc/energy-node/` | `config.json` (the single source of truth) and `mqtt.pw` |
| Node, `/etc/energy-node-dashboard/` | `auth.pw` (dashboard admin password) |
| Node, `/etc/systemd/system/` | one unit per service |
| Node, `/etc/mosquitto/conf.d/` | broker config and the bridge to the main site |

Nothing is built on the node. The development machine builds an installation
package with the cross-compiled Go dashboard, the Python services as source and
their dependencies as wheels.

## 1. Prerequisites

At the remote site you need a Raspberry Pi (Pi 1 Model B or newer) with a
reliable power supply, an SD card, a network connection (Ethernet or WiFi) and
SSH access from the development machine. A Pi 1 with too little voltage
throttles without telling you, which is why the node's diagnostics report
`vcgencmd get_throttled`.

The main site needs Home Assistant with an MQTT broker (for example the
Mosquitto add-on) and the MQTT integration enabled. You also need a Tailscale
account with both machines in the same tailnet. The free tier is enough.

The development machine needs Go 1.22 or newer (`dashboard/go.mod`), Node.js
for the dashboard's JS tests, Python 3.9 or newer with a project venv, `git`
and `bash`. The installer module pins a newer Go than the dashboard.
`scripts/dev/run-installer.sh` lets Go fetch that toolchain itself.

```sh
git clone <your-fork> energy-node && cd energy-node
python3 -m venv .venv && .venv/bin/pip install -e libs/energy_node_common -e libs/battery_soc_core pytest
```

## 2. Operating system

Which image fits which model, how to write the card or stick and how the
node is prepared is part of the installer guide, see
[Operating system](../installer-operating-system.md). Do it first, then
continue here.

## 3. Packages you must install outdated first

Two ecosystems have dropped or restricted ARMv6 support. In both cases the
program still runs and only the installer refuses. The fix is the same each
time: install the last version that still supports the architecture, then let
the software update itself in place.

| Software | Why the normal path fails | What to do instead |
|---|---|---|
| Tailscale (ARMv6 only) | The official install script serves no ARMv6 build past 1.62.0 | The installer installs the pinned 1.62.0 package, then runs `tailscale update`. By hand, unpack the 1.62.0 tarball, then `tailscale update` |
| Python packages | Recent Raspberry Pi OS marks the system Python as externally managed (PEP 668) | Install into the system interpreter with `--break-system-packages` |

The Tailscale limit applies to ARMv6 only. The installation packages for `arm64`
and `amd64` carry a current Tailscale version, so the installer needs no
workaround there. On `armv6` the installer installs 1.62.0 and then runs
`tailscale update`.

### How the installer brings Tailscale and Caddy

The installer installs Tailscale (step 40) and Caddy (step 70) from Debian
packages. The installation package pins one package per architecture. Your
computer downloads the official packages and checks each file against the
checksum in the signed manifest before it copies them to the node. The node
needs no internet access for these two packages.

On `arm64` and `amd64` the node then follows the vendor's apt repository. Later
updates come with the system update step, like other system packages.

On `armv6` the installer installs Tailscale at the pinned 1.62.0 and then
updates it in place with `tailscale update`, so the node runs the current
Tailscale version after the installation. Caddy stays on the pinned version and
is held in apt, so a system update does not change it. A newer Caddy version
comes only with a new installation package.

If a download fails and the package is not on the node, the installer stops at
the **Prepare package** screen and asks how to go on. It asks only for a package
whose step you selected:

- **Install from the package source** installs the package from the vendor's
  apt repository on the node. It is offered only on `arm64` and `amd64`, and
  the node needs internet access for it.
- **Continue without Tailscale** or **Continue without Caddy** deselects that
  step and the installation goes on without the package. Without Tailscale,
  the device is only reachable on the local network. Without Caddy, the admin
  login in the dashboard stays locked.

### 3.1 Tailscale: install 1.62.0, then update

This is the manual route. The installer does this for you, see above. On a
node that already runs this tarball, the installer replaces its files with the
package.

```sh
cd ~
wget https://dl.tailscale.com/stable/tailscale_1.62.0_arm.tgz
tar -xzf tailscale_1.62.0_arm.tgz
cd tailscale_1.62.0_arm

sudo cp tailscale tailscaled /usr/sbin/
sudo cp systemd/tailscaled.service /etc/systemd/system/
sudo cp systemd/tailscaled.defaults /etc/default/tailscaled
```

Leave `/etc/default/tailscaled` at its default (`FLAGS=""`). Do not set
`--tun=userspace-networking`. With it, `tailscaled` creates no `tailscale0`
interface and installs no kernel routes, so the node cannot route IP traffic to
other tailnet peers at all. The ARMv6 kernel does not need it: `wireguard-go`
runs in userspace on every architecture and only needs `/dev/net/tun`, which
Raspberry Pi OS already provides.

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now tailscaled
sudo tailscale up
```

Open the printed login URL in any browser and approve the machine. Then update
in place to get off the old version:

```sh
sudo tailscale update
tailscale status
tailscale ip -4        # the node's 100.x.x.x address
```

Disable key expiry for this machine in the Tailscale admin console. Otherwise
the node drops off the tailnet after about 180 days and needs an interactive
login on site.

Reach the main site over its Tailscale IP (`100.x.x.x`) and not over a subnet
route.

Do not pass `--accept-routes` on the node and do not advertise routes from it.
On one node, an old `--advertise-routes` setting took effect once IP forwarding
was enabled and turned the node into a router for a whole `/24` without anyone
noticing. Clients with `--accept-routes` sent their LAN traffic for that subnet
through the tunnel, so `ping` and `curl` to those hosts failed while existing
SSH sessions stayed up. Clear it with
`sudo tailscale up --reset --advertise-routes=` and check in the admin console
that the route is gone.

On the Home Assistant side, run the Tailscale add-on with
`userspace_networking: false` so the HA host gets a `100.x` address. The
Mosquitto add-on's `1883:1883` port mapping binds every interface, so the node
reaches the broker at `<HA-Tailscale-IP>:1883`. You need neither
`advertise_routes` nor a route approval in the admin console.

Never point the bridge at a LAN IP that could also exist in the node site's own
network. Tailscale prefers a local network over an advertised route, so the
bridge would connect to the wrong local host. A `100.x` address cannot be
confused like that.

### 3.2 Python packages on an externally managed system

Recent Raspberry Pi OS releases mark the system Python as externally managed,
so `pip install` refuses to touch it. The services run on the system
interpreter on purpose. Every unit starts `/usr/bin/python3` and there is no
venv on the node, so you need the flag:

```sh
sudo python3 -m pip install --break-system-packages \
    "paho-mqtt>=2.0" apsystems-ez1 tinytuya requests
```

Install only what you need. Every service needs `paho-mqtt`. `apsystems-ez1`
is only for the EZ1 bridge, `tinytuya` only for Tuya devices and `requests` for
the Shelly and Trucki bridges.

On an older Legacy image the `--break-system-packages` flag does not exist yet.
Leave it out there and use plain `sudo python3 -m pip install …`.

The installer's bootstrap step 50 does this for you. It installs the wheels
from the bundle with `--no-index`. The build machine downloaded them from
piwheels beforehand. A wheel that the node does not have yet, for example one
that a new version of the installation package pins, is fetched by the node
itself from piwheels or PyPI. That needs internet access on the node.

### 3.3 Go and Node.js: do not install them on the node

Compiling Go on a Pi 1 takes forever and is not needed. The dashboard is
cross-compiled on the development machine into one statically linked ARMv6
binary with CGO disabled:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build -o energy-node-dashboard ./cmd/dashboard
```

`scripts/build/make_bundle.sh` runs this when it builds the installation
package. Node.js is only used for the dashboard's browser side tests on the
development machine.

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
sudo ufw allow 8081/tcp    # Home Assistant companion (HTTPS, own certificate)
sudo ufw allow 443/tcp     # dashboard (HTTPS via Caddy, see section 8)
sudo ufw enable
```

If you use the Shelly wake webhook (opt-in, see `docs/services/index.md`),
open its port as well. The installer only does this when you tick
**Shelly wake webhook in the firewall** on its configuration screen (step 35,
off by default, only offered while the Shelly service is selected):

```sh
sudo ufw allow 8082/tcp    # Shelly wake webhook, default webhook_port
```

## 5. Bridge to the main site

You configure the bridge to the main site on the dashboard's **MQTT** settings
page. The node opens an outgoing connection to the main site's broker and
mirrors `outstation/#` in both directions. Enter the main site's Tailscale IP
(`100.x`, see section 3.1) and the broker credentials.

Check on the main site that `outstation/#` messages arrive, with MQTT Explorer
or `mosquitto_sub -t 'outstation/#' -v`. Tailscale already encrypts the link,
so you need no extra TLS.

Next: [Deploying and verifying](manual-installation-deploy.md)

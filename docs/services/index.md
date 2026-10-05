---
title: "Device services"
redirect_from:
  - /device-services.html
anchor_moves:
  shelly: shelly.html
  sleepy-battery-devices-ht-and-friends: shelly.html#sleepy-battery-devices-ht-and-friends
  apsystems-ez1: apsystems-ez1.html
  trucki-stick: trucki.html
  tuya: tuya.html
  battery-state-of-charge: battery-soc.html
  automations: automations.html
  the-node-itself: ../dashboard/diagnostics.html
---

# Device services

Everything Energy Node knows about the world arrives through one of a handful
of small Python services. Each one is a **single systemd unit** that talks to
one family of hardware, and publishes what it finds to the local MQTT broker
as Home Assistant MQTT Discovery entities. Nothing talks HTTP to the
dashboard; the broker is the only coupling.

```
device ──(local HTTP / tinytuya)──▶ bridge ──(MQTT)──▶ Mosquitto ──▶ dashboard
                                                            │
                                                            └──▶ Home Assistant
                                                                 (via the bridge
                                                                  to the main site)
```

---

## What is available

| Service | Unit | Talks to | Direction |
|---|---|---|---|
| [Shelly](#shelly) | `shelly-rpc.service` | Shelly Gen1 (`/status`) and Gen2+ (`/rpc`) over HTTP | read + switch |
| [APsystems EZ1](#apsystems-ez1) | `apsystems-ez1.service` | EZ1 microinverters, local REST API | read + control |
| [Trucki stick](#trucki-stick) | `trucki-http.service` | T2SG / T2MG / T2HG sticks on Lumentree inverters, HTTP | read-only |
| [Tuya](#tuya) | `tuya.service` | Local Tuya devices via `tinytuya` | read + switch |
| [Battery SoC](#battery-state-of-charge) | `battery-soc.service` | Nothing — computes from other services' topics | computed |
| [Automations](#automations) | `automation.service` | The broker itself; evaluates rules | writes |

All of them are configured the same way, and all of them are editable from the
dashboard's *Configuration* tab rather than by hand.

---

## What every service has in common

**One central config, one device file.** Broker address, log level, paths and
per-service settings live in `/etc/energy-node/config.json` — mandatory, with
no environment-variable fallback: a service whose section is missing refuses to
start rather than come up half-configured. The devices themselves live in a
separate JSON file per service under `paths.devices_dir`, with a JSON schema
next to it. That schema is what the dashboard's configuration editor renders as
a form, so the field descriptions quoted below are the ones you see in the UI.

**Every service is self-describing.** Each service directory also carries a
`manifest.json` (`service_id`, `unit`, `schema`, `required`) and a
`config.schema.json` — the JSON Schema fragment for that service's
`services.<service_id>` block in the central config. `dashboard/cmd/schemagen`
composes those fragments, plus a hand-maintained core schema, into the
committed `dashboard/internal/appconfig/config.schema.json`; the Python
services read the active service set and its required fields from the manifests
alone.

**A predictable topic tree.** Every device gets `outstation/<id>/…` as its base
topic, where `<id>` is the `id` from its config entry:

| Topic | Purpose |
|---|---|
| `outstation/<id>/status/online` | Availability, also the MQTT last will |
| `outstation/<id>/<measurement>` | State, one value per topic |
| `outstation/<id>/set/<thing>` | Command topics, where a service accepts writes |
| `outstation/<id>/settings/<name>` / `…/set` | Poll-rate protocol, see below |
| `homeassistant/<component>/<id>/<object>/config` | Retained discovery payload |

`outstation/#` is exactly the subtree the Mosquitto bridge mirrors to the main
site, which is why nothing else is allowed to publish outside it.

**Availability is explicit.** Each device announces an `availability_topic` and
sets a last will, so an unreachable device shows as offline in both Home
Assistant and the dashboard rather than freezing on its last value.

**Poll rates are centrally controlled.** `energy_node_mqtt.py` is the *master*
of a small master/slave protocol in `energy_node_common`; every other service
is a *slave*. The node offers Home Assistant `number` entities for the poll
interval (5–3600 s) and the diagnostic multiplier (1–200), forwards a change to
the service concerned, and mirrors back the value the service actually adopted.
The state topic is the acknowledgement, never the request — the two are
separate topics on purpose.

**Diagnostic data is polled far less often.** Each service splits its work into
a core poll and a diagnostic poll; the latter runs every
`poll_interval_s × diagnostic_poll_multiplier` seconds. Values that change
slowly (firmware version, configured limits, signal strength) do not cost a
request every cycle.

**Simulation mode.** Each slave can be switched into a simulation mode over
MQTT, in which it publishes plausible moving values without touching the
hardware — useful to check a layout or a rule without a device on the bench.

---

## The shared package

`libs/energy_node_common/` is an installable package that every service depends
on: MQTT client setup and last will, discovery payload construction,
availability publishing, the asyncio poll scheduler with its diagnostic-poll
hook, the central config loader, and both sides of the master/slave settings
protocol. It exists so a topic convention or a validation rule is written once
— a value the master accepts can never be rejected differently by a slave.

---

## Services

- [Shelly](shelly.md)
- [APsystems EZ1](apsystems-ez1.md)
- [Trucki stick](trucki.md)
- [Tuya](tuya.md)
- [Battery state of charge](battery-soc.md)
- [Automations engine](automations.md)

## See also

- [The dashboard, page by page](../dashboard/index.md) — including the configuration editor for these files
- [Data flow](../developing/data-flow.md) — the full path of a value, from device to Home Assistant
- [Configuration](../operating/configuration.md) — every field of `/etc/energy-node/config.json`
- [Performance and resources](../operating/performance.md) — what each service costs on a Pi 1
- [Dependencies](../developing/dependencies.md) — third-party code and ideas a service's implementation was adapted from

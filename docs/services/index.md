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

All data in Energy Node comes from a handful of small Python services. Each one
is a single systemd unit that talks to one family of hardware and publishes
what it finds to the local MQTT broker as Home Assistant MQTT Discovery
entities. None of them talks HTTP to the dashboard. They are only connected
through the broker.

```
device ──(local HTTP / tinytuya)──▶ bridge ──(MQTT)──▶ Mosquitto ──▶ dashboard
                                                            │
                                                            └──▶ Home Assistant
                                                                 (via the bridge
                                                                  to the main site)
```

## What is available

| Service | Unit | Talks to | Direction |
|---|---|---|---|
| [Shelly](#shelly) | `shelly-rpc.service` | Shelly Gen1 (`/status`) and Gen2+ (`/rpc`) over HTTP | read + switch |
| [APsystems EZ1](#apsystems-ez1) | `apsystems-ez1.service` | EZ1 microinverters, local REST API | read + control |
| [Trucki stick](#trucki-stick) | `trucki-http.service` | T2SG / T2MG / T2HG sticks on Lumentree inverters, HTTP | read-only |
| [Tuya](#tuya) | `tuya.service` | Local Tuya devices via `tinytuya` | read + switch |
| [Battery SoC](#battery-state-of-charge) | `battery-soc.service` | No device. Computes from other services' topics | computed |
| [Automations](#automations) | `automation.service` | The broker itself. Evaluates rules | writes |

All of them are configured the same way, and you can edit all of them in the
dashboard's *Configuration* tab instead of by hand.

## What every service has in common

Broker address, log level, paths and the settings of each service live in
`/etc/energy-node/config.json`. The file is mandatory and there is no fallback
to environment variables. A service whose section is missing does not start.
The devices live in a separate JSON file per service under `paths.devices_dir`,
with a JSON schema next to it. The dashboard's configuration editor renders that
schema as a form, so the field descriptions quoted on these pages are the ones
you see in the UI.

Each service directory also has a `manifest.json` (`service_id`, `unit`,
`schema`, `required`) and a `config.schema.json`, the JSON Schema fragment for
the service's `services.<service_id>` block in the central config.
`dashboard/cmd/schemagen` combines those fragments with a hand-written core
schema into the committed `dashboard/internal/appconfig/config.schema.json`.
The Python services read the active services and their required fields from
the manifests only.

Every device gets `outstation/<id>/…` as its base topic, where `<id>` is the
`id` from its config entry:

| Topic | Purpose |
|---|---|
| `outstation/<id>/status/online` | Availability, also the MQTT last will |
| `outstation/<id>/<measurement>` | State, one value per topic |
| `outstation/<id>/set/<thing>` | Command topics, where a service accepts writes |
| `outstation/<id>/settings/<name>` / `…/set` | Poll-rate protocol, see below |
| `homeassistant/<component>/<id>/<object>/config` | Retained discovery payload |

The Mosquitto bridge mirrors exactly the `outstation/#` subtree to the main
site, so the services publish nothing outside it.

Each device announces an `availability_topic` and sets a last will. An
unreachable device then shows as offline in Home Assistant and the dashboard
instead of keeping its last value.

Poll rates are controlled centrally through a small master/slave protocol in
`energy_node_common`. `energy_node_mqtt.py` is the master and every other
service is a slave. The node offers Home Assistant `number` entities for the
poll interval (5–3600 s) and the diagnostic multiplier (1–200), forwards a
change to the service concerned and reports back the value the service actually
took. The state topic is the acknowledgement and the request goes to a separate
topic.

Each service splits its work into a core poll and a diagnostic poll. The
diagnostic poll runs every `poll_interval_s × diagnostic_poll_multiplier`
seconds, so slowly changing values such as firmware version, configured limits
or signal strength do not cost a request every cycle.

Each slave can be switched into simulation mode over MQTT. It then publishes
plausible changing values without touching the hardware, which lets you check a
layout or a rule without a device on the bench.

## The shared package

Every service depends on the installable package `libs/energy_node_common/`.
It contains the MQTT client setup and last will, the construction of discovery
payloads, availability publishing, the asyncio poll scheduler with its hook for
diagnostic polls, the central config loader and both sides of the master/slave
settings protocol. Topic conventions and validation rules are written once
there, so a slave cannot reject a value the master accepted.

## Services

- [Shelly](shelly.md)
- [APsystems EZ1](apsystems-ez1.md)
- [Trucki stick](trucki.md)
- [Tuya](tuya.md)
- [Battery state of charge](battery-soc.md)
- [Automations engine](automations.md)

## See also

- [The dashboard, page by page](../dashboard/index.md), including the configuration editor for these files
- [Data flow](../developing/data-flow.md): the full path of a value from the device to Home Assistant
- [Configuration](../operating/configuration.md): every field of `/etc/energy-node/config.json`
- [Performance and resources](../operating/performance.md): what each service costs on a Pi 1
- [Dependencies](../developing/dependencies.md): third-party code and ideas the services were adapted from

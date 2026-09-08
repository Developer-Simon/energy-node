---
title: "Device services"
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
| [Energy Node](#the-node-itself) | `energy-node.service` | The Raspberry Pi it runs on | read + master |

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

## Shelly

`src/shelly/` · `shelly-rpc.service` · `shelly_devices.json`

Polls Shelly devices over HTTP and publishes one state per cycle. MQTT is
deliberately left **disabled on the devices themselves**: their own MQTT client
publishes on nearly every value change, which is more traffic than a Pi 1 and a
bridged broker want, and the rate is not configurable on the device.

Both generations are covered by one implementation — `generation: 1` polls
`/status`, `generation: 2` polls `/rpc`.

Capabilities are declared per device rather than detected, which keeps the
polling honest about what it asks for:

| Field | Meaning |
|---|---|
| `switch_channels` | Number of relay channels |
| `has_power` / `has_energy` | Reports instantaneous power / energy |
| `has_3phase` | Shelly 3EM — three phases, incl. returned energy |
| `adc_channels` | Number of ADC inputs (Shelly Uni) |
| `has_temperature` / `has_humidity` | e.g. Shelly Plus H&T |
| `auth_user` / `auth_password` | Optional HTTP basic auth |

**Entities published:** a `switch` per relay channel (command topic
`outstation/<id>/relay/<ch>/set`); power and energy per channel, or per phase
plus voltage and returned energy on a 3EM; one voltage sensor per ADC channel;
temperature; humidity; and WLAN signal strength as a diagnostic entity that is
disabled by default.

**Presets.** `shelly_presets.json` holds reusable capability templates — Shelly
1, Plug S, Plug S+, 3EM, Uni, 1PM Gen2, 1PM Gen3, Plus H&T — that the
configuration UI applies to a new entry. A preset never carries identity
(`id`, `name`, `host`) or credentials, only the technical fields above.

Editing `shelly_devices.json` needs a service restart; that file has no hot
reload.

---

## APsystems EZ1

`src/apsystems_ez1/` · `apsystems-ez1.service` · `apsystems_devices.json`

One service for all configured EZ1 microinverters, over their **local** REST
API (`host`, `port`, default 8050) — no cloud account. Each inverter keeps its
own topic prefix, entities and availability.

**Entities published:** power, daily yield and lifetime yield, each for string
PV1, string PV2 and the total — nine sensors, with `total_increasing` state
class on the lifetime counters so Home Assistant's energy dashboard accepts
them. Additional fields found on the inverter are published as diagnostic
sensors that are disabled by default.

**Control:**

| Entity | Topic | Range |
|---|---|---|
| Power limit (`number`) | `outstation/<id>/set/max_power_limit_w` | 30–800 W |
| Operating status (`switch`) | `outstation/<id>/set/power_status` | on/off |

The EZ1 stores its power limit in flash, and community reports point at flash
wear from frequent writes. The bridge therefore enforces a **minimum of five
minutes between two writes** — a rule in the service, not in the UI, so it
holds no matter who publishes the command.

---

## Trucki stick

`src/trucki/` · `trucki-http.service` · `trucki_devices.json`

Community-firmware WLAN sticks on Lumentree/Growatt inverters. All three
variants are covered by one implementation — T2SG (zero export), T2MG and T2HG
(surplus charging) — and **not** by per-variant special cases: the bridge
publishes every field it finds, so a variant with an extra field needs no code
change.

Like the Shelly bridge, this exists to replace the stick's own MQTT client,
which publishes on practically every value change. Instead the bridge fetches
two flat JSON endpoints on an interval it controls:

- `/jsonlive` — live measurements, on the normal poll cycle
- `/jsononce` — configuration and identity data, on the diagnostic cycle and
  once on connect

**Entities published:** grid voltage, battery voltage, temperature, AC power
and setpoint, power limit, zero-export power and target, meter power, daily and
total energy, meter energy and readout time, round-trip times — each mapped to
the right unit, device class and state class.

Two deliberate limits:

- **Read-only.** The firmware accepts writes over `GET /?KEY=VALUE&…&save=true`,
  `GET /?reboot=true` and `GET /?zepc_enable=…`. None of it is implemented.
- **Password fields from `/jsononce` are never published** — `ADMIN_PASS`,
  `METER_PASS`, `MQTT_PASS`, `WIFIPASS`, `BEARER` are dropped before publishing.

If the stick and a battery bank are physically the same box, set `via_device`
to the `id` of the matching `battery_soc_devices.json` entry; Home Assistant
then shows the stick as connected via the battery device instead of merging
them into one.

---

## Tuya

`src/tuya_mqtt/` · `tuya.service` · `tuya_devices.json`

Local Tuya devices through `tinytuya` — local key, local IP, local protocol,
no cloud at runtime. A device needs `device_id`, `local_key`, `ip` and the
protocol `version` (default 3.3), all of which come out of
`python3 -m tinytuya wizard` once.

`datapoints` maps a logical function to a Tuya data point number. The switch
data point is **not** reliably `1`, which is why the dashboard ships a
[TinyTuya wizard](dashboard.md#tinytuya-setup-tinytuya-einrichten) that probes
the device and fills the number in.

**Entities published:** a `switch` per device, state on
`outstation/<id>/switch`, commands on `outstation/<id>/set/switch`. A device
that cannot be reached publishes `UNKNOWN` rather than a stale on/off.

---

## Battery state of charge

`src/battery_soc/` + `src/battery_soc_core/` · `battery-soc.service` ·
`battery_soc_devices.json`

The one service that polls nothing. It **subscribes** to power and voltage
topics that the other bridges already publish, and computes the state of charge
of one or two LiFePO4 banks by coulomb counting — with voltage recalibration at
the ends of the curve, per-converter efficiency, and load compensation on the
measured cell voltage.

It is a monitoring estimate, not a BMS. How the engine works is documented in
detail in
[`knowledge/services/battery-soc-how-it-works.md`](knowledge/services/battery-soc-how-it-works.md).

**Inputs.** Charger power, inverter power, and one voltage topic per bank —
each as a topic plus an optional JSON key, because a Trucki stick publishes a
bare number where a Shelly publishes an object. Optional DC-side power topics
take over from the AC measurements while they are fresh (`dc_max_age_s`), and
fall back automatically when they go stale.

**Topology.** `parallel` (both banks on one DC bus, one voltage) or `series`;
`bank_b_enabled: false` for a single-bank installation. The entity list follows
the topology — a series pack additionally gets per-bank SoC, the voltage
difference between banks and an imbalance warning.

**Calibration and efficiency.** Cell count and capacity per bank; the
open-circuit volts per cell that count as empty and full; how far those
thresholds may soften at rest (`calibration_tolerance_v_per_cell`); how long a
voltage must hold before calibration applies; charger AC→DC and inverter DC→AC
efficiency; and the charge efficiency of the cells themselves.

**Entities published:** combined SoC, net battery power, "inputs stale" and
"AC fallback active" as problem sensors, time to full and time to empty; then
per unit (pack, or bank A and bank B) voltage, estimated current, remaining
Ah, load-corrected cell voltage, last calibration timestamp, the two active
calibration thresholds, a voltage-based SoC estimate and a
voltage-versus-coulomb mismatch warning. Finally a `number` entity to **set the
SoC by hand** — one for the pack, or one per bank on a series pack — which is
the way back after an outage that lost the count.

Discovery is cleaned up as the topology changes: object IDs that do not belong
to the current configuration are cleared with an empty retained payload rather
than left behind as ghost entities.

The same engine is also available as a **native Home Assistant integration**
under `integrations/homeassistant/`, installable through HACS — the same core
with a config flow instead of MQTT topics. See
[`integration/ha-integration-hacs-release.md`](integration/ha-integration-hacs-release.md).

---

## Automations

`src/automation/` · `automation.service` · `automation_rules.json`

The rule engine. It is a **separate process from the dashboard on purpose**: the
dashboard edits the rule file and never publishes a rule's action itself, so a
read-only web UI cannot switch a relay.

It subscribes to the balance the dashboard publishes on
`outstation/dashboard/energy/balance`, plus any MQTT topics the rules name, and
evaluates on a tick (`tick_interval_s`, default 10 s). At most 16 rules, each
with up to 8 conditions and 8 actions.

**Conditions:**

| Type | Matches on |
|---|---|
| `balance_threshold` | A balance field — `pv`, `grid_import`, `grid_export`, `load_total`, `base`, `wallbox`, `heat_pump`, `battery_charge`, `battery_discharge`, `battery_soc`, `autarkie`, `eigenverbrauch`, … |
| `topic_value` | A raw MQTT topic, optionally a JSON key inside it |
| `entity_value` | An entity, with a Home Assistant style value template |
| `time_window` | Start/end time and weekdays |

Threshold conditions carry `hysteresis` and `hold_seconds`, so a rule does not
chatter around its threshold or fire on a single spike, and a rule carries
`cooldown_seconds` as a lockout after it has fired. `settling_seconds` keeps
everything quiet for a while after a restart, and `balance_max_age_s` stops
rules from acting on a balance that has gone stale.

**Actions** are `publish` (constant payload, a balance field, a value copied
from another topic, or a toggle — with scale, offset, min/max, step and
decimals) and `notification` (info, warning or critical).

**Safety.** `publish_allowed_prefixes` is the list of topic prefixes a rule may
write to, and the Python service is **fail-closed**: an empty list means no rule
publishes anywhere. The dashboard fills the prefixes in when a rule is saved,
but the enforcement lives in the service.

Rule history (`history_limit`, `history_persist`) records the last triggers per
rule so the dashboard can show why something switched.

---

## The node itself

`src/energy-node/` · `energy-node.service`

The Raspberry Pi published as its own Home Assistant device, and the **master**
of the poll-rate protocol.

**Entities published:** CPU temperature, CPU load, RAM use, disk use, WLAN
signal strength (disabled by default — it only matters on a WLAN-attached
node); undervoltage now, undervoltage since last boot and CPU throttled now, as
`problem` binary sensors; last boot as a timestamp; IP address; Mosquitto
running and Tailscale connected; and the number of pending package updates.
On top of that, one "last update" sensor per bridge, which is how a silently
dead bridge becomes visible.

Its device ID is what the other services point their `via_device` at, so Home
Assistant lists them all as *connected via Energy Node*.

---

## The shared package

`src/energy_node_common/` is an installable package that every service depends
on: MQTT client setup and last will, discovery payload construction,
availability publishing, the asyncio poll scheduler with its diagnostic-poll
hook, the central config loader, and both sides of the master/slave settings
protocol. It exists so a topic convention or a validation rule is written once
— a value the master accepts can never be rejected differently by a slave.

---

## See also

- [The dashboard, page by page](dashboard.md) — including the configuration editor for these files
- [`knowledge/data-flow.md`](knowledge/data-flow.md) — the full path of a value, from device to Home Assistant
- [`knowledge/configuration.md`](knowledge/configuration.md) — every field of `/etc/energy-node/config.json`
- [`knowledge/performance-and-resources.md`](knowledge/performance-and-resources.md) — what each service costs on a Pi 1

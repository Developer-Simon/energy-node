---
title: "APsystems EZ1 — Entities and Power-Limit Handling"
---

# APsystems EZ1 — Entities and Power-Limit Handling

What `services/apsystems_ez1/apsystems_ez1_mqtt.py` publishes for each
configured EZ1 microinverter, and how it protects the inverter's flash memory
while changing the power limit. Supplements the "APsystems EZ1" section in
[device-services.md](../../device-services.md).

One service instance handles every configured inverter over its **local**
REST API (`host`, `port`, default `8050`) — no cloud account, no vendor app in
the loop. Each inverter keeps its own topic prefix
(`outstation/<id>/…`), entities and availability, following the conventions
described in [device-services.md](../../device-services.md#what-every-service-has-in-common).

## Entities

**Core sensors**, polled every cycle:

| Entity | Topic | Unit | Notes |
|---|---|---|---|
| PV1 power | `pv1/power_w` | W | `state_class: measurement` |
| PV2 power | `pv2/power_w` | W | `state_class: measurement` |
| Total power | `total/power_w` | W | sum of PV1 + PV2 |
| PV1 energy today | `pv1/energy_today_kwh` | kWh | resets at midnight |
| PV2 energy today | `pv2/energy_today_kwh` | kWh | resets at midnight |
| Total energy today | `total/energy_today_kwh` | kWh | |
| PV1 energy lifetime | `pv1/energy_lifetime_kwh` | kWh | `state_class: total_increasing` |
| PV2 energy lifetime | `pv2/energy_lifetime_kwh` | kWh | `state_class: total_increasing` |
| Total energy lifetime | `total/energy_lifetime_kwh` | kWh | |

`total_increasing` on the lifetime counters is what makes Home Assistant's
energy dashboard accept them as a source.

**Control entities:**

| Entity | Topic | Range |
|---|---|---|
| Power limit (`number`) | `outstation/<id>/set/max_power_limit_w` | 30–800 W |
| Operating status (`switch`) | `outstation/<id>/set/power_status` | on/off |

**Diagnostic sensors**, polled on the diagnostic cycle
(`poll_interval_s × diagnostic_poll_multiplier`, default every 10 minutes) and
disabled by default in Home Assistant:

- `device_info/*` — every field the inverter's `getDeviceInfo` returns
  (`deviceId`, `devVer`, `ssid`, `ipAddr`, `minPower`, `maxPower`,
  `isBatterySystem`), published one sensor per field.
- `alarm/*` — every field of `getAlarmInfo` (`offgrid`, `shortcircuit_1`,
  `shortcircuit_2`, `operating`).
- `max_power_flash_default_w` — the power limit as stored in **flash**, kept
  separate from the RAM-backed `number` entity above. See
  [Power-limit handling](#power-limit-handling-ram-vs-flash) below. Only
  published once the service has confirmed the inverter supports the
  RAM/flash split.
- `output_detail/*` — extended electrical readings from the undocumented
  `getOutputDataDetail` endpoint: `v1`/`v2` (PV input voltage per string, V),
  `c1`/`c2` (PV input current per string, A), `gv`/`gf` (grid voltage/frequency),
  `t` (inverter temperature, °C). Not every firmware exposes all of these —
  see [Extended diagnostics](#extended-diagnostics-getoutputdatadetail) below.

Any field the library or the raw API happens to return beyond what is listed
above is still published: `_publish_object_fields()` iterates the response
object generically rather than hand-picking keys, so a firmware update that
adds a field shows up as a new diagnostic sensor without a code change.

## Power-limit handling: RAM vs. flash

Early EZ1 firmware stores the power limit exclusively in flash, and every
`setMaxPower` call is a flash write. Frequent writes — an automation that
adjusts the limit often, for example — wear the flash out over time.
Community reports on newer firmware found a second, undocumented pair of
endpoints, `getDefaultMaxPower` / `setDefaultMaxPower`, that read and write a
**flash-backed ceiling** independently of `setMaxPower`, which on that
firmware generation writes **RAM only**. See
[Source and credit](#source-and-credit) for where this came from.

The service adapts to whichever behavior the connected inverter actually has,
rather than assuming a firmware version threshold:

1. **Detection, once per device per service run.** On the first diagnostic
   poll, the service calls `getDefaultMaxPower`. If the inverter answers, the
   RAM/flash split is available (`_ram_mode = True`); if the endpoint errors
   out, the inverter is treated as flash-only (`_ram_mode = False`) for the
   rest of the run — permanently, not re-probed every cycle.
2. **One-time flash-ceiling raise.** If RAM mode is confirmed and the
   flash-stored default is below the inverter's hardware maximum, the service
   calls `setDefaultMaxPower` exactly once to raise it to that maximum. From
   then on, every `setMaxPower` call only ever touches RAM.
3. **Drift detection and restore.** The inverter reloads flash into RAM after
   it restarts — typically overnight, when it powers down at dusk and back up
   at dawn. Each diagnostic poll compares the live RAM value against the last
   value the service (or a user, via the `number` entity) explicitly
   requested. A mismatch is treated as a post-restart reset and the desired
   value is written back immediately — a RAM write, so it does not consume a
   flash-write cycle.
4. **Flash-only fallback.** Inverters without the RAM/flash split keep the
   behavior this service always had: writes go straight to flash, and a
   minimum of **five minutes between two writes** is enforced in the service
   itself (`MIN_SECONDS_BETWEEN_POWER_WRITES`) — a rule that holds no matter
   who publishes the command, not just the dashboard.

The `number.max_power_limit` entity always reflects the **RAM** value (what
the inverter is actually doing right now); the separate
`max_power_flash_default_w` diagnostic sensor shows the **flash** ceiling, so
the two concepts stay visible and distinguishable instead of being collapsed
into one number.

## Extended diagnostics (`getOutputDataDetail`)

`getOutputDataDetail` is not part of the inverter's documented API and is
called through the underlying library's internal request method. Firmware
support varies:

- Some firmware returns all seven fields (`v1`, `v2`, `c1`, `c2`, `gv`, `gf`,
  `t`).
- Some returns grid voltage/frequency and temperature but omits the PV
  input voltage/current fields — still treated as supported, just with those
  four sensors left unpublished.
- Firmware that returns none of the seven fields, or fails the request
  outright, is marked unsupported after the first attempt and not polled
  again for the lifetime of the service run.

This mirrors the RAM/flash detection: probe once, remember the result, never
retry an endpoint a device has already shown it does not have.

## Source and credit

The RAM/flash power-limit handling and the `getOutputDataDetail` diagnostics
were adapted from ideas documented by the community-maintained
[`apsystems-ez1-enhanced`](https://github.com/shopf/apsystems-ez1-enhanced)
Home Assistant integration, discussed in the
[Home Assistant community forum thread](https://community.home-assistant.io/t/apsystems-ez1-m-ez1-spe-ez1-lv-ez1-h-ez1d-l-ez1d-ez1d-h-community-enhanced-integration-extended-sensors-all-models-overnight-fix-more/994091)
for the same project. Both projects talk to the same inverter family through
the same underlying `apsystems-ez1` PyPI package — no code was copied, only
the endpoint behavior and the flash-protection strategy. See also
[Third-party sources](../dependencies.md#apsystems-ez1).

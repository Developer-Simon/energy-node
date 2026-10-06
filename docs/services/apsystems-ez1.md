---
title: "APsystems EZ1"
component: service:apsystems_ez1
---

# APsystems EZ1

This page covers what `services/apsystems_ez1/apsystems_ez1_mqtt.py` publishes
for each configured EZ1 microinverter and how it protects the inverter's flash
memory when it changes the power limit. [Device services](index.md) shows where
the service sits among the others.

## At a glance

One service handles all configured EZ1 microinverters through their local REST
API (`host`, `port`, default 8050). No cloud account is needed. Each inverter
has its own topic prefix, entities and availability.

The service publishes power, daily yield and lifetime yield for string PV1,
string PV2 and the total, which makes nine sensors. The lifetime counters have
the `total_increasing` state class, so Home Assistant's energy dashboard accepts
them. There is also a power limit `number`, an operating status `switch` and a
set of diagnostic sensors that are disabled by default. Where the firmware
supports it, those include extended electrical readings: PV input voltage and
current, grid voltage and frequency, and temperature.

The EZ1 can store its power limit in flash, and frequent writes wear out flash
over time. The service checks for each inverter whether its firmware can keep
the limit in RAM instead and adjusts how it writes. For inverters without that
support, the service allows at most one write every five minutes, no matter who
publishes the command.

## Enabling local mode

Out of the box the EZ1 only talks to APsystems' own cloud (EMA). Switch on the
inverter's local API first, using the official APsystems EZ1 app:

1. Connect the app directly to the inverter, over Bluetooth or the inverter's
   own Wi-Fi access point if it is not on the home network yet, and not through
   an APsystems cloud account. If the app is signed in to the cloud, sign out
   first. The **Local Mode** menu is hidden while a cloud session is active.
2. Open **Settings → Local Mode**, enable it and set it to **Continuous**
   instead of only the current session, so it survives a reconnect.
3. Note the IP address shown there. It is the `host` for the service's device
   entry, on port `8050`.

The inverter then runs its own HTTP server on that port and no longer needs the
cloud for API access. The manufacturer's
[EZ1 Local API User Manual](https://forum.iobroker.net/assets/uploads/files/1701255814508-apsystems-ez1-local-api-user-manual.pdf)
walks through it with screenshots.

## Entities

Core sensors, polled every cycle:

| Entity | Topic | Unit | Notes |
|---|---|---|---|
| PV1 power | `pv1/power_w` | W | `state_class: measurement` |
| PV2 power | `pv2/power_w` | W | `state_class: measurement` |
| Total power | `total/power_w` | W | Sum of PV1 and PV2 |
| PV1 energy today | `pv1/energy_today_kwh` | kWh | Resets at midnight |
| PV2 energy today | `pv2/energy_today_kwh` | kWh | Resets at midnight |
| Total energy today | `total/energy_today_kwh` | kWh | |
| PV1 energy lifetime | `pv1/energy_lifetime_kwh` | kWh | `state_class: total_increasing` |
| PV2 energy lifetime | `pv2/energy_lifetime_kwh` | kWh | `state_class: total_increasing` |
| Total energy lifetime | `total/energy_lifetime_kwh` | kWh | |

Home Assistant's energy dashboard accepts the lifetime counters as a source
because of `total_increasing`.

Control entities:

| Entity | Topic | Range |
|---|---|---|
| Power limit (`number`) | `outstation/<id>/set/max_power_limit_w` | 30–800 W |
| Operating status (`switch`) | `outstation/<id>/set/power_status` | on/off |

Diagnostic sensors are polled on the diagnostic cycle
(`poll_interval_s × diagnostic_poll_multiplier`, by default every 10 minutes)
and are disabled by default in Home Assistant:

- `device_info/*`: every field the inverter's `getDeviceInfo` returns
  (`deviceId`, `devVer`, `ssid`, `ipAddr`, `minPower`, `maxPower`,
  `isBatterySystem`), one sensor per field.
- `alarm/*`: every field of `getAlarmInfo` (`offgrid`, `shortcircuit_1`,
  `shortcircuit_2`, `operating`).
- `max_power_flash_default_w`: the power limit stored in flash, separate from
  the RAM based `number` entity above. See
  [Power-limit handling](#power-limit-handling-ram-vs-flash) below. It is only
  published once the service has confirmed that the inverter supports the
  RAM/flash split.
- `output_detail/*`: extended electrical readings from the undocumented
  `getOutputDataDetail` endpoint. `v1`/`v2` are the PV input voltage per string
  (V), `c1`/`c2` the PV input current per string (A), `gv`/`gf` the grid
  voltage and frequency and `t` the inverter temperature (°C). Not every
  firmware provides all of them, see
  [Extended diagnostics](#extended-diagnostics-getoutputdatadetail) below.

Fields the library or the raw API return beyond this list are published as
well. `_publish_object_fields()` walks the whole response object instead of
picking keys, so a field added by a firmware update appears as a new diagnostic
sensor without a code change.

## Power-limit handling: RAM vs. flash

Early EZ1 firmware stores the power limit only in flash, so every `setMaxPower`
call is a flash write. Frequent writes, for example from an automation that
adjusts the limit often, wear out the flash. On newer firmware the community
found a second, undocumented pair of endpoints, `getDefaultMaxPower` and
`setDefaultMaxPower`. They read and write a ceiling stored in flash, separately
from `setMaxPower`, which on that firmware only writes to RAM. See
[Source and credit](#source-and-credit) for where this came from.

The service checks what the connected inverter actually does instead of
relying on a firmware version number:

1. **Detection, once per device per service run.** On the first diagnostic
   poll the service calls `getDefaultMaxPower`. If the inverter answers, the
   RAM/flash split is available (`_ram_mode = True`). If the endpoint fails,
   the inverter is treated as flash only (`_ram_mode = False`) for the rest of
   the run and is not probed again.
2. **Raising the flash ceiling once.** If RAM mode is confirmed and the default
   stored in flash is below the inverter's hardware maximum, the service calls
   `setDefaultMaxPower` once to raise it to that maximum. After that,
   `setMaxPower` only ever writes to RAM.
3. **Restoring after a restart.** The inverter loads the flash value into RAM
   when it restarts, usually overnight when it powers down at dusk and back up
   at dawn. Each diagnostic poll compares the RAM value with the last value the
   service or a user (through the `number` entity) requested. A difference
   counts as a reset after a restart, and the service writes the requested
   value back at once. That is a RAM write and costs no flash write cycle.
4. **Flash only fallback.** Inverters without the RAM/flash split write
   straight to flash. The service itself enforces at least five minutes between
   two writes (`MIN_SECONDS_BETWEEN_POWER_WRITES`), so the limit applies to
   every publisher and not only to the dashboard.

The `number.max_power_limit` entity always shows the RAM value, which is what
the inverter is doing right now. The separate `max_power_flash_default_w`
diagnostic sensor shows the ceiling in flash, so you can see both values.

## Extended diagnostics (`getOutputDataDetail`)

`getOutputDataDetail` is not part of the inverter's documented API. The service
calls it through the library's internal request method. Firmware support
varies. Some firmware returns all seven fields (`v1`, `v2`, `c1`, `c2`, `gv`,
`gf`, `t`). Some returns grid voltage, grid frequency and temperature but not
the PV input voltage and current. That still counts as supported, and those four
sensors are simply not published. Firmware that returns none of the seven
fields or fails the request is marked unsupported after the first attempt and
is not asked again until the service restarts.

Like the RAM/flash detection, the service probes once and remembers the
result.

## Source and credit

The RAM/flash power limit handling and the `getOutputDataDetail` diagnostics
are based on ideas documented by the community maintained
[`apsystems-ez1-enhanced`](https://github.com/shopf/apsystems-ez1-enhanced)
Home Assistant integration. Both projects talk to the same inverters through
the same `apsystems-ez1` PyPI package. No code was copied, only the knowledge
of how the endpoints behave and the flash protection strategy. See also
[Third-party sources](../developing/dependencies.md#apsystems-ez1).

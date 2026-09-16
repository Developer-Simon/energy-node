---
title: "Battery State of Charge (SoC)"
---

# Battery State of Charge (SoC)

What `services/battery_soc/` publishes and how it is configured. See
[device-services.md](../device-services.md#battery-state-of-charge) for where
this service sits among the others, and
[`knowledge/services/battery-soc-how-it-works.md`](../knowledge/services/battery-soc-how-it-works.md)
for the coulomb-counting algorithm itself — the calibration math, the AC/DC
efficiency handling and why it works the way it does.

`services/battery_soc/` + `libs/battery_soc_core/` · `battery-soc.service` ·
`battery_soc_devices.json`

The one service that polls nothing. It **subscribes** to power and voltage
topics that the other bridges already publish, and computes the state of
charge of one or two LiFePO4 banks by coulomb counting — with voltage
recalibration at the ends of the curve, per-converter efficiency, and load
compensation on the measured cell voltage.

It is a monitoring estimate, not a BMS.

## Configuration

**Inputs.** Charger power, inverter power, and one voltage topic per bank —
each as a topic plus an optional JSON key, because a Trucki stick publishes a
bare number where a Shelly publishes an object. Optional DC-side power topics
take over from the AC measurements while they are fresh (`dc_max_age_s`), and
fall back automatically when they go stale.

**Topology.** `parallel` (both banks on one DC bus, one voltage) or `series`;
`bank_b_enabled: false` for a single-bank installation. The entity list
follows the topology — a series pack additionally gets per-bank SoC, the
voltage difference between banks and an imbalance warning.

**Calibration and efficiency.** Cell count and capacity per bank; the
open-circuit volts per cell that count as empty and full; how far those
thresholds may soften at rest (`calibration_tolerance_v_per_cell`); how long a
voltage must hold before calibration applies; charger AC→DC and inverter
DC→AC efficiency; and the charge efficiency of the cells themselves.

## Entities

All sensor and binary-sensor entities read from a single JSON state topic,
`outstation/<id>/state`, via a `value_template`; the "Field" column below is
the JSON key inside that payload. Most carry `entity_category: diagnostic`,
noted where it applies — unlike the other services' diagnostic sensors, none
of these are disabled by default.

**Always published:**

| Entity | Field | Unit | Notes |
|---|---|---|---|
| SoC | `soc_combined_pct` | % | combined; on a series pack this is the weaker bank |
| Net battery power | `net_power_w` | W | positive = net charge |
| Inputs stale | `inputs_stale` | — | `binary_sensor`, problem, diagnostic |
| AC fallback active | `ac_fallback_active` | — | `binary_sensor`, problem, diagnostic |
| Time to full | `time_to_full_h` | h | diagnostic |
| Time to empty | `time_to_empty_h` | h | diagnostic |

**Per unit** — once for `pack`, or once each for `bank_a` / `bank_b` on a
series topology (`<unit>` below stands for that unit's name):

| Entity | Field | Unit | Notes |
|---|---|---|---|
| Voltage | `<unit>_voltage_v` | V | diagnostic |
| Current (estimated) | `<unit>_current_a` | A | diagnostic |
| Remaining capacity | `<unit>_remaining_ah` | Ah | diagnostic |
| Load-corrected cell voltage | `<unit>_corrected_v_per_cell` | V | diagnostic |
| Last calibration | `last_calibration_<unit>` | timestamp | diagnostic |
| Calibration threshold, empty | `<unit>_calibration_empty_v_per_cell` | V | diagnostic |
| Calibration threshold, full | `<unit>_calibration_full_v_per_cell` | V | diagnostic |
| Voltage-based SoC (uncertain) | `<unit>_voltage_soc_pct` | % | diagnostic |
| Voltage/coulomb mismatch | `<unit>_voltage_soc_mismatch` | — | `binary_sensor`, problem, diagnostic |
| Calibration jump | `<unit>_last_calibration_residual_ah` | Ah | diagnostic |
| Current at calibration | `<unit>_last_calibration_current_a` | A | diagnostic |

**Series topology only:**

| Entity | Field | Unit | Notes |
|---|---|---|---|
| SoC Bank A | `soc_a_pct` | % | |
| SoC Bank B | `soc_b_pct` | % | |
| Voltage difference A/B | `voltage_delta_v` | V | diagnostic |
| Banks unbalanced | `imbalance_warning` | — | `binary_sensor`, problem, diagnostic |

**Control:**

| Entity | Topic | Range |
|---|---|---|
| Set SoC by hand (`number`) | `outstation/<id>/cmd/manual_soc` — parallel/single-bank, or `.../cmd/manual_soc/bank_a` and `.../bank_b` on a series pack | 0–100 % |

This is the way back after an outage that lost the coulomb count.

Discovery is cleaned up as the topology changes: object IDs that do not
belong to the current configuration are cleared with an empty retained
payload rather than left behind as ghost entities.

## Also available as a native Home Assistant integration

The same engine is also available as a **native Home Assistant integration**
under `integrations/homeassistant/`, installable through HACS — the same core
with a config flow instead of MQTT topics. See
[`integration/ha-integration-hacs-release.md`](../integration/ha-integration-hacs-release.md).

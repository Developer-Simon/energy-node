---
title: "Battery state of charge (SoC)"
component: service:battery_soc
---

# Battery state of charge (SoC)

This page covers what `services/battery_soc/` publishes and how you configure
it. [Device services](index.md#battery-state-of-charge) shows where the service
sits among the others.
[`battery-soc-how-it-works.md`](battery-soc-how-it-works.md) explains the
coulomb counting itself: the calibration math, the AC/DC efficiency handling
and the reasons behind them.

## At a glance

`services/battery_soc/` + `libs/battery_soc_core/` · `battery-soc.service` ·
`battery_soc_devices.json`

This is the only service that polls nothing. It subscribes to power and voltage
topics the other bridges already publish and computes the state of charge of
one or two LiFePO4 banks by coulomb counting. It recalibrates from the voltage
at both ends of the curve, applies an efficiency per converter and compensates
the measured cell voltage for the load.

The result is an estimate for monitoring. It does not replace a BMS. Optional
DC side topics take over from the AC measurements while they are fresh. The
service publishes the combined SoC and the SoC per bank, the net battery power,
problem sensors for stale inputs, time to full and to empty, and a `number`
entity to set the SoC by hand after an outage.

## Configuration

**System type.** `system_type` is `ac_coupled` (default) or `dc_only`. It only
changes which fields the dashboard form shows. `dc_only` hides the AC inputs,
their efficiencies and `dc_max_age_s`, and the service rejects a `dc_only`
entry that still has AC inputs.

**Inputs.** Charger power, inverter power and one voltage topic per bank, each
as a topic plus an optional JSON key, because a Trucki stick publishes a bare
number and a Shelly publishes an object. Optional DC side power topics take
over from the AC measurements while they are fresh (`dc_max_age_s`). When they
go stale, the service falls back to AC. A side with only a DC input uses it as
its only source and has no AC fallback.

Every power input has an `*_invert` switch. The same topic can be used in
several power inputs, typically one signed sensor for charging and the same
sensor inverted for discharging. Negative values count as zero on each side.
The DC inputs take `*_unit: "W"` or `"A"`. A current is converted to watts with
the pack voltage.

**Validity.** Each side needs a source (AC or DC), and bank A needs a voltage.
The service rejects any other configuration with an error code and keeps
running with the previous one. The dashboard's configuration page shows the
reason. A file without any charge or discharge source does not load.

**Topology.** `parallel` (both banks on one DC bus with one voltage) or
`series`. Set `bank_b_enabled: false` for a single bank. The entity list
follows the topology. A series pack also gets the SoC per bank, the voltage
difference between the banks and an imbalance warning. On a series pack,
`bank_a_voltage_measures` says whether the bank A sensor measures bank A alone
(`bank_a`, default) or the whole stack (`stack`). With `stack`, bank A is the
stack minus bank B.

**Calibration and efficiency.** Cell count and capacity per bank, the open
circuit volts per cell that count as empty and full, how far those thresholds
may relax at rest (`calibration_tolerance_v_per_cell`), how long a voltage has
to hold before calibration applies, the charger's AC→DC and the inverter's
DC→AC efficiency, and the charge efficiency of the cells.

## Entities

All sensor and binary sensor entities read from one JSON state topic,
`outstation/<id>/state`, through a `value_template`. The "Field" column is the
JSON key in that payload. Most of them have `entity_category: diagnostic`, as
noted in the tables. Unlike the diagnostic sensors of the other services, none
of them are disabled by default.

Always published:

| Entity | Field | Unit | Notes |
|---|---|---|---|
| SoC | `soc_combined_pct` | % | Combined. On a series pack this is the weaker bank |
| Net battery power | `net_power_w` | W | Positive means net charge |
| Inputs stale | `inputs_stale` | — | `binary_sensor`, problem, diagnostic |
| Time to full | `time_to_full_h` | h | diagnostic |
| Time to empty | `time_to_empty_h` | h | diagnostic |

**AC fallback active** (`ac_fallback_active`, `binary_sensor`, problem,
diagnostic) is only announced when a side has both an AC and a DC input.

Per unit, once for `pack` or once each for `bank_a` and `bank_b` in a series
topology (`<unit>` stands for the unit's name):

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

Series topology only:

| Entity | Field | Unit | Notes |
|---|---|---|---|
| SoC Bank A | `soc_a_pct` | % | |
| SoC Bank B | `soc_b_pct` | % | |
| Voltage difference A/B | `voltage_delta_v` | V | diagnostic |
| Banks unbalanced | `imbalance_warning` | — | `binary_sensor`, problem, diagnostic |

Control:

| Entity | Topic | Range |
|---|---|---|
| Set SoC by hand (`number`) | `outstation/<id>/cmd/manual_soc` for parallel or single bank, `.../cmd/manual_soc/bank_a` and `.../bank_b` on a series pack | 0–100 % |

Use it after an outage that lost the coulomb count.

When the topology changes, the service cleans up discovery. Object IDs that do
not belong to the current configuration are cleared with an empty retained
payload, so no orphaned entities stay behind.

## Also available as a native Home Assistant integration

The same engine is available as a native Home Assistant integration that you
can install through HACS. See [`ha/battery-soc.md`](../ha/battery-soc.md).

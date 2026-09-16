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

**Entities published:** combined SoC, net battery power, "inputs stale" and
"AC fallback active" as problem sensors, time to full and time to empty; then
per unit (pack, or bank A and bank B) voltage, estimated current, remaining
Ah, load-corrected cell voltage, last calibration timestamp, the two active
calibration thresholds, a voltage-based SoC estimate and a
voltage-versus-coulomb mismatch warning. Finally a `number` entity to **set
the SoC by hand** — one for the pack, or one per bank on a series pack —
which is the way back after an outage that lost the count.

Discovery is cleaned up as the topology changes: object IDs that do not
belong to the current configuration are cleared with an empty retained
payload rather than left behind as ghost entities.

## Also available as a native Home Assistant integration

The same engine is also available as a **native Home Assistant integration**
under `integrations/homeassistant/`, installable through HACS — the same core
with a config flow instead of MQTT topics. See
[`integration/ha-integration-hacs-release.md`](../integration/ha-integration-hacs-release.md).

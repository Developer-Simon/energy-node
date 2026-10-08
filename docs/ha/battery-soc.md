---
title: "Battery SoC integration for Home Assistant"
component: ha-integration
anchor_moves:
  requirements: battery-soc.html#install-via-hacs
  set-up: battery-soc-setup.html
  what-it-supplies: battery-soc-entities.html
  support-and-feedback: battery-soc-setup.html#support-and-feedback
---

<img class="page-icon" src="../images/ha/battery-soc.svg" alt="" width="72" height="72">

# Battery SoC (LiFePO4 coulomb-counting)

This Home Assistant custom integration estimates the state of charge of one or
two LiFePO4 battery banks from sensors you already have. It does not talk to a
BMS and needs no extra hardware. You point it at a charge power sensor, a
discharge power sensor and one voltage sensor per bank.

The same engine also runs on the node as the
[battery state of charge service](../services/battery-soc.md).

| `display: column` | `display: trajectory` |
|---|---|
| ![Battery SoC Lovelace card, column display](../images/ha/battery-soc/card-column.png) | ![Battery SoC Lovelace card, trajectory display](../images/ha/battery-soc/card-trajectory.png) |

> The result is an estimate for monitoring and diagnostics, not a safety
> function of a BMS. Do not use it for automatic shutdowns without independent
> protection, such as cell monitoring in the charger or BMS. The MQTT service
> carries the same warning.

## What it does

Each battery becomes one device with the state of charge, net power, time to
full or empty and a set of diagnostic values
([Entities & actions](battery-soc-entities.md)). A single bank and two banks in
parallel share one state of charge. Two banks in series get one per bank, and
the weakest bank becomes the combined value ([Setup](battery-soc-setup.md)).

The integration brings its own Lovelace card. It shows the stored energy and
time remaining as a column, or a ring with six hours of history and a six hour
projection ([Lovelace card](battery-soc-card.md)).

It also keeps track of its own calibrations and suggests better values for the
inverter efficiency and the internal resistance
([Calibration suggestions](battery-soc-suggestions.md)).

## How it works

1. **Coulomb counting.** The net battery power (charger power minus inverter
   power, AC or DC) is integrated over time in Ah. This is the main SoC
   source. It is accurate in the short term but drifts slowly because of
   measurement errors, efficiency and self-discharge.
2. **Voltage recalibration at the ends.** A LiFePO4 cell's voltage curve is
   almost flat between roughly 15 and 85 % SoC, so the voltage says nothing
   about the SoC in that range. It only moves noticeably near empty and near
   full. There the coulomb counter is reset to 0 % or 100 % once the load
   corrected voltage crosses a threshold and the current is low enough for the
   reading to count as a resting voltage.
3. **Load compensation.** Instead of a family of voltage curves per load
   current, the measured voltage is corrected by an offset that depends on the
   current (mΩ per cell) before it is compared with the resting voltage curve.
4. **Topology.** A single bank or two banks in parallel get one SoC, because
   parallel banks share one voltage and a split per bank would be made up. Two
   banks in series get a SoC per bank plus a combined figure for the weakest
   bank.

The [service documentation](../services/battery-soc-how-it-works.md) explains
the calculation in detail.

## Install via HACS

You need Home Assistant with HACS, power sensors for your charger and
inverter, and one voltage sensor per battery bank.

[![Open your Home Assistant instance and add this repository to HACS.](https://my.home-assistant.io/badges/hacs_repository.svg)](https://my.home-assistant.io/redirect/hacs_repository/?owner=Developer-Simon&repository=ha-battery-soc&category=integration)

The button opens HACS in your Home Assistant with the repository pre-filled. Or add it by hand:

1. Open **HACS** → **⋮** (top right) → **Custom repositories**.
2. Repository URL: `https://github.com/Developer-Simon/ha-battery-soc`
3. Category: **Integration** → **Add**.
4. Search for **Battery SoC** → **Download**.
5. **Restart Home Assistant.**

### Manual install

1. Download the latest release from [ha-battery-soc](https://github.com/Developer-Simon/ha-battery-soc/releases).
2. Extract it to `<config>/custom_components/battery_soc/`.
3. Restart Home Assistant.

Then continue with the [Setup](battery-soc-setup.md).

## License

MIT, see [LICENSE](https://github.com/Developer-Simon/ha-battery-soc/blob/main/LICENSE).

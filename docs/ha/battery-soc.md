---
title: "Battery SoC integration for Home Assistant"
component: ha-integration
---

<img class="page-icon" src="../images/ha/battery-soc.svg" alt="" width="72" height="72">

# Battery SoC (LiFePO4 coulomb-counting)

The same engine also runs on the node as the [battery state of charge service](../services/battery-soc.md).

This Home Assistant custom integration estimates the state of charge of one or
two LiFePO4 battery banks from sensors you already have. It does not talk to a
BMS and needs no extra hardware. You point it at a charge power sensor, a
discharge power sensor and one voltage sensor per bank.

> The result is an estimate for monitoring and diagnostics, not a safety
> function of a BMS. Do not use it for automatic shutdowns without independent
> protection, such as cell monitoring in the charger or BMS. The MQTT service
> carries the same warning.

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

## Requirements

- Home Assistant with HACS (Home Assistant Community Store)
- Power sensors for your charger and inverter
- One voltage sensor per battery bank

## Install via HACS

1. Open **HACS** → **⋮** (top right) → **Custom repositories**.
2. Repository URL: `https://github.com/Developer-Simon/ha-battery-soc`
3. Category: **Integration** → **Add**.
4. Search for **Battery SoC** → **Download**.
5. **Restart Home Assistant.**

### Manual install

1. Download the latest release from [ha-battery-soc](https://github.com/Developer-Simon/ha-battery-soc/releases).
2. Extract it to `<config>/custom_components/battery_soc/`.
3. Restart Home Assistant.

## Set up

**Settings → Devices & Services → Add Integration → "Battery SoC (LiFePO4 coulomb-counting)".**

The setup has four steps.

**Battery** (`user` step) asks for a name and the system type. In an
*AC-coupled system* the charger, the inverter or both are measured on the
mains (AC) side, optionally refined by DC measurements. That is typical for
grid connected home batteries. In a *DC-only system* all power or current
measurements sit on the battery's DC bus, which is typical for embedded
devices.

**Sources and bank A** (`sources_ac` / `sources_dc`) asks for the bank layout
(*Single bank*, *Two banks in parallel*, *Two banks in series (A + B)*), one or
more power sensors per side, bank A's voltage sensor and scale, capacity, cells
in series, chemistry and SoC curve. Charging and discharging each need at least
one sensor. DC inputs accept power (W, kW, mW) or current (A, mA). A current is
converted with the pack voltage. The unit is read from the sensor, so you do
not set it. Every power input has an **invert** switch. A single signed sensor,
for example an INA219 shunt, goes into both the charging and the discharging
input, inverted in one of them. Each side ignores negative values, so each
input only sees its own direction.

**Bank B** (`bank_b`, only with two banks) asks for capacity and cell count,
and for banks in series also a voltage sensor and scale. In series, bank A is
the upper bank. The stack runs from A+ to B-, and A- is connected to B+ (the
middle tap). The bank B sensor measures bank B alone (B+ to B-). The bank A
sensor measures either bank A alone (A+ to A-) or the whole stack (A+ to B-).
For the whole stack, choose *The whole stack (A+ to B-)* in this step. Bank A
is then calculated as the stack minus bank B.

**Advanced Battery Parameters** (`advanced` step) holds the empty and full
volts per cell, efficiencies, calibration tolerance and hold time, thresholds
for the voltage/coulomb mismatch and the imbalance, timeouts for stale inputs
and DC age, the internal resistance (mΩ per cell) and the fallback interval.
DC-only systems do not show the AC converter efficiencies or the DC age
timeout.

All of this can be changed later in the integration's **Configure** dialog,
including the system type and the bank layout.

Power sensors must report a unit. A reading without one, or with an
unsupported unit, is ignored and logged once as a warning.

## What it supplies

Each configured battery becomes one device. The main entities:

| Entity | Meaning |
|---|---|
| `sensor` SoC (`soc_combined`) | Main state of charge (%). In series, the weakest bank. |
| `sensor` Net battery power (`net_power`) | Charge (+) or discharge (−) power (W). |
| `sensor` Time to full / Time to empty | Projection at the current rate (h, diagnostic). |
| `binary_sensor` Inputs stale | A source sensor stopped updating. |
| `binary_sensor` AC fallback active | Running on AC power sensors because a DC sensor went stale. Only exists when one side has both an AC and a DC sensor. |
| `sensor` Voltage / Current / Remaining Ah / Load-corrected cell voltage | Per unit (pack, bank A, bank B), diagnostic. |
| `sensor` Calibration thresholds / Last calibration | When and at what voltage the counter was last reset. |
| `sensor` Voltage-based SoC (uncertain) and `binary_sensor` Voltage/coulomb mismatch | Plausibility check against the coulomb count. |
| Series only: `sensor` SoC Bank A/B, Voltage delta A/B, `binary_sensor` Banks imbalanced | |
| `number` Set manual SoC (per bank in series) | Writes a known SoC to anchor the counter. |

The integration comes with its own Lovelace card, `custom:battery-soc-card`,
and registers it with the frontend itself, so you do not need a resource entry
under **Settings → Dashboards → Resources**. The card has two displays: a
column (stored energy and time remaining) and a trajectory (a ring with six
hours of history and a six hour projection).

| `display: column` | `display: trajectory` |
|---|---|
| ![Battery SoC Lovelace card, column display](https://raw.githubusercontent.com/Developer-Simon/ha-battery-soc/main/docs/img/LovelaceColumn.png) | ![Battery SoC Lovelace card, trajectory display](https://raw.githubusercontent.com/Developer-Simon/ha-battery-soc/main/docs/img/LovelaceTrajectory.png) |

> The card only appears once the integration is set up as a device. It is
> registered with the frontend from `async_setup_entry`, so add a
> **Battery SoC** entry under *Settings → Devices & Services* first and then
> restart Home Assistant. Until then Lovelace reports *Custom element doesn't
> exist: battery-soc-card*. If it still fails after the restart, reload the
> browser with Ctrl+Shift+R to drop the cached dashboard.

```yaml
type: custom:battery-soc-card
display: trajectory          # column | trajectory
soc_entity: sensor.speicher_soc_combined
power_entity: sensor.speicher_net_power
capacity_kwh: 12.8
reserve_percent: 10          # 0 = no reserve
invert_power: false          # true if your meter reports discharge as positive
runtime_entity: sensor.speicher_time_to_empty   # optional, wins over the linear estimate
```

`soc_entity` is the only required option. Without a Recorder history for
`soc_entity` (Recorder disabled, or retention shorter than six hours) the
trajectory display only shows the projection. That is normal after a restart.

## Support and feedback

Report problems with the integration as an issue in
[ha-battery-soc](https://github.com/Developer-Simon/ha-battery-soc/issues). The
integration is developed in this repository under
`integrations/homeassistant/mirror/battery_soc/`. Pull requests belong in the
mirror, not here.

## License

MIT, see [LICENSE](https://github.com/Developer-Simon/ha-battery-soc/blob/main/LICENSE).

# Battery SoC — configuration, entities and actions

Reference for the Battery SoC integration once it is installed. For install
steps and the concept overview see the [README](../README.md).

## Configuration

**Settings → Devices & Services → Add Integration → “Battery SoC (LiFePO4
coulomb-counting)”.**

- **Battery** (`user` step): a name and the **system type**.
  - *AC-coupled system*: charger and/or inverter are measured on the mains
    (AC) side, optionally refined by DC measurements. Typical for
    grid-connected home batteries.
  - *DC-only system*: all power or current measurements sit on the
    battery's DC bus. Typical for embedded devices.
- **Sources and bank A** (`sources_ac` / `sources_dc`): the bank layout
  (*Single bank*, *Two banks in parallel*, *Two banks in series (A + B)*),
  one or more power sensors per side, bank A's voltage sensor and scale,
  capacity, cells in series, chemistry and SoC curve. Each side (charging,
  discharging) needs at least one sensor.
  - DC inputs accept **power (W, kW, mW) or current (A, mA)**. Current is
    converted with the pack voltage. Units are read from the sensor, so
    nothing has to be set.
  - Every power input has an **invert** switch. A single signed sensor (for
    example an INA219 shunt) goes into both the charging and the
    discharging input, inverted in one of them. Negative values are
    ignored on each side, so each input only sees its own direction.
- **Bank B** (`bank_b`, only with two banks): capacity and cell count, plus
  a voltage sensor and scale for banks in series.
  - In series, bank A is the upper bank. The stack runs from A+ to B-, and
    A- is connected to B+ (the middle tap).
  - The bank B sensor measures bank B alone (B+ to B-). The bank A sensor
    measures either bank A alone (A+ to A-) or the whole stack (A+ to B-).
    For the whole stack, choose *The whole stack (A+ to B-)* in this step,
    and bank A is calculated as the stack minus bank B.
- **Advanced Battery Parameters** (`advanced` step): empty/full volts per
  cell, efficiencies, calibration tolerance and hold time, voltage/coulomb
  mismatch thresholds, imbalance threshold, stale-input and DC-age
  timeouts, internal resistance (mΩ/cell), fallback interval. DC-only
  systems do not show the AC converter efficiencies or the DC-age timeout.

All of this can be changed later in the integration's **Configure** dialog,
including the system type and the bank layout.

Power sensors must report a unit. A reading without one, or with an
unsupported unit, is ignored and logged once as a warning.

## Entities

One device per configured battery. Highlights:

| Entity | Meaning |
|---|---|
| `sensor` **SoC** (`soc_combined`) | Primary state of charge (%). In series: the weakest bank. |
| `sensor` **Net battery power** (`net_power`) | Charge (+) / discharge (−) power (W). |
| `sensor` **Time to full / Time to empty** | Projection at the current rate (h, diagnostic). |
| `binary_sensor` **Inputs stale** | A source sensor stopped updating. |
| `binary_sensor` **AC fallback active** | Running on AC power sensors because a DC sensor went stale. Only exists when one side has both an AC and a DC sensor. |
| `sensor` **Voltage / Current / Remaining Ah / Load-corrected cell voltage** | Per unit (pack / bank A / bank B), diagnostic. |
| `sensor` **Calibration thresholds / Last calibration** | When and at what voltage the counter was last snapped. |
| `sensor` **Voltage-based SoC (uncertain)** + `binary_sensor` **Voltage/coulomb mismatch** | Sanity cross-check against the coulomb count. |
| series only: `sensor` **SoC Bank A/B**, **Voltage delta A/B**, `binary_sensor` **Banks imbalanced** | |
| `number` **Set manual SoC** (per bank in series) | Write a known SoC to anchor the counter. |

The full descriptor list lives in
[`battery_soc_core/entities.py`](../custom_components/battery_soc/battery_soc_core/entities.py).

## Action: `battery_soc.set_state_of_charge`

Anchor the coulomb counter to a known value — e.g. right after a full charge, or
from a shunt-based reference.

```yaml
action: battery_soc.set_state_of_charge
target:
  device_id: <your battery device>
data:
  state_of_charge: 100     # percent, 0–100
  # bank: a                # series topology only: which bank to anchor
```

The **SoC** sensor jumps to the value immediately. The `number` entities do the
same thing from the UI.

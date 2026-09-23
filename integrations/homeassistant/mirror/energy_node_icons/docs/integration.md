# Battery SoC — configuration, entities and actions

Reference for the Battery SoC integration once it is installed. For install
steps and the concept overview see the [README](../README.md).

## Configuration

**Settings → Devices & Services → Add Integration → “Battery SoC (LiFePO4
coulomb-counting)”.**

- **Battery Configuration** (`user` step): name, topology (parallel/series),
  the charger and inverter power sensors (AC and/or DC), a voltage sensor per
  bank with an optional scale factor, per-bank capacity (Ah) and cell count,
  whether Bank B is enabled, chemistry and SoC curve profile.
- **Advanced Battery Parameters** (`advanced` step): empty/full volts per cell,
  charger/inverter/charge efficiencies, calibration tolerance and hold time,
  voltage/coulomb mismatch warning thresholds, imbalance threshold, stale-input
  and DC-age timeouts, internal resistance (mΩ/cell), fallback interval.

All of these are editable afterwards via the integration's **Configure** dialog
(Power & Voltage Sources / Tunable Battery Parameters).

## Entities

One device per configured battery. Highlights:

| Entity | Meaning |
|---|---|
| `sensor` **SoC** (`soc_combined`) | Primary state of charge (%). In series: the weakest bank. |
| `sensor` **Net battery power** (`net_power`) | Charge (+) / discharge (−) power (W). |
| `sensor` **Time to full / Time to empty** | Projection at the current rate (h, diagnostic). |
| `binary_sensor` **Inputs stale** | A source sensor stopped updating. |
| `binary_sensor` **AC fallback active** | Running on AC power sensors because DC is unavailable. |
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

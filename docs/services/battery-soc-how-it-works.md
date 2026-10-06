---
title: "Battery state of charge: how it works"
component: service:battery_soc
redirect_from:
  - /knowledge/services/battery-soc-how-it-works.html
---

# Battery state of charge: how it works

This page explains what `services/battery_soc/battery_soc_mqtt.py` does, why it
computes the way it does and which setting affects what. It adds to the
general description in [`battery-soc.md`](battery-soc.md) and the installation
steps in [install/index.md](../install/index.md).

The result is an estimate for monitoring and diagnostics, not a BMS function.
Do not use it for automatic shutdowns without additional safeguards.

## 0. Architecture: core and adapter

The SoC math (coulomb counting, recalibration, load correction, the declarative
entity list) lives in the package `battery_soc_core`. It only uses the standard
library and knows nothing about the transport, so it needs neither `paho-mqtt`
nor `energy_node_common`. `battery_soc_mqtt.py` is a thin MQTT adapter. It maps
MQTT topics to `SocInputs`, calls `engine.tick()` and turns the result
(`outputs` dict and entity spec) into discovery configs and the `/state`
payload. Simulation mode exists only in the adapter. The core knows no
substitute values.

The `number` entity "Set manual SoC" (command topic
`outstation/<id>/cmd/manual_soc`, or per bank in a series configuration
`.../cmd/manual_soc/bank_a` and `.../bank_b`) sets the coulomb counter by hand
to a known state of charge. That helps when an installation rarely reaches the
calibration thresholds (§3).

The Home Assistant integration (§12a) uses the same core as a second adapter,
so the domain logic exists only once.

## 1. The installation and what can be measured

Two LiFePO4 banks are on the same DC bus. A MeanWell charger charges them and a
Lumentree inverter discharges them. These are the measurements:

| Quantity | Source | What's awkward about it |
|---|---|---|
| Charge power | Shelly Plug S `netz_meanwell` | Measures the grid side, not the DC bus |
| Inverter power | Shelly Plug S `netz_lumentree` | Also the grid side |
| Charge power DC (optional) | Trucki T2MG `DCPOWER` | Already on the bus, needs no efficiency |
| Inverter power DC (optional) | — | The Lumentree stick provides no DC value |
| Voltage bank A | Shelly Uni | Really per bank |
| Voltage bank B | Shelly Uni | Really per bank |

There is no current sensor per bank, so the bank current is always estimated
from power and voltage.

## 2. Coulomb counting as the basis

The SoC is an integrated charge counter (`coulomb_ah` per bank). Each cycle
(default 10 s) computes:

```
dc_charge_w    = max(0, charge_power)      * charger_ac_dc_efficiency
dc_discharge_w = max(0, inverter_power)    / inverter_dc_ac_efficiency
net_power_w    = dc_charge_w - dc_discharge_w      # positive = net charge
```

The efficiency is applied per converter and not once to the sum. Both Shellys
measure on the grid side, but the calculation happens on the DC bus. If only
the charger runs, the difference is a constant factor. If both run at the same
time, which is normal when there is ongoing consumption while the battery
recharges, the two losses work in opposite directions. A single factor applied
to the net power is then off by the largest possible amount. Example with
η = 0.9 per converter, 100 W charging and 50 W inverter:

| Calculation path | Result |
|---|---|
| Wrong: `(100 − 50) · 0.9` | 45.0 W |
| Right: `100·0.9 − 50/0.9` | 34.4 W |

The `max(0, …)` drops the small negative values a Shelly Plug S reports when
idle, which the division would otherwise make even larger.

### DC measurement takes precedence

If a `*_dc_power_topic` is configured and its last value is younger than
`dc_max_age_s` (default 60 s), it replaces the AC measurement of that side
without an efficiency factor, because the value is already on the DC bus.
Otherwise the AC path above applies, with efficiency. Each side decides this on
its own. Only the Trucki T2MG provides `DCPOWER`. The Lumentree stick has no DC
value.

The net DC power is split between the banks (`current_split_mode`:
`capacity_ratio` splits by nominal capacity, `equal` splits 50/50), converted to
a current per bank with that bank's own voltage and integrated:

```
delta_ah = current_a * dt_h * (charge_efficiency if charging, else 1.0)
```

`charge_efficiency` (0.98) is the coulombic loss inside the cell, which has
nothing to do with the converter efficiencies above. On discharge it is 1.0,
because everything that flows out is counted.

The split is only an assumption for the counter. Recalibration (§3) uses the
real bank voltage, so it corrects a wrong split as soon as one bank reaches an
end of its range.

### Protection against time jumps

An integration step longer than `MAX_TICK_HOURS` (1 h) or with a negative `dt`
is discarded (`dt_hours = 0`). This protects against NTP corrections, suspend
and a stalled scheduler. Without it, one time jump could move the SoC by any
amount.

## 3. Recalibration at the ends

LiFePO4 has such a flat voltage curve in the middle range (~15–85 % SoC) that
the voltage tells you nothing about the SoC there. It only changes noticeably
near empty and near full, and only there is the coulomb counter reset:

- Corrected cell voltage ≤ `empty_v_per_cell` (2.50 V) sets the counter to 0.
- Corrected cell voltage ≥ `full_v_per_cell` (3.55 V) sets the counter to the
  nominal capacity.

Both only apply after the condition has held without interruption for
`calibration_hold_s` (120 s), which filters out spikes. A value in between
resets both timers.

The counter drifts between two calibrations because of measurement errors,
efficiency and self-discharge, but it is corrected again at each end.

## 4. Load correction of the voltage

Under load the terminal voltage differs from the resting voltage. It is too
high while charging and too low while discharging. A plain comparison with
`full_v_per_cell` would calibrate a charging bank to 100 % far too early.
Instead of a family of voltage curves for every load current, the measured
voltage is corrected by an offset that depends on the current. That is
mathematically the same with far fewer parameters.

There are two ways to set the offset.

The default is the bin table `LOAD_OFFSET_TABLE_MV` (mV per cell per C-rate):

| C-rate up to | Offset |
|---|---|
| 0.05 | 5 mV |
| 0.2 | 25 mV |
| 0.5 | 60 mV |
| 1.0 | 120 mV |
| above | 200 mV |

The alternative is `internal_resistance_mohm_per_cell`. Left empty, the table
applies. When set, the offset is linear: `Offset = I · R`. The table is a
constant resistance anyway (25 mV/20 A = 1.25 mΩ, 60 mV/50 A = 1.20 mΩ,
120 mV/100 A = 1.20 mΩ), so a single value is the natural parameter. You can
measure it, and it is continuous, while the table jumps at the bin boundaries
(by 35 mV at exactly C = 0.2, which alone can trigger a calibration).

To measure it, compute
`(voltage under load − resting voltage) / current / cell count`. The value `0`
means "no load correction" and is different from "not set". At 0 A both ways
give the same value.

The corrected cell voltage is published as its own entity ("Load-corrected cell
voltage bank A/B"). The calibration decision is based on this number, and you
need it to tune the thresholds or the internal resistance.

## 5. Calibration telemetry and tuning suggestions

Every recalibration at an end (§3) records an event with:

- the time it happened (ISO timestamp),
- how far off the coulomb counter was (residual),
- how much charge and discharge the counter saw since the previous full
  calibration,
- the current at the moment of the calibration,
- whether the current stayed below the `full_taper_c_rate` threshold.

These events are the basis for tuning suggestions for
`inverter_dc_ac_efficiency` (the share of DC power that becomes AC) and,
indirectly, for checking the `empty_v_per_cell` threshold.

A full-end calibration is reliable when three conditions hold:

1. Corrected cell voltage ≥ `full_v_per_cell` (3.55 V).
2. Current ≤ `full_taper_c_rate` × pack capacity, so only the tail of the
   charge and not the bulk phase.
3. Both held without interruption for `calibration_hold_s` (120 s).

Between two such calibrations the pack was full at both ends, so the net charge
over the interval is zero. The coulomb counter still recorded `charged_ah` in
and `discharged_ah` out. That gives one equation:

```
a · charged_ah − b · discharged_ah = 0
```

`a` and `b` are unknown correction factors. With two unknowns and one equation,
only the ratio `a/b` can be determined, not the absolute values. Any system with
a single calibration anchor has this limit.

The charge side is measured directly on the DC bus (`charger_dc_power_topic`)
when configured, so `a := 1` is a reasonable normalisation. The discharge side
has no DC measurement, because the Lumentree stick provides none, and is
estimated from the grid side with `inverter_dc_ac_efficiency`. That efficiency
is the only free assumption, so the correction goes there:

```
inverter_dc_ac_efficiency_new = inverter_dc_ac_efficiency_old / (charged_ah / discharged_ah)
```

The algorithm only makes suggestions and changes nothing itself. The service
fails closed. You apply a suggestion yourself in the dashboard, as with
`publish_allowed_prefixes`. With fewer than five intervals there is no
suggestion at all, because a single cycle is not enough data.

The capacity cannot be estimated from full-to-full intervals. That needs a path
from full to empty. Until a 0 % calibration can be reached (see roadmap), the
algorithm reports this limit and makes no capacity suggestion, because it takes
different measurements and not tuning.

The quality of the thresholds is reported separately. It comes from the state
at the moment of calibration and not from the residual. A calibration at
0.08 C is suspicious no matter how small the residual was. If more than half of
the recent full calibrations happened too close to the tail current limit, a
finding is reported. That points to a `full_v_per_cell` threshold that is too
low or a tolerance that is too wide.

### Estimating the internal resistance from calibrations

The raw pack voltage (without load correction) at a calibration anchor,
together with the known anchor voltage and the current at that moment, gives an
estimate of the internal resistance. At the anchor, the true open circuit
voltage equals the configured threshold (`full_v_per_cell` or
`empty_v_per_cell`), whichever load correction model (bin table or
`internal_resistance_mohm_per_cell`) got it there. So:

```
delta_v_per_cell = raw_v_per_cell − anchor_v_per_cell
R_i = delta_v_per_cell / current_a · 1000     [mΩ/cell]
```

A simulation with realistic sensor noise (±3 mV per cell, the resolution limit
of a Shelly Uni) shows the difficulty. Full-side calibrations only happen in the
taper phase (`full_taper_c_rate` ≤ 0.05 C, so ≤ 10 A on a 200 Ah system). Small
currents in the denominator amplify the noise a lot, and single estimates
scatter by 50–90 % of the true value. Empty-side calibrations have no taper
condition, so their currents are larger and less noisy. `offset = I·R` holds
for both signs, unlike the direction dependent bin table, so both sides can be
combined into one estimate. That halves the scatter compared with the full side
alone. Events with `|current_a|` below 2.0 A are left out, because the
denominator is then too small to tell signal from noise, however many samples
there are.

## 7. Stale inputs

If nothing arrives on a configured topic for longer than `stale_input_s`
(120 s), the input counts as stale. Integration and calibration then stop.

Integration stops because a charger Shelly that failed at 500 W would otherwise
keep its last value and let the SoC rise without limit. Calibration stops
because the 120 s hold time could otherwise complete on stale data alone and
wrongly set the bank to 100 %.

The currents are still computed and published, because they are only shown and
are not state. Only the counter stops.

Only configured inputs count. An empty topic is intentional and not an error,
otherwise an installation with placeholder topics would show the warning all
the time. `stale_inputs` in the state payload names the affected inputs in
plain text, so you can see the reason without reading the log.

Staleness is checked per input group and not per topic. The charge power, for
example, is the group {DC topic, AC topic}, and it is only stale when none of
its topics is fresh. Otherwise every planned AC fallback (see above) would show
"input data stale", even though the charge power is known the whole time.

## 8. Single-bank installations

`bank_b_enabled: false` takes bank B out of the current split, the total SoC,
the imbalance and the stale check. Bank A gets the full current. All `bank_b_*`
values in the payload become `null` and show as "unknown" in Home Assistant.

The bank B discovery entries stay on purpose. `energy_node_common` has no way
to remove discovery again, so switching back and forth would leave orphaned HA
entities. "Unknown" is also more accurate than a made-up 50 %.

## 9. Input topics and JSON keys

Each of the four inputs is a pair of `*_topic` and `*_json_key`. The key is
optional:

| Payload | `json_key` | Result |
|---|---|---|
| `{"apower": 12.5}` | `apower` | 12.5 ✔ |
| `26.8` | empty | 26.8 ✔ (first number in the raw text) |
| `26.8` | `apower` | No value, because the payload is not an object |
| `{"id":0,"apower":12.5}` | empty | **0**, because the first number in the raw text is the `0` |

In the two bottom cases the service writes one warning line per topic and
error reason, with the topic, the expected key, the start of the payload and
the keys actually present. If the reason changes, it warns again. Without that
warning, the input would never update, go stale after 120 s and leave nothing
in the log.

The key path never falls back to the regex path. A configured key means that
key. Silently taking the first number in the raw text would give a wrong value
instead of a visible error. To avoid mistakes, the dashboard form suggests the
keys from the last payload, see the
[configuration editor](../dashboard/configuration.md).

All six `*_json_key` fields default to `""`, and they have to. The dashboard
stores a field that is empty or at its schema default as an omitted key
(`readNode()` in `config.page.js`), so "explicitly empty" and "never set" look
the same in the JSON. With a guessed manufacturer key as the default, you could
not select "no JSON key" in the UI, and a topic with a bare number as payload
(which is how the Trucki sticks publish every field) would be dropped without a
message. That happened while `charger_power_json_key` and
`inverter_power_json_key` defaulted to `apower`. Suggestions belong in the
`<datalist>` built from the real payload, not in the default.

## 10. Settings overview

All settings live in `services/battery_soc/battery_soc_devices.json`, with the
schema next to it, and can be edited as a form in the dashboard. Saving
triggers `outstation/battery_soc/config/reload`.

| Setting | Default | Effect |
|---|---|---|
| `charger_ac_dc_efficiency` | 0.9 | Share of the measured grid power that arrives on the DC bus (MeanWell 0.88–0.92) |
| `inverter_dc_ac_efficiency` | 0.9 | Share of the DC power that comes out on the grid side (Lumentree 0.90–0.93) |
| `charger_dc_power_topic` / `_json_key` | *(empty)* | Optional DC side charge power. Replaces the AC measurement without efficiency while fresh |
| `inverter_dc_power_topic` / `_json_key` | *(empty)* | Optional DC side inverter power, same principle |
| `dc_max_age_s` | 60 s | How long a DC measurement counts as fresh before AC applies again |
| `charge_efficiency` | 0.98 | Coulombic loss inside the cell, only when charging |
| `battery_chemistry` | `lifepo4` | Cell chemistry. Only LiFePO4 so far, and the calibration thresholds and the simulation curve assume it |
| `current_split_mode` | `capacity_ratio` | Split between the banks, or `equal` |
| `empty_v_per_cell` / `full_v_per_cell` | 2.50 / 3.55 | Calibration thresholds, load corrected |
| `calibration_hold_s` | 120 s | Hold time before a calibration |
| `internal_resistance_mohm_per_cell` | *(empty)* | Empty uses the bin table, a value uses linear I·R, `0` means no correction |
| `stale_input_s` | 120 s | When an input counts as stale |
| `bank_b_enabled` | `true` | `false` for single-bank installations |
| `imbalance_warn_v` | 0.5 V | Threshold of the imbalance warning (≈ 60 mV per cell with 8 cells) |
| `bank_*_cell_count` / `bank_*_capacity_ah` | 8 / 100 | Installation data, used for the C-rate and the split |
| `bank_*_voltage_scale` | 1.0 | Factor on the reported voltage (divider ratio at the Shelly Uni) |
| `simulation_*` | | Substitute power values in simulation mode when the real topics are silent (see below) |

## 11. Published entities

All entities read one JSON payload on `outstation/battery_soc/state` through
`value_template`, in the order of `entities()`. The payload also carries
`charger_power_source` and `inverter_power_source` (`"dc"` or `"ac"`), which
have no entity of their own:

| Entity | Unit | Meaning |
|---|---|---|
| SoC bank A / B / total | % | State of charge, `device_class: battery` |
| Voltage bank A / B | V | Raw measurement |
| Voltage difference A/B | V | Diagnostic for ageing and imbalance |
| Net battery power | W | DC side, after efficiencies |
| Last calibration A / B | timestamp | When the counter was last calibrated |
| Input data stale | ON/OFF | At least one configured input is silent |
| AC fallback active | ON/OFF | One side has a DC topic but currently uses the AC measurement |
| Current bank A / B (estimated) | A | From power and voltage, not measured |
| Remaining capacity bank A / B | Ah | Value of the coulomb counter |
| Load-corrected cell voltage A / B | V | The number behind the calibration decision |
| Banks imbalanced | ON/OFF | \|ΔU\| > `imbalance_warn_v` |
| Time to full / empty | h | Unfiltered, jumps with the measurement |
| Set manual SoC (bank A / B in series) | % | `number` entity that sets the coulomb counter directly (see §0) |

The time estimates come from an unfiltered Shelly measurement, and the service
does not smooth anything. They are fine as an order of magnitude, but the
displayed value jumps around. Below 10 W net power and with stale inputs they
are `null`.

## 12. Persistence, simulation, operation

**Persistence.** The coulomb counter, the charge balances and the calibration
events are stored in `state_file` (`/home/energynode/battery_soc/state.json`).
The service writes it at most every `state_save_interval_s` seconds (service
setting, default 300). It also writes right after a calibration, after a manual
state of charge, before a config reload and when it stops (SIGTERM, which is
also how the updater restarts it). Writes are atomic (temp file, fsync,
rename). Write and read errors are not fatal on purpose.

**Recovery after an unexpected stop.** If the file was not written on a clean
stop, the next start recovers. The retained `{base}/state` carries an unrounded
`recovery` snapshot. If it is newer than the file, the difference is added to
the counter. Otherwise, after 5 s, the last saved current is extrapolated over
half of the unsaved window (`min(gap, interval) / 2`). A manual state of charge
or a calibration in the meantime takes precedence.

**Simulation.** With the master/slave switch (`settings/simulation_active/set`)
the service computes with substitute values instead of real MQTT inputs, and
nothing counts as stale. Real measurements still win: a topic that delivers
fresh data is used, and only silent topics fall back to
`simulation_charger_power_w` and `simulation_inverter_power_w`. That lets you
check the efficiency calculation while the topics are silent. 200 W charging and
50 W inverter must give `200·0.9 − 50/0.9 = 124.4 W`.

In simulation the bank voltage follows a normalised LiFePO4 resting voltage
curve between `empty_v_per_cell` and `full_v_per_cell`, with a flat middle
section like a real cell. The same load offset that
`corrected_voltage_per_cell()` removes in real operation is added to the resting
voltage. The simulation is therefore a closed loop. Power fills the coulomb
counter, the voltage follows the state of charge, and at the ends the same
recalibration applies as in operation (§3).

**Device link.** The service is its own HA device, linked to `energy_node`
through `via_device`. The Trucki stick in turn points to this device through
`via_device`, so the two are not merged in discovery (see
[services/trucki.md](trucki.md)).

## 12a. Home Assistant integration

The custom integration `custom_components/battery_soc/` runs the same
`battery_soc_core` as a second adapter. Its config flow (`user` and `advanced`
steps) covers all ~40 parameters, with one config entry per battery
(deduplicated by name through `async_set_unique_id`). A
`DataUpdateCoordinator` runs `tick()` on every state change of a configured
source entity and also on a fallback timer, so time based calibration and
coulomb integration keep running while the inputs are idle. The state is stored
with `homeassistant.helpers.storage.Store`. After a reboot it starts at 50 %
SoC, and it does not import the state of the MQTT service. A
`battery_soc.set_state_of_charge` service and a `number` entity
"Set manual SoC" set the calibration anchors. The integration has no simulation
mode.

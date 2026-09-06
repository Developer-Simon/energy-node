---
title: "Battery State of Charge (SoC) — How It Works"
---

# Battery State of Charge (SoC) — How It Works

What `src/battery_soc/battery_soc_mqtt.py` does, why it computes the way it
does, and which setting turns which screw. Supplements the "Battery state of
charge" section in [device-services.md](../../device-services.md) and the
installation steps in [INSTALLATION.md](../../../INSTALLATION.md).

**Explicitly a monitoring/diagnostic estimate, not a BMS function.** Do not use
for automatic shutdowns without additional safeguards.

## 0. Architecture: Core + Adapter

The actual SoC math (coulomb counting, recalibration, load correction, the
declarative entity list) lives transport-free in the stdlib-only package
`battery_soc_core` — no `paho-mqtt`, no `energy_node_common`.
`battery_soc_mqtt.py` is now only the thin MQTT adapter: it maps MQTT topics to
`SocInputs`, calls `engine.tick()`, and translates the result (`outputs` dict +
entity spec) into discovery configs and the `/state` payload. Simulation mode
stays **exclusively** in the adapter — the core knows no substitute values.

A new `number` entity **"Set manual SoC"** (command topic
`outstation/<id>/cmd/manual_soc`, or per bank in a series configuration
`.../cmd/manual_soc/bank_a` and `.../bank_b` respectively) makes it possible to
set the coulomb counter by hand to a known state of charge — useful when an
installation rarely reaches the calibration thresholds (§3) during operation.

A future Home Assistant custom integration (separate plan) will use the same
core as a second adapter without duplicating the domain logic.

---

## 1. The Installation and What Can Actually Be Measured

Two LiFePO4 banks hang on the **same DC bus**. A MeanWell charger charges, a
Lumentree inverter discharges. What is measured:

| Quantity | Source | What's awkward about it |
|---|---|---|
| Charge power | Shelly Plug S `netz_meanwell` | measures the **grid side**, not the DC bus |
| Inverter power | Shelly Plug S `netz_lumentree` | also the **grid side** |
| Charge power DC (optional) | Trucki T2MG `DCPOWER` | already on the bus, no efficiency needed |
| Inverter power DC (optional) | — | the Lumentree stick provides no DC value |
| Voltage bank A | Shelly Uni | genuine per bank |
| Voltage bank B | Shelly Uni | genuine per bank |

There is **no** current sensor per bank. The bank current is always an
estimate — hence the detour via power and voltage.

## 2. Coulomb Counting as the Basis

The actual SoC is an integrated charge counter (`coulomb_ah` per bank). Per
cycle (default 10 s):

```
dc_charge_w    = max(0, charge_power)      * charger_ac_dc_efficiency
dc_discharge_w = max(0, inverter_power)    / inverter_dc_ac_efficiency
net_power_w    = dc_charge_w - dc_discharge_w      # positive = net charge
```

**Why per converter and not once on the sum:** both Shellys sit on the grid
side, but the calculation happens on the DC bus. If only the charger runs, the
difference is a constant factor. If **both run at the same time** — the normal
case with ongoing consumption and simultaneous recharging — the two losses act
in **opposite** directions, and a single factor applied to the already-formed
net power is off by the maximum. Example with η = 0.9 per converter, 100 W
charging and 50 W inverter:

| Calculation path | Result |
|---|---|
| wrong: `(100 − 50) · 0.9` | 45.0 W |
| right: `100·0.9 − 50/0.9` | 34.4 W |

The `max(0, …)` catches the small negative values that a Shelly Plug S reports
when idle and that would otherwise inflate the division even further.

### DC measurement takes precedence

If a `*_dc_power_topic` is configured and the last value is younger than
`dc_max_age_s` (default 60 s), it replaces the AC measurement of that side
**without** an efficiency factor — the value is already on the DC bus.
Otherwise the AC path described above applies, with efficiency. Each side
decides this independently: only the Trucki T2MG provides `DCPOWER`, the
Lumentree stick has no DC value.

The net DC power is split across the banks (`current_split_mode`:
`capacity_ratio` = in proportion to the nominal capacities, or `equal` =
50/50), converted to current per bank via the **bank's own** voltage, and
integrated:

```
delta_ah = current_a * dt_h * (charge_efficiency if charging, else 1.0)
```

`charge_efficiency` (0.98) is the **coulombic** loss *inside the cell* and
something entirely different from the converter efficiencies above. On
discharge it is 1.0: what flows out, flows out.

**Why a split is defensible at all:** it is only an assumption for the counter.
Recalibration (§3) uses the **real** bank voltage and thereby corrects even a
wrong split as soon as one bank reaches its end.

### Protection against time jumps

An integration step longer than `MAX_TICK_HOURS` (1 h) or with a negative `dt`
is discarded (`dt_hours = 0`). This guards against NTP corrections, suspend,
and a stalled scheduler — without the guard, a single jump would be an
arbitrarily large jump in SoC.

## 3. Recalibration at the Ends

LiFePO4 has such a flat voltage curve in the mid-range (~15–85 % SoC) that
voltage is **unusable** there as a SoC source. Only near empty and near full
does it change measurably. Exactly there — and only there — the coulomb counter
is reset:

- corrected cell voltage ≤ `empty_v_per_cell` (2.50 V) → counter to 0
- corrected cell voltage ≥ `full_v_per_cell` (3.55 V) → counter to nominal capacity

Both only after the condition has held **continuously** for
`calibration_hold_s` (120 s) (debounce against spikes). An intermediate value
resets both timers.

So the counter does drift between two calibrations (measurement error,
efficiency, self-discharge), but is caught again at each end.

## 4. Load Correction of the Voltage ("Voltage Curve Family")

Under load the terminal voltage is not the resting voltage: too high when
charging, too low when discharging. A naive comparison with `full_v_per_cell`
would calibrate a charging bank to 100 % far too early. Instead of a curve
family for every load current, the measured voltage is corrected by a
current-dependent offset — mathematically equivalent, but with far fewer
parameters.

Two ways, same meaning:

**Default — bin table `LOAD_OFFSET_TABLE_MV`** (mV/cell per C-rate):

| C-rate up to | Offset |
|---|---|
| 0.05 | 5 mV |
| 0.2 | 25 mV |
| 0.5 | 60 mV |
| 1.0 | 120 mV |
| above | 200 mV |

**Optional — `internal_resistance_mohm_per_cell`**: leave empty = table as
before; set = linear `Offset = I · R`. If you work through the table, it is a
constant resistance anyway (25 mV/20 A = 1.25 mΩ, 60 mV/50 A = 1.20 mΩ,
120 mV/100 A = 1.20 mΩ) — a single value is therefore the natural,
**measurable** parameterization and at the same time continuous, whereas the
table jumps at the bin boundaries (at exactly C = 0.2 by 35 mV, which on its
own can already trigger a calibration).

Measuring: `(voltage under load − resting voltage) / current / cell count`. The
value `0` explicitly means "no load correction" and is distinguishable from
"not set". At 0 A both ways yield the same value.

The corrected cell voltage is published as its own entity ("Load-corrected cell
voltage bank A/B"). **This is the number that makes the calibration decision** —
without it, neither the thresholds nor the internal resistance can be
meaningfully re-tuned.

## 5. Calibration Telemetry and Tuning Suggestions

Every recalibration at the ends (§3) records an event with:
- When it occurred (ISO timestamp)
- How far the coulomb counter was off (residual)
- How much charge and discharge the counter saw between this and the previous full calibration
- The current at the moment the counter was caught
- Whether the current held below the `full_taper_c_rate` threshold

These events form the basis for tuning suggestions on the two model parameters
that actually matter: the `inverter_dc_ac_efficiency` (what fraction of DC power
becomes AC), and indirectly the validation of the `empty_v_per_cell` threshold.

**The three conditions of a reliable full-end calibration are:**
1. Corrected cell voltage ≥ `full_v_per_cell` (3.55 V)
2. Current ≤ `full_taper_c_rate` × pack capacity (charging *tail* only, not bulk)
3. The condition held continuously for `calibration_hold_s` (120 s)

Between two such calibrations, the pack was at both ends full: net charge over
the interval is zero. Yet the coulomb counter recorded `charged_ah` in and
`discharged_ah` out. This yields an equation:

```
a · charged_ah − b · discharged_ah = 0
```

where `a` and `b` are unknown correction factors. Two unknowns, one equation — **only
their ratio `a/b` is determinable, not the absolute values.** This is not a weakness
of the measurement but a property of a system with only one calibration anchor.

The way out is a **reasoned normalization**: the charge side is measured directly on
the DC bus (`charger_dc_power_topic`) if configured, so `a := 1`. The discharge side
has no DC measurement — the Lumentree stick provides none — and is estimated via
`inverter_dc_ac_efficiency` from the grid side. That efficiency is the only free
assumption, so the correction belongs exactly where the assumption sits:

```
inverter_dc_ac_efficiency_new = inverter_dc_ac_efficiency_old / (charged_ah / discharged_ah)
```

**The algorithm proposes suggestions; it changes nothing.** The service stays
fail-closed. Applying a suggestion is a conscious action in the dashboard, the same
way `publish_allowed_prefixes` are handled. With fewer than five intervals to work
from, nothing is suggested at all — a single cycle is an anecdote, not a measurement
series.

The **capacity** cannot be estimated from full-to-full intervals; it requires a
full-to-empty path. Until a 0 % calibration is reachable (see roadmap), the
algorithm reports this constraint and offers no capacity suggestion — fixing it
requires different measurements, not tuning.

**The threshold quality is a separate finding** and does not come from the residual
but from the state when the counter was caught: a calibration at 0.08 C is
suspicious regardless of how small the residual was. A finding is reported if more
than half of the recent full calibrations happened too close to the tail-current limit,
pointing to a threshold that is too low (`full_v_per_cell`) or a tolerance that is
too wide.

## 7. Stale Inputs

If nothing arrives on a configured topic for longer than `stale_input_s`
(120 s), the input is considered stale. Then **integration and calibration are
frozen**:

- **Integration**, because a charger Shelly that failed at 500 W would
  otherwise let the SoC keep rising without limit — the last measured value
  would just stay put.
- **Calibration**, because the 120 s hold time could otherwise complete on pure
  stale data and wrongly set the bank to 100 %.

The currents are still computed and published (they are a display, not a
state); only the counter stops.

**Only configured inputs count.** A topic left empty is not a fault but a
statement — otherwise the warning would be permanently active in an
installation with placeholder topics. `stale_inputs` in the state payload names
the affected inputs in plain text, so the reason is visible without log access.

**"Stale" has, since the DC extension, applied per input group, not per
topic.** The charge power, for example, is the group {DC topic, AC topic}: it
is stale only when **none** of its topics is fresh. Otherwise "input data
stale" would be permanently shown on every scheduled AC fallback (see above),
even though the charge power is known throughout.

## 8. Single-Bank Installations

`bank_b_enabled: false` removes bank B from the current split, the total SoC,
the imbalance, and the stale check; bank A receives the full current. All
`bank_b_*` values in the payload become `null` and appear in Home Assistant as
"unknown".

Discovery is deliberately **not** omitted: `energy_node_common` knows no way to
remove discovery again, so toggling would leave orphaned HA entities behind.
"Unknown" is more honest than an invented 50 %.

## 9. Input Topics and JSON Keys

Each of the four inputs is a pair `*_topic` + `*_json_key`. The key is
optional:

| Payload | `json_key` | Result |
|---|---|---|
| `{"apower": 12.5}` | `apower` | 12.5 ✔ |
| `26.8` | empty | 26.8 ✔ (first number in the raw text) |
| `26.8` | `apower` | **no value** — payload is not an object |
| `{"id":0,"apower":12.5}` | empty | **0** — the first number in the raw text is the `0` |

The two bottom cases used to be completely silent: the input was never updated,
went "stale" after 120 s, and nothing appeared in the log. Now there is
**exactly one warning line per topic and error reason** with the topic, the
expected key, the start of the payload, and the keys actually present; if the
reason changes, it warns again.

There is **no fallback** from the key path to the regex path: whoever
configured a key wants that key. Silently taking the first number in the raw
text would again be a wrong value instead of a visible error. As a preventive
measure, the dashboard form suggests the keys from the last payload — see the
configuration editor in [dashboard.md](../../dashboard.md).

**All six `*_json_key` have `""` as their default**, and that is not
carelessness but a requirement: the dashboard stores a field that is empty *or
at its schema default* as an omitted key (`readNode()` in `config.page.js`).
"Explicitly empty" and "never set" are thus the same in the JSON. If a guessed
manufacturer name stood there, "no JSON key" would be **unreachable** via the
UI — and a topic with a bare numeric payload (which is how the Trucki sticks
publish every field) would be silently discarded. That was exactly the case as
long as `charger_power_json_key` and `inverter_power_json_key` were set to
`apower`. Suggestions belong in the `<datalist>` from the real payload, not in
the default.

## 10. Settings Overview

All in `src/battery_soc/battery_soc_devices.json`, schema alongside, editable as
a form in the dashboard. Saving triggers
`outstation/battery_soc/config/reload`.

| Setting | Default | Effect |
|---|---|---|
| `charger_ac_dc_efficiency` | 0.9 | fraction of the measured grid power that arrives on the DC bus (MeanWell 0.88–0.92) |
| `inverter_dc_ac_efficiency` | 0.9 | fraction of the DC power that reappears on the grid side (Lumentree 0.90–0.93) |
| `charger_dc_power_topic` / `_json_key` | *(empty)* | optional DC-side charge power, replaces the AC measurement without efficiency when fresh |
| `inverter_dc_power_topic` / `_json_key` | *(empty)* | optional DC-side inverter power, same principle |
| `dc_max_age_s` | 60 s | how long a DC measurement counts as fresh before AC applies again |
| `charge_efficiency` | 0.98 | coulombic loss **inside the cell**, only when charging |
| `battery_chemistry` | `lifepo4` | cell chemistry, so far only LiFePO4 — calibration thresholds and simulation curve assume it |
| `current_split_mode` | `capacity_ratio` | split across the banks, alternatively `equal` |
| `empty_v_per_cell` / `full_v_per_cell` | 2.50 / 3.55 | calibration thresholds (load-corrected!) |
| `calibration_hold_s` | 120 s | debounce before a calibration |
| `internal_resistance_mohm_per_cell` | *(empty)* | empty = bin table, set = linear I·R, `0` = no correction |
| `stale_input_s` | 120 s | when an input counts as stale |
| `bank_b_enabled` | `true` | `false` for single-bank installations |
| `imbalance_warn_v` | 0.5 V | threshold of the imbalance warning (≈ 60 mV/cell with 8 cells) |
| `bank_*_cell_count` / `bank_*_capacity_ah` | 8 / 100 | installation data, feed into C-rate and split |
| `bank_*_voltage_scale` | 1.0 | factor on the reported voltage (divider ratio at the Shelly Uni) |
| `simulation_*` | | power substitute values in simulation mode when the real topics are silent (see below) |

## 11. Published Entities

All from one JSON payload on `outstation/battery_soc/state` via
`value_template`. Order as in `entities()`. The payload additionally carries
`charger_power_source` / `inverter_power_source` (`"dc"` or `"ac"`), even if no
dedicated entity exists for it:

| Entity | Unit | Meaning |
|---|---|---|
| SoC bank A / B / total | % | state of charge, `device_class: battery` |
| Voltage bank A / B | V | raw measured |
| Voltage difference A/B | V | diagnostic for aging/imbalance |
| Net battery power | W | **DC side**, after efficiencies |
| Last calibration A / B | timestamp | when the counter was last caught |
| Input data stale | ON/OFF | at least one configured input is silent |
| AC fallback active | ON/OFF | one side has a DC topic but is currently using the AC measurement |
| Current bank A / B (estimated) | A | from power and voltage, not measured |
| Remaining capacity bank A / B | Ah | value of the coulomb counter |
| Load-corrected cell voltage A / B | V | the number behind the calibration decision |
| Banks imbalanced | ON/OFF | \|ΔU\| > `imbalance_warn_v` |
| Time to full / empty | h | unfiltered, jitters with the measurement |
| Set manual SoC (bank A / B in series) | % | `number` entity, sets the coulomb counter directly (see §0) |

The time estimates arise from an **unfiltered** Shelly measurement; the service
smooths nothing anywhere. As an order of magnitude they are useful, as a
displayed value they jitter. Below 10 W net power and with stale inputs they
return `null`.

## 12. Persistence, Simulation, Operation

- **Persistence:** the coulomb counter and calibration timestamps live in
  `state_file` (`/home/energynode/battery_soc/state.json`), so a restart does
  not discard the counter value. Write/read errors are deliberately not fatal.
- **Simulation:** via the master/slave switch
  (`settings/simulation_active/set`) the service computes with substitute values
  instead of real MQTT inputs; "stale" is then off by definition. Real
  measurements still take precedence here too: if a topic is delivering fresh
  data right now, it is used; only silent topics fall back to
  `simulation_charger_power_w` / `simulation_inverter_power_w`. Useful for
  checking the efficiency calculation when topics are silent: 200 W charging and
  50 W inverter must give `200·0.9 − 50/0.9 = 124.4 W`.

  The bank voltage is **no longer a constant** here but follows a normalized
  LiFePO4 resting-voltage curve between `empty_v_per_cell` and `full_v_per_cell`
  (flat middle section, just like the real cell) — the same load offset that
  `corrected_voltage_per_cell()` removes again in real operation is added to the
  resting voltage. This makes the simulation a closed loop: power fills the
  coulomb counter, the voltage follows the state of charge, and at the ends the
  same recalibration applies as in operation (§3).
- **Device coupling:** its own HA device, linked via `via_device` under
  `energy-node`. The Trucki stick in turn points via `via_device` to this
  device — deliberately no discovery merge (see the Trucki section in
  [device-services.md](../../device-services.md)).

## 12a. Home Assistant Integration

A native custom integration `custom_components/battery_soc/` runs the same
`battery_soc_core` as a second adapter. The config flow (`user` + `advanced`
steps) captures all ~40 parameters; there is one config entry per battery
(normalized to the name via `async_set_unique_id`). A `DataUpdateCoordinator`
recomputes the `tick()` on every state change of a configured source entity and
additionally on a fallback timer, so that time-based calibration and coulomb
integration keep running even with idle inputs. The state is stored in
`homeassistant.helpers.storage.Store` — a reboot starts at 50 % SoC, there is
no import of the MQTT service state. A `battery_soc.set_state_of_charge` service
plus a `number` entity "Set manual SoC" form the calibration anchors.
**Simulation mode is not carried over** — only real operation.

## 13. Tests

The pure SoC domain logic has its own test suite in the core package; the
adapter now only covers MQTT wiring, config loading, and golden-fixture parity:

```bash
.venv/bin/pytest src/battery_soc_core/tests -v   # coulomb counting, calibration, entity spec, …
.venv/bin/pytest src/battery_soc/tests -v        # MQTT adapter + parity with pre-refactor behavior
```

Three tests are structural rather than behavioral checks and are worth
mentioning:

- `test_schema_properties_match_dataclass_fields` — catches "schema key added,
  dataclass field forgotten" in both directions. With
  `additionalProperties: false` plus `BatteryConfig(**values)` this would
  otherwise only surface as a hard reload error on the Pi.
- `test_*_entities_do_not_reference_missing_payload_keys` — every `value_key`
  from `entity_specs()` must be present in the published `/state` payload. A
  typo in between is otherwise just a silent "unknown" entity in Home
  Assistant.
- `src/battery_soc/tests/test_core_parity.py` — drives a scenario matrix
  (parallel/series, fresh/stale, simulation, …) through the core + adapter and
  compares `/state` and discovery configs byte-for-byte with the
  `golden/*.json` fixtures recorded before the core extraction. A deviation
  there is a real behavior change, not a test artifact.

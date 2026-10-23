---
title: "Automations engine: how it works"
component: service:automation
---

# Automations engine: how it works

This page explains how `services/automation/automation_mqtt.py` evaluates a
rule, when it fires and what it refuses to do. It adds to the overview in
[`automations.md`](automations.md). The editor is described on
the [Automations page](../dashboard/automations.md).

## 1. The result of a rule

On every tick the service gives each rule exactly one result. The checks run in
this order, and the first one that applies wins:

| Result | Meaning |
|---|---|
| `disabled` | The rule is switched off. |
| `settling` | The service started less than `settling_seconds` ago (default 60 s). Retained MQTT messages are still arriving, so nothing fires yet. |
| `balance_stale` | A condition needs the energy balance, but the last one is older than `balance_max_age_s` (default 30 s) or none has arrived yet. |
| `conditions_not_met` | At least one condition is not met right now. |
| `fired` | The rule fired in this episode and its conditions still hold. It stays `fired` without publishing again. |
| `cooldown` | The conditions hold again, but the lockout after the last trigger (`cooldown_seconds`) is still running. |
| `hold_pending` | All conditions are met, but at least one has not held for its `hold_seconds` yet. |
| `fired`, `blocked`, `error` | The rule fires now. The result is `blocked` if an action was refused (for example by `publish_allowed_prefixes`) and `error` if an action failed. |

`balance_stale` only affects rules with a balance condition. Rules built on
`topic_value`, `entity_value`, `time_window` or `sun_window` keep running when
the dashboard stops publishing the balance.

The dashboard shows these results as the state between conditions and actions,
see the [Automations page](../dashboard/automations.md#reading-a-rule).

## 2. When a rule fires

All conditions of a rule are combined with AND. A rule fires once all of them
are met and have held long enough.

Hysteresis applies to `balance_threshold` conditions with `hysteresis`
above 0. With `comparison: "above"` the condition turns on when the value rises
above `threshold` and stays on until the value falls below
`threshold − hysteresis`. With `below` it is mirrored. A hysteresis of 0 means
a plain comparison.

Hold time (`hold_seconds`) starts when a condition becomes true. As soon as
the condition is false on a tick, the hold time starts over from zero.

Edge triggered. A rule fires once per episode, not on every tick. An
episode ends when one of its conditions is no longer met. Only then can the
rule fire again.

Cooldown (`cooldown_seconds`) starts each time the rule fires, also when an
action failed. A failing publish therefore does not repeat on every tick.

Repeat (`repeat_seconds`) runs the actions again while all conditions keep
holding, once `repeat_seconds` have passed since the last firing. A repeat
does not wait for the cooldown, because the cooldown only applies to starting
a new episode. Each repeat counts up `fire_count`, but only the first trigger of
an episode is written to the history. The repeat uses the same actions as the
first trigger, so a rule with a `toggle` action cannot repeat.

## 3. Restart and rejected rules

The service keeps no evaluation state across a restart.

- Nothing fires on boot. Hold times start counting after the restart, so a
  condition with a 5 minute hold fires 5 minutes after the restart at the
  earliest, even if it has been true for an hour.
- The cooldown does not survive a restart either. Together with the settling
  time, the first trigger comes after `max(hold_seconds, settling_seconds)` at
  the earliest.
- Rules that depend on the balance also wait for the first fresh balance.
- Scenario phases live in memory as well. After a restart every scenario
  starts `idle`. An `active` scenario from before the restart is not ended,
  so its `on_end` does not run.

An invalid `automation_rules.json` never stops the process. If the file is
invalid at start, the service runs with no rules, stays online and reports
`runtime_status: "rejected"` with the reason. If a saved change is invalid, the
service keeps running the previous rules and reports the same status. The
dashboard shows the reason after saving.

## 4. Values for publish actions

A `publish` action takes its payload from one of four sources
(`payload_source`):

| Source | Payload |
|---|---|
| `constant` | `payload` as entered |
| `balance` | a balance field (`field`) |
| `topic` | a number from another topic (`source_topic`, optionally `source_json_key`) |
| `toggle` | `payload_on` or `payload_off`, whichever is the opposite of the current state read from `source_topic` |

For `balance` and `topic` the service computes

```text
value = clamp(floor_to_step(base × scale + offset), min, max)
```

and formats the result with `decimals`. Rounding to `step` always rounds down.
A setpoint derived from the PV surplus must never end up above the surplus that
is actually there.

Example: charge a wallbox with the PV surplus minus 200 W, between 1380 W and
11 kW.

```json
{"type": "publish", "topic": "wallbox/charge_power/set",
 "payload_source": "balance", "field": "grid_export",
 "scale": 1, "offset": -200, "min": 1380, "max": 11000, "step": 1, "decimals": 0}
```

The service only publishes a topic and a value. It knows nothing about
entities. When the editor builds an action from an entity, it resolves the
command topic and the limits itself and stores them in the rule.

## 5. Safety rules

The service refuses a rule file that breaks any of these rules:

- A publish topic must not contain `+` or `#`, must have at least two segments
  and no empty segment.
- A publish topic must not start with `homeassistant/` (no fake discovery
  configs) or `$SYS/`.
- A publish topic must not be one of the dashboard's own topics: the balance
  `outstation/energy_node/energy/balance`, `outstation/energy_node/status/online`,
  `outstation/energy_node/state` and `outstation/energy_node/diagnostics`. The
  `settings/` topics below `outstation/energy_node/` stay allowed.
- A rule with a `publish` action needs `cooldown_seconds` of at least
  `publish_min_interval_s` (default 30 s), and every `hold_seconds` and a
  `repeat_seconds` above 0 in it must be at least `publish_min_interval_s` too.
- A rule with `repeat_seconds` above 0 must not contain a `toggle` action.
- At most 16 rules, each with 1 to 8 conditions and 1 to 8 actions. Rule IDs
  match `^[a-z0-9][a-z0-9_-]{0,63}$` and are unique. Scenario IDs follow the
  same pattern and share the namespace with the rule IDs.
- At most 8 scenarios. Two scenarios cannot regulate the same output topic.
- A `hold` in a scenario must not contain a `toggle` action.

At run time a `publish` action is also checked against
`publish_allowed_prefixes`. With an empty list, every publish is blocked and
the rule reports `blocked` with the reason. The same check applies to every
step of a regulator. A blocked step leaves the last sent value unchanged.

## 6. Details of the conditions

Weekdays. `weekdays` counts from `0` = Monday to `6` = Sunday. An empty
list means every day.

Windows across midnight. A `time_window` or `sun_window` belongs to the day
it starts on. Only that day counts for `weekdays`. Equal start and end times
give an empty window.

Sun windows. A `sun_window` ends at the next `to` event after its start, so
"sunset to sunrise" runs until sunrise the next morning. The offsets are whole
minutes up to ±240. The service computes sunrise and sunset itself (NOAA
method, accurate to about a minute). `settings.latitude` and
`settings.longitude` are only valid together. Without them, or on a day without
sunrise or sunset (polar day or night), the condition stays unmet.

Value templates. `entity_value` stores the entity's state topic and value
template in the rule, so the service never needs the device registry. The
service understands the same subset of Home Assistant templates as the
dashboard: plain fields, a nested path, the form `'ON' if … else 'OFF'` and the
filters `| int`, `| default(0)` and `| timestamp_local`. A filter is only
recognised with a space before the pipe. `{{ value_json.x|int }}` ignores the
filter, `{{ value_json.x | int }}` applies it. An empty template uses the raw
payload. The editor only offers entities whose template the service can read.

## 7. Rule history

Each rule can record its last triggers (`history_enabled`, off by default).
The service writes an entry once per episode when the rule fires, and also for
every test run, marked as a test. `settings.history_limit` (1 to 200, default
10) caps the entries per rule, the oldest drop out first.

With `settings.history_persist` set to `false` the history stays in memory
only. The dashboard still shows it, but nothing is written to the SD card, and
a restart clears it. The switch takes effect on the next reload without a
restart. Entries of deleted rules, or of rules whose history was switched off,
stay in the file.

## 8. Scenarios and the regulator

The fields of a scenario are listed in [`automations.md`](automations.md#scenarios).
This section explains its phases, the regulator and what happens at the edges.

The service keeps every scenario in one of four phases:

| Phase | Meaning |
|---|---|
| `disabled` | The scenario is switched off. Its conditions are not used to start it. |
| `idle` | Waiting for `when`. |
| `active` | Started. `on_start` has run, and `while` runs on every tick. |
| `fallback` | Active, but the regulator has no fresh balance or no base value. Only for `regulate`. |

The transitions are:

- `idle` to `active`: all `when` conditions hold for their hold time. Nothing
  starts while the service is settling or while a `when` condition needs a
  stale balance. `on_start` runs once, then the `while` part runs in the same
  tick.
- `active` to `idle`: the end checks run in this order. First, with
  `max_duration_s` set, once the scenario has been active that long
  (`reason = duration_reached`). Then, with `until`, as soon as all `until`
  conditions have held for their hold time. Without `until` and without
  `max_duration_s`, as soon as one `when` condition is no longer met, without
  waiting for a hold time. A stale balance does not end the scenario. A
  scenario with `max_duration_s` and no `until` ignores a `when` condition that
  drops, and ends only by time. `on_end` runs once in every case.
- Rearming: after a time end, the scenario does not start again at once. It
  waits until one tick finds a `when` condition unmet, and only then starts
  again on a later tick where `when` holds. Until then its reason is
  `rearm_pending`.
- `active` to `fallback`: the balance is stale, or the base value is missing.
  The `fallback` payload is published once when the phase changes, if one is
  set. A blocked fallback does not count as sent.
- `fallback` to `active`: the balance and the base value are back. The
  regulator carries on with its next step, and the `recovered` event is
  recorded.
- `enabled` set to `false` while active: `on_end` runs once, then the phase is
  `disabled`. Deleting an active scenario also runs `on_end` once. Enabling a
  disabled scenario returns it to `idle`.

A `hold` scenario runs its actions when it starts and then every
`repeat_seconds` while it is active. It has no fallback.

The regulator computes one step per tick, when its interval has passed:

```text
net    = balance.grid_import - balance.grid_export    (W, positive = import)
error  = net - target_w
if |error| <= deadband_w: no step
value  = base + sign * gain * error                   sign = +1 producer, -1 consumer
value  = last sent value +/- max_change_w            (only when max_change_w is set and a value was sent)
value  = floor(value / step) * step                  (only when step is set)
value  = clamp(value, min, max), rounded to decimals
publish only if value differs from the last sent value
```

For a producer, export (negative net) lowers the setpoint. For a consumer,
export raises it. The step rounds down, so a setpoint derived from a surplus
never exceeds that surplus. The clamp runs after the rounding to `step`, so
`min` and `max` apply as configured, even when they are not multiples of
`step`.

The `base` of the regulator comes from one of three sources:

- `entity`: the value of the entity's state topic, read with its value
  template. If it has no number, the regulator goes to `fallback`. It does not
  use the last value instead.
- `balance`: one balance field, named by `field`.
- `last`: the last value sent. Before the first step, the start value is `max`
  for a producer and `min` for a consumer.

A regulator steps at most once per interval. The interval is `interval_s` of
the regulator if set, otherwise `regulator_interval_s`, and it is never shorter
than `tick_interval_s`. The first step runs in the tick in which the scenario
starts. Inside the deadband nothing is sent. When the error is above five times
the deadband, the step is recorded as `jump` in the history.

The `scenario_<id>_output` sensor shows the last sent value while the scenario
is `active` or in `fallback`. The state document `<base>/state` has a
`scenarios` object per scenario ID, with `phase`, `since`, `net`, `error`,
`output`, `base`, `last_sent_at`, `reason` and the condition reports.

The settling time counts from the service start, so a reload does not start
it again. A reload keeps the phase, the last sent value and the timers of every
scenario with the same ID. An active scenario is not started again, so
`on_start` does not run twice. The regulator and hold settings come from the
reloaded file. The `on_end` actions are the ones saved when the scenario
started, so an edited `on_end` takes effect at the next start. When a scenario
is removed from the file, its discovery entries and its state topic are
cleared.

With `history_enabled` on a scenario, the history records `started`, `ended`,
`fallback`, `recovered` and `jump`, together with the actions that ran.

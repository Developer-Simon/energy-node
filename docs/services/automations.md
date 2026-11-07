---
title: "Automations engine"
component: service:automation
---

# Automations engine

`services/automation/` · `automation.service` · `automation_rules.json`

The rule engine runs as a process separate from the dashboard on purpose. The
dashboard edits the rule file but never publishes a rule's action itself, so
the web UI cannot switch a relay. The templates of the dashboard assistant live
only in the browser, not in the service, which sees only the finished rules and
scenarios.

[How it works](automations-how-it-works.md) explains when a rule fires, what
its result means, how publish values are computed and which topics a rule may
not write to.

The engine subscribes to the balance the dashboard publishes on
`outstation/energy_node/energy/balance` and to any MQTT topics the rules name.
It evaluates the rules on a tick (`tick_interval_s`, default 10 s). There can be
at most 16 rules, each with up to 8 conditions and 8 actions.

Two settings set the smallest intervals for commands and regulators:

| Setting | Default | Range | Meaning |
|---|---|---|---|
| `publish_min_interval_s` | 30 | 5 to 3600 | The smallest `cooldown_seconds`, `hold_seconds` and `repeat_seconds` of a rule that sends a command. |
| `regulator_interval_s` | 10 | 5 to 3600 | How often a regulator steps when its scenario sets no `interval_s` of its own. |

Both defaults suit a Raspberry Pi 1. Faster hardware can go lower. A regulator
never steps faster than `tick_interval_s`.

These are the condition types:

| Type | Matches on |
|---|---|
| `balance_threshold` | A balance field: `pv`, `grid_import`, `grid_export`, `load_total`, `base`, `wallbox`, `heat_pump`, `battery_charge`, `battery_discharge`, `battery_soc`, `autarkie`, `eigenverbrauch`, …, or `custom:<id>` for a custom category of the energy page |
| `topic_value` | A raw MQTT topic, optionally a JSON key inside it |
| `entity_value` | An entity, with a Home Assistant style value template |
| `time_window` | Start/end time and weekdays |
| `sun_window` | Sunrise or sunset, each with an offset in minutes, and weekdays |
| `chain_finding` | A finding of a measurement chain, see [Measurement chain conditions](#measurement-chain-conditions) |
| `chain_value` | The loss, efficiency or standby value of a measurement chain |

A `custom:<id>` field reads `categories.<id>` from the balance, in watts. If the
category is removed later, the field has no value and the condition stays unmet.
The same applies to a publish action that takes its value from the balance.

A window that crosses midnight belongs to the day it starts on: a `time_window`
from 22:00 to 06:00 on Fridays also holds at 03:00 on Saturday. A `time_window`
with the same start and end covers the whole day: 00:00 to 00:00 holds from one
midnight to the next, without a gap. `sun_window`
computes sunrise and sunset on the device itself from the location in
`settings.latitude` and `settings.longitude`, with no internet access. Without
a location, a `sun_window` condition stays unmet. The dashboard can fill the
location in from the browser, which works over HTTPS (for example through Caddy)
or on localhost.

Threshold conditions have `hysteresis` and `hold_seconds`, so a rule does not
flip back and forth around its threshold or fire on a single spike. A rule has
`cooldown_seconds`, a lockout after it has fired. `settling_seconds` keeps all
rules quiet for a while after a restart, and `balance_max_age_s` stops rules
from acting on an outdated balance.

`repeat_seconds` makes a rule send its actions again while all its conditions
keep holding. Only the first trigger of an episode goes into the rule history.
A rule with a publish action needs at least `publish_min_interval_s` here. A
rule with a `toggle` action cannot repeat, because every repeat would flip the
state back. The default `0` keeps the rule to one trigger per episode. See
[How it works](automations-how-it-works.md#2-when-a-rule-fires).

There are two action types. `publish` sends a constant payload, a balance
field, a value copied from another topic or a toggle, with scale, offset,
min/max, step and decimals. `notification` raises an info, warning or critical
notification.

## Measurement chain conditions

The dashboard publishes the state of every measurement chain on
`outstation/energy_node/energy/chains`, retained. Two condition types read it:
`chain_finding` for the findings of a chain and `chain_value` for its loss,
efficiency and standby values. Both work in rules, in `when` and in `until` of
a scenario.

The service reads the last payload and does not query the chain itself. A
payload older than 60 seconds counts as stale. A condition on a stale, missing
or unknown chain is unmet, so a rule waits instead of acting on an old value.

`chain_finding` fields:

| Field | Meaning |
|---|---|
| `chain_id` | The chain, lower case letters, digits and `_`. |
| `kind` | `any`, `drift`, `frozen`, `contradiction`, `sign` or `unit`. The findings are explained in [Diagnosis](../dashboard/efficiency.md#diagnosis). |
| `points` | Optional list of entity IDs. The finding matches when its first or its second point is one of them. Empty matches every point. |
| `min_age_s` | Optional, 0 to 86400. The finding must have been active for this long. Default 0. |
| `once` | Optional, default `false`. The finding fires the condition only once, see below. |

`chain_value` fields:

| Field | Meaning |
|---|---|
| `chain_id` | The chain. |
| `field` | `loss_w` in W, `efficiency_pct` in percent or `standby_w` in W. |
| `comparison` | `above` or `below`. |
| `value` | The number to compare with. |
| `hold_seconds` | Optional, 0 to 86400. The comparison must hold this long before the condition counts. Default 0. |

A finding that is shown, for example a frozen reading, can start a rule:

```json
{ "type": "chain_finding", "chain_id": "keller", "kind": "frozen", "points": ["meter_power"], "min_age_s": 900, "once": true }
```

A chain whose standby stays above 10 W for two hours:

```json
{ "type": "chain_value", "chain_id": "keller", "field": "standby_w", "comparison": "above", "value": 10, "hold_seconds": 7200 }
```

With `once` set, the service remembers the findings that have already fired the
condition. Each finding has its own start time, so a finding fires once and a
later finding fires again. `repeat_seconds` never repeats a used finding. The
finding no longer counts as met, the episode ends, and the repeat does not run.
The memory of used findings lives in RAM only. A restart clears it, so a finding
that is still active when the service starts can fire the rule once more.

## Scenarios

A scenario covers a period instead of a single trigger. `when` starts it once
all its conditions hold, the optional `until` ends it, and `on_start` and
`on_end` run once at the start and at the end. While the scenario is active,
`while` acts on every tick. A file holds at most 8 scenarios. Scenario IDs share
the namespace with rule IDs.

`while.mode` is one of:

- `regulate`: a regulator steers one output topic, for example an inverter
  limit or a wallbox setpoint, so that the net grid power reaches a target.
- `hold`: the `actions` run when the scenario starts and then every
  `repeat_seconds` until it ends.
- `none`: the scenario only runs `on_start` and `on_end`.

An example scenario, with the empty fields left out:

```json
{
  "id": "nulleinspeisung",
  "name": "Zero feed-in",
  "enabled": true,
  "template": "zero_feed",
  "history_enabled": true,
  "when": { "conditions": [ { "type": "sun_window", "from": "sunrise", "to": "sunset" } ] },
  "on_start": { "actions": [ { "type": "notification", "severity": "info", "title": "Zero feed-in", "message": "started" } ] },
  "while": {
    "mode": "regulate",
    "regulator": {
      "output": { "topic": "solar/inverter/limit/set", "entity_id": "bkw_limit", "min": 50, "max": 800, "step": 10, "decimals": 0 },
      "direction": "producer",
      "target_w": 30,
      "deadband_w": 20,
      "gain": 0.8,
      "base": { "source": "entity", "entity_id": "bkw_power", "topic": "solar/inverter/state", "value_template": "{{ value_json.power }}" },
      "fallback": { "payload": "800" }
    }
  },
  "on_end": { "actions": [ { "type": "publish", "topic": "solar/inverter/limit/set", "payload_source": "constant", "payload": "800" } ] }
}
```

Scenario fields:

| Field | Meaning |
|---|---|
| `id`, `name`, `enabled` | ID as for rules, display name, and on or off. |
| `template` | A label for the wizard. It has no effect on the service. |
| `history_enabled` | Records the start, end, fallback, recovery and large regulator jumps. |
| `when` | 1 to 8 conditions of the same types as rules. All must hold to start the scenario. |
| `until` | Optional, 1 to 8 conditions. Without it, the scenario ends when a `when` condition is no longer met. With it, the scenario ends when all `until` conditions hold. |
| `max_duration_s` | Optional, 10 to 86400 seconds. After this time the scenario ends, even while `when` still holds. It starts again only after `when` has been unmet once. Without `until`, this time is the only end condition. Missing means no time limit. |
| `on_start`, `on_end` | 0 to 8 actions each, of the same types as rule actions. |
| `while.regulator` | Required for `regulate`. See below. |
| `while.actions`, `while.repeat_seconds` | For `hold`: 1 to 8 actions and a repeat interval above 0. |

Regulator fields:

| Field | Meaning |
|---|---|
| `output.topic`, `output.entity_id` | The topic that receives the setpoint and the entity it belongs to. The topic must match `publish_allowed_prefixes`. Two scenarios cannot regulate the same topic. |
| `output.min`, `output.max` | The limits. `min` must be below `max`. |
| `output.step`, `output.decimals` | Optional rounding step and number of decimals. |
| `direction` | `producer` for an inverter, where export lowers the setpoint, or `consumer` for a wallbox or heater, where export raises it. |
| `target_w` | The wanted net grid power in W. Default 0. |
| `deadband_w` | No step while the error stays inside this band. Default 20, 0 or more. |
| `gain` | From 0.1 to 1, default 0.8. |
| `max_change_w` | Optional. The largest change per step, above 0. |
| `base` | Where the regulator reads its current value from: an entity, a balance field or the last sent value. |
| `interval_s` | Optional, 5 to 3600 s. Without it, `regulator_interval_s` applies. |
| `fallback.payload` | Required for `producer`, optional for `consumer`. Sent when the balance is stale or the base value is missing. |
| `efficiency.chain_id` | Optional. The chain whose best efficiency range the setpoint keeps to. Without the key the option is off. |

The efficiency option keeps the setpoint inside the best range of a chain. The
range is the chain's `best_down` for a `consumer` and its `best_up` for a
`producer`. The shaping runs on the computed setpoint, before `max_change_w`
and the rounding to `step`:

- Above the upper end, the setpoint becomes the upper end.
- Inside the range, the setpoint stays as it is.
- Below the lower end but at least half of it, the setpoint becomes the lower end.
- Below half of the lower end, the setpoint becomes `output.min`.

Without a usable range, the regulator works without a limit. This happens when
the chain has not learned a range yet (`no_range`), is missing from the payload
(`chain_missing`), its payload is stale (`chains_stale`) or the chains topic has
never arrived (`chains_unknown`). The scenario state reports the reason in
`efficiency`. The option is meant for setpoints in W, as the help text of the
dashboard says. The service does not check the unit.

The regulator and the state machine are described in
[How it works, section 8](automations-how-it-works.md#8-scenarios-and-the-regulator).

Each scenario publishes its state to `<base>/scenario/<id>/state`, retained.
`<base>` is the service's own topic, `outstation/<service_id>`. The discovery
adds two sensors per scenario to the automation device:

| Sensor | Value | Unit |
|---|---|---|
| `scenario_<id>_phase` | `disabled`, `idle`, `active` or `fallback`. A diagnostic entity. | |
| `scenario_<id>_output` | The last sent setpoint. Only for scenarios with `while.mode` set to `regulate`. | W |

`publish_allowed_prefixes` lists the topic prefixes a rule may write to. The
Python service fails closed, so with an empty list no rule publishes anything.
The dashboard fills in the prefixes when a rule is saved, but the service
enforces them.

Rule history (`history_enabled` per rule, `history_limit`, `history_persist`)
records the last triggers per rule so the dashboard can show why something
switched.

## Chain templates

The dashboard assistant has two templates for measurement chains. The browser
builds the rule or the scenario from the answers, and the service runs it like
any other. The first point of the chain is the plug. Its switch is preselected,
and the other points of the chain are the ones the watchdog observes.

### Restart a device when its reading hangs

The scenario starts when a `frozen` finding on one of the other points has
lasted for the chosen time and the plug draws at least the chosen load. The
finding is `once`, so it fires the scenario once. The plug switches off at the
start and on again at the end, after the pause.

| Question | Default | Range | Stored as |
|---|---|---|---|
| How long may the reading hang? | 15 min | 15 to 120 min, steps of 5 | `min_age_s` of the finding, in seconds |
| From which load? | 20 W | 5 to 500 W, steps of 5 | `entity_value` of the plug, above this value |
| How long off? | 30 s | 10 to 300 s, steps of 10 | `max_duration_s` |
| How often again? | 6 h | 1 to 24 h | `cooldown_s` |

A notice before the switch off is off by default. The history is on by default.

### Switch off standby

The plug switches off when the chain has drawn more than the chosen standby
power for the chosen time.

| Question | Default | Range | Stored as |
|---|---|---|---|
| From which standby? | 10 W | 5, 10 or 20 W, the slider goes from 0 to 100 W | `standby_w` of a `chain_value`, above this value |
| How long standby? | 2 h | 1 to 24 h | `hold_seconds` of that condition |
| When to switch on again? | At a set time | At a set time or by hand | see below |

**At a set time** is the default. It makes a scenario that switches the plug off
at the start and on again when the time window opens. The time window is one
hour long, from 07:00 to 08:00 on every day by default, and the days can be
chosen. **By hand** makes a rule with a cooldown of one hour that only switches
the plug off. Nothing switches it on again, so the plug stays off until you
switch it on yourself. The history is on by default.

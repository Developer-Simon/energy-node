---
title: "Automations engine"
component: service:automation
---

# Automations engine

`services/automation/` · `automation.service` · `automation_rules.json`

The rule engine runs as a process separate from the dashboard on purpose. The
dashboard edits the rule file but never publishes a rule's action itself, so
the web UI cannot switch a relay.

The engine subscribes to the balance the dashboard publishes on
`outstation/energy_node/energy/balance` and to any MQTT topics the rules name.
It evaluates the rules on a tick (`tick_interval_s`, default 10 s). There can be
at most 16 rules, each with up to 8 conditions and 8 actions.

These are the condition types:

| Type | Matches on |
|---|---|
| `balance_threshold` | A balance field: `pv`, `grid_import`, `grid_export`, `load_total`, `base`, `wallbox`, `heat_pump`, `battery_charge`, `battery_discharge`, `battery_soc`, `autarkie`, `eigenverbrauch`, …, or `custom:<id>` for a custom category of the energy page |
| `topic_value` | A raw MQTT topic, optionally a JSON key inside it |
| `entity_value` | An entity, with a Home Assistant style value template |
| `time_window` | Start/end time and weekdays |
| `sun_window` | Sunrise or sunset, each with an offset in minutes, and weekdays |

A `custom:<id>` field reads `categories.<id>` from the balance, in watts. If the
category is removed later, the field has no value and the condition stays unmet.
The same applies to a publish action that takes its value from the balance.

A window that crosses midnight belongs to the day it starts on: a `time_window`
from 22:00 to 06:00 on Fridays also holds at 03:00 on Saturday. `sun_window`
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

There are two action types. `publish` sends a constant payload, a balance
field, a value copied from another topic or a toggle, with scale, offset,
min/max, step and decimals. `notification` raises an info, warning or critical
notification.

`publish_allowed_prefixes` lists the topic prefixes a rule may write to. The
Python service fails closed, so with an empty list no rule publishes anything.
The dashboard fills in the prefixes when a rule is saved, but the service
enforces them.

Rule history (`history_limit`, `history_persist`) records the last triggers per
rule so the dashboard can show why something switched.

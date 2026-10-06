---
title: "Automations engine"
component: service:automation
---

# Automations engine

`services/automation/` · `automation.service` · `automation_rules.json`

The rule engine. It is a **separate process from the dashboard on purpose**: the
dashboard edits the rule file and never publishes a rule's action itself, so a
read-only web UI cannot switch a relay.

It subscribes to the balance the dashboard publishes on
`outstation/energy_node/energy/balance`, plus any MQTT topics the rules name, and
evaluates on a tick (`tick_interval_s`, default 10 s). At most 16 rules, each
with up to 8 conditions and 8 actions.

**Conditions:**

| Type | Matches on |
|---|---|
| `balance_threshold` | A balance field — `pv`, `grid_import`, `grid_export`, `load_total`, `base`, `wallbox`, `heat_pump`, `battery_charge`, `battery_discharge`, `battery_soc`, `autarkie`, `eigenverbrauch`, … |
| `topic_value` | A raw MQTT topic, optionally a JSON key inside it |
| `entity_value` | An entity, with a Home Assistant style value template |
| `time_window` | Start/end time and weekdays |
| `sun_window` | Sunrise or sunset, each with an offset in minutes, and weekdays |

A window that crosses midnight belongs to the day it starts on: a `time_window`
from 22:00 to 06:00 on Fridays also holds at 03:00 on Saturday. `sun_window`
computes sunrise and sunset on the device itself from the location in
`settings.latitude` and `settings.longitude`, with no internet access. Without
a location, a `sun_window` condition stays unmet. The dashboard can fill the
location in from the browser, which works over HTTPS (for example through Caddy)
or on localhost.

Threshold conditions carry `hysteresis` and `hold_seconds`, so a rule does not
chatter around its threshold or fire on a single spike, and a rule carries
`cooldown_seconds` as a lockout after it has fired. `settling_seconds` keeps
everything quiet for a while after a restart, and `balance_max_age_s` stops
rules from acting on a balance that has gone stale.

**Actions** are `publish` (constant payload, a balance field, a value copied
from another topic, or a toggle — with scale, offset, min/max, step and
decimals) and `notification` (info, warning or critical).

**Safety.** `publish_allowed_prefixes` is the list of topic prefixes a rule may
write to, and the Python service is **fail-closed**: an empty list means no rule
publishes anywhere. The dashboard fills the prefixes in when a rule is saved,
but the enforcement lives in the service.

Rule history (`history_limit`, `history_persist`) records the last triggers per
rule so the dashboard can show why something switched.

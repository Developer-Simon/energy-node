---
title: "Automations"
---

# Automations

![Automation rules](../images/dashboard-automation.png)

This page edits the scenarios and single rules of the separate
`automation_mqtt.py` service. The two tabs, **Scenarios** and **Single rules**,
show the same saved document. A single rule reads from left to right:
conditions, then state, then actions. A condition can be a threshold on a
balance field (with hysteresis and hold time), a raw MQTT topic value, a Home
Assistant style entity template or a time window. An action publishes to a
topic or raises a notification.

Besides the built-in balance fields, a threshold can watch any custom category
from the energy page. They are listed under **Custom categories** in the field
selection. A rule whose category was removed shows it as an unknown category
and its condition stays unmet until you pick another field.

The header shows whether the automation service is reachable. In the
screenshot it is offline because the smoke test harness does not start it, so
the rules are drawn from their saved state without live values. The dashboard
never runs rules itself. It only edits the JSON, and the Python service
evaluates the rules and publishes. See
[Device services → Automations](../services/automations.md).

## Choosing devices

The editors offer devices in role groups: inverter, wallbox, heat pump, grid and
other. A field shows as many device chips as fit in one row, with the selected
device first. Each chip shows the icon of its role in the colour of that role
and the current value of the entity. **Show all (n)** opens the device picker
with every matching device and its current value. The picker has a search field
and role filter chips. A click on a device selects it, and a double click or
Enter takes it at once. Picking a wallbox or heat pump as the output switches
the regulator to **Follow consumer**, picking an inverter switches it back.

The balance field and the entity of a condition use the same picker. The
selected field or entity shows as a chip with its role icon, **Change** opens
the picker.

## Scenarios

A scenario has three phases. **Start** holds the conditions that begin it,
**While** is what happens during the scenario, and **End** holds the actions
that run when it stops. The usual use is a regulator that keeps the grid
balance near a target by adjusting one output, for example an inverter.

### The scenario card

A collapsed card shows the scenario icon, its name, a short status line, a
status pill and an on/off switch. While the start conditions are not yet met,
a thin bar under the name shows how close the first balance threshold is to
its limit, with the current value and the limit in W.

A click on the card expands it. The icon tile collapses it again, outside the
editor a click on the name does the same. Expanding the card shows the figures,
the chart and the phase bar. The status
pill shows the current state of the scenario:

| Pill | Meaning |
|---|---|
| Off | The scenario is switched off. |
| Waiting | At least one start condition is not met yet. |
| Ready | All start conditions are met and the hold time runs. The line below tells how long until the start. |
| Running since (time) | The start conditions hold and the scenario is active. |
| Regulating since (time) | The scenario regulates the grid to its target. |
| Fallback value | The regulator cannot calculate and sends its fallback value. The line below names the reason, either a stale balance or a missing measurement. |
| Service offline | The automation service does not answer. |
| State unknown | The service has not reported this scenario yet. |

For a regulating scenario the card shows three figures. **Grid now** is the
current grid balance against the target, with the tolerance band around it.
**Output** is the value the regulator sends, with its minimum and maximum.
**Fed in today** is the energy measured at the grid meter since midnight.

The chart plots the grid balance and the output over the selected period. The
periods are **30 min**, **6 h** and **Today**, which starts at midnight. The
shaded band marks the tolerance around the target. The grid and output
series come from the history kept in this browser, which the history recorder
fills, and the chart adds the newest values while the page is open. The
dashboard also samples these two series on the server once a minute for the
history exchange with other devices. The chart on this page does not read
that buffer.

The phase bar shows Start, While and End side by side, each with its condition
or action in short text. The phase that is running is highlighted. The While
phase is highlighted while the scenario runs or falls back, and the End phase
for one minute after it has stopped.

### Editing a scenario

The pencil button on an expanded card opens the editor. It has three phases.

- **Start** lists the start conditions and the actions that run at the start.
  Each phase takes the same condition and action rows as a single rule.
- **While** selects what happens during the scenario: **Regulate**, **Hold**
  or **Nothing**. Changing the mode discards the content of the previous mode.
- **End** runs the end actions. With **Own end condition** switched off, the
  scenario ends as soon as its start conditions are no longer met. With the
  switch on, its own conditions decide when it ends.

For **Regulate** the form asks for the following values. Each field has a help
text next to it.

- **Target in W**, the grid balance to hold, positive for import and negative
  for export.
- **Deadband in W**, the range around the target where the output stays
  unchanged.
- **Output**, the device output the regulator writes to.
- **Direction**, either **Throttle inverter** or **Follow consumer**.
- **Measured value**, which can be a **Device** meter, the **Balance** or the
  **Last value**.

A band preview under the target shows the tolerance band and the current grid
balance. It states whether the grid is inside or outside the band, or that the
grid value is not known yet.

Under **Fine tuning** you find **Gain**, **Minimum in W**, **Maximum in W**,
**Step size in W**, **Max. change per step in W**, **Interval in s** and
**Fallback value**. An empty interval uses the regulator interval from the
settings (see Settings below).

Each fine tuning field shows its default as a grey placeholder. A default is
not saved, so the field stays empty and keeps following the default. A stepper
on an empty field starts from the default.

For **Hold** the phase sends its actions again every **Repeat every (s)**
seconds, 60 by default, while the scenario is active.

**Record history** at the bottom of the editor stores the scenario's own
history, as for single rules.

The start phase needs at least one condition, and each phase takes at most 8
conditions or actions. The dashboard checks the scenario before saving and names the problem in a message. Examples are a
regulator without an output, an output that another scenario already uses, a
minimum above the maximum, a gain outside 0.1 to 1, an inverter without a
fallback value, or a repeat that is shorter than the minimum interval for
commands.

## Single rules

The **Single rules** tab lists the rules as cards. A card shows the rule's
current state and, in the expanded view, its blocks.

### Rule editor

The rule editor has four parts.

- **When all of these apply** (Wenn) lists the conditions. All of them must hold
  for the rule to trigger. **Above** and **below** carry an up and a down arrow.
  A balance condition shows its **Hysteresis** directly in the card, a new one
  starts with 5 in the unit of its field (5 W for power).
- **Then** (Dann) lists the actions that run on a trigger.
- **Flow** (Ablauf) sets the **Cooldown after triggering (s)**, the time a rule
  waits after a trigger before it may trigger again, the **Run mode** and
  **Record history**.
- **Repeat** (Wiederholen) is the **Repeat** segment of the run mode. It shows
  **Every (s) while met**, which makes the rule send its actions again while its
  conditions keep holding. **Once** sends them once per trigger. Repeat must be
  at least the minimum interval for commands (see Settings). The repeat is off
  for a rule with a toggle action, because every repeat would flip the state back.

### Action cards

Each action is a card with the icon of its kind. **Switch device** under
**+ Action** opens the device picker, a switch starts on On and a setpoint
with the device's current value. An action that sends to the command topic of
a known device shows as **Switch** or **Setpoint** with that device, **MQTT
command** is left for topics without a device. The **Advanced** tiles appear
once **+ Action** is open. **Switch** sets a device to
On, Off or Toggle. **Setpoint** sends a value to an output, and the value can
come from a measurement. **Notify** raises a notification with a level, a title
and a text. **MQTT command** publishes a payload to a topic. The head of a
setpoint card takes the colour of its device's role.

### Reading a rule

The state between conditions and actions shows the rule's current result, for
example **Waiting for hold time**, **Cooldown running** or **Energy balance
stale**. While a hold time or cooldown runs, the remaining time is shown below
it, together with the last trigger and how often the rule has fired.
[How it works](../services/automations-how-it-works.md#1-the-result-of-a-rule)
lists every result. When the service is offline, the state always reads
**Service offline**, because none of the values can be trusted.

Each condition card shows one of four states:

| State | Meaning |
|---|---|
| no value | The topic has never carried a message, or the balance is stale. |
| threshold not reached | There is a value, but the condition is not met. |
| met, hold time left | The condition is met and waits for its hold time. |
| met | The condition is met and its hold time is over. |

The rule card also shows a timeline of the triggers in the last 6 hours. Above
the blocks, in the view and in the editor, each numeric condition has a bar with
its live value next to the threshold.

The live values come from the automation service, which evaluates every
`tick_interval_s` (default 10 s). They show what the rule last computed with,
not the newest value the dashboard has elsewhere. The header says how long ago
the service last reported. When it is offline, the page shows the last known
state.

Collapsed rule cards show a sparkline of their recent triggers. **Show full
history** opens the list of the last triggers of one rule, each marked as Triggered,
Blocked or Error. A trigger from a test carries a Test badge.

## Undo

A new scenario or rule starts switched on and records its history. Adding one
shows a bar above the list for 10 seconds, for
example **Scenario added**, with an **Undo** button. Undo removes the new item
again. The bar disappears on its own after the time runs out, and it does not
touch items that were already there.

## Limits

The dashboard holds at most 16 single rules and 8 scenarios. The add buttons
are disabled once the limit is reached.

## Settings

The settings under **Settings** on this page set the smallest intervals for
commands and regulators:

- **Minimum interval for commands (s)** (`publish_min_interval_s`, default 30)
  is the shortest cooldown, hold time and repeat interval that a rule with a
  command may have. The default suits a Raspberry Pi 1. Faster hardware can go
  lower.
- **Regulator interval (s)** (`regulator_interval_s`, default 10) is how often a
  regulator adjusts its output, when its scenario sets no interval of its own.
  A regulator never runs faster than the tick interval.

## Saving

**Save** writes all rules and scenarios and then waits up to 15 seconds for the
service to confirm. **applied** means the new rules are running. **rejected**
names the reason, and the service keeps running the previous rules. **Service
not responding** means no answer came in time, usually because the service is
offline. The rules are saved and take effect once it is back.

The rule file is also listed under [Configuration](configuration.md), with its
revisions and **Restore**. Use the raw JSON there if this page cannot express
something.

## Testing an action

**Test now** on an action runs exactly that action at once, without checking
conditions, hold time or cooldown. For a publish action the dashboard first
shows the topic and payload that would be sent and asks for confirmation. The
result appears as a message: published, blocked with the reason, or failed.

- The test always uses the saved rule. With unsaved changes, save first.
- A test works on a disabled rule too.
- A test does not start the cooldown and does not count as a trigger.
- `publish_allowed_prefixes` applies to tests just as to real triggers.
- The button needs the `automations` role and a reachable service.

## Rules that refer to entities

When you pick an entity, the editor copies its topic (and for conditions its
value template) into the rule. The service works with that copy and never looks
at the device registry. If the entity changes later, for example because a
device was discovered again with a new topic, the editor shows a note with the
current topic. Conditions offer **Apply** to take it over. Nothing is updated
automatically, so a rule never points at a different device without you
noticing. If the entity is no longer in the registry at all, the rule keeps
running with the saved topic.

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

## Assistant

**Assistant** in the bar above the list opens the assistant. It is the only way to add an entry. It builds a scenario or a rule
from a few questions, so you do not need to know the fields.

Below 900 px wide the assistant opens as a bottom sheet that slides up from the
bottom of the screen. Drag its handle down to close it, or tap the dark area
above it, or press Escape. A drag that stops short springs back. When the
assistant closes, the focus returns to the **Assistant** button.

The first page is the gallery. A search field matches the title, the text and
the keywords of each template. Category buttons narrow the list to **Charging**,
**Grid feed**, **Grid**, **Heating**, **Switching**, **Tariff**, **Time** or
**Warnings**. Templates that fit your
devices show as tiles on a desktop and as rows on a phone. Templates whose device or battery is missing are listed
under **More templates** with the reason. Under the tiles, **Without a
template** creates an empty rule or an empty scenario.

A template then asks its steps one at a time. Each step has a device choice,
a set of cards or tiles, a number or a time window. Options that do not apply,
for example the sun option without a known location, are greyed out with the
reason. The dots at the top show your place. **Back** returns to the previous
step or to the gallery, and **Next** is enabled once the step has an answer.

The step **How tight should it regulate?** of the zero feed template shows the
target as a window on the grid axis. Drag the window to move the target, tap
the axis to jump there, or use the arrow keys (5 W, with Shift 20 W). The three
cards **Careful**, **Balanced** and **Tight** set +60, +30 and +10 W. The
switch **Adjust tolerance** adds a handle on each edge of the window, so the
tolerance can be dragged between 5 and 60 W. Switched off, the tolerance is
20 W. The bars show how yesterday's grid power was spread around the target,
and three figures show how much would have been fed in and drawn yesterday and
how often the regulator would have adjusted per hour. Without history from
yesterday the figures are left out and only the axis and the window remain.

The last page is the summary. It lists the answers as plain sentences grouped
by phase: **When**, **Running**, **Ends**, **On start**, **On end** and
**Then**. Each row has a **change** button that jumps back to the step that
sets it. **Create** adds the entry.

The new entry lands in its list, expanded and in edit mode, switched on and
marked as new. It is not saved yet. The page counts it as an unsaved change
until you save the document. A bar shows **Scenario added** or **Rule added**
with an **Undo** button for 10 seconds, as in [Undo](#undo). If the list is
full, the summary shows a note and **Create** is disabled.

The templates are these. The phases describe what the entry does in words.

| Template | Category | Creates | Phases |
|---|---|---|---|
| Zero feed-in | Grid feed | A scenario that limits an inverter so that no power is fed in. The regulator keeps a small grid import of 10, 30 or 60 W as a buffer. The period is the sun window, always, or a time window with weekdays. The regulator measures the inverter's own reading, the house grid balance or the last setpoint sent. | When the period starts. Running: the setpoint follows the measured value. Ends: with the period, the limit returns to its maximum if the reset option is on. A notification at the start is optional. |
| Surplus charging | Charging | A scenario that regulates a wallbox with the surplus. It starts when feed-in stays above 1400, 2000 or 3000 W for 3 minutes. The vehicle must be plugged in if that option is on. | When feed-in stays above the chosen power. Running: the charging power follows the surplus, at least the minimum of the wallbox. Ends: with one of three end conditions that you choose, sunset, a charge level below a limit between 10 and 90 %, or grid import above 200 W for 5 minutes. On start the switch of the wallbox turns on, on end it turns off or the power drops back to the minimum. |
| Device on surplus | Switching | A scenario that switches a device on when feed-in is at least 1000, 1500 or 2500 W for 2 minutes. Optionally it stays on by repeating the switch command each minute, and it applies only on chosen weekdays. | When feed-in is high enough. Running: the device stays on while the condition holds, if repeat is on. Ends: after a fixed runtime of 10 to 600 minutes, at sunset, or when grid import stays above 100 W for 2 minutes. On end the device turns off. |
| Battery warning | Warnings | A rule that sends a notification when the charge level falls below 10, 20 or 30 %. You choose the severity info, warning or critical. It fires at most once an hour. | When the charge level falls below the limit. Then: a notification with the chosen severity appears. |
| Time switch | Time | A scenario that switches a device on for a daily time window, by default 11:00 to 15:00, optionally on chosen weekdays. | When the window starts. Ends: when the window ends, the device turns off. |
| Heat pump lift | Heating | A scenario that lifts a heat pump when feed-in stays above 1500, 2500 or 3500 W. It switches the SG Ready contact or raises the setpoint by 1 to 10 K, and it restores the old setpoint at the end. The minimum run time and the pause protect the compressor. | When feed-in stays above the chosen power for 5 minutes, and the battery is above its charge level if that option is on. Running: the heat pump is lifted. Ends: when feed-in stays below 40 % of the start power for 10 minutes. Then the contact goes off or the setpoint goes back, and the pause starts. |
| Low-tariff battery charging | Tariff | A scenario that charges the battery at a chosen power up to a target charge level. It runs in a fixed night window or while the price is below a threshold of 10, 15 or 20 ct/kWh. Discharging can be blocked during the charge when the battery has a discharge limit. | When the window starts or the price falls below the threshold. Running: the battery charges at the chosen power until the target. Ends: when the window ends or the price rises above the threshold, and the power returns to automatic. |
| Grid cap | Grid | A scenario that keeps the grid import just below a limit of 11, 16 or 25 kW by throttling a wallbox. | When the import stays above the limit minus 500 W for 10 seconds. Running: the wallbox follows the limit minus 500 W. Ends: when the import stays below the limit minus 1500 W for 10 minutes, and the wallbox returns to its maximum. |
| Control signal per §14a | Grid | A scenario that answers a control signal of the grid operator. Heat pumps and wallboxes go to a chosen power, 4.2 kW by default, and return afterwards. | When the input of the control box shows its on value. Running: each chosen device goes to the chosen power. Ends: every device goes back to its maximum. |
| Heating rod | Heating | A scenario that uses the surplus for a stepless heating rod. The rod takes the surplus and holds the grid export in a chosen band. | When the export stays above 100 W for one minute and the temperature is below the chosen maximum. Running: the rod follows the surplus. Ends: when the import stays above 100 W for 10 minutes, and the rod goes to its minimum power. |
| PV yield missing | Warnings | A rule that warns when an inverter reports too little power during the day. | When the reading stays below a limit of 5 to 200 W for 10 to 120 minutes, between one hour after sunrise and one hour before sunset. Then a notification appears, at most once in six hours. |

The energy templates need more than one kind of device. The assistant greys
out a template and names the missing part when a requirement is not met.

- **Heat pump lift** needs a device with the heat pump role on the energy page.
  Assign the role there if the device is not listed. The device needs an SG
  Ready switch, either its own switch or another switch you choose, or a
  writable setpoint. The control step decides which one is used. With the SG
  Ready contact, a contact step follows, and the device's own switch comes
  first and is preselected. The minimum run time and the pause of the heat
  pump are the **Run at least** and **Pause after the end** fields of its
  scenario. The start step shows yesterday's surplus with the start threshold.
- **Low-tariff battery charging** needs a battery with a writable charging
  power. The price comes either from fixed times or from a measured price in
  ct/kWh. A price in €/kWh is converted to ct/kWh. The block option needs a
  discharge limit on the battery. The charge step shows the energy and the
  duration from the capacity of the battery.
- **Grid cap** needs a wallbox with a writable power. The speed step sets the
  largest change per step, 1000 W for calm and 3000 W for fast.
- **Control signal per §14a** needs an input of the control box with a payload
  that means on, and at least one heat pump with a setpoint or one wallbox with
  a writable power. Devices with another unit than W or A are locked with the
  reason. A current setpoint is calculated for three phases at 230 V, so amps
  are the power in kW times 1000 divided by 690, rounded.
- **Heating rod** needs a device with a writable power in W or % and a
  temperature sensor. The maximum temperature is checked only at the start of
  a run. During the run the thermostat of the heating rod limits the
  temperature, the dashboard does not. Battery priority needs a state of
  charge.
- **PV yield missing** needs an inverter with its own power reading. The sun
  window needs a location in the settings.

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

Two more fields sit in the End phase. **Run at least (min)** keeps the scenario
running for this many minutes, even if its end condition is met sooner.
Switching the scenario off and the maximum run time still end it at once. Empty
means no minimum. **Pause after the end (min)** blocks a new start for this many
minutes after the scenario has ended, even if its start conditions hold again.
Empty means no pause.

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

The **Single rules** tab lists the rules. A collapsed row shows the rule's icon
tile, its name, the device path (the condition icons, an arrow and the action
icons), a status pill, the on/off switch and a chevron. A click on the row
expands it, and the icon tile collapses it again. The switch does not expand
the row.

The expanded card shows the live bars, the three columns **When all of these
apply**, **Then** and **Flow**, and the trigger strip. In the card head,
**Test** runs every action of the rule once, in order. **Edit** opens the
editor. A rule that is being edited shows neither button.

### Rule editor

**Edit** opens the rule as sentence rows on desktop. Each condition and each
action is one row, with its field labels, and the **Flow** column holds the
cooldown, the run mode and **Record history**.

The footer has **Delete rule** on the left, and **Discard** and **Done** on the
right. **Discard** restores the rule as it was when you pressed **Edit**, so
nothing you changed in the editor stays. **Done** keeps the changes and leaves
the editor. Changes are not saved to the device until you save the document.

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

Each condition shows one of three states, in a badge next to its line:

| State | Meaning |
|---|---|
| open | The condition is not met. The text beside the badge says why, for example that the threshold is not reached or that there is no value yet. |
| waiting | The condition is met and waits for its hold time. |
| met | The condition is met and its hold time is over. |

The expanded rule card also shows a trigger strip with the last four triggers.
Its time window fits those four triggers and is at least 30 minutes wide. The
strip needs **Record history** to be on for the rule. Otherwise the card says
that history is off for this rule. Above the blocks, each numeric condition has
a live bar with the caption **Now · field**. The value is on the left, the
threshold on the right, and a hatched part marks the hysteresis.

The live values come from the automation service, which evaluates every
`tick_interval_s` (default 10 s). They show what the rule last computed with,
not the newest value the dashboard has elsewhere. The header says how long ago
the service last reported. When it is offline, the page shows the last known
state.

With **Show history** on in the toolbar, each rule with a balance condition shows a
30 minute sparkline under its row. The line is the value of the balance field,
and the dashed line is the threshold. Grid export is shown as positive feed-in.
A rule without readings shows **No readings**. **Show full history** in the
expanded card opens the list of the last triggers of one rule, each marked as
Triggered, Blocked or Error. A trigger from a test carries a Test badge.

### On a phone

Below 900 px wide the toolbar has two rows. The first row switches between
**Scenarios** and **Single rules**. The second row has **Assistant**, **Save**
and a history icon button in place of the **Show history** switch. The button
is pressed while history is on.

A rule row shows its device path and a short status instead of the pill. The
short status is **met**, **not met**, **Cooldown**, **waiting**, **Error** or
**off**. The rule's switch stays at the right. When history is on, the sparkline
sits under the row.

The expanded card ends with a row of **Edit**, **Test** and a trash icon for
**Delete rule**. The editor head stays at the top while you scroll. It shows
the back tile, the name, the switch and one line with the status and the last
trigger. The footer with **Discard** and **Done** stays at the bottom. The
condition cards show their state badge, and the device and value fields take
the full width of the card.


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

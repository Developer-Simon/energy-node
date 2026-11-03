---
title: "Automations: Single rules"
---

# Automations: Single rules

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
  A time window and a sun window also take **days of the month**, 1 to 31, next to
  the weekdays. Without a chosen day the window applies every day. Like the
  weekdays, the day on which the window starts counts. A condition on an entity
  or a topic has the comparison **no new values**. It is met when the last
  message is older than the minutes you set. If no message has arrived since the
  service started, the time counts from the start of the service.
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

---
title: "Settings"
anchor_moves:
  general: settings.html#learning-features
  appearance: settings-appearance.html#appearance
  history: settings-history.html#history
  mqtt: settings-mqtt.html#mqtt
  tailscale: settings-wizards.html#tailscale
  tinytuya: settings-wizards.html#tinytuya
  versions: settings-versions.html#versions
  system: settings-system.html#system
  updating-from-the-dashboard: settings-versions.html#updating-from-the-dashboard
---

# Settings

Settings has twelve pages: Learning features, Appearance, History, MQTT, Tailscale,
TinyTuya, Network, Users, Backup, Versions, Checklist and System. Tailscale and TinyTuya only
appear when the matching service is installed.

On wide screens the pages are listed in a sidebar next to the content, each
with an icon and a short description. On narrow screens a button above the
content shows the current page. Tapping it opens the full list as a sheet from
the bottom. Swiping sideways on the button moves to the previous or next page.

## Learning features

The learning features turn the behaviour of your devices into suggestions for
chains and into learned values for rules. They never change a rule or a chain
on their own. Each feature has its own switch, and the page has four cards.

### Chains

**Suggestions** searches the power values for devices that measure the same
thing and suggests chains. **Learn values** learns the normal loss of each
measurement chain, see [Efficiency](efficiency.md#learning). When you switch it
off, the values learned so far stay, and the warnings rely on them alone.

### Automations

**Learn patterns** learns the switching times, the run times, the use per role
and the energy flows. When you switch it off, the learned values stay at their
last state, so a rule falls back to its default value after 7 days.

**Location missing** appears on this page while **Learn patterns** is on and no
location is set. Without a location the patterns have no sun times. **Enter location** opens the
automation settings. Learning goes on by calendar week meanwhile.

**Outdoor temperature** takes a temperature sensor. The node stores its value in
the daily profiles and uses it for the heating season, see
[Outdoor temperature and heating season](automations-rules.md#outdoor-temperature-and-heating-season).

Under **Fine-tuning**, two switches sit above **Exclude devices**. They only
appear while **Learn patterns** is on, and each works on its own.

- **Habits** turns the [suggestions](automations-rules.md#suggestions) for
  switching times, run times and devices that follow another device on or off.
- **Reliability** turns the reliability checks of the
  [rule check](automations-rules.md#rule-check) on or off: flapping, no effect,
  overridden, runs while offline, feedback and competition.

**Exclude devices** opens a picker for the devices that the learning leaves out.
**Allow all** removes every exclusion. The trigger and value checks of the
[rule check](automations-rules.md#rule-check) always run. They only read the
trigger history and write nothing new, so they have no switch.

### Recording

**Keep profiles** sets how many days of daily profiles stay, from 14 to 365
days. The default is 56 days. Chains and automations share one recording, and
each value is written only once. The card shows the memory in use and how much
is written per day.

**Seasonal data for a whole year** keeps the PV sun course for a year, also
when the profiles are kept for a shorter time. The rule check needs it to tell
from which calendar week a PV threshold is reached again. When it is off, the
sun course stays as long as the profiles. In the first year, while the summer
sun is still missing, a PV threshold is never reported as **Never triggers**.

**Record minute values** is on by default. The node keeps the power of each
device and the grid balance for every minute. The chain suggestions use this
record. After a restart they catch up on the minutes they missed, if the record
is kept on the drive or the system drive. Switch it off and the node stops
recording minutes.

The recording has three modes. **Always** records every minute. **Pause while
Home Assistant delivers** stops the own recording as long as the companion
offers minute values from Home Assistant, and starts it again within a minute
when the companion disconnects. It needs a paired companion, so the row stays
greyed out without one. **Off** records nothing. With an extra drive the node
records always. Without a drive but with a paired companion it pauses while
Home Assistant delivers. A mode you choose yourself is never changed.

**Reconcile with Home Assistant** is on by default and shows only with a paired
companion. Before the heavy nightly calculations the node asks Home Assistant
for the last days that its own recording does not cover without a gap. These
values live in memory for the calculation and are never written to a drive. A
result that rests on five minute values from Home Assistant counts as an
estimate at most.

**Where minute values are kept** chooses the place:

- **Memory** keeps the minutes in RAM, for up to 3 days on the economical level
  and up to 14 days on the full level. They are lost on restart, so the node
  cannot catch up from memory.
- **Extra drive** keeps the minutes on the drive. It is the recommended place
  once a drive is set up. The row shows the free space of the drive.
- **System drive** keeps the minutes on the SD card of the node. It writes
  about the amount shown on the row each day, so it is never the default.

Without an extra drive the node records in memory. With a drive, the node
records on the drive. When you set up a drive while the place is on automatic,
the recording moves there by itself, and the page shows the note **An extra
drive is set up. Minute values are now kept there.** with the button **Keep**.
A place you chose yourself is never changed.

If the drive fails, the recording goes on in memory and the row says **Drive
not available, still recording in memory**. When the drive comes back, the
missing minutes are added, so the day has no gap and no minute twice.

**Keep minute values** sets how many days stay on the drive or the system drive,
from 1 to 365 days. The default is 60 days. Minute values are large and the node
can learn them again, so they are not part of a backup. **Delete learned data**
removes them too.

**Delete learned data** asks for confirmation first. It deletes the day
profiles, the reaction times, the sums of the chain suggestions and the learned
loss values of the chains. It also deletes the minute values. Dismissed
suggestions, ignored hints and the counters of the rule check stay.

### New features

When an update switches a learning feature on for the first time, the page
shows a hint with the new features, and the menu entry and each new feature
carry the pill **newly on**. On a fresh installation the hint says that the node
learns from the start. **Keep all** confirms the features and removes the pills.
Single features can still be switched off on the page.

### Moved settings

The General page is gone. **Save running values** is now on the Storage page,
and **Stale values** is on the [MQTT](settings-mqtt.md) page.

## Other pages

| Page | Tabs | What you set there |
|---|---|---|
| [Learning features](#learning-features) | Chains, Automations, Recording | learning of chains and automations, how long daily profiles are kept, deleting the learned data |
| [Appearance](settings-appearance.md) | Appearance | default view, colour scheme, language and number format |
| [History](settings-history.md) | History | recording and retention in the browser, exchange between devices, recording of the energy statistics |
| [MQTT](settings-mqtt.md) | MQTT | broker connection, the "Energy Node" device in Home Assistant, the bridge to the main site |
| [Tailscale & TinyTuya](settings-wizards.md) | Tailscale, TinyTuya | the setup wizards for the tailnet and for Tuya devices |
| [Network](settings-network.md) | Network | the connection over LAN and Wi-Fi, scanning for Wi-Fi networks, connecting and forgetting networks |
| [Users](settings-users.md) | Me, Accounts, Sessions | sign-in and password, accounts and permissions, presets, guest access, Home Assistant users, signed-in sessions |
| [Backup](settings-backup.md) | Backup | backup archives of the settings, devices, users, statistics and history, automatic backups before updates and on a schedule, restore |
| [Versions & updates](settings-versions.md) | Versions | installed versions, changelogs, update check and the update from the dashboard |
| [Checklist](settings-checklist.md) | Checklist | the steps of the first-time setup and the tours of new features |
| [System](settings-system.md) | System | system actions (restart and shutdown), the central configuration file |

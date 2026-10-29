---
title: "Device dialog"
---

# Device dialog

Every device opens the same dialog, whether you come from the
[device list](devices.md), the overview, the diagnostics page or the
[device map](device-map.md). It shows one device: what it measures, what you
can switch, and where it comes from.

![Device dialog](../images/dashboard-device-modal.png)

## Header and side column

The header shows the device icon, its name, an online pill and manufacturer and
model. Click the icon to choose another one, see [Icon](#icon). The **⋯**
button opens the [more menu](#more-menu).

The side column on the left holds the device ID, the number of entities, the
devices it is linked to and the latest activity. A linked device is a chip:
click it to jump to that device. **Manage sub-devices** appears for devices that
measure power, see [sub-devices](device-map.md#sub-devices).

## Favourites

Each measurement and control tile has a star. Click it to mark the entity as a
favourite. A device can have up to three favourites. A fourth star is not
saved, the dialog shows **At most 3 favourites.** instead.

Changes are saved at once and a toast confirms them with **Favourites saved.**
and an **Undo** button. Undo puts the previous choice back.

The favourites row sits at the top of the dialog, above the tabs. It shows the
favourites as tiles with their live values. If fewer than three favourites are
set, the row fills the free places with measurements of the device on its own.
Those tiles carry an **Auto** label and their star stays empty. The compact
card of the device shows the same rows, so both always match.

Stars need the right to style devices, see [rights](#rights). Without it the
row still shows the values, but no stars.

## Icon

Click the device icon in the header to open the icon picker. It lists the icon
catalogue by category. Choosing an icon saves it at once with **Icon saved.**
and an **Undo** button.

A device without a chosen icon shows the suggested icon for its type, labelled
**(suggested)** in the picker. Choosing the suggestion stores no custom icon,
so the device keeps following its type.

## More menu

The **⋯** button, labelled **More actions**, offers:

- **Reload discovery** rebuilds the discovery of this device from the retained
  MQTT topics.
- **Ignore device** hides the device after a confirmation. Ignoring is
  reversible: the device is listed under the ignored devices and can be
  reactivated there.
- **Remove discovery** deletes the retained discovery topics of the device from
  the broker. A confirmation first lists exactly what will be deleted and which
  configuration still refers to the device. Only administrators can do that.

## Measurements and controls

Measurements are shown as tiles with value and unit. Values change live and
count smoothly to the new number. A tile with a warning frame belongs to an
entity that is stale or reports itself unavailable. **updated … ago** tells how
fresh the data is.

Controls sit below, next to each other when there is room: a slider for
numbers, a button for switches, a choice list for selections. Changing a
control sends the command at once. Under the control you see whether the device
confirmed it.

When the dialog is tall enough, measurements and controls share one tab.
Otherwise they get a tab each. Tabs without any entity are not shown. Devices
that expose configuration or diagnostic entities get a **Configuration &
diagnostics** tab: configuration entities can be changed, diagnostic values are
read-only.

Each tab carries a small counter with the number of entities inside.

## Analysis

The last tab is labelled **Analysis**. It is only shown to users with the
[device analysis right](#rights). It has two parts:

- **Warnings** explain what is wrong with the configuration of the device, for
  example an entity that stopped reporting, with the affected entities and a
  hint. Operational warnings, such as a device that is offline, appear above
  the tabs.
- **Entities** lists every entity of the device. Each row shows its name, a
  warning chip for stale entities and the number of commands sent to it. Click
  a row to open it. It shows the state topic, the last message, the commands
  with their results, and the discovery topic and source. The discovery payload
  sits behind **Show**. This is the quickest way to find out why Home Assistant
  sees a device differently from the dashboard.

![Analysis tab](../images/dashboard-device-modal-manage.png)

## Rights

Three rights shape the dialog. They are set per user on the users page.

- **Device analysis** (`view_device_analysis`, needs **Devices**) shows the
  Analysis tab with the raw data of the device: discovery payloads, the last
  MQTT message of each entity, the command history and the configuration
  warnings.
- **Style devices** (`edit_device_appearance`, needs **Devices**) allows the
  stars and the icon. Without it neither is offered, and the server refuses
  such a change.
- **Arrange the overview** (`edit_layout`) only controls the layout of the
  overview now. Favourites and icons of a device no longer depend on it.

Without **Device analysis** the server withholds the analysis data, not only the
tab. The device data leaves out discovery payloads and the last MQTT message of
each entity, which is reduced to its time. The command history is empty, and
only the operational warnings are returned. The activity list of the side
column still shows the latest values, but never topics.

When the users file is upgraded to the current version, the rights are carried
over once and automatically. Users with **Devices** receive **Device analysis**,
and users with **Arrange the overview** receive **Style devices**.

## On narrow screens

On a phone or in a narrow window the side column and the tab bar turn into one
list of sections. One section is open at a time, the others stay collapsed
with their entity count. The device data from the side column becomes the first
section.

![Device dialog on a narrow screen](../images/dashboard-device-modal-narrow.png)

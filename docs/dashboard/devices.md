---
title: "Devices"
anchor_moves:
  device-map: device-map.html
  energy-groups: energy.html#groups
  custom-categories: energy.html#custom-categories
  energy-balance-layer: device-map.html#roles-layer
  data-flow-layer: device-map.html#data-flow-layer
  data-flow-for-service-developers: ../developing/data-flow.html#12-data-flow-on-the-device-map
---

# Devices

Everything the node has seen through MQTT Discovery, grouped by device. The
button in the top right switches between two views.

![Device list, compact mode](../images/dashboard-devices.png)

The compact view shows the name, the availability and a few headline values.

![Device list, detailed mode](../images/dashboard-devices-detailed.png)

The detailed view shows every entity with its controls: sliders for `number`
entities, toggles for `switch` and live values for `sensor`.

Availability comes from each device's `availability_topic`. `Details` on a
device opens the raw discovery payload, the topics involved and whether the
discovery message was retained. That is usually the quickest way to find out
why Home Assistant does not see a device.

The default view is a setting, so a tablet on the wall can open straight into
the compact view.

The device dialog of a device that measures power lists its
[sub-devices](device-map.md#sub-devices). There you can create, edit and delete
a sub-device, which gives the measurement a name such as "Washing machine".

How the devices are wired and what they contribute to the energy balance is on
the [device map](device-map.md).

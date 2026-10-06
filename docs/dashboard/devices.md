---
title: "Devices and device map"
---

# Devices and device map

This page lists everything the node has seen through MQTT Discovery, grouped by
device. The button in the top right switches between two views.

The compact view shows the name, the availability and a few headline values:

![Device list, compact mode](../images/dashboard-devices.png)

The detailed view shows every entity of every device with its component type
and its controls: sliders for `number` entities, toggles for `switch` and live
values for `sensor`:

![Device list, detailed mode](../images/dashboard-devices-detailed.png)

Availability comes from each device's `availability_topic`. `Shelly Uni Bank B`
above is the fixture's device that is offline on purpose. `Details` on a device
opens a dialog with the raw discovery payload, the topics involved and whether
the discovery message was retained. That is usually the quickest way to find
out why Home Assistant does not see a device.

The default view is a setting, so a tablet on the wall can open straight into
the compact view.

## Device map

![Device map](../images/dashboard-device-map.png)

The device map is a free canvas with all devices on it. You place the nodes by
hand, they are coloured by health, and you can connect them with straight,
orthogonal or curved edges to record how the site is wired. That shows what the
automatic views cannot, such as which meter sits ahead of which sub-distribution
or what a device is physically attached to.

Positions and edges are stored on the server and versioned like the layout, so
you can roll back an accidental drag.

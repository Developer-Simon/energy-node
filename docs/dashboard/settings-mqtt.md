---
title: "Settings: MQTT"
---

# Settings: MQTT

The tab **MQTT** under *Settings*. Which topics the dashboard reads and writes
is described in [Data flows](../developing/data-flow.md).

## MQTT

![MQTT settings](../images/dashboard-settings-mqtt.png)

The broker connection comes with a live status table: connected or not,
address, where the configuration came from, connected since, last reconnect
and failed attempts.

**Publish energy values as a Home Assistant device** makes the dashboard
announce itself over MQTT Discovery as the device "Energy Node" with PV, grid,
battery and house load sensors. The computed balance then reaches Home
Assistant without an extra service.

**Use dashboard settings instead of the central configuration** decides which
values win. When it is off, the values from `/etc/energy-node/config.json` are
used unchanged. When it is on, the settings stored here replace them
completely. Fields are not merged.

Further down, the same tab configures the **Mosquitto bridge to the main site**
and can apply it and restart it.

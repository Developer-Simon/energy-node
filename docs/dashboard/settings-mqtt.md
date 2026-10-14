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

**Published metrics** selects which system metrics this device publishes to MQTT
as diagnostic entities, for example Wi-Fi signal or undervoltage. Remove a metric
from the selection to stop publishing it and add it again to resume. Changes are
saved immediately. Guests see the selection but cannot change it. Changing it
needs a login with configuration rights over HTTPS.

## Bridge to the main site

The bridge section of the tab starts with the same steps as listed here.

1. Run an MQTT broker on the main system, for example the Mosquitto add-on in
   Home Assistant.
2. Create a dedicated user for this bridge on the main system.
3. Put both systems into the same tailnet on the Tailscale page and use the main
   system's Tailscale IPv4 address as the bridge address.
4. Under bridge configuration, enter a name, the address and port of the main
   system, a unique client ID, and the user and password from step 2.
5. In the topic table, set for each topic whether it goes to the main system
   (out), comes from it (in) or runs both ways.
6. Save and check the preview, then apply. Applying writes the configuration to
   `/etc/mosquitto/conf.d/bridge.conf` and restarts Mosquitto, which briefly
   interrupts all MQTT connections of this device, including the dashboard's.
7. In the Mosquitto bridge status, check that the bridge connection shows as
   connected and that "Installed = saved" shows yes.

Guests see the bridge status and these steps. The configuration form stays hidden
for them.

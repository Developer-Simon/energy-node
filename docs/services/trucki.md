---
title: "Trucki stick"
component: service:trucki
---

# Trucki stick

`services/trucki/` · `trucki-http.service` · `trucki_devices.json`

Trucki sticks are WLAN sticks with community firmware for Lumentree and
Growatt inverters. One implementation covers all three variants: T2SG (zero
export), T2MG and T2HG (surplus charging). The bridge publishes every field it
finds, so a variant with an extra field needs no code change.

Like the Shelly bridge, it replaces the stick's own MQTT client, which
publishes on almost every value change. The bridge fetches two flat JSON
endpoints at an interval it controls. `/jsonlive` has the live measurements and
is fetched on the normal poll cycle. `/jsononce` has configuration and identity
data and is fetched on the diagnostic cycle and once on connect.

The published entities are grid voltage, battery voltage, temperature, AC power
and setpoint, power limit, zero export power and target, meter power, daily and
total energy, meter energy and readout time, and round trip times. Each has the
right unit, device class and state class.

The bridge is read-only. The firmware accepts writes over
`GET /?KEY=VALUE&…&save=true`, `GET /?reboot=true` and `GET /?zepc_enable=…`,
but none of that is implemented. Password fields from `/jsononce`
(`ADMIN_PASS`, `METER_PASS`, `MQTT_PASS`, `WIFIPASS`, `BEARER`) are dropped
before publishing.

If the stick and a battery bank are physically the same box, set `via_device`
to the `id` of the matching `battery_soc_devices.json` entry. Home Assistant
then shows the stick as connected through the battery device instead of
merging the two.

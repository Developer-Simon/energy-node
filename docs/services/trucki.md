---
title: "Trucki stick"
component: service:trucki
---

# Trucki stick

`services/trucki/` · `trucki-http.service` · `trucki_devices.json`

Community-firmware WLAN sticks on Lumentree/Growatt inverters. All three
variants are covered by one implementation — T2SG (zero export), T2MG and T2HG
(surplus charging) — and **not** by per-variant special cases: the bridge
publishes every field it finds, so a variant with an extra field needs no code
change.

Like the Shelly bridge, this exists to replace the stick's own MQTT client,
which publishes on practically every value change. Instead the bridge fetches
two flat JSON endpoints on an interval it controls:

- `/jsonlive` — live measurements, on the normal poll cycle
- `/jsononce` — configuration and identity data, on the diagnostic cycle and
  once on connect

**Entities published:** grid voltage, battery voltage, temperature, AC power
and setpoint, power limit, zero-export power and target, meter power, daily and
total energy, meter energy and readout time, round-trip times — each mapped to
the right unit, device class and state class.

Two deliberate limits:

- **Read-only.** The firmware accepts writes over `GET /?KEY=VALUE&…&save=true`,
  `GET /?reboot=true` and `GET /?zepc_enable=…`. None of it is implemented.
- **Password fields from `/jsononce` are never published** — `ADMIN_PASS`,
  `METER_PASS`, `MQTT_PASS`, `WIFIPASS`, `BEARER` are dropped before publishing.

If the stick and a battery bank are physically the same box, set `via_device`
to the `id` of the matching `battery_soc_devices.json` entry; Home Assistant
then shows the stick as connected via the battery device instead of merging
them into one.

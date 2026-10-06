---
title: "Tuya"
component: service:tuya_mqtt
---

# Tuya

`services/tuya_mqtt/` · `tuya.service` · `tuya_devices.json`

This service talks to Tuya devices locally through `tinytuya`, with the local
key, local IP and local protocol. It uses no cloud at runtime. A device needs
`device_id`, `local_key`, `ip` and the protocol `version` (default 3.3). You
get all of them by running `python3 -m tinytuya wizard` once.

`datapoints` maps a logical function to a Tuya data point number. The switch
is not always data point `1`, so the dashboard has a
[TinyTuya wizard](../dashboard/settings.md#tinytuya) that probes the device and
fills in the number.

Each device gets a `switch` entity with its state on `outstation/<id>/switch`
and commands on `outstation/<id>/set/switch`. A device that cannot be reached
publishes `UNKNOWN` instead of an outdated on or off.

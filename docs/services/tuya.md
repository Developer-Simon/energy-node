---
title: "Tuya"
component: service:tuya_mqtt
---

# Tuya

`services/tuya_mqtt/` · `tuya.service` · `tuya_devices.json`

Local Tuya devices through `tinytuya` — local key, local IP, local protocol,
no cloud at runtime. A device needs `device_id`, `local_key`, `ip` and the
protocol `version` (default 3.3), all of which come out of
`python3 -m tinytuya wizard` once.

`datapoints` maps a logical function to a Tuya data point number. The switch
data point is **not** reliably `1`, which is why the dashboard ships a
[TinyTuya wizard](../dashboard/settings.md#tinytuya) that probes
the device and fills the number in.

**Entities published:** a `switch` per device, state on
`outstation/<id>/switch`, commands on `outstation/<id>/set/switch`. A device
that cannot be reached publishes `UNKNOWN` rather than a stale on/off.

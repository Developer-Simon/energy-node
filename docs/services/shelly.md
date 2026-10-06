---
title: "Shelly"
component: service:shelly
---

# Shelly

`services/shelly/` · `shelly-rpc.service` · `shelly_devices.json`

This service polls Shelly devices over HTTP and publishes one state per cycle.
MQTT stays disabled on the devices on purpose. Their own MQTT client publishes
on almost every value change, which is too much traffic for a Pi 1 and a
bridged broker, and the device does not let you change the rate.

One implementation covers both generations. `generation: 1` polls `/status`,
`generation: 2` polls `/rpc`.

You declare each device's capabilities instead of having them detected, so the
service only asks for what the device has:

| Field | Meaning |
|---|---|
| `switch_channels` | Number of relay channels |
| `has_power` / `has_energy` | Reports instantaneous power or energy |
| `has_3phase` | Shelly 3EM, three phases including returned energy |
| `adc_channels` | Number of ADC inputs (Shelly Uni) |
| `has_temperature` / `has_humidity` | For example Shelly Plus H&T |
| `sleepy` / `offline_grace_s` | Battery device that sleeps between reports (for example H&T), see below |
| `auth_user` / `auth_password` | Optional HTTP basic auth |

The service publishes a `switch` per relay channel (command topic
`outstation/<id>/relay/<ch>/set`) and power and energy per channel. On a 3EM it
publishes them per phase, together with voltage and returned energy. It also
publishes one voltage sensor per ADC channel, temperature, humidity, and the
WLAN signal strength as a diagnostic entity that is disabled by default.

`shelly_presets.json` holds capability templates for Shelly 1, Plug S,
Plug S+, 3EM, Uni, 1PM Gen2, 1PM Gen3, Plus H&T and H&T Gen1. The configuration
UI applies them to a new entry. A preset only carries the technical fields
above, never identity (`id`, `name`, `host`) or credentials.

After editing `shelly_devices.json`, restart the service. The file is not
reloaded while it runs.

## Sleepy (battery) devices: H&T and friends

A Shelly H&T sleeps between reports and can only be reached over HTTP for a
short time after it wakes up. Polled on the same cycle as an always-on device
such as a Plug, almost every poll times out and the device would show as
offline nearly all the time.

Set `sleepy: true` on such a device (both H&T presets already do). Optionally
set `offline_grace_s` (default 3600 s) a little higher than the device's wake
or report interval. A sleepy device then only goes offline after
`offline_grace_s` has passed without a successful contact, not on the next
failed poll.

A Gen1 device can also call this service the moment it wakes up, so the next
poll does not depend on the shared cycle hitting its short wake window. This
wake webhook is off by default:

1. Enable it in the shelly service config with `webhook_enabled: true` and
   `webhook_port` (default `8082`).
2. Open the port in the firewall. The installer only does this when you tick
   **Shelly wake webhook in the firewall** on its configuration screen (step
   35, `scripts/bootstrap/35-ufw-shelly-webhook.sh`). It is off by default,
   and an update never turns it on. The switch only appears while the Shelly
   device service is selected, and deselecting Shelly also removes the rule.
   The installer opens the `webhook_port` from `config.json` if it is set,
   otherwise 8082. If you untick it later, the next run removes the rule.
   Without the installer, run `sudo ufw allow <port>/tcp` yourself. Once the
   option is on, the installer's diagnose checks the rule (an error that step
   35 repairs) and whether the service listens on the port (only a hint,
   because it does not listen until step 1 is done).
3. On the device, under *Settings → Actions*, add a **Report URL** or sensor
   report action pointing to
   `http://<node-host>:<webhook_port>/shelly/wake/<device id>`, where
   `<device id>` is the `id` field from `shelly_devices.json`.

The webhook only triggers an immediate poll of that one device. It does not
parse the device's report payload, and the normal HTTP poll still reads
temperature, humidity and battery. This avoids the device's own MQTT client,
because the Shelly firmware disables cloud access when MQTT is on. See the
module docstring in `shelly_rpc_mqtt.py`.

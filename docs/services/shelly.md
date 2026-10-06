---
title: "Shelly"
component: service:shelly
---

# Shelly

`services/shelly/` · `shelly-rpc.service` · `shelly_devices.json`

Polls Shelly devices over HTTP and publishes one state per cycle. MQTT is
deliberately left **disabled on the devices themselves**: their own MQTT client
publishes on nearly every value change, which is more traffic than a Pi 1 and a
bridged broker want, and the rate is not configurable on the device.

Both generations are covered by one implementation — `generation: 1` polls
`/status`, `generation: 2` polls `/rpc`.

Capabilities are declared per device rather than detected, which keeps the
polling honest about what it asks for:

| Field | Meaning |
|---|---|
| `switch_channels` | Number of relay channels |
| `has_power` / `has_energy` | Reports instantaneous power / energy |
| `has_3phase` | Shelly 3EM — three phases, incl. returned energy |
| `adc_channels` | Number of ADC inputs (Shelly Uni) |
| `has_temperature` / `has_humidity` | e.g. Shelly Plus H&T |
| `sleepy` / `offline_grace_s` | Battery device that sleeps between reports (e.g. H&T) — see below |
| `auth_user` / `auth_password` | Optional HTTP basic auth |

**Entities published:** a `switch` per relay channel (command topic
`outstation/<id>/relay/<ch>/set`); power and energy per channel, or per phase
plus voltage and returned energy on a 3EM; one voltage sensor per ADC channel;
temperature; humidity; and WLAN signal strength as a diagnostic entity that is
disabled by default.

**Presets.** `shelly_presets.json` holds reusable capability templates — Shelly
1, Plug S, Plug S+, 3EM, Uni, 1PM Gen2, 1PM Gen3, Plus H&T, H&T Gen1 — that the
configuration UI applies to a new entry. A preset never carries identity
(`id`, `name`, `host`) or credentials, only the technical fields above.

Editing `shelly_devices.json` needs a service restart; that file has no hot
reload.

## Sleepy (battery) devices: H&T and friends

A Shelly H&T sleeps between reports and is only reachable over HTTP for a
short window after it wakes up. Polling it on the same cycle as an
always-on device (e.g. a Plug) means nearly every poll times out, which used
to mark it offline almost permanently.

Set `sleepy: true` on such a device (both H&T presets already do) and,
optionally, `offline_grace_s` (default 3600s) to a bit more than its
configured wake/report interval. A failed poll on a sleepy device then only
turns it offline once `offline_grace_s` has passed without a successful
contact, instead of on the very next failed cycle.

**Optional wake webhook (off by default).** A Gen1 device can be configured
to call this service the moment it wakes, so the next poll doesn't have to
wait for the shared cycle to happen to land inside its short wake window:

1. Enable it in the shelly service config: `webhook_enabled: true`,
   `webhook_port` (default `8082`).
2. Open that port in the firewall. This is an explicit opt-in: the installer
   only opens it when you tick **Shelly wake webhook in the firewall** on
   its configuration screen (step 35, `scripts/bootstrap/35-ufw-shelly-webhook.sh`,
   off by default; an update never turns it on by itself). The switch only
   appears while the Shelly device service is selected; deselecting Shelly
   also drops the opt-in and removes the rule. It opens the
   `webhook_port` from `config.json` if set, otherwise 8082. Unticking it
   later removes the rule again on the next run. Without the installer, run
   `sudo ufw allow <port>/tcp` by hand. Once chosen, the installer's
   diagnose checks the rule (an error, repaired by step 35) and whether the
   service listens on the port (only a hint: it does not until step 1 is
   done).
3. On the device, under *Settings → Actions*, add a **Report URL** / sensor
   report action pointing at `http://<node-host>:<webhook_port>/shelly/wake/<device id>`
   (the `<device id>` is the `id` field from `shelly_devices.json`).

The webhook only triggers an immediate poll for that one device — it does
not itself parse the device's report payload; the normal HTTP poll still
reads temperature/humidity/battery. This deliberately avoids the device's
own MQTT client (which the Shelly firmware disables cloud access for when
enabled) — see the module docstring in `shelly_rpc_mqtt.py`.

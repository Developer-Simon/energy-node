---
title: "Energy Node Companion for Home Assistant"
component: ha-energy-node-companion
redirect_from:
  - /integration/energy-node-companion.html
---

<img class="page-icon" src="../images/ha/companion.svg" alt="" width="72" height="72">

# Energy Node Companion for Home Assistant

**Energy Node Companion** is a Home Assistant integration that works alongside your node. It does not create sensors (those come from MQTT discovery). Today it lets Home Assistant fill gaps in the dashboard's history charts.

The dashboard keeps its history in each browser. A browser only knows the hours it was open itself, so a phone that was closed overnight shows an empty chart for the night. Home Assistant already stores the node's sensors in its recorder. The integration joins the dashboard's history exchange as a permanent peer and supplies the missing hours and days from there.

The integration only supplies data. It never asks the dashboard for anything, never writes to the node and never changes your Home Assistant entities.

## Requirements

- Energy Node dashboard **v0.8.5 or newer**. Older dashboards do not announce which series they record, and the setup stops with "The dashboard is too old. Update the energy-node first."
- Home Assistant and the node in the **same Tailscale tailnet**.
- The node's energy sensors in Home Assistant through MQTT discovery. The dashboard publishes them itself as the device "Energy Node" (see [Dashboard](../dashboard/index.md)).
- The Home Assistant **recorder**, which is on by default. How far back the integration can fill depends on the recorder's `purge_keep_days`, 10 days by default.

## Install via HACS

1. Open **HACS** → **⋮** (top right) → **Custom repositories**.
2. Repository URL: `https://github.com/Developer-Simon/ha-energy-node-companion`
3. Category: **Integration** → **Add**.
4. Search for **Energy Node Companion** → **Download**.
5. **Restart Home Assistant.**

### Manual install

1. Download the latest release from [ha-energy-node-companion](https://github.com/Developer-Simon/ha-energy-node-companion/releases).
2. Extract it to `<config>/custom_components/energy_node_companion/`.
3. Restart Home Assistant.

## Set up

1. **Settings → Devices & Services → Add Integration → "Energy Node Companion"**.
2. Check the address. It is pre-filled from the node's device link in Home Assistant, for example `http://energy-node.tail1234.ts.net:8080`.
3. Submit. The integration checks that the dashboard answers and speaks the same exchange version, then connects.

Use the dashboard's own port (`8080` by default), not port 80. The reverse proxy on port 80 overwrites a header the integration will need later. If the dashboard runs on another port, change it in the address field.

To change the address later, open the integration and choose **Reconfigure**.

The address is only suggested when Home Assistant knows the MQTT device "Energy Node" and the node has a Tailscale name. Otherwise the field is empty and you enter the address by hand.

## What it supplies

The integration offers only the series the dashboard announces as recorded.

| Dashboard series | Home Assistant source |
|---|---|
| PV power | `energy_node_pv_power` |
| Grid power | grid import minus grid export |
| Battery power | battery charge minus battery discharge |
| Battery state of charge | `energy_node_battery_soc` |
| Additional history entities | the MQTT entity with the same `unique_id` |

Load, wallbox and heat pump are **not** supplied. The dashboard's load series is the measured load, while the Home Assistant sensor carries the total load including the computed part. Wallbox and heat pump have no sensor of their own in Home Assistant.

- **Minute values** come from the recorder's states, weighted by how long each value held.
- **5-minute values** come from the 5-minute statistics where the entity has a `state_class`, otherwise from the states.
- **Units** are converted to the dashboard's unit, for example kW to W. A series whose unit cannot be converted (°C instead of W) is skipped.
- **Unavailable values** stay gaps and never become zeros.
- The current, incomplete minute is never delivered. The browser never overwrites a value, so a half minute would stay half for good.

## How you see it working

Open the dashboard in a browser that missed some time, then go to **Settings → History** (German: **Verläufe**). After the first backfill the history exchange status shows a second line:

> Last filled in by Home Assistant.

In German: "Zuletzt ergänzt von Home Assistant."

The history chart then shows the hours the browser had missed.

## How it works

The integration is an ordinary peer of the dashboard's history exchange (protocol 1). The [HTTP API](../developing/api.md) describes the endpoints, and [Data flows](../developing/data-flow.md) describes the exchange between devices.

1. **Login.** The integration logs in as a guest and keeps the session cookie. It logs in again only after the dashboard answers 401, for example after a dashboard restart. Every guest login is written to the node's SD card, so a plain network reconnect does not log in again.
2. **Announcement.** It reads which series the dashboard records and matches them to Home Assistant entities.
3. **Offer.** It tells every browser which hours and days it can supply, labelled "Home Assistant". It recomputes this every full hour.
4. **Requests.** A browser that misses rows asks for them, and the integration answers from the recorder in chunks.

If the connection drops, the integration reconnects on its own, with a growing pause of 2 to 60 seconds.

## Troubleshooting

- **"The dashboard is too old. Update the energy-node first.":** update the node to v0.8.5 or newer.
- **"The dashboard cannot be reached at this address.":** check the Tailscale name and the port, and open the address in a browser on the Home Assistant host.
- **Nothing is filled in:** the missing time must lie within the recorder's `purge_keep_days`. For details, enable debug logging for `custom_components.energy_node_companion` under **Settings → System → Logs**.

## Support and feedback

- **Problems with the integration:** open an issue in [ha-energy-node-companion](https://github.com/Developer-Simon/ha-energy-node-companion/issues).
- **Development:** the integration is developed in this repository under `integrations/homeassistant/custom_components/energy_node_companion/`. Pull requests belong here, not in the mirror.

## License

MIT, see [LICENSE](https://github.com/Developer-Simon/ha-energy-node-companion/blob/main/LICENSE).

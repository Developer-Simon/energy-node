# Energy Node

<img src="https://raw.githubusercontent.com/Developer-Simon/ha-energy-node/main/custom_components/energy_node/brand/icon.png" alt="Energy Node" width="88" align="right">

A Home Assistant integration for the [energy-node](https://github.com/Developer-Simon/energy-node) dashboard.

The dashboard keeps its history charts in each browser. A browser only knows the hours it was open itself. Home Assistant already stores the node's sensors in its recorder, so this integration joins the dashboard's history exchange as a permanent peer and supplies the missing hours and days from there. It only supplies data. It never asks the dashboard for anything and never changes the node.

## Requirements

- An energy-node dashboard that announces the series it records (dashboard 0.8.5 or newer).
- Home Assistant and the node in the same Tailscale tailnet.
- The node's energy sensors in Home Assistant through MQTT discovery (the dashboard publishes them itself).
- The recorder (on by default). The reach is the recorder's `purge_keep_days`, 10 days by default.

## Install (HACS custom repository)

[![Open your Home Assistant instance and add this repository to HACS.](https://my.home-assistant.io/badges/hacs_repository.svg)](https://my.home-assistant.io/redirect/hacs_repository/?owner=Developer-Simon&repository=ha-energy-node&category=integration)

The button above pre-fills the custom repository dialog. Or by hand:

1. HACS → ⋮ (top right) → **Custom repositories**.
2. Repository: `https://github.com/Developer-Simon/ha-energy-node`, category **Integration**. Add.
3. HACS → search **Energy Node** → **Download**.
4. **Restart Home Assistant.**
5. **Settings → Devices & Services → Add Integration → "Energy Node"**.

## Setup

The address field is pre-filled from the node's device link in Home Assistant, for example `http://energy-node.tail1234.ts.net:8080`. Use the dashboard's own port (8080 by default), not port 80. If the dashboard runs on another port, change it in the field, or later through **Reconfigure**.

Once connected, open the dashboard's settings under **History** (German: **Verläufe**). After the first backfill it shows "Last filled in by Home Assistant."

## What is supplied

- Only the series the dashboard announces as recorded.
- The energy roles map to the dashboard's own MQTT sensors: PV power, grid import minus export, battery charge minus discharge and the battery state of charge. The load, wallbox and heat pump roles are not supplied.
- Every other series is matched by its MQTT `unique_id`.
- Minute values come from the recorder's states, 5-minute values from its 5-minute statistics where the entity has a `state_class`, otherwise from states.
- Values are converted to the dashboard's unit (kW to W and so on). Series with incompatible units are skipped. Unavailable values stay gaps and never become zeros.

More detail: [docs/integration.md](docs/integration.md).

## Pull requests

The integration is developed in the [energy-node repository](https://github.com/Developer-Simon/energy-node). Pull requests belong there, not in this mirror. The mirror is derived from the main repo and is regenerated with each release.

## Built with AI

This integration was written with AI assistance (Claude, via Claude Code),
reviewed and maintained by [@Developer-Simon](https://github.com/Developer-Simon).
Contributors must disclose which AI tools assisted their pull request, see
[AI-DISCLAIMER.md](AI-DISCLAIMER.md).

## License

MIT, see [LICENSE](LICENSE).

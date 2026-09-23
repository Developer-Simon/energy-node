# energy-node Icons

<img src="https://raw.githubusercontent.com/Developer-Simon/ha-energy-node-icons/main/custom_components/energy_node_icons/brand/icon.png" alt="energy-node Icons" width="88" align="right">

A Home Assistant icon set of eighteen device symbols plus a generic fallback from the energy-node dashboard. Each icon is drawn as an outline so Home Assistant can fill it with your chosen theme color.

The icons are designed for energy-management devices — batteries, solar panels, grid connections, heat pumps and more — and appear throughout the energy-node dashboard's device cards and device modal.

## Install (HACS custom repository)

[![Open your Home Assistant instance and add this repository to HACS.](https://my.home-assistant.io/badges/hacs_repository.svg)](https://my.home-assistant.io/redirect/hacs_repository/?owner=Developer-Simon&repository=ha-energy-node-icons&category=integration)

The button above pre-fills the custom-repository dialog. Or by hand:

1. HACS → ⋮ (top right) → **Custom repositories**.
2. Repository: `https://github.com/Developer-Simon/ha-energy-node-icons` — Category:
   **Integration**. Add.
3. HACS → search **energy-node Icons** → **Download**.
4. **Restart Home Assistant.**
5. **Settings → Devices & Services → Add Integration → "energy-node Icons"** (this loads the icon set into Home Assistant).

## Use

Once installed, the icon set appears in the entity icon picker. Click the icon field of any entity and type `energy-node` — the set will appear in the dropdown list. Pick the icon you want from the table below.

The icon names start with the `energy-node:` prefix (e.g. `energy-node:solar-panel`).

## Icon catalogue

| HA Name | Description |
|---|---|
| `battery-charging` | Charger |
| `chip-outline` | Standard |
| `current-ac` | Inverter |
| `ev-station` | EV Charger (Wallbox) |
| `flash-circle` | Consumption |
| `gas-burner` | Oil/Gas Heating |
| `heat-pump` | Heat Pump |
| `home` | Building |
| `home-battery` | Battery |
| `meter-electric` | 3-Phase Energy Meter |
| `pipe-valve` | Heating-pipe Valve |
| `power-plug` | Smart Socket |
| `power-socket-de` | Flush-mounted Socket |
| `raspberry-pi` | Raspberry Pi |
| `sitemap` | Automations |
| `solar-panel` | Solar Panel |
| `thermometer` | Thermometer |
| `transmission-tower` | Grid Connection |
| `water-boiler` | Water Boiler |

## About the drawings

The icon strokes are converted to filled outlines at the dashboard's stroke width, so each icon takes Home Assistant's icon colour and looks like the dashboard's stroked symbol.

## Pull requests

The icons are generated from the energy-node dashboard's icon catalogue. Pull requests belong in the [energy-node repository](https://github.com/Developer-Simon/energy-node), not in the mirror. The mirror is derived from the main repo and is regenerated with each release.

## Built with AI

This integration was written with AI assistance (Claude, via Claude Code),
reviewed and maintained by [@Developer-Simon](https://github.com/Developer-Simon).
Contributors must disclose which AI tools assisted their pull request — see
[AI-DISCLAIMER.md](AI-DISCLAIMER.md).

## License

MIT — see [LICENSE](LICENSE).

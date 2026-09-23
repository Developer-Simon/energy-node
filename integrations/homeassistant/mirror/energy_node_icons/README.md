# energy-node Icons

<img src="https://raw.githubusercontent.com/Developer-Simon/ha-energy-node-icons/main/custom_components/energy_node_icons/brand/icon.png" alt="energy-node Icons" width="88" align="right">

A Home Assistant icon set of nineteen device symbols from the energy-node dashboard. Each icon is drawn as an outline so Home Assistant can fill it with your chosen theme color.

The icons are designed for energy-management devices — batteries, solar panels, grid connections, heat pumps and more — and are already in use in the energy-node dashboard's compact energy-flow diagram.

## Install (HACS custom repository)

[![Open your Home Assistant instance and add this repository to HACS.](https://my.home-assistant.io/badges/hacs_repository.svg)](https://my.home-assistant.io/redirect/hacs_repository/?owner=Developer-Simon&repository=ha-energy-node-icons&category=integration)

The button above pre-fills the custom-repository dialog. Or by hand:

1. HACS → ⋮ (top right) → **Custom repositories**.
2. Repository: `https://github.com/Developer-Simon/ha-energy-node-icons` — Category:
   **Integration**. Add.
3. HACS → search **energy-node Icons** → **Download**.
4. **Restart Home Assistant.**

## Use

Once installed, the icon set appears in the entity icon picker. Click the icon field of any entity and type `energy-node` — the set will appear in the dropdown list. Pick the icon you want from the table below.

The icon names start with the `energy-node:` prefix (e.g. `energy-node:solar-panel`).

## Icon catalogue

| Icon | Name |
|---|---|
| ![battery-charging](https://placeholder) | `energy-node:battery-charging` |
| ![chip-outline](https://placeholder) | `energy-node:chip-outline` |
| ![current-ac](https://placeholder) | `energy-node:current-ac` |
| ![ev-station](https://placeholder) | `energy-node:ev-station` |
| ![flash-circle](https://placeholder) | `energy-node:flash-circle` |
| ![gas-burner](https://placeholder) | `energy-node:gas-burner` |
| ![heat-pump](https://placeholder) | `energy-node:heat-pump` |
| ![home](https://placeholder) | `energy-node:home` |
| ![home-battery](https://placeholder) | `energy-node:home-battery` |
| ![meter-electric](https://placeholder) | `energy-node:meter-electric` |
| ![pipe-valve](https://placeholder) | `energy-node:pipe-valve` |
| ![power-plug](https://placeholder) | `energy-node:power-plug` |
| ![power-socket-de](https://placeholder) | `energy-node:power-socket-de` |
| ![raspberry-pi](https://placeholder) | `energy-node:raspberry-pi` |
| ![sitemap](https://placeholder) | `energy-node:sitemap` |
| ![solar-panel](https://placeholder) | `energy-node:solar-panel` |
| ![thermometer](https://placeholder) | `energy-node:thermometer` |
| ![transmission-tower](https://placeholder) | `energy-node:transmission-tower` |
| ![water-boiler](https://placeholder) | `energy-node:water-boiler` |

## About the drawings

The icons are converted to **filled outlines** — Home Assistant renders them as silhouettes of the dashboard's stroked symbols, filled with your chosen icon color. This conversion is automatic, so an icon that relies on thin interior details may appear as a solid shape at badge size (the smallest size Home Assistant uses). This is a characteristic of the conversion process, not a bug in this integration — if an icon appears too solid, it will need to be redrawn with thicker strokes in the source.

## Pull requests

The icons are generated from the energy-node dashboard's icon catalogue. Pull requests belong in the [energy-node repository](https://github.com/Developer-Simon/energy-node), not in the mirror. The mirror is derived from the main repo and is regenerated with each release.

## Built with AI

This integration was written with AI assistance (Claude, via Claude Code),
reviewed and maintained by [@Developer-Simon](https://github.com/Developer-Simon).
Contributors must disclose which AI tools assisted their pull request — see
[AI-DISCLAIMER.md](AI-DISCLAIMER.md).

## License

MIT — see [LICENSE](LICENSE).

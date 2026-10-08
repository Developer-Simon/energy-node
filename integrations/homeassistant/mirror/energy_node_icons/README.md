# energy-node Icons

<img src="https://raw.githubusercontent.com/Developer-Simon/ha-energy-node-icons/main/custom_components/energy_node_icons/brand/icon.png" alt="energy-node Icons" width="88" align="right">

The eighteen device symbols of the [energy-node](https://github.com/Developer-Simon/energy-node) dashboard plus a generic fallback, as a Home Assistant icon set. Each icon is an outline that Home Assistant fills with your theme's icon colour.

![All icons of the set](https://developer-simon.github.io/energy-node/images/ha/icons/overview.svg)

## Features

### In the icon picker

Click the icon field of any entity and type `energy-node`. The set appears in the list, and you pick an icon like any built-in one.
[How to use →](https://developer-simon.github.io/energy-node/ha/icons.html#how-to-use)

<img src="https://developer-simon.github.io/energy-node/images/ha/icons/picker.png" alt="The icon picker filtered by energy-node" width="264">

### In YAML

Every icon has a fixed name with the `energy-node:` prefix, so it works in any card or entity setting:

```yaml
type: tile
entity: sensor.energy_node_pv_power
icon: energy-node:solar-panel
```

[Icon catalogue →](https://developer-simon.github.io/energy-node/ha/icons-catalogue.html)

### Same look as the dashboard

The icons are generated from the dashboard's own drawings. The strokes are converted to filled outlines at the dashboard's stroke width, so they look the same in Home Assistant.

## Install

[![Open your Home Assistant instance and add this repository to HACS.](https://my.home-assistant.io/badges/hacs_repository.svg)](https://my.home-assistant.io/redirect/hacs_repository/?owner=Developer-Simon&repository=ha-energy-node-icons&category=integration)

1. Add this repository in HACS with the button above, or under HACS → ⋮ → **Custom repositories** with category **Integration**.
2. Search for **energy-node Icons** → **Download**, then **restart Home Assistant**.
3. **Settings → Devices & Services → Add Integration → "energy-node Icons"**. This loads the icon set.

## Documentation

- [Overview](https://developer-simon.github.io/energy-node/ha/icons.html)
- [Icon catalogue](https://developer-simon.github.io/energy-node/ha/icons-catalogue.html)

## Pull requests

The icons are generated from the energy-node dashboard's icon catalogue. Pull requests belong in the [energy-node repository](https://github.com/Developer-Simon/energy-node), not in this mirror. The mirror is derived from the main repository and regenerated with each release.

## Built with AI

This integration was written with AI assistance (Claude, via Claude Code), reviewed and maintained by [@Developer-Simon](https://github.com/Developer-Simon). Contributors must disclose which AI tools assisted their pull request, see [AI-DISCLAIMER.md](AI-DISCLAIMER.md).

## License

MIT, see [LICENSE](LICENSE).

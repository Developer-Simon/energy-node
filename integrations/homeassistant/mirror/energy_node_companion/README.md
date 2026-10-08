# Energy Node Companion

<img src="https://raw.githubusercontent.com/Developer-Simon/ha-energy-node-companion/main/custom_components/energy_node_companion/brand/icon.png" alt="Energy Node Companion" width="88" align="right">

Brings the [energy-node](https://github.com/Developer-Simon/energy-node) dashboard into Home Assistant and fills the gaps in its history charts from the Home Assistant recorder.

![The energy-node dashboard, as it opens in the Home Assistant sidebar](https://developer-simon.github.io/energy-node/images/dashboard-overview.png)

## Features

### Dashboard in the sidebar

<img src="https://developer-simon.github.io/energy-node/images/ha/companion/sidebar-entry.png" alt="The Energy Node entry in the Home Assistant sidebar" width="257" align="right">

The node's dashboard opens as its own page in the Home Assistant sidebar. Home Assistant passes it through, so it also works on a phone outside your tailnet. You choose whether all users or only administrators see it.
See [Dashboard in the sidebar](https://developer-simon.github.io/energy-node/ha/companion-sidebar.html).
<br clear="right">

### History backfill

The dashboard keeps its history in each browser, so a phone that was closed overnight shows an empty chart for the night. Home Assistant already stores the node's sensors in its recorder and supplies the missing hours and days from there. The integration never writes to the node.
See [History backfill](https://developer-simon.github.io/energy-node/ha/companion-history.html).

<img src="https://developer-simon.github.io/energy-node/images/dashboard-settings-history.png" alt="The History settings of the dashboard with the device exchange" width="560">

### Setup and options

<img src="https://developer-simon.github.io/energy-node/images/ha/companion/options.png" alt="The options dialog" width="360" align="right">

Home Assistant pre-fills the dashboard address from the node's device. In the options you turn history backfill and the sidebar entry on or off, and you set the entry's name and icon.
See [Setup & options](https://developer-simon.github.io/energy-node/ha/companion-setup.html).
<br clear="right">

## Requirements

- An energy-node dashboard v0.8.5 or newer.
- Home Assistant and the node in the same Tailscale tailnet.
- The node's energy sensors in Home Assistant through MQTT discovery (the dashboard publishes them itself).
- The recorder (on by default) for history backfill.

## Install

[![Open your Home Assistant instance and add this repository to HACS.](https://my.home-assistant.io/badges/hacs_repository.svg)](https://my.home-assistant.io/redirect/hacs_repository/?owner=Developer-Simon&repository=ha-energy-node-companion&category=integration)

1. Add this repository in HACS with the button above, or under HACS → ⋮ → **Custom repositories** with category **Integration**.
2. Search for **Energy Node Companion** → **Download**, then **restart Home Assistant**.
3. **Settings → Devices & Services → Add Integration → "Energy Node Companion"**.

## Documentation

- [Overview](https://developer-simon.github.io/energy-node/ha/companion.html)
- [Setup & options](https://developer-simon.github.io/energy-node/ha/companion-setup.html)
- [Dashboard in the sidebar](https://developer-simon.github.io/energy-node/ha/companion-sidebar.html)
- [History backfill](https://developer-simon.github.io/energy-node/ha/companion-history.html)
- [Troubleshooting](https://developer-simon.github.io/energy-node/ha/companion-troubleshooting.html)

## Pull requests

The integration is developed in the [energy-node repository](https://github.com/Developer-Simon/energy-node). Pull requests belong there, not in this mirror. The mirror is derived from the main repository and regenerated with each release.

## Built with AI

This integration was written with AI assistance (Claude, via Claude Code), reviewed and maintained by [@Developer-Simon](https://github.com/Developer-Simon). Contributors must disclose which AI tools assisted their pull request, see [AI-DISCLAIMER.md](AI-DISCLAIMER.md).

## License

MIT, see [LICENSE](LICENSE).

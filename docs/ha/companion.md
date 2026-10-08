---
title: "Energy Node Companion for Home Assistant"
component: ha-energy-node-companion
redirect_from:
  - /integration/energy-node-companion.html
anchor_moves:
  set-up: companion-setup.html#set-up
  options: companion-setup.html#options
  what-it-supplies: companion-history.html#what-it-supplies
  how-you-see-it-working: companion-history.html#how-you-see-it-working
  how-it-works: companion-history.html#how-it-works
  troubleshooting: companion-troubleshooting.html
  support-and-feedback: companion-troubleshooting.html#support-and-feedback
---

<img class="page-icon" src="../images/ha/companion.svg" alt="" width="72" height="72">

# Energy Node Companion for Home Assistant

Energy Node Companion connects Home Assistant with the dashboard of your node.
The sensors themselves come from MQTT discovery, so the integration creates
none of its own.

It puts the node's dashboard into the Home Assistant sidebar. Home Assistant
passes the page through, so it opens wherever you can open Home Assistant, see
[Dashboard in the sidebar](companion-sidebar.md).

It also fills gaps in the dashboard's history charts. The dashboard keeps its
history in each browser, and a phone that was closed overnight shows an empty
chart for the night. Home Assistant has the node's sensors in its recorder and
supplies the missing hours and days from there, see
[History backfill](companion-history.md).

![The energy-node dashboard, as it opens in the Home Assistant sidebar](../images/dashboard-overview.png)

The integration never writes to the node and never changes your Home Assistant
entities.

## Requirements

- Energy Node dashboard **v0.8.5 or newer**. Older dashboards do not announce
  which series they record, and the setup stops with "The dashboard is too
  old. Update the energy-node first."
- Home Assistant and the node in the **same Tailscale tailnet**.
- The node's energy sensors in Home Assistant through MQTT discovery. The
  dashboard publishes them itself as the device "Energy Node" (see
  [Dashboard](../dashboard/index.md)).
- The Home Assistant **recorder**, which is on by default. How far back the
  integration can fill depends on the recorder's `purge_keep_days`, 10 days by
  default.

## Install via HACS

[![Open your Home Assistant instance and add this repository to HACS.](https://my.home-assistant.io/badges/hacs_repository.svg)](https://my.home-assistant.io/redirect/hacs_repository/?owner=Developer-Simon&repository=ha-energy-node-companion&category=integration)

The button opens HACS in your Home Assistant with the repository pre-filled. Or add it by hand:

1. Open **HACS** → **⋮** (top right) → **Custom repositories**.
2. Repository URL: `https://github.com/Developer-Simon/ha-energy-node-companion`
3. Category: **Integration** → **Add**.
4. Search for **Energy Node Companion** → **Download**.
5. **Restart Home Assistant.**

### Manual install

1. Download the latest release from [ha-energy-node-companion](https://github.com/Developer-Simon/ha-energy-node-companion/releases).
2. Extract it to `<config>/custom_components/energy_node_companion/`.
3. Restart Home Assistant.

## Next steps

- [Setup & options](companion-setup.md): connect the integration and choose
  what it does.
- [Dashboard in the sidebar](companion-sidebar.md): who sees the page and how
  the sign-in works.
- [History backfill](companion-history.md): which series are supplied and how
  the exchange works.
- [Troubleshooting](companion-troubleshooting.md): error messages and where to
  report problems.

## License

MIT, see [LICENSE](https://github.com/Developer-Simon/ha-energy-node-companion/blob/main/LICENSE).

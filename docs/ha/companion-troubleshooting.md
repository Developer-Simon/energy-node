---
title: "Energy Node Companion: troubleshooting"
component: ha-energy-node-companion
---

# Troubleshooting

| Message or symptom | What to do |
|---|---|
| "The dashboard is too old. Update the energy-node first." | Update the node to v0.8.5 or newer. |
| "The dashboard cannot be reached at this address." | Check the Tailscale name and the port, and open the address in a browser on the Home Assistant host. |
| "The dashboard speaks a different version of the history exchange." | Update the node and the integration to current versions. |
| No address is suggested | Home Assistant has no MQTT device "Energy Node" yet, or the node has no Tailscale name. Enter the address by hand. |
| The sidebar page says "The dashboard cannot be reached right now." | The node is offline or the address changed. Fix it under **Reconfigure**, see [Setup](companion-setup.md#set-up). |
| A user does not see the sidebar entry | The **Sidebar** option is set to administrators only, see [Dashboard in the sidebar](companion-sidebar.md#who-sees-it). |
| Nothing is filled in | The missing time must lie within the recorder's `purge_keep_days`. For details, enable debug logging for `custom_components.energy_node_companion` under **Settings → System → Logs**. |

## Support and feedback

Report problems with the integration as an issue in
[ha-energy-node-companion](https://github.com/Developer-Simon/ha-energy-node-companion/issues).
The integration is developed in the
[energy-node repository](https://github.com/Developer-Simon/energy-node) under
`integrations/homeassistant/custom_components/energy_node_companion/`, so pull
requests belong there and not in the mirror.

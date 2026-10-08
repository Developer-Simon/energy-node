# Battery SoC (LiFePO4 coulomb-counting)

<img src="https://raw.githubusercontent.com/Developer-Simon/ha-battery-soc/main/custom_components/battery_soc/brand/icon.png" alt="Battery SoC icon" width="88" align="right">

Estimates the state of charge of one or two LiFePO4 battery banks from sensors you already have. It needs no BMS connection and no extra hardware. A charge power sensor, a discharge power sensor and one voltage sensor per bank are enough.

| `display: column` | `display: trajectory` |
|---|---|
| ![Battery SoC Lovelace card, column display](https://developer-simon.github.io/energy-node/images/ha/battery-soc/card-column.png) | ![Battery SoC Lovelace card, trajectory display](https://developer-simon.github.io/energy-node/images/ha/battery-soc/card-trajectory.png) |

> The result is an estimate for monitoring and diagnostics, not a safety function of a BMS. Do not use it for automatic shutdowns without independent protection, such as cell monitoring in the charger or BMS.

## Features

### Coulomb counting with voltage recalibration

The net battery power is integrated over time. Near empty and near full, where the LiFePO4 voltage curve actually moves, the counter is reset to 0 % or 100 % using the load corrected voltage. In the flat middle of the curve the counter ignores the voltage.
See [How it works](https://developer-simon.github.io/energy-node/ha/battery-soc.html#how-it-works).

### One device per battery

State of charge, net power, time to full or empty and diagnostic values such as the load corrected cell voltage and the last calibration. Banks in series get a value per bank plus the weakest bank as the combined figure. The action `battery_soc.set_state_of_charge` anchors the counter to a known value.
See [Entities & actions](https://developer-simon.github.io/energy-node/ha/battery-soc-entities.html).

<img src="https://developer-simon.github.io/energy-node/images/ha/battery-soc/device-page.png" alt="The Battery SoC device page in Home Assistant" width="640">

### Fits your wiring

It handles AC-coupled home batteries and DC-only systems, power or current inputs, and one signed sensor as well as separate ones per direction. You set everything in the UI and can change it later.

<img src="https://developer-simon.github.io/energy-node/images/ha/battery-soc/setup-source.png" alt="The setup step Sources and bank A" width="360">
See [Setup](https://developer-simon.github.io/energy-node/ha/battery-soc-setup.html).

### Lovelace card

`custom:battery-soc-card` is registered by the integration itself. It shows the stored energy and time remaining as a column, or the state of charge as a ring with six hours of history and a six hour projection.
See [Lovelace card](https://developer-simon.github.io/energy-node/ha/battery-soc-card.html).

### Calibration suggestions

The integration watches its own calibrations and suggests better values for the inverter efficiency and the internal resistance. Nothing is applied automatically.
See [Calibration suggestions](https://developer-simon.github.io/energy-node/ha/battery-soc-suggestions.html).

## Install

[![Open your Home Assistant instance and add this repository to HACS.](https://my.home-assistant.io/badges/hacs_repository.svg)](https://my.home-assistant.io/redirect/hacs_repository/?owner=Developer-Simon&repository=ha-battery-soc&category=integration)

1. Add this repository in HACS with the button above, or under HACS → ⋮ → **Custom repositories** with category **Integration**.
2. Search for **Battery SoC** → **Download**, then **restart Home Assistant**.
3. **Settings → Devices & Services → Add Integration → "Battery SoC (LiFePO4 coulomb-counting)"**.

## Documentation

- [Overview](https://developer-simon.github.io/energy-node/ha/battery-soc.html)
- [Setup](https://developer-simon.github.io/energy-node/ha/battery-soc-setup.html)
- [Entities & actions](https://developer-simon.github.io/energy-node/ha/battery-soc-entities.html)
- [Lovelace card](https://developer-simon.github.io/energy-node/ha/battery-soc-card.html)
- [Calibration suggestions](https://developer-simon.github.io/energy-node/ha/battery-soc-suggestions.html)

## Pull requests

The integration is developed in the [energy-node repository](https://github.com/Developer-Simon/energy-node). Pull requests belong there, not in this mirror. The mirror is derived from the main repository and regenerated with each release.

## Built with AI

This integration was written with AI assistance (Claude, via Claude Code), reviewed and maintained by [@Developer-Simon](https://github.com/Developer-Simon). Contributors must disclose which AI tools assisted their pull request, see [AI-DISCLAIMER.md](AI-DISCLAIMER.md).

## License

MIT, see [LICENSE](LICENSE).

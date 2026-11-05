# Changelog

## v0.4.0 (2026-11-05)

### Features

- **auth:** sign in home assistant users through the companion (#174) (7b1ba8c)

### Chores

- **release:** prepare v0.11.0 (#203) (fda100b)

## v0.3.1 (2026-10-15)

### Features

- **discovery:** The dashboard announces wallbox, heat pump and custom category sensors in Home Assistant once something is assigned to them, and each can be deselected in the MQTT tab. The companion supplies these roles for history and energy statistics. (#142) (66cd698)
- **energy:** The automatic load mode now always calculates the house consumption from the balance, and a house meter serves as a cross-check. A status line on the energy page shows the source, the fallback and the meter deviation, and Home Assistant gets measured and calculated house load as separate sensors. (#143) (59dfeb3)
- hourly energy statistics from Home Assistant via the companion (#130) (752f59a)

## v0.2.2 (2026-10-06)

### Features

- **homeassistant:** add history, sidebar name and icon options to the companion (#96) (60ca521)

### Fixes

- **homeassistant:** The sidebar dashboard fills the whole height and the language switch works. (#89) (42baa01)

### Documentation

- **homeassistant:** point the integration manifests at the documentation site (#97) (b5e506d)

## v0.2.0 (2026-10-04)

### Features

- **homeassistant:** The dashboard opens in the Home Assistant sidebar without a second login. (#88) (c15a920)
- **homeassistant:** add Energy Node Companion as a history exchange peer (#85) (5bd7f48)

### Fixes

- **homeassistant:** polish Energy Node Companion for its first releases (#86) (7df380c)


---
title: "Overview and layout editor"
---

# Overview and layout editor

![Overview page](../images/dashboard-overview.png)

The start tab is not a fixed screen but a **grid of cards you arrange
yourself**. Above, the fixture's cards:

- **Status card** — the current situation in one sentence
  ("1.25 kW surplus. Good time for the wallbox …"), a bar showing where the
  balance sits between grid import and feed-in, and the surplus / grid draw /
  battery / data-quality tiles with their thresholds.
- **Battery status card** — segmented charge column,
  time to full or empty, the reserve kept back for a grid outage, and the
  usable capacity.
- **Energy flow** — PV, storage, building, grid and the
  individual loads as animated flows whose speed follows the actual watts.
- **Self-sufficiency ring** — coverage versus use, split by
  source and by consumer.
- **System diagram** (titled *Installation* on the card) — a wiring-style
  diagram of the site with the live power on each leg.

Every card is driven purely by MQTT Discovery data plus the role assignment
from the Energy tab. Nothing here is hard-coded to a particular device.

## The layout editor

![Layout editor with the block picker open](../images/dashboard-layout-editor.png)

**Edit** turns the overview into an editor: drag and resize cards, add
pages, and pick new blocks from the panel on the right. The picker has three
tabs — **cards**, **devices** and **entities** — so a layout page can mix
computed energy cards with a raw device tile or a single measured value.

| Block | Layout type | What it shows |
|---|---|---|
| Energy: status card | `energy_status` | Situation, thresholds, data quality |
| Energy: energy flow | `energy_flow` | Animated flow graph |
| Energy: self-sufficiency ring | `energy_ring` | Coverage/use ring |
| Energy: system diagram | `energy_schema` | Plant diagram |
| Energy: balance band | `energy_band` | Balance over time as a band |
| Energy: day band | `energy_day` | The day's curve |
| Energy: data board | `energy_board` | The balance as a plain number board |
| Battery: status card | `battery_status` | Charge column or projection |
| Diagnostics | `diagnostics` | Health summary of all devices |
| Value card | `entity_value` | One entity as a large value |
| Entity list | `entity_group` | Several entities in one card |
| (Devices tab) | `device` | A full device tile with its controls |

Editor width can be previewed as phone, tablet or monitor. Layouts are
versioned — every save is a revision that can be restored.

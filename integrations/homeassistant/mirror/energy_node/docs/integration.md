# Energy Node: how the history exchange works

The energy-node dashboard lets browsers swap history with each other (the history exchange, protocol 1). This integration takes part as a permanent peer that only supplies data.

## Connection

1. The integration logs in to the dashboard as a guest and keeps the session cookie. It logs in again only after the dashboard answers 401, for example after a dashboard restart. Every guest login is written to the node's SD card, so a plain network reconnect never logs in again.
2. It reads `GET /api/v1/history/exchange`. The announcement lists the series the dashboard records. Home Assistant offers nothing else.
3. It holds the server-sent event stream `/api/v1/history/exchange/stream` open and reconnects with a growing delay (2 s up to 60 s) when it drops.

## Offers and requests

- After joining and whenever another peer joins, the integration posts an `offer` with its coverage for the `1m` and `5m` tiers and the label "Home Assistant". The dashboard's settings page shows that label as the source of added rows.
- The coverage is recomputed every full hour, so the browser's raster and the offer always line up.
- A browser that misses rows sends a `request`. The integration answers with `deliver` chunks of at most the announced row limit. It never delivers the current, incomplete bucket.

## Data sources

| Tier | Source | Fallback |
|---|---|---|
| `1m` | recorder states, time weighted | none |
| `5m` | 5-minute statistics | states, for entities without `state_class` and for derived roles |

Derived roles subtract two sensors: grid is import minus export, battery is charge minus discharge.

## Troubleshooting

- **Setup says the dashboard is too old:** update the node. The dashboard must announce its recorded series.
- **No address is suggested:** Home Assistant has no `energy_node` MQTT device yet, or the node has no Tailscale name. Enter the address by hand.
- **Nothing is filled in:** enable debug logging for `custom_components.energy_node` and check that the browser's missing range lies within the recorder's `purge_keep_days`.

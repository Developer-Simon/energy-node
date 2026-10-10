# Device icons

Each device card and device modal shows an icon from the catalogue in
`dashboard/internal/webui/deviceicons.go`. Which icon is shown is decided in
this order:

1. The icon saved for the device in the modal ("Verwaltung & Analyse" →
   "Darstellung"), stored in `device-prefs.json`.
2. The suggested icon for the device type, derived from the `manufacturer`
   and `model` of its Home Assistant discovery device block.
3. The chip (`energy-node:chip`).

Picking the suggested icon in the modal stores an empty choice, so the device
keeps following its type. The device detail endpoint
(`GET /api/v1/devices/{id}`) returns the suggestion as `suggested_icon`.

## Suggestions

The rules live in `deviceIconSuggestions`. Manufacturer matches exactly
(case-insensitive), model by prefix; the first matching rule wins.

| Manufacturer | Model prefix | Icon |
|---|---|---|
| APsystems | any | Solarpanel (`energy-node:solar-panel`) |
| Trucki (Community-Firmware) | any | Wechselrichter (`energy-node:inverter`) |
| DIY | `LiFePO4` | Heimspeicher (`energy-node:battery-home`) |
| Raspberry Pi Foundation | any | Raspberry Pi (`energy-node:raspberry-pi`) |
| Energy Node | any | Automationen (`energy-node:automations`) |
| Tuya | `Tuya valve` | Heizungsrohr-Ventil (`energy-node:pipe-valve`) |
| Shelly | `SHPLG`, `SNPL`, `S3PL` (Plug / Plug S) | Smarte Steckdose (`energy-node:plug-smart`) |
| Shelly | `SHSW`, `SNSW`, `S3SW`, `SPSW` (1, 1PM, 2.5, 2PM, Mini) | Unterputz-Steckdose (`energy-node:socket-wall`) |
| Shelly | `SHEM`, `SPEM`, `S3EM` (EM, 3EM, Pro 3EM) | 3-Phasen-Energiezähler (`energy-node:meter-electric`) |
| Shelly | `SHHT`, `SNSN-0013A`, `S3SN-0U12A` (H&T) | Thermometer (`energy-node:thermometer`) |

Shelly model codes are the hardware IDs reported by `Shelly.GetDeviceInfo`
(gen1 `SH*`, Plus `SN*`, Pro `SP*`, gen3 `S3*`). To add a device type, add a
rule and a case to `TestSuggestedDeviceIconMatchesKnownModels`.

## Names

Every catalogue entry has two names. `name` (`energy-node:wallbox`) is what
the dashboard stores in `device-prefs.json` and `energy.json` and what the
Home Assistant icon set registers. `mdi` (`mdi:ev-station`) is the Material
Design icon with the same meaning, for places that announce an icon through
Home Assistant discovery, which only accepts `mdi:` names. A test checks
every `mdi` name against `internal/webui/testdata/mdi-names-7.4.47.txt`.

Names saved before the October 2026 reorganisation (`mdi:ev-station`,
`mdi:chip-outline`, …) are mapped by `internal/deviceiconname` and
rewritten on the next save. The Home Assistant icon set keeps resolving the
eleven renamed names (`energy-node:ev-station` and so on) through its
`ALIASES` table without listing them in the icon picker.

## Categories and shared shapes

The catalogue is grouped into categories (`generation`, `storage`, `grid`,
`switching`, `heating`, `mobility`, `building`, `control`). The picker shows
one heading per category, so each category must stay one contiguous block.

Icon families share their parts through `deviceicon_shapes.go`: one
point-symmetric lightning bolt (`bolt(cx, cy, h)`), the fan rotor, the
battery body, the house, the plug head and the wallbox body. A new icon with
a bolt uses `bolt()` instead of drawing its own.

The device map draws its role icons (PV, battery, grid, load, wallbox, heat
pump, state of charge) from the catalogue, and the sprite in `base.html`
renders its device-like symbols with `deviceIconSymbol`.

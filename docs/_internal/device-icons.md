# Device icons

Each device card and device modal shows an icon from the catalogue in
`dashboard/internal/webui/deviceicons.go`. Which icon is shown is decided in
this order:

1. The icon saved for the device in the modal ("Verwaltung & Analyse" →
   "Darstellung"), stored in `device-prefs.json`.
2. The suggested icon for the device type, derived from the `manufacturer`
   and `model` of its Home Assistant discovery device block.
3. The chip outline (`mdi:chip-outline`).

Picking the suggested icon in the modal stores an empty choice, so the device
keeps following its type. The device detail endpoint
(`GET /api/v1/devices/{id}`) returns the suggestion as `suggested_icon`.

## Suggestions

The rules live in `deviceIconSuggestions`. Manufacturer matches exactly
(case-insensitive), model by prefix; the first matching rule wins.

| Manufacturer | Model prefix | Icon |
|---|---|---|
| APsystems | any | Solarpanel (`mdi:solar-panel`) |
| Trucki (Community-Firmware) | any | Wechselrichter (`mdi:current-ac`) |
| DIY | `LiFePO4` | Batterie (`mdi:home-battery`) |
| Raspberry Pi Foundation | any | Raspberry Pi (`mdi:raspberry-pi`) |
| Energy Node | any | Automationen (`mdi:sitemap`) |
| Tuya | `Tuya valve` | Heizungsrohr-Ventil (`mdi:pipe-valve`) |
| Shelly | `SHPLG`, `SNPL`, `S3PL` (Plug / Plug S) | Smarte Steckdose (`mdi:power-plug`) |
| Shelly | `SHSW`, `SNSW`, `S3SW`, `SPSW` (1, 1PM, 2.5, 2PM, Mini) | Unterputz-Steckdose (`mdi:power-socket-de`) |
| Shelly | `SHEM`, `SPEM`, `S3EM` (EM, 3EM, Pro 3EM) | 3-Phasen-Energiezähler (`mdi:meter-electric`) |
| Shelly | `SHHT`, `SNSN-0013A`, `S3SN-0U12A` (H&T) | Thermometer (`mdi:thermometer`) |

Shelly model codes are the hardware IDs reported by `Shelly.GetDeviceInfo`
(gen1 `SH*`, Plus `SN*`, Pro `SP*`, gen3 `S3*`). To add a device type, add a
rule and a case to `TestSuggestedDeviceIconMatchesKnownModels`.

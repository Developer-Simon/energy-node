---
title: "Dashboard API: Settings and connections"
---

# Dashboard API: Settings and connections

Part of the [Dashboard API documentation](api.md). The fundamentals there
(base URL, response format, error codes) apply to every endpoint.

In the *Gate* column, `–` means a session is enough. Otherwise the column names
the role or other requirements.

## Settings, layout, device card

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET/PUT | `/api/v1/settings` | `edit_setup` for setup fields, `view_history` for history views, `edit_settings` for design fields | Dashboard settings |
| GET | `/api/v1/settings/revisions` | – | Settings revisions |
| GET | `/api/v1/settings/revisions/{revision}` | – | A single settings revision |
| POST | `/api/v1/settings/restore` | – | Restore settings, body `{"revision":"…"}` |
| GET/PUT | `/api/v1/layout` | – | Tile layout |
| GET | `/api/v1/layout/revisions` | – | Layout revisions |
| GET | `/api/v1/layout/revisions/{revision}` | – | A single layout revision |
| POST | `/api/v1/layout/restore` | – | Restore layout |
| GET | `/api/v1/device/icons` | – | Icon catalogue (name, label, markup) |
| GET | `/api/v1/device/prefs` | – | Device preferences document |
| PUT | `/api/v1/device/prefs/{device_id}` | `edit_device_appearance` + CSRF | Save icon and favourites |
| GET/PUT | `/api/v1/device/map` | – | Device card including manual edges |
| GET | `/api/v1/device/map/revisions` | – | Revisions |
| POST | `/api/v1/device/map/restore` | – | Restore |
| POST | `/api/v1/device/map/relations` | – | Create a relation (201) |
| DELETE | `/api/v1/device/map/relations/{id}` | – | Delete a relation (204) |
| GET | `/api/v1/shelly/presets` | – | Shelly presets (read-only) |

`PUT /api/v1/settings` checks one role per changed field: `edit_setup` for the
setup fields, such as `live_update_interval_seconds` and the history retention,
`view_history` for `history_views`, and `edit_settings` for every other field,
the design. A session without the role gets 403 with `edit_setup_forbidden`,
`view_forbidden` or `edit_settings_forbidden`.

Version 3 of `users.json` split the old `tune_live_updates` role into
`edit_setup` and `setup_tiny_tuya`. Every account keeps its rights: holders of
`tune_live_updates` or `edit_settings` get `edit_setup`, and non-guest holders
of `edit_config` get `setup_tiny_tuya`. Guests do not get `setup_tiny_tuya`.

Version 5 of `users.json` replaces untouched old default presets with the
current ones. Presets that an admin has changed stay as they are.

Since layout v3 (Spec F), `GET /api/v1/layout` also returns `card_types`. It
maps each item type to `{min_span, min_width, min_height, fills_height,
default_span}` from `internal/settings/cardcatalog.go`, the one place that
defines how much space each card type needs. Instead of coordinates, an item
carries `span` (size class `"1"` to `"6"` or `"full"`) and optionally `height` (a
fixed height of 1 to 12 units of 7 rem). `card_types` is only output, and `PUT`
ignores it.

`GET /api/v1/device/icons` returns the full icon catalogue as an array:

```json
[
  {
    "name": "energy-node:solar-panel",
    "mdi": "mdi:solar-panel",
    "category": "generation",
    "categoryKey": "device_icon_category.generation",
    "label": "Solarpanel",
    "labelKey": "device_icon.solar_panel",
    "markup": "…"
  },
  …
]
```

`markup` is the inner SVG of a 24×24 stroked icon. You can use it directly in a
`<symbol>` or draw it on a canvas. `name` is the value `device-prefs.json`
stores, `mdi` is the matching Material Design icon for Home Assistant
discovery. Entries of one `category` are contiguous.

**`GET /api/v1/device/prefs`** returns the complete `device-prefs.json` with the
`icon` and `favorite_refs` of every device. Older records may still carry
`pin_favorites`, which nothing reads any more. All browser sessions share it.

**`PUT /api/v1/device/prefs/{device_id}`**, with `{device_id}` URL-encoded.
The body contains the fields to change:

```json
{
  "icon": "mdi:solar-panel",
  "favorite_refs": ["entity-1", "entity-2"]
}
```

The server merges the changes into the saved document and returns the updated
`device-prefs.json`. It validates:

- `icon` must match `^mdi:[a-z0-9-]+$` (names from the icon catalogue). `null`
  or a missing field clears the custom icon.
- `favorite_refs` is an array of at most 3 entity IDs. Leave it out to clear it.
- `pin_favorites` is no longer used. The server still accepts it as a boolean
  and stores it, but the dashboard ignores it, so new clients leave it out.

A validation error returns `400` with `device_prefs_rejected`. With
authentication active, the endpoint needs the `edit_device_appearance` role and a
CSRF token.

`POST /api/v1/device/map/relations` expects
`{"child_id":"…","parent_id":"…","kind":"via_device"}` and rejects
self-references (`relation_self_reference`), unknown IDs (`unknown_device`,
404), and cycles (`relation_cycle`).

## MQTT connection

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/mqtt` | – | Effective configuration (masked) |
| PUT | `/api/v1/mqtt` | `mqtt_config` + HTTPS + CSRF | Save configuration |
| POST | `/api/v1/mqtt/credentials` | `mqtt_config` + HTTPS + CSRF | Set password |
| DELETE | `/api/v1/mqtt/credentials` | `mqtt_config` + HTTPS + CSRF | Delete password |
| POST | `/api/v1/mqtt/test` | `mqtt_config` + HTTPS + CSRF | Test connection |
| POST | `/api/v1/mqtt/reconnect` | `mqtt_config` + HTTPS + CSRF | Rebuild the running connection |
| PUT | `/api/v1/mqtt/energy-device` | `mqtt_config` + HTTPS + CSRF | Toggle `publish_energy_device` in `mqtt.json`, body `{"publish_energy_device": true}` |
| GET | `/api/v1/mqtt/status` | – | Connection status |

A saved and activated `mqtt.json` replaces the central configuration
(`config.json`) completely. Fields are not merged. `GET /api/v1/mqtt` returns
the configuration in effect and names its origin in `source`: `settings` or
`config`.

Saving, testing and reconnecting are three separate operations on purpose.
`PUT /api/v1/mqtt` only saves and leaves the running connection alone.
`POST /api/v1/mqtt/test` opens a short, separate connection.
`POST /api/v1/mqtt/reconnect` switches to the saved configuration and falls
back to the previous one if that fails. The response always contains the
resulting status, also after a failure.

The password lives in `mqtt_credentials.json`, separate from `mqtt.json`, so it
does not end up in revision copies. The API never returns it and only reports
`password_configured`.

## Mosquitto bridge

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/mqtt/bridge` | `mqtt_config` + HTTPS | Configuration including preview |
| PUT | `/api/v1/mqtt/bridge` | `mqtt_config` + HTTPS + CSRF | Save configuration |
| POST | `/api/v1/mqtt/bridge/credentials` | `mqtt_config` + HTTPS + CSRF | Set remote password |
| DELETE | `/api/v1/mqtt/bridge/credentials` | `mqtt_config` + HTTPS + CSRF | Delete remote password |
| POST | `/api/v1/mqtt/bridge/apply` | `mqtt_config` + `system_actions` + HTTPS + CSRF | Write to `/etc`, restart mosquitto |
| POST | `/api/v1/mqtt/bridge/restart` | `system_actions` + HTTPS + CSRF | Just restart mosquitto |
| GET | `/api/v1/mqtt/bridge/status` | – | Service, `$SYS` state, drift |
| GET | `/api/v1/mqtt/bridge/revisions` | `mqtt_config` + HTTPS | Revisions |
| POST | `/api/v1/mqtt/bridge/restore` | `mqtt_config` + HTTPS + CSRF | Restore a revision |

The API works with one connection. `bridge.json` stores a list internally, and
the HTTP layer hides that. `GET` and `PUT` return a `preview` with a masked
password.

`POST .../apply` requires `{"confirm": true}` and is the only privileged way to
`/etc/mosquitto/conf.d/bridge.conf`. The dashboard renders the configuration,
puts it into its own data directory and calls the root helper, which installs
it and rolls back on failure. Error codes: `bridge_helper_failed` (file
rejected), `bridge_restart_failed` (restart failed, rolled back), `bridge_busy`
(409).

`GET .../status` combines three sources:

```json
{
  "service_state": "active",
  "bridge": { "configured": true, "connected": true },
  "drift": { "known": true, "matches": true, "checked_at": "…" },
  "last_apply": { "at": "…", "user": "admin", "ok": true }
}
```

`drift.matches` compares checksums over the directives and ignores comments,
including the timestamp in the header. `last_apply` only exists in memory and is
gone after a restart.

## System configuration

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/system/config` | – | Central configuration file (`config.json`) + revision list |
| PUT | `/api/v1/system/config` | `system_actions` + HTTPS + CSRF | Validate and save configuration |
| GET | `/api/v1/system/config/schema` | – | The embedded `config.schema.json`, composed by `dashboard/cmd/schemagen` from the per-service fragments (the settings form is built from it) |

The central config has no `/revisions` or `/restore` routes. `GET` returns the
revision list inline, and there is no restore endpoint yet.

**`GET /api/v1/system/config`** returns
`{"config": {…}, "revisions": ["<UTC timestamp>.json", …]}`. All `*_file`
fields contain only the path, never the password itself. Not role-gated.

**`PUT /api/v1/system/config`** (body limit 1 MiB):

1. validates the document against `config.schema.json`.
2. checks every `*_file` value against the allowlist. Only paths under
   `/etc/energy-node/` and `/etc/energy-node-dashboard/` are accepted.
3. writes a revision of the *previous* file, kept under
   `data_dir/revisions/system-config/`, at most 20.
4. writes the new file atomically.

Response: `{"config": {…}, "restart_required": [...], "reloaded": {…}}`.
`restart_required` lists the changed fields that need a unit restart (`mqtt`,
`paths`, `dashboard.node_device_id`, any `services.*.service_id`,
`dashboard.port`, `.bind_address`, `.tls`, `.admin_username`,
`.admin_password_file`). The services pick up everything else over
`outstation/<id>/config/reload`. `reloaded` maps a service ID to `"ok"` or an
error string. It is only filled when a reload dispatcher is wired into the
router. The current build passes `nil`, so `reloaded` comes back empty and no
reload command is sent.

Error codes: `config_unreadable`, `invalid_body`, `schema_violation`,
`path_not_allowed`, `revision_failed`, `config_not_writable`.

## Tailscale

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/tailscale/status` | – | `tailscale status --json` + last action |
| GET | `/api/v1/tailscale/prereqs` | – | Installed? Service active? |
| POST | `/api/v1/tailscale/login` | `system_actions` + HTTPS + CSRF | `tailscale up` (202) |
| POST | `/api/v1/tailscale/logout` | `system_actions` + HTTPS + CSRF | `tailscale logout` |
| POST | `/api/v1/tailscale/restart` | `system_actions` + HTTPS + CSRF | Restart `tailscaled` |

`login` and `logout` require `{"confirm": true}`. `login` answers right away with
`202` and starts the CLI in the background. The auth URL then appears in
`GET /api/v1/tailscale/status`, so the client has to poll.

## TinyTuya

| Method | Path | Gate | Purpose |
|---|---|---|---|
| POST | `/api/v1/tiny-tuya/devices` | – | Fetch devices from the Tuya cloud |
| POST | `/api/v1/tiny-tuya/status` | – | Status of a local device |
| POST | `/api/v1/tiny-tuya/configure` | `setup_tiny_tuya` | Add a device to `tuya_devices.json` |
| GET/POST | `/api/v1/tiny-tuya/credentials` | `setup_tiny_tuya` for POST | Read/set cloud credentials |

Error responses in this group also contain `tool_output` with the output of the
Python helper. `GET .../credentials` returns metadata only:
`{"configured":true,"region":"eu","access_id_hint":"****abcd"}`.

`POST .../devices` uses stored credentials if the body contains no
`access_secret`. If a new `access_id` is sent without an `access_secret`, the
server rejects it.

`POST .../configure` validates all fields, merges the entry into
`tuya_devices.json` (matching by `id` or `device_id`, otherwise appended) and
saves it the same way as `PUT /api/v1/configurations/tuya_devices`, with
revision and reload.

---
title: "Dashboard API documentation"
redirect_from:
  - /knowledge/dashboard/api-documentation.html
---

# Dashboard API documentation

This is the HTTP interface of the Energy Node dashboard (Go binary
`energy-node-dashboard`). All endpoints live under `/api/v1/`.

Related pages:

- [data-flow.md](data-flow.md): data flows across the whole system
- [../operating/reverse-proxy.md](../operating/reverse-proxy.md): running under a sub-path behind a reverse proxy
- [../operating/secrets.md](../operating/secrets.md): secrets, passwords and credential files
- The cache-busting notes in the repository cover frontend assets.

When in doubt, the routing table in `dashboard/internal/httpapi/httpapi.go`
(`NewRouterWithDependencies`) is authoritative.

## Fundamentals

### Base URL and subpath

The default is `http://<host>:8080/`. Behind a reverse proxy under a subpath,
`basepath.Middleware` strips the subpath before the router sees the request, so
the router only knows root-relative paths. Clients put the subpath in front of
`/api/v1/...`.

### Response format

All responses are `application/json`. Errors always take this form:

```json
{ "code": "device_not_found", "message": "Gerät wurde nicht gefunden" }
```

`code` is stable and meant for programs. `message` is a German fallback text.
The dashboard shows the catalog text for `code` or `message_key` and only uses
`message` for codes it does not know. On `405` the server also sets the `Allow`
header.

Some errors carry additional fields, all optional:

```json
{ "code": "bridge_forbidden", "message": "Für Bridge-Revisionen fehlt die Berechtigung",
  "message_key": "error.bridge_forbidden.revisions" }
{ "code": "settings_rejected", "message": "layout: page id is empty",
  "detail": "layout: page id is empty" }
```

- `message_key` and `params` name a catalog text for a code with several
  variants, with the values for its placeholders.
- `detail` carries a passed-through technical error, usually in English. The
  dashboard appends it to the translated text of `code`.
- `tool_output` (TinyTuya only) is the output of the helper tool.

### Common error codes

| Code | Status | Meaning |
|---|---|---|
| `authentication_required` | 401 | No session, or the session has expired |
| `secure_login_required` | 403 | Admin operation attempted without HTTPS |
| `secure_connection_required` | 403 | Endpoint requires HTTPS |
| `csrf_failed` | 403 | `X-CSRF-Token` missing or does not match |
| `method_not_allowed` | 405 | Method not supported |
| `*_forbidden` | 403 | Role missing (e.g. `mqtt_config_forbidden`) |
| `*_busy` | 409 | A system action is already running |
| `*_rejected` | 400 | Input rejected (validation, schema) |


## Authentication and permissions

### Sessions

A middleware (`authMiddleware`) protects everything except `/static/*` and
`/api/v1/auth/*`. Without a valid session, `GET /` returns the login page and
any `/api/*` call returns `401`.

There are two cookies, depending on the transport:

| Cookie | When set/read | Session lifetime |
|---|---|---|
| `energy_node_session` | HTTPS requests | 24 h (password login) |
| `energy_node_guest_session` | HTTP requests | 7 days (guest session) |

Both are `HttpOnly` and `SameSite=Lax`. A request counts as HTTPS if the server
terminates TLS itself or if `X-Forwarded-Proto: https` is set.

Sessions survive a restart of the dashboard, so an update from the
Re-Deploy screen keeps the operator logged in. They live in `sessions.json`
next to `users.json` (mode 0600). The file stores a SHA-256 hash of each
session token, never the token itself. Expired sessions and sessions of
deleted users are dropped when the dashboard starts.

In practice the transport decides the session type. Password login only works
over HTTPS, guest access also over HTTP.

A session with the `system_actions` role is useless over plain HTTP. The
middleware discards it and asks for authentication again, so admin work needs
HTTPS.

### Roles

| Role | Allows |
|---|---|
| `system_actions` | Restart/reboot/poweroff, apply bridge, Tailscale actions |
| `mqtt_config` | Configure the MQTT connection and the Mosquitto bridge |
| `automations` | Write and test automation rules |
| `delete_device_discovery` | Delete a device's discovery entries |
| `tune_live_updates` | Change `live_update_interval_seconds` |
| `edit_layout` | Reserved for layout editing. Reported in the session payload but not enforced yet: `PUT /api/v1/layout` has no gate |

A session without a role can read everything and change the layout, the device
card, the energy roles, switch commands and the remaining settings.

### CSRF

Privileged write endpoints need the `X-CSRF-Token` header with the value from
`GET /api/v1/auth/session` (field `csrf_token`). It is checked against the
cookie's session.

### Three-stage pattern

The security critical endpoints (bridge, MQTT configuration, Tailscale) always
check in this order: role, then HTTPS, then CSRF.

## Endpoint overview

In the *Gate* column, `–` means a session is enough. Otherwise the column names
the role or other requirements.

### Auth

| Method | Path | Gate | Purpose |
|---|---|---|---|
| POST | `/api/v1/auth/login` | HTTPS | Log in with username/password |
| POST | `/api/v1/auth/guest` | – | Guest session without a password |
| GET | `/api/v1/auth/session` | – | Current session, roles, CSRF token |
| POST | `/api/v1/auth/logout` | – | Log out, delete cookie |

`GET /api/v1/auth/session` responds with:

```json
{
  "username": "admin",
  "guest": false,
  "roles": ["system_actions", "mqtt_config"],
  "system_actions": true,
  "delete_device_discovery": true,
  "tune_live_updates": true,
  "mqtt_config": true,
  "automations": true,
  "edit_layout": false,
  "csrf_token": "…",
  "expires_at": "2026-08-12T09:00:00Z"
}
```

### Devices and entities

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/devices` | – | All devices with entities and live values |
| GET | `/api/v1/devices/{id}` | – | A single device including `warnings` and `command_actions` |
| GET | `/api/v1/devices/{id}/diagnostics` | – | The device's diagnostic warnings |
| POST | `/api/v1/devices/{id}/reload` | – | Rebuild the registry completely |
| POST | `/api/v1/devices/{id}/ignore` | CSRF | Hide device |
| POST | `/api/v1/devices/{id}/unignore` | CSRF | Undo hiding |
| GET | `/api/v1/devices/{id}/discovery-delete/preview` | `delete_device_discovery` | What would be deleted? |
| POST | `/api/v1/devices/{id}/discovery-delete` | `delete_device_discovery` + CSRF | Delete discovery topics |
| GET | `/api/v1/devices/ignored` | – | List of hidden devices |
| GET | `/api/v1/discovery` | – | Devices + parse errors + duplicate IDs |
| GET | `/api/v1/discovery/summary` | – | `device_count`, `entity_count`, `discovery_errors`, `duplicate_ids`; carries `ETag: "registry-N"` |
| GET | `/api/v1/topics` | – | All known topics |
| GET | `/api/v1/topics/samples` | – | Last payload per topic |
| GET | `/api/v1/automation/notification` | – | Last `{at, message}` event from the automation service's `last_event` topic, parsed. `404` when no automation service, `204` before its first event. Polled by `notifications.js` for toasts. |
| GET | `/api/v1/events` | – | SSE stream with the registry version |
| POST | `/api/v1/entities/{unique_id}/command` | – | Switch an entity or set a value |

**`GET /api/v1/devices`** returns `ETag: "registry-N"`. With a matching
`If-None-Match` the server answers `304 Not Modified` without a body. Use this
when you poll repeatedly.

**`GET /api/v1/events`** is a Server-Sent Events stream and carries no values on
purpose:

```
event: registry
data: {"version":42}
```

When the version changes, the client fetches the devices with
`GET /api/v1/devices`. The send interval comes from `settings.json` and takes
effect without a reconnect.

**`POST /api/v1/entities/{unique_id}/command`**, with `{unique_id}` URL-encoded.
The body depends on the component:

```jsonc
// switch / light: without a body the server toggles, otherwise explicit
{ "payload": "ON" }

// number: the value is checked against min/max/step from discovery
{ "value": 42 }
```

Response: `{"entity_id":"…","payload":"ON","status":"published"}`.
Error codes: `command_not_supported` (404), `invalid_command_value`,
`command_value_out_of_range`, `invalid_command_step`,
`invalid_command_payload` (all 400), `command_publish_failed` (502).

The dashboard only publishes to `command_topic`s from discovery. The API cannot
reach arbitrary topics.

### Energy

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/energy` | – | Current balance including interpretation |
| GET/PUT | `/api/v1/energy/roles` | – | Role assignment per entity |
| GET/PUT | `/api/v1/energy/interpretation` | – | Interpretation parameters |
| GET | `/api/v1/energy/revisions` | – | Revision list for `energy.json` (roles + interpretation) |
| GET | `/api/v1/energy/revisions/{revision}` | – | A single revision |
| POST | `/api/v1/energy/restore` | – | Restore a revision, body `{"revision":"…"}` |

Available roles: `pv`, `battery`, `battery_charge`, `battery_discharge`,
`battery_soc`, `grid`, `grid_import`, `grid_export`, `load`, `wallbox`,
`heat_pump`.

`PUT /api/v1/energy/roles` replaces all of `assignments`. If the body has no
`interpretation`, the stored one is kept, so saving only the roles does not
reset the interpretation.

The balance is also published every 10 s to
`outstation/energy_node/energy/balance`, which the automations use.

### History

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/history/entities` | – | Current value of the entities on the history allowlist |
| GET | `/api/v1/history/exchange` | – | Self-describing announcement: protocol version + limits |
| GET | `/api/v1/history/exchange/stream` | – | SSE stream: peer join/leave, offers, requests, deliveries |
| POST | `/api/v1/history/exchange/offer` | – | Broadcast a coverage offer to the other peers |
| POST | `/api/v1/history/exchange/request` | – | Ask one peer (or the ring buffer) for specific ranges |
| POST | `/api/v1/history/exchange/deliver` | – | Hand rows to one peer, answering a request |
| POST | `/api/v1/history/exchange/buffer` | – | Feed rows into the server's 24 h ring buffer |

The dashboard keeps no history on disk. Samples live in each browser's
IndexedDB (see [data-flow.md](data-flow.md) §10), and these endpoints only
serve the recorder in the browser.

**`GET /api/v1/history/entities`** returns the current value of the entities
named in `settings.json` under `history_extra_entities`. That list is an
allowlist on the server, so the browser cannot poll arbitrary IDs:

```json
{
  "at": "2026-09-06T09:00:00Z",
  "samples": [
    { "entity_id": "…", "value": 42.0, "unit": "W", "stale": false }
  ]
}
```

`value` is `null` when the entity currently has no numeric value.

**`/api/v1/history/exchange/*`** transfers history between devices. The server
only relays and stores nothing. Browsers on the same broker exchange their
recorded `1m` and `5m` tiers (never raw), so a dashboard that was just opened
can fill its gaps. `GET /api/v1/history/exchange` describes the protocol
(version 1, 500 rows per delivery, 20 000 per request, 1 MiB per body). The
`.../stream` SSE carries the messages. A peer answers with the POST endpoints
`offer`, `request` and `deliver`. `buffer` feeds a 24 h ring buffer in memory
that takes part as the pseudo-peer `server`. Every POST must carry a `peer`
field that matches a connected stream, otherwise the answer is `peer_unknown`
(403). Other codes are `peer_gone` (404, the target left), `tier_unknown`
(400), `label_too_long` (400) and `body_too_large` / `too_many_rows` (413).

The announcement also lists `series`, the series this dashboard records
(`[{ "id": "role:pv", "unit": "W" }, …]`: the energy roles that currently have a
value, plus `history_extra_entities` with their unit). Other peers only offer
these series. An `offer` can carry an optional `label` of at most 64
characters (otherwise `label_too_long`, 400). The server relays it, and the
settings page shows it as the source of added rows. The Home Assistant
integration `energy_node_companion` (Energy Node Companion) is such a peer. It
supplies `1m` and `5m` rows from the HA recorder and never requests anything.

### Diagnostics and health

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/health` | – | Overall status: `ok` or `degraded` |
| GET | `/api/v1/health/storage` | – | State of the storage medium |
| GET | `/api/v1/runtime-cache` | – | Status of the runtime cache |
| GET | `/api/v1/diagnostics` | – | All warnings |
| GET | `/api/v1/diagnostics/rules` | – | IDs of the diagnostic rules |
| GET | `/api/v1/diagnostics/health` | – | Health score |

Each warning names its rule and the text variant it uses:

```json
{
  "rule_id": "Offline",
  "key": "offline",
  "severity": "critical",
  "device_id": "node",
  "entity_id": "temp",
  "message": "Gerät meldet sich als offline.",
  "hint": "Stromversorgung, Netzwerk und Bridge prüfen"
}
```

`key` selects the catalog texts `diagnostics.rule.<key>.message` and
`.hint`, which the dashboard shows in the page language. `message` and `hint`
are the German fallback for clients that do not know the key. `key` is not
`rule_id`, because one rule can produce more than one text.

`GET /api/v1/health` combines several sources:

```json
{
  "status": "ok",
  "uptime_seconds": 86400,
  "runtime_cache": { "…": "…" },
  "mqtt": { "connected": true, "broker_address": "localhost:1883" },
  "storage": { "available": true, "medium": "…", "mode": "…", "confidence": "…" },
  "node": {
    "telemetry": { "cpu_temp_c": 47.8, "ram_used_pct": 63.1, "undervoltage_now": false },
    "services": [
      { "id": "shelly", "state": "active" },
      { "id": "trucki", "state": "configured" }
    ]
  }
}
```

`status` becomes `degraded` when the runtime cache is degraded, the MQTT
connection is missing or the storage check fails.

`node` reports what the dashboard's node agent (`internal/nodeagent`) publishes
about the Pi. It is only present when the agent is wired in. `node.telemetry`
is missing until the first reading arrives and then carries `cpu_temp_c` and
`ram_used_pct` (each a number or `null`) and the `undervoltage_now` flag.
`node.services` has one entry per configured device service,
`{ "id": "<service_id>", "state": … }`. `state` is `active` when the service's
`outstation/<id>/status/online` is `1` and the `last_update` in its
`outstation/<id>/settings/status` is no older than `poll_interval_s *
diagnostic_poll_multiplier + 60 s`. Otherwise it is `configured`.

### Configurations (device JSONs)

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/configurations` | – | All configuration documents, each with `checksum` |
| GET/PUT | `/api/v1/configurations/{name}` | PUT for `automation_rules`: `automations` + CSRF | Read/write content |
| GET | `/api/v1/configurations/{name}/schema` | – | JSON schema |
| GET | `/api/v1/configurations/{name}/revisions` | – | Revision list |
| GET | `/api/v1/configurations/{name}/revisions/{revision}` | – | A single revision |
| POST | `/api/v1/configurations/{name}/restore` | – | Restore a revision |
| POST | `/api/v1/configurations/{name}/reload` | – | Trigger a reload via MQTT |
| GET | `/api/v1/configurations/{name}/status` | – | Reload result of the owning service: `runtime_status`, `error`, `error_code`, `config_revision`, `applied_revision` |
| POST | `/api/v1/automations/test` | `automations` + CSRF | Test a single rule action |
| GET | `/api/v1/automations/history/{rule_id}` | – | Recorded fire events for one rule, read from `automation_history.json` in the devices directory (empty array if the file or rule is absent) |

`PUT` validates against the schema, creates a revision, writes atomically and
then sends a reload command over MQTT to the service. If the reload fails, the
endpoint still answers with the saved document plus `reload_failed` and
`reload_error`. The file is written in that case, but the service has not
loaded it yet.

Body limit for `PUT`: 2 MiB.

`POST /api/v1/automations/test`:

```json
{ "rule_id": "pv-ueberschuss", "action_index": 0 }
```

The dashboard then publishes to the fixed topic
`outstation/automation/test/set`. The automation service does all checks
(rule exists, index valid, topic allowed), not the dashboard.

### Settings, layout, device card

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET/PUT | `/api/v1/settings` | `tune_live_updates` only for interval changes | Dashboard settings |
| GET | `/api/v1/settings/revisions` | – | Settings revisions |
| GET | `/api/v1/settings/revisions/{revision}` | – | A single settings revision |
| POST | `/api/v1/settings/restore` | – | Restore settings, body `{"revision":"…"}` |
| GET/PUT | `/api/v1/layout` | – | Tile layout |
| GET | `/api/v1/layout/revisions` | – | Layout revisions |
| GET | `/api/v1/layout/revisions/{revision}` | – | A single layout revision |
| POST | `/api/v1/layout/restore` | – | Restore layout |
| GET | `/api/v1/device/icons` | – | Icon catalogue (name, label, markup) |
| GET | `/api/v1/device/prefs` | – | Device preferences document |
| PUT | `/api/v1/device/prefs/{device_id}` | `edit_layout` + CSRF | Save icon, favourites, and pinning |
| GET/PUT | `/api/v1/device/map` | – | Device card including manual edges |
| GET | `/api/v1/device/map/revisions` | – | Revisions |
| POST | `/api/v1/device/map/restore` | – | Restore |
| POST | `/api/v1/device/map/relations` | – | Create a relation (201) |
| DELETE | `/api/v1/device/map/relations/{id}` | – | Delete a relation (204) |
| GET | `/api/v1/shelly/presets` | – | Shelly presets (read-only) |

`PUT /api/v1/settings` only checks the `tune_live_updates` role when
`live_update_interval_seconds` differs from the stored value. Any logged-in
session can change every other field.

Since layout v3 (Spec F), `GET /api/v1/layout` also returns `card_types`. It
maps each item type to `{min_span, min_width, min_height, fills_height,
default_span}` from `internal/settings/cardcatalog.go`, the one place that
defines how much space each card type needs. Instead of coordinates, an item
carries `span` (size class `"1"`–`"6"` or `"full"`) and optionally `height` (a
fixed height of 1–12 units of 7 rem). `card_types` is only output, and `PUT`
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
`icon`, `favorite_refs` and `pin_favorites` of every device. All browser
sessions share it.

**`PUT /api/v1/device/prefs/{device_id}`**, with `{device_id}` URL-encoded.
The body contains the fields to change:

```json
{
  "icon": "mdi:solar-panel",
  "favorite_refs": ["entity-1", "entity-2"],
  "pin_favorites": true
}
```

The server merges the changes into the saved document and returns the updated
`device-prefs.json`. It validates:

- `icon` must match `^mdi:[a-z0-9-]+$` (names from the icon catalogue). `null`
  or a missing field clears the custom icon.
- `favorite_refs` is an array of at most 3 entity IDs. Leave it out to clear it.
- `pin_favorites` is a boolean. Leave it out to clear it.

A validation error returns `400` with `device_prefs_rejected`. With
authentication active, the endpoint needs the `edit_layout` role and a CSRF
token, like every endpoint that changes the layout.

`POST /api/v1/device/map/relations` expects
`{"child_id":"…","parent_id":"…","kind":"via_device"}` and rejects
self-references (`relation_self_reference`), unknown IDs (`unknown_device`,
404), and cycles (`relation_cycle`).

### MQTT connection

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

### Mosquitto bridge

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

### System configuration

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

1. schema-validates the document against `config.schema.json`;
2. checks every `*_file` value against the allowlist (only paths under
   `/etc/energy-node/` and `/etc/energy-node-dashboard/` are accepted);
3. writes a revision of the *previous* file (kept under
   `data_dir/revisions/system-config/`, max 20);
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

### System actions

| Method | Path | Gate | Purpose |
|---|---|---|---|
| POST | `/api/v1/system/restart-dashboard` | `system_actions` + CSRF | Restart the dashboard service |
| POST | `/api/v1/system/reboot` | `system_actions` + CSRF | Reboot the Pi |
| POST | `/api/v1/system/poweroff` | `system_actions` + CSRF | Shut down the Pi |

The response is `202 Accepted` with `{"action":"…","status":"accepted"}`. If a
system action is already running, the answer is `409` with
`system_action_busy`.

### Versions and changelog

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/versions` | signed-in session | The installed bundle and the version of every component |
| GET | `/api/v1/changelog` | signed-in session | The installed `changelog.json`, `?component=<id>` (repeatable) keeps only those components |

Both read the installer's state directory (`/var/lib/energy-node-installer`:
`installed-manifest.json`, `selection.json`, `changelog.json`) and never write.
On a node the installer never touched, `versions` answers `200` with
`"bundle": null` and `"components": []`. `changelog` answers `404` with code
`no_changelog` (also for a bundle from before `changelog.json` existed).

`versions` response:

```json
{
  "bundle": {"version": "v0.7.5", "built_at": "2026-09-21T10:00:00+02:00", "arch": "armv6"},
  "running": {"dashboard": "v0.7.5"},
  "has_changelog": true,
  "components": [
    {"id": "dashboard", "label": "Dashboard", "kind": "app", "version": "v0.7.5", "installed": true},
    {"id": "service:shelly", "label": "Shelly", "kind": "service", "version": "v0.4.2", "installed": false}
  ]
}
```

`installed` is `false` for an optional service the operator deselected
(`selection.json`). `running.dashboard` is the build version of the running
binary, which can differ from `bundle.version` after the dashboard updated
itself. Without a `changelog.json`, `components` falls back to what the manifest
knows, with labels and kinds from a table in `internal/versions` that mirrors
`scripts/version/components.json` (a test keeps them in step).
`ENERGY_NODE_INSTALLER_STATE_DIR` overrides the directory. Only the local smoke
test uses it.

### Tailscale

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

### TinyTuya

| Method | Path | Gate | Purpose |
|---|---|---|---|
| POST | `/api/v1/tiny-tuya/devices` | – | Fetch devices from the Tuya cloud |
| POST | `/api/v1/tiny-tuya/status` | – | Status of a local device |
| POST | `/api/v1/tiny-tuya/configure` | – | Add a device to `tuya_devices.json` |
| GET/POST | `/api/v1/tiny-tuya/credentials` | – | Read/set cloud credentials |

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

## Not under `/api/v1/`

| Path | Purpose |
|---|---|
| `/` | Server-side rendered overview or login page |
| `/static/*` | Embedded assets (CSS, JS, icons) |


## Operational notes

### Configuration

The dashboard is configured only through the central configuration file
`/etc/energy-node/config.json`. It reads no environment variables. Start it
with:
```bash
./dashboard -config /etc/energy-node/config.json
```

For tests, `-config <path>` points to a different file.

### Files in the data directory

`settings.json`, `layout.json`, `energy.json`, `mqtt.json`, `bridge.json`,
`users.json`, `sessions.json`, `runtime.json`, `ignored_devices.json` and the separate secret
files `mqtt_credentials.json`, `mqtt_bridge_credentials.json`, and
`tinytuya_credentials.json`.

### Deliberate limits

- The server stores no history. The command history per device (at most 12
  entries) and the ring buffer of the history exchange (24 h) only exist in
  memory. The rolling measurement history lives in each browser's IndexedDB,
  not on the Pi (see History above). There is no time series and no persistent
  event list on the server.
- Write endpoints limit the body to 2 MiB, commands to 4 KiB.
- There is no rate limiting. The dashboard is built for a trusted network and
  Tailscale and should not be exposed openly.

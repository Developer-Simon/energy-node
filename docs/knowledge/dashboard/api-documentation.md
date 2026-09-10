---
title: "Dashboard API Documentation"
---

# Dashboard API Documentation

HTTP interface of the Energy Node dashboard (Go binary
`energy-node-dashboard`). All endpoints live under `/api/v1/`.

Related documents:

- System-wide data flows → [../data-flow.md](../data-flow.md)
- Reverse proxy / sub-path operation → [reverse-proxy.md](reverse-proxy.md)
- Secrets, passwords, credential files → [secrets-and-credentials.md](secrets-and-credentials.md)
- Frontend asset cache-busting → [lazy-assets-cache-busting.md](lazy-assets-cache-busting.md)

The source of truth is the routing table in
`dashboard/internal/httpapi/httpapi.go` (`NewRouterWithDependencies`).

---

## Fundamentals

### Base URL and subpath

The default is `http://<host>:8080/`. If the dashboard runs behind a reverse
proxy under a subpath, `basepath.Middleware` strips the path before the router
sees it — so the router only ever knows root-relative paths. Clients simply
prepend the subpath to `/api/v1/...`.

### Response format

All responses are `application/json`. Errors always take this form:

```json
{ "code": "device_not_found", "message": "Gerät wurde nicht gefunden" }
```

`code` is stable and machine-readable, `message` is human-readable German text
for the UI. On `405` the server additionally sets the `Allow` header.

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

---

## Authentication and permissions

### Sessions

A middleware wrapper (`authMiddleware`) protects **everything except**
`/static/*` and `/api/v1/auth/*`. Without a valid session, `GET /` returns the
login page, whereas any `/api/*` call returns `401`.

There are two cookies, depending on the transport:

| Cookie | When set/read | Session lifetime |
|---|---|---|
| `energy_node_session` | HTTPS requests | 24 h (password login) |
| `energy_node_guest_session` | HTTP requests | 7 days (guest session) |

Both are `HttpOnly` and `SameSite=Lax`. A request counts as HTTPS if TLS is
terminated at the server *or* if `X-Forwarded-Proto: https` is set.

In practice, transport and session type coincide: password login is HTTPS-only,
while guest access also works over HTTP.

**Important:** A session with the `system_actions` role is worthless over plain
HTTP — the middleware discards it and demands authentication. Admin operation
requires HTTPS.

### Roles

| Role | Allows |
|---|---|
| `system_actions` | Restart/reboot/poweroff, apply bridge, Tailscale actions |
| `mqtt_config` | Configure the MQTT connection and the Mosquitto bridge |
| `automations` | Write and test automation rules |
| `delete_device_discovery` | Delete a device's discovery entries |
| `tune_live_updates` | Change `live_update_interval_seconds` |
| `edit_layout` | Reserved for layout editing — reported in the session payload but **not currently enforced**: `PUT /api/v1/layout` has no gate |

Logged in but without a role: everything read-only, plus layout, device card,
energy roles, switch commands, and the remaining settings.

### CSRF

Privileged write endpoints require the `X-CSRF-Token` header carrying the value
from `GET /api/v1/auth/session` (field `csrf_token`). It is checked against the
cookie's session.

### Three-stage pattern

The security-critical endpoints (bridge, MQTT configuration, Tailscale) check
consistently in this order: **role → HTTPS → CSRF**.

---

## Endpoint overview

Legend for the *Gate* column: `–` means session only, otherwise a role or
additional requirements.

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
`If-None-Match`, the server responds `304 Not Modified` with no body — the
recommended way to poll repeatedly.

**`GET /api/v1/events`** is a Server-Sent Events stream and deliberately carries
no payload:

```
event: registry
data: {"version":42}
```

When the version changes, the client fetches the devices via
`GET /api/v1/devices`. The send interval comes from `settings.json` and takes
effect without a reconnect.

**`POST /api/v1/entities/{unique_id}/command`** — `{unique_id}` is URL-encoded.
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

The dashboard publishes exclusively to `command_topic`s from discovery —
arbitrary topics are not reachable through the API.

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

`PUT /api/v1/energy/roles` replaces `assignments` **completely**. If the body
omits `interpretation`, the stored interpretation is kept — this is intentional,
so that saving roles alone does not reset the interpretation.

The balance is additionally published every 10 s to
`outstation/energy_node/energy/balance`; the automations build on that.

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

The dashboard keeps **no history on disk** — samples live in each browser's
IndexedDB (see [../data-flow.md](../data-flow.md) §10). These endpoints only
support the browser-side recorder.

**`GET /api/v1/history/entities`** returns the current value of exactly the
entities named in `settings.json` under `history_extra_entities` (a server-side
allowlist; the browser cannot poll arbitrary IDs):

```json
{
  "at": "2026-09-06T09:00:00Z",
  "samples": [
    { "entity_id": "…", "value": 42.0, "unit": "W", "stale": false }
  ]
}
```

`value` is `null` when the entity currently has no numeric value.

**`/api/v1/history/exchange/*`** is a device-to-device transfer channel; the
server is a **relay, not a store**. Browsers on the same broker exchange their
recorded `1m` and `5m` tiers (never raw) so a freshly opened dashboard can
backfill. `GET /api/v1/history/exchange` describes the protocol (version 1; 500
rows per delivery, 20 000 per request, 1 MiB per body). The `.../stream` SSE
carries the message flow; `offer` / `request` / `deliver` are the POST
counterparts a peer uses to talk back; `buffer` feeds a 24 h in-memory ring
buffer that participates as the pseudo-peer `server`. Every POST must carry a
`peer` field matching a currently connected stream, else `peer_unknown` (403).
Other codes: `peer_gone` (404, the target left), `tier_unknown` (400),
`body_too_large` / `too_many_rows` (413).

### Diagnostics and health

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/health` | – | Overall status: `ok` or `degraded` |
| GET | `/api/v1/health/storage` | – | State of the storage medium |
| GET | `/api/v1/runtime-cache` | – | Status of the runtime cache |
| GET | `/api/v1/diagnostics` | – | All warnings |
| GET | `/api/v1/diagnostics/rules` | – | IDs of the diagnostic rules |
| GET | `/api/v1/diagnostics/health` | – | Health score |

`GET /api/v1/health` combines several sources:

```json
{
  "status": "ok",
  "uptime_seconds": 86400,
  "runtime_cache": { "…": "…" },
  "mqtt": { "connected": true, "broker_address": "localhost:1883" },
  "storage": { "available": true, "medium": "…", "mode": "…", "confidence": "…" }
}
```

`status` is set to `degraded` as soon as the runtime cache is degraded, the
MQTT connection is missing, or the storage check fails.

### Configurations (device JSONs)

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/configurations` | – | All configuration documents |
| GET/PUT | `/api/v1/configurations/{name}` | PUT for `automation_rules`: `automations` + CSRF | Read/write content |
| GET | `/api/v1/configurations/{name}/schema` | – | JSON schema |
| GET | `/api/v1/configurations/{name}/revisions` | – | Revision list |
| GET | `/api/v1/configurations/{name}/revisions/{revision}` | – | A single revision |
| POST | `/api/v1/configurations/{name}/restore` | – | Restore a revision |
| POST | `/api/v1/configurations/{name}/reload` | – | Trigger a reload via MQTT |
| POST | `/api/v1/automations/test` | `automations` + CSRF | Test a single rule action |
| GET | `/api/v1/automations/history/{rule_id}` | – | Recorded fire events for one rule, read from `automation_history.json` in the devices directory (empty array if the file or rule is absent) |

`PUT` validates against the schema, writes atomically, and creates a revision
beforehand; afterwards a reload command goes out via MQTT to the responsible
service. If the reload fails, the endpoint still responds with the saved
document and the fields `reload_failed` / `reload_error` — the file has been
written in that case, only the service has not picked it up yet.

Body limit for `PUT`: 2 MiB.

`POST /api/v1/automations/test`:

```json
{ "rule_id": "pv-ueberschuss", "action_index": 0 }
```

The dashboard then publishes to the hard-wired topic
`outstation/automation/test/set`. All domain-level checks (rule exists, index
valid, topic allowed) are done by the automation service, not the dashboard.

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
| GET/PUT | `/api/v1/device/map` | – | Device card including manual edges |
| GET | `/api/v1/device/map/revisions` | – | Revisions |
| POST | `/api/v1/device/map/restore` | – | Restore |
| POST | `/api/v1/device/map/relations` | – | Create a relation (201) |
| DELETE | `/api/v1/device/map/relations/{id}` | – | Delete a relation (204) |
| GET | `/api/v1/shelly/presets` | – | Shelly presets (read-only) |

`PUT /api/v1/settings` checks the `tune_live_updates` role **only when**
`live_update_interval_seconds` actually changes relative to the stored value.
Every other field may be changed by any logged-in session.

`GET /api/v1/layout` additionally returns `card_types` since layout v3 (Spec F):
a mapping of item type → `{min_span, min_width, min_height, fills_height,
default_span}` from `internal/settings/cardcatalog.go`, the single source of
truth about the space requirements per card type. An item carries `span` (size
class `"1"`–`"6"`/`"full"`) and optionally `height` (forced height, 1–12 units
of 7 rem) instead of coordinates. `card_types` is output only and is ignored by
`PUT`.

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

**Precedence rule:** A saved and activated `mqtt.json` wins *completely* over
the central configuration (`config.json`) — there is no per-field merge.
`GET /api/v1/mqtt` returns the configuration that is actually in effect and
names its origin in the `source` field: `settings` or `config`.

Three separate operations, deliberately not merged:

- `PUT /api/v1/mqtt` only saves — the running connection is left untouched.
- `POST /api/v1/mqtt/test` opens a short-lived, separate connection.
- `POST /api/v1/mqtt/reconnect` adopts the saved configuration and falls back
  to the previous one on failure. The response always contains the resulting
  status, even on failure.

The password lives in `mqtt_credentials.json`, separate from `mqtt.json`, so
that it does not end up in revision copies. It is never returned; the API only
reports `password_configured`.

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

The API works with **one** connection; internally `bridge.json` stores a list,
and the HTTP layer hides it. `GET`/`PUT` return a `preview` with a masked
password.

`POST .../apply` requires `{"confirm": true}` and is the only privileged path
to `/etc/mosquitto/conf.d/bridge.conf`: the dashboard renders the
configuration, places it in its own data directory, and calls the root helper,
which installs it and rolls back on failure. Error codes:
`bridge_helper_failed` (file rejected), `bridge_restart_failed` (restart
failed, rolled back), `bridge_busy` (409).

`GET .../status` bundles three independent sources:

```json
{
  "service_state": "active",
  "bridge": { "configured": true, "connected": true },
  "drift": { "known": true, "matches": true, "checked_at": "…" },
  "last_apply": { "at": "…", "user": "admin", "ok": true }
}
```

`drift.matches` compares checksums over the directives — comments, and thus the
timestamp in the header, are ignored. `last_apply` exists only in memory and is
absent after a restart.

### System configuration

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/system/config` | – | Central configuration file (`config.json`) + revision list |
| PUT | `/api/v1/system/config` | `system_actions` + HTTPS + CSRF | Validate and save configuration |
| GET | `/api/v1/system/config/schema` | – | The embedded `config.schema.json`, composed by `dashboard/cmd/schemagen` from the per-service fragments (the settings form is built from it) |

There are **no** separate `/revisions` or `/restore` routes for the central
config — the revision list is returned inline by `GET`, and there is currently
no restore endpoint.

**`GET /api/v1/system/config`** returns
`{"config": {…}, "revisions": ["<UTC timestamp>.json", …]}`. All `*_file`
fields contain only the path, never the password itself. Not role-gated.

**`PUT /api/v1/system/config`** (body limit 1 MiB):

1. schema-validates the document against `config.schema.json`;
2. checks every `*_file` value against the allowlist — only paths under
   `/etc/energy-node/` and `/etc/energy-node-dashboard/` are accepted;
3. writes a revision of the *previous* file (kept under
   `data_dir/revisions/system-config/`, max 20);
4. writes the new file atomically.

Response: `{"config": {…}, "restart_required": [...], "reloaded": {…}}`.
`restart_required` lists the changed fields that need a unit restart (`mqtt`,
`paths`, `dashboard.node_device_id`, any `services.*.service_id`, `dashboard.port`,
`.bind_address`, `.tls`, `.admin_username`, `.admin_password_file`); everything
else is picked up by the services over `outstation/<id>/config/reload`.
`reloaded` maps a service's ID to `"ok"` or an error string, and is populated
only when a reload
dispatcher is wired into the router — in the current build it is passed as
`nil`, so `reloaded` comes back empty and no reload command is sent.

Error codes: `config_unreadable`, `invalid_body`, `schema_violation`,
`path_not_allowed`, `revision_failed`, `config_not_writable`.

### System actions

| Method | Path | Gate | Purpose |
|---|---|---|---|
| POST | `/api/v1/system/restart-dashboard` | `system_actions` + CSRF | Restart the dashboard service |
| POST | `/api/v1/system/reboot` | `system_actions` + CSRF | Reboot the Pi |
| POST | `/api/v1/system/poweroff` | `system_actions` + CSRF | Shut down the Pi |

Response `202 Accepted` with `{"action":"…","status":"accepted"}`. If a system
action is already running, `409` with `system_action_busy` is returned.

### Tailscale

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/tailscale/status` | – | `tailscale status --json` + last action |
| GET | `/api/v1/tailscale/prereqs` | – | Installed? Service active? |
| POST | `/api/v1/tailscale/login` | `system_actions` + HTTPS + CSRF | `tailscale up` (202) |
| POST | `/api/v1/tailscale/logout` | `system_actions` + HTTPS + CSRF | `tailscale logout` |
| POST | `/api/v1/tailscale/restart` | `system_actions` + HTTPS + CSRF | Restart `tailscaled` |

`login` and `logout` require `{"confirm": true}`. `login` responds immediately
with `202` and starts the CLI in the background; the auth URL then appears in
`GET /api/v1/tailscale/status`, so the client has to poll.

### TinyTuya

| Method | Path | Gate | Purpose |
|---|---|---|---|
| POST | `/api/v1/tiny-tuya/devices` | – | Fetch devices from the Tuya cloud |
| POST | `/api/v1/tiny-tuya/status` | – | Status of a local device |
| POST | `/api/v1/tiny-tuya/configure` | – | Add a device to `tuya_devices.json` |
| GET/POST | `/api/v1/tiny-tuya/credentials` | – | Read/set cloud credentials |

Error responses in this group additionally contain `tool_output` with the
output of the Python helper. `GET .../credentials` returns metadata only:
`{"configured":true,"region":"eu","access_id_hint":"****abcd"}`.

`POST .../devices` uses stored credentials if the body contains no
`access_secret`. If a new `access_id` is sent without an `access_secret`, the
server rejects it.

`POST .../configure` validates all fields, merges the entry into
`tuya_devices.json` (matching by `id` or `device_id`, otherwise appended) and
saves it through the same path as
`PUT /api/v1/configurations/tuya_devices` — including revision and reload.

---

## Not under `/api/v1/`

| Path | Purpose |
|---|---|
| `/` | Server-side rendered overview or login page |
| `/static/*` | Embedded assets (CSS, JS, icons) |

---

## Operational notes

### Configuration

The dashboard is configured entirely through the central configuration file
`/etc/energy-node/config.json`. Environment variables are no longer read.

On startup:
```bash
./dashboard -config /etc/energy-node/config.json
```

For tests, a different path can be given via `-config <path>`.

### Files in the data directory

`settings.json`, `layout.json`, `energy.json`, `mqtt.json`, `bridge.json`,
`users.json`, `runtime.json`, `ignored_devices.json`, as well as the separate
secret files `mqtt_credentials.json`, `mqtt_bridge_credentials.json`, and
`tinytuya_credentials.json`.

### Deliberate limits

- The server persists **no history**: the per-device command history (max. 12
  entries) and the history-exchange ring buffer (24 h) are in memory only, and
  the rolling measurement history lives in each browser's IndexedDB, not on the
  Pi (see the History section above). No server-side time series, no persistent
  event list.
- Write endpoints limit the body to 2 MiB, commands to 4 KiB.
- There is **no rate limiting** — the dashboard is designed for a trusted
  network plus Tailscale, not for open exposure.

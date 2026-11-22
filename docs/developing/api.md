---
title: "Dashboard API documentation"
redirect_from:
  - /knowledge/dashboard/api-documentation.html
anchor_moves:
  authentication-and-permissions: api-auth.html#authentication-and-permissions
  sessions: api-auth.html#sessions
  roles: api-auth.html#roles
  route-access-groups: api-auth.html#route-access-groups
  restricted-http-sessions: api-auth.html#restricted-http-sessions
  users-apiv1users: api-auth.html#users-apiv1users
  guest-roles: api-auth.html#guest-roles
  guest-access: api-auth.html#guest-access
  companion-over-http: api-auth.html#companion-over-http
  csrf: api-auth.html#csrf
  three-stage-pattern: api-auth.html#three-stage-pattern
  auth: api-auth.html#auth
  companion-port-8081: api-auth.html#companion-port-8081
  devices-and-entities: api-data.html#devices-and-entities
  energy: api-data.html#energy
  history: api-data.html#history
  configurations-device-jsons: api-data.html#configurations-device-jsons
  settings-layout-device-card: api-settings.html#settings-layout-device-card
  mqtt-connection: api-settings.html#mqtt-connection
  mosquitto-bridge: api-settings.html#mosquitto-bridge
  system-configuration: api-settings.html#system-configuration
  tailscale: api-settings.html#tailscale
  tinytuya: api-settings.html#tinytuya
  diagnostics-and-health: api-system.html#diagnostics-and-health
  system-actions: api-system.html#system-actions
  updates: api-system.html#updates
  versions-and-changelog: api-system.html#versions-and-changelog
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


## Endpoint overview

The endpoints are described on four pages:

- [Authentication](api-auth.md): sessions, roles and permissions, restricted
  HTTP sessions, users, guests, CSRF, the sign-in endpoints and the companion
  port 8081.
- [Devices and data](api-data.md): devices and entities, energy, history and
  the device configurations.
- [Settings and connections](api-settings.md): settings, layout and device
  card, MQTT connection, Mosquitto bridge, system configuration, Tailscale and
  TinyTuya.
- [System and updates](api-system.md): diagnostics and health, system actions,
  updates, versions and changelog, and the backup routes.

## Monitoring

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/monitoring` | view monitoring | The notices of the monitoring page, the number of new ones and the learned values |
| POST | `/api/v1/monitoring/ack` | view monitoring, edit setup, CSRF | Acknowledges a notice (`ack` true) or takes the acknowledgement back (`ack` false), answers like the GET |

**`GET /api/v1/monitoring`** lists the notices of the round, each with `id`,
`source` (`energy`, `chain` or `learn`), `severity`, `code`, `params`, `steps`,
`first_seen` (unix time of the first sighting) and `ack`, which is `null` or
`{"at","user"}`. `new_count` counts the notices without an acknowledgement.
`learned` has the same values as `GET /api/v1/automations/learned`, with the
device name and icon added. `notices` and `learned.values` are never null.

**`POST /api/v1/monitoring/ack`** takes `{"id":"energy:unassigned","ack":true}`. The ID
must be the ID of a current notice. The answer is the same as the GET.

```json
{"notices":[{"id":"energy:unassigned","source":"energy","severity":"info","code":"unassigned","params":{"count":2},"steps":[],"first_seen":1794000000,"ack":null}],
 "new_count":1,"learned":{"enabled":true,"values":[]}}
```

| Status | Code | When |
|---|---|---|
| 400 | `monitoring_ack_invalid` | The POST body has no notice ID |
| 403 | `view_forbidden` | The account lacks `view_monitoring` |
| 403 | `monitoring_ack_forbidden` | The POST needs `edit_setup` and the account lacks it |
| 403 | `csrf_failed` | The POST has no CSRF token or a wrong one |
| 404 | `monitoring_ack_unknown` | The ID is not a current notice |
| 500 | `monitoring_save_failed` | Saving the acknowledgement failed |
| 501 | `monitoring_unavailable` | The node runs without the monitoring services |

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
  not on the Pi (see [History](api-data.md#history)). There is no time series and no persistent
  event list on the server.
- Write endpoints limit the body to 2 MiB, commands to 4 KiB.
- There is no rate limiting. The dashboard is built for a trusted network and
  Tailscale and should not be exposed openly.

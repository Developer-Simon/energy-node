---
title: "Dashboard API: System and updates"
---

# Dashboard API: System and updates

Part of the [Dashboard API documentation](api.md). The fundamentals there
(base URL, response format, error codes) apply to every endpoint.

In the *Gate* column, `–` means a session is enough. Otherwise the column names
the role or other requirements.

## Diagnostics and health

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
    "telemetry": { "cpu_temp_c": 47.8, "cpu_load_pct": 12.5, "ram_used_pct": 63.1, "undervoltage_now": false },
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
is missing until the first reading arrives and then carries `cpu_temp_c`,
`cpu_load_pct` (one minute load average per core, capped at 100) and
`ram_used_pct` (each a number or `null`) and the `undervoltage_now` flag.
`node.services` has one entry per configured device service,
`{ "id": "<service_id>", "state": … }`. `state` is `active` when the service's
`outstation/<id>/status/online` is `1` and the `last_update` in its
`outstation/<id>/settings/status` is no older than `poll_interval_s *
diagnostic_poll_multiplier + 60 s`. Otherwise it is `configured`.

## System actions

| Method | Path | Gate | Purpose |
|---|---|---|---|
| POST | `/api/v1/system/restart-dashboard` | `system_actions` + CSRF | Restart the dashboard service |
| POST | `/api/v1/system/reboot` | `system_actions` + CSRF | Reboot the Pi |
| POST | `/api/v1/system/poweroff` | `system_actions` + CSRF | Shut down the Pi |
| GET | `/api/v1/system/logs/units` | `view_logs` | Allowlisted units, since and line presets, and the defaults |
| GET | `/api/v1/system/logs` | `view_logs` | Journal entries of one allowlisted unit (`unit`, `since`, `lines`, `cursor`, `q`) |

The response is `202 Accepted` with `{"action":"…","status":"accepted"}`. If a
system action is already running, the answer is `409` with
`system_action_busy`.

The service log endpoints only read. They need the `view_logs` role but
no CSRF token. `unit` is required and must be one of the allowlisted units
that `/logs/units` lists. `since` is in minutes (default `60`) and `lines` is
the number of entries (default `200`), both limited to the presets from
`/logs/units`. `cursor` continues an earlier page and `q` filters the lines by
substring. Errors: `log_query_invalid` (400), `journal_not_permitted` (503),
`journal_failed` (502, `message` holds the detail), `journal_timeout` (504) and
`journal_busy` (429, two queries already run). Responses carry
`Cache-Control: no-store`.

## Updates

| Method | Path | Gate | Purpose |
|---|---|---|---|
| POST | `/api/v1/updates/install` | `install_updates` + CSRF | Install the update the dashboard currently offers |
| GET | `/api/v1/updates/progress` | signed-in session | How far a staged update is, or how the last install failed |

`POST /api/v1/updates/install` takes no parameters. The body and the query
are ignored, so a client cannot name a version: the request installs exactly
the offered release, which is signed and is the only one the dashboard
offers. The route is not HTTPS-only, because the role is not HTTPS-only
either. The answer is `202 Accepted` with `{"version":"v0.10.9"}`, the
version being installed.

Errors: `install_updates_forbidden` (403), `csrf_failed` (403),
`no_update_available` (409, nothing is offered), `update_in_progress` (409,
an install or an updater job is already running), `install_failed` (500) and
`install_unavailable` (501, this device cannot install directly).

`GET /api/v1/updates/progress` reports `{"state":"preparing","version":"…"}`
while the installer prepares the offered update (download and staging). When
the preparation fails, it reports `{"state":"fail","version":"…","code":"…",
"finished_at":"…"}` until a newer job replaces it. After the staging run the
job shows the usual `pending`, `running` and `ok` or `fail` states. The
endpoint answers `Cache-Control: no-store`.

## Versions and changelog

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

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

## Backup

Every route is under `/api/v1/backup/` and needs HTTPS (`secure_connection_required`,
403). The backup roles are described in [Settings: Backup](../dashboard/settings-backup.md#what-a-backup-contains).
Routes that change something also need the CSRF token.

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/status` | `backup_export` or `backup_import` | Settings, archives, the job, the last skipped run and the size of each area |
| PUT | `/settings` | `backup_export` + CSRF, and `backup_secrets` to switch credentials on | Change the backup settings |
| POST | `/archives` | `backup_export` + CSRF, and `backup_secrets` with credentials | Start a manual backup as a job |
| GET | `/archives/{id}` | `backup_export`, and `backup_secrets` for an unprotected archive with credentials | Download an archive |
| DELETE | `/archives/{id}` | `backup_export` + CSRF | Delete an archive from the node |
| GET | `/job` | `backup_export` or `backup_import` | Status of the last export or restore job |
| POST | `/import` | `backup_import` + CSRF | Upload an archive or name a stored one, and return its preview |
| POST | `/import/unlock` | `backup_import` + CSRF | Give the password of the waiting archive |
| POST | `/import/apply` | `backup_import` + CSRF, and `backup_secrets` when credentials are restored | Start the restore as a job |
| GET | `/import/result` | `backup_import` | Result of the last restore, `{}` if there is none |

Archive IDs have the form `YYYYMMDD-HHMMSS-<trigger>` in UTC. The
triggers are `manual`, `auto`, `update` and `pre_import`.

### Status, settings and archives

`GET /status` answers with the settings, the archive list, the current job, the
last skipped scheduled run and the size of the live data per area:

```json
{
  "settings": { "before_update": true, "schedule": false, "every": "week", "keep": 5,
                "system_fallback": true, "secrets": false,
                "content": { "settings": true, "devices": true, "users": true, "secrets": false,
                             "stats": true, "history": true, "battery": false } },
  "archives": [ { "id": "20261108-030000-auto", "trigger": "auto", "version": "v0.11.7",
                  "place": "system", "size": 48213, "created_at": "2026-11-08T03:00:02Z",
                  "areas": ["settings", "devices", "users", "stats", "history"],
                  "secrets": false, "locked": false } ],
  "job": { "id": "job-3", "kind": "export", "phases": [], "current": 0, "error": "", "result": "", "running": false },
  "last_skipped": { "at": null, "reason": "" },
  "place": "system",
  "areas": [ { "key": "settings", "size": 12345 } ]
}
```

`PUT /settings` accepts the same fields. Missing fields keep their stored value.
A `content` object replaces the stored map as a whole. An unknown `every` or a
`keep` below 1 is replaced by the default. The answer is the saved settings.
Turning on `secrets` or the credentials area needs `backup_secrets`.

`POST /archives` takes `areas` (area to boolean), `secrets` (boolean), an
optional `password` and `target`, which is `store` (kept on the node) or
`download` (kept in the staging area until it is fetched, at most one hour). A password has at least 8
characters. The answer is `202 Accepted` with `{"job":"job-3"}`. Poll
`GET /job` for the result. The result is the archive ID.

`GET /archives/{id}` answers with the archive as an attachment,
`application/octet-stream`, `Cache-Control: no-store`, and the name
`<id>.tar.gz` or `<id>.tar.gz.enc`. A download created with `target: download`
is removed after it was sent, or after one hour if nobody fetched it.

`DELETE /archives/{id}` answers `{"id":"…","status":"deleted"}`.

`GET /job` reports the last export or import. `kind` is `export` or `import`.
The phases are `write` and `finish` for an export, and `safety`, `stage`,
`stop` and `restart` for a restore. `current` is the index of the running phase.
`error` holds the error text when the job fails.

### Restore

`POST /import` takes either the raw archive as the body, or a JSON body
`{"archive":"<id>"}` for an archive in the list. An optional header
`X-Backup-Password` carries the password. The answer is the preview:

```json
{
  "manifest": { "node_version": "v0.11.7", "hostname": "node", "trigger": "auto",
                "created_at": "2026-11-08T03:00:02Z", "areas": ["settings", "users"], "secrets": false },
  "areas": [ { "area": "devices", "available": true, "count": 3, "size": 2048,
               "changes": [ { "kind": "mod", "label": "Device", "name": "shelly-1" } ] } ],
  "environment": [ { "key": "broker", "here": "localhost:1883", "archive": "10.0.0.5:1883", "off": "" } ],
  "different_device": true
}
```

`kind` in a change is `mod`, `new` or `del`. `environment` is only filled when
`different_device` is true. Its keys are `broker`, `port`, `bridge` and
`tailscale`. Only `broker` and `port` can be taken from the archive, and a row
with `off` set names a bridge or Tailscale of the archive that this device lacks.

An encrypted archive without a password answers `409 password_required`, and
the upload waits for `POST /import/unlock`. That route takes `{"password":"…"}`
and answers with the same preview. Without a waiting archive it answers
`409 backup_no_import`.

`POST /import/apply` takes `areas` (area to boolean) and `use_archive`
(`broker` and `port` to boolean). Send at least one area or one `use_archive`
value, or the answer is `400 backup_bad_request`. The answer is `202 Accepted`
with a job. Its phases are `safety` (the `pre_import` archive of all areas),
`stage`, `stop` (stops the enabled device services and writes the configuration
file when values come from the archive), and `restart` (restarts the
dashboard). The dashboard applies the staged files at its next start and starts
the services again. The outcome is then in `GET /import/result`:

```json
{ "ok": true, "applied": ["settings", "users"], "disabled_users": ["guest-old"],
  "finished_at": "2026-11-08T10:12:44Z" }
```

`code` is empty on success. It is `partial_stats` when some monthly statistics
files were unreadable, `apply_failed` when the restore failed and the previous
state was restored, and `interrupted` when a restart interrupted the restore.
`applied` and `disabled_users` are only present when they have values.

### Error codes

| Code | Status | Meaning |
|---|---|---|
| `backup_forbidden` | 403 | The role for this route is missing |
| `secure_connection_required` | 403 | The route needs HTTPS |
| `csrf_failed` | 403 | `X-CSRF-Token` is missing or wrong |
| `backup_bad_request` | 400 | The body is invalid, the target is unknown or the archive ID is malformed |
| `backup_password_short` | 400 | The password has fewer than 8 characters |
| `password_required` | 409 | The archive is encrypted and needs its password |
| `wrong_password` | 422 | The password does not match the archive |
| `backup_no_location` | 409 | The node has no location for archives |
| `backup_busy` | 409 | Another backup or restore job is running |
| `backup_no_import` | 409 | No archive is waiting for a restore |
| `backup_not_found` | 404 | No archive with this ID |
| `backup_no_space` | 507 | Not enough free space. An upload needs twice its size, or 256 MiB without a length |
| `backup_corrupt` | 422 | The archive is damaged, or the upload is larger than the limit of 256 MiB |
| `backup_incompatible` | 422 | The file is not a backup archive |
| `backup_too_new` | 422 | The archive has a newer format or comes from a newer version |
| `backup_area_unavailable` | 422 | A chosen area is not in the archive |
| `backup_failed` | 500 | Any other error. The message holds the detail |

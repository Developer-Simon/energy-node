---
title: "Dashboard API: Devices and data"
---

# Dashboard API: Devices and data

Part of the [Dashboard API documentation](api.md). The fundamentals there
(base URL, response format, error codes) apply to every endpoint.

In the *Gate* column, `–` means a session is enough. Otherwise the column names
the role or other requirements.

## Devices and entities

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/devices` | – | All devices with entities and live values |
| GET | `/api/v1/devices/{id}` | – | A single device including `warnings`, `command_actions` and `card_refs`. `card_refs` lists the entity IDs of the compact card rows, favourites first, at most three. Without `view_device_analysis` the discovery payloads and last messages are removed, `command_actions` is empty and only the operational warnings are returned |
| GET | `/api/v1/devices/{id}/diagnostics` | – | The device's diagnostic warnings |
| GET | `/api/v1/devices/{id}/activity` | `view_devices` | Recent commands (up to 3, `kind` `command`) and the states of the 3 newest messages (`kind` `message`) as `{"entries": [...]}`. Each entry has `key`, `at`, `kind`, `label`, `value` and `ok`, where `ok` marks a published command. Topics are never included, and `entries` is never `null`. The analysis right does not apply here |
| POST | `/api/v1/devices/{id}/reload` | – | Rebuild the registry completely |
| POST | `/api/v1/devices/{id}/ignore` | CSRF | Hide device |
| POST | `/api/v1/devices/{id}/unignore` | CSRF | Undo hiding |
| GET | `/api/v1/devices/{id}/discovery-delete/preview` | `delete_device_discovery` | What would be deleted? |
| POST | `/api/v1/devices/{id}/discovery-delete` | `delete_device_discovery` + CSRF | Delete discovery topics |
| GET | `/api/v1/devices/ignored` | – | List of hidden devices |
| GET | `/api/v1/discovery` | – | Devices + parse errors + duplicate IDs |
| GET | `/api/v1/discovery/summary` | – | `device_count`, `entity_count`, `discovery_errors`, `duplicate_ids`, with `ETag: "registry-N"` |
| GET | `/api/v1/topics` | – | All known topics |
| GET | `/api/v1/topics/samples` | – | Last payload per topic |
| GET | `/api/v1/automation/notification` | – | Last `{at, message}` event from the automation service's `last_event` topic, parsed. `404` when no automation service, `204` before its first event. Polled by `notifications.js` for toasts. |
| GET | `/api/v1/automation/state` | – | The automation service's full state document (`at`, `online`, `rules`, `events`, counters) from its `state` topic. `404` when no automation service, `204` before the first usable document. Read by the automations page instead of `last_message`, which is capped at 4096 bytes. |
| GET | `/api/v1/automation/history` | – | The live trigger history `{at, rules, rule_count}` from the `history` topic, also when `history_persist` is off. Same `404` and `204` rules as the state route. |
| GET | `/api/v1/events` | `topics, devices` | SSE stream with the registry version and the subscribed topics |
| POST | `/api/v1/entities/{unique_id}/command` | – | Switch an entity or set a value |

**`GET /api/v1/devices`** returns `ETag: "registry-N"`. With a matching
`If-None-Match` the server answers `304 Not Modified` without a body. Use this
when you poll repeatedly.

**`GET /api/v1/events`** is a Server-Sent Events stream. Each event carries the
registry version and one branch per subscribed topic:

```
event: registry
data: {"version":42,"energy":{...},"entities":{...},"entities_delta":true}
```

`topics` is a comma-separated list. Known topics are `energy` (energy
snapshot), `entities` (entity values, full on connect, then only changes with
`entities_delta: true`), `diagnostics` (summary), `devices` (see below) and `energy_stats` (`{"rev":N}`, the revision of the
energy statistics, present when the dashboard has a data directory). An event
arrives when `rev` changes, even if the registry is idle. Unknown names are ignored. Without the parameter you get all topics except `devices`. With an empty parameter (`?topics=`) you get only
`{"version":N}` whenever the registry changes, which is enough if you fetch
`GET /api/v1/devices` yourself.

`devices` (per subscribed device: `structure`, `availability`, `status` and,
with `:auto`, the automatic compact `rows`). Choose the devices with
`devices=a,b:auto`, or `*` / `*:auto` for all of them. A delta carries
`devices_delta: true` and only changed devices, a removed one as `null`. With
`*` the event also carries `devices_list`, the device IDs in display order.
`devices` is not part of the default set.

The server builds a topic only while someone subscribes to it, once per change
for all connections. To change the topics, close the stream and open it again.
The send interval comes from `settings.json` and takes effect without a
reconnect.

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

## Energy

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/energy` | – | Current balance including interpretation |
| GET/PUT | `/api/v1/energy/roles` | – | Role assignment per entity |
| POST | `/api/v1/energy/chains` | edit energy | Create a measurement chain |
| PUT | `/api/v1/energy/chains/{id}` | edit energy | Change a measurement chain |
| DELETE | `/api/v1/energy/chains/{id}` | edit energy | Delete a measurement chain |
| PUT | `/api/v1/energy/chains/{id}/tolerance` | edit energy | Set or clear the fixed normal range of one segment |
| POST | `/api/v1/energy/chains/{id}/relearn` | edit energy | Forget the learned values of one segment |
| GET | `/api/v1/energy/chain-suggestions` | – | Suggested measurement chains `{"suggestions"}` from the correlated power of two devices, an empty list when there are none |
| POST | `/api/v1/energy/chain-suggestions/dismiss` | edit energy | Dismiss a suggested pair `{"a", "b"}` for good, answers 204 |
| GET/PUT | `/api/v1/energy/interpretation` | – | Interpretation parameters |
| GET | `/api/v1/energy/revisions` | – | Revision list for `energy.json` (roles + interpretation) |
| GET | `/api/v1/energy/revisions/{revision}` | – | A single revision |
| POST | `/api/v1/energy/restore` | – | Restore a revision, body `{"revision":"…"}` |
| GET | `/api/v1/energy/stats/hours` | – | Hourly means in W for the history page |
| GET | `/api/v1/energy/stats/chains` | view stats | Measurement chain losses per bucket, `period` and `at` as in `/energy/stats` |
| GET | `/api/v1/energy/efficiency` | view efficiency | Round trip, standby and losses per measurement chain |

Available roles: `pv`, `battery`, `battery_charge`, `battery_discharge`,
`battery_soc`, `grid`, `grid_import`, `grid_export`, `load`, `wallbox`,
`heat_pump`.

`PUT /api/v1/energy/roles` replaces all of `assignments`. If the body has no
`interpretation`, the stored one is kept, so saving only the roles does not
reset the interpretation.

**Measurement chains.** `POST /api/v1/energy/chains` takes
`{"label", "points", "counting", "direction"}` and creates a chain. `points`
run in the normal flow direction, `counting` is the point that counts and
defaults to the first point, `direction` is `auto` (default), `reversed`,
`fixed_down` or `fixed_up`. The ID is a slug of the label, `_2`, `_3` and so on
for a taken one. `PUT /api/v1/energy/chains/{id}` takes the same body and
replaces the chain. `DELETE /api/v1/energy/chains/{id}` removes it. Every
answer is the saved energy config. The role moves to the counting point, the
other points keep no role, and a point that leaves the chain without a role is
listed in `role_needed`. An unknown ID gives 404 `chain_not_found`. A point
that does not measure power, or a point of the energy node itself, gives 400
`energy_roles_rejected` with the key `chain_point_not_power`. After every save
of a chain, the learned values of pairs that no chain uses any more are dropped.

**Tolerances and relearning.** `PUT /api/v1/energy/chains/{id}/tolerance` takes
`{"a", "b", "w", "pct"}` and sets a fixed normal range for the segment between
the neighbouring points `a` and `b`, instead of the learned one. `w` is an
absolute width in W, `pct` a width in percent of the source power, and the
wider of the two applies. A new tolerance for a pair replaces the old one. When
`w` and `pct` are both `null`, the tolerance of the pair is removed. The answer
is the saved energy config. Points that are not neighbours give 400
`energy_roles_rejected`. The tolerances of a chain are listed as `tolerances` in
`chains[]` of `GET /api/v1/energy`, and a segment with a tolerance has
`normal.manual` set to `true`.

`POST /api/v1/energy/chains/{id}/relearn` takes `{"a", "b"}`. It forgets
everything the diagnosis learned about the segment in both directions, and every
finding on it, and answers 204. The segment then learns again from new hours.
Points that are not neighbours give 400 `energy_roles_rejected`. Without the
diagnosis, the node answers 501 `energy_chain_monitor_unavailable`.

**`PATCH /api/v1/energy/roles`** also takes `role_needed: {"<id>": false}`,
which dismisses that entry of `role_needed`. `true` is ignored. A `PUT` on
roles never changes `chains` or `role_needed`.

The balance is also published every 10 s to
`outstation/energy_node/energy/balance`, which the automations use.

**`GET /api/v1/energy/stats/hours?from=<ms>&to=<ms>`** returns hourly means for
the history page. `from` and `to` are Unix times in milliseconds. `from` is
rounded down to the full hour, `to` must be later than `from`, and the span may
be at most 400 days.

```json
{
  "rows": [
    { "series": "role:pv", "ts": 1785999600000, "min": 1520.4, "max": 1520.4, "avg": 1520.4, "n": 1, "u": "W" }
  ]
}
```

Each row is one hour. `ts` is the start of the hour in milliseconds. `min`,
`max` and `avg` are the mean power of that hour in W, so they are equal, and `n`
is 1. The series are:

- `role:pv`, `role:wallbox`, `role:heat_pump`
- `role:grid` (import minus export) and `role:battery` (charge minus discharge)
- `berechnet:hausverbrauch`, the house consumption from `load_total`
- `role:custom:<base>:<id>` for custom categories, except storage categories

An own hour counts only if the node recorded at least 50 minutes of it. Hours
from Home Assistant always count. An hour without data is missing from the rows,
and a series appears only if at least one of its hours is not zero. Rows are
sorted by series and then by time.

| Status | Code | When |
|---|---|---|
| 400 | `energy_stats_invalid_range` | `from` or `to` is missing or not an integer, `to` is not after `from`, or the span is longer than 400 days |
| 501 | `energy_stats_unavailable` | The node runs without a data directory, so there is no statistics store |
| 500 | `energy_stats_failed` | Reading the monthly statistics failed |

**`GET /api/v1/energy/stats/chains?period=<period>&at=<date>`** takes the same
`period` and `at` as `/api/v1/energy/stats` and returns the losses of every
measurement chain per bucket, plus the total of the period. Hours from the
current one on do not count.

```json
{
  "period": "day",
  "at": "2026-11-06",
  "revision": 42,
  "chains": [
    {
      "id": "balkon",
      "label": "Balkon",
      "buckets": [
        { "loss_kwh": 0.012, "standby_kwh": 0.003, "efficiency": 0.91, "source_kwh": 0.4, "has_data": true }
      ],
      "total": { "loss_kwh": 0.25, "standby_kwh": 0.05, "efficiency": 0.8, "source_kwh": 1.6, "has_data": true }
    }
  ]
}
```

- `loss_kwh` is the loss of all segments in both directions, plus the standby
  energy of the first point.
- `standby_kwh` is the standby energy of the first point.
- `efficiency` is the product of the efficiencies of the segments. It is `null`
  when a segment has less than 0.01 kWh of source energy or has no data.
- `source_kwh` is the source energy entering the first segment of the chain,
  both directions summed. It is `0` when `efficiency` is `null`. The statistics
  weight the efficiencies of several chains by it.
- `has_data` is `false` when no hour of the bucket holds data for the chain.
- `revision` is the revision of the energy statistics, as in
  `/api/v1/energy/stats`.

**`GET /api/v1/energy/efficiency`** returns the figures of every measurement
chain for the efficiency view, for the rolling last 30 days, the local month and
the last seven days:

```json
{
  "revision": 42,
  "chains": [
    {
      "id": "balkon",
      "label": "Balkon",
      "round_trip_30d": 0.8,
      "standby_w_30d": 1.5,
      "loss_month_kwh": 0.31,
      "month": "2026-11",
      "days": [
        { "local": "2026-10-31T00:00", "loss_kwh": 0.04, "standby_kwh": 0.001, "efficiency": 0.9, "has_data": true }
      ],
      "curve": { "down": [{ "power_w": 100, "efficiency": 0.88 }], "up": [], "best_down": { "from_w": 80, "to_w": 160 } }
    }
  ]
}
```

- `round_trip_30d` is the energy taken out divided by the energy put in at the
  counting point, over the last 30 days. It is `null` for a counting point
  without a storage role, for a fixed direction, and below 0.1 kWh charged.
- `standby_w_30d` is the mean standby power in W over the last 30 days, `null`
  when no standby was measured.
- `loss_month_kwh` is the loss of the local month so far. `month` is that month
  in local time.
- `days` holds seven days, the oldest first, in the same shape as a bucket of
  `/api/v1/energy/stats/chains`. `local` is the start of the day in local time.
- `curve` is the efficiency curve of the chain from the learned model. `down`
  and `up` list the points `{"power_w", "efficiency"}` of each direction. The
  best span of a direction, the power range with the highest efficiency, is
  `best_down` or `best_up` as `{"from_w", "to_w"}`, and is left out when there is
  none. Without learned values, both lists are empty.

| Status | Code | When |
|---|---|---|
| 400 | `energy_stats_invalid_period` | `/stats/chains` only: the period is unknown or `at` does not match it |
| 403 | `view_forbidden` | The account lacks `view_stats` (`/stats/chains`) or `view_efficiency` (`/efficiency`) |
| 500 | `energy_stats_failed` | Reading the measurement chain store failed |
| 501 | `energy_stats_unavailable` | The node runs without a data directory |

## Learning

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/learning/status` | view automations | State of the learning features: first start, level, usage, and which features are new |
| POST | `/api/v1/learning/reset` | edit setup + CSRF | Deletes everything the learning features have learned, answers `{"ok":true}` |

**`GET /api/v1/learning/status`** returns the state of the three learning
features `chain_suggestions`, `chain_learning` and `auto_patterns`. Each one has
`on` (switched on in the settings), `since` (unix time of the oldest stored data,
0 when there is none) and `devices` (entities or devices with stored data).
`usage` shows the storage of the day profiles: `bytes`, `written_per_day`
(mean of the last seven days), `since` and `devices`. `fresh` is true when the
node had nothing learned at its first start. `setup_done` tells whether the
onboarding is complete. `level` is the resolved level (`eco`, `balanced` or
`full`), `level_setting` the setting as saved (`auto` when empty).
`acknowledged` lists the features the user has confirmed, `new` the features
that are switched on and not yet confirmed. `location_set` is true when the
automation rule document has a site location (latitude and longitude, both not
zero). It comes from the automation rules, not from `GET /api/v1/settings`. Example:

```json
{"fresh":false,"setup_done":true,"level":"eco","level_setting":"auto",
 "features":{"chain_suggestions":{"on":true,"since":1794000000,"devices":9},
             "chain_learning":{"on":true,"since":1794000000,"devices":4},
             "auto_patterns":{"on":true,"since":1794000000,"devices":14}},
 "usage":{"bytes":412000,"written_per_day":96000,"since":1794000000,"devices":14},
 "acknowledged":["chain_learning"],"new":["chain_suggestions","auto_patterns"],
 "location_set":true}
```

**The `learning` block of `GET /api/v1/performance`** shows the compute load of
the learning engine on the performance page. `setting` is the saved level
(`auto` when empty), `level` the resolved level. `share` is the compute share
of one core: the mean of the daily shares recorded for the last seven days, or
the engine's share over the last 24 hours while no daily share is recorded.
`target` the target share of that level
(0.05 on `eco`, 0.10 on `balanced` and `full`). `per_run_ms` is the mean compute
time of one round in the window, and `top` lists the three most expensive
detectors in descending order with their `ms` in the window. `suggest` is only
present when the load stayed under half the target for seven days, and names
the level to move up to. Without the learning dependencies the block is `null`.

The learning engine runs inside the dashboard process, so its load is part of
the dashboard unit. It is not a unit of its own. In `breakdown.dashboard.parts`
it is the part `learning` (`pct`, its share of the whole budget: `share` × 100
divided by the core count), listed last. The other parts shrink by that amount,
so they still add up to the dashboard unit. Its `detectors` lists up to eight
detectors in descending order, each with `id` and `pct` (its share of the whole
budget, the parts of the learning part add up to its `pct`). The learning part is
absent without the learning dependencies, without compute time in the window, or
when the dashboard has no split.

```json
{"setting":"auto","level":"eco","share":0.021,"target":0.05,"per_run_ms":840,
 "top":[{"detector":"switch_time","ms":420},{"detector":"runtime","ms":210}],
 "suggest":"balanced"}
```

```json
{"id":"learning","pct":0.525,"detectors":[{"id":"switch_time","pct":0.29},{"id":"runtime","pct":0.14}]}
```

**`POST /api/v1/learning/reset`** drops the day profiles, the learned loss values
of the measurement chains and the reaction times. Dismissed chain suggestions
stay dismissed. The settings are not changed. The reset needs `edit_setup` and a
CSRF token, and is a POST only.

| Status | Code | When |
|---|---|---|
| 403 | `view_forbidden` | The account lacks `view_automations` (`/learning/status`) |
| 403 | `learning_forbidden` | `/learning/reset`: the account lacks `edit_setup` |
| 403 | `csrf_failed` | `/learning/reset`: the CSRF token is missing or wrong |
| 500 | `learning_reset_failed` | Deleting the learned data failed |
| 500 | `settings_invalid` | `/learning/status`: the settings cannot be read |
| 501 | `learning_unavailable` | The node runs without the learning services |

## History

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
IndexedDB (see [Browser history](data-flow-storage.md#10-browser-history-indexeddb-not-on-the-pi)), and these endpoints only
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

## Configurations (device JSONs)

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
| GET | `/api/v1/automations/findings` | – | Findings of the rule check as `{ "findings": [...] }`, most severe first, at most 20, without the ignored ones. Never `null` |
| POST | `/api/v1/automations/findings/ignore` | `automations` + CSRF | Hides a finding by its `id`, or shows it again with `undo: true`. Answers `204` |
| POST | `/api/v1/automations/findings/preview` | `view_automations` + CSRF | Static findings of a rule document that is not saved yet, as `{ "findings": [...] }`. Body: the rule document. Changes nothing. Never `null` |
| GET | `/api/v1/automations/suggestions` | `view_automations` | Suggestions of the learned habits as `{ "enabled": true, "learning": { "since": 1794000000, "devices": 14, "days": 9 }, "suggestions": [...] }`. `enabled` is `false` while "Muster lernen" is off, then `suggestions` is empty. Devices on the hidden-devices list and devices that do not exist are left out. Never `null` |
| POST | `/api/v1/automations/suggestions/{id}/dismiss` | `automations` + CSRF | Hides a suggestion for good. Answers `{ "ok": true }`. Unknown `id`: `404` `suggestion_unknown` |
| POST | `/api/v1/automations/suggestions/{id}/later` | `automations` + CSRF | Hides a suggestion for seven days. Answers `{ "ok": true }` |
| POST | `/api/v1/automations/suggestions/{id}/undo` | `automations` + CSRF | Shows a dismissed or postponed suggestion again. Answers `{ "ok": true }` |
| GET | `/api/v1/automations/learned` | `view_automations` | Learned values as `{ "enabled": true, "values": [...] }`. `enabled` is `false` while "Muster lernen" or the habit group is off, then the values stay in the list. Values of hidden devices and devices that do not exist are left out. Never `null` |

`learning.since` is the Unix time of the oldest learned day, `learning.days`
the whole days from there to now. Without `automations` the POST answers `403`
`suggestion_forbidden`. A failed save answers `500` `suggestion_save_failed`.

`GET /api/v1/automations/learned` lists the values that rules use in place of a
fixed number, a clock time or a state. The keys are `device/<id>/usual_on` and
`device/<id>/usual_off` (clock times), `device/<id>/usual_runtime_s` (seconds),
`site/day_start` and `site/night_rest` (clock times) and `site/house_state`
(`active` or `quiet`). `kind` is `time`, `duration` or `state`. `device` is empty
for site values. `value` holds the clock text, the seconds or the state, `n` the
number of observed days, `at` the Unix time of the last publication, `prev` the
value before the last change and `changed_at` the Unix time of that change:

```json
{"enabled":true,"values":[{"key":"device/stehlampe/usual_off","device":"stehlampe","kind":"time","value":"22:30","n":12,"at":1794000000,"prev":"22:25","changed_at":1793650000}]}
```

Each value has a `year` list when the season store knows the value. It holds
one entry per week of the last twelve months in which the value was learned,
oldest first, at most 53 entries: `{"week":"2026-W30","value":"22:30"}`. `week`
is the ISO week as `YYYY-Www`. A clock time is text `HH:MM` (the weekly median,
rounded to the minute), any other kind is a number. `year` is left out when no
week has the value. The field is not stored in `learned.json` and not published
over MQTT.

The same values go out as retained MQTT messages on the topic
`outstation/energy_node/learned/<key>`, for example
`outstation/energy_node/learned/device/stehlampe/usual_off`. The payload is
`{"value":"22:30","n":12,"at":1794000000}`, where `value` is the same as above
and `at` is Unix seconds. A value is published when its rounded value changes,
at most every 15 minutes per value. Clock times are rounded to 5 minutes and
durations to 60 seconds. An unchanged value is sent again with a new `at` at
least every 60 minutes, so that rules do not treat it as old. Nothing is
published while the learning is off. An empty retained payload deletes a topic,
which happens when the device of a value is gone or when the learned values are
deleted.

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

`POST /api/v1/automations/findings/ignore` takes the ID of a finding. Each
finding has an `id` such as `frequent:pv-ueberschuss`, and `undo: true` shows
it again:

```json
{ "id": "frequent:pv-ueberschuss", "undo": false }
```

`POST /api/v1/automations/findings/preview` checks the rule document the
Automations page holds, before it is saved. The body is the document itself,
the same JSON that `PUT /api/v1/configurations/automation_rules` takes. The
answer has the same shape as the GET, with the static findings only (no
observed ones), ignored IDs left out, most severe first, at most 20. The
stored document and the ignored file are not touched. The body is capped at
1 MiB:

```json
{ "rules": [ { "id": "pv-ueberschuss", "name": "PV Überschuss", "enabled": true, "conditions": [], "actions": [] } ] }
```

Errors of the findings endpoints:

| Status | Code | When |
|---|---|---|
| 400 | `automation_finding_invalid` | The body cannot be decoded or the `id` is empty (ignore), or the rule document does not parse (preview) |
| 401 | `authentication_required` | The POST has no signed-in user |
| 403 | `automation_findings_forbidden` | The user lacks the `automations` role (ignore) or the `view_automations` role (preview) |
| 403 | `csrf_failed` | The CSRF token is missing or wrong |
| 413 | `body_too_large` | The preview body is over 1 MiB |
| 500 | `automation_findings_save_failed` | The ignored findings could not be saved |
| 501 | `automation_findings_unavailable` | The rule check is not available on this instance, for example without a data directory |

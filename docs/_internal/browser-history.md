---
title: "Browser history: development notes"
---

# Browser history: development notes

Notes for changing the recorder, the store and the compaction
(`history-recorder.js`, `history-store.js`, `history-maintenance.js`,
`history-rollup.js`). What the operator sees is on [History](../dashboard/history.md)
and [Settings: History](../dashboard/settings-history.md), the data flow and the
exchange protocol are in [data flows](../developing/data-flow.md) section 10.

The measurements live only in the browser (IndexedDB). The Go process stores the
configuration and the saved views, never a sample. A desktop and a phone share
the configuration and the views, but each has its own recording.

## Data model

IndexedDB `energy-node-dashboard`, version 2, four object stores:

| Store | keyPath | Row |
|---|---|---|
| `samples_raw` | `['series', 'ts']` | `{series, ts, v, u}` |
| `samples_1m` | `['series', 'ts']` | `{series, ts, min, max, avg, n, u}` |
| `samples_5m` | `['series', 'ts']` | `{series, ts, min, max, avg, n, u}` |
| `meta` | `'key'` | `{key, value}` |

`series` is a role (`role:pv`, `role:battery`, `role:grid`, `role:load`,
`role:wallbox`, `role:heat_pump`) or an entity ID from `history_extra_entities`.
`role:battery_soc` exists when a state of charge entity is assigned. It is the
only role series with `u: '%'`, and it does not take part in the derived
house consumption line, which `history.js` guards through `HISTORY_ROLES`.

**`ts` is epoch milliseconds as a number, not an ISO string.** Only then does
`IDBKeyRange.bound([series, from], [series, to])` give real range queries and
range deletes. With a string every read or cleanup would scan the whole store.
Version 1 (store `energy-samples`, ISO strings) is dropped on the upgrade to
version 2, not migrated. It held six hours of disposable data.

A raw row and a compacted row differ in shape (`v` against `min`, `max`, `avg`
and `n`). `HistoryRollup.normalize()` turns both into the compacted shape, so
the read path and the chart need one code path.

`meta` keys: `raw_compacted_until`, `minute_compacted_until`, `evicted_until`
(epoch ms watermarks, a run only handles the span since the last mark),
`bytes` (estimated byte count) and `recording_paused`.

The hourly resolution is not in the browser. See the history page and
`/api/v1/energy/stats/hours`.

## Compaction

`history-maintenance.js` `compact()` turns everything older than
`history_raw_window_hours` into minute means in `samples_1m`, and everything
older than `history_minute_window_days` into five minute means in `samples_5m`.
The mean is **weighted with `n`** (`HistoryRollup.bucket()`). Without that, a
bucket with one sample counts as much as one with sixty, and the curve drifts
a little more with every tier.

Reading picks a tier from the requested span (`selectTier`, `availableTiers`,
`autoTier` in `history-rollup.js`). Without that, a chart over a month would
load millions of raw rows.

## One writing tab

Only one tab writes and compacts. Two independently polling tabs would write
slightly shifted timestamps, which the key `[series, ts]` does not deduplicate,
and two tabs compacting at once delete rows under each other. The writing tab
holds a Web Locks lease (`energy-node-historizer`, `mode: 'exclusive'`) and the
promise never resolves, so the browser releases the lock when the tab closes and
another tab takes over. Without `navigator.locks` the tab simply becomes leader
without any coordination. The leader runs `collect()` on the sample interval and
`runMaintenance()` every 60 s. The other tabs get a `BroadcastChannel` message
and reload on demand.

## Retention

`history_retention_mode` is `time` or `size`, never both.

- **By time** (`evictByTime`) deletes everything before `now -
  history_retention_hours`. The watermark `evicted_until` saves the pass when
  nothing has expired since the last run, which is the normal case.
- **By size** (`evictBySize`) deletes hour by hour from the oldest row until the
  estimated byte count is below `EVICT_TARGET_RATIO` (90 %) of
  `history_budget_mb`. The hysteresis stops the count from sitting on the limit
  after every run, which would make the next sample start the next cleanup.

Both modes delete in all three tiers (`evictBefore`).

## The byte count

`ROW_BYTES_RAW = 120` and `ROW_BYTES_ROLLUP = 160` in `history-store.js` are
measured estimates per row, including the IndexedDB index overhead. They are the
control variable of the size mode, not exact values.
`navigator.storage.estimate()` returns only a coarsely rounded figure for the
whole origin. It does not work as a control variable, but it is shown on the
settings page as a cross check.

**Measure both numbers again whenever the row shape changes** (new fields on
`{series, ts, v, u}` or on the compacted row). Otherwise the count drifts away
from reality and the size mode cleans too early or too late.

## When the quota is full

A write against a full quota throws `QuotaExceededError`. It is not a temporary
error, every further write fails the same way. The recorder therefore sets
`paused`, writes `recording_paused` into `meta`, and shows the reason in the
status line. `becomeLeader()` reads the flag back, so the pause survives a reload.

`resume()` clears the flag and collects at once. If the cause is still there,
it pauses again. **Nothing in the UI calls `resume()`**, only the tests do. The
message "then resume" in `history.recorder.storage_full` therefore has no
control behind it, and the only ways out are the console or clearing the site
data. See the idea P2.22 in the idea list.

`window.HistoryStore.persist()` asks for `navigator.storage.persist()`. It is a
request, not a promise. The result is in `dashboardHistorizer.status().persisted`
and shows as the "Storage not guaranteed" pill.

## Diagnosing a silent start

Only the leader tab records and exchanges, and `becomeLeader()` starts the
exchange last, after `persist()`, the first `collect()` and the first
`runMaintenance()`. A step that hangs or throws therefore delays or prevents the
exchange without any visible sign except "Not connected" on the settings tile.

Errors of those steps and of the exchange start now reach the console through
`console.error` (prefix `[history]` and `[history-exchange]`). Nothing in the UI
listens to the `dashboard-history-error` event.

The step log is off by default. Switch it on in the browser console, then reload:

```js
dashboardHistorizer.debug(true)   // off again: dashboardHistorizer.debug(false)
```

The flag lives in `localStorage` under `energy-node.history.debug`, because the
steps run while the page loads. With it on, the console shows one line per step
with its duration (`leader step maintenance: done in 2300 ms`), the exchange
start, the hello of the stream and every stream error with the reconnect delay.
Filter the console by `[history`.

## Export

CSV and JSON are built entirely in the browser from the local data
(`history-export.js`). The Pi takes no part in it and gets no copy.

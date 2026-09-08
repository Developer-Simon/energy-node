---
title: "Performance & Resources on the Pi 1 Node"
---

# Performance & Resources on the Pi 1 Node

This document records the node's resource analysis: **how much CPU and RAM the
individual services actually need**, how they were measured, and which
optimizations follow from that.

Measurement: **2026-08-29**, live node (Raspberry Pi 1 Model B Rev 2, ARMv6,
single-core, 512 MB — of which 427 MB usable, the rest GPU reserve). Uptime at
the time of measurement: 11 days. Raw logs are not in the repo; the numbers
below are the distilled state.

This is a **point-in-time snapshot**, not a figure that is kept in sync with the
code. Everything below (service list, rule counts, CPU/RAM shares, the open
items in §5) describes the node on that date; treat it as a baseline to
re-measure against, not as the current state.

---

## 1. Measurement method

| Metric | Source | Why |
|---|---|---|
| CPU per service | `systemctl show <unit> -p CPUUsageNSec` divided by runtime since `ActiveEnterTimestamp` | cgroup-accurate, incl. all child processes — more honest than `ps` `TIME` |
| RAM per process | `Pss:` from `/proc/<pid>/smaps_rollup` | PSS counts shared pages proportionally — the only fair per-process figure |
| Idle baseline | `vmstat 2` over several seconds | averages out short polling spikes |
| Live spikes | `top -b -d 3 -n N` | shows which service is currently burning |

Values are normalized to **"% of one core"** (single-core, so = % of the whole
system) and extrapolated linearly to **7 days**, so that freshly restarted
services become comparable.

---

## 2. CPU — 7-day extrapolation

| Unit | Ø % / 1 core | Projection 7 d | Measurement basis | Assessment |
|---|---:|---:|---|---|
| **dashboard** | **~20–30 %** | **~2000–3000 min** | 75 min, NRestarts=0 | **By far the largest consumer.** Re-measured 2026-08-29 22:35 (see 2.1) — *not* a startup effect, real sustained load |
| tailscaled | 3.19 % | ~321 min | 11 d | Fixed cost (WireGuard crypto, possibly a DERP relay) — hardly reducible |
| **shelly-rpc** | **2.83 %** | **~285 min** | 7.5 d | Largest real consumer **among the bridges.** Spikes up to ~22 % when all devices are polled at once |
| energy-node | 1.12 % | ~113 min | 8 d | Contains the expensive `apt list --upgradable` call on the diagnostic cycle |
| trucki-http | 0.86 % | ~86 min | 8 d | |
| mosquitto | 0.78 % | ~78 min | 11 d | Inconspicuous at ~3–10 msg/s |
| battery-soc | 0.36 % | ~37 min | 28 h | |
| caddy | 0.30 % | ~30 min | 11 d | |
| **automation** | **0.28 %** | **~29 min** | 3.9 h | Third-lightest service. Only **3 rules** (2× `balance_threshold`, 1× `time_window`), evaluated per balance message. **Not an optimization target.** |
| apsystems-ez1 | 0.14 % | ~15 min | 8 d | |
| tuya | 0.11 % | ~11 min | 8 d | |

The 1094 lines of the automation rule engine
([`services/automation/automation_mqtt.py`](../../services/automation/automation_mqtt.py))
are large in the code but irrelevant to load, as long as only a handful of
rules are configured.

### 2.1 dashboard — re-measurement 2026-08-29 22:35

The first value (18 %, 19 min uptime) had been dismissed as a startup artifact.
The re-measurement after **75 min of uninterrupted runtime** (`NRestarts=0`)
disproves that:

| Method | CPU | 7-day projection |
|---|---:|---:|
| Ø since start (868.8 CPU-s / 4509 s) | 19.3 % / 1 core | ~1940 min |
| Delta rate over a 150 s window | **29.8 % / 1 core** | **~3005 min** |

In that window `top` shows the dashboard process as the clear main CPU user
(`us` 57–82 %). This is **not burst load but baseline load** — the dashboard is
the most expensive single process on the node, well ahead of `tailscaled` and
`shelly`.

Observed drivers:

- **`live_update_interval_seconds: 3`** (default) — every connected client
  polls the registry state every 3 s; on a change, a full
  `GET /api/v1/devices` follows (SSE itself only transmits `{version: N}`, see
  [`data-flow.md`](data-flow.md) §4).
- **18 open connections on `:8080`** at the time of measurement. Whether these
  are several real clients or an EventSource reconnect leak from a long-lived
  tab is open — 18 × full serialization of the device list every 3 s explains
  the order of magnitude.
- **Fan-out amplification:** every retained state publication of a bridge
  (shelly ~20 topics / 10 s, balance / 10 s, …) bumps `registry.version`,
  whereupon *every* client re-fetches the complete device list.

---

## 3. RAM — PSS per process

| Process | PSS (MB) | RSS (MB) |
|---|---:|---:|
| dashboard (Go) | 20.2 | 20.2 |
| automation.py | 12.4 | 18.7 |
| shelly.py | 11.9 | 17.1 |
| battery_soc.py | 9.9 | 15.8 |
| trucki.py | 6.4 | 11.8 |
| apsystems.py | 5.1 | 10.5 |
| node.py | 3.6 | 9.0 |
| tuya.py | 2.7 | 7.9 |
| **Σ 7 Python bridges** | **~52** | **~91** |
| caddy | ~25 | 29.6 |
| tailscaled | — | 27.0 |

System state at the time of measurement: **116 MB free**, 153 MB buff/cache
(reclaimable), **74 MB swap used — but stable** (`vmstat` si/so = 0, nothing is
being swapped right now).

---

## 4. Interpretation

- **The sustained-load driver is the dashboard** (~20–30 % of one core,
  re-measurement 2.1) — ahead of `tailscaled` (~3 %) and all bridges combined.
  Originally misjudged as a startup artifact.
- **The rest is bursts**, not baseline load:
  1. **shelly poll** — all 6 devices are queried *simultaneously* per cycle via
     `asyncio.gather` → short spike up to ~22 %.
  2. **`apt list --upgradable`** in the node service — parses the entire APT
     package cache every time, ran on the diagnostic cycle (every ~10 min) and
     was caught at 40–72 % CPU.
- **Without the dashboard**, the sum of all remaining services except
  `tailscaled` is ~6.6 % of one core; `vmstat` then shows 93–98 % idle. With
  the dashboard, idle time drops to 30–50 %.
- **RAM is tight, but not critical.** The swap level is old and stable.
- `tailscaled`, at ~321 min/7 d, is the second-largest item after the
  dashboard, but essentially a fixed cost — a poor cost/benefit ratio for
  tackling it.

---

## 5. Optimization options (sorted by impact)

### 5.1 shelly: reuse HTTP connections — implemented 2026-08-29

[`shelly_rpc_mqtt.py`](../../services/shelly/shelly_rpc_mqtt.py) opened a new TCP
connection on *every* poll (`requests.get`/`requests.post` directly). On ARMv6,
establishing the connection is the most expensive part of a poll.

**Implementation:** a module-wide `requests.Session` with `HTTPAdapter` +
`Retry`. Keep-alive holds the TCP connection open per device; if a pooled
connection drops (device restarted, Wi-Fi gone), urllib3 discards it and builds
a new one on demand, and `Retry(connect≥1, allowed_methods=None)` covers the
case where the drop is only noticed on send — including for the Gen2 RPC POSTs.

**Not changed:** the poll intervals. The 10 s for the fastest channel is
explicitly wanted by the operator and lives in `shelly_devices.json` /
`config.json` anyway, not in the code.

Open (later, optional): stagger the 6 device polls within a cycle instead of
firing them simultaneously → the 22 % spike would become several small ones.

### 5.2 node: decouple `apt list --upgradable` — implemented 2026-08-29

`read_apt_updates_pending()` in
[`energy_node_mqtt.py`](../../services/energy-node/energy_node_mqtt.py)
ran on the diagnostic cycle (every ~10 min).

**Implementation:** a TTL cache around the call, `APT_UPDATES_TTL_S = 86400`
(once per day). Between two real calls, the last determined value is returned. A
failed call is not cached (the next cycle tries again); if a later call fails,
the last good value is passed on instead of flickering to `None`. The
diagnostic cycle still calls the function every ~10 min, but it is now usually
just a dict lookup.

### 5.3 dashboard: reduce live-update load — **now the biggest lever**

Per re-measurement 2.1, the dashboard is the most expensive process on the node
(~20–30 % of one core, sustained). Starting points, roughly by expected impact:

1. **`live_update_interval_seconds` from 3 s to 10 s.** It lives in
   `data/settings.json` and takes effect without a reconnect
   ([`data-flow.md`](data-flow.md) §4). Cuts the poll/serialization rate by a
   factor of ~3.
2. **Clarify the 18 connections on `:8080`.** If this is one client with an
   EventSource reconnect leak (each reconnect leaves the old SSE connection
   open), a fix in the browser JS or a server-side timeout on orphaned SSE
   streams is enough. If there really are that many clients, point 1 applies
   all the more strongly.
3. **Decouple the fan-out.** Currently *every* client re-fetches the complete
   device list on *every* `registry.version` bump. Options: coalesce balance
   and state updates in the server into a single tick (e.g. at most one version
   bump every 2–3 s instead of one per MQTT message), or have the SSE message
   carry the changed entities directly, so that the `GET /api/v1/devices` round
   trip is eliminated.
4. **Check restarts:** `systemctl show energy-node-dashboard -p NRestarts` (0 at
   the time of measurement). Frequent deploys add up to ~180 CPU-s each.

### 5.4 tailscaled

`tailscale status` shows whether the connection is direct or via DERP (relay =
permanently more crypto + copy). Disable unused features (SSH/serve/funnel,
`--accept-routes`). Overall low ROI.

### 5.5 Merge the bridges into one process

7 interpreters → 1 saves roughly **25–35 MB** by PSS and removes 6
idle-polling paho clients. **This is a RAM gain, not a meaningful CPU gain.**
Two ways:

- **One Python process** (threads/asyncio, one paho client): almost zero
  rewrite risk, `tinytuya` and the pytest suites stay. It costs the per-service
  systemd fault isolation (in-process supervision as a replacement).
- **One Go central service with a central scheduler**: net ~30–35 MB PSS,
  enables a staggered poll fan-out and global rate limiting in one place.
  Counter-cost: porting ~7,700 lines of tested Python domain logic, plus
  rebuilding the local protocol for Tuya without a `tinytuya` equivalent. Only
  worthwhile if there is also a wish to standardize on Go long-term. Automation
  would remain a separate process in any case (dashboard-read-only property).

A Go central service is **not urgently** justified by the current state: the
~30 MB alone do not carry the porting effort.

---

## 6. Conclusion

- 5.1 + 5.2 (shelly keep-alive, apt cache) — small, low-risk, one file each,
  **done 2026-08-29**. Addresses the burst load.
- **5.3 (dashboard) is now the actual lever** for more idle time: the sustained
  load is there, not with the bridges. Next step: `live_update_interval_seconds`
  to 10 s and clarify the 18 open `:8080` connections.
- 5.5 (merge the bridges) remains a RAM topic, to be decided separately.

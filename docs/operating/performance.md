---
title: "Performance and resources on the Pi 1 node"
redirect_from:
  - /knowledge/performance-and-resources.html
---

# Performance and resources on the Pi 1 node

This page records how much CPU and RAM each service on the node needs, how
that was measured and which optimisations came out of it.

The measurement was taken on 2026-08-29 on the live node, a Raspberry Pi 1
Model B Rev 2 (ARMv6, single core, 512 MB of which 427 MB are usable and the
rest is reserved for the GPU). The node had been up for 11 days. The raw logs
are not in the repo. The numbers below are a summary.

The numbers describe the node on that date and are not kept in sync with the
code. That applies to the service list, the rule counts, the CPU and RAM shares
and the open items in §5. Use them as a baseline to measure against.

## 1. Measurement method

| Metric | Source | Why |
|---|---|---|
| CPU per service | `systemctl show <unit> -p CPUUsageNSec` divided by runtime since `ActiveEnterTimestamp` | Exact per cgroup, including all child processes, so more accurate than `ps` `TIME` |
| RAM per process | `Pss:` from `/proc/<pid>/smaps_rollup` | PSS splits shared pages between processes, so it is the only fair figure per process |
| Idle baseline | `vmstat 2` over several seconds | Averages out short polling spikes |
| Live spikes | `top -b -d 3 -n N` | Shows which service is busy right now |

Values are given as a percentage of one core. The Pi 1 has a single core, so
that is also the share of the whole system. They are extrapolated linearly to
7 days so that services restarted recently can be compared with the others.

## 2. CPU, extrapolated to 7 days

| Unit | Ø % / 1 core | Projection 7 d | Measurement basis | Assessment |
|---|---:|---:|---|---|
| dashboard | ~20–30 % | ~2000–3000 min | 75 min, NRestarts=0 | By far the largest consumer. Measured again on 2026-08-29 at 22:35 (see 2.1). This is sustained load and not a startup effect |
| tailscaled | 3.19 % | ~321 min | 11 d | Fixed cost (WireGuard crypto, possibly a DERP relay), hard to reduce |
| shelly-rpc | 2.83 % | ~285 min | 7.5 d | Largest consumer among the bridges. Spikes up to ~22 % when all devices are polled at once |
| nodeagent (measured as a separate service, now `internal/nodeagent` in the dashboard) | 1.12 % | ~113 min | 8 d | Runs the expensive `apt list --upgradable` call on the diagnostic cycle |
| trucki-http | 0.86 % | ~86 min | 8 d | |
| mosquitto | 0.78 % | ~78 min | 11 d | Unremarkable at ~3–10 msg/s |
| battery-soc | 0.36 % | ~37 min | 28 h | |
| caddy | 0.30 % | ~30 min | 11 d | |
| automation | 0.28 % | ~29 min | 3.9 h | Third lightest service. Only 3 rules (2× `balance_threshold`, 1× `time_window`), evaluated per balance message. Not worth optimising |
| apsystems-ez1 | 0.14 % | ~15 min | 8 d | |
| tuya | 0.11 % | ~11 min | 8 d | |

The automation rule engine
([`services/automation/automation_mqtt.py`](../../services/automation/automation_mqtt.py))
has 1094 lines, but that does not matter for the load as long as only a handful
of rules are configured.

### 2.1 dashboard: second measurement on 2026-08-29 at 22:35

The first value (18 % after 19 min of uptime) looked like a startup effect. A
second measurement after 75 minutes of uninterrupted runtime (`NRestarts=0`)
showed it was not:

| Method | CPU | 7-day projection |
|---|---:|---:|
| Ø since start (868.8 CPU-s / 4509 s) | 19.3 % / 1 core | ~1940 min |
| Delta rate over a 150 s window | 29.8 % / 1 core | ~3005 min |

In that window `top` shows the dashboard process as by far the largest CPU user
(`us` 57–82 %). The load is constant, not bursty. The dashboard is the most
expensive single process on the node, well ahead of `tailscaled` and `shelly`.

Three causes showed up.

`live_update_interval_seconds` defaults to 3. Every connected client polls the
registry state every 3 s and fetches the full `GET /api/v1/devices` after a
change. SSE itself only sends `{version: N}`, see
[`data-flow.md`](../developing/data-flow.md) §4.

There were 18 open connections on `:8080` during the measurement. It is still
unclear whether these were several real clients or an EventSource reconnect
leak from a tab left open for a long time. Serialising the full device list 18
times every 3 s explains the order of magnitude.

Every retained state message from a bridge (shelly ~20 topics every 10 s,
balance every 10 s and so on) bumps `registry.version`, and then every client
fetches the complete device list again.

## 3. RAM: PSS per process

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

During the measurement the system had 116 MB free and 153 MB in buff/cache
(reclaimable). 74 MB of swap were in use but stable: `vmstat` showed si/so = 0,
so nothing was being swapped at the time.

## 4. Interpretation

The dashboard causes the sustained load (~20–30 % of one core, see 2.1). That
is more than `tailscaled` (~3 %) and all bridges together.

Everything else comes in bursts. The shelly service queries all 6 devices at
the same time each cycle via `asyncio.gather`, which causes a short spike of up
to ~22 %. The node service ran `apt list --upgradable` on the diagnostic cycle
(every ~10 min). It parses the whole APT package cache each time and was seen
at 40–72 % CPU.

Without the dashboard, all other services except `tailscaled` add up to ~6.6 %
of one core, and `vmstat` shows 93–98 % idle. With the dashboard, idle time
drops to 30–50 %.

RAM is tight but not critical. The swap usage is old and stable.

`tailscaled` is the second largest item after the dashboard at ~321 min per 7
days. It is mostly a fixed cost, so reducing it would take a lot of work for
little gain.

## 5. Optimization options (sorted by impact)

### 5.1 shelly: reuse HTTP connections (done 2026-08-29)

[`shelly_rpc_mqtt.py`](../../services/shelly/shelly_rpc_mqtt.py) opened a new TCP
connection on every poll (`requests.get` and `requests.post` called directly).
On ARMv6, setting up the connection is the most expensive part of a poll.

The fix is a module wide `requests.Session` with `HTTPAdapter` and `Retry`.
Keep-alive holds one TCP connection open per device. If a pooled connection
drops (device restarted, Wi-Fi gone), urllib3 discards it and opens a new one
when needed. `Retry(connect≥1, allowed_methods=None)` covers the case where the
drop is only noticed while sending, including the Gen2 RPC POSTs.

The poll intervals stay as they are. The operator wants 10 s for the fastest
channel, and the intervals live in `shelly_devices.json` and `config.json`, not
in the code.

Still open and optional: spread the 6 device polls over the cycle instead of
firing them at once. The 22 % spike would then become several small ones.

### 5.2 node: decouple `apt list --upgradable` (done 2026-08-29)

`read_apt_updates_pending()` in `energy_node_mqtt.py`, which was a separate
service then and is now part of the dashboard as
[`internal/nodeagent`](https://github.com/Developer-Simon/energy-node/tree/main/dashboard/internal/nodeagent),
ran on the diagnostic cycle (every ~10 min).

The fix is a TTL cache around the call with `APT_UPDATES_TTL_S = 86400`, so the
real call runs once per day. In between, the last value is returned. A failed
call is not cached, so the next cycle tries again. If a later call fails, the
last good value is passed on instead of switching to `None`. The diagnostic
cycle still calls the function every ~10 min, but usually that is only a dict
lookup.

### 5.3 dashboard: reduce the live update load

According to 2.1, the dashboard is the most expensive process on the node at
~20–30 % of one core, all the time. These are the options, roughly ordered by
expected effect:

1. Raise `live_update_interval_seconds` from 3 s to 10 s. It lives in
   `data/settings.json` and takes effect without a reconnect
   ([`data-flow.md`](../developing/data-flow.md) §4). That cuts the poll and
   serialisation rate by about a factor of 3.
2. Find out what the 18 connections on `:8080` are. If it is one client with an
   EventSource reconnect leak, where each reconnect leaves the old SSE
   connection open, a fix in the browser JS or a server side timeout for
   orphaned SSE streams is enough. If there really are that many clients,
   point 1 matters even more.
3. Reduce the fan-out. Right now every client fetches the complete device list
   again on every `registry.version` bump. The server could combine balance and
   state updates into one tick, for example at most one version bump every
   2–3 s instead of one per MQTT message. Or the SSE message could carry the
   changed entities itself, which removes the `GET /api/v1/devices` round trip.
4. Check the restarts with `systemctl show energy-node-dashboard -p NRestarts`
   (0 during the measurement). Each deploy costs about 180 CPU seconds, which
   adds up with frequent deploys.

### 5.4 tailscaled

`tailscale status` shows whether the connection is direct or goes through a
DERP relay, which permanently costs more crypto and copying. Unused features
(SSH, serve, funnel, `--accept-routes`) can be turned off. The gain is small.

### 5.5 Merge the bridges into one process

Going from 7 interpreters to 1 saves roughly 25–35 MB of PSS and removes 6 idle
paho clients. That helps RAM but does little for CPU. There are two ways to do
it.

One Python process (threads or asyncio, one paho client) carries almost no
rewrite risk, and `tinytuya` and the pytest suites stay. It gives up the
systemd fault isolation per service, which in-process supervision would have
to replace.

One central Go service with a central scheduler saves ~30–35 MB of PSS net and
allows staggered polls and global rate limiting in one place. It means porting
about 7,700 lines of tested Python domain logic and rewriting the local Tuya
protocol, because Go has no `tinytuya` equivalent. That only pays off if the
project wants to move to Go in the long run anyway. Automation would stay a
separate process either way, because the dashboard only reads its rules.

The current numbers do not make a central Go service urgent. About 30 MB alone
do not justify the porting work.

## 6. Conclusion

5.1 and 5.2 (shelly keep-alive, apt cache) were small, low risk changes of one
file each and were done on 2026-08-29. They deal with the bursts.

More idle time has to come from 5.3, because the sustained load is in the
dashboard and not in the bridges. The next steps are raising
`live_update_interval_seconds` to 10 s and finding out what the 18 open
`:8080` connections are.

5.5 (merging the bridges) is about RAM and needs a separate decision.

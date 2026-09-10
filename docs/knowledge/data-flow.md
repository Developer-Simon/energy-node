---
title: "Data flows in the Energy Node system"
---

# Data flows in the Energy Node system

This document describes **which data is produced where, which channels it
travels through, and who consumes it**. It is deliberately system-wide: the
Python bridges, the MQTT broker, the Go dashboard and the browser all appear
together.

Details about individual building blocks live elsewhere:

- The dashboard's HTTP interface → [dashboard/api-documentation.md](dashboard/api-documentation.md)
- The Python infrastructure package → `libs/energy_node_common/`
- The Mosquitto bridge to the main site → see INSTALLATION.md section 5

---

## 1. The whole picture

Everything runs through **exactly one local MQTT broker** (Mosquitto on the
Raspberry Pi). There is no direct HTTP path between the device bridges and the
dashboard — the broker is the only coupling.

{% raw %}
```mermaid
flowchart TB
    subgraph Physik["Devices / field level"]
        AP["AP Systems EZ1<br/>(local HTTP API)"]
        TR["Trucki stick<br/>(HTTP)"]
        SH["Shelly devices<br/>(RPC / HTTP)"]
        TY["Tuya devices<br/>(local, tinytuya)"]
    end

    subgraph Bridges["Python services (systemd, one process each)"]
        APS["apsystems_ez1_mqtt.py"]
        TRS["trucki_http_mqtt.py"]
        SHS["shelly_rpc_mqtt.py"]
        TYS["tuya_mqtt.py"]
        BAT["battery_soc_mqtt.py<br/>(computes, polls nothing)"]
        AUT["automation_mqtt.py<br/>(rules)"]
    end

    BROKER{{"Mosquitto<br/>localhost:1883"}}

    subgraph Dash["Dashboard (Go, one binary)"]
        MC["mqttclient<br/>Discovery + state"]
        REG[("registry<br/>in-memory state")]
        API["httpapi<br/>/api/v1/*"]
        WEB["webui<br/>server-side HTML"]
        FILES[("data directory<br/>*.json")]
        NODE["nodeagent<br/>(node identity,<br/>Pi diagnostics)"]
    end

    BR["Mosquitto bridge"]
    HS["Main site<br/>(Home Assistant)"]
    BROWSER["Browser"]

    AP --> APS
    TR --> TRS
    SH --> SHS
    TY --> TYS

    APS -->|publish| BROKER
    TRS -->|publish| BROKER
    SHS -->|publish| BROKER
    TYS -->|publish| BROKER
    BAT -->|publish| BROKER
    AUT -->|publish| BROKER
    NODE -->|"outstation/energy_node/… (diagnostics, poll rate, simulation)"| BROKER
    BROKER -->|"subscribe: raw values"| BAT
    BROKER -->|"subscribe: balance, topics"| AUT
    BROKER -->|"config/reload, */set"| APS
    BROKER -->|"config/reload, */set"| SHS
    BROKER -->|"config/reload, */set"| TYS

    BROKER -->|"homeassistant/+/+/+/config<br/>state and availability topics"| MC
    MC --> REG
    REG --> API
    REG --> WEB
    API <--> FILES
    API -->|"publish: command_topic"| BROKER
    API -->|"outstation/energy_node/energy/balance"| BROKER

    WEB -->|HTML| BROWSER
    API <-->|"JSON / SSE"| BROWSER

    BROKER <--> BR
    BR <--> HS
```
{% endraw %}

**Core statement:** device state flows *upward* (device → bridge → broker →
dashboard → browser), commands and configuration flow *downward* (browser →
dashboard → broker → bridge → device).

---

## 2. Topic conventions

Two namespaces, with strictly separated jobs:

| Namespace | Purpose | Retained | Who writes |
|---|---|---|---|
| `homeassistant/{component}/{device_id}/{object_id}/config` | **Discovery** — describes *which* entity exists and where its value is | yes | Python bridges |
| `outstation/{device_id}/...` | **Payload data** — measurements, status, commands | partly | bridges + dashboard |

Important sub-structures below `outstation/`:

| Topic | Direction | Content |
|---|---|---|
| `outstation/{device_id}/{group}/{field}` | bridge → broker | measurement (e.g. `outstation/t2mg81a4e9/field/acpower`) |
| `outstation/{device_id}/status/online` | bridge → broker | availability; also set as the MQTT *last will* |
| `outstation/{device_id}/.../set` | broker → bridge | switch command (`command_topic` from Discovery) |
| `outstation/{service_id}/config/reload` | dashboard → bridge | bridge reloads its JSON configuration |
| `outstation/energy_node/energy/balance` | dashboard → broker | energy balance, every 10 s |
| `outstation/automation/test/set` | dashboard → automation | run a single rule action as a test |
| `outstation/automation/test/result` | automation → broker | result of the test |
| `$SYS/broker/connection/{remote_client_id}/state` | broker → dashboard | is the bridge to the main site up? (`1`/`0`) |

When publishing, the automation service enforces guard rules: no wildcards, no
`homeassistant/...` (no forged Discovery), no `$SYS/...`, and no writes to the
dashboard's own `outstation/energy_node/energy/balance` and
`outstation/energy_node/status/online` topics.

---

## 3. Discovery — how the dashboard learns what exists

The dashboard has **no device list in a configuration file**. It learns
everything at runtime from retained Discovery messages. A restart of the
dashboard is enough, because the broker re-delivers the retained messages.

{% raw %}
```mermaid
sequenceDiagram
    participant B as Python bridge
    participant M as Mosquitto
    participant C as mqttclient
    participant R as registry
    participant U as Browser

    Note over B,M: When the bridge starts
    B->>M: publish retained<br/>homeassistant/sensor/x/y/config
    B->>M: publish outstation/x/status/online = "online"

    Note over C,M: When the dashboard starts / reconnects
    C->>M: subscribe homeassistant/+/+/+/config
    M-->>C: all retained config messages
    C->>R: UpsertEntity(Discovery)
    R-->>C: new state_topic / availability_topic
    C->>M: subscribe to exactly those topics
    M-->>C: retained values
    C->>R: UpdateState / UpdateAvailability
    R->>R: version++
    U->>R: GET /api/v1/events (SSE) sees the new version
```
{% endraw %}

Details that matter day to day:

- The dashboard subscribes **only** to concretely discovered topics, never to
  `outstation/#`. An empty Discovery field means: the dashboard does not see the
  value.
- An **empty** payload on a `config` topic is the deletion: the entity
  disappears, orphaned topics are unsubscribed.
- Parse errors land in `reg.DiscoveryErrors()` and are visible via
  `GET /api/v1/discovery` — they are not silently dropped.
- The `discovery_prefix` is configurable (default `homeassistant`).

---

## 4. Live values all the way to the browser

The browser does **not** get values pushed from MQTT. Between the registry and
the browser sits a deliberately simple version mechanism.

{% raw %}
```mermaid
flowchart LR
    M{{Mosquitto}} -->|"state / availability"| C[mqttclient]
    C --> R[("registry<br/>version: uint64")]
    R -->|"ObserveChanges"| RC[("runtime.json<br/>runtime cache")]
    R -->|"ETag: registry-N"| D["GET /api/v1/devices"]
    R -->|"event: registry<br/>data: version"| S["GET /api/v1/events (SSE)"]
    S -->|"version changed?"| JS["browser JS"]
    JS -->|"only then"| D
    D --> JS
```
{% endraw %}

- **SSE carries no payload data**, only `{"version": N}`. On a change the client
  fetches the devices via `GET /api/v1/devices` — that saves a diff
  implementation on both sides.
- The poll interval of the SSE loop comes from `settings.json`
  (`live_update_interval_seconds`, default 3 s) and takes effect **without a
  reconnect**.
- `GET /api/v1/devices` returns `ETag: "registry-N"`; with a matching
  `If-None-Match` the server answers `304`.
- The **runtime cache** (`runtime.json`) survives restarts: it remembers the
  last seen values and restores them when an entity reappears, so tiles are not
  empty after a restart.

---

## 5. Commands — from the click to the device

{% raw %}
```mermaid
sequenceDiagram
    participant U as Browser
    participant A as httpapi
    participant R as registry
    participant M as Mosquitto
    participant B as Bridge
    participant D as Device

    U->>A: POST /api/v1/entities/UNIQUE_ID/command<br/>body with payload or value
    A->>R: Command(unique_id)
    R-->>A: command_topic, payload_on/off, min/max/step
    A->>A: validate payload (range, step size, allowed values)
    A->>M: publish command_topic
    A->>R: ApplyCommandState (optimistic)
    A-->>U: status published
    M->>B: command on .../set
    B->>D: device access (HTTP/RPC/local)
    B->>M: publish new actual value
    M->>A: state update → registry → version++
```
{% endraw %}

The dashboard publishes **only to `command_topic`s that come from Discovery** —
there is no API path to write an arbitrary topic. The one exception is the
hard-wired automation test on `outstation/automation/test/set`.

Every command is recorded per device in an in-memory history (max. 12 entries)
and appears in `GET /api/v1/devices/{id}` as `command_actions`.

---

## 6. Configuration — files, not a database

There are two separate directories with different ownership.

{% raw %}
```mermaid
flowchart TB
    subgraph DEV["Devices directory (paths.devices_dir)"]
        D1["apsystems_devices.json"]
        D2["shelly_devices.json"]
        D3["tuya_devices.json"]
        D4["battery_soc_devices.json"]
        D5["automation_rules.json"]
        DS["*.schema.json<br/>(read-only)"]
        DR["revisions/ per configuration"]
    end

    subgraph DAT["Data directory (paths.data_dir)"]
        S1["settings.json"]
        S2["layout.json"]
        S3["energy.json"]
        S4["mqtt.json / bridge.json"]
        S5["users.json"]
        S6["runtime.json"]
        S7["ignored_devices.json"]
        S8["*_credentials.json<br/>(passwords, separate)"]
    end

    UI["configuration UI"] -->|"PUT /api/v1/configurations/NAME"| D1
    UI --> D2
    UI --> D3
    UI --> D5
    D1 -->|"schema validation"| OK{"valid?"}
    OK -->|no| REJ["400, file left unchanged"]
    OK -->|yes| WRITE["write atomically<br/>+ store revision"]
    WRITE --> DR
    WRITE --> RELOAD["publish outstation/SERVICE/config/reload"]
    RELOAD --> SVC["bridge reloads<br/>(no process restart)"]

    UI2["dashboard settings"] -->|"PUT /api/v1/settings, /layout, ..."| S1
```
{% endraw %}

Characteristics of the storage path:

- **Writing is atomic** (temp file + `rename`) and stores a revision first —
  every configuration is viewable via `.../revisions` and restorable via
  `.../restore`.
- **The reload runs over MQTT**, not over systemd: the target service swaps its
  configuration in the running process. `automation_rules` maps to the service
  ID `automation`, otherwise `{name}_devices` → `{name}` applies.
- **Passwords always live in their own files** (`*_credentials.json`) and thus
  outside the revision copies. They are never returned in clear text — the API
  reports only `password_configured: true/false`.

---

## 7. Energy balance

The balance is a derived data stream: roles are assigned to entities, and from
the roles a balance object is produced that is available both over HTTP and over
MQTT.

{% raw %}
```mermaid
flowchart LR
    REG[("registry<br/>entity values")] --> RES["energy.Resolver<br/>role per entity"]
    EJ[("energy.json<br/>assignments + interpretation")] --> RES
    RES --> AGG["energy.Aggregate"]
    AGG --> BAL["Balance:<br/>pv, grid_import/export,<br/>battery_charge/discharge,<br/>load_total, wallbox, heat_pump,<br/>battery_soc, self-sufficiency"]
    BAL --> HTTP["GET /api/v1/energy"]
    BAL --> MQTT["publish every 10 s<br/>outstation/energy_node/energy/balance"]
    MQTT --> AUTO["automation_mqtt.py<br/>balance_threshold condition"]
    HTTP --> UI["energy-flow chart"]
```
{% endraw %}

The automation service consumes the balance as *finished numbers* — it computes
nothing itself. If the balance goes stale (older than `balance_max_age_s`,
default 30 s), balance-based rules stop firing.

---

## 8. Automations

{% raw %}
```mermaid
sequenceDiagram
    participant UI as automations editor
    participant A as httpapi
    participant F as automation_rules.json
    participant M as Mosquitto
    participant S as automation_mqtt.py

    UI->>A: PUT /api/v1/configurations/automation_rules<br/>(role automations + CSRF)
    A->>F: validate, revision, write atomically
    A->>M: publish outstation/automation/config/reload
    M->>S: reload
    S->>S: reload rules, re-subscribe topics

    Note over S,M: Running operation
    M-->>S: balance + subscribed topics
    S->>S: check conditions (with hold times)
    S->>M: publish action (allowed prefixes only)

    Note over UI,S: Single test
    UI->>A: POST /api/v1/automations/test<br/>with rule_id and action_index
    A->>M: publish outstation/automation/test/set
    M->>S: test job
    S->>M: publish outstation/automation/test/result
```
{% endraw %}

Condition types: `balance_threshold`, `battery_soc`, `topic_value`,
`time_window`, `entity_value`. Action types: `publish`, `notification`.

---

## 9. Path to the main site

The Mosquitto bridge mirrors both namespaces bidirectionally to the main site
(`topic outstation/# both 0`, `topic homeassistant/# both 0`). This way the main
site sees the same Discovery and measurement topics as the dashboard.

The dashboard **manages** the bridge but is not itself part of the bridge data
path:

{% raw %}
```mermaid
flowchart LR
    UI["bridge UI"] -->|"PUT /api/v1/mqtt/bridge"| BJ[("bridge.json")]
    UI -->|"POST .../apply<br/>(mqtt_config + system_actions)"| REN["mqttbridge.Render"]
    BJ --> REN
    CRED[("mqtt_bridge_credentials.json")] --> REN
    REN --> STG["staged conf in the data directory"]
    STG --> HLP["root helper<br/>energy-node-dashboard-system-action"]
    HLP --> TGT["/etc/mosquitto/conf.d/bridge.conf"]
    HLP --> RST["restart mosquitto<br/>(rollback on error)"]
    TGT -.->|"drift comparison via checksum"| ST["GET /api/v1/mqtt/bridge/status"]
    SYS["$SYS connection state<br/>of the bridge client"] --> ST
```
{% endraw %}

The dashboard never writes directly to `/etc` — it drops a file in its own data
directory and lets a root helper do the rest. The status combines three
independent sources: the systemd service state, the `$SYS` connection state, and
a checksum comparison between the stored and the installed configuration.

---

## 10. Browser history (IndexedDB, not on the Pi)

The dashboard keeps a rolling measurement history, but it lives **in each
browser's IndexedDB** (`energy-node-dashboard`, store version 2) — the Pi
stores nothing. `internal/history` on the server side only defines the data
contract and the retention window; it holds no samples.

{% raw %}
```mermaid
flowchart LR
    subgraph Browser["Browser (one leading tab records)"]
        REC["history-recorder.js<br/>Web Locks leader"]
        IDB[("IndexedDB<br/>samples_raw / _1m / _5m")]
        MNT["history-maintenance.js<br/>compaction"]
        CHART["history.js<br/>chart panel"]
    end
    API1["GET /api/v1/energy"] --> REC
    API2["GET /api/v1/history/entities"] --> REC
    REC --> IDB
    IDB --> MNT --> IDB
    IDB --> CHART
    EX["history-exchange.js"] <-->|"/api/v1/history/exchange/*"| SRV{{"dashboard<br/>relay + 24 h ring buffer"}}
    IDB <--> EX
```
{% endraw %}

- **What is recorded:** the energy roles (`role:pv`, `role:grid_import`, …)
  taken from the balance, plus any individual entities listed in `settings.json`
  under `history_extra_entities`, whose current values the browser polls from
  `GET /api/v1/history/entities` — a server-side allowlist, so the browser
  cannot ask for arbitrary IDs. The sampling interval comes from the dashboard
  settings, not from code.
- **One recorder per browser.** The writing tab holds a Web Locks lease
  (`energy-node-historizer`); the other tabs read only and are notified over a
  `BroadcastChannel`. Two tabs writing slightly offset timestamps would defeat
  the composite key and bloat the store.
- **Three tiers, bounded retention:** `samples_raw` (~6 h), `samples_1m`
  (~7 days), `samples_5m` (~30 days). `history-maintenance.js` rolls raw → 1 m
  → 5 m in the leading tab, advancing a watermark in the `meta` store so a run
  only ever touches the newest span.
- **Device-to-device exchange** (`/api/v1/history/exchange/*`): the server is a
  **relay, not a store** — offers are broadcast, requests and deliveries are
  passed to exactly one peer over an SSE stream, nothing is written to disk. A
  24 h in-memory ring buffer joins as the pseudo-peer `server`, so a single
  browser can still backfill after a reload. Peers announce coverage as coarse
  rasters (counts per time bucket), diff them, and request only the missing
  ranges. Only the `1m` and `5m` tiers are exchanged — never raw, whose offset
  timestamps would not deduplicate. Protocol version 1; limits are 500 rows per
  delivery, 20 000 per request, 1 MiB per body.

---

## 11. What the system deliberately does *not* do

These non-features explain many of the design decisions above:

- **No server-side time-series database.** The Pi's registry holds only the
  *current* state; the rolling history lives in the browser (§10), and any
  long-term archive is the main site's job.
- **No server-side event history.** Command history, the last bridge apply and
  the last Tailscale action live in memory only and are gone after a restart —
  as is the history-exchange ring buffer (§10).
- **No wildcard subscription.** What was not announced via Discovery, the
  dashboard does not see.
- **No direct device access from the dashboard.** The exception is the TinyTuya
  helper, which calls a Python script for the initial setup.

---
title: "Data flows in the Energy Node system"
redirect_from:
  - /knowledge/data-flow.html
anchor_moves:
  3-discovery-how-the-dashboard-learns-what-exists: data-flow-devices.html#3-discovery-how-the-dashboard-learns-what-exists
  the-dashboard-as-a-discovery-publisher: data-flow-devices.html#the-dashboard-as-a-discovery-publisher
  4-live-values-all-the-way-to-the-browser: data-flow-devices.html#4-live-values-all-the-way-to-the-browser
  5-commands-from-the-click-to-the-device: data-flow-devices.html#5-commands-from-the-click-to-the-device
  12-data-flow-on-the-device-map: data-flow-devices.html#12-data-flow-on-the-device-map
  6-configuration-files-not-a-database: data-flow-storage.html#6-configuration-files-not-a-database
  10-browser-history-indexeddb-not-on-the-pi: data-flow-storage.html#10-browser-history-indexeddb-not-on-the-pi
  13-energy-statistics: data-flow-storage.html#13-energy-statistics
  hours-from-home-assistant: data-flow-storage.html#hours-from-home-assistant
  7-energy-balance: data-flow-energy.html#7-energy-balance
  8-automations: data-flow-energy.html#8-automations
  9-path-to-the-main-site: data-flow-energy.html#9-path-to-the-main-site
  14-home-assistant-and-the-energy-node-companion: data-flow-ha.html#14-home-assistant-and-the-energy-node-companion
  where-the-companions-values-come-from: data-flow-ha.html#where-the-companions-values-come-from
  exchange-with-the-dashboard: data-flow-ha.html#exchange-with-the-dashboard
  pairing-and-the-companion-port: data-flow-ha.html#pairing-and-the-companion-port
  the-sidebar-page: data-flow-ha.html#the-sidebar-page
---

# Data flows in the Energy Node system

This page describes which data is produced where, which channels it travels
through and who reads it. It covers the whole system, so the Python bridges,
the MQTT broker, the Go dashboard and the browser all appear together.

Details of the individual parts are documented elsewhere: the dashboard's HTTP
interface in [api.md](api.md), the Python infrastructure package in
`libs/energy_node_common/`, and the Mosquitto bridge to the main site in
section 5 of INSTALLATION.md.

## 1. The whole picture

Device traffic goes through one local MQTT broker (Mosquitto on the Raspberry
Pi). The device bridges and the dashboard have no direct HTTP path between
them.

Home Assistant reaches the node in two ways. It gets the MQTT topics through
the Mosquitto bridge, and with the Energy Node Companion it also talks HTTP to
the dashboard, for the history exchange and for the sidebar page (§14).

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
        EDISC["energydiscovery<br/>(own HA device<br/>Energy Node)"]
        STATS[("energystats<br/>hourly kWh")]
    end

    BR["Mosquitto bridge"]
    subgraph HA["Main site (Home Assistant)"]
        HAMQTT["MQTT integration<br/>+ recorder"]
        COMP["Energy Node Companion<br/>(history peer, panel proxy)"]
    end
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
    EDISC -->|"homeassistant/sensor/energy_node/…/config"| BROKER
    REG --> STATS
    STATS <--> API

    WEB -->|HTML| BROWSER
    API <-->|"JSON / SSE"| BROWSER

    BROKER <--> BR
    BR <--> HAMQTT
    HAMQTT -->|"entity registry,<br/>states, statistics"| COMP
    COMP <-->|"HTTP / SSE<br/>/api/v1/history/exchange/*"| API
    COMP <-->|"panel proxy<br/>(whole dashboard)"| WEB
```
{% endraw %}

Device state flows up (device → bridge → broker → dashboard → browser).
Commands and configuration flow down (browser → dashboard → broker → bridge →
device). Home Assistant closes a loop: it records what the bridge mirrors and
the companion hands parts of that back to the dashboard.

## 2. Topic conventions

There are two namespaces with separate jobs:

| Namespace | Purpose | Retained | Who writes |
|---|---|---|---|
| `homeassistant/{component}/{device_id}/{object_id}/config` | Discovery: which entity exists and where its value is | yes | Python bridges, and the dashboard for its own device `energy_node` |
| `outstation/{device_id}/...` | Payload data: measurements, status, commands | partly | bridges and dashboard |

The main topics below `outstation/`:

| Topic | Direction | Content |
|---|---|---|
| `outstation/{device_id}/{group}/{field}` | bridge → broker | Measurement (for example `outstation/t2mg81a4e9/field/acpower`) |
| `outstation/{device_id}/status/online` | bridge → broker | Availability, also set as the MQTT last will |
| `outstation/{device_id}/.../set` | broker → bridge | Switch command (`command_topic` from Discovery) |
| `outstation/{service_id}/config/reload` | dashboard → bridge | The bridge reloads its JSON configuration |
| `outstation/energy_node/energy/balance` | dashboard → broker | Energy balance, every 10 s, retained |
| `outstation/energy_node/status/online` | dashboard → broker | Availability of the dashboard (`1`/`0`), retained, also the last will |
| `homeassistant/sensor/energy_node/{object_id}/config` | dashboard → broker | Discovery of the dashboard's own energy sensors (§3) |
| `outstation/automation/test/set` | dashboard → automation | Run one rule action as a test |
| `outstation/automation/test/result` | automation → broker | Result of the test |
| `$SYS/broker/connection/{remote_client_id}/state` | broker → dashboard | Whether the bridge to the main site is up (`1`/`0`) |

The automation service refuses to publish to wildcards, to `homeassistant/...`
(no forged Discovery), to `$SYS/...` and to the dashboard's own topics
`outstation/energy_node/energy/balance` and
`outstation/energy_node/status/online`.

## Chapters

The chapters after the overview are on four pages:

- [Devices](data-flow-devices.md): 3. Discovery, 4. Live values, 5. Commands
  and 12. Data flow on the device map.
- [Storage](data-flow-storage.md): 6. Configuration files, 10. Browser history
  and 13. Energy statistics.
- [Energy and automations](data-flow-energy.md): 7. Energy balance,
  8. Automations and 9. Path to the main site.
- [Home Assistant](data-flow-ha.md): 14. Home Assistant and the Energy Node
  Companion.

## 11. What the system does not do on purpose

Several design decisions above follow from these limits.

There is no time series database on the server. The Pi's registry only holds
the current state, and the only thing the Pi keeps over time is the hourly
energy per role (§13). The rolling measurement history lives in the browser
(§10), and a long term archive is the main site's job. With the companion the
main site feeds parts of that archive back (§14).

There is no event history on the server either. Command history, the last
bridge apply and the last Tailscale action live in memory and are lost on a
restart, like the ring buffer of the history exchange (§10).

There is no wildcard subscription. The dashboard does not see what was not
announced through Discovery.

The dashboard does not access devices directly. The only exception is the
TinyTuya helper, which calls a Python script for the initial setup.

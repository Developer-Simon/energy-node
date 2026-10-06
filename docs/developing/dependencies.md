---
title: "Third-party sources"
redirect_from:
  - /knowledge/dependencies.html
---

# Third-party sources

This page lists the third-party code the project runs on. A separate section
lists services whose behaviour was adapted from ideas or documentation in
another open source project without using its code. The exact versions are in
the package files (`pyproject.toml`, `go.mod`, `package.json`). This page says
what each dependency is for and where less obvious implementation choices came
from.

## Python services

Every service depends on `paho-mqtt` (via `libs/energy_node_common`) for its
MQTT connection. Beyond that:

| Package | Used by | What for |
|---|---|---|
| [`paho-mqtt`](https://pypi.org/project/paho-mqtt/) | All services | MQTT client, declared in [`libs/energy_node_common/pyproject.toml`](../../libs/energy_node_common/pyproject.toml) |
| [`apsystems-ez1`](https://pypi.org/project/apsystems-ez1/) | `services/apsystems_ez1/` | Client for the EZ1 microinverter's local REST API |
| [`requests`](https://pypi.org/project/requests/) + [`urllib3`](https://pypi.org/project/urllib3/) | `services/shelly/`, `services/trucki/` | HTTP polling with connection pooling and retry |
| [`tinytuya`](https://pypi.org/project/tinytuya/) | `services/tuya_mqtt/` | Local protocol for Tuya devices without the cloud, including the setup wizard the dashboard calls |

`services/battery_soc/` and `services/automation/` need nothing beyond
`paho-mqtt`. Their logic lives in the internal packages `battery_soc_core` and
`energy_node_common`, which only use the standard library.

## Go: dashboard and installer

| Package | Used by | What for |
|---|---|---|
| [`eclipse/paho.mqtt.golang`](https://github.com/eclipse/paho.mqtt.golang) | `dashboard/` | MQTT client. `gorilla/websocket`, `golang.org/x/net` and `golang.org/x/sync` come in as its dependencies |
| [`pkg/sftp`](https://github.com/pkg/sftp) | `installer/` | File transfer to the target Pi over SSH |
| [`zalando/go-keyring`](https://github.com/zalando/go-keyring) | `installer/` | Keeps the node access details in the OS keychain |
| `golang.org/x/crypto`, `golang.org/x/term` | `installer/` | SSH client and terminal handling for the setup |

`installer/webui/` (`github.com/Developer-Simon/energy-node-webui`) only uses
the standard library.

## Dashboard web UI (JavaScript)

The libraries are vendored into the repository, with no CDN and no build step.
[`dashboard/internal/webui/static/js-deps/THIRD-PARTY-NOTICES.md`](../../dashboard/internal/webui/static/js-deps/THIRD-PARTY-NOTICES.md)
lists them with versions and licenses (Alpine.js, ApexCharts, Choices.js,
Cytoscape.js, flatpickr, GridStack.js, htmx, Popper, Tippy.js).
`installer/webui/` embeds the same build, and its notices file points to that
list.

## Adapted from, not depended on

### APsystems EZ1

[`apsystems-ez1-enhanced`](https://github.com/shopf/apsystems-ez1-enhanced) is
a community maintained Home Assistant integration for the same inverters. Its
documentation of the RAM and flash power limit (`getDefaultMaxPower` /
`setDefaultMaxPower`) and its use of the undocumented `getOutputDataDetail`
endpoint are the basis for the power limit handling and the extended
diagnostics in [`services/apsystems-ez1.md`](../services/apsystems-ez1.md). No
code was copied. Both projects use the same `apsystems-ez1` PyPI package, so
the endpoint behaviour applies directly.

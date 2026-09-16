---
title: "Third-Party Sources"
---

# Third-Party Sources

The third-party code this project runs on, and — separately — cases where a
service's *behavior* was adapted from ideas or documentation in another
open-source project without using its code. Package managers
(`pyproject.toml`, `go.mod`, `package.json`) remain the source of truth for
exact versions; this page exists to name what each dependency is actually
*for*, and to trace a non-obvious implementation choice back to where it came
from.

## Python services

Every service depends on `paho-mqtt` (via `libs/energy_node_common`) for its
MQTT connection. Beyond that:

| Package | Used by | What for |
|---|---|---|
| [`paho-mqtt`](https://pypi.org/project/paho-mqtt/) | all services | MQTT client, declared in [`libs/energy_node_common/pyproject.toml`](../../libs/energy_node_common/pyproject.toml) |
| [`apsystems-ez1`](https://pypi.org/project/apsystems-ez1/) | `services/apsystems_ez1/` | client for the EZ1 microinverter's local REST API |
| [`requests`](https://pypi.org/project/requests/) + [`urllib3`](https://pypi.org/project/urllib3/) | `services/shelly/`, `services/trucki/` | HTTP polling with connection pooling/retry |
| [`tinytuya`](https://pypi.org/project/tinytuya/) | `services/tuya_mqtt/` | local (non-cloud) protocol for Tuya devices, including the device-setup wizard the dashboard shells out to |

`services/battery_soc/` and `services/automation/` have no third-party
dependency beyond `paho-mqtt`; their logic lives in the stdlib-only internal
packages `battery_soc_core` and `energy_node_common`.

## Go: dashboard and installer

| Package | Used by | What for |
|---|---|---|
| [`eclipse/paho.mqtt.golang`](https://github.com/eclipse/paho.mqtt.golang) | `dashboard/` | MQTT client (direct dependency; `gorilla/websocket`, `golang.org/x/net` and `golang.org/x/sync` come in transitively) |
| [`pkg/sftp`](https://github.com/pkg/sftp) | `installer/` | file transfer to the target Pi over SSH |
| `golang.org/x/crypto`, `golang.org/x/term` | `installer/` | SSH client and terminal handling for the provisioning flow |

`installer/webui/` (`github.com/Developer-Simon/energy-node-webui`) has no
external dependency — standard library only.

## Dashboard web UI (JavaScript)

Vendored directly, no CDN and no build step — see
[`dashboard/internal/webui/static/js-deps/THIRD-PARTY-NOTICES.md`](../../dashboard/internal/webui/static/js-deps/THIRD-PARTY-NOTICES.md)
for the full list with versions and licenses (Alpine.js, ApexCharts,
Choices.js, Cytoscape.js, flatpickr, GridStack.js, htmx, Popper, Tippy.js).
`installer/webui/` embeds the same build; its own notices file points back
here rather than duplicating the table.

## Adapted from, not depended on

### APsystems EZ1

- [`apsystems-ez1-enhanced`](https://github.com/shopf/apsystems-ez1-enhanced)
  — a community-maintained Home Assistant integration for the same inverter
  family. Its documented RAM-vs-flash power-limit behavior
  (`getDefaultMaxPower` / `setDefaultMaxPower`) and its use of the
  undocumented `getOutputDataDetail` endpoint informed the power-limit
  handling and extended diagnostics described in
  [`services/apsystems-ez1.md`](../services/apsystems-ez1.md). No code was
  copied — both projects use the same underlying `apsystems-ez1` PyPI
  package, so the endpoint behavior applies directly.

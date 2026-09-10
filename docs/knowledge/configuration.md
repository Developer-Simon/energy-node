---
title: "Central configuration file `/etc/energy-node/config.json`"
---

# Central configuration file `/etc/energy-node/config.json`

The central configuration file replaces the previously scattered environment variables: instead of seven different `*.env` files, there is now a single JSON document that holds all the values needed by the Python services, the Go dashboard, and the system components.

## What the file is for

Previously the configuration was spread across seven files:
- `services/apsystems_ez1/apsystems.env`
- `services/automation/automation.env`
- `services/battery_soc/battery_soc.env`
- `services/shelly/shelly_rpc.env`
- `services/trucki/trucki.env`
- `services/tuya_mqtt/tuya.env`
- `dashboard/energy_node_dashboard.env`

This led to duplication (for example `MQTT_HOST` in all seven files), naming inconsistencies, and maintenance problems. The central file creates a **single source of truth** for 49 values. It is mandatory — there is no fallback to environment variables.

## Location and override

The file is located at `/etc/energy-node/config.json`.

The directory and the file are owned by user `root` and group `energynode`:
- **Directory:** `root:energynode` with permissions `0755`
- **File:** `root:energynode` with permissions `0664`

For local tests or non-standard paths, the `--config <path>` parameter can be passed when starting a Python service or the dashboard, for example:
```bash
.venv/bin/python3 -m src.shelly.shelly_rpc_mqtt --config /home/test/my-config.json
```

The Go dashboard server accepts `-config`:
```bash
./cmd/dashboard/dashboard -config /path/to/config.json
```

## All fields

### Structure and mapping

The file follows this JSON structure:

```json
{
  "schema_version": 2,
  "mqtt": { "host": "...", "port": 1883, "username": "...", "password_file": "..." },
  "paths": { "devices_dir": "...", "data_dir": "...", "services_version_file": "..." },
  "logging": { "level": "INFO" },
  "services": { "apsystems": {...}, "battery_soc": {...}, "shelly": {...}, "trucki": {...}, "tuya": {...}, "automation": {...} },
  "dashboard": { "bind_address": "...", "port": 8080, "client_id": "...", "device_identifier": "...", "log_level": "info", "sweep_interval_seconds": 300, "admin_username": "...", "admin_password_file": "...", "tls": {...}, "system_action_helper": "...", "mosquitto_bridge_target": "...", "node_device_id": "...", "node_device_name": "...", "node_poll_interval_s": 60, "node_diagnostic_poll_multiplier": 10 },
  "tailscale": { "bin": "...", "status_timeout_s": 10 },
  "tinytuya": { "probe_python": "...", "probe_script": "...", "probe_timeout_s": 30 }
}
```

### Fields by purpose

| Field | Type | Read by | Meaning |
|---|---|---|---|
| `schema_version` | Integer | Python, Go | Version control of the file; must be 2 |
| | | | |
| **MQTT broker (shared)** | | | |
| `mqtt.host` | String | Python, Go | IP or hostname of the broker |
| `mqtt.port` | Integer | Python, Go | Broker port (normally 1883) |
| `mqtt.username` | String | Python, Go | Username for broker authentication |
| `mqtt.password_file` | String | Python, Go | Path to the file containing the broker password |
| | | | |
| **Paths (base directories)** | | | |
| `paths.devices_dir` | String | Python, Go | Base path for `*_devices.json` and other device configurations |
| `paths.data_dir` | String | Go | Path for `settings.json`, `mqtt.json`, `bridge.json`, `layout.json` (dashboard operating state) |
| `paths.services_version_file` | String | Go | Optional: path to the `services/VERSION` file deployed by `scripts/deploy/deploy_src_to_remote.sh`, shown on the settings page. Empty/missing = not configured. |
| | | | |
| **Logging** | | | |
| `logging.level` | String | Python | Log level: DEBUG, INFO, WARNING, ERROR, CRITICAL |
| | | | |
| **Services (service-specific values)** | | | |
| `services.<name>.service_id` | String | Python | unique ID of the service (e.g. `apsystems`, `shelly`) |
| `services.<name>.poll_interval_s` | Integer | Python | poll interval of this service in seconds |
| `services.<name>.diagnostic_poll_multiplier` | Integer | Python | factor for diagnostic polls of this service |
| `services.<name>.http_timeout_s` | Integer | Python | HTTP timeout for HTTP-based services (Shelly, Trucki) |
| | | | |
| **Dashboard (Go)** | | | |
| `dashboard.bind_address` | String | Go | bind address (normally `0.0.0.0` for both local and network access) |
| `dashboard.port` | Integer | Go | HTTP port of the dashboard (default: 8080) |
| `dashboard.client_id` | String | Go | unique MQTT client ID of the dashboard |
| `dashboard.device_identifier` | String | Go | device identifier for the dashboard's MQTT communication |
| `dashboard.log_level` | String | Go | log level: debug, info, warn, error |
| `dashboard.sweep_interval_seconds` | Integer | Go | interval for periodic UI refreshes (default: 300) |
| `dashboard.admin_username` | String | Go | admin username for dashboard access |
| `dashboard.admin_password_file` | String | Go | path to the file containing the admin password |
| `dashboard.tls.cert_file` | String | Go | path to the TLS certificate file (empty = no TLS) |
| `dashboard.tls.key_file` | String | Go | path to the TLS key file (empty = no TLS) |
| `dashboard.system_action_helper` | String | Go | path to the helper program for system actions (e.g. reboot) |
| `dashboard.mosquitto_bridge_target` | String | Go | target path for `bridge.conf` on the target device |
| `dashboard.node_device_id` | String | Go | unique ID of the central node (must be `energy_node`); read by `internal/nodeagent` |
| `dashboard.node_device_name` | String | Go | human-readable name for display |
| `dashboard.node_poll_interval_s` | Integer | Go | poll interval of the node in seconds (default: 60) |
| `dashboard.node_diagnostic_poll_multiplier` | Integer | Go | factor for the diagnostic poll interval (default: 10) |
| | | | |
| **Tailscale integration** | | | |
| `tailscale.bin` | String | Go | path to the `tailscale` binary |
| `tailscale.status_timeout_s` | Integer | Go | timeout for `tailscale status` queries |
| | | | |
| **TinyTuya probe** | | | |
| `tinytuya.probe_python` | String | Python | path to the Python interpreter for the TinyTuya probe |
| `tinytuya.probe_script` | String | Python | path to the TinyTuya probe script |
| `tinytuya.probe_timeout_s` | Integer | Python | timeout for the probe in seconds |

## Conventions

The file follows fixed conventions for paths and file names:

| Purpose | Convention | Example |
|---|---|---|
| Device configuration of a service | `{paths.devices_dir}/{name}_devices.json` | `{devices_dir}/shelly_devices.json` |
| Schema for device configuration | `{paths.devices_dir}/{name}_devices.schema.json` | `{devices_dir}/shelly_devices.schema.json` |
| Automation rules | `{paths.devices_dir}/automation_rules.json` | `{devices_dir}/automation_rules.json` |
| Shelly presets | `{paths.devices_dir}/shelly_presets.json` | `{devices_dir}/shelly_presets.json` |
| Dashboard operating state | `{paths.data_dir}/{settings,mqtt,bridge,layout}.json` | `{data_dir}/settings.json` |
| MQTT password | `{mqtt.password_file}` | `/etc/energy-node/mqtt.pw` |
| Admin password | `{dashboard.admin_password_file}` | `/etc/energy-node-dashboard/auth.pw` |

The `<name>` key under `services` is consistent with the first part of the corresponding file names. A service must be defined under `services` in order to start — there is no implicit default name.

## Credentials

### Why passwords live in separate files

Passwords are **not** stored in `config.json` itself, only as file paths in the `*_file` fields:
- `mqtt.password_file` for the broker password
- `dashboard.admin_password_file` for the dashboard's admin password

**Reason for the ownership change:** Previously systemd read the `*.env` files as root and passed the values to a process started as `energynode`. Without `EnvironmentFile`, the process must now open the password file itself — so the `energynode` group needs read permission.

### Password files: permissions and content

**MQTT password** (`/etc/energy-node/mqtt.pw`):
- Owner: `root:energynode`
- Permissions: `0640`
- Content: only the password itself, with no trailing whitespace

**Admin password** (`/etc/energy-node-dashboard/auth.pw`):
- Owner: `root:energynode`
- Permissions: `0640`
- Content: only the password itself, with no trailing whitespace

Example of how to create one:
```bash
sudo install -o root -g energynode -m 0640 /dev/null /etc/energy-node/mqtt.pw
echo -n "my_mqtt_password" | sudo tee /etc/energy-node/mqtt.pw
```

### Path allowlist when writing

When saving, the dashboard checks every `*_file` field against an allowlist. Only paths below the following directories are permitted:
- `/etc/energy-node/`
- `/etc/energy-node-dashboard/`

A missing or unreadable path is a startup error with a descriptive message.

## Reload versus restart

The file contains two classes of values:

| Reloadable (no restart) | Restart required |
|---|---|
| `logging.level` | `mqtt.*` (host, port, authentication) |
| `services.*.poll_interval_s` | `paths.*` (files, directories) |
| `services.*.diagnostic_poll_multiplier` | `dashboard.node_device_id`, `services.*.service_id` |
| `services.*.http_timeout_s` | `dashboard.port`, `.bind_address`, `.tls.*` |
| `tinytuya.*`, `tailscale.*` | `dashboard.admin_*` |

**The dividing line:** A value belongs on the right-hand side if it determines the identity of a connection, a topic, or a listening port.

### Reload sequence

When the dashboard changes a reloadable value:

1. It writes `config.json` atomically (to a temp file, then `rename`) and stores a revision in `data_dir/revisions/`.
2. For each affected service, a message is published on `outstation/<service_id>/config/reload`.
3. The service loads the new configuration; on an error it keeps the old values and reports `runtime_status: rejected` with a reason.
4. If the change includes a field from the right-hand column, the dashboard shows "restart required" together with the list of units.

The restart remains an explicit action performed through the dashboard interface.

## `schema_version`

The integer `schema_version` is currently set to `2`. Every service (Python and Go) checks at startup that this version number matches the one it knows. A mismatch is a startup error:

```
error loading config.json: schema_version 3 found, but only 2 supported
```

A `schema_version` of `1` is rejected with a dedicated hint: version 2 dissolved the former top-level `node` block into flat `dashboard.node_*` fields, so an old file needs that block moved before it loads.

This concept ensures that a deployment in which the dashboard and the Python services come from different versions of the repository is noticed immediately — instead of surfacing as a subtle misconfiguration.

## Error handling

| Error case | Behavior |
|---|---|
| File missing at the path | Startup error: names the expected path and the `--config` parameter |
| Invalid JSON (syntax) | Startup error: names line and column |
| Wrong field type (e.g. string instead of integer) | Startup error: names the JSON path and the expected type |
| Wrong or mismatched `schema_version` | Startup error: names the expected and found version |
| Referenced password file missing or unreadable | Startup error: names the file |
| Required service missing under `services` | Startup error: names the expected key |
| Runtime reload fails | The error is logged, the old values stay active, the dashboard shows `runtime_status: rejected` with a reason |
| Dashboard cannot write `config.json` | HTTP error with a descriptive reason, the file stays unchanged |

**Principle:** "Fail-closed" at startup — the service does not start without a valid configuration. At runtime, a faulty change is rejected and the previous state is kept.

## Exceptions

### The Mosquitto bridge stays in `bridge.conf`

The Mosquitto MQTT bridge is still configured through the file `/etc/mosquitto/conf.d/bridge.conf` in ini format, not through JSON. The reason: Mosquitto reads this file, not `config.json`.

`config.json` only contains the target path (`dashboard.mosquitto_bridge_target`) so that the dashboard knows where to write the configuration rendered from `bridge.json`. The bridge configuration itself is maintained in `bridge.json` or through the dashboard, not by any location in `config.json`.

### Operating state stays in separate files

The files `settings.json`, `mqtt.json`, and `bridge.json` in `data_dir` are the operating state and do not belong to `config.json`. They contain settings that the user saves at runtime through the dashboard:
- `settings.json`: general dashboard settings
- `mqtt.json`: MQTT settings (broker alternatives). Two optional fields beyond the broker connection:
  - `metrics`: map `metric-name → bool`. A missing key means the metric is published (default on); setting a key to `false` stops the dashboard's node agent from publishing that individual system metric.
  - `simulation_active`: bool, default `false`. When `true`, the dashboard retained-broadcasts simulation mode to all bridges on `outstation/energy_node/settings/simulation_active/set`.
- `bridge.json`: Mosquitto bridge settings

This separation stays in place — `config.json` is only for settings that are set by the admin (locally or via deployment), not for runtime operating state.

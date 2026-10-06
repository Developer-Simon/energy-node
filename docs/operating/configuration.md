---
title: "Central configuration file `/etc/energy-node/config.json`"
redirect_from:
  - /knowledge/configuration.html
---

# Central configuration file `/etc/energy-node/config.json`

One JSON document holds all values the Python services, the Go dashboard and
the system components need.

## What the file is for

The file is the single place for 49 values that used to be spread over seven
`*.env` files, one per service plus one for the dashboard. Those files repeated
values such as `MQTT_HOST` and named them inconsistently. The file is
mandatory. There is no fallback to environment variables.

## Location and override

The file is located at `/etc/energy-node/config.json`.

The directory and the file belong to user `root` and group `energynode`. The
directory has mode `0755`, the file `0664`.

For local tests or other paths, pass `--config <path>` when you start a Python
service, for example:
```bash
.venv/bin/python3 -m src.shelly.shelly_rpc_mqtt --config /home/test/my-config.json
```

The Go dashboard takes `-config`:
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
  "paths": { "devices_dir": "...", "data_dir": "..." },
  "logging": { "level": "INFO" },
  "services": { "apsystems": {...}, "battery_soc": {...}, "shelly": {...}, "trucki": {...}, "tuya": {...}, "automation": {...} },
  "installed_services": { "apsystems": true, "automation": true, "battery_soc": true, "shelly": true, "tailscale": true, "trucki": true, "tuya": true },
  "dashboard": { "bind_address": "...", "port": 8080, "client_id": "...", "device_identifier": "...", "log_level": "info", "sweep_interval_seconds": 300, "admin_username": "...", "admin_password_file": "...", "tls": {...}, "system_action_helper": "...", "mosquitto_bridge_target": "...", "node_device_id": "...", "node_device_name": "...", "node_poll_interval_s": 60, "node_diagnostic_poll_multiplier": 10 },
  "tailscale": { "bin": "...", "status_timeout_s": 10 },
  "tinytuya": { "probe_python": "...", "probe_script": "...", "probe_timeout_s": 30 }
}
```

### Fields by purpose

| Field | Type | Read by | Meaning |
|---|---|---|---|
| `schema_version` | Integer | Python, Go | Version of the file format. Must be 2 |
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
| `paths.services_version_file` | String | Go | Ignored. Accepted so older config files stay valid. Versions are shown under Settings > Versions. |
| | | | |
| **Logging** | | | |
| `logging.level` | String | Python | Log level: DEBUG, INFO, WARNING, ERROR, CRITICAL |
| | | | |
| **Services (service-specific values)** | | | |
| `services.<name>.service_id` | String | Python | Unique ID of the service (for example `apsystems`, `shelly`) |
| `services.<name>.poll_interval_s` | Integer | Python | Poll interval of this service in seconds |
| `services.<name>.diagnostic_poll_multiplier` | Integer | Python | Factor for the diagnostic polls of this service |
| `services.<name>.http_timeout_s` | Integer | Python | HTTP timeout for the HTTP based services (Shelly, Trucki) |
| | | | |
| **Installed services (optional)** | | | |
| `installed_services.<key>` | Boolean | Go | One boolean per service (`apsystems`, `automation`, `battery_soc`, `shelly`, `tailscale`, `trucki`, `tuya`). The dashboard hides the service's tab or subpage when it is `false`. A missing block or a missing key means the service is on, as everywhere else in this file. The installer's bootstrap step `65-dashboard-config.sh` writes it from the service selection. Do not edit it by hand, because the next install or redeploy overwrites it. |
| | | | |
| **Dashboard (Go)** | | | |
| `dashboard.bind_address` | String | Go | Bind address (usually `0.0.0.0` for local and network access) |
| `dashboard.port` | Integer | Go | HTTP port of the dashboard (default: 8080) |
| `dashboard.client_id` | String | Go | Unique MQTT client ID of the dashboard |
| `dashboard.device_identifier` | String | Go | Device identifier for the dashboard's MQTT communication |
| `dashboard.log_level` | String | Go | Log level: debug, info, warn, error |
| `dashboard.sweep_interval_seconds` | Integer | Go | Interval for periodic UI refreshes (default: 300) |
| `dashboard.admin_username` | String | Go | Admin username for dashboard access |
| `dashboard.admin_password_file` | String | Go | Path to the file containing the admin password |
| `dashboard.tls.cert_file` | String | Go | Path to the TLS certificate file (empty means no TLS) |
| `dashboard.tls.key_file` | String | Go | Path to the TLS key file (empty means no TLS) |
| `dashboard.system_action_helper` | String | Go | Path to the helper program for system actions such as reboot |
| `dashboard.mosquitto_bridge_target` | String | Go | Target path for `bridge.conf` on the target device |
| `dashboard.node_device_id` | String | Go | Unique ID of the central node (must be `energy_node`), read by `internal/nodeagent` |
| `dashboard.node_device_name` | String | Go | Display name of the node |
| `dashboard.node_poll_interval_s` | Integer | Go | Poll interval of the node in seconds (default: 60) |
| `dashboard.node_diagnostic_poll_multiplier` | Integer | Go | Factor for the diagnostic poll interval (default: 10) |
| | | | |
| **Tailscale integration** | | | |
| `tailscale.bin` | String | Go | Path to the `tailscale` binary |
| `tailscale.status_timeout_s` | Integer | Go | Timeout for `tailscale status` queries |
| | | | |
| **TinyTuya probe** | | | |
| `tinytuya.probe_python` | String | Python | Path to the Python interpreter for the TinyTuya probe |
| `tinytuya.probe_script` | String | Python | Path to the TinyTuya probe script |
| `tinytuya.probe_timeout_s` | Integer | Python | Timeout for the probe in seconds |

## Conventions

Paths and file names follow fixed conventions:

| Purpose | Convention | Example |
|---|---|---|
| Device configuration of a service | `{paths.devices_dir}/{name}_devices.json` | `{devices_dir}/shelly_devices.json` |
| Schema for device configuration | `{paths.devices_dir}/{name}_devices.schema.json` | `{devices_dir}/shelly_devices.schema.json` |
| Automation rules | `{paths.devices_dir}/automation_rules.json` | `{devices_dir}/automation_rules.json` |
| Shelly presets | `{paths.devices_dir}/shelly_presets.json` | `{devices_dir}/shelly_presets.json` |
| Dashboard operating state | `{paths.data_dir}/{settings,mqtt,bridge,layout}.json` | `{data_dir}/settings.json` |
| MQTT password | `{mqtt.password_file}` | `/etc/energy-node/mqtt.pw` |
| Admin password | `{dashboard.admin_password_file}` | `/etc/energy-node-dashboard/auth.pw` |

The `<name>` key under `services` matches the first part of the service's file
names. A service only starts when it is defined under `services`. There is no
implicit default name.

## Service manifests delivery

The installer delivers the service manifests. The bundle carries the complete
set from `services/*/manifest.json`, and on every run bootstrap step 60
(`scripts/bootstrap/60-node-install.sh`) installs it as
`/etc/energy-node/manifests/<service_id>.json` and removes orphaned manifests.
The set matches the `services` block in `config.json`, and `energy_node_common`
checks that on the node in both directions.

## Credentials

### Why passwords live in separate files

`config.json` holds no passwords, only the paths to them in the `*_file`
fields: `mqtt.password_file` for the broker password and
`dashboard.admin_password_file` for the dashboard's admin password.

The units have no `EnvironmentFile`. Each process opens its password file
itself, so the `energynode` group needs read permission.

### Password files: permissions and content

The MQTT password (`/etc/energy-node/mqtt.pw`) and the admin password
(`/etc/energy-node-dashboard/auth.pw`) both belong to `root:energynode` with
mode `0640`. Each contains only the password, without trailing whitespace.

To create one:
```bash
sudo install -o root -g energynode -m 0640 /dev/null /etc/energy-node/mqtt.pw
echo -n "my_mqtt_password" | sudo tee /etc/energy-node/mqtt.pw
```

### Path allowlist when writing

When saving, the dashboard checks every `*_file` field against an allowlist.
Only paths below `/etc/energy-node/` and `/etc/energy-node-dashboard/` are
allowed.

A missing or unreadable path is a startup error with a message that names it.

## Reload versus restart

Some values can be reloaded at runtime, others need a restart:

| Reloadable (no restart) | Restart required |
|---|---|
| `logging.level` | `mqtt.*` (host, port, authentication) |
| `services.*.poll_interval_s` | `paths.*` (files, directories) |
| `services.*.diagnostic_poll_multiplier` | `dashboard.node_device_id`, `services.*.service_id` |
| `services.*.http_timeout_s` | `dashboard.port`, `.bind_address`, `.tls.*` |
| `tinytuya.*`, `tailscale.*` | `dashboard.admin_*` |

A value needs a restart when it defines the identity of a connection, a topic
or a listening port.

### Reload sequence

When the dashboard changes a reloadable value:

1. It writes `config.json` atomically (to a temp file, then `rename`) and stores a revision in `data_dir/revisions/`.
2. It publishes a message on `outstation/<service_id>/config/reload` for each affected service.
3. The service loads the new configuration. On an error it keeps the old values and reports `runtime_status: rejected` with a reason.
4. If the change includes a field from the right-hand column, the dashboard shows "restart required" with the list of units.

You start the restart yourself in the dashboard.

The service reports `error_code` and two checksums. `config_revision` is the
SHA-256 of the file from its last load attempt, successful or not.
`applied_revision` is the SHA-256 of the file it currently runs with. A
rejection stays until a reload succeeds. A service that starts with an invalid
file keeps running in `rejected` and waits for `config/reload`.

## `schema_version`

`schema_version` is `2`. Every service, Python and Go, checks at startup that
the number matches the one it knows. A mismatch is a startup error:

```
error loading config.json: schema_version 3 found, but only 2 supported
```

The dashboard migrates a `schema_version` of `1` on startup. Version 2 replaced
the top-level `node` block with flat `dashboard.node_*` fields, so
`internal/appconfig` converts an old file in memory while loading it. It moves
the four remaining `node.*` fields, drops `node.managed_bridges`, sets
`node_device_id` to `energy_node` and, in files older than the rename of
`device_id` to `service_id`, renames that key in each `services.*` entry.

The dashboard then saves the migrated file through the privileged system action
helper (`apply-app-config`). The helper backs up the old file to
`/etc/energy-node/.config.json.bak` before installing the new one. A direct
write would fail, because `60-node-install.sh` creates `/etc/energy-node` with
mode `0755` and the service group cannot create files there. If the helper is
not available (not installed or no sudoers entry), the dashboard still starts
on the config in memory, logs a warning and tries again on the next start. The
Python services do not migrate. They reject `schema_version 1` and expect the
dashboard to have upgraded the file, so restart them after it has.

The version check makes a deployment with dashboard and Python services from
different versions of the repository fail at once, so it cannot show up later
as a hard to find misconfiguration.

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

At startup a service fails closed and does not start without a valid
configuration. At runtime a faulty change is rejected and the previous state
stays active.

## Exceptions

### The Mosquitto bridge stays in `bridge.conf`

The Mosquitto bridge is configured in `/etc/mosquitto/conf.d/bridge.conf`,
because Mosquitto reads that file and not `config.json`.

`config.json` only contains the target path (`dashboard.mosquitto_bridge_target`),
so the dashboard knows where to write the configuration it renders from
`bridge.json`. The bridge settings themselves live in `bridge.json` and are
edited in the dashboard.

### Operating state stays in separate files

`settings.json`, `mqtt.json` and `bridge.json` in `data_dir` hold the
operating state and are not part of `config.json`. They contain what users save
in the dashboard at runtime:

- `settings.json`: general dashboard settings.
- `mqtt.json`: MQTT settings (alternative broker settings), with two optional
  fields besides the broker connection. `metrics` maps a metric name to a bool.
  A missing key means the metric is published. Setting it to `false` stops the
  dashboard's node agent from publishing that system metric.
  `simulation_active` is a bool, default `false`. When it is `true`, the
  dashboard sends simulation mode as a retained message to all bridges on
  `outstation/energy_node/settings/simulation_active/set`.
- `bridge.json`: Mosquitto bridge settings.

`config.json` only holds settings the admin sets, locally or through a deploy.
Runtime state stays in these files.

---
title: "Service library"
---

# Service library

`energy_node_common` (`libs/energy_node_common/`) is the Python package every
device service builds on. It holds the infrastructure and no device logic. This
page describes what the library does on the wire: the settings protocol, the
status message and the scheduler. How to put it together into a service is on
[Writing a device service](new-device-service.md).

| Module | Purpose |
|---|---|
| `appconfig` | reads and validates `/etc/energy-node/config.json` |
| `config` | JSON loading and `ReloadableConfig`, which swaps a configuration only after a successful load |
| `mqtt` | client set-up, publish helpers, the availability pattern |
| `discovery` | Home Assistant discovery topics and entity configs |
| `settings` | topic names and the status structure |
| `scheduler` | thread and asyncio schedulers for the two poll rounds |
| `slave` | `Slave`, which ties the above together |

## Reading config.json

`appconfig.load(path)` returns an `AppConfig` with the sections `mqtt`, `paths`,
`dashboard` and one `ServiceConfig` per service. `config.service("shelly")`
returns one block, `config.devices_config("shelly")` the path of its devices
file, `config.mqtt.password()` reads the password file. An invalid file raises
`ConfigError` with the JSON path and what was expected, for example
`services.shelly.poll_interval_s: expected number, found "20"`. The library
validates by hand, because a schema library would be its only extra dependency.
The dashboard checks the same file against `config.schema.json`, so a change to
a field has to be made in both places.

## Settings protocol

Each device service is one `Slave`. All its topics start with
`outstation/<service_id>/`:

| Topic | Direction | Content |
|---|---|---|
| `settings/status` | service to broker, retained | the status message below |
| `config/reload` | to the service | any payload asks the service to reload its configuration |
| `status/online` | service to broker, retained | `1` or `0`, the last will sets `0` |
| `status/last_update` | service to broker, retained | Unix time of the last successful poll |

Simulation is set per physical device, not per service. The topics use the
device ID: `outstation/<device_id>/settings/simulation_active/set` takes the
command, `outstation/<device_id>/settings/simulation_active` carries the value
the service accepted, retained. The service also listens to
`outstation/<node_device_id>/settings/simulation_active/set`, the broadcast for
every device of the node. `1`, `true`, `on`, `yes` and `enabled` switch it on,
`0`, `false`, `off`, `no` and `disabled` switch it off. Anything else is
refused. After a restart the retained value is read back, so a simulated device
stays simulated. A device that is new after a reload starts with simulation off.

Poll interval and diagnostics multiplier come only from `config.json`
(`services.<id>.poll_interval_s` and `diagnostic_poll_multiplier`). They take
effect at start and on `config/reload`. There is no topic to change them while
the service runs.

### The status message

The three poll fields and `pending` in this table are marked *(will be removed in future: slave cleanup)*. Nothing reads them. Do not build on them.

| Field | Meaning |
|---|---|
| `poll_interval_s`, `diagnostic_poll_multiplier` | the values in use. *(will be removed in future: slave cleanup)* |
| `actual_poll_interval_s` | the interval the scheduler runs with. Always equal to `poll_interval_s`. *(will be removed in future: slave cleanup)* |
| `simulation_active` | `1` if every registered device simulates |
| `last_update` | Unix time of the last successful poll round |
| `runtime_status` | `ok`, `rejected` or `pending`. The value `pending` is never set. *(will be removed in future: slave cleanup)* |
| `error`, `error_code` | the reason for `rejected`. The code is stable and translated by the dashboard |
| `config_revision`, `applied_revision` | SHA-256 of the file that was tried last and of the file in use |

The two revisions are the same checksum the dashboard forms when it saves a
file. If they match, the status belongs to exactly that file, so the dashboard
knows whether a service has seen the latest save.

A rejected reload stays visible until a later reload succeeds. A single refused
command, such as an invalid simulation value, is reported once and does not
stick. A service that starts with a faulty devices file runs with no devices
and reports `rejected` from the start.

## Scheduler

Every service has a core poll and, optionally, a diagnostics poll for values
that change slowly. `Scheduler` runs them in a thread for synchronous
services, `AsyncScheduler` in a task of the service's event loop. Both:

- run the core poll, then wait `poll_interval_s`
- run diagnostics after the core poll whenever `poll_interval_s *
  diagnostic_poll_multiplier` seconds of real time have passed since the last
  time, so a changed interval or multiplier applies at once, and the first
  round includes diagnostics
- catch an exception, hand it to `on_poll_error` and keep running. An exception
  skips the rest of that round, including diagnostics.

The core poll has to call `slave.note_update(client)` after a successful round,
otherwise `last_update` stands still and the dashboard shows the service as
not current.

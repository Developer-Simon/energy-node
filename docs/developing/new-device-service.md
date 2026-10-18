---
title: "Writing a device service"
---

# Writing a device service

A device service is one small Python program and one systemd unit. It talks to
one family of hardware and publishes what it finds to the local MQTT broker as
Home Assistant MQTT Discovery entities. It never talks to the dashboard
directly, the broker is the only connection. The overview of the existing
services is on [Device services](../services/index.md), the channels they use
are in [data flows](data-flow.md).

This page walks through what a new service consists of and lists every place
in the repository that has to know about it. The best template is the
[APsystems EZ1](../services/apsystems-ez1.md) service
(`services/apsystems_ez1/`), because it reads and controls. The
[Trucki stick](../services/trucki.md) service is the shortest read-only one.

## Decide first

| Decision | Example | Where it shows up |
|---|---|---|
| Service ID | `apsystems` | `outstation/<service_id>/...` topics, `services.<service_id>` in `config.json`, the name of the devices file |
| Directory | `services/apsystems_ez1/` | the unit, the bundle, the version component `service:apsystems_ez1` |
| Step number | `81` | the installer step `scripts/bootstrap/81-apsystems.sh`. Services use 81 to 88, each number once |
| Kind | `device` or `service` | `device` is listed under device services, `service` gets its own row |

The devices file is always called `<service_id>_devices.json`. The name decides
which service the dashboard asks to reload after a save, see
[Configuration schemas](config-schema.md).

## What is in the service directory

| File | Purpose |
|---|---|
| `<name>_mqtt.py` | the service. Contains only the device logic |
| `<name>.service` | the systemd unit. It starts the script with `--config /etc/energy-node/config.json` |
| `manifest.json` | `service_id`, `version`, `unit`, `schema`, `bootstrap_step`, `required`, `kind` |
| `config.schema.json` | schema of the service's block `services.<service_id>` in `config.json` |
| `<service_id>_devices.json` | the devices as shipped. Operator data, written only if missing |
| `<service_id>_devices.schema.json` | schema of the devices file, shipped and renewed on every update |
| `CHANGELOG.md` | maintained by CI, see [releasing](releasing.md) |
| `tests/` | pytest tests. Copy `conftest.py` from a neighbour |

`manifest.json` is the one place that describes the service. `required` lists
the fields every installation has to set in its `services.<service_id>` block,
the first entry is `service_id`. The block always carries `poll_interval_s` and
`diagnostic_poll_multiplier`, a service can add its own such as
`http_timeout_s`.

## The program

The shared library `libs/energy_node_common` carries everything that is not
device specific. A service uses it like this:

1. **Read `config.json`.** `appconfig.load(appconfig.config_path_from_argv())`.
   A missing or invalid file ends the program with a message. Systemd then
   restarts it, there is no fallback.
2. **Load the devices.** A `ReloadableConfig(path, load_devices)` wraps the
   loader. `load_or([])` lets the service start with an invalid file, run with
   no devices and report the error, instead of crashing in a loop.
3. **Create the `Slave`.** It owns the scheduler and the settings protocol.
   The service passes `poll_core` and, if it has slow values, `poll_diagnostics`.
   Everything else comes from the library: the poll rhythm, the per-device
   simulation switch, `config/reload` and the retained `settings/status`.
4. **Build the MQTT client** with `mqtt.build_client`, with the last will on the
   service's `status/online` topic, and let `on_connect` call `slave.start()`.
   `on_message` first offers every message to `slave.handle_message()`.
5. **Publish discovery** for every device and entity, retained, in the
   three-level form `homeassistant/<component>/<device_id>/<object_id>/config`.
   The helpers in `energy_node_common.discovery` build the config with
   `unique_id`, availability and the device block. Set `via_device` to the
   node's device ID, so the device hangs below the node in Home Assistant and
   on the device map.
6. **Publish values** under `outstation/<device_id>/<topic>`, retained, and the
   availability of each device with `publish_online_status`. A device that does
   not answer is marked offline with a reason, the others carry on.
7. **Accept commands** on `outstation/<device_id>/set/...` for entities that can
   be controlled, and answer with the new state.

### Reload

`config/reload` arrives as a message and may be handled while a poll is
running. The reload function must load **both** the `config.json` block and the
devices file as candidates first and commit them together:

```python
new_app = appconfig.load(path)
new_devices = config_store.load_candidate()
# only now swap, nothing above may have raised
config_store.commit(new_devices)
```

If anything fails, the old configuration stays and the `Slave` reports
`runtime_status: "rejected"` with the reason. A service must never run with half
of a new configuration. Raise `ConfigRejected(message, code)` for errors the
operator should see translated, the dashboard maps the stable `code` to a text.

### Simulation

`slave.register_devices(ids)` registers the devices for the simulation switch.
The service decides what `simulation_active_for(device_id)` means, usually
plausible fake values instead of calls to the hardware. New devices start with
simulation off.

## Where the service must be registered

A new service touches more than its own directory. The list below is what the
existing services have in common. A missed place is caught by a test in most
cases, and the quickest way to find the rest is to search the repository for the
ID of an existing service, for example `trucki`.

| Area | What to add |
|---|---|
| Bootstrap | `scripts/bootstrap/<step>-<name>.sh` with one `service_step <step> <dir> <unit>` call and the `step-inputs` lines. The table in `scripts/tests/test_bootstrap_service_scripts.sh` and `scripts/tests/test_build_make_bundle_manifest_steps.sh` |
| Build | the Python packages the service needs in `scripts/build/lib/wheels.sh` and `requirements-dev.txt`. The bundle picks the directory up from its manifest |
| Versions | a row in `scripts/version/components.json` and `scripts/version/components.sh`, the target in `scripts/generate_changelog.sh` and in `.github/workflows/version-bump.yml`, the entry in `dashboard/internal/versions/versions.go` |
| Central config | the `services.<service_id>` block in `services/energy-node.config.json`, the flag `installed_services.<service_id>` in `dashboard/cmd/schemagen/core.schema.json`. Then run `go run ./cmd/schemagen` in `dashboard/`, which rewrites `dashboard/internal/appconfig/config.schema.json` |
| Dashboard | the label of the devices file in `dashboard/internal/config/config.go` and its text `config.file.<name>_devices` in the English catalog, `services.<service_id>.service_id` in `restartRequiredFields` in `dashboard/internal/httpapi/systemconfig.go` |
| Translations | the German texts of all schema fields in `dashboard/internal/schemaloc/catalogs/de.json`. The drift tests in `dashboard/internal/schemaloc` list missing keys and check the style rules for schema texts |
| Device map | a `x-dataflow` annotation for every `mqtt-topic` field, see [data flows](data-flow.md#12-data-flow-on-the-device-map). A manufacturer to icon rule in `dashboard/internal/webui/deviceicons.go` if the devices should get an icon |
| Installer | the name `service.<service_id>` in `installer/webui/catalogs/en.json` and `de.json`, and the plan test data in `installer/internal/steps` if you add a step to a test scenario |
| Tests | the service in `EXPECTED`, `EXPECTED_STEPS` and `EXPECTED_KINDS` of `libs/energy_node_common/tests/test_service_manifests.py`, and in the `conftest.py` copies that list all services |
| Docs | a page `docs/services/<name>.md`, a row in the table of [Device services](../services/index.md), the entry in `docs/_data/nav.yml`, the versions and step lists in [installation](../install/deploy.md) |

## Checks

Run these before you open the pull request. The first two catch most missed
places.

```sh
.venv/bin/pytest libs/energy_node_common/tests services/<dir>/tests
bash scripts/tests/test_bootstrap_service_scripts.sh
bash scripts/tests/test_build_manifests.sh
cd dashboard && go run ./cmd/schemagen && go test ./...
```

Then start the dashboard locally with `dashboard/test/smoke/run-local-dashboard.sh`
and open the configuration page. The new file should appear with its form, with
the descriptions you wrote, and a save should reach the service.

## Documenting the service

Write the service page for the operator: what the service talks to, what you
have to configure, which entities appear and what the fields mean. The field
texts come from the schema, so quote them rather than rewrite them. Describe
anything an operator cannot read from the UI, such as formulas, timings or
limits, in a separate "how it works" page like
[Battery SoC](../services/battery-soc-how-it-works.md).

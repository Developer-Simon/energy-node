# Changelog

## v0.4.3 (2026-09-12)

### Features

- **dashboard:** replace theme select with a four-way segmented slider (#23) (3855e9c)

### Refactors

- **build:** derive the bash service table from service manifests (d6f39ff)

## v0.4.3 (2026-09-11)

### Features

- **dashboard:** migrate config.json schema_version 1 to 2 on load (#20) (b8f72d1)
- **deploy:** deliver service manifests, fix changelog bump ordering (#22) (a1f6ec6)

### Fixes

- **dashboard:** finish the config.json v1 -> v2 migration path (#21) (9d4fb53)

## v0.4.2 (2026-09-11)

### Features

- **dashboard:** migrate config.json schema_version 1 to 2 on load (#20) (b8f72d1)
- **⚠ Breaking:** move node telemetry into the dashboard nodeagent (#19) (8049117)
- make device services self-describing with per-service manifests (#12) (2e8c911)
- **⚠ Breaking:** fold the config.json node block into dashboard.node_* (schema_version 2) (#14) (e7f4ba1)
- **dashboard:** rebuild the history settings tab (#16) (5a41c5c)
- **dashboard:** rebuild the settings tabs as responsive material cards (#18) (d905f8b)
- **dashboard:** migrate config.json schema_version 1 to 2 on load (3ac68ef)
- **appconfig:** add dashboard.node_* alongside the node block (e1c73d1)
- add a manifest and schema fragment per device service (7124a64)

### Fixes

- **dashboard:** finish the config.json v1 -> v2 migration path (#21) (9d4fb53)
- **dashboard:** finish the config.json v1 -> v2 migration path (89252c9)
- **changelog:** roll unreleased sections into a hand minor bump (ea01be2)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)
- remove git hooks, and stop the per-PR CHANGELOG stacking (#13) (c643260)
- **appconfig:** derive the service set from manifests/, not a table (652345c)
- rename product Werkstatt-IoT to Energy Node repo-wide (3978730)
- rename Werkstatt-IoT to Energy Node, externalize deploy config, and document pi migration (daf06da)

### Documentation

- finish the energy-node -> energy_node rename and guard it (a01c32e)
- drop remaining git-hooks references (7786f6d)
- refresh changelogs before public fork (220794e)

### Tests

- **manifests:** lock the manifest set to the config.json template services (a0c71c9)

### Chores

- bump minor versions for dashboard, services and energy_node_common (549a100)
- initial public release of Energy Node (e9c9417)

### Dev

- added version file to common package and adjusted versioning (3e10297)
- **changelog:** add changelog for dashboard, src, and energy_node_common (768b498)
- **scripts:** auto-generate battery_soc_core + HA integration changelogs, move deploy scripts under scripts/ (1f98ab1)

### CI

- **version-bump:** refresh component changelogs on the PR branch (#11) (6326afb)
- path-filter the workflow and flag breaking changes in the changelog (#15) (5282452)
- skip Markdown-only diffs and gate the job set on the version bump (#17) (9446219)

### Other

- feat!: drop the node block, bump schema_version to 2 (7990f5c)

## Unversioniert (bis 2026-08-24)

### Features

- created module for central refresh rate configuration (8f8382f)
- added central simulation mode (ad8311a)
- added availability topic for all devices (7704fc0)
- added reload for configurable services (e7af942)
- replace per-service *.env files with a central config.json (d060b5a)
- **automation:** add per-rule trigger history with dashboard view (7eb80b6)

### Fixes

- non-working install script (42dd7e8)
- slave.py doesn't update last_update (8bf2de3)

### Refactors

- no device ha discovery for services, only outstation topics (3a4e503)

### Other

- reduced diagnostics multiplier to a single setter in the node (c7180e8)


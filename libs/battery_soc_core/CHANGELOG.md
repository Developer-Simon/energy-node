# Changelog

## v0.1.9 (2026-09-10)

### Features

- make device services self-describing with per-service manifests (#12) (2e8c911)
- **⚠ Breaking:** fold the config.json node block into dashboard.node_* (schema_version 2) (#14) (e7f4ba1)
- **dashboard:** rebuild the history settings tab (#16) (5a41c5c)
- **dashboard:** rebuild the settings tabs as responsive material cards (#18) (d905f8b)
- **⚠ Breaking:** move node telemetry into the dashboard nodeagent (#19) (8049117)
- **battery_soc:** extract core logic and adapter modules (4a31188)
- **battery_soc:** show net battery power as a measurement, not diagnostic (b3878e8)
- **ha:** native Home Assistant integration for battery SoC (99ef3cf)

### Fixes

- **changelog:** roll unreleased sections into a hand minor bump (ea01be2)

### Refactors

- split src/ into services/ and libs/, rename service-level device_id to service_id (#10) (75abe73)
- remove git hooks, and stop the per-PR CHANGELOG stacking (#13) (c643260)
- rename product Werkstatt-IoT to Energy Node repo-wide (3978730)

### Documentation

- drop remaining git-hooks references (7786f6d)
- refresh changelogs before public fork (220794e)

### Chores

- initial public release of Energy Node (e9c9417)

### Dev

- **scripts:** auto-generate battery_soc_core + HA integration changelogs, move deploy scripts under scripts/ (1f98ab1)

### CI

- **version-bump:** refresh component changelogs on the PR branch (#11) (6326afb)
- path-filter the workflow and flag breaking changes in the changelog (#15) (5282452)
- skip Markdown-only diffs and gate the job set on the version bump (#17) (9446219)


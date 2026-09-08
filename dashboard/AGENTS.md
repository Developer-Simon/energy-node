# Dashboard Guidelines

These instructions apply to the Go dashboard and its embedded web UI.

## Architecture

- `cmd/dashboard/main.go` owns process wiring, lifecycle, environment defaults, and graceful shutdown.
- `internal/mqttclient` reads Home Assistant MQTT Discovery and dynamically subscribes to entity state and availability topics. It must remain independent of the Python bridge implementations.
- `internal/registry` owns synchronized in-memory device/entity state. HTTP and UI code should use snapshots or value copies rather than reaching into its locks.
- `internal/runtimecache` persists only reduced stale/fallback state. `internal/config` manages bridge JSON plus schemas and revisions; `internal/settings` manages dashboard-owned settings, energy assignments, and layout. Keep these data concerns separate.
- `internal/httpapi` owns versioned `/api/v1` routes. `internal/webui` owns server-rendered templates and locally embedded JavaScript/CSS assets.
- `internal/appconfig` embeds `config.schema.json` for the central `config.json`. That file is **generated**, not hand-edited: `cmd/schemagen` composes it from `cmd/schemagen/core.schema.json` plus one `services/<name>/config.schema.json` fragment per device service. After changing the core schema or a fragment, run `go run ./cmd/schemagen`; `git-hooks/pre-commit` and `go test ./...` guard it against drift.

## Development

Run commands from `dashboard/`; `go.mod` is located there, not at the repository root. Running `go test ./...` from the root fails with a module-directory error. From the root, use `cd dashboard && go test ./...` (and the same prefix for `go vet` and `go build`):

```sh
gofmt -w path/to/changed.go
go vet ./...
```

Use the `runTests` tool for Go tests in this module, selecting the relevant test files when possible. Run it from the `dashboard/` module context; use the shell commands above for formatting, vetting, and builds.

The static JS in `internal/webui/static/js/` (e.g. `config.page.js`) has its own regression tests under `test/`, run with Node's built-in test runner plus jsdom for DOM emulation (no browser or build step). From `dashboard/`:

```sh
npm install   # once, populates the gitignored node_modules/
npm test
```

For changes that unit tests alone can't convincingly cover (registry → HTTP
API → form, or visual/theme checks — a Sichtprüfung), this repo has a local
integration test — start the real dashboard with
`test/smoke/run-local-dashboard.sh` (no mosquitto, no Pi access, no real
devices needed — see `test/smoke/README.md`). Use `--keep` to leave it
running and open it in a browser instead of tearing it down after the
automated checks.

The deployment target is a statically linked ARMv6 binary without CGO:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build -o energy-node-dashboard ./cmd/dashboard
```

Use `../scripts/deploy/deploy_dashboard_to_remote.sh` for the established remote deployment flow. Keep frontend dependencies local and embedded; do not add CDN runtime dependencies.

`VERSION` holds the release triple shown on the settings page (`major.minor` hand-edited, `patch` auto-bumped per commit by `../git-hooks/pre-commit` — run `../scripts/install_git_hooks.sh` once to activate it locally). The build binds it via `-ldflags "-X main.buildVersion=..."`; see `scripts/deploy/deploy_dashboard_to_remote.sh` for how the branch prerelease suffix is computed. A plain `go build` without that flag reports version `dev`.

The Python services' own `../services/VERSION` (same auto-bump hook, independent counter) is shown next to it, read at runtime from the path in `config.json`'s `paths.services_version_file` — see [Konfiguration](../docs/knowledge/configuration.md). Empty/unset shows "unbekannt" instead of failing to start.

## Invariants

- Treat Home Assistant MQTT Discovery as the dashboard input contract, not bridge-specific Python code. The current supported topic shape is the retained three-level form `homeassistant/{component}/{device_id}/{object_id}/config`; do not expand to four-level Discovery without an explicit scope decision. Device/bridge configuration files and the automation service's persisted trigger history are the established exception: both are read directly from the shared devices directory (`internal/config`, and `GET /api/v1/automations/history/{rule_id}`) — not a new input channel, the same convention `automation_rules.json` already uses.
- The dashboard stays read-only with three named exceptions: registry-validated entity commands; its own energy-balance broadcast on outstation/dashboard/energy/balance (non-retained, no discovery entry); and POST /api/v1/automations/test, which forwards an operator's explicit "test this action" request to the automation service on the fixed topic outstation/automation/test/set (non-retained, RoleAutomations + CSRF, payload restricted to {rule_id, action_index}). The third exception is deliberate: a test is not a rule-driven write but an explicit human action, the same category as an entity command, and every substantive check — does the rule exist, is the index valid, is the target topic allowed — happens in the service, not here. Rule-driven write paths still belong explicitly outside the dashboard, in services/automation/.
- Mark restored runtime-cache values as stale/non-live and let a fresh MQTT state message replace them. Cache writes must remain atomic and must not happen for a timestamp-only change.
- Validate persisted JSON against its schema before saving, use atomic writes, and keep revisions in the configured data directory. Never treat runtime state or layout as bridge device configuration.
- Preserve `/api/v1` versioning and use consistent JSON errors for API failures. Add method checks to handlers that do not support every HTTP method.
- Keep Raspberry Pi resource use in mind: avoid unnecessary dependencies, background work, and broker-wide wildcard subscriptions. Durable server-side history is avoided with two named exceptions: the Verläufe history lives entirely in the browser's IndexedDB, the Go process stores only its configuration; and the automation service's persisted, opt-in, per-rule trigger history file, which the dashboard only reads — bounded and capped by the service, not evaluated or accumulated by the Go process. SQLite, four-level Discovery, plugins, backup/import/export, and device commands are currently excluded; no automation engine and no automation runtime state in the Go process.
- Do not commit credentials from environment files. Do not change existing bridge topics or Python services as part of an unrelated dashboard change.

## References

- [MQTT and Discovery format](mqtt-topics-und-discovery-format.md) records the verified bridge conventions; it is not a replacement for the Home Assistant Discovery contract.
- [Service unit](energy-node-dashboard.service) and [deployment script](../scripts/deploy/deploy_dashboard_to_remote.sh) define runtime paths and remote installation behavior.
- [Reverse proxy subpath](../docs/knowledge/dashboard/reverse-proxy.md) documents `internal/basepath`: how a proxy-supplied prefix reaches templates, JS and cookies, and the rule that template URLs arrive prefixed while JS literals prefix themselves.
- If the target is online, it is reachable at its configured hostname or LAN IP on port 8080.
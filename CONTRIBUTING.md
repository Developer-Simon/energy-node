# Contributing

Energy Node is a personal project shaped by one specific site's hardware —
see [About this repository](README.md#about-this-repository). Issues and
pull requests are welcome, but forking and adapting to your own devices is
the expected way to use this beyond the maintainer's own site.

## Before you start

- For anything beyond a small fix, open an issue first describing the
  change. This is a single-maintainer project; a heads-up avoids wasted work
  on a PR that doesn't fit the architecture in [`dashboard/AGENTS.md`](dashboard/AGENTS.md)
  or the invariants documented under [`docs/`](docs/).
- Bridge- or dashboard-specific conventions live next to the code
  (`dashboard/AGENTS.md`, the module docstrings under `src/`). Read the one
  for the area you're touching before changing it.

## Development setup

See **[INSTALLATION.md](INSTALLATION.md)** ("Development machine") for the
full environment. In short:

```sh
python3 -m venv .venv && .venv/bin/pip install -e src/energy_node_common -e src/battery_soc_core pytest
.venv/bin/pip install -r requirements-dev.txt   # needed for the full src/ test suite
./scripts/install_git_hooks.sh
```

The Home Assistant integration needs its own virtualenv — see the
[README's Development section](README.md#development) for why and how.

## Running the checks

Run whichever of these apply to your change before opening a PR — CI runs
all of them:

```sh
.venv/bin/pytest src scripts/tests                                    # Python bridges + tooling
bash scripts/tests/test_publish_mirror.sh                             # HACS mirror assembly
cd integrations/homeassistant && ../../.venv-ha/bin/pytest            # HA integration
cd dashboard && gofmt -l . && go vet ./... && go test ./...           # Go dashboard
cd dashboard && npm install && npm test                               # dashboard JS
dashboard/test/smoke/run-local-dashboard.sh                           # real dashboard, no Pi, no broker
```

## Secrets

Never commit real credentials, IPs from your own site's private range, or
device serials — see
[`docs/knowledge/dashboard/secrets-and-credentials.md`](docs/knowledge/dashboard/secrets-and-credentials.md).
`./scripts/deploy/check_tracked_secrets.sh` scans tracked files for obvious
leaks; run it before pushing.

## Disclose AI use

If an AI coding assistant helped write your change, say so in the PR
description — see [AI-DISCLAIMER.md](AI-DISCLAIMER.md) for what to include
and why. This is a hard requirement, not a preference.

## Commit hygiene

`./scripts/install_git_hooks.sh` links this repo's hooks into `.git/hooks/`.
The pre-commit hook auto-bumps `dashboard/VERSION` / `src/VERSION` on `main`
and rejects a commit that leaves the vendored `battery_soc_core` copy under
`integrations/homeassistant/` out of sync with `src/battery_soc_core/` — run
`.venv/bin/python scripts/vendor_core.py` and stage the result if it fires.

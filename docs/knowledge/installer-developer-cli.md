---
title: "Installer developer CLI"
---

# Installer developer CLI

The installer (`installer/`, a separate Go module) is planned as four layers —
core, HTTP/SSE, web UI, app shell. The core layer, exposed directly as a CLI
(internally "E12"), and the HTTP/SSE + web UI layers (Schicht 2/3) both exist
today; only the fourth layer — a packaged, downloadable app shell for
non-technical end users — does not (see "Trying the graphical UI" below for
what "app shell" means today: an unsigned local dev binary that opens a
browser window, not a release artifact). The CLI builds a signed bundle from
your local checkout, ships it to a Raspberry Pi over SSH/SFTP, and runs the
same idempotent [bootstrap steps](https://github.com/Developer-Simon/energy-node/tree/main/scripts/bootstrap)
the web UI also runs. `--only dashboard` runs exactly the step a full
redeploy would run — there is no second code path for a partial update.

It is the fast iteration loop for developing on a real node without touching
the Pi's shell by hand, and it is meant to replace `scripts/deploy/*.sh`
(still documented in `INSTALLATION.md` §6) once its parity with those scripts
is proven.

## Building the CLI

```sh
cd installer
go build -o installer ./cmd/installer
```

`go vet` and `go test` for `installer/` run in CI whenever `installer/` or
`scripts/bootstrap/` change; `installer/webui/` (a further nested Go module,
`replace`d in `installer/go.mod`) has its own Go and JS test suites, plus a
Playwright browser suite, all gated the same way.

## Trying the graphical UI

Running the binary with no subcommand — or only flags — starts the web UI
instead of the CLI:

```sh
./installer
```

This opens a browser window (Chrome/Edge in app mode, falling back to the
default browser) on a local one-time-token URL. Unlike the CLI, it does not
build a bundle on demand: it expects one already unpacked at `bundle/` next
to the binary (`--bundle <dir>` to point elsewhere). Build one first:

```sh
cd ..   # repo root
bash scripts/build/make_bundle.sh --arch armv6 --skip-wheels
mkdir -p installer/bundle
tar -xzf dist/energy-node-*-armv6.tar.gz -C installer/bundle
```

`--skip-wheels` avoids the piwheels round-trip for a quick local try; the
dashboard binary is cross-compiled and the Tailscale tarball downloaded
automatically unless `--dashboard-binary` / `--tailscale-tarball` inject
prebuilt ones. Signing works the same as `deploy` — an unsigned bundle is
fine for driving the UI locally.

| Flag | Purpose |
| --- | --- |
| `--port` | Port on 127.0.0.1; 0 (default) lets the OS pick one |
| `--bundle <dir>` | Unpacked bundle directory; default: `bundle/` next to the binary |
| `--lang` | UI language; default: guessed from the OS locale |
| `--no-window` | Don't open a browser window, just print the URL |

The target node needs passwordless sudo, same as the CLI: the very first
thing the UI does after connecting is create and take ownership of
`/var/lib/energy-node-installer` on the node (`sudo install -d`), because a
plain SSH user cannot write under root-owned `/var/lib` on a node that has
never had this installer run before.

## Configuring a target node

Every subcommand needs a target node and a way to reach it over SSH.

1. **Target.** Create `secrets/deploy-target.env` in your checkout:

   ```
   TARGET_HOST=<node-hostname-or-ip>
   TARGET_USER=<ssh-user>
   TARGET_BASE=/home/<ssh-user>
   ```

   `TARGET_BASE` defaults to `/home/<TARGET_USER>` when omitted. `--host`,
   `--user` and `--base` override the file per invocation; `--target-env`
   points at a different file entirely.

2. **Authentication.** The CLI tries an SSH key first — `~/.ssh/id_ed25519`
   by default, or `--identity <path>` — then falls back to a cached password
   at `secrets/system-ssh.pw` (`--password-file` to override).

3. **Signing.** Deployed bundles are signature-verified by default, and there
   is no private key for the embedded release public key outside CI. Pass
   `--dev-unsigned` on `deploy` and `ensure-secrets` to skip verification —
   this is required for any local build, not an optional shortcut. Use
   `--sign-key <path>` instead if you do have a signing key to test with.

## Commands

### `installer deploy`

```sh
./installer deploy --dev-unsigned
```

Builds a bundle from `--repo` (default: the current directory) and deploys it
to the target, running whichever bootstrap steps changed.

| Flag | Purpose |
| --- | --- |
| `--only <target>` | Deploy just one step: `dashboard`, `wheels`, or a device service id (`apsystems`, `battery-soc`, `shelly`, `trucki`, `tuya`, `automation`) |
| `--dry-run` | Preview what would change without touching the node |
| `--force-config` | Overwrite the node's existing `config.json` (asks for confirmation first) |
| `--arch` | Target architecture: `armv6` (default, Pi 1 / Pi Zero W), `arm64`, `amd64` |
| `--python-minor`, `--abi` | Override the bundle's Python version / wheel ABI tag |

### `installer ensure-secrets`

```sh
./installer ensure-secrets --dev-unsigned
```

Bootstraps the MQTT and dashboard admin passwords on the node, caching them
locally at `secrets/mqtt.pw` / `secrets/dashboard-admin.pw`
(`--mqtt-secret-path` / `--admin-secret-path` to change the path). Takes the
same bundle flags as `deploy` because it may need to build one.

### `installer fetch-config`

```sh
./installer fetch-config
```

Copies the node's live `/etc/energy-node/config.json` (`--remote-path` to
change it) down to a local template path (`--local`, default:
`services/energy-node.config.json`) so you can inspect or diff it.

### `installer diagnose`

```sh
./installer diagnose
```

Reads the installed bundle's `manifest.json` on the node, runs
`diagnose.sh` remotely, and prints a checklist:

```
Installed version: 1.4.2

[OK]   apt                      ...
[FAIL] dashboard                dashboard.service is not active
       -> retry: installer deploy --only dashboard

4/5 checks OK
```

Every failing check comes with a copy-pasteable retry command — usually
`installer deploy --only <target>` — so this is the first thing to run when a
node is behaving unexpectedly.

## Common flags

These apply to every subcommand:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--repo <path>` | current directory | Repository checkout to build a bundle from |
| `--host` / `--user` / `--base` | from `deploy-target.env` | Override the target node |
| `--target-env <path>` | `<repo>/secrets/deploy-target.env` | Alternate target file |
| `--identity <path>` | `~/.ssh/id_ed25519` if present | SSH private key |
| `--password-file <path>` | `<repo>/secrets/system-ssh.pw` | Cached SSH password |

Run `./installer help` (or any unknown subcommand) to print the same usage
summary from the binary itself.

---
title: "Installer developer CLI"
---

# Installer developer CLI

The installer (`installer/`, a separate Go module) is built in four layers —
core, HTTP/SSE, web UI, window shell. All four exist. The core layer is exposed
directly as a CLI (internally "E12"); the web UI and the window shell are
described for end users on [Deploying a node](../installer.md). This page covers
the developer side: the CLI builds a signed bundle from your local checkout,
ships it to a Raspberry Pi over SSH/SFTP, and runs the same idempotent
[bootstrap steps](https://github.com/Developer-Simon/energy-node/tree/main/scripts/bootstrap)
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
instead of the CLI. `scripts/dev/run-installer.sh` builds the binary and starts
it, passing its arguments through. `--no-build` skips the build and reuses the
existing binary (it is still built if none exists yet):

```sh
scripts/dev/run-installer.sh              # or: ./installer/installer
scripts/dev/run-installer.sh --no-build   # skip the go build
```

The window opens in the first stage of a four-stage chain that works: an
embedded system WebView (Cocoa/WKWebView on macOS, WebView2 on Windows,
GTK/WebKitGTK on Linux — loaded at run time, so the binary stays CGo-free),
then a Chrome/Chromium/Edge `--app=` window, then the default browser, then
just the printed URL. `--no-window` jumps straight to the last stage. When the
WebView opens, closing its window ends the process. The URL carries a one-time
token. The UI starts without a bundle. On the connect screen you pick where the
package comes from, and after connecting the installer asks the node for its
architecture (`uname -m`), resolves the package for it, uploads it and
verifies it on the node:

| Source | What happens |
| --- | --- |
| Bundled | The unpacked bundle next to the binary (or `--bundle <dir>`), if there is one |
| Package file | A `.tar.gz` you pick in the browser; uploaded to the local installer, verified, then sent to the node |
| Build from repository | Linux only. Runs `scripts/build/make_bundle.sh` in a checkout (path is pre-filled when the installer runs inside one). Needs `bash`, `git` and `go`; unsigned |
| Live from GitHub | Downloads the newest stable release's signed `energy-node-<version>-<arch>.tar.gz`; the signature must match the embedded release key |

Downloads and repo builds are cached under `~/.energy-node/cache/`, unpacked
scratch space under `~/.energy-node/work/`. A bundle without a signature is
accepted (except from GitHub) and flagged "unsigned" in the UI; a bundle with
a signature that does not verify is always refused.

| Flag | Purpose |
| --- | --- |
| `--port` | Port on 127.0.0.1; 0 (default) lets the OS pick one |
| `--bundle <dir>` | Unpacked bundle directory (optional) |
| `--lang` | UI language; default: guessed from the OS locale |
| `--no-window` | Don't open a browser window, just print the URL |

The target node needs passwordless sudo, same as the CLI: the very first
thing the UI does after connecting is create and take ownership of
`/var/lib/energy-node-installer` on the node (`sudo install -d`), because a
plain SSH user cannot write under root-owned `/var/lib` on a node that has
never had this installer run before.

### Working on the UI without a Pi

`installer/webui/cmd/fakehost` serves the same screens against a canned
backend. It is what the Playwright suite drives, and the source of the
screenshots on [Deploying a node](../installer.md):

```sh
scripts/dev/run-installer.sh --fakehost --lang en --port 8099
# or: cd installer/webui && go run ./cmd/fakehost --lang en --port 8099
```

`--fakehost` makes `run-installer.sh` build and start the demo host instead of
the installer (`--no-build` applies to it as well); every other argument goes to
the demo host. `--scenario vorlage-update` serves the update preview, `--hold-step <id>`
stops a run at that step, `--fail-step <id>:<CODE>` fails one, `--trusted`
skips the fingerprint dialog and `--dashboard` plays the dashboard-hosted
variant. `npm run test:e2e` in `installer/webui` runs the browser suite.

### Publishing release binaries

`.github/workflows/installer-release.yml` is triggered manually
(`workflow_dispatch` only). It runs `go vet` and `go test`, cross-compiles the
installer with `CGO_ENABLED=0` for Windows amd64, macOS amd64/arm64 and Linux
amd64, and publishes the archives plus `SHA256SUMS` as a GitHub release tagged
`installer-<contents of installer/VERSION>`; a version containing a hyphen is
marked as a prerelease. The tag is independent of the dashboard's `v*`
releases. The binaries are unsigned and carry no bundle: they embed only the
release public key, and the `bundle/` directory still has to be supplied.

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

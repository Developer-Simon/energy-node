---
title: "Installer developer CLI"
component: installer
redirect_from:
  - /knowledge/installer-developer-cli.html
---

# Installer developer CLI

The installer (`installer/`, a separate Go module) has four layers: core,
HTTP/SSE, web UI and window shell. The core layer is also available directly as
a CLI (internally "E12"). [Deploying a node](../installer.md) describes the web
UI and the window shell for end users. This page covers the developer side.
The CLI builds a signed bundle from your local checkout, sends it to a
Raspberry Pi over SSH/SFTP and runs the same idempotent
[bootstrap steps](https://github.com/Developer-Simon/energy-node/tree/main/scripts/bootstrap)
as the web UI. `--only dashboard` runs the same step a full redeploy would run,
so a partial update has no code path of its own.

Use it to iterate quickly on a real node without working in the Pi's shell by
hand. It replaced the old `scripts/deploy/*.sh`. A bundle built by the CLI
carries a dev version (`<VERSION>-dev.<commit>`, plus `.dirty` for uncommitted
changes), so a new commit runs every step again and the dashboard shows which
build is running.

## Building the CLI

```sh
cd installer
go build -o installer ./cmd/installer
```

`scripts/dev/run-installer.sh` builds it for you and passes every argument on
to the binary, so it runs the subcommands below as well (`--no-build` reuses the
existing binary):

```sh
scripts/dev/run-installer.sh deploy --dev-unsigned --only dashboard
scripts/dev/run-installer.sh diagnose
```

CI runs `go vet` and `go test` for `installer/` whenever `installer/` or
`scripts/bootstrap/` change. `installer/webui/`, another nested Go module that
`installer/go.mod` pulls in with `replace`, has its own Go and JS test suites
and a Playwright browser suite, which run under the same conditions.

## Trying the graphical UI

Without a subcommand, or with flags only, the binary starts the web UI instead
of the CLI. `scripts/dev/run-installer.sh` builds the binary and starts it with
your arguments. `--no-build` skips the build and reuses the existing binary,
unless there is none yet:

```sh
scripts/dev/run-installer.sh              # or: ./installer/installer
scripts/dev/run-installer.sh --no-build   # skip the go build
```

The installer tries four ways to show the window and uses the first that works:
an embedded system WebView (Cocoa/WKWebView on macOS, WebView2 on Windows,
GTK/WebKitGTK on Linux, loaded at run time so the binary needs no CGo), a
Chrome, Chromium or Edge `--app=` window, the default browser, and finally only
the printed URL. `--no-window` goes straight to the last one. Closing the
WebView window ends the process. The URL carries a one-time token.

The UI starts without a bundle. On the connect screen you pick where the
package comes from. After connecting, the installer asks the node for its
architecture (`uname -m`), finds the matching package, uploads it and verifies
it on the node:

| Source | What happens |
| --- | --- |
| Bundled | The unpacked bundle next to the binary (or `--bundle <dir>`), if there is one |
| Package file | A `.tar.gz` you pick in the browser. It is uploaded to the local installer, verified and then sent to the node |
| Build from repository | Linux only. Runs `scripts/build/make_bundle.sh` in a checkout (the path is filled in when the installer runs inside one). Needs `bash`, `git` and `go`. Unsigned |
| Live from GitHub | Downloads the signed `energy-node-<version>-<arch>.tar.gz` of the newest stable release. The signature must match the embedded release key |

Downloads and repository builds are cached under `~/.energy-node/cache/`, and
unpacked files go to `~/.energy-node/work/`. A bundle without a signature is
accepted (except from GitHub) and marked "unsigned" in the UI. A bundle whose
signature does not verify is always refused.

### How the package reaches the node

The CLI (`deploy`) and the web UI both send only what changed. The installer
compares the new bundle's `manifest.json` with the content of the node's bundle
directory, uploads a small archive of the changed files and then deletes the
files the new bundle no longer has. `manifest.json` and `manifest.json.sig`
are always sent.

The installer learns what is on the node from `.verified-manifest.json` in the
bundle directory. It writes the file after every successful check on the node
and removes it before every transfer, so the file always describes the last
verified content. Without it (first run after an installer update, an
interrupted transfer, a node that never had a bundle) the installer hashes the
bundle directory on the node instead, which takes a few seconds on a Pi 1. The
node then checks every file hash with `verify_bundle.sh`, signed or not.

The whole bundle directory is only replaced when you ask for it: with
`--force-full`, with the switch "Transfer whole package again" on the connect
screen, or when the check after a partial transfer fails and you accept the
full transfer the installer then suggests.

| Flag | Purpose |
| --- | --- |
| `--port` | Port on 127.0.0.1. 0 (default) lets the OS pick one |
| `--bundle <dir>` | Unpacked bundle directory (optional) |
| `--lang` | UI language. Default: guessed from the OS locale |
| `--no-window` | Do not open a browser window, only print the URL |

As with the CLI, the target node needs sudo without a password. Right after
connecting, the UI creates `/var/lib/energy-node-installer` on the node and
takes ownership of it (`sudo install -d`), because a plain SSH user cannot
write under the root owned `/var/lib` on a node where this installer has never
run.

### Working on the UI without a Pi

`installer/webui/cmd/fakehost` serves the same screens with a canned backend.
The Playwright suite runs against it, and the screenshots on
[Deploying a node](../installer.md) come from it:

```sh
scripts/dev/run-installer.sh --fakehost --lang en --port 8099
# or: cd installer/webui && go run ./cmd/fakehost --lang en --port 8099
```

`--fakehost` makes `run-installer.sh` build and start the demo host instead of
the installer (`--no-build` works here too). All other arguments go to the demo
host. `--scenario vorlage-update` serves the update preview, `--hold-step <id>`
stops a run at that step and `--fail-step <id>:<CODE>` makes one fail.
`--trusted` skips the fingerprint dialog, `--dashboard` plays the variant
hosted by the dashboard, and `--system-updates` answers the preview's
"Check for updates" and the diagnose with three pending system packages.
`npm run test:e2e` in `installer/webui` runs the browser suite.

### Publishing release binaries

`.github/workflows/installer-release.yml` runs as part of every `v*` release
(see [Cutting a release](releasing.md)), and you can run it again by hand for an
existing tag. It runs `go vet` and `go test`, cross-compiles the installer with
`CGO_ENABLED=0` for Windows amd64 (plain `.exe`), macOS (one universal binary,
zipped) and Linux amd64 and arm64 (`.tar.gz`), and attaches them together with
`SHA256SUMS.installer` to the release. The binaries are unsigned and contain no
bundle. They only embed the release public key and download the signed bundle
for the target node from the same releases.

## Configuring a target node

Every subcommand needs a target node and a way to reach it over SSH.

1. **Target.** Create `secrets/deploy-target.env` in your checkout:

   ```
   TARGET_HOST=<node-hostname-or-ip>
   TARGET_USER=<ssh-user>
   TARGET_BASE=/home/<ssh-user>
   ```

   Without `TARGET_BASE`, the base is `/home/<TARGET_USER>`. `--host`,
   `--user` and `--base` override the file for one call, and `--target-env`
   points to a different file.

2. **Authentication.** The CLI tries an SSH key first (`~/.ssh/id_ed25519` by
   default, or `--identity <path>`) and then a stored password in
   `secrets/system-ssh.pw` (`--password-file` points elsewhere).

3. **Signing.** Deployed bundles are verified against their signature by
   default, and outside CI there is no private key for the embedded release
   public key. Every local build therefore needs `--dev-unsigned` on `deploy`
   and `ensure-secrets` to skip verification. If you have a signing key to test
   with, use `--sign-key <path>` instead.

## Commands

### `installer deploy`

```sh
./installer deploy --dev-unsigned
```

Builds a bundle with a dev version from `--repo` (default: the current
directory) and deploys it to the target. A new commit gives a new dev version,
so every step runs again and every service restarts afterwards. Code from a
working tree can change without the service's own version changing, and the
normal restart rule would miss that.

| Flag | Purpose |
| --- | --- |
| `--only <target>` | Deploy only one step: `dashboard`, `wheels` or a device service id (`apsystems`, `battery-soc`, `shelly`, `trucki`, `tuya`, `automation`). The step runs even if it already ran for this version (its stamp is cleared first), and its unit restarts afterwards. `wheels` has no unit of its own, so run `restart` after it |
| `--dry-run` | Preview what would change without touching the node |
| `--force-config` | Overwrite the node's existing `config.json` (asks for confirmation first) |
| `--force-full` | Replace the node's whole bundle directory instead of sending only the changed files |
| `--arch` | Target architecture: `armv6` (default, Pi 1 and Pi Zero W), `arm64`, `amd64` |
| `--python-minor`, `--abi` | Override the bundle's Python version and wheel ABI tag |

### `installer ensure-secrets`

```sh
./installer ensure-secrets --dev-unsigned
```

Sets up the MQTT and dashboard admin passwords on the node and keeps local
copies in `secrets/mqtt.pw` and `secrets/dashboard-admin.pw`
(`--mqtt-secret-path` and `--admin-secret-path` change the paths). It takes the
same bundle flags as `deploy`, because it may have to build a bundle.

### `installer fetch-config`

```sh
./installer fetch-config
```

Copies the node's live `/etc/energy-node/config.json` (`--remote-path` changes
it) to a local template path (`--local`, default
`services/energy-node.config.json`) so you can inspect or diff it.

`--devices` also copies the operator's device files from `<base>/devices/` back
into `services/<dir>/`. These are the JSON files a service ships as a template
and the dashboard then edits on the node (`*_devices.json`,
`automation_rules.json`). Schemas and presets stay as they are, because every
deploy replaces them on the node anyway. A file the node does not have is
skipped, and the CLI asks once before overwriting. Run
`scripts/dev/check_tracked_secrets.sh` before you commit what came back.

### `installer restart`

```sh
./installer restart
./installer restart --only shelly
```

Restarts units on the node without deploying anything. The installed
bundle's manifest names them. Without `--only`, the dashboard and every
service unit get `systemctl try-restart`, so a service that is not running
stays stopped. `--only dashboard` or `--only <service id>` restarts that one
unit unconditionally.

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

Every failing check comes with a retry command you can copy, usually
`installer deploy --only <target>`. Run `diagnose` first when a node behaves
oddly.

## Common flags

These apply to every subcommand:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--repo <path>` | Current directory | Repository checkout to build a bundle from |
| `--host` / `--user` / `--base` | from `deploy-target.env` | Override the target node |
| `--target-env <path>` | `<repo>/secrets/deploy-target.env` | Alternate target file |
| `--identity <path>` | `~/.ssh/id_ed25519` if present | SSH private key |
| `--password-file <path>` | `<repo>/secrets/system-ssh.pw` | Cached SSH password |

`./installer help` (or any unknown subcommand) prints the same usage summary
from the binary.

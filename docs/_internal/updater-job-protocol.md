---
title: "The job protocol between the dashboard and energy-node-updater"
---

# The job protocol between the dashboard and energy-node-updater

The dashboard can redeploy the node it runs on without SSH. It does so by
writing a job file that a root `energy-node-updater` oneshot unit picks up,
and then following that job's log. This page describes that hand-off. The
dashboard itself fetches the newest signed release bundle and stages it for
the updater, as described below.

## Where the updater takes its bundle from

`internal/updaterhost.Config.CandidateBundleDir` points at a bundle
directory (the `redeploy-candidate/` folder) that is already unpacked but
**not yet verified**. The directory is filled by `internal/bundlefetch`
when the operator opens the *Aktualisieren* (update) screen, or manually.
Whatever puts a bundle there is responsible for `manifest.json` and
`manifest.json.sig` belonging to that exact bundle. The verification itself
happens only in `energy-node-updater.sh`, on its next run — never at the
sending end.

## The job directory

`/var/lib/energy-node-installer/job/`:

| File | Written by | Read by |
|---|---|---|
| `bundle/` | `internal/updaterjob.Stage` | `energy-node-updater.sh`, which moves it out to a root-only directory before verifying it |
| `job.json` → `pending.json` | `internal/updaterjob.Stage` (an atomic rename as its last step) | `energy-node-updater.path` (existence only), `energy-node-updater.sh` |
| `current.json` | `energy-node-updater.sh` (its first action: `pending.json` renamed) | `internal/updaterjob.InFlight` |
| `log` | `energy-node-updater.sh` (`<unix-millis> <line>`) | `internal/updaterhost.tailJobLog` |
| `status.json` | `energy-node-updater.sh` (its last action) | `internal/updaterjob.ReadStatus` |

## Configuration files read by the updater

| File | Written by | Purpose |
|---|---|---|
| `/etc/energy-node-updater/target.json` | Step 60 (as root) | Holds the target user and base directory for this node; read by `energy-node-updater.sh` to resolve template placeholders and step-execution paths. |

## What the job may and may not carry

The job directory belongs to the dashboard's own unprivileged service
account, and `job.json` is not covered by the bundle signature. So the job
says only *which* steps to run, and even that list is held against the step
list in the verified `manifest.json` before anything executes. Everything
that steers a root-run command does not: the target user and base directory
come from the root-owned `/etc/energy-node-updater/target.json` (or, for a
bundle still pinned with `make_bundle.sh --user/--base`, from its verified
manifest; see below), and the bundle version comes from the verified manifest.

The job carries no secrets either. A redeploy never needs `mqtt.pw` or
`auth.pw`: they already exist on a node the dashboard is running on, and
root reads them from the paths the node's own `config.json` names.

## How the candidate gets there

The dashboard downloads the newest stable GitHub release that has a bundle
asset named `energy-node-v<version>-<arch>.tar.gz` for this node's
architecture (determined from the running binary's `GOARCH`). If the same
version is already staged, the download is skipped.

The download goes to a work directory (`redeploy-candidate.work/`), where
the archive is extracted with strict path confinement. The extraction rejects
symlinks and any path that would escape the bundle directory, with caps on
the unpacked size (1 GB) and entry count (20,000). After extraction, the
bundle is checked structurally: `manifest.json` must be readable and contain
a matching architecture, and `manifest.json.sig` must be present. No
cryptographic verification happens in the dashboard; that is the updater's
job. If all checks pass, the staged bundle is swapped atomically into
`redeploy-candidate/`, making it available to the updater.

The download respects the environment variable `ENERGY_NODE_UPDATES_API_BASE`,
which redirects the GitHub API lookup for testing.

Error codes emitted during the fetch:

| Code | Cause |
|---|---|
| `GITHUB_UNREACHABLE` | The GitHub API could not be reached or returned a non-200 status. |
| `GITHUB_NO_RELEASE` | No stable release carries a bundle for this architecture. |
| `BUNDLE_TOO_LARGE` | The asset is larger than 512 MB. |
| `PACKAGE_FILE_INVALID` | The archive is corrupted or contains invalid entries (path escape, unsupported file type). |
| `BUNDLE_UNSIGNED` | `manifest.json.sig` is missing. |
| `ARCH_MISMATCH` | The manifest's architecture does not match the node's. |
| `CANDIDATE_INSTALL_FAILED` | A file system operation (download, extraction, swap) failed. |
| `JOB_IN_PROGRESS` | A previous redeploy is still running (the updater holds a lock). |

## Where the target user comes from

Bundles are user-independent by default. When a bundle carries no embedded
user (the case since the generic-bundle rollout), step 60 renders templates
and writes `/etc/energy-node-updater/target.json` with the operator's chosen
target user and base directory:

```json
{"user":"energynode","base":"/home/energynode"}
```

The updater reads the target from this file on each run, never from `job.json`
(which the dashboard owns and may edit) or the state directory (which the
SSH user owns). A bundle that was pinned to a specific user at build time
(via `make_bundle.sh --user/--base`) embeds that user in its manifest; the
updater will reject a run if the manifest's user and base do not match the
file (error code `TARGET_MISMATCH`).

A node without `target.json` accepts only pinned bundles. If no file exists
and the bundle is generic (no embedded target), the updater fails with
`TARGET_UNKNOWN`. If the file exists but is missing, unreadable, or contains
invalid user or base values, the error is `TARGET_INVALID`.

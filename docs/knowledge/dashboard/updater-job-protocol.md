---
title: "The job protocol between the dashboard and energy-node-updater"
---

# The job protocol between the dashboard and energy-node-updater

The dashboard can redeploy the node it runs on without SSH. It does so by
writing a job file that a root `energy-node-updater` oneshot unit picks up,
and then following that job's log. This page describes that hand-off, which
is the interface a future over-the-air delivery has to feed: getting a new
bundle onto the node in the first place is deliberately out of scope here.

## Where the updater takes its bundle from

`internal/updaterhost.Config.CandidateBundleDir` points at a bundle
directory that is already unpacked but **not yet verified**. Whatever puts
a bundle there is responsible for `manifest.json` and `manifest.json.sig`
belonging to that exact bundle. The verification itself happens only in
`energy-node-updater.sh`, on its next run — never at the sending end.

## The job directory

`/var/lib/energy-node-installer/job/`:

| File | Written by | Read by |
|---|---|---|
| `bundle/` | `internal/updaterjob.Stage` | `energy-node-updater.sh`, which moves it out to a root-only directory before verifying it |
| `job.json` → `pending.json` | `internal/updaterjob.Stage` (an atomic rename as its last step) | `energy-node-updater.path` (existence only), `energy-node-updater.sh` |
| `current.json` | `energy-node-updater.sh` (its first action: `pending.json` renamed) | `internal/updaterjob.InFlight` |
| `log` | `energy-node-updater.sh` (`<unix-millis> <line>`) | `internal/updaterhost.tailJobLog` |
| `status.json` | `energy-node-updater.sh` (its last action) | `internal/updaterjob.ReadStatus` |

## What the job may and may not carry

The job directory belongs to the dashboard's own unprivileged service
account, and `job.json` is not covered by the bundle signature. So the job
says only *which* steps to run, and even that list is held against the step
list in the verified `manifest.json` before anything executes. Everything
that steers a root-run command — the target user, the target base
directory, the bundle version — comes from the manifest instead.

The job carries no secrets either. A redeploy never needs `mqtt.pw` or
`auth.pw`: they already exist on a node the dashboard is running on, and
root reads them from the paths the node's own `config.json` names.

## What an OTA delivery still has to add

- Get a new bundle to `CandidateBundleDir` somehow — download, upload,
  whatever. That is the one remaining gap.
- Nothing else. Staging, verification, execution, progress reporting and
  surviving the dashboard's own restart mid-update are all covered here.

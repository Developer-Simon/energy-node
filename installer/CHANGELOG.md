# Changelog

## v0.1.11 (2026-09-23)

### Features

- **⚠ Breaking — installer:** replace scripts/deploy with the developer CLI (#55) (b7e313e)
- **installer:** make the developer CLI a full replacement for scripts/deploy (167d2a4)

### Refactors

- **⚠ Breaking — scripts:** remove scripts/deploy in favour of the installer CLI (c3a8b5e)

### Documentation

- point developers at the installer CLI for deploying to a node (9da9e28)

## v0.1.10 (2026-09-23)

### Features

- **installer:** add the developer CLI's transport engine (#26) (1f8e44e)
- **webui:** add the installer's layer-3 web UI, browser tests and CI (#28) (e819b5e)
- **installer:** hide dashboard tabs for deselected optional services (#31) (59f2d40)
- **installer:** open the UI in an embedded system WebView (Part C2) (#35) (84bdc7a)
- **installer:** add package sources (file, repo build, GitHub) (#42) (f831eab)
- **dashboard:** download the newest release bundle from the redeploy page (#44) (245b277)
- **services:** give every service its own version and changelog (#45) (5bc91b8)
- **installer:** restart only the service units whose version changed (#48) (1d2c57f)
- **installer:** pass step requirements from the manifest to the web UI (69ffe39)
- **installer:** check the Shelly wake webhook in the diagnose checklist (9cadae6)
- **installer:** carry a restart-all request from the UI to the service steps (bad757a)
- **installer:** show per service which units restart in the preview (e2464cb)
- **installer:** report the bundle upload progress and write concurrently (19df0c0)

### Fixes

- **installer:** restart service units on update and record the installed manifest (#43) (0f2aec2)
- **installer:** make redeploy, repair and repo-built bundles install cleanly (#46) (bf1fe10)
- **shelly:** support sleepy H&T devices with an opt-in wake webhook (#49) (1e6d571)
- **installer:** localize the package-preparation log lines (c67b49d)
- **installer:** expand ~ in the repo package path (873bdda)
- **installer:** replace remote files the SSH user cannot open for writing (eca0198)
- **installer:** give step 20 its MQTT arguments from the node on redeploy and repair (bb4cd40)
- **installer:** add texts for every fault code the bootstrap steps emit (23a64c3)


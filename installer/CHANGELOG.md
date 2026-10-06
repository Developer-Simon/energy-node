# Changelog

## v0.2.0 (2026-10-05)

### Features

- **installer:** check and install system package updates (opt-out) (#90) (d898a63)
- **installer:** carry pending system updates into the preview and diagnose (6ad251f)
- **installer:** check system updates on request and refresh them in the diagnose (b00f687)

## v0.1.16 (2026-10-04)

### Features

- **installer:** remember the access details in the OS keychain (#83) (a0d7a72)
- **installer:** report a pending reboot in the diagnose and add APT_UPGRADE_FAILED (982395f)
- **installer:** add a keychain store for the node access details (5c44a23)

### Refactors

- **installer:** use pointer receivers for the saved credentials (e42fc51)

## v0.1.15 (2026-10-01)

### Features

- **⚠ Breaking — installer:** replace scripts/deploy with the developer CLI (#55) (b7e313e)
- **installer:** break down versions per service in preview and diagnostics (#76) (4719142)
- **installer:** transfer only changed bundle files to the node (#79) (f782acd)
- ship changelog.json in the bundle and show what is new before an update (#82) (d862195)
- **installer:** record changelog.json next to the installed manifest (30629c9)
- **installer:** serve the package changelog next to the node's installed versions (948f4a6)
- **installer:** add DiffManifest, comparing two bundle manifests' file hashes (209e407)
- **installer:** add PackFiles, packing a named subset of a bundle directory (2fa4de9)
- **installer:** add ReadInstalledManifest, downloading the node's last-applied manifest (3fb2d03)
- **installer:** add DeployDelta, an incremental counterpart to Deploy (004fc07)
- **installer:** stage the dev CLI's deploy incrementally, with a confirmed full-retransfer fallback (6ddd357)
- **installer:** add --force-full to the deploy subcommand (42902cb)
- **installer:** stage the web UI host's prepare step incrementally (ee2f587)
- **installer:** diff deltas against the verified bundle dir, hashing it as fallback (6671680)
- **devcli:** update bundle API callers for incremental transfers (297326a)
- **host:** update bundle API callers for incremental transfers (466d314)
- **installer:** carry installed versions and devices into the diagnose view (4e6937b)

### Fixes

- **installer:** keep the changed service selection in the update preview (#80) (2341643)
- **installer:** show the saved service selection in the update preview (83fe286)
- **installer:** plan the update preview against the saved service selection (c2e38f3)
- **installer:** use a local path join for the delta signature check (1231471)
- **installer:** stage the dev CLI's delta from the local build directory (b2f22d0)
- **installer:** parse hashed file names exactly and test the verify error detail (b00dfd4)
- **bootstrap:** hash-check unsigned bundles on the node with --no-signature (e0c8380)

### Tests

- **installer:** prove a delta transfer verifies exactly like a full one (195d7c0)

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


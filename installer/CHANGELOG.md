# Changelog

## v0.3.3-pre.2 (2026-11-13)

### Features

- **installer:** The installer prepares the SD card for a Raspberry Pi itself, with Wi-Fi and SSH access set up (#236) (6d169d7)
- **network:** choose the Wi-Fi fallback and manage the emergency hotspot (#239) (83a275f)
- **installer:** show the emergency hotspot credentials after install (df40283)
- **installer:** stage the hotspot password for step 67 (adca255)
- **installer:** pass the hotspot password to the host only on install (c04ec74)
- **installer:** add the device table for the media mode (656ba7b)
- **installer:** read the Raspberry Pi OS list (ed78124)
- **installer:** download and verify images for the media mode (8880f68)
- **installer:** list drives for the media mode and hide system disks (c9a93e5)
- **installer:** generate cloud-init and firstrun files with a fixed host key (b33a361)
- **installer:** find the Raspberry Pi Imager and read its progress (a0b3dcc)
- **installer:** pin pre-generated host keys and probe new devices (cee0e34)
- **installer:** validate media write requests (88acb34)
- **installer:** run media writes through the Raspberry Pi Imager (f0083e2)
- **installer:** read timezone, keyboard and region from this computer (5244654)
- **installer:** offer the media mode on Linux (678ae64)
- **installer:** prefill the Wi-Fi name from this computer (cf30615)
- **installer:** compare the Wi-Fi password with the one this computer knows (a7168b6)
- **installer:** check the Wi-Fi password against this computer and pass the band (2128fcf)
- **installer:** list the Wi-Fi networks in range (8810f50)
- **installer:** scan for Wi-Fi networks on request (c936754)
- **installer:** check the Wi-Fi password against the twin on the other band (650b726)
- **installer:** count the Pi 3 as tested and put the Pi Zero with the Pi 1 (2c5dc68)
- **installer:** allow SSH login with the emergency password on request (b07a90a)

### Fixes

- **installer:** keep the media cache check cheap (5553171)
- **installer:** read the Wi-Fi name without a rescan and keep its band (092e503)
- **installer:** unblock Wi-Fi on Raspberry Pi OS by setting its country (ffb360a)

## v0.3.2 (2026-11-07)

### Features

- **installer:** The installer now fetches Caddy, Tailscale and the Python wheels itself and checks each file against the signed manifest, so releases are smaller. (#222) (f1344a1)
- **bundle:** ship package pins instead of the Tailscale tarball (0a32dd8)
- **bundle:** pin third-party wheels instead of shipping them (fe04ddd)
- **bootstrap:** add helpers to install pinned Debian packages (be678a4)
- **bootstrap:** install Tailscale from the pinned package (26e9d5b)
- **bootstrap:** install Caddy from the pinned package (7ba8a8b)
- **bootstrap:** fetch missing wheels on the node before installing (02b2a5c)
- **installer:** download and verify pinned packages (07882a8)
- **installer:** transfer pinned packages and wheels to the node (7b39f8e)
- **installer:** ask when a pinned package is missing on the node (0716b8d)
- **installer:** fall back to the upstream pool when the package mirror fails (7aa921e)

### Refactors

- point the remaining template readers at the new path (3cc3d01)

### Chores

- remove the shared services component (#209) (28d50a7)

## v0.3.0 (2026-11-03)

### Features

- **installer:** The installer asks for the sudo password in a dialog and sets up passwordless sudo on the device. (#187) (c179354)
- **installer:** The installer can load the Python dependencies online when the node's Python version differs from the package, after you confirm it. (#189) (86dfb1e)
- **installer:** The installer has a Dashboard step where users, guest access and the look of the dashboard can be preset or left for later. (#190) (f03d069)
- **installer:** The installer diagnose shows a live log of the dashboard and the updater service with their state. (#193) (c4b434a)
- **installer:** repair the MQTT login with a new password (284572b)
- **installer:** report a refused MQTT login in the diagnose (b364cbb)
- **installer:** parse journal lines and build the remote journal command (10cda6c)
- **installer:** stream dashboard and updater journals over SSH (9b73955)
- **installer:** hand the dashboard presets to the node (1fea19b)
- **installer:** let the node fetch Python dependencies online when the operator allows it (f8be687)
- **installer:** translate package check messages and flag the decisions a run waits for (1a3aa24)
- **installer:** add an opt-in step that keeps the system journal in RAM only (fa45acc)
- **installer:** set up passwordless sudo and register the Linux window icon (3ebc64c)
- **installer:** ask for the sudo password in a dialog on the precheck screen (1519c22)

### Fixes

- **installer:** A fresh install now starts the dashboard. It no longer fails on a missing data folder or on an MQTT login the broker rejects. (#191) (a96bebf)
- **installer:** The installer diagnose now detects an MQTT login that the broker refuses and repairs it with a new password. (#194) (ef2d7d6)
- **onboarding:** apply the installer admin name and detect more setup steps as done (#202) (c3a37e8)
- **installer:** pass the admin username from the dashboard step to step 60 (623ab0a)
- **installer:** force the step on a diagnose repair (fa2a5bc)
- **installer:** end the journal session when journalctl exits and redact its error (fef88a9)
- **installer:** run every step in a first setup, skip done steps only on update (5de64f7)
- **installer:** write the broker user into config.json (1be6458)
- **installer:** word the Python ABI mismatch detail in the window's language (2ba8848)
- **installer:** let a tolerated Python ABI pass even when the package predates the consent (73fbcb3)
- **installer:** open links in the system browser from the installer window (3aae129)
- **installer:** install the Python dependencies next to system packages (4aa1498)
- **installer:** save logs and reports through the host (bb903fc)

### Refactors

- **installer:** install the Python dependencies online from the installer itself (a626640)

## v0.2.3 (2026-10-14)

### Features

- **changelog:** Choose how many versions the changelog shows, with the history kept on the device across updates. (#137) (14f781e)
- **installer:** keep every received changelog version on the node (fb8eecd)

## v0.2.2 (2026-10-11)

### Features

- **dashboard:** show update progress and what's new after an update (#123) (7c1388f)
- **updater:** keep the previously installed manifest on a version change (45d303d)

## v0.2.1 (2026-10-06)

### Features

- **installer:** check and install system package updates (opt-out) (#90) (d898a63)
- **installer:** carry pending system updates into the preview and diagnose (6ad251f)
- **installer:** check system updates on request and refresh them in the diagnose (b00f687)

### Fixes

- **installer:** skip unchanged update steps and keep the session across the restart (#102) (726ad05)
- **installer:** skip unchanged steps on an update (446e9d2)

## v0.1.16 (2026-10-04)

### Features

- **installer:** remember the access details in the OS keychain (#83) (a0d7a72)
- **installer:** report a pending reboot in the diagnose and add APT_UPGRADE_FAILED (982395f)
- **installer:** add a keychain store for the node access details (5c44a23)

### Refactors

- **installer:** use pointer receivers for the saved credentials (e42fc51)

## v0.1.15 (2026-10-01)

### Features

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

## v0.1.3 (2026-09-20)

### Features

- **installer:** open the UI in an embedded system WebView (Part C2) (#35) (84bdc7a)

## v0.1.2 (2026-09-15)

### Features

- **webui:** add the installer's layer-3 web UI, browser tests and CI (#28) (e819b5e)
- **installer:** hide dashboard tabs for deselected optional services (#31) (59f2d40)

## v0.1.0 (2026-09-13)

### Features

- **installer:** add the developer CLI's transport engine (#26) (1f8e44e)


# Changelog

## v0.3.4-pre.10 (2026-11-06)

### Features

- **energy:** add measurement chains with substitute values (#215) (0457bcc)
- **installer:** The installer prepares the SD card for a Raspberry Pi itself, with Wi-Fi and SSH access set up (#236) (6d169d7)
- **network:** choose the Wi-Fi fallback and manage the emergency hotspot (#239) (83a275f)
- **installer:** stream the diagnose and show skeleton rows while it loads (#244) (faa61a6)
- **installer:** write a Debian install stick from the media mode (#255) (cb9e1aa)
- **onboarding:** start empty and offer an example to try out (#274) (35490ab)
- **installer:** The configure screen of the installer shows its services and tuning as modules (#280) (f85e2a6)
- **installer:** apply a changed service selection without a full update (#282) (ab11a7a)
- **installer:** highlight that the Debian stick needs a network cable (#286) (01a23f2)
- **installer:** show the Windows install paths for the imager (559aae2)
- **installer:** prepare storage media on Windows (#298)
- **installer:** prepare from the package stored on the node (f5b1c87)
- **installer:** accept the apply-selection run mode (5dc8abe)
- **installer:** offer the package stored on the node as a source (1ac4e7a)
- **installer:** offer apply selection in the update preview (c1a616d)
- **installer:** show the run and result of an applied selection (6aaca33)
- **installer:** point to the node package from the package list (0e761e9)
- **installer:** group the configure steps into service, tuning and example modules (e1ea6c1)
- **installer:** add the module icons from the dashboard pool (f651129)
- **installer:** drive the configure screen from the service modules (5c7624e)
- **installer:** show the configure services as control center modules (66bdc03)
- **installer:** animate the configure modules quietly (3c1c6d0)
- **installer:** show the update preview services as modules (7a2fca3)
- **installer:** allow a step to require several steps (47b7493)
- **installer:** offer the example on a first install (68b81a0)
- **installer:** add the Debian devices to the media device table (7fa1b05)
- **installer:** pass the Debian write settings and errors through the host (bc13024)
- **installer:** expose the Debian media settings and steps in the host API (16fbf0a)
- **installer:** add the Debian model rules to the media mode (f0cfd63)
- **installer:** add the Debian choices to the media device and settings steps (eec993f)
- **installer:** add the Debian drive step to the media mode (9ed563a)
- **installer:** add the Debian write and done steps to the media mode (1922bfa)
- **network:** add the emergency hotspot bootstrap step (d30f816)
- **installer:** show the emergency hotspot credentials after install (df40283)
- **installer:** carry the hotspot password through the run request (cca04e8)
- **installer:** pass the hotspot password to the host only on install (c04ec74)
- **installer:** ask for an optional hotspot password on the configure screen (4690793)
- **webui:** add the media API and its fake (5a7025d)
- **webui:** add the media entry to the shell (c6b1e3e)
- **webui:** add the pure helpers of the media mode (d7295e0)
- **webui:** add the device and settings screens of the media mode (47a79e0)
- **webui:** add the drive screen of the media mode (cca1afa)
- **webui:** write the medium and hand over to setup (d2e0f1f)
- **installer:** prefill the Wi-Fi name from this computer (cf30615)
- **installer:** check the Wi-Fi password against this computer and pass the band (2128fcf)
- **webui:** take the Wi-Fi country from the time zone (5d648e5)
- **webui:** check the Wi-Fi password and warn about 5 GHz (2925948)
- **installer:** list the Wi-Fi networks in range (8810f50)
- **webui:** offer the Wi-Fi networks in range (6e4253d)
- **installer:** scan for Wi-Fi networks on request (c936754)
- **webui:** load the Wi-Fi networks in range after the screen opens (35f8ec7)
- **webui:** pick the Wi-Fi from a list and offer the 2.4 GHz twin (6c38fa0)
- **installer:** check the Wi-Fi password against the twin on the other band (650b726)
- **webui:** name the twin network and say when the password cannot be checked (97c6d6b)
- **installer:** count the Pi 3 as tested and put the Pi Zero with the Pi 1 (2c5dc68)
- **installer:** allow SSH login with the emergency password on request (b07a90a)
- **webui:** add a switch for SSH login with the emergency password (72eee4a)
- **webui:** point first-time users to the media mode (d8d59c1)
- **webui:** pick time zone and keyboard from rows instead of drop-downs (05d9951)

### Fixes

- **installer:** keep the apply selection button while it runs (5281d56)
- **installer:** translate the skip reasons of step 89 (23d333a)
- **installer:** show the hotspot credentials after every successful run (bbeac07)
- **webui:** bust the asset cache on every asset change (90fcdf8)
- **webui:** keep the language switch on the media screens (040d376)
- **webui:** center the radio dot and color the spinners of the media mode (dbf5347)
- **webui:** leave space below the drive list and say SD-Karte (96e292e)
- **webui:** keep the media hint above the page and the SD card name in one piece (d038b46)

### Refactors

- **installer:** move the media platform choices behind one file per system (5738454)

### Documentation

- **installer:** name the winget link kind (31d78ce)
- **installer:** draw the configure modules in the reference draft (ffb05a9)
- **installer:** draw the update preview modules in the reference draft (505550a)
- **installer:** tighten the media texts and describe the Wi-Fi list (553f296)

### Tests

- **installer:** show the node package and apply selection in the fakehost (2a14121)
- **installer:** follow the reworded package text in the connect draft (9a90595)
- **installer:** show the journal and tailscale log steps in the fakehost (d894248)
- **installer:** give the fakehost the example step (2426446)
- **installer:** cover the Debian stick in the media e2e (46daa09)
- **installer:** wait for the diagnose skeleton rows instead of the old banner (8ea5ba1)
- **webui:** cover the media mode end to end (69364e7)
- **webui:** pick the device list, not the language switch, in the media e2e (6087dff)

### Chores

- **installer:** polish the German module texts (f8bd618)

## v0.3.3 (2026-11-16)

### Fixes

- **system:** cut SD card writes from tailscaled logs and runtime.json (#271) (05dc304)

## v0.3.2 (2026-11-07)

### Features

- **installer:** The installer now fetches Caddy, Tailscale and the Python wheels itself and checks each file against the signed manifest, so releases are smaller. (#222) (f1344a1)
- **bootstrap:** add helpers to install pinned Debian packages (be678a4)
- **bootstrap:** install Tailscale from the pinned package (26e9d5b)
- **bootstrap:** install Caddy from the pinned package (7ba8a8b)
- **bootstrap:** fetch missing wheels on the node before installing (02b2a5c)
- **installer:** download and verify pinned packages (07882a8)
- **installer:** transfer pinned packages and wheels to the node (7b39f8e)
- **installer:** ask when a pinned package is missing on the node (0716b8d)
- **installer:** ask how to continue when a package could not be downloaded (7702bf1)
- **installer:** fall back to the upstream pool when the package mirror fails (7aa921e)
- **versions:** name the shared libraries in the version overviews (5b9eaba)

### Fixes

- **installer:** pass error arguments through the run-finished event (8d5dc7c)

### Chores

- remove the shared services component (#209) (28d50a7)

## v0.3.0 (2026-11-05)

### Features

- **installer:** The installer asks for the sudo password in a dialog and sets up passwordless sudo on the device. (#187) (c179354)
- **installer:** The installer can load the Python dependencies online when the node's Python version differs from the package, after you confirm it. (#189) (86dfb1e)
- **installer:** The installer has a Dashboard step where users, guest access and the look of the dashboard can be preset or left for later. (#190) (f03d069)
- **installer:** The installer diagnose shows a live log of the dashboard and the updater service with their state. (#193) (c4b434a)
- **installer:** report a refused MQTT login in the diagnose (b364cbb)
- **installer:** ask for the MQTT password when repairing the login (b130360)
- **installer:** stream unit logs over /api/unit-logs (a233363)
- **installer:** buffer, scrub and stream live unit logs in the page (85fb1d7)
- **installer:** export live logs as report section and issue markdown (c92459d)
- **installer:** add the live log card markup, styles and texts to the diagnose (7ef2f78)
- **installer:** drive the diagnose live log card (d40ef59)
- **installer:** add the dashboard step logic (79acd60)
- **installer:** add the dashboard step (37e101b)
- **installer:** hand the dashboard presets to the node (1fea19b)
- **installer:** let the node fetch Python dependencies online when the operator allows it (f8be687)
- **installer:** offer the online dependency install in the prepare screen (24536b2)
- **installer:** translate package check messages and flag the decisions a run waits for (1a3aa24)
- **installer:** add an opt-in step that keeps the system journal in RAM only (fa45acc)
- **installer:** set up passwordless sudo and register the Linux window icon (3ebc64c)
- **installer:** ask for the sudo password in a dialog on the precheck screen (1519c22)

### Fixes

- **installer:** skip recommended apt packages and clean the package cache (#192) (bc356b2)
- **installer:** The installer diagnose now detects an MQTT login that the broker refuses and repairs it with a new password. (#194) (ef2d7d6)
- **onboarding:** apply the installer admin name and detect more setup steps as done (#202) (c3a37e8)
- **installer:** apply the dashboard name rule and unique names in the dashboard step (4d8ba78)
- **installer:** pass the admin username from the dashboard step to step 60 (623ab0a)
- **installer:** reword the MQTT login failure text (85eb618)
- **installer:** fit the issue markdown after scrubbing and mask ipv6 after a colon (a17aae8)
- **installer:** keep the diagnose columns from collapsing under the live log card (4c87895)
- **installer:** fade only live-appended log lines, not the rebuild after a unit switch (265ca72)
- **installer:** add space above the live log card in the diagnose (227fbfc)
- **installer:** show the real guest access default and tighten the updates text (4a1d4fb)
- **installer:** say what to do when the package needs another Python version (aaf9d68)
- **installer:** keep the chosen package source while connecting (7acba32)
- **installer:** word the Python ABI mismatch detail in the window's language (2ba8848)
- **installer:** reword the decision notice and tighten the Python version texts (10cf952)
- **installer:** let a tolerated Python ABI pass even when the package predates the consent (73fbcb3)
- **installer:** open links in the system browser from the installer window (3aae129)
- **installer:** save logs and reports through the host (bb903fc)
- **installer:** tighten the sudo texts (a73c68f)

### Refactors

- **installer:** install the Python dependencies online from the installer itself (a626640)

### Tests

- **docs:** script the connect, fingerprint and result installer screenshots (c230110)
- **docs:** script the installer screenshots for the new steps (df08a14)
- **installer:** add a refused MQTT login scenario to the fake host (246dac1)
- **installer:** cover the MQTT password repair dialog in the browser (ef7eb33)
- **installer:** fake live logs and cover the issue copy end to end (ce61140)
- **installer:** configure draft ends with Weiter (64f407b)
- **installer:** install drafts show the dashboard step in the stepper (6064db4)

### Chores

- **release:** prepare v0.11.0 (#203) (fda100b)
- **fakehost:** add --journal-step to show the RAM journal step (49932e3)

## v0.2.6 (2026-10-20)

### Fixes

- clearer update notice, firewall station completes, service steps skip unchanged libraries (#153) (c184d81)
- **installer:** firewall station completes without the webhook step (4d33879)

### Tests

- **installer:** fakehost scenario for the dashboard updater run (2c3cb0d)

## v0.2.5 (2026-10-16)

### Features

- **changelog:** Release highlights now have a German text that shows in the dashboard, the installer and the release notes. (#134) (ccd90ca)
- **changelog:** Choose how many versions the changelog shows, with the history kept on the device across updates. (#137) (14f781e)
- **installer-webui:** merge the per-version changelog files (77e912d)
- **installer-webui:** show release highlights in the page language (59987e3)

### Fixes

- **installer:** fit the update result screen to the dashboard host (#131) (494c792)
- **dashboard:** Help texts, device map, energy roles and statistics fit narrow screens, and export and battery charge are hatched in the statistics. (#144) (ba54b31)
- **installer:** result screen keeps the update's from-version after a reload (be443d2)

## v0.2.1 (2026-10-10)

### Fixes

- **installer:** list every step and service as its own run station (#121) (abca58b)

### Documentation

- **installer:** refresh the run screenshot and show log text in the window language (14f3827)

## v0.2.0 (2026-10-05)

### Features

- **installer:** check and install system package updates (opt-out) (#90) (d898a63)
- **installer:** carry pending system updates into the preview and diagnose (6ad251f)
- **installer:** show pending system updates in the preview and diagnose (300f77d)
- **installer:** check system updates on request and refresh them in the diagnose (b00f687)

### Documentation

- **installer:** describe the system update step (d8c6071)

## v0.1.18 (2026-10-04)

### Features

- **installer:** remember the access details in the OS keychain (#83) (a0d7a72)
- The changelog pages open on the highlights, the switch at the top shows everything. (#84) (5cc1cfb)
- **installer:** report a pending reboot in the diagnose and add APT_UPGRADE_FAILED (982395f)
- **installer:** offer the system update step and show a pending reboot (10568b2)
- **installer:** changelog model knows highlights (3f23a46)
- **installer:** open what's new on the highlights with a switch to everything (a45f21d)
- **webui:** carry saved access details through bootstrap and connect (b903522)
- **installer:** add a remember switch for the access details to the connect screen (ba9d513)
- **installer:** make the key option on the connect screen a switch (58c9837)

### Fixes

- **installer:** prefill the connect form once the bootstrap has arrived (32cd044)

### Documentation

- highlights and details, screenshot script for the installer (105e72e)

## v0.1.16 (2026-10-01)

### Features

- **installer:** break down versions per service in preview and diagnostics (#76) (4719142)
- **installer:** transfer only changed bundle files to the node (#79) (f782acd)
- ship changelog.json in the bundle and show what is new before an update (#82) (d862195)
- **webui:** add GET /api/changelog as an optional backend capability (7a3aeea)
- **webui:** add a model that slices a changelog against the installed versions (471ea80)
- **webui:** add the changelog screen with breaking changes and filters (611854f)
- **webui:** summarise what is new in the update preview (c44a811)
- **installer:** add catalog entries for the partial transfer UI (a07a73a)
- **installer:** stage the web UI host's prepare step incrementally (ee2f587)
- **installer:** offer a full retransfer after a delta verify failure (e0f7fb1)
- **installer:** add a connect-screen checkbox to force a full transfer (037f9f4)
- **installer:** add log texts for comparing and transferring changed files (69609ed)
- **installer:** use a switch for the full transfer option (8b3f426)
- **installer-webui:** break down service versions and restart reasons in the update preview (6a96afd)
- **installer:** carry installed versions and devices into the diagnose view (4e6937b)
- **installer-webui:** show versions, devices and not-installed services in diagnostics (2225623)

### Fixes

- **installer:** keep the changed service selection in the update preview (#80) (2341643)
- **webui:** keep the existing error texts and the catalog word (3fb6426)
- **webui:** spell the fixes group as Korrekturen (e0a6654)
- **webui:** style the entry switch on the changelog screen (d876035)
- **installer:** show the saved service selection in the update preview (83fe286)

### Tests

- **webui:** give the dev host a changelog and cover the screen in the browser (65ad1f5)
- **installer-webui:** per-service versions and devices in the fakehost scenarios (e8fed9a)

### Chores

- **webui:** fix a word in a model comment (75e2631)

## v0.1.12 (2026-09-29)

### Features

- **dashboard:** show installed component versions under settings (#71) (c8ef59b)
- **dashboard:** show the redeploy screen in the dashboard language (#74) (6542022)
- **installer-webui:** resolve the host language per request (af62b5c)
- **installer-webui:** translate the dashboard host's error codes (0f8a231)
- **dashboard:** note the bundle download in catalog keys (a158773)
- **webui:** add hostapi helpers that read changelog.json and installed component versions (17e0f19)

### Fixes

- **redeploy:** keep following a run across the dashboard's self-update restart (#52) (38a0609)
- **dashboard:** tidy localization texts after the final review (c2b5853)
- **dashboard:** drop two unused catalog keys (b1e01ca)
- **webui:** add the changelog error codes to the installer web UI catalogs (6f23aff)

### Documentation

- **dashboard:** final review of the localization (#75) (ea05c63)

## v0.1.8 (2026-09-23)

### Features

- **installer:** add package sources (file, repo build, GitHub) (#42) (f831eab)
- **dashboard:** download the newest release bundle from the redeploy page (#44) (245b277)
- **installer:** restart only the service units whose version changed (#48) (1d2c57f)
- **installer-webui:** add an opt-in toggle for the Shelly wake webhook rule (48ea429)
- **installer:** pass step requirements from the manifest to the web UI (69ffe39)
- **installer-webui:** show the wake webhook switch only with the Shelly service (e7f8b59)
- **installer-webui:** show the Shelly wake webhook on the diagnose screen (32340d3)
- **installer:** carry a restart-all request from the UI to the service steps (bad757a)
- **installer:** show per service which units restart in the preview (e2464cb)
- **webui:** restart only the services that changed, with a restart-all switch (63aa406)
- **webui:** give the prepare step its own stepper entry (787d1bb)
- **installer:** report the bundle upload progress and write concurrently (19df0c0)

### Fixes

- **installer:** make redeploy, repair and repo-built bundles install cleanly (#46) (bf1fe10)
- **shelly:** support sleepy H&T devices with an opt-in wake webhook (#49) (1e6d571)
- **installer:** localize the package-preparation log lines (c67b49d)
- **webui:** stop the "restart all" label from overlapping neighbouring text (bbf1e27)
- **webui:** update the design drafts for the new prepare stepper entry (c958d03)
- **webui:** show the error detail of a failed run (5017432)
- **installer:** give step 20 its MQTT arguments from the node on redeploy and repair (bb4cd40)
- **installer:** add texts for every fault code the bootstrap steps emit (23a64c3)

## v0.1.3 (2026-09-16)

### Features

- **webui:** add the installer's layer-3 web UI, browser tests and CI (#28) (e819b5e)
- **installer:** polish for the installer webui (#30) (05d424b)
- **installer:** hide dashboard tabs for deselected optional services (#31) (59f2d40)
- **installer:** add the dashboard's local self-update path (Plan D) (#33) (e50d236)
- **dashboard:** add a GitHub update check with a masthead notification (#34) (e5fc9db)


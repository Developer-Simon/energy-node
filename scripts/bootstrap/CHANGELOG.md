# Changelog

## v0.2.2 (2026-10-06)

### Features

- **installer:** check and install system package updates (opt-out) (#90) (d898a63)
- **bootstrap:** report pending system package updates in plan and diagnose (670b080)
- **installer:** check system updates on request and refresh them in the diagnose (b00f687)

### Fixes

- **installer:** skip unchanged update steps and keep the session across the restart (#102) (726ad05)
- **installer:** skip unchanged steps on an update (446e9d2)
- **installer:** harden step 15 and select it on dashboard updates (bab88e1)
- **bootstrap:** give the ok detail its own function for shellcheck 0.9 (9c1a8b5)

### Documentation

- redesign the documentation site with an Energy Node layout (#94) (5a7080f)
- point the repository at the new pages and add site search (72fa46e)

## v0.1.13 (2026-10-01)

### Features

- **dashboard:** show installed component versions under settings (#71) (c8ef59b)
- **installer:** break down versions per service in preview and diagnostics (#76) (4719142)
- **installer:** transfer only changed bundle files to the node (#79) (f782acd)
- **bootstrap:** check and install system package updates in an opt-out step 15 (49f9389)
- **installer:** report a pending reboot in the diagnose and add APT_UPGRADE_FAILED (982395f)
- **bootstrap:** report installed versions, devices and never-installed services in diagnose.sh (1f57d70)

### Fixes

- **bootstrap:** hash-check unsigned bundles on the node with --no-signature (e0c8380)

## v0.1.11 (2026-09-28)

### Features

- **dashboard:** show installed component versions under settings (#71) (c8ef59b)

### Refactors

- **dashboard:** stop reading the services VERSION file on the node (c1e19de)

## v0.1.10 (2026-09-23)

### Features

- **installer:** build the node-half bootstrap chain and signed bundle pipeline (#25) (f11e962)
- **installer:** add the developer CLI's transport engine (#26) (1f8e44e)
- **webui:** add the installer's layer-3 web UI, browser tests and CI (#28) (e819b5e)
- **installer:** hide dashboard tabs for deselected optional services (#31) (59f2d40)
- **installer:** add the dashboard's local self-update path (Plan D) (#33) (e50d236)
- **dashboard:** download the newest release bundle from the redeploy page (#44) (245b277)
- **installer:** restart only the service units whose version changed (#48) (1d2c57f)
- **shelly:** optional wake webhook so a sleeping Gen1 device is polled the moment it wakes (3105234)
- **bootstrap:** support opt-in optional steps (3465818)
- **bootstrap:** open the Shelly wake webhook port only on explicit opt-in (18436ce)
- **bootstrap:** only open the wake webhook port while the Shelly service is selected (00a841a)
- **bootstrap:** report the Shelly wake webhook rule and listener in diagnose.sh (04b401b)
- **bootstrap:** add the rule that decides which service units restart (5405aca)
- **bootstrap:** restart a service unit only when its version or library changed (440f113)
- **installer:** show per service which units restart in the preview (e2464cb)

### Fixes

- **bootstrap:** make steps 20, 40, 65, 70 and the diagnosis work on a real node (#37) (11eef9d)
- **installer:** restart service units on update and record the installed manifest (#43) (0f2aec2)
- **installer:** make redeploy, repair and repo-built bundles install cleanly (#46) (bf1fe10)
- **shelly:** support sleepy H&T devices with an opt-in wake webhook (#49) (1e6d571)
- **bootstrap:** ignore a commented-out userspace-networking flag in step 40 (a1aeee1)
- **bootstrap:** let step 70 keep an installed Caddy when the bundle has no Caddy pack (f8fbc8d)


# Changelog

## v0.1.9 (2026-09-23)

### Fixes

- **redeploy:** keep following a run across the dashboard's self-update restart (#52) (38a0609)

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


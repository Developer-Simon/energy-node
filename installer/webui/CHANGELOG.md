# Changelog

## v0.1.6 (2026-09-21)

### Features

- **installer:** add package sources (file, repo build, GitHub) (#42) (f831eab)
- **dashboard:** download the newest release bundle from the redeploy page (#44) (245b277)
- **installer:** report the bundle upload progress and write concurrently (19df0c0)

### Fixes

- **installer:** make redeploy, repair and repo-built bundles install cleanly (#46) (bf1fe10)
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


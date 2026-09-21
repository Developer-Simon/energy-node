# Changelog

## v0.1.8 (2026-09-21)

### Features

- **installer:** build the node-half bootstrap chain and signed bundle pipeline (#25) (f11e962)
- **installer:** add the developer CLI's transport engine (#26) (1f8e44e)
- **webui:** add the installer's layer-3 web UI, browser tests and CI (#28) (e819b5e)
- **installer:** hide dashboard tabs for deselected optional services (#31) (59f2d40)
- **installer:** add the dashboard's local self-update path (Plan D) (#33) (e50d236)
- **dashboard:** download the newest release bundle from the redeploy page (#44) (245b277)

### Fixes

- **bootstrap:** make steps 20, 40, 65, 70 and the diagnosis work on a real node (#37) (11eef9d)
- **installer:** restart service units on update and record the installed manifest (#43) (0f2aec2)
- **bootstrap:** ignore a commented-out userspace-networking flag in step 40 (a1aeee1)
- **bootstrap:** let step 70 keep an installed Caddy when the bundle has no Caddy pack (f8fbc8d)


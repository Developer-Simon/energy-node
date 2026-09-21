# Changelog

## v0.1.8 (2026-09-21)

### Features

- **installer:** add the developer CLI's transport engine (#26) (1f8e44e)
- **webui:** add the installer's layer-3 web UI, browser tests and CI (#28) (e819b5e)
- **installer:** hide dashboard tabs for deselected optional services (#31) (59f2d40)
- **installer:** open the UI in an embedded system WebView (Part C2) (#35) (84bdc7a)
- **installer:** add package sources (file, repo build, GitHub) (#42) (f831eab)
- **dashboard:** download the newest release bundle from the redeploy page (#44) (245b277)
- **services:** give every service its own version and changelog (#45) (5bc91b8)
- **installer:** report the bundle upload progress and write concurrently (19df0c0)

### Fixes

- **installer:** restart service units on update and record the installed manifest (#43) (0f2aec2)
- **installer:** expand ~ in the repo package path (873bdda)
- **installer:** replace remote files the SSH user cannot open for writing (eca0198)
- **installer:** give step 20 its MQTT arguments from the node on redeploy and repair (bb4cd40)
- **installer:** add texts for every fault code the bootstrap steps emit (23a64c3)


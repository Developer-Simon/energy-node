---
title: "The dashboard, page by page"
redirect_from:
  - /dashboard.html
anchor_moves:
  start--the-overview-pages: overview.html
  the-layout-editor: overview.html#the-layout-editor
  devices: devices.html
  device-map: devices.html#device-map
  history: history.html
  configuration: configuration.html
  energy: energy.html
  diagnostics: diagnostics.html
  automations: automations.html
  settings: settings.html
  general: settings.html#general
  appearance: settings.html#appearance
  history-1: settings.html#history
  mqtt: settings.html#mqtt
  tailscale: settings.html#tailscale
  tinytuya: settings.html#tinytuya
  versions: settings.html#versions
  system: settings.html#system
  updating-from-the-dashboard: settings.html#updating-from-the-dashboard
  colour-schemes: themes.html
---

# The dashboard, page by page

The Energy Node dashboard is a single statically linked Go binary. Templates,
CSS and JavaScript are compiled into it with `go:embed`. There is no build
step, no CDN and no server side database. The dashboard renders HTML on the
server, pushes value updates over SSE and keeps chart history in the browser.

The UI is in German and English. Each browser picks its language with the
`DE | EN` switch in the header (see the localization notes in the repository).
The screenshots and the labels quoted here are the English ones. Device and
entity names are not translated. They come from MQTT Discovery in whatever
language the devices announce them, which is German for the fixture devices in
the screenshots.

All screenshots come from the local smoke test with the `docs-screenshots`
preset: the real dashboard against a fixture broker, without a Raspberry Pi or
any device.

## The window frame

![The dashboard on a wide screen](../images/dashboard.png)

Every tab has the same frame.

The system status bar shows the broker connection, storage health, uptime and
dashboard version. You can choose which of these fields appear or switch the
bar off.

The tab bar starts with the layout pages, which are your own overview pages
named in the layout editor. After the divider come the fixed tabs: Devices,
History, Configuration, Energy, Device Map, Diagnostics, Automations and
Settings.

The frame also shows who you are logged in as. Over plain HTTP the dashboard
only offers *continue as guest* and refuses the admin login unless the request
arrives over HTTPS. A guest can look at everything and edit layouts and
automation rules. The protected actions need a registered user with the
matching role and HTTPS: the MQTT and bridge configuration (`mqtt_config`) and
restart, reboot and shutdown (`system_actions`). See
[operating/secrets](../operating/secrets.md).

Panels load lazily. The browser only fetches the HTML fragment, scripts and
stylesheets of the active tab, which keeps the first paint cheap on a
Raspberry Pi 1.

## Pages

- [Overview and layout editor](overview.md)
- [Devices and device map](devices.md)
- [History](history.md)
- [Energy](energy.md)
- [Automations](automations.md)
- [Diagnostics](diagnostics.md)
- [Configuration editor](configuration.md)
- [Settings](settings.md)
- [Colour schemes](themes.md)

## See also

- [Device services](../services/index.md): where the dashboard's data comes from
- [HTTP API](../developing/api.md): the `/api/v1` API behind every page
- [Data flows](../developing/data-flow.md): where each value comes from
- [Reverse proxy](../operating/reverse-proxy.md): running the dashboard under a sub-path

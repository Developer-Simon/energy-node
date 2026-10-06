---
title: "Configuration editor"
---

# Configuration editor

![Configuration editor for the battery service](../images/dashboard-config.png)

This page edits the device JSON files of the Python services, the same
`*_devices.json` files described in [Device services](../services/index.md).
The form is generated from the JSON schema next to each config file, so field
names, help texts, required markers and value ranges all come from one place.

The dropdowns suggest MQTT topics and JSON keys from retained payloads the
broker has actually carried. A topic that has never carried a message says so
instead of offering an empty list.

Values are validated before anything is written. A value outside the schema's
range is rejected with HTTP 400, so it cannot end up in the file and stop a
service from starting later.

Saving writes the file and asks the owning service to reload it. The response
carries the reload result, so if a service refuses the new file you see it here
and not on the next restart. Editing automation rules also requires the
`automations` role and a CSRF token.

---
title: "Settings"
---

# Settings

Settings has eight sub-tabs: General, Appearance, History, MQTT, Tailscale,
TinyTuya, Versions and System. Tailscale and TinyTuya only appear when the
matching service is installed.

## General

![General settings](../images/dashboard-settings-general.png)

This tab holds the health threshold, the sweep interval, the live update
interval and the storage health check. On a Pi the check reads the SD card's
wear counters. When the root filesystem is not a readable MMC medium, the page
says so. Settings are versioned, and **Settings revisions** restores an earlier
state. The footer lists every bundled JavaScript library with its version,
license and license link.

## Appearance

![Appearance settings](../images/dashboard-settings-display.png)

Here you set the default device view, the colour scheme and which extra
information the UI shows: discovery JSON tooltips, the global status bar, and
configuration and diagnostic values on tiles. **Tabs without a width limit**
lets selected tabs use the full window width instead of the centred column.
**Items in the system status** picks the fields in the status bar.

The **Formatting** card holds the language of this browser, the switch that
shows or hides the `DE | EN` pill for everyone, and the number format.

## History

![History settings](../images/dashboard-settings-history.png)

This tab controls recording and retention of the browser side history: the
sample rate, how long raw data is kept before compaction and how long minute
values are kept. Retention is limited either by time or by a storage budget in
MB. The page shows the projected daily volume, the actual usage and the
browser's quota. The last block switches off the exchange between devices.

The setting applies to every browser, but the recorded data does not. The
measurements only exist in the browser that recorded them, and the page says so.

## MQTT

![MQTT settings](../images/dashboard-settings-mqtt.png)

The broker connection comes with a live status table: connected or not,
address, where the configuration came from, connected since, last reconnect
and failed attempts.

**Publish energy values as a Home Assistant device** makes the dashboard
announce itself over MQTT Discovery as the device "Energy Node" with PV, grid,
battery and house load sensors. The computed balance then reaches Home
Assistant without an extra service.

**Use dashboard settings instead of the central configuration** decides which
values win. When it is off, the values from `/etc/energy-node/config.json` are
used unchanged. When it is on, the settings stored here replace them
completely. Fields are not merged.

Further down, the same tab configures the **Mosquitto bridge to the main site**
and can apply it and restart it.

## Tailscale

![Tailscale wizard](../images/dashboard-settings-tailscale.png)

The wizard has three steps: check the prerequisites, start the login, check the
result. It reports whether Tailscale is installed and whether `tailscaled` is
active and enabled. Once connected, it shows the connection state, the number
of peers and the key expiry, with buttons to check again, log out or restart
the service. Without it, putting a headless Pi on a tailnet means an SSH
session and copying a login URL by hand.

## TinyTuya

![TinyTuya wizard](../images/dashboard-settings-tinytuya.png)

This four step wizard (credentials, device, data points, apply) wraps the
`tinytuya` cloud lookup. You enter the Tuya region and API credentials, list
the devices on the account, probe the device's data points locally and write
the result into `tuya_devices.json`. The probe step is there because a switch
is not always data point `1`. Credentials are only stored when you ask for it.

## Versions

![Versions settings](../images/dashboard-settings-versions.png)

This tab shows what is installed on the node. The top card shows the installed
package with its version, build date and architecture, and the version the
dashboard itself runs as. When the dashboard was updated on its own after the
installation, the two differ and the page points that out. Below that every
application, service and library is listed with its version. **Changes** opens
its changelog. A service that is not installed is marked as such. The switch
in the package card chooses between the **Highlights** of each release and
**Everything**, which includes every commit.

The **Updates** card checks GitHub for a newer release when you click
**Check for updates**.

## System

![System settings](../images/dashboard-settings-system.png)

This tab shows who you are logged in as and with which role, and holds the
system actions: restart the dashboard service, restart the system and shut it
down. The screenshot shows an admin session, so the actions are enabled. In a
guest session they are greyed out, because system actions need a registered
user with the `system_actions` role and HTTPS.

Below that is the content of `/etc/energy-node/config.json` as a generated
form, built the same way as the configuration tab. Passwords are never in that
file, only the paths of the files that hold them.

## Updating from the dashboard

![Update notice in the header](../images/dashboard-update-available.png)

When a newer release is out on GitHub, the header shows a notice next to the
title (**Update … available**). The same notice appears under
*Settings → Versions*, next to **Check for updates** and the link
**Release notes on GitHub**. For a registered user with the `system_actions`
role the notice opens the update page (`/redeploy/`). Everyone else gets the
release notes on GitHub in a new tab.

![Update preview](../images/dashboard-update-preview.png)

The update page first downloads the newest signed release package for the
node's architecture. **View changes** lists what the new package changes. The
page shows the same preview as the
[desktop installer](../installer.md#updating-a-node): which components change,
which services restart and which services the node runs. **Update** runs the
pending steps on the node itself, without the installer and without SSH.

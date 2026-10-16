---
title: "Settings: General & appearance"
---

# Settings: General & appearance

The tabs **General** and **Appearance** under *Settings*. The other tabs are
listed on [Settings](settings.md).

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

**Main menu when space is short** decides what the tab bar does when not all tabs fit. *More* keeps one row and moves the rest into a **More** list. *Dropdown* collapses the bar into a menu on screens narrower than 700 px. The choice applies to this browser only and takes effect immediately, without saving. The sub-tabs under Settings always use the **More** list.

The **Formatting** card holds the language of this browser, the switch that
shows or hides the `DE | EN` pill for everyone, and the number format.

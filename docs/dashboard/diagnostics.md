---
title: "Diagnostics"
---

# Diagnostics

![Diagnostics](../images/dashboard-diagnosis.png)

Two blocks:

- **Health score** — one row per device: score, status, the time of its last
  signal, the staleness threshold it is measured against, and the number of
  warnings. A device that has never sent shows as critical. In the screenshot
  every device is "unhealthy" because the fixture broker replays retained
  messages once and then goes quiet — on real hardware the poll intervals keep
  the scores up.
- **MQTT Discovery** — the count of devices, entities, parse errors and
  duplicate `unique_id`s, plus the concrete problems behind them. Duplicate
  unique IDs are the classic reason Home Assistant merges two devices into
  one.

Both can be filtered by severity, rule and device.

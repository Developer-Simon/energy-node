---
title: "Automations"
---

# Automations

![Automation rules](../images/dashboard-automation.png)

This page edits the rules of the separate `automation_mqtt.py` service. A rule
reads from left to right: conditions, then state, then actions. A condition can
be a threshold on a balance field (with hysteresis and hold time), a raw MQTT
topic value, a Home Assistant style entity template or a time window. An action
publishes to a topic or raises a notification.

Besides the built-in balance fields, a threshold can watch any custom category
from the energy page. They are listed under **Custom categories** in the field
selection. A rule whose category was removed shows it as an unknown category
and its condition stays unmet until you pick another field.

The header shows whether the automation service is reachable. In the
screenshot it is offline because the smoke test harness does not start it, so
the rules are drawn from their saved state without live values. The dashboard
never runs rules itself. It only edits the JSON, and the Python service
evaluates the rules and publishes. See
[Device services → Automations](../services/automations.md).

The **Assistant** button walks you through building a rule. **Show history**
lists the last triggers of one rule.

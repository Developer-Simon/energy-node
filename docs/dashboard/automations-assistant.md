---
title: "Automations: Assistant"
---

# Automations: Assistant

## Assistant

![The assistant with its templates](../images/dashboard-automation-assistant.png)

**Assistant** in the bar above the list opens the assistant. It is the only way to add an entry. It builds a scenario or a rule
from a few questions, so you do not need to know the fields.

Below 900 px wide the assistant opens as a bottom sheet that slides up from the
bottom of the screen. Drag its handle down to close it, or tap the dark area
above it, or press Escape. A drag that stops short springs back. When the
assistant closes, the focus returns to the **Assistant** button.

The first page is the gallery. A search field matches the title, the text and
the keywords of each template. Category buttons narrow the list to **Charging**,
**Grid feed**, **Grid**, **Heating**, **Switching**, **Tariff**, **Time** or
**Warnings**. Templates that fit your
devices show as tiles on a desktop and as rows on a phone. Templates whose device or battery is missing are listed
under **More templates** with the reason. Under the tiles, **Without a
template** creates an empty rule or an empty scenario.

A template then asks its steps one at a time. Each step has a device choice,
a set of cards or tiles, a number or a time window. Options that do not apply,
for example the sun option without a known location, are greyed out with the
reason. The dots at the top show your place. **Back** returns to the previous
step or to the gallery, and **Next** is enabled once the step has an answer.

![A step of the assistant](../images/dashboard-automation-assistant-step.png)

Every template is described on [Templates](automations-templates.md).

The last page is the summary. It lists the answers as plain sentences grouped
by phase: **When**, **Running**, **Ends**, **On start**, **On end** and
**Then**. Each row has a **change** button that jumps back to the step that
sets it. **Create** adds the entry.

The new entry lands in its list, expanded and in edit mode, switched on and
marked as new. It is not saved yet. The page counts it as an unsaved change
until you save the document. A bar shows **Scenario added** or **Rule added**
with an **Undo** button for 10 seconds, as in [Undo](automations.md#undo). If the list is
full, the summary shows a note and **Create** is disabled.

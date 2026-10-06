---
title: "Energy"
---

# Energy

![Energy roles and balance interpretation](../images/dashboard-energy.png)

The bridge between "a pile of sensors" and "an energy balance". Each measured
entity is given a **role** — PV, grid, battery, house load, wallbox, heat
pump, battery state of charge, and the mirrored import/export and
charge/discharge variants. Several entities may share a role; they are summed.
Everything else stays unassigned and is listed at the top, because an
unassigned power sensor is the usual cause of a balance that does not add up.

**Balance interpretation** decides how house consumption is obtained:

| Mode | Meaning |
|---|---|
| Measured | Take the entity carrying the house consumption role |
| Calculated | PV + grid import + discharging − feed-in − charging |
| Combined | Calculated, with measured consumers shown separately |
| Automatic (default) | Measured if available, otherwise calculated |

Below that sit the knobs that the status card reads: whether a remaining
balance gap is folded into house consumption, the tolerance threshold in watts
or percent, and the thresholds at which the status card calls a situation a
surplus or a grid draw, plus the battery reserve.

The tiles at the bottom show every assigned role with its current value and
two flags: whether the value is fresh, and whether it is arriving live.

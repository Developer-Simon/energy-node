---
title: "Energy"
---

# Energy

![Energy roles and balance interpretation](../images/dashboard-energy.png)

This page turns a set of sensors into an energy balance. Each measured entity
gets a role: PV, grid, battery, house load, wallbox, heat pump, battery state of
charge, or one of the split import/export and charge/discharge variants.
Several entities can share a role, and their values are summed. Entities
without a role are listed at the top, because an unassigned power sensor is
the usual reason a balance does not add up.

**Balance interpretation** decides where the house consumption comes from:

| Mode | Meaning |
|---|---|
| Measured | Take the entity carrying the house consumption role |
| Calculated | PV + grid import + discharging − feed-in − charging |
| Combined | Calculated, with measured consumers shown separately |
| Automatic (default) | Measured if available, otherwise calculated |

Below that are the settings the status card reads: whether a remaining gap in
the balance is added to the house consumption, the tolerance in watts or
percent, the thresholds at which the status card calls a situation a surplus
or a grid draw, and the battery reserve.

## Plant live

At the top of the tab, Plant live shows the plant as a read-only view. It uses
the arrangement of the device map, so the devices sit where you placed them
there. The energy flow animates along the connections in the direction of the
power, and the energy balance node carries its role edges. Rules and services
are not drawn here, and nothing can be dragged or edited in this view.

Clicking a device lights up its rows in the role table below and keeps the
node highlighted. Clicking a group lights up its group card. The other way
round, clicking a role row highlights its device in the plant view. Roles are
still changed in the table, and the ring on the plant follows once they are
saved.

With reduced motion enabled in the system, the flows stand still and carry an
arrow, as on the device map. Leaving the Energy tab stops the animation.

## Groups

A group bundles devices without a meter of their own. It lists its members,
which can be devices or other groups, and can carry a role. Without a role
every member counts with its own role. With a role the group counts as a whole
under that category, and its members must not carry a role that conflicts with
it. A device sits in at most one group, and groups cannot contain themselves.
Groups can also be created and edited on the device map.

## Custom categories

A custom category has a name, a base type, a colour and an icon. The base type
sets the balance rule: consumers are subtracted like house load, producers add
like PV and storage behaves like a battery. Each consumer category appears as
its own sink next to the house consumption, which is reduced by the same
amount, so the balance still adds up. A category that is still assigned to a
device or group cannot be removed.

The tiles at the bottom show every assigned role with its current value and
two flags: whether the value is fresh and whether it arrives live.

Changing roles, the interpretation or restoring an older revision needs the
permission `edit_energy`. Until there is a user management, every account has
it, guests included. The same permission covers the role panel on the device
map. The roles saved there show up on this page right away.

---
title: "Energy"
---

# Energy

The Energy tab turns your sensors into an energy balance. It sets the energy
roles of every power and charge entity, the groups and the custom categories,
and it shows how the house consumption is derived. To see where that energy
flows, open the [device map](device-map.md). Roles, groups and categories are
shared between both tabs, so a change on one shows up on the other right away.

## Plant live

![Plant live on the Energy tab](../images/dashboard-energy-plant.png)

The device map arrangement, read only, with the live flow and the energy
balance. Click a device to light up its rows in the role table, click a group
to jump to its card.

## Balance interpretation

![Balance interpretation](../images/dashboard-energy-interpretation.png)

| Mode | House consumption is |
|---|---|
| Automatic (default) | PV + grid import + discharging − feed-in − charging. A house meter is a cross-check and is used when no grid role is assigned |
| Calculated | always the calculation, a house meter is only compared |
| Measured | the house meter |

In every mode, wallbox, heat pump and custom consumer categories are
subtracted from house consumption. What is left is shown as remaining
consumption. A line below the modes tells where house consumption currently
comes from and whether the house meter matches the calculation.

The role House meter always means the whole house. Metered sub-consumers belong
in a custom consumer category. Setups that used the former Combined mode were
converted automatically: every entity with the old role became its own
consumer category named after the entity. Rename them under Custom categories.
An entity that only got the role by name detection (not saved) is treated as
house meter afterwards. If the status line reports a deviation, check it.

Below that: whether a remaining gap counts as house consumption, the tolerance,
the surplus and grid import thresholds of the status card and the battery
reserve.

## Energy roles

![Energy roles](../images/dashboard-energy-roles.png)

Every power and charge entity gets a role such as PV, grid, battery, house
meter, wallbox, heat pump or a custom category. Entities sharing a role
are summed. The tiles show each role with its value and whether it is fresh and
live. Power values without a role are listed at the top of the tab, because
they are the usual reason a balance does not add up.

## Groups

![Groups with the count as a whole switch](../images/dashboard-energy-groups.png)

A group bundles devices without a meter of their own.

- **Count as a whole off** (Garage): every member counts with its own role.
- **Count as a whole on** (Workshop): the group counts in one consumer
  category. Pick it from the chips or create a new one in place.

A device sits in at most one group. Groups can hold other groups.

## Custom categories

![Custom categories](../images/dashboard-energy-categories.png)

Your own consumer, producer or storage types next to the built-in roles. The
base decides how the category counts: consumers like house load, producers
like PV, storage like a battery. Each consumer category appears as a sink of
its own in the overview tiles and in the history. A category that roles or
groups still use cannot be removed. Automation rules do not block removal, but
the dashboard lists the rules that still read the category before you confirm.

## Permissions

Changing roles, groups, categories or the interpretation needs the permission
`edit_energy`. Until there is a user management every account has it, guests
included.

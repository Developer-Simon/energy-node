---
title: "Energy and device map"
---

# Energy and device map

The Energy tab turns your sensors into an energy balance. The Device map tab
shows where that energy flows. Both work on the same roles, groups and
categories, and a change on one tab shows up on the other right away.

## Energy tab

### Plant live

![Plant live on the Energy tab](../images/dashboard-energy-plant.png)

The device map arrangement, read only, with the live flow and the energy
balance. Click a device to light up its rows in the role table, click a group
to jump to its card.

### Balance interpretation

![Balance interpretation](../images/dashboard-energy-interpretation.png)

| Mode | House consumption is |
|---|---|
| Measured | the entity with the role House consumption |
| Calculated | PV + grid import + discharging − feed-in − charging |
| Combined | calculated, measured consumers shown separately |
| Automatic (default) | measured if available, otherwise calculated |

Below that: whether a remaining gap counts as house consumption, the tolerance,
the surplus and grid import thresholds of the status card and the battery
reserve.

### Energy roles

![Energy roles](../images/dashboard-energy-roles.png)

Every power and charge entity gets a role such as PV, grid, battery, house
consumption, wallbox, heat pump or a custom category. Entities sharing a role
are summed. The tiles show each role with its value and whether it is fresh and
live. Power values without a role are listed at the top of the tab, because
they are the usual reason a balance does not add up.

### Groups

![Groups with the count as a whole switch](../images/dashboard-energy-groups.png)

A group bundles devices without a meter of their own.

- **Count as a whole off** (Garage): every member counts with its own role.
- **Count as a whole on** (Workshop): the group counts in one consumer
  category. Pick it from the chips or create a new one in place.

A device sits in at most one group. Groups can hold other groups.

### Custom categories

![Custom categories](../images/dashboard-energy-categories.png)

Your own consumer, producer or storage types next to the built-in roles. The
base decides how the category counts: consumers like house load, producers
like PV, storage like a battery. Each consumer category appears as a sink of
its own in the overview tiles and in the history. A category that roles or
groups still use cannot be removed. Automation rules do not block removal, but
the dashboard lists the rules that still read the category before you confirm.

## Device map

![Device map](../images/dashboard-device-map.png)

A free canvas with all devices. Place the nodes by hand and connect them to
record how the site is wired, such as which meter feeds which sub-distribution.
Positions, connections and layers are versioned, so an accidental drag can be
rolled back.

How to read a node:

- **Ring colour**: the energy roles of the device, one segment per role.
- **Solid ring**: role assigned by you. **Dashed**: only detected. **Grey
  dotted**: no role.
- **Dot top right**: availability, green, amber, red or grey without data.
- **Σ in a dashed circle**: a group, its value is the sum of its members.
- **Moving dashes**: the live flow, faster for more power, with the value at
  the child end.

### Layers and view

![Layer bar and view menu](../images/dashboard-device-map-view-menu.png)

| Layer | Shows |
|---|---|
| Wiring | the connections between devices and groups |
| Live energy | roles, values and the moving flow |
| Data flow | topics between devices, services, automation rules and the balance conditions of automations |
| Roles | every device with an energy role, connected to the Energy Node |

The view menu holds the grid, snap to grid, width by power, the line style and
the legend. With reduced motion enabled in the system the lines stand still and
carry an arrow.

### Connection types

Click a connection to set what it stands for:

| Type | Meaning | Flow |
|---|---|---|
| Energy | an electrical line, such as inverter to battery | yes |
| Data | communication through a hub, gateway or service | no |
| Informational | a plain assignment, such as an accessory | no |

Until you choose, the map decides by itself: a connection between two devices
that both measure power counts as energy, every other one as data. Such a
detected type is marked in the card.

For an energy connection the flow direction is automatic by default, taken
from the sign of the measurement. Reversed turns it round, and Always to fixes
it to one end. A connection you drew yourself can be reversed in the same card.

### Device panel

![Device panel with the energy roles](../images/dashboard-device-map-device-panel.png)

Click a device to see the roles of its measurements. A chip tells where each
role comes from: detected, assigned or no role. Changes are drafts until you
save, and the map already follows them. Show in the role table jumps to the
Energy tab.

### Group panel

![Group panel](../images/dashboard-device-map-group-panel.png)

Click a group for its sum, its members and the count as a whole switch. Members
are added with the picker or by hanging a device under the group in connect
mode.

![Device picker](../images/dashboard-device-map-picker.png)

![Create group dialog](../images/dashboard-device-map-create-group.png)

![Create category dialog](../images/dashboard-device-map-category.png)

The plus in the toolbar creates a group. New category in a group creates a
category and selects it for that group.

### Data flow layer

![Data flow layer](../images/dashboard-device-map-dataflow.png)

Topics as dashed edges from the producer to the receiver. Automation rules are
diamonds, services are squares, and a topic nobody publishes is a stub labelled
Topic without a device. Click an edge or a rule for its details and a link into
the configuration or the Automations tab. How a service declares its topics is
described in [Data flows](../developing/data-flow.md#12-data-flow-on-the-device-map).
The automations device sits under the Energy Node and connects to every rule it runs.
A rule with an energy value condition gets an edge from the balance, so the
value a rule compares against is visible here without the Roles layer.

### Roles layer

![Energy balance layer](../images/dashboard-device-map-balance.png)

The Roles layer connects every device with an energy role to the Energy Node, in
the colour of its role. The Energy Node is the balance itself, the device the
dashboard publishes for Home Assistant, and it shows the net grid power. Without
the Energy Node device, a separate balance node stands in for it.

## Permissions

Changing roles, groups, categories or the interpretation needs the permission
`edit_energy`. Until there is a user management every account has it, guests
included.

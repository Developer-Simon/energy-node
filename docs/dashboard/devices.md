---
title: "Devices and device map"
---

# Devices and device map

This page lists everything the node has seen through MQTT Discovery, grouped by
device. The button in the top right switches between two views.

The compact view shows the name, the availability and a few headline values:

![Device list, compact mode](../images/dashboard-devices.png)

The detailed view shows every entity of every device with its component type
and its controls: sliders for `number` entities, toggles for `switch` and live
values for `sensor`:

![Device list, detailed mode](../images/dashboard-devices-detailed.png)

Availability comes from each device's `availability_topic`. `Shelly Uni Bank B`
above is the fixture's device that is offline on purpose. `Details` on a device
opens a dialog with the raw discovery payload, the topics involved and whether
the discovery message was retained. That is usually the quickest way to find
out why Home Assistant does not see a device.

The default view is a setting, so a tablet on the wall can open straight into
the compact view.

## Device map

![Device map](../images/dashboard-device-map.png)

The device map is a free canvas with all devices on it. You place the nodes by
hand, they are coloured by health, and you can connect them with straight,
orthogonal or curved edges to record how the site is wired. That shows what the
automatic views cannot, such as which meter sits ahead of which sub-distribution
or what a device is physically attached to.

Each node is a ring around the device icon. The colour of the ring segments
shows the energy roles the device carries, such as PV, battery, grid, load,
wallbox or heat pump, and a device with several roles gets one segment per
role. A dashed ring means the role was only guessed from the entities, a solid
ring means you assigned it on the Energy tab. A device without any role shows
a grey dotted ring. The small dot at the top right is the availability, green
for reachable, amber for partly reachable, red for unreachable and grey when
the device publishes no availability. Under the name the node shows the live
value, for example the PV power, the grid import or export, or the battery
power with its direction.

The layer bar above the map switches what the canvas draws. Wiring shows the
connections between devices, Live energy shows the roles and values. With only
Live energy on, the connections stay as a dotted track. Energy balance and
Data flow are listed but switched off, they follow in a later version. The
layer choice is saved with the map. Click a node to focus it, the node and its
direct neighbours stay bright while the rest fades out. A click on the
background, the button in the hint or Esc ends the focus. If the energy values
cannot be loaded, the map still opens with devices, connections and
availability and shows a short note.

With Live energy on, the connections carry the live power flow. Moving dashes
run in the direction of the flow, faster for more power, and a label at the
child end shows the value and the direction. An arrow down means power flows
toward the child device, an arrow up means it flows toward the parent, so a
discharging battery or a PV system flips the flow automatically. A connection
shows the measurement of the child device if it has one. Otherwise it shows the
signed sum of everything below it, marked with a Σ, and a connection without
any data shows no flow. The line colour follows the role of the child, the
sums are grey. The switch Width by power makes stronger flows thicker. With
reduced motion enabled in the system, the lines stand still and carry an arrow
instead. The animation stops while the browser tab is hidden or another
dashboard tab is open, and a legend in the corner explains the colours.

Clicking a device also opens a side panel with the energy roles of its
measurements. Every power and charge entity shows its role, with a chip that
tells where the role comes from. "Detected" means it was guessed from the name
and the unit, "assigned" means it is stored as your own assignment, and "no
role" means nothing was found. Changing a role is a draft first. The chip turns
to "not saved" and the ring, the values and the flows on the map already
follow the draft, so you see the effect before you commit. Keep next to a
detected role turns the guess into a fixed assignment. Scale and sign sit in a
fold-out for entities that report in other units or with the opposite sign.
Save roles writes all drafts at once. Closing the panel, tapping another
device, switching to connect mode or pressing Esc with unsaved drafts asks
whether to discard them. Show in the role table jumps to the Energy tab and
highlights the rows of this device. The panel does not open for the Energy Node
device itself, because its values come from the balance.

### Energy groups

A group bundles devices that share a feed but have no meter of their own, such
as the charger and the workshop socket behind one sub-distribution. Create
Group in the toolbar asks for a name and puts a dashed circle with a Σ on the
map. New groups and new devices are placed below the existing arrangement, and
a short note says how many nodes were placed.

The group shows the signed sum of its members as its value. A group has no
measurement of its own, so the sum is calculated from the members. To add a
device, hang it under the group in connect mode or pick it in the group panel.
Releasing the membership edge or the remove button in the panel takes a device
out again. A device belongs to one group at a time. Hanging it under another
group asks before it moves. The feed of the group is an ordinary connection
from the meter or distribution above it to the group, drawn like any other
edge.

The group panel opens with a click on the group. It holds the name, the
members, a picker for further devices and the group role. Without its own role
every member counts with its own role in the balance. With a role the group
counts as a whole under that category. Deleting a group removes its feed and
leaves the members and their roles untouched.

### Custom categories

Custom categories add your own consumer, producer or storage types to the
built-in roles, for example Workshop or Heat pump circuit. A category has a
name, a base type, one of six colours and an icon. The base type decides how
the category counts in the balance, so a consumer category is subtracted like
house load and a producer category adds like PV. Create Category in the
device panel or on the Energy tab adds one, and it is available as a role right
away. A category in use cannot be removed, the error names the devices and
groups that still use it.

On the map a device with a custom role shows a ring in the category colour and
the category icon. In the overview energy tiles every consumer category is a
sink of its own, and the history records its value like any other role.

Positions, edges and the layer choice are stored on the server and versioned
like the layout, so you can roll back an accidental drag.

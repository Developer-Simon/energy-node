---
title: "Efficiency"
---

# Efficiency

The **Efficiency** tab shows how much energy each measurement chain loses. Pick
the chain in the selector at the top. The tab then shows four figures, the loss
per day for the last seven days, the efficiency curve of the chain and a
diagnosis of its segments.

The tab is hidden until at least one [measurement chain](energy.md#measurement-chains)
exists. It also needs the permission `view_efficiency`, see
[Settings, Users](settings-users.md#permissions), and the manager rights that
the Energy and Device map tabs need.

## The figures

- **Efficiency now**: the product of the efficiencies of all segments of the
  chain, from the current values. It shows a dash when a segment is not
  available, has no efficiency, or the chain has no segments.
- **Round trip 30 days**: the discharged energy divided by the charged energy,
  both measured at the counting point, over the last 30 days. It only applies
  to a chain whose counting point has the storage role, for example a battery.
  Charging follows the order of the points, and a chain with the direction
  reversed flips it. The value needs at least 0.1 kWh of charge in the period.
- **Standby**: the mean standby power of the chain in W over the last 30 days.
  See [Standby](#standby) below.
- **Loss** for the current month: the energy the chain loses in the local
  calendar month, in kWh.

A figure stays empty, not zero, while the chain has no recorded hour.

## Efficiency curve

The curve shows the efficiency of the chain at each power of the counting point.
A point stands for a class of 50 W and sits in the middle of it. The efficiency
of a class is the product of the learned efficiencies of all segments in that
direction, see [Learning](#learning). A class is shown once every segment has at
least 30 samples in it.

For a storage chain the curve shows charging as a solid line and discharging as
a dashed line. Any other chain shows each direction it can flow in, along the
points and against them. The shaded area marks the best range: the most efficient
class and the neighbouring classes that stay within one percentage point of it.
It needs at least three classes in the direction.

## Learning

Each segment learns its normal loss from the readings taken every 10 seconds
while the chain flows. The samples are sorted by the load class of the counting
point, so every class learns its own loss. Each direction of the flow learns
separately. A sample counts only when the counting point is current and no
substitute value is in use, the source of the segment reads at least 10 W, and
no unit, frozen, contradiction or drift finding is active on the segment.

A class is learned after 1080 samples, about 3 hours, or after 60 samples, about
10 minutes, once the segment has been known for 7 days. The loss of a class is a
moving mean over the latest 4320 samples, about 12 hours. As long as a segment
of the chain has no learned class at all, the chain shows **Still learning**,
unless a finding is shown first.

## Normal range

The normal range of a segment is centred on its learned loss in W, the source
minus the target power, for the current load class. Its half width is the
largest of these values:

- three times the learned mean deviation,
- 5 W,
- 3 % of the source power.

### Adjusting the range

**Adjust** sets a fixed tolerance instead of the learned range. Enter a width in
W, a width in % of the source power, or both. The wider one applies, and the
range is still centred on the learned loss. Empty fields return the segment to
the learned range. Tolerances are saved with the chain on the Energy tab and need
the permission `edit_energy`.

## Diagnosis

The diagnosis card lists the normal range of every segment, each with the button
to adjust it. Below that are the findings of the chain, each with its text, the
time since it is active and its suggestion if there is one. Without a finding the
card says **No findings**.

The model knows five kinds of finding. The hold time is how long the condition
must last before the finding shows. The clear time is how long a shown finding
stays after the condition has stopped.

| Finding | Condition | Hold | Clear |
|---|---|---|---|
| Unit | Two neighbouring points, both current, differ by a factor of 1000 within 25 % of the larger one, and the larger one reads at least 50 W | 15 min | 5 min |
| Frozen | A current point reads at least 10 W and does not change, while another current point moves by at least 20 W in the same time | 15 min | ends at the first change |
| Contradiction | One point reads under 1 W while the other reads at least 50 W, or at least 20 W with a normal range and a loss outside it. A point reading at least 20 W is also a contradiction when the only switch of its device is off | 10 min | 5 min |
| Sign | The meter counts against the flow: the pair is at least 24 hours old, has at least 360 samples with opposite signs and at most 5 % samples with equal signs | none | 5 min |
| Drift | The loss lies outside the normal range | 30 min | 5 min |

Drift does not show while a unit, frozen or contradiction finding is active on
the same segment.

A unit finding suggests scaling the smaller point by 1000, or the larger point by
0.001 from 50 kW on. A sign finding suggests inverting the point of the segment
that lies farther from the counting point. **Apply suggestion** fills the change
into the Energy tab as a draft. Nothing is saved until you save there, and the
button needs the permission `edit_energy`.

## Relearn

**Relearn** forgets what was learned about one segment, in both directions, and
the findings on it. It asks for confirmation first. The segment learns again from
the next readings, and the chain shows **Still learning** until a class is
learned. Tolerances stay. It needs the permission `edit_energy`.

Use it after a change the node cannot see, such as a replaced meter or a rewired
cable, or when the learned range came from faulty readings. A changed scale of
one of the points forgets the segment by itself, and a changed invert only
restarts the sign count, so these need no relearn.

## How the loss is counted

For each hour, every segment of the chain adds up the energy at its source and
at its target. The loss is the source energy minus the target energy. Both
directions of a segment count, so energy that flows back also adds to the loss.
The standby energy of the chain counts as loss as well.

The efficiency of a period is the product of the segment efficiencies, each one
the target energy divided by the source energy. A segment whose source energy
in the period is below 0.01 kWh has no efficiency, and then the efficiency of
the period is empty.

The bar of a day shows its loss relative to the largest loss of the seven days.
The days follow the time zone of the node.

## Standby

A chain is in standby when its first point reads at least 1 W, every other
point reads below 10 W, and all points report fresh values. The standby power is
the power of the first point in that state. While a chain is in standby, its
segments record no energy flow.

## Recording

The node records the chains by itself, in the same way as the energy hours. It
looks at the chains every 10 seconds. A reading counts until the next one, for
at most 5 minutes. Recording follows the [Own recording](statistics.md#own-recording)
setting. Pausing while the companion delivers keeps recording the chains.

Recording begins with the first reading after a chain is created. The segments
of a chain change when a measuring point is inserted or removed. The segment
that disappears keeps the hours it already has, and the new segments start with
the next reading that contains them.

Hours are stored per month in `energystats/chains` in the data directory. The
file format is described in [Data flows: Energy and automations](../developing/data-flow-energy.md#15-measurement-chain-losses).

## Use in automations

The chains also feed the automations. A rule or a scenario can start on a finding
of a chain, or on its loss, efficiency or standby value, see
[Measurement chain conditions](../services/automations.md#measurement-chain-conditions).
A regulator scenario can keep its setpoint in the best efficiency range of a
chain. Switch on **Regulate in the best efficiency range** in the scenario
editor and pick the chain. The line below the switch shows the learned range in
W, or why the regulator works without a limit. The assistant also offers two
templates for chains, see [Chain templates](../services/automations.md#chain-templates).

## Home Assistant

With **Measurement chain sensors** on in the [MQTT settings](settings-mqtt.md),
Home Assistant gets three sensors per chain: the efficiency in percent, the loss
in W and the loss energy in kWh. The loss energy is a total that the node adds up
itself from the live loss. It starts at 0 when the sensor is first offered and is
not filled in afterwards. Standby is not part of it, although the figures above
count it as loss. A deleted chain takes its counter with it.

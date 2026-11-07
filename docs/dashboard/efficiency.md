---
title: "Efficiency"
---

# Efficiency

The **Efficiency** tab shows how much energy each measurement chain loses. Pick
the chain in the selector at the top. The tab then shows four figures and the
loss per day for the last seven days.

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

---
title: "Monitoring"
---

# Monitoring

The **Monitoring** tab collects what the node wants you to look at. It shows
notices about the energy balance, the measurement chains and the learning
functions, it shows the forecast the node uses for the surplus, the comparison
with the previous year, and the values the system has learned. The tab sits
before [Diagnostics](diagnostics.md). It needs the permission
`view_monitoring`, see [Settings, Users](settings-users.md#permissions). While a
notice is new, the tab shows a dot.

Diagnostics stays a separate tab. It covers devices, services and the broker.
Monitoring covers the energy side and what the system has learned.

## Notices

The top left of the tab switches between **Notices**, **Learned values** and
**Forecast**. The filter on the right limits the notices to **All**, **Energy**,
**Learning** or **Suggestions**.

The list has two groups, **New** and **Acknowledged**. Select a notice to see its
card. The card has the text, the facts of the notice, the evidence if there is
one, the recommended steps and the footer with the acknowledgement. On a narrow
screen the card opens below the notice, and a second tap closes it again.

Notices come from three sources:

| Source | Notices |
|---|---|
| Energy balance | The house load is set to measured, but no meter has the role house meter. A battery level exists without a capacity in kWh. Power values have no role. Values were left without a role by a measurement chain. |
| Measurement chains | A finding of a [measurement chain](energy.md#measurement-chains), such as a unit error, a frozen value, a contradiction or a sign error. The card shows the time since the finding started. |
| Learning | A finding that belongs to no rule and to no suggestion, for example a fixed shade that reduces the PV power, a battery that is full early or empty before morning, or a rising base load without a switchable device. The card shows the direction and height of the shade, the calendar weeks, the loss per year and a daily curve. |
| Suggestions | Every suggestion of the [suggestion band](automations-rules.md#suggestions). **Open suggestion** opens it on the Automations page. The chip **Suggestions** lists only these, **All** includes them. |

Findings that belong to a rule stay on the rule card in
[Automations](automations-rules.md). Findings with a suggestion to apply stay in
the suggestion band of [Automations](automations.md).

## Acknowledging

**Acknowledge** moves a notice to the group **Acknowledged**. Its dot goes away,
and the tab only counts the new notices. The notice stays visible while its
condition lasts. The footer then shows when and by whom it was acknowledged. When
the condition ends, the notice disappears. If it comes back later, it is new
again.

The acknowledgement belongs to the node, not to a user. Acknowledging needs the
permission `edit_setup`. Without it the card says that only users with the setup
right can acknowledge. **Mark as new again** takes the acknowledgement back.

## Recommended steps

Each notice lists its steps. A step explains what to do and, where it makes sense,
a button leads to the place for it:

- **Energy** opens the Energy tab, with the rows of the role table marked where
  that helps.
- **Measurement chain** opens the dialog of the chain on the Energy tab.
- **Efficiency** and **Device map** open their tabs.
- **Learned values** switches the tab to the learned values.

A step that leads to a place you may not see is hidden, and its text stays. A
shade notice names the direction and the height. It also says that nothing needs
to be done when the shade cannot be removed, because the remaining surplus and
the surplus window already leave it out.

## Forecast

The **Forecast** view shows how well the node estimates the surplus. At 10:00
each day the node estimates the remaining surplus of the day, the value
`site/surplus_left_wh`. The status card names the rules that use this value. If
no rule uses it yet, the card says so.

The status card tells which estimate the node currently follows. It shows one of
three states:

- **The node follows its forecast**
- **The node currently follows the season median**
- **The node is still checking its forecast**

Below the title, the card shows the mean deviation of the forecast and of the
season median from the measured rest of the day as two bars. It also says on how
many of the last 14 days the forecast was closer.

Below the status card, the last 14 days appear as pairs. The forecast is drawn
hollow, the measured value filled, and the season median as a dashed mark. A dot
marks each day on which the forecast was closer. Arrow keys choose a day.

After 14 finished days the node compares the forecast with the season median,
until then the forecast applies. If the median has been at least as accurate as
the forecast over the last 14 days, the median is published instead, marked as an
estimate.

## Year comparison

The **Year comparison** sits under the Forecast view. It shows weekly values from
the season memory: PV, consumption, feed-in, base load and outdoor temperature.
Choose the value with the selector **Figure**.

The current year is drawn as an area, the previous year dashed, and a third year
dotted. Weeks taken from the energy statistics are hatched. A gap appears as a
band, marked as no data.

The headline figure compares the weeks up to now with the same weeks of the
previous year. Only weeks that are present in both years count. If weeks are
missing in one of the years, the headline says how many were left out. Moving the
pointer or finger over the chart shows one week with all years. Arrow keys choose
the week as well.

The view needs the permission `view_monitoring`. A young node shows a note until
a previous year exists, because the season memory fills up week by week.

## Learned values

The **Learned values** view shows what the system has learned. It opens with **A
usual day**, a picture of one day on a 24 hour axis. A label at the top says
whether the house is usually active or quiet.

- The house row shows the night rest as a band.
- Each device with a usual on and off time shows a bar from on to off. A bar with
  fewer than 10 learned days is hatched and marked as still learning.
- A device that only has a run length shows a dashed row, for example
  "40 min at a time, no fixed time".
- A dashed line marks the current time.
- Moving the pointer or finger over the axis shows which devices are usually on at
  that time. The arrow keys move the time by 15 minutes.
- Tapping a row opens the course over the year, the band between on and off per
  week. It also shows the origin: learned from N days or runs, the certainty, when
  the value last changed and the value before.

Two more cards follow the picture:

- **One run** shows the energy per run on one scale. Detected programs appear as
  marks, and the power and the run time are shown below.
- **System** shows the PV reserve and the remaining surplus of today, each once
  it is learned. A link leads to the Forecast view.

The view shows these values:

| Name | Meaning | Unit |
|---|---|---|
| Start of day | The learned start of the day, the point at which the usual day begins. | time of day |
| Night rest | The night rest of the house, shown as a band in the house row. | time span |
| House state | Whether the house is usually active or quiet. | active or quiet |
| PV reserve | The PV power still available. It is marked as an estimate until it comes from the minute values. | W |
| Remaining surplus today | The surplus the rest of the day is expected to bring, see the Forecast view. | kWh |
| Usual switch-on time | The time at which the device is usually switched on. | time of day |
| Usual switch-off time | The time at which the device is usually switched off. | time of day |
| Usual runtime | How long the device usually runs at a time. | min |
| Typical operating power | The power the device usually draws while it runs. | W |
| Typical cycle energy | The energy the device usually takes per run, see One run. | kWh |

If pattern learning is off, the view only shows a note that you can turn it on
under *Settings, Learning*.

## Energy line

The Energy tab no longer shows the notices itself. Instead, the top of the tab
has a summary line with a bell. It says how many notices about energy are open,
how many of them are new, and it offers **Open in monitoring**, which opens the
tab with the filter *Energy*. Without open notices the line says that there are no
open notices about energy. The line is hidden without the permission
`view_monitoring`.

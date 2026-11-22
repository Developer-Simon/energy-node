---
title: "Monitoring"
---

# Monitoring

The **Monitoring** tab collects what the node wants you to look at. It shows
notices about the energy balance, the measurement chains and the learning
functions, and it lists every value the system has learned. The tab sits before
[Diagnostics](diagnostics.md). It needs the permission `view_monitoring`, see
[Settings, Users](settings-users.md#permissions). While a notice is new, the tab
shows a dot.

Diagnostics stays a separate tab. It covers devices, services and the broker.
Monitoring covers the energy side and what the system has learned.

## Notices

The top left of the tab switches between **Notices** and **Learned values**. The
filter on the right limits the notices to **All**, **Energy**, **Learning** or **Suggestions**.

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

## Learned values

The second view lists all values the system has learned. Each value shows its
origin, for example from 14 days, from 3 runs, changed 2 days ago or before a
previous value, and whether it is an estimate. When the seasonal memory covers
at least four weeks, a yearly line shows the curve.

The first group, **System**, always lists the five system values: start of day,
night rest, house state, PV reserve and remaining surplus today. A value that is
not learned yet shows its reason, such as needing PV minutes by day or more days.
Below that, each device has its own group with the usual switch-on time, the
usual switch-off time, the usual runtime, the typical operating power and the
typical cycle energy.

If pattern learning is off, a hint above the list links to the learning settings
under *Settings, Learning*.

## Energy line

The Energy tab no longer shows the notices itself. Instead, the top of the tab
has a summary line with a bell. It says how many notices about energy are open,
how many of them are new, and it offers **Open in monitoring**, which opens the
tab with the filter *Energy*. Without open notices the line says that there are no
open notices about energy. The line is hidden without the permission
`view_monitoring`.

---
title: "History"
---

# History

![History charts](../images/dashboard-history.png)

The node stores no history. The browser records it: a recorder samples
`/api/v1/energy` at an interval and writes to IndexedDB, so the Pi's SD card
and RAM are not involved at all.

You can choose the time range (1 h, 6 h, day, week, month or a custom range)
and the metric per bucket (average, minimum or maximum). The series are every
energy role plus computed ones such as *House consumption (calculated)*. Click
a chip to show or hide a series. Under **Advanced: views & export** you can save
a set of series as a named view and export the visible data as CSV or JSON.

Raw samples are compacted into one minute and five minute tiers as they age.
When two browsers have the dashboard open at the same time, each fetches the
tiers it is missing from the other through the node. The server runs an SSE
hub for this and stores no data. A browser's own measurements are never
overwritten.

The line above the chart names the tier, the number of points and series, and
whether the browser has granted persistent storage.

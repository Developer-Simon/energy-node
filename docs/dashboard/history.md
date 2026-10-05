---
title: "History"
---

# History

![History charts](../images/dashboard-history.png)

The node stores **no** history. The browser records it: a recorder samples
`/api/v1/energy` on an interval and writes to IndexedDB, which is what keeps
the Pi's SD card and RAM out of the loop entirely.

- **Time range** — 1 h, 6 h, day, week, month, or a custom range.
- **Metric** — average, minimum or maximum per bucket.
- **Series** — every energy role plus computed series such as
  *House consumption (calculated)*. Click a chip to show or hide it.
- **Advanced: views & export** — save a set of series as a named view, and
  export the visible data as CSV or JSON.

Raw samples are compacted into one-minute and five-minute tiers as they age.
Two browsers that have the dashboard open at the same time will **exchange the
tiers each is missing** over the node — an SSE hub on the server, no data
stored server-side, and never overwriting a browser's own measurements.

The line above the chart names the tier, the number of points, the number of
series, and whether the browser has promised the storage as persistent.

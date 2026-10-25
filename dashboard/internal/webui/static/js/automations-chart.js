(() => {
  // Reine Geometrie fuer die Szenario-Karten und die Regel-Zeitleisten.
  // Keine DOM-Zugriffe: die Aufrufer setzen die Werte in SVG um. Zeiten sind
  // Sekunden seit Epoche, ebenso wie Ereignisse (at) und Punkte (ts).
  const CHART_RANGES = ['30m', '6h', 'today'];
  const RANGE_SECONDS = { '30m': 30 * 60, '6h': 6 * 3600 };
  const GAP_SECONDS = 180;
  const AXIS_MIN_W = 100;
  const AXIS_PAD = 0.1;
  const TICK_COUNT = 4;
  const SPARK_PAD = 0.1;

  // Toene fuer Szenario-Ergebnisse. Die Regel-Ergebnisse fired, blocked und
  // error kommen aus historyResultInfo in automations-view.js.
  const SCENARIO_RESULT_TONE = {
    started: 'ok',
    recovered: 'ok',
    ended: 'off',
    fallback: 'bad',
    jump: 'info',
  };

  function rangeBounds(range, nowMs) {
    const nowSec = nowMs / 1000;
    if (range === 'today') {
      const midnight = new Date(nowMs);
      midnight.setHours(0, 0, 0, 0);
      return { from: midnight.getTime() / 1000, to: nowSec };
    }
    const span = RANGE_SECONDS[range] || RANGE_SECONDS['30m'];
    return { from: nowSec - span, to: nowSec };
  }

  function resultInfo(result) {
    if (Object.prototype.hasOwnProperty.call(SCENARIO_RESULT_TONE, result)) {
      return { tone: SCENARIO_RESULT_TONE[result], label: result };
    }
    return window.__automationsView.historyResultInfo(result);
  }

  // Die Achse umfasst mindestens -100..100 W und jeden Wert samt Toleranzband,
  // dazu 10 % Rand.
  function axisRange(values) {
    let lo = Math.min(-AXIS_MIN_W, ...values);
    let hi = Math.max(AXIS_MIN_W, ...values);
    const pad = (hi - lo) * AXIS_PAD;
    lo -= pad;
    hi += pad;
    return { lo, hi };
  }

  // Schritt 1, 2, 5 oder 10 mal eine Zehnerpotenz, etwa TICK_COUNT Striche.
  function tickStep(span) {
    const raw = span / TICK_COUNT;
    const magnitude = 10 ** Math.floor(Math.log10(raw));
    const normalized = raw / magnitude;
    const factor = normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10;
    return factor * magnitude;
  }

  // Ein Abstand ueber GAP_SECONDS setzt die Linie neu an (M statt L).
  function gapPath(points, xOf, yOf) {
    let previous = null;
    return points.map((point) => {
      const command = previous === null || point.ts - previous > GAP_SECONDS ? 'M' : 'L';
      previous = point.ts;
      return `${command}${xOf(point.ts).toFixed(1)} ${yOf(point.value).toFixed(1)}`;
    }).join(' ');
  }

  function scenarioChart({ grid, output, target, deadband, events, from, to, width, height }) {
    const span = to - from || 1;
    const inRange = (point) => point.ts >= from && point.ts <= to;
    const gridPoints = grid.filter(inRange);
    const outputPoints = output.filter(inRange);
    const empty = gridPoints.length === 0 && outputPoints.length === 0;

    const values = [...gridPoints, ...outputPoints].map((point) => point.value);
    values.push(target - deadband, target + deadband);
    const { lo, hi } = axisRange(values);

    const xOf = (ts) => ((ts - from) / span) * width;
    const yOf = (value) => height - ((value - lo) / (hi - lo)) * height;

    const linePath = (points) => gapPath(points, xOf, yOf);

    const bandTop = yOf(target + deadband);
    const bandBottom = yOf(target - deadband);

    const step = tickStep(hi - lo);
    const yTicks = [];
    for (let k = Math.ceil(lo / step); k * step <= hi; k += 1) {
      const value = Math.round(k * step * 1000) / 1000;
      yTicks.push({ y: yOf(value), label: String(value) });
    }

    const markers = events.filter((event) => event.at >= from && event.at <= to).map((event) => ({
      x: xOf(event.at),
      kind: event.result,
      label: resultInfo(event.result).label,
    }));

    return {
      gridPath: linePath(gridPoints),
      outputPath: linePath(outputPoints),
      band: { y: bandTop, height: bandBottom - bandTop },
      markers,
      yTicks,
      empty,
    };
  }

  // Minidiagramm der zugeklappten Regler-Zeile: Netzlinie und Toleranzband.
  // Die Y-Achse umfasst Werte und Band mit 10 % Rand, ohne die feste 100-W-Achse.
  function sparkline({ points, target, deadband, from, to, width = 150, height = 34 }) {
    const span = to - from || 1;
    const inRange = points.filter((point) => point.ts >= from && point.ts <= to);
    const values = [...inRange.map((point) => point.value), target - deadband, target + deadband];
    const low = Math.min(...values);
    const high = Math.max(...values);
    const pad = (high - low) * SPARK_PAD || 1;
    const lo = low - pad;
    const hi = high + pad;

    const xOf = (ts) => ((ts - from) / span) * width;
    const yOf = (value) => height - ((value - lo) / (hi - lo)) * height;

    const bandTop = yOf(target + deadband);
    const bandBottom = yOf(target - deadband);

    return {
      path: gapPath(inRange, xOf, yOf),
      band: { y: bandTop, height: bandBottom - bandTop },
      empty: inRange.length === 0,
    };
  }

  function triggerTimeline(events, from, to, width) {
    const span = to - from || 1;
    return events
      .filter((event) => event.at >= from && event.at <= to)
      .map((event) => ({
        x: ((event.at - from) / span) * width,
        tone: resultInfo(event.result).tone,
      }));
  }

  // Die letzten count Ausloesungen, neueste zuerst. Das Fenster passt sich an
  // sie an (mindestens 30 Minuten), damit jede Regel ihren eigenen Rhythmus zeigt.
  function lastTriggers(events, nowSec, count = 4) {
    const newest = events
      .filter((event) => Number.isFinite(event.at))
      .sort((a, b) => b.at - a.at)
      .slice(0, count);
    if (!newest.length) return { marks: [], sinceLabel: '', empty: true };
    const agoMin = newest.map((event) => (nowSec - event.at) / 60);
    const span = Math.max(30, agoMin[agoMin.length - 1] * 1.12);
    let shownX = -Infinity;
    const marks = newest.map((event, index) => {
      const x = (100 - (agoMin[index] / span) * 92 - 4) / 100;
      const showLabel = Math.abs(x - shownX) > 0.12;
      if (showLabel) shownX = x;
      return { x, label: clockLabel(event.at), showLabel, tone: resultInfo(event.result).tone };
    });
    return { marks, sinceLabel: clockLabel(newest[newest.length - 1].at), empty: false };
  }

  function clockLabel(atSec) {
    const date = new Date(atSec * 1000);
    const pad = (value) => String(value).padStart(2, '0');
    return `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  }

  // Vorschau des Toleranzbands im Regler-Formular. Die Achse reicht von
  // target - span bis target + span, in Prozent der Breite. Liegt net sonst
  // ausserhalb, waechst span auf |net - target| * 1.2.
  function bandPreview({ target, deadband, net, span = 200 }) {
    let axisSpan = span;
    const hasNet = net !== null && net !== undefined && Number.isFinite(net);
    if (hasNet && Math.abs(net - target) > axisSpan) {
      axisSpan = Math.abs(net - target) * 1.2;
    }
    const lo = target - axisSpan;
    const total = axisSpan * 2;
    const percent = (value) => ((value - lo) / total) * 100;
    return {
      zone: {
        left: percent(target - deadband),
        width: (deadband * 2 / total) * 100,
      },
      target: percent(target),
      now: hasNet ? percent(net) : null,
      inside: hasNet ? Math.abs(net - target) <= deadband : null,
    };
  }

  window.__automationChart = { CHART_RANGES, rangeBounds, scenarioChart, sparkline, triggerTimeline, lastTriggers, bandPreview };
})();

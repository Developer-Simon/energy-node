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

    // Ein Abstand ueber GAP_SECONDS setzt die Linie neu an (M statt L).
    const linePath = (points) => {
      let previous = null;
      return points.map((point) => {
        const command = previous === null || point.ts - previous > GAP_SECONDS ? 'M' : 'L';
        previous = point.ts;
        return `${command}${xOf(point.ts).toFixed(1)} ${yOf(point.value).toFixed(1)}`;
      }).join(' ');
    };

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

  function triggerTimeline(events, from, to, width) {
    const span = to - from || 1;
    return events
      .filter((event) => event.at >= from && event.at <= to)
      .map((event) => ({
        x: ((event.at - from) / span) * width,
        tone: resultInfo(event.result).tone,
      }));
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

  window.__automationChart = { CHART_RANGES, rangeBounds, scenarioChart, triggerTimeline, bandPreview };
})();

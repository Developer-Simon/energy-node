// AUTOMATISCH ERZEUGT - nicht von Hand aendern.
// Quelle: dashboard/internal/webui/static/js/battery-card-core.js
// Erneuern mit: .venv/bin/python scripts/vendor_card.py
// Hostunabhaengiger Kern der Batterie-Statuskarte. Er rechnet, er baut DOM -
// und er kennt weder das Dashboard noch Home Assistant.
//
// Eingabe ist ein flaches BatteryInput:
//   {soc, capacity, watts, reserve, stale, nowTs, history, runtimeHours}
//   soc          Ladestand in %, null wenn keine Quelle zugeordnet ist
//   capacity     nutzbare Kapazitaet in kWh, 0 wenn unbekannt
//   watts        + laedt, - entlaedt
//   reserve      Notreserve in %, 0 heisst abgeschaltet
//   history      [{ts, v}] Ladestand-Verlauf, darf leer sein
//   runtimeHours optionale Restlaufzeit des Wirts, gewinnt gegen die
//                lineare Schaetzung (Home Assistant bringt eine mit)
//
// Wer den Kern erweitert: kein Zugriff auf EnergyModel, HistoryStore,
// Alpine oder hass. Der erste Test in battery-card-core.test.mjs liest
// diese Datei als Text und faellt um, sobald einer dieser Namen darin steht.
// Der Verlauf kommt ueber eine injizierte Lesefunktion (siehe readHistory),
// nicht ueber einen globalen Store.
(() => {
  'use strict';

  const DEFAULT_RESERVE = 10;

  // Ab hier ist eine Leistung ein Fluss und kein Messrauschen.
  const IDLE_WATTS = 5;

  const clamp = (value, low, high) => Math.max(low, Math.min(high, value));
  const num = (value, fallback = 0) => (Number.isFinite(Number(value)) ? Number(value) : fallback);

  // Verlaufs- und Fortschreibungsfenster in Stunden. historyHours gilt fuer
  // beide Haelften, forecastHours ueberschreibt nur die Fortschreibung -
  // faellt es weg, folgt es historyHours. Beide Wirte reichen hier ihre
  // Einstellung herein (Dashboard: data-battery-window, HA: config.window).
  const DEFAULT_WINDOW_HOURS = 6;
  const posNum = (value, fallback) => (Number.isFinite(Number(value)) && Number(value) > 0 ? Number(value) : fallback);

  function normalizeInput(raw) {
    const source = raw || {};
    const soc = Number.isFinite(Number(source.soc)) ? clamp(Number(source.soc), 0, 100) : null;
    const historyHours = posNum(source.historyHours, DEFAULT_WINDOW_HOURS);
    return {
      soc,
      capacity: Math.max(0, num(source.capacity, 0)),
      watts: num(source.watts, 0),
      reserve: clamp(num(source.reserve, DEFAULT_RESERVE), 0, 100),
      stale: !!source.stale,
      nowTs: num(source.nowTs, Date.now()),
      history: Array.isArray(source.history) ? source.history : [],
      runtimeHours: Number.isFinite(Number(source.runtimeHours)) ? Number(source.runtimeHours) : null,
      historyHours,
      forecastHours: posNum(source.forecastHours, historyHours),
    };
  }

  function batteryState(input) {
    const soc = input.soc === null ? 0 : input.soc;
    const stored = input.capacity * soc / 100;
    const reserveKWh = input.capacity * input.reserve / 100;
    return {
      hasSoC: input.soc !== null,
      hasCapacity: input.capacity > 0,
      soc,
      capacity: input.capacity,
      reserve: input.reserve,
      stored,
      reserveKWh,
      usable: Math.max(0, stored - reserveKWh),
      headroom: Math.max(0, input.capacity - stored),
      watts: input.watts,
      mode: input.watts > IDLE_WATTS ? 'charge' : (input.watts < -IDLE_WATTS ? 'discharge' : 'idle'),
      stale: input.stale,
    };
  }

  // Restlaufzeit bis zu der Grenze, die der aktuelle Fluss ansteuert. Ohne
  // Kapazitaet oder ohne Fluss gibt es keine - dann bleibt hours null und
  // die Karte sagt das, statt eine Zahl zu erfinden.
  function runtime(state, hoursOverride) {
    const kw = Math.abs(state.watts) / 1000;
    const idle = !state.hasCapacity || state.mode === 'idle' || kw < 0.01;
    const kind = idle ? 'idle'
      : state.mode === 'charge' ? 'full'
        : (state.reserve > 0 ? 'reserve' : 'empty');
    const bound = kind === 'idle' ? state.soc : kind === 'full' ? 100 : state.reserve;
    // Eine mitgelieferte Restlaufzeit (z. B. sensor.*_time_to_empty aus Home
    // Assistant) ist ihrer Natur nach eine Entlade-Schaetzung. Beim Laden
    // (kind 'full') wuerde sie unter der Beschriftung "bis voll" erscheinen
    // und eine Zeit bis leer behaupten - deshalb gilt sie nur, wo tatsaechlich
    // entladen wird.
    if (Number.isFinite(hoursOverride) && kind !== 'idle' && kind !== 'full') {
      return {hours: Math.max(0, hoursOverride), bound, kind};
    }
    if (idle) return {hours: null, bound, kind};
    return {hours: (kind === 'full' ? state.headroom : state.usable) / kw, bound, kind};
  }

  function toneOf(state, run) {
    if (state.stale) return 'stale';
    if (run.hours === null || run.kind === 'full') return 'ok';
    if (run.hours < 1) return 'bad';
    if (run.hours < 3) return 'warn';
    return 'ok';
  }

  // Horizont der Trajektorie: sechs Stunden Fortschreibung, dieselbe Spanne
  // wie SPAN_MS fuer die Vergangenheitshaelfte (dort in Millisekunden, hier
  // in Stunden - beide meinen "sechs Stunden Verlauf und sechs Stunden
  // Fortschreibung" aus der Spec).
  const FORECAST_HOURS = 6;

  // Fortschreibung bei konstanter Leistung, an der angesteuerten Grenze
  // abgeflacht. Ausdruecklich keine Prognose - genau das steht auch unter
  // der Achse der Trajektorie.
  function forecast(state, run, hours = FORECAST_HOURS, steps = 12) {
    const ratePerHour = state.hasCapacity ? (state.watts / 1000) / state.capacity * 100 : 0;
    const points = [];
    for (let index = 0; index <= steps; index += 1) {
      const h = hours * index / steps;
      let v = clamp(state.soc + ratePerHour * h, 0, 100);
      if (run.kind === 'reserve') v = Math.max(v, state.reserve);
      points.push({h, v});
    }
    return points;
  }

  // "4:51 h" statt "4,85 h" - eine Restlaufzeit liest man wie eine Uhrzeit.
  // Unter einer Stunde wechselt die Einheit auf Minuten, weil "0:11"
  // zoegern laesst und "11 min" nicht.
  function formatRuntime(hours) {
    if (hours === null || !Number.isFinite(hours)) return null;
    if (hours < 1) return `${Math.round(hours * 60)} min`;
    const total = Math.round(hours * 60);
    return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')} h`;
  }

  // Unter einer Viertelstunde ist eine Zeitachse kein Verlauf, sondern ein
  // Punkt mit Zittern. Dann zeichnet die Karte lieber gar keine
  // Vergangenheit als eine, die nichts zeigt.
  const MIN_HISTORY_MS = 15 * 60 * 1000;
  const SPAN_MS = 6 * 60 * 60 * 1000;

  // Festes Koordinatensystem des Diagramms. Die Raender sind so gewaehlt,
  // dass die aeussersten Beschriftungen innerhalb der viewBox bleiben - ein
  // Label ueber dem Rand ist im Bild abgeschnitten und nicht scrollbar.
  const VIEW = {x0: 10, x1: 310, y0: 14, y1: 98, tickY: 112, footY: 126};

  const median = values => {
    const sorted = [...values].sort((a, b) => a - b);
    const middle = Math.floor(sorted.length / 2);
    return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
  };

  // Eigene Umsetzung statt HistoryRollup.detectGaps(): der Kern darf das
  // Dashboard-Global nicht kennen. Die Regel ist dieselbe - ein Abstand vom
  // Fuenffachen des ueblichen Rasters, mindestens anderthalb Minuten.
  function detectGaps(points, thresholdMultiplier = 5, minGapMs = 90000) {
    if (points.length < 2) return [];
    const deltas = [];
    for (let index = 1; index < points.length; index += 1) deltas.push(points[index].ts - points[index - 1].ts);
    const typical = median(deltas);
    if (!(typical > 0)) return [];
    const limit = Math.max(typical * thresholdMultiplier, minGapMs);
    const gaps = [];
    for (let index = 1; index < points.length; index += 1) {
      if (points[index].ts - points[index - 1].ts > limit) {
        gaps.push({from: points[index - 1].ts, to: points[index].ts});
      }
    }
    return gaps;
  }

  // reader(fromTs, toTs) liefert Saetze mit {ts, v} oder {ts, avg} - die
  // Verdichtungsstufen des Dashboards tragen avg, Home Assistants Recorder
  // liefert Zustaende. Faellt der Leser um, hat die Karte eben keinen
  // Verlauf; dafuer gibt es historyMode('none').
  async function readHistory(reader, fromTs, toTs) {
    if (typeof reader !== 'function') return [];
    let rows = [];
    try {
      rows = await reader(fromTs, toTs);
    } catch (error) {
      return [];
    }
    return (rows || [])
      .map(row => ({ts: Number(row.ts), v: Number(typeof row.v === 'number' || typeof row.v === 'string' ? row.v : row.avg)}))
      .filter(point => Number.isFinite(point.ts) && Number.isFinite(point.v))
      .sort((a, b) => a.ts - b.ts);
  }

  // Zerlegt das Fenster in zusammenhaengende Stuecke. Eine
  // Aufzeichnungsluecke bricht die Linie auf, statt quer darueber zu ziehen.
  function socSegments(points, fromTs, toTs) {
    const inside = (points || []).filter(point =>
      Number.isFinite(point.ts) && Number.isFinite(point.v) && point.ts >= fromTs && point.ts <= toTs);
    if (inside.length < 2) return {segments: [], covered: null, points: inside};
    const sorted = inside.sort((a, b) => a.ts - b.ts);
    const gaps = detectGaps(sorted);
    const segments = [];
    let current = [sorted[0]];
    for (let index = 1; index < sorted.length; index += 1) {
      const broken = gaps.some(gap => gap.from === sorted[index - 1].ts && gap.to === sorted[index].ts);
      if (broken) {
        if (current.length > 1) segments.push(current);
        current = [sorted[index]];
      } else {
        current.push(sorted[index]);
      }
    }
    if (current.length > 1) segments.push(current);
    return {segments, covered: {from: sorted[0].ts, to: sorted[sorted.length - 1].ts}, points: inside};
  }

  // Wieviel der Vergangenheitshaelfte tatsaechlich belegt ist. 'none' laesst
  // die Fortschreibung die volle Breite nehmen - die Karte verliert damit
  // nichts, was sie haette zeigen koennen.
  function historyMode(windowed, nowTs, spanMs = SPAN_MS) {
    if (!windowed.segments.length || !windowed.covered) return {mode: 'none', fromTs: nowTs, spanMs: 0};
    const have = nowTs - windowed.covered.from;
    if (have < MIN_HISTORY_MS) return {mode: 'none', fromTs: nowTs, spanMs: 0};
    if (have < spanMs * 0.9) return {mode: 'partial', fromTs: windowed.covered.from, spanMs: have};
    return {mode: 'full', fromTs: nowTs - spanMs, spanMs};
  }

  const py = value => VIEW.y1 - (clamp(value, 0, 100) / 100) * (VIEW.y1 - VIEW.y0);
  const toPath = pairs => pairs.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' ');
  const roundHours = ms => Math.round(ms / 3600000 * 10) / 10;

  // opts.forecastHours setzt die Spanne der Fortschreibungshaelfte (Default
  // FORECAST_HOURS), opts.width die tatsaechliche Zeichenbreite in
  // SVG-Einheiten (Default VIEW.x1 + VIEW.x0 = die feste 320er-viewBox). Der
  // Renderer misst die Karte und rechnet die Geometrie bei jedem Resize mit
  // der neuen Breite neu - so wird eine breite Karte breiter statt hoeher.
  function chartGeometry(state, run, windowed, mode, opts = {}) {
    const forecastHours = posNum(opts.forecastHours, FORECAST_HOURS);
    const forecastMs = forecastHours * 3600000;
    const x1 = posNum(opts.width, VIEW.x1 + VIEW.x0) - VIEW.x0;
    const view = {...VIEW, x1};

    // nowX teilt die Flaeche im Verhaeltnis der beiden Zeitspannen. Ohne
    // Verlauf liegt es am linken Rand, die Fortschreibung bekommt alles.
    const nowX = VIEW.x0 + (x1 - VIEW.x0) * (mode.spanMs / (mode.spanMs + forecastMs));

    const historyPaths = mode.mode === 'none' ? [] : windowed.segments.map(segment => toPath(
      segment.map(point => [
        VIEW.x0 + (nowX - VIEW.x0) * clamp((point.ts - mode.fromTs) / Math.max(1, mode.spanMs), 0, 1),
        py(point.v),
      ]),
    ));

    const forecastPath = toPath(forecast(state, run, forecastHours).map(point => [
      nowX + (x1 - nowX) * (point.h / forecastHours),
      py(point.v),
    ]));

    const showBand = state.reserve > 0;
    const bandY = showBand ? py(state.reserve) : VIEW.y1;
    const showCrossing = run.hours !== null && (run.hours <= forecastHours || run.kind === 'empty');
    const crossingHours = showCrossing ? Math.min(run.hours, forecastHours) : null;

    return {
      historyPaths,
      forecastPath,
      nowX,
      nowY: py(state.soc),
      showBand,
      bandY,
      bandHeight: showBand ? VIEW.y1 - bandY : 0,
      crossing: !showCrossing ? null : {
        x: nowX + (x1 - nowX) * (crossingHours / forecastHours),
        y: py(run.bound),
        label: run.kind === 'full' ? 'voll' : run.kind === 'empty' ? 'leer' : 'Reserve',
      },
      hint: mode.mode === 'none'
        ? 'Kein Verlauf aufgezeichnet — nur die Fortschreibung.'
        : mode.mode === 'partial'
          ? `Erst ${roundHours(mode.spanMs)} h aufgezeichnet.`
          : null,
      ticks: {
        left: mode.mode === 'none' ? 'jetzt' : `−${roundHours(mode.spanMs)} h`,
        center: mode.mode === 'none' ? '' : 'jetzt',
        right: `+${roundHours(forecastMs)} h`,
      },
      view,
    };
  }

  // Das Karten-CSS als Zeichenkette, damit dieselbe Datei es ins Dokument
  // (Dashboard) und in ein Shadow-DOM (Lovelace) legen kann. Jede Farbe
  // faellt durch drei Stufen: Werkstatt-Dashboard, Home Assistant, Literal.
  // Keine font-family - beide Wirte geben ihre eigene vor.
  const CARD_CSS = `
.battery-card {
  --battery-surface: var(--panel, var(--ha-card-background, var(--card-background-color, #fff)));
  --battery-ink: var(--text-strong, var(--primary-text-color, #16181d));
  --battery-ink-2: var(--text, var(--primary-text-color, #2b3138));
  --battery-ink-3: var(--text-muted, var(--secondary-text-color, #6b7885));
  --battery-line: var(--border-soft, var(--divider-color, #e4e8ec));
  --battery-track: var(--track, var(--divider-color, #e6eaee));
  --battery-accent: var(--flow-battery, var(--state-icon-color, #7b4fd1));
  --battery-ok: var(--ok, var(--success-color, #1f7a52));
  --battery-warn: var(--warn, var(--warning-color, #9a6c12));
  --battery-bad: var(--bad, var(--error-color, #b23b45));
  --battery-faint: var(--text-faint, var(--disabled-text-color, #9aa5b1));
  --battery-radius: var(--radius-md, var(--ha-card-border-radius, 12px));
  --battery-tone: var(--battery-accent);
  --battery-ease: cubic-bezier(.23, 1, .32, 1);
  box-sizing: border-box; display: flex; flex-direction: column; gap: .75rem;
  padding: .9rem .95rem; height: 100%;
  background: var(--battery-surface); border: 1px solid var(--battery-line);
  border-radius: var(--battery-radius); color: var(--battery-ink-2);
  font-size: 14px; line-height: 1.45;
}
.battery-card * { box-sizing: border-box; }
.battery-card[data-tone="ok"] { --battery-tone: var(--battery-ok); }
.battery-card[data-tone="warn"] { --battery-tone: var(--battery-warn); }
.battery-card[data-tone="bad"] { --battery-tone: var(--battery-bad); }
.battery-card[data-tone="stale"] { --battery-tone: var(--battery-faint); }

.battery-head { display: flex; align-items: center; justify-content: space-between; gap: .5rem; }
.battery-head h3 { margin: 0; font-size: 12px; font-weight: 600; letter-spacing: .07em; text-transform: uppercase; color: var(--battery-ink-3); }
.battery-state { display: inline-flex; align-items: center; gap: .4rem; font-size: 12px; font-weight: 600; color: var(--battery-tone); font-variant-numeric: tabular-nums; transition: color 200ms ease; }
.battery-dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; flex: none; }

.battery-column-body { display: flex; gap: .9rem; align-items: stretch; flex: 1; }
.battery-column-side { display: flex; flex-direction: column; gap: .35rem; width: 46px; flex: none; }
.battery-column-gauge { position: relative; flex: 1; min-height: 118px; border-radius: 8px; overflow: hidden; background: var(--battery-track); }
.battery-column-fill { position: absolute; inset: 0; transform-origin: center bottom; background: var(--battery-tone); transition: transform 420ms var(--battery-ease), background-color 200ms ease; }
.battery-column-seams { position: absolute; inset: 0; background-image: repeating-linear-gradient(to top, var(--battery-surface) 0 2px, transparent 2px 10%); opacity: .85; }
.battery-column-reserve { position: absolute; left: 0; right: 0; bottom: 0; border-top: 2px dashed var(--battery-surface); }
.battery-column-pct { text-align: center; font-size: 13px; font-weight: 600; color: var(--battery-ink); font-variant-numeric: tabular-nums; }

.battery-rows { margin: 0; display: flex; flex-direction: column; gap: .55rem; flex: 1; min-width: 0; }
.battery-row { display: flex; flex-direction: column; gap: .05rem; padding-left: .55rem; border-left: 2px solid var(--battery-line); }
.battery-row.is-lead { border-left-color: var(--battery-tone); transition: border-color 200ms ease; }
.battery-row dt { font-size: 10.5px; letter-spacing: .05em; text-transform: uppercase; color: var(--battery-ink-3); }
.battery-row dd { margin: 0; font-size: 13.5px; font-weight: 600; color: var(--battery-ink); font-variant-numeric: tabular-nums; }
.battery-row dd small { display: block; font-size: 11.5px; font-weight: 400; color: var(--battery-ink-3); }

.battery-traj-body { display: flex; gap: 1.1rem; align-items: center; flex: 1; }
.battery-ring { position: relative; width: 112px; height: 112px; flex: none; }
.battery-ring svg { display: block; width: 100%; height: 100%; }
.battery-ring-track { fill: none; stroke: var(--battery-track); stroke-width: 9; }
.battery-ring-arc { fill: none; stroke: var(--battery-tone); stroke-width: 9; stroke-linecap: round; transition: stroke-dashoffset 420ms var(--battery-ease), stroke 200ms ease; }
.battery-ring-label { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 1px; }
.battery-ring-label b { font-size: 26px; font-weight: 600; letter-spacing: -.03em; line-height: 1; color: var(--battery-ink); font-variant-numeric: tabular-nums; }
.battery-ring-label span { font-size: 10.5px; letter-spacing: .05em; text-transform: uppercase; color: var(--battery-ink-3); }

.battery-chart { flex: 1; min-width: 0; }
/* Feste Hoehe, Breite fuellt die Karte: der Renderer (mountTrajectory) setzt
   die viewBox-Breite per ResizeObserver passend zur gemessenen Pixelbreite,
   damit eine Einheit ~ ein Pixel bleibt und nichts verzerrt. Bis zur ersten
   Messung skaliert die Default-preserveAspectRatio ("xMidYMid meet") die feste
   320er-viewBox mittig ein - ein Frame Briefkasten, keine Verzerrung. */
.battery-chart svg { display: block; width: 100%; height: 150px; }
.battery-band { fill: var(--battery-ink-3); opacity: .13; }
.battery-axis { stroke: var(--battery-line); stroke-width: 1; }
.battery-nowline { stroke: var(--battery-ink-3); stroke-width: 1; stroke-dasharray: 2 3; opacity: .8; }
.battery-hist { fill: none; stroke: var(--battery-tone); stroke-width: 2.2; stroke-linejoin: round; stroke-linecap: round; transition: stroke 200ms ease; }
.battery-forecast { fill: none; stroke: var(--battery-tone); stroke-width: 2; stroke-dasharray: 4 3.5; opacity: .72; stroke-linecap: round; }
.battery-nowdot { fill: var(--battery-tone); }
.battery-crossing { fill: var(--battery-surface); stroke: var(--battery-tone); stroke-width: 1.6; }
.battery-tick, .battery-foot { font-size: 8.5px; fill: var(--battery-ink-3); }
.battery-foot { opacity: .75; }
.battery-hint { margin: .3rem 0 0; font-size: 11.5px; color: var(--battery-ink-3); }
.battery-hint[hidden] { display: none; }

.battery-kpis { margin: 0; padding-top: .7rem; border-top: 1px solid var(--battery-line); display: grid; grid-template-columns: repeat(3, 1fr); gap: .6rem; }
.battery-kpis div { display: flex; flex-direction: column; gap: .1rem; min-width: 0; }
.battery-kpis dt { font-size: 10.5px; letter-spacing: .05em; text-transform: uppercase; color: var(--battery-ink-3); }
.battery-kpis dd { margin: 0; font-size: 14px; font-weight: 600; color: var(--battery-ink); font-variant-numeric: tabular-nums; white-space: nowrap; }

@media (prefers-reduced-motion: reduce) {
  .battery-card { --battery-ease: linear; }
  .battery-column-fill, .battery-ring-arc { transition-duration: 1ms; }
}
`;

  function injectStyle(target) {
    const host = target && target.head ? target.head : target;
    if (!host || host.querySelector('style[data-battery-card-style]')) return;
    const style = (host.ownerDocument || document).createElement('style');
    style.setAttribute('data-battery-card-style', '');
    style.textContent = CARD_CSS;
    host.appendChild(style);
  }

  const RING_CIRCUMFERENCE = 2 * Math.PI * 44;

  const nf = (value, digits) =>
    value.toLocaleString('de-DE', {minimumFractionDigits: digits, maximumFractionDigits: digits});

  function boundLabel(run) {
    if (run.kind === 'full') return 'bis voll';
    if (run.kind === 'empty') return 'bis leer';
    return 'bis Reserve';
  }

  function rowsFor(state, run) {
    const coverage = !state.hasSoC
      ? {value: 'keine Speicher-Quelle', note: 'Erst eine Entität als Ladezustand zuordnen.'}
      : run.hours === null
        ? {value: 'hält den Stand', note: state.hasCapacity ? 'Weder Laden noch Entladen.' : 'Ohne nutzbare Kapazität keine Restlaufzeit.'}
        : {value: `noch ${formatRuntime(run.hours)}`, note: `${boundLabel(run)} bei ${nf(Math.abs(state.watts) / 1000, 2)} kW`};

    const second = state.reserve > 0
      ? {key: 'reserve', label: 'Reserve', value: `${nf(state.reserve, 0)} % · ${nf(state.reserveKWh, 1)} kWh`, note: 'bleibt für den Netzausfall stehen'}
      : {key: 'stock', label: 'Vorrat', value: `${nf(state.stored, 1)} kWh`, note: 'bis 0 % nutzbar, keine Reserve gesetzt'};

    return [
      {key: 'coverage', label: 'Deckung', value: coverage.value, note: coverage.note},
      second,
      {
        key: 'capacity',
        label: 'Kapazität',
        value: state.hasCapacity ? `${nf(state.capacity, 1)} kWh` : '—',
        note: state.hasCapacity ? `ein Segment ${nf(state.capacity / 10, 2)} kWh` : 'Kapazität hinterlegen',
      },
    ];
  }

  function viewFrom(rawInput) {
    const input = normalizeInput(rawInput);
    const state = batteryState(input);
    const run = runtime(state, input.runtimeHours);
    const historySpanMs = input.historyHours * 3600000;
    const windowed = socSegments(input.history, input.nowTs - historySpanMs, input.nowTs);
    const mode = historyMode(windowed, input.nowTs, historySpanMs);
    // chartInputs reicht die Zutaten an den Renderer weiter, damit er die
    // Geometrie beim Resize mit der gemessenen Breite neu rechnen kann, ohne
    // den ganzen Schnappschuss noch einmal durch viewFrom zu schicken.
    const chartInputs = {state, run, windowed, mode, forecastHours: input.forecastHours};
    return {
      tone: toneOf(state, run),
      socLabel: state.hasSoC ? `${nf(state.soc, 0)} %` : '—',
      socNumber: state.hasSoC ? nf(state.soc, 0) : '—',
      reserveLabel: state.reserve > 0 ? `Reserve ${nf(state.reserve, 0)} %` : '',
      stateLabel: state.stale ? 'Werte veraltet'
        : state.mode === 'charge' ? `Lädt · ${nf(Math.abs(state.watts) / 1000, 2)} kW`
          : state.mode === 'discharge' ? `Entlädt · ${nf(Math.abs(state.watts) / 1000, 2)} kW`
            : 'Ruht',
      ringOffset: RING_CIRCUMFERENCE * (1 - state.soc / 100),
      gauge: {fillScale: state.soc / 100, reserveHeight: state.reserve, showReserve: state.reserve > 0},
      rows: rowsFor(state, run),
      kpis: [
        {key: 'runtime', label: 'Restlaufzeit', value: run.hours === null ? '—' : `noch ${formatRuntime(run.hours)}`},
        {key: 'usable', label: 'Abrufbar', value: `${nf(state.usable, 1)} kWh`},
        {key: 'flow', label: 'Fluss', value: `${state.watts >= 0 ? '+' : '−'}${nf(Math.abs(state.watts) / 1000, 2)} kW`},
      ],
      chart: chartGeometry(state, run, windowed, mode, {forecastHours: input.forecastHours}),
      chartInputs,
    };
  }

  // --- DOM-Bauer. Gerueste einmal, danach nur Werte. ------------------------

  const el = (tag, className, text) => {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  };
  const svgEl = (tag, attrs) => {
    const node = document.createElementNS('http://www.w3.org/2000/svg', tag);
    Object.entries(attrs || {}).forEach(([name, value]) => node.setAttribute(name, String(value)));
    return node;
  };

  function head(title) {
    const header = el('header', 'battery-head');
    header.append(el('h3', null, title));
    const state = el('span', 'battery-state');
    state.append(el('i', 'battery-dot'), el('span'));
    header.append(state);
    return {header, stateText: state.lastChild};
  }

  function mountColumn(root) {
    root.classList.add('battery-card');
    root.textContent = '';
    const {header, stateText} = head('Speicher');

    const fill = el('div', 'battery-column-fill');
    const reserve = el('div', 'battery-column-reserve');
    const gauge = el('div', 'battery-column-gauge');
    gauge.setAttribute('role', 'img');
    gauge.append(fill, el('div', 'battery-column-seams'), reserve);
    const pct = el('span', 'battery-column-pct');
    const side = el('div', 'battery-column-side');
    side.append(gauge, pct);

    const list = el('dl', 'battery-rows');
    const rows = [0, 1, 2].map(() => {
      const row = el('div', 'battery-row');
      const term = el('dt');
      const value = el('span');
      const note = el('small');
      const def = el('dd');
      def.append(value, note);
      row.append(term, def);
      list.append(row);
      return {row, term, value, note};
    });

    const body = el('div', 'battery-column-body');
    body.append(side, list);
    root.append(header, body);

    return {
      update(view) {
        root.dataset.tone = view.tone;
        stateText.textContent = view.stateLabel;
        pct.textContent = view.socLabel;
        gauge.setAttribute('aria-label', `Ladestand ${view.socLabel}`);
        fill.style.transform = `scaleY(${view.gauge.fillScale})`;
        reserve.hidden = !view.gauge.showReserve;
        reserve.style.height = `${view.gauge.reserveHeight}%`;
        view.rows.forEach((data, index) => {
          const slot = rows[index];
          slot.row.classList.toggle('is-lead', data.key === 'coverage');
          slot.term.textContent = data.label;
          slot.value.textContent = data.value;
          slot.note.textContent = data.note;
        });
      },
    };
  }

  function mountTrajectory(root) {
    root.classList.add('battery-card');
    root.textContent = '';
    const {header, stateText} = head('Speicher · Verlauf und Fortschreibung');

    const ringSvg = svgEl('svg', {viewBox: '0 0 104 104', 'aria-hidden': 'true', focusable: 'false'});
    const arc = svgEl('circle', {
      class: 'battery-ring-arc', cx: 52, cy: 52, r: 44,
      transform: 'rotate(-90 52 52)', 'stroke-dasharray': RING_CIRCUMFERENCE.toFixed(2),
    });
    ringSvg.append(svgEl('circle', {class: 'battery-ring-track', cx: 52, cy: 52, r: 44}), arc);
    const ringNumber = el('b');
    const ringLabel = el('div', 'battery-ring-label');
    ringLabel.append(ringNumber, el('span', null, '% Ladestand'));
    const ring = el('div', 'battery-ring');
    ring.append(ringSvg, ringLabel);

    const chartSvg = svgEl('svg', {viewBox: `0 0 ${VIEW.x1 + VIEW.x0} 130`, role: 'img'});
    const band = svgEl('rect', {class: 'battery-band', x: VIEW.x0, width: VIEW.x1 - VIEW.x0, y: VIEW.y1, height: 0});
    const axis = svgEl('line', {class: 'battery-axis', x1: VIEW.x0, y1: VIEW.y1, x2: VIEW.x1, y2: VIEW.y1});
    const nowLine = svgEl('line', {class: 'battery-nowline', y1: VIEW.y0, y2: VIEW.y1, x1: VIEW.x0, x2: VIEW.x0});
    const histGroup = svgEl('g', {});
    const forecastLine = svgEl('polyline', {class: 'battery-forecast', points: ''});
    const nowDot = svgEl('circle', {class: 'battery-nowdot', r: 3.4, cx: VIEW.x0, cy: VIEW.y1});
    const crossDot = svgEl('circle', {class: 'battery-crossing', r: 3.4, cx: 0, cy: 0});
    const tickLeft = svgEl('text', {class: 'battery-tick', x: VIEW.x0, y: VIEW.tickY});
    const tickCenter = svgEl('text', {class: 'battery-tick', y: VIEW.tickY, 'text-anchor': 'middle', x: VIEW.x0});
    const tickRight = svgEl('text', {class: 'battery-tick', x: VIEW.x1, y: VIEW.tickY, 'text-anchor': 'end'});
    const bandLabel = svgEl('text', {class: 'battery-tick', x: VIEW.x0 + 4, y: VIEW.y1 - 3});
    const foot = svgEl('text', {class: 'battery-foot', x: VIEW.x1, y: VIEW.footY, 'text-anchor': 'end'});
    foot.textContent = 'Fortschreibung bei konstanter Leistung';
    chartSvg.append(band, axis, nowLine, histGroup, forecastLine, crossDot, nowDot, tickLeft, tickCenter, tickRight, bandLabel, foot);

    const hint = el('p', 'battery-hint');
    const chart = el('div', 'battery-chart');
    chart.append(chartSvg, hint);

    const body = el('div', 'battery-traj-body');
    body.append(ring, chart);

    const kpiList = el('dl', 'battery-kpis');
    const kpis = [0, 1, 2].map(() => {
      const cell = el('div');
      const term = el('dt');
      const value = el('dd');
      cell.append(term, value);
      kpiList.append(cell);
      return {term, value};
    });

    root.append(header, body, kpiList);

    // Die viewBox ist 130 Einheiten hoch und im CSS auf CHART_HEIGHT Pixel
    // festgenagelt. Ihre Breite folgt der gemessenen Kartenbreite, damit eine
    // Einheit ~ ein Pixel bleibt - so waechst eine breite Karte in die Breite
    // (mehr Zeitachse) statt in die Hoehe (dieselbe Achse, nur groesser).
    const CHART_HEIGHT = 150;
    const BASE_WIDTH = VIEW.x1 + VIEW.x0;
    let plotWidth = BASE_WIDTH;
    let lastInputs = null;
    let lastSocLabel = '—';

    const widthForPixels = px =>
      px > 0 ? Math.max(BASE_WIDTH, Math.round(130 * px / CHART_HEIGHT)) : BASE_WIDTH;

    function drawChart(geometry) {
      const {x0, x1} = geometry.view;
      chartSvg.setAttribute('viewBox', `0 0 ${(x1 + x0).toFixed(0)} 130`);
      // Die frueher fest verdrahteten rechten Kanten wandern mit x1.
      band.setAttribute('width', (x1 - x0).toFixed(1));
      axis.setAttribute('x2', x1.toFixed(1));
      tickRight.setAttribute('x', x1.toFixed(1));
      foot.setAttribute('x', x1.toFixed(1));

      band.setAttribute('y', geometry.bandY.toFixed(2));
      band.setAttribute('height', geometry.bandHeight.toFixed(2));
      band.style.display = geometry.showBand ? '' : 'none';

      nowLine.setAttribute('x1', geometry.nowX.toFixed(1));
      nowLine.setAttribute('x2', geometry.nowX.toFixed(1));
      nowDot.setAttribute('cx', geometry.nowX.toFixed(1));
      nowDot.setAttribute('cy', geometry.nowY.toFixed(1));

      // Nur die Zahl der Linien wird angeglichen - der Rest des Geruests
      // bleibt stehen, damit Farbwechsel weiter ueberblenden koennen.
      while (histGroup.childNodes.length > geometry.historyPaths.length) histGroup.lastChild.remove();
      while (histGroup.childNodes.length < geometry.historyPaths.length) {
        histGroup.append(svgEl('polyline', {class: 'battery-hist', points: ''}));
      }
      geometry.historyPaths.forEach((path, index) => histGroup.childNodes[index].setAttribute('points', path));
      forecastLine.setAttribute('points', geometry.forecastPath);

      if (geometry.crossing) {
        crossDot.style.display = '';
        crossDot.setAttribute('cx', geometry.crossing.x.toFixed(1));
        crossDot.setAttribute('cy', geometry.crossing.y.toFixed(1));
      } else {
        crossDot.style.display = 'none';
      }

      tickLeft.textContent = geometry.ticks.left;
      tickCenter.textContent = geometry.ticks.center;
      tickCenter.setAttribute('x', geometry.nowX.toFixed(1));
      tickRight.textContent = geometry.ticks.right;

      hint.textContent = geometry.hint || '';
      hint.hidden = !geometry.hint;

      chartSvg.setAttribute('aria-label',
        `Ladestand ${lastSocLabel}. ${geometry.hint || 'Verlauf und Fortschreibung des Ladestands.'}`);
    }

    function renderChart() {
      if (!lastInputs) return;
      drawChart(chartGeometry(lastInputs.state, lastInputs.run, lastInputs.windowed, lastInputs.mode,
        {forecastHours: lastInputs.forecastHours, width: plotWidth}));
    }

    let observer = null;
    if (typeof ResizeObserver === 'function') {
      observer = new ResizeObserver(entries => {
        const px = entries[entries.length - 1]?.contentRect?.width || chart.clientWidth || 0;
        const next = widthForPixels(px);
        if (next === plotWidth) return;
        plotWidth = next;
        renderChart();
      });
      observer.observe(chart);
    }

    return {
      update(view) {
        root.dataset.tone = view.tone;
        stateText.textContent = view.stateLabel;
        ringNumber.textContent = view.socNumber;
        arc.setAttribute('stroke-dashoffset', view.ringOffset.toFixed(2));
        bandLabel.textContent = view.reserveLabel;
        lastSocLabel = view.socLabel;

        lastInputs = view.chartInputs || null;
        if (lastInputs) renderChart();
        else drawChart(view.chart);

        view.kpis.forEach((kpi, index) => {
          kpis[index].term.textContent = kpi.label;
          kpis[index].value.textContent = kpi.value;
        });
      },
      destroy() {
        if (observer) observer.disconnect();
      },
    };
  }

  window.BatteryCardCore = {
    DEFAULT_RESERVE, MIN_HISTORY_MS, SPAN_MS, VIEW, CARD_CSS, RING_CIRCUMFERENCE, injectStyle, viewFrom, mountColumn, mountTrajectory,
    normalizeInput, batteryState, runtime, toneOf, forecast, formatRuntime,
    detectGaps, readHistory, socSegments, historyMode, chartGeometry,
  };
})();

// Ist-Soll-Grafik fuer Regelbefunde. Reine Rechnung als HTML-String, keine DOM-Abhaengigkeit.
// Keine Texte in den SVG-Grafiken: Spurmarken und Achse stehen als HTML.
// Form und Feldnamen folgen dem Go-Evidence in dashboard/internal/autolearn/finding.go.
(() => {
  const esc = (value) => String(value == null ? '' : value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');

  const DAY = 1440;
  const FULL_DAY = { from: 0, to: DAY, full: true };
  const BAR_BASE = 8.5; // Unterkante der Balken in der Spur (viewBox 0..10)
  const BAR_FULL = 7; // Hoehe eines vollen Balkens
  const BAR_MIN = 0.3; // Mindesthoehe, damit ein einzelnes Ereignis sichtbar bleibt
  const EVENT_BINS = 24;
  const MAJOR_STEPS = [30, 60, 120, 180, 240, 360, 720]; // Hauptmarken in Minuten, je Ausschnitt
  const MINUTE_STEPS = [30, 60, 120, 240, 480, 600, 720, 1440]; // Massstab in Minuten
  const NICE = [1, 2, 2.5, 5, 10];
  const EDGE = 30; // Zeitgrenzen des Ausschnitts runden auf volle halbe Stunden
  const MIN_WINDOW = 120;
  const MAX_WINDOW = 960;

  const clamp = (v) => Math.max(0, Math.min(100, v));
  // Zeitpunkt auf die Spur: ohne Zeitraum bei 0 %, sonst auf 0..100 begrenzt.
  const pos = (v, from, to) => (to === from ? 0 : clamp(((v - from) / (to - from)) * 100));
  // Wie pos, aber ohne Begrenzung: Balken am Rand werden erst beim Zeichnen beschnitten.
  const rawPos = (v, from, to) => (to === from ? 0 : ((v - from) / (to - from)) * 100);
  const pct = (v, max) => clamp((v / max) * 100);
  const round = (n) => Math.round(n * 1000) / 1000;
  const norm = (v) => ((v % DAY) + DAY) % DAY;
  // Abstand auf dem Kreis der Tagesminuten, im Bereich -720..720.
  const wrap = (d) => ((((d % DAY) + DAY + DAY / 2) % DAY) + DAY) % DAY - DAY / 2;
  const finiteList = (list) => (Array.isArray(list) ? list.filter((v) => Number.isFinite(v)) : []);
  const finitePairs = (list) => (Array.isArray(list)
    ? list.filter((p) => Array.isArray(p) && p.length === 2 && Number.isFinite(p[0]) && Number.isFinite(p[1]))
    : []);

  // Ohne Max (z. B. unerfuellbare Regel) ergibt sich ein Massstab aus den Werten.
  function niceCeil(x, unit) {
    if (unit === 'min') return MINUTE_STEPS.find((s) => s >= x) || Math.ceil(x / DAY) * DAY;
    const mag = Math.pow(10, Math.floor(Math.log10(x)));
    return NICE.map((m) => m * mag).find((v) => v >= x);
  }

  function scaleMaxOf(ev) {
    if (ev.max > 0) return ev.max;
    const seen = Array.isArray(ev.seen) ? ev.seen[1] || 0 : 0;
    const ist = Number.isFinite(ev.ist_value) ? ev.ist_value : 0;
    const soll = Number.isFinite(ev.soll_value) ? ev.soll_value : 0;
    const top = Math.max(seen, ist, soll) * 1.2;
    return top > 0 ? niceCeil(top, ev.unit) : 1;
  }

  function laneSvg(inner) {
    return '<svg class="auto-learn-lane-svg" viewBox="0 0 100 10" preserveAspectRatio="none" aria-hidden="true">' + inner + '</svg>';
  }

  function lane(label, body, soll) {
    return '<div class="auto-learn-lane' + (soll ? ' is-soll' : '') + '"><span class="auto-learn-lane-label">' + esc(label) + '</span>' + body + '</div>';
  }

  // Hilfslinien hinter den Balken, an den Hauptmarken der Achse.
  function gridLines(xs, height = 10) {
    return xs.map((x) => '<line class="auto-learn-grid" x1="' + round(x) + '" x2="' + round(x) + '" y1="0" y2="' + height + '" vector-effect="non-scaling-stroke"/>').join('');
  }

  function spans(list, from, to, cls) {
    return (list || []).map((s) => {
      const x = pos(s[0], from, to);
      const w = pos(s[1], from, to) - x;
      return '<rect class="' + cls + '" x="' + x + '" y="1.5" width="' + w + '" height="7" rx="0.8"/>';
    }).join('');
  }

  function marks(list, toX, cls) {
    return (list || []).map((v) => {
      const x = toX(v);
      return '<line class="' + cls + '" x1="' + x + '" x2="' + x + '" y1="0.5" y2="9.5" vector-effect="non-scaling-stroke"/>';
    }).join('');
  }

  // Ein Balken mit abgerundeter Oberkante. x0 und x1 sind Prozent, h ein Anteil der Hoehe.
  function bar(x0, x1, h) {
    const gap = (x1 - x0) * 0.2;
    const a = clamp(x0 + gap / 2);
    const b = clamp(x1 - gap / 2);
    if (!(b - a > 0)) return '';
    const top = BAR_BASE - BAR_FULL * h;
    const r = Math.min(0.8, (b - a) / 2, (BAR_BASE - top) / 2);
    const d = 'M' + round(a) + ',' + BAR_BASE + 'V' + round(top + r)
      + 'Q' + round(a) + ',' + round(top) + ' ' + round(a + r) + ',' + round(top)
      + 'H' + round(b - r)
      + 'Q' + round(b) + ',' + round(top) + ' ' + round(b) + ',' + round(top + r)
      + 'V' + BAR_BASE + 'Z';
    return '<path class="auto-learn-bar" d="' + d + '" data-x="' + round(a) + '" data-w="' + round(b - a) + '" data-h="' + round(h) + '"/>';
  }

  // Zaehlt die Werte in Klassen und zeichnet je belegter Klasse einen Balken. Hoehe relativ zur staerksten Klasse.
  function histogram(values, start, binSize, count, xOf) {
    if (!(binSize > 0) || !(count > 0)) return '';
    const tally = new Map();
    values.forEach((u) => {
      const i = Math.min(count - 1, Math.floor((u - start) / binSize));
      if (i >= 0) tally.set(i, (tally.get(i) || 0) + 1);
    });
    const top = Math.max(0, ...tally.values());
    if (top === 0) return '';
    return Array.from(tally.entries())
      .map(([i, n]) => bar(xOf(start + i * binSize), xOf(start + (i + 1) * binSize), Math.max(BAR_MIN, n / top)))
      .join('');
  }

  // Ein Tagesminutenwert auf dem abgewickelten Band: alle Vorkommen im Fenster, halboffen.
  function occurrences(v, from, to) {
    const base = norm(v);
    const out = [];
    [-DAY, 0, DAY].forEach((k) => {
      const u = base + k;
      if (u >= from && u < to) out.push(u);
    });
    return out;
  }

  function occurrencesOf(list, from, to) {
    return finiteList(list).reduce((acc, v) => acc.concat(occurrences(v, from, to)), []);
  }

  function marksOf(list, cls, from, to) {
    return occurrencesOf(list, from, to)
      .map((u) => '<line class="' + cls + '" x1="' + round(pos(u, from, to)) + '" x2="' + round(pos(u, from, to)) + '" y1="0.5" y2="9.5" vector-effect="non-scaling-stroke"/>')
      .join('');
  }

  // Spannen wie ein Band auf der Tagesachse, ein Band darf ueber Mitternacht laufen.
  function bandsOf(list, cls, from, to) {
    const out = [];
    finitePairs(list).forEach((s) => {
      [-DAY, 0, DAY].forEach((k) => {
        const a = norm(s[0]) + k;
        const b = a + (s[1] - s[0]);
        const lo = Math.max(a, from);
        const hi = Math.min(b, to);
        if (hi > lo) {
          const x = pos(lo, from, to);
          out.push('<rect class="' + cls + '" x="' + round(x) + '" y="1.5" width="' + round(pos(hi, from, to) - x) + '" height="7" rx="0.8"/>');
        }
      });
    });
    return out.join('');
  }

  // Achse als HTML: Striche in einer SVG, Beschriftung als absolut gesetzte Spans.
  // ticks: [{x, major}] und labels: [{x, text}] mit x in Prozent.
  function axisHtml(ticks, labels) {
    const svg = '<svg class="auto-learn-ticks" viewBox="0 0 100 5" preserveAspectRatio="none" aria-hidden="true">'
      + ticks.map((k) => '<line class="' + (k.major ? 'is-major' : 'is-minor') + '" x1="' + round(k.x) + '" x2="' + round(k.x) + '" y1="0" y2="' + (k.major ? 5 : 2.5) + '" vector-effect="non-scaling-stroke"/>').join('')
      + '</svg>';
    const text = labels.map((l, i) => {
      const edge = i === 0 ? ' is-start' : (i === labels.length - 1 ? ' is-end' : '');
      return '<span class="auto-learn-axis-label' + edge + '" style="left:' + round(l.x) + '%">' + esc(l.text) + '</span>';
    }).join('');
    return '<div class="auto-learn-axis"><span></span><div class="auto-learn-axis-track">' + svg + text + '</div></div>';
  }

  const pad2 = (n) => (n < 10 ? '0' : '') + n;

  // Uhrzeit HH:MM aus Tagesminuten. Der Vollschnitt endet mit 24:00, ein Ausschnitt rechnet modulo einen Tag.
  function clockText(minute, full) {
    if (full && minute === DAY) return '24:00';
    const m = norm(minute);
    return pad2(Math.floor(m / 60)) + ':' + pad2(m % 60);
  }

  // Hauptmarken und Nebenmarken fuer die Tagesachse. Nebenmarken stuendlich, bei 3 h und weniger alle 15 min.
  function dayAxis(from, to, full) {
    const len = to - from;
    const major = full ? 360 : (MAJOR_STEPS.find((s) => len / s <= 4) || 720);
    const minor = full ? 60 : (len <= 180 ? 15 : 60);
    const majors = [];
    const minors = [];
    for (let v = Math.ceil(from / minor) * minor; v <= to; v += minor) {
      (v % major === 0 ? majors : minors).push(v);
    }
    return { majors, minors };
  }

  // Zeitfenster der Tagesgrafik. Alle Ist- und Soll-Werte werden auf dem Kreis der Tagesminuten
  // um ihren Mittelpunkt abgewickelt, der Ausschnitt umfasst sie samt Soll-Spannen.
  function windowOf(ev) {
    if (!ev || ev.shape !== 'day') return { ...FULL_DAY };
    const ist = finiteList(ev.ist);
    const soll = finiteList(ev.soll);
    const sollSpans = finitePairs(ev.soll_spans);
    const spanBounds = finitePairs(ev.ist_spans).concat(sollSpans);
    const points = ist.concat(soll, spanBounds.reduce((acc, s) => acc.concat([s[0], s[1]]), []));
    if (points.length === 0) return { ...FULL_DAY };

    const sums = points.map((c) => points.reduce((acc, v) => acc + Math.abs(wrap(norm(v) - norm(c))), 0));
    const centre = norm(points[sums.indexOf(Math.min(...sums))]);
    const unwrap = (v) => centre + wrap(norm(v) - centre);
    const unwrapped = points.map(unwrap);
    const med = median(unwrapped);
    const mad = median(unwrapped.map((u) => Math.abs(u - med)));
    const half = Math.max(60, 3 * 1.4826 * mad + 30);

    let lo = med - half;
    let hi = med + half;
    soll.forEach((v) => {
      const u = unwrap(v);
      lo = Math.min(lo, u);
      hi = Math.max(hi, u);
    });
    sollSpans.forEach((s) => {
      const a = unwrap(s[0]);
      lo = Math.min(lo, a, a + (s[1] - s[0]));
      hi = Math.max(hi, a, a + (s[1] - s[0]));
    });

    let from = Math.floor(lo / EDGE) * EDGE;
    // Das obere Ende liegt strikt ueber dem hoechsten Wert, damit er im halboffenen Fenster bleibt.
    let to = (Math.floor(hi / EDGE) + 1) * EDGE;
    if (to - from < MIN_WINDOW) {
      const mid = (from + to) / 2;
      from = Math.floor((mid - MIN_WINDOW / 2) / EDGE) * EDGE;
      to = (Math.floor((mid + MIN_WINDOW / 2) / EDGE) + 1) * EDGE;
    }
    if (to - from >= MAX_WINDOW) return { ...FULL_DAY };
    return { from, to, full: false };
  }

  function median(list) {
    const s = list.slice().sort((a, b) => a - b);
    const n = s.length;
    return n % 2 ? s[(n - 1) / 2] : (s[n / 2 - 1] + s[n / 2]) / 2;
  }

  function zoomable(ev) {
    return Boolean(ev) && ev.shape === 'day' && !windowOf(ev).full;
  }

  function dayShape(ev, t, opts) {
    const win = opts && opts.full ? FULL_DAY : windowOf(ev);
    const from = win.from;
    const to = win.to;
    const xOf = (v) => rawPos(v, from, to);
    const axis = dayAxis(from, to, win.full);
    const grid = gridLines(axis.majors.map((v) => pos(v, from, to)));
    const binSize = to - from <= 240 ? 5 : 10;
    const start = Math.floor(from / binSize) * binSize;
    const count = Math.ceil((to - start) / binSize);
    const ist = grid
      + bandsOf(ev.ist_spans, 'auto-learn-ist auto-learn-soft', from, to) // i18n-ignore
      + histogram(occurrencesOf(ev.ist, from, to), start, binSize, count, xOf);
    const soll = grid
      + bandsOf(ev.soll_spans, 'auto-learn-soll', from, to)
      + marksOf(ev.soll, 'auto-learn-mark-soll', from, to);
    const ticks = axis.minors.map((v) => ({ x: pos(v, from, to), major: false }))
      .concat(axis.majors.map((v) => ({ x: pos(v, from, to), major: true })));
    const labels = axis.majors.map((v) => ({ x: pos(v, from, to), text: clockText(v, win.full) }));
    return lane(t('automations.learn.lane.ist'), laneSvg(ist)) // i18n-ignore
      + lane(t('automations.learn.lane.soll'), laneSvg(soll), true)
      + axisHtml(ticks, labels);
  }

  const clockOf = (sec) => {
    const d = new Date(sec * 1000);
    return pad2(d.getHours()) + ':' + pad2(d.getMinutes());
  };

  function eventsShape(ev, t) {
    const from = ev.from;
    const to = ev.to;
    const xOf = (v) => rawPos(v, from, to);
    const inside = finiteList(ev.ist).filter((u) => u >= from && u <= to);
    const mid = Math.round((from + to) / 2);
    const majors = [from, mid, to];
    const grid = gridLines(majors.map((v) => pos(v, from, to)));
    const ist = grid + histogram(inside, from, (to - from) / EVENT_BINS, EVENT_BINS, xOf);
    const soll = grid + spans(ev.soll_spans, from, to, 'auto-learn-soll');
    const step = to - from <= 10800 ? 15 : 60;
    const minors = [];
    if (to > from) {
      for (let s = Math.ceil(from / 60) * 60; s <= to; s += 60) {
        if (s !== from && s !== to && new Date(s * 1000).getMinutes() % step === 0) minors.push(s);
      }
    }
    const ticks = minors.map((s) => ({ x: pos(s, from, to), major: false }))
      .concat(majors.map((v) => ({ x: pos(v, from, to), major: true })));
    const labels = majors.map((v) => ({ x: pos(v, from, to), text: clockOf(v) }));
    return lane(t('automations.learn.lane.ist'), laneSvg(ist)) // i18n-ignore
      + lane(t('automations.learn.lane.soll'), laneSvg(soll), true)
      + axisHtml(ticks, labels);
  }

  // Einheiten mit Katalogtext, sonst die Einheit wie geliefert.
  const UNIT_KEYS = { kwh_week: 'automations.learn.unit.kwh_week' };
  function unitOf(ev, t) {
    return UNIT_KEYS[ev.unit] ? t(UNIT_KEYS[ev.unit]) : ev.unit;
  }

  // Ein Balkenpaar je Parameter, wenn die Evidenz Scales liefert.
  // i18n-keys: automations.learn.param.gain, automations.learn.param.deadband_w, automations.learn.param.max_change_w
  const PARAM_KEYS = {
    gain: 'automations.learn.param.gain',
    deadband_w: 'automations.learn.param.deadband_w',
    max_change_w: 'automations.learn.param.max_change_w',
  };
  function scaleBarsShape(ev, t) {
    return ev.scales.map((bar) => {
      const top = Math.max(bar.ist || 0, bar.soll || 0);
      const max = top > 0 ? top * 1.2 : 1;
      const toX = (v) => pct(v, max);
      const unit = bar.unit ? ' ' + bar.unit : '';
      const majors = [0, max / 2, max];
      const grid = gridLines(majors.map(toX));
      const istBar = '<rect class="auto-learn-ist" x="0" y="1.5" width="' + toX(bar.ist || 0) + '" height="7" rx="0.8"/>'; // i18n-ignore
      const sollBar = '<rect class="auto-learn-soll auto-learn-soft" x="0" y="1.5" width="' + toX(bar.soll || 0) + '" height="7" rx="0.8"/>'; // i18n-ignore
      const ticks = majors.map((v) => ({ x: toX(v), major: true }));
      const labels = majors.map((v) => ({ x: toX(v), text: (Math.round(v * 100) / 100) + unit }));
      return '<div class="auto-learn-scale-bar">' + esc(t(PARAM_KEYS[bar.name] || bar.name)) + '</div>'
        + lane(t('automations.learn.lane.ist'), laneSvg(grid + istBar)) // i18n-ignore
        + lane(t('automations.learn.lane.soll'), laneSvg(grid + sollBar), true)
        + axisHtml(ticks, labels);
    }).join('');
  }

  function scaleShape(ev, t) {
    if (Array.isArray(ev.scales) && ev.scales.length > 0) return scaleBarsShape(ev, t);
    const max = scaleMaxOf(ev);
    const toX = (v) => pct(v, max);
    const unit = ev.unit ? ' ' + unitOf(ev, t) : '';
    const majors = [0, max / 2, max];
    const grid = gridLines(majors.map(toX));
    const seen = Array.isArray(ev.seen) && ev.seen[1] > 0
      ? '<rect class="auto-learn-ist auto-learn-soft" x="0" y="1.5" width="' + toX(ev.seen[1]) + '" height="7" rx="0.8"/>' // i18n-ignore
      : '';
    const hasIst = Number.isFinite(ev.ist_value);
    const hasSoll = Number.isFinite(ev.soll_value);
    const istBar = hasIst ? '<rect class="auto-learn-ist" x="0" y="1.5" width="' + toX(ev.ist_value) + '" height="7" rx="0.8"/>' : ''; // i18n-ignore
    const threshold = hasIst ? marks([ev.ist_value], toX, 'auto-learn-mark-threshold') : '';
    const sollBar = hasSoll ? '<rect class="auto-learn-soll auto-learn-soft" x="0" y="1.5" width="' + toX(ev.soll_value) + '" height="7" rx="0.8"/>' : '';
    const soll = hasSoll ? marks([ev.soll_value], toX, 'auto-learn-mark-soll auto-learn-mark-wide') : '';
    const ticks = majors.map((v) => ({ x: toX(v), major: true }));
    const labels = majors.map((v) => ({ x: toX(v), text: Math.round(v) + unit }));
    return lane(t('automations.learn.lane.ist'), laneSvg(grid + seen + istBar + threshold)) // i18n-ignore
      + lane(t('automations.learn.lane.soll'), laneSvg(grid + sollBar + soll), true)
      + axisHtml(ticks, labels);
  }

  // Ist und Soll teilen eine Kurvenflaeche, die Soll-Ebene liegt ueber der Ist-Ebene.
  // Tageskurve aus 96 Viertelstunden in W. Ist als Linie, Soll-Spur mit Klarhimmel-Kurve, Bestkurve
  // gestrichelt und die abgeregelte Flaeche zwischen Best und Ist, wo die Bestkurve darueber liegt.
  const CURVE_N = 96;
  const CURVE_HOURS = [0, 6, 12, 18, 24];

  // Werte mit Index, fehlende oder nicht endliche Werte entfallen.
  function curveSeries(list) {
    if (!Array.isArray(list)) return [];
    const out = [];
    list.slice(0, CURVE_N).forEach((v, i) => {
      if (Number.isFinite(v)) out.push([i, v]);
    });
    return out;
  }

  const curveX = (i) => round((i / CURVE_N) * 100);
  // Ist und Soll teilen ein Koordinatensystem, so hoch wie zwei Spuren.
  const PLOT_H = 20;
  const curveY = (v, max) => round(PLOT_H - 0.5 - (Math.max(0, v) / max) * (PLOT_H - 1));

  function plotSvg(inner, soll) {
    return '<svg class="auto-learn-plot-svg' + (soll ? ' is-soll' : '') + '" viewBox="0 0 100 ' + PLOT_H + '" preserveAspectRatio="none" aria-hidden="true">' + inner + '</svg>'; // i18n-ignore
  }

  function curveShape(ev, t) {
    const ist = curveSeries(ev.ist_q);
    const soll = curveSeries(ev.soll_q);
    const best = curveSeries(ev.best_q);
    if (ist.length === 0 && soll.length === 0 && best.length === 0) return '';
    const max = Math.max(0, ...ist.concat(soll, best).map((p) => p[1]));
    const scale = max > 0 ? max : 1;
    const point = (i, v) => curveX(i) + ',' + curveY(v, scale);
    const line = (series, cls, dashed) => (series.length === 0 ? '' // i18n-ignore
      : '<polyline class="' + cls + '" points="' + series.map((p) => point(p[0], p[1])).join(' ') + '" vector-effect="non-scaling-stroke"' // i18n-ignore
      + (dashed ? ' stroke-dasharray="4 3"' : '') + '/>'); // i18n-ignore
    const istAt = new Map(ist);
    const bestAt = new Map(best);
    // Zusammenhaengende Viertelstunden, in denen die Bestkurve ueber der Istkurve liegt.
    const runs = [];
    let run = [];
    for (let i = 0; i < CURVE_N; i++) {
      const open = bestAt.has(i) && istAt.has(i) && bestAt.get(i) > istAt.get(i);
      if (open) {
        run.push(i);
      } else {
        if (run.length > 1) runs.push(run);
        run = [];
      }
    }
    if (run.length > 1) runs.push(run);
    const throttled = runs.map((r) => '<polygon class="auto-learn-throttled" points="' // i18n-ignore
      + r.map((i) => point(i, bestAt.get(i))).concat(r.slice().reverse().map((i) => point(i, istAt.get(i)))).join(' ')
      + '"/>').join(''); // i18n-ignore
    const hours = CURVE_HOURS.map((h) => h * 60);
    const grid = gridLines(hours.map((m) => (m / DAY) * 100), PLOT_H);
    const istBody = grid + line(ist, 'auto-learn-ist auto-learn-curve', false); // i18n-ignore
    // Mit Bestkurve (Verschattung) ist Soll die Klarhimmel-Referenz, sonst die Sollkurve selbst.
    const sollCls = best.length > 0 ? 'auto-learn-clear' : 'auto-learn-soll-line'; // i18n-ignore
    const sollBody = throttled + line(soll, sollCls, false) + line(best, 'auto-learn-best', true); // i18n-ignore
    const hasSoll = soll.length > 0 || best.length > 0;
    const ticks = [];
    for (let h = 0; h <= 24; h++) {
      ticks.push({ x: (h / 24) * 100, major: CURVE_HOURS.indexOf(h) >= 0 });
    }
    const labels = hours.map((m) => ({ x: (m / DAY) * 100, text: clockText(m, true) }));
    const legend = '<div class="auto-learn-plot-legend">' // i18n-ignore
      + '<span class="auto-learn-plot-key">' + esc(t('automations.learn.lane.ist')) + '</span>' // i18n-ignore
      + (hasSoll ? '<span class="auto-learn-plot-key is-soll">' + esc(t('automations.learn.lane.soll')) + '</span>' : '') // i18n-ignore
      + '</div>';
    return '<div class="auto-learn-plot">' + legend + '<div class="auto-learn-plot-area">' // i18n-ignore
      + plotSvg(istBody, false) + (hasSoll ? plotSvg(sollBody, true) : '')
      + '</div></div>' + axisHtml(ticks, labels);
  }

  function chips(names) {
    return (names || []).map((n) => '<span class="auto-learn-chip">' + esc(n) + '</span>').join('');
  }

  // Ist: Regelnamen als Chips. Ziel: ein Knoten, gestrichelt bei Missing.
  // Soll: die Soll-Namen, ohne Soll-Namen steht nur das Ziel auf der Spur.
  function linksShape(ev, t) {
    const hasSoll = Array.isArray(ev.soll_names) && ev.soll_names.length > 0;
    const targetNode = '<div class="auto-learn-target' + (ev.missing ? ' is-missing' : '') + '">' + esc(ev.target) + '</div>';
    const middle = ev.target && hasSoll ? targetNode : '';
    const sollBody = hasSoll ? chips(ev.soll_names) : (ev.target ? targetNode : '');
    return '<div class="auto-learn-lane"><span class="auto-learn-lane-label">' + esc(t('automations.learn.lane.ist')) + '</span><div class="auto-learn-chips">' + chips(ev.ist_names) + '</div></div>' // i18n-ignore
      + middle
      + '<div class="auto-learn-lane is-soll"><span class="auto-learn-lane-label">' + esc(t('automations.learn.lane.soll')) + '</span><div class="auto-learn-chips">' + sollBody + '</div></div>';
  }

  const SHAPES = { day: dayShape, events: eventsShape, scale: scaleShape, links: linksShape, curve: curveShape };

  // render(evidence, t, opts) -> HTML. t(key, params) liefert die Spurmarken.
  // opts.full: true zeigt die Tagesachse 0 bis 24 h statt des Ausschnitts.
  function render(evidence, t, opts) {
    if (!evidence || !Object.prototype.hasOwnProperty.call(SHAPES, evidence.shape)) return '';
    const tr = typeof t === 'function' ? t : (key) => key;
    const body = SHAPES[evidence.shape](evidence, tr, opts || {});
    return body ? '<div class="auto-learn-is auto-learn-shape-' + evidence.shape + '">' + body + '</div>' : '';
  }

  window.AutomationEvidence = { render, windowOf, zoomable };
})();

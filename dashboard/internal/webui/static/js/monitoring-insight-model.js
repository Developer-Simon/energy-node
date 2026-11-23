// Modell der Monitoring-Seite fuer Prognose und Saison: reine Funktionen ohne DOM, Alpine und Intl.
// Die Monatsnamen kommen als monthLabels herein, Zahlen am Rand werden hier ganzzahlig gruppiert.
(() => {
  const METRICS = [
    { id: 'pv', key: 'pv_kwh', unit: 'kWh', color: 'var(--flow-pv)', sum: true, d: 0, good: 1, area: true },
    { id: 'load', key: 'load_kwh', unit: 'kWh', color: 'var(--flow-load)', sum: true, d: 0, good: -1, area: true },
    { id: 'export', key: 'export_kwh', unit: 'kWh', color: 'var(--flow-grid)', sum: true, d: 0, good: 1, area: true },
    { id: 'base', key: 'base_w', unit: 'W', color: 'var(--flow-heatpump)', sum: false, d: 0, good: -1, area: true },
    { id: 'temp', key: 'temp_c', unit: '°C', color: 'var(--accent)', sum: false, d: 1, good: 0, area: false },
  ];
  // erste ISO-Woche jedes Monats, fuer die Achse
  const MONTH_WEEKS = [1, 5, 9, 14, 18, 23, 27, 31, 36, 40, 44, 49];
  const FORECAST_NEEDED = 14;

  const metricOf = (id) => METRICS.find((m) => m.id === id) || METRICS[0];

  // Ganzzahl mit Punkt als Tausendertrenner, wie de-DE, ohne Intl.
  function intText(v) {
    const r = Math.round(v);
    const digits = String(Math.abs(r)).replace(/\B(?=(\d{3})+(?!\d))/g, '.');
    return (r < 0 ? '-' : '') + digits;
  }

  function niceStep(raw) {
    const p = Math.pow(10, Math.floor(Math.log10(raw || 1)));
    for (const f of [1, 2, 2.5, 5, 10]) if (f * p >= raw) return f * p;
    return 10 * p;
  }

  // Index = ISO-Woche (1..53), Eintrag { v, src } oder null fuer eine Luecke.
  function seasonSeries(weeks, year, metricId) {
    const m = metricOf(metricId);
    // Index = ISO-Woche, 1..52 wie im Entwurf (ISO-Woche 53 wird nicht gezeichnet).
    const out = new Array(53).fill(null);
    for (const w of weeks || []) {
      const [yy, ww] = String(w.key).split('-W').map(Number);
      if (yy !== year || !(ww >= 1 && ww <= 52)) continue;
      const v = w[m.key];
      if (typeof v !== 'number' || !isFinite(v)) continue;
      out[ww] = { v, src: w.source };
    }
    return out;
  }

  // Kopfzahl: bis zur aktuellen Woche, nur Wochen, die in beiden Jahren da sind.
  function seasonSummary(season, metricId) {
    const m = metricOf(metricId);
    const a = seasonSeries(season.weeks, season.year, metricId);
    const b = seasonSeries(season.weeks, season.year - 1, metricId);
    let sa = 0; let sb = 0; let n = 0;
    for (let w = 1; w <= season.week; w++) if (a[w] && b[w]) { sa += a[w].v; sb += b[w].v; n++; }
    if (!n) return null;
    const cur = m.sum ? sa : sa / n;
    const prev = m.sum ? sb : sb / n;
    const diff = m.id === 'temp' ? cur - prev : (prev ? (cur - prev) / prev * 100 : 0);
    const g = m.good * Math.sign(cur - prev);
    return { cur, prev, diff, tone: g > 0 ? 'is-up' : g < 0 ? 'is-down' : '', skipped: season.week - n };
  }

  // Linienstuecke, ein neuer Lauf beginnt bei jeder Quellenwechsel oder Luecke.
  function runs(s) {
    const out = []; let cur = null;
    for (let w = 1; w <= 52; w++) {
      const p = s[w];
      if (!p) { cur = null; continue; }
      if (!cur || cur.src !== p.src) {
        cur = { src: p.src, pts: [] };
        // naechster Lauf beginnt am letzten Punkt des vorigen, damit die Flaeche durchgeht
        const prev = out[out.length - 1];
        if (prev && s[w - 1]) cur.pts.push(prev.pts[prev.pts.length - 1]);
        out.push(cur);
      }
      cur.pts.push([w, p.v]);
    }
    return out;
  }

  // Jahresdiagramm: years ist 2 oder 3 (wie viele Jahre gezeigt werden, das aktuelle zuerst).
  function seasonChart(season, metricId, years, width, narrow, monthLabels) {
    const m = metricOf(metricId);
    const y = season.year;
    const yearList = years === 3 ? [y, y - 1, y - 2] : [y, y - 1];
    const W = width || 600; const H = narrow ? 168 : 212;
    const pad = { l: 38, r: 10, t: 10, b: 24 };
    const series = (yr) => seasonSeries(season.weeks, yr, metricId);
    let lo = Infinity; let hi = -Infinity;
    for (const yr of yearList) for (const p of series(yr)) if (p) { lo = Math.min(lo, p.v); hi = Math.max(hi, p.v); }
    if (!isFinite(lo)) { lo = 0; hi = 1; }
    const step = niceStep((hi - Math.min(0, lo)) / 3);
    const y0 = lo < 0 || m.id === 'temp' ? Math.floor(Math.min(0, lo) / step) * step : 0;
    const y1 = Math.ceil(hi / step) * step;
    const x = (w) => pad.l + (w - 1) / 51 * (W - pad.l - pad.r);
    const yv = (v) => pad.t + (1 - (v - y0) / (y1 - y0)) * (H - pad.t - pad.b);
    const geo = { W, H, pad, x, y: yv, y0, y1, step };

    const line = (pts) => pts.map((p, i) => (i ? 'L' : 'M') + x(p[0]).toFixed(1) + ' ' + yv(p[1]).toFixed(1)).join(' ');
    const area = (pts) => line(pts) + ' L' + x(pts[pts.length - 1][0]).toFixed(1) + ' ' + yv(y0).toFixed(1) + ' L' + x(pts[0][0]).toFixed(1) + ' ' + yv(y0).toFixed(1) + ' Z';

    let s = '<svg class="mi-svg" width="' + W + '" height="' + H + '" viewBox="0 0 ' + W + ' ' + H + '" style="color:' + m.color + '" aria-hidden="true">';
    s += '<defs><pattern id="mi-hatch" width="5" height="5" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><rect width="5" height="5" fill="currentColor" opacity=".08"/><line x1="0" y1="0" x2="0" y2="5" stroke="currentColor" stroke-width="1.6" opacity=".45"/></pattern></defs>';
    for (let v = y0; v <= y1 + 1e-9; v += step) {
      s += '<line class="mi-grid" x1="' + pad.l + '" x2="' + (W - pad.r) + '" y1="' + yv(v).toFixed(1) + '" y2="' + yv(v).toFixed(1) + '"/>';
      s += '<text class="mi-ylab" x="' + (pad.l - 6) + '" y="' + (yv(v) + 3.5).toFixed(1) + '">' + intText(v) + '</text>';
    }
    MONTH_WEEKS.forEach((w, i) => {
      const label = String((monthLabels || [])[i] || '');
      const lab = narrow ? label.charAt(0) : label.slice(0, 3);
      s += '<text class="mi-xlab" x="' + x(w).toFixed(1) + '" y="' + (H - 6) + '">' + lab + '</text>';
    });
    // Luecken im aktuellen Jahr als Band
    const cur = series(y);
    for (let w = 1; w <= season.week; w++) if (!cur[w]) {
      s += '<rect class="mi-gap" x="' + (x(w) - (x(2) - x(1)) / 2).toFixed(1) + '" y="' + pad.t + '" width="' + (x(2) - x(1)).toFixed(1) + '" height="' + (H - pad.t - pad.b) + '"/>';
    }
    // Vorjahre als Linien
    yearList.slice(1).forEach((yr, i) => {
      for (const r of runs(series(yr))) s += '<path class="mi-prev mi-prev-' + (i + 1) + '" d="' + line(r.pts) + '"/>';
    });
    // aktuelles Jahr: Flaeche, Linie, Endpunkt
    s += '<g class="mi-cur">';
    for (const r of runs(cur)) {
      if (m.area || r.src === 'stats') s += '<path class="mi-area' + (r.src === 'stats' ? ' is-stats' : '') + (m.area ? '' : ' is-band') + '" d="' + area(r.pts) + '"/>';
      s += '<path class="mi-line' + (r.src === 'stats' ? ' is-stats' : '') + '" d="' + line(r.pts) + '"/>';
    }
    const last = cur[season.week];
    if (last) s += '<circle class="mi-end" cx="' + x(season.week).toFixed(1) + '" cy="' + yv(last.v).toFixed(1) + '" r="4"/>';
    s += '</g>';
    // Lesemarke, wird ohne Neuzeichnen verschoben
    s += '<g class="mi-cursor"><line class="mi-hair" y1="' + pad.t + '" y2="' + (H - pad.b) + '"/>';
    yearList.forEach((yr, i) => { s += '<circle class="mi-dot mi-dot-' + i + '" r="3.5"/>'; });
    s += '</g></svg>';
    return { svg: s, geo };
  }

  // Ergebnis der Prognose: Lernphase, Median oder Prognose, mit den mittleren Fehlern.
  function forecastState(check) {
    const days = check.days || [];
    const needed = check.needed != null ? check.needed : FORECAST_NEEDED;
    const n = days.length;
    const maeF = days.reduce((sum, d) => sum + Math.abs(d.forecast_wh - d.actual_wh), 0) / Math.max(1, n);
    const maeM = days.reduce((sum, d) => sum + Math.abs(d.season_wh - d.actual_wh), 0) / Math.max(1, n);
    const hits = days.filter((d) => Math.abs(d.forecast_wh - d.actual_wh) < Math.abs(d.season_wh - d.actual_wh)).length;
    let kind = 'forecast';
    if (n < needed) kind = 'learning';
    else if (maeF >= maeM) kind = 'median';
    return { kind, maeF, maeM, hits, have: n, needed };
  }

  // Tagesbalken Prognose gegen Messung, leere Plaetze bis needed.
  function forecastChart(days, needed, width, narrow) {
    const W = width || 600; const H = narrow ? 150 : 176;
    const pad = { l: 34, r: 6, t: 8, b: 36 };
    const n = needed != null ? needed : FORECAST_NEEDED;
    const cw = (W - pad.l - pad.r) / n;
    const max = days.reduce((top, d) => Math.max(top, d.forecast_wh, d.actual_wh, d.season_wh), 4000);
    const top = Math.ceil(max / 2000) * 2000;
    const yv = (wh) => pad.t + (1 - wh / top) * (H - pad.t - pad.b);
    const bw = Math.min(14, cw * 0.28);
    const geo = { W, H, pad, cw };

    let s = '<svg class="mi-svg" width="' + W + '" height="' + H + '" viewBox="0 0 ' + W + ' ' + H + '" aria-hidden="true">';
    for (let v = 0; v <= top; v += 2000) {
      s += '<line class="mi-grid" x1="' + pad.l + '" x2="' + (W - pad.r) + '" y1="' + yv(v).toFixed(1) + '" y2="' + yv(v).toFixed(1) + '"/>';
      s += '<text class="mi-ylab" x="' + (pad.l - 6) + '" y="' + (yv(v) + 3.5).toFixed(1) + '">' + intText(v / 1000) + '</text>';
    }
    s += '<g class="mi-cols">';
    for (let i = 0; i < n; i++) {
      const d = days[i]; const cx = pad.l + cw * (i + 0.5);
      if (!d) {
        s += '<rect class="mi-slot" x="' + (cx - bw - 1).toFixed(1) + '" y="' + yv(top * 0.55).toFixed(1) + '" width="' + (2 * bw + 2).toFixed(1) + '" height="' + (yv(0) - yv(top * 0.55)).toFixed(1) + '" rx="3"/>';
        continue;
      }
      const near = Math.abs(d.forecast_wh - d.actual_wh) < Math.abs(d.season_wh - d.actual_wh);
      s += '<g class="mi-col" data-i="' + i + '">';
      s += '<rect class="mi-hit" x="' + (cx - cw / 2).toFixed(1) + '" y="' + pad.t + '" width="' + cw.toFixed(1) + '" height="' + (H - pad.t) + '"/>';
      s += '<rect class="mi-fc" x="' + (cx - bw - 1 + 0.75).toFixed(1) + '" y="' + yv(d.forecast_wh).toFixed(1) + '" width="' + (bw - 1.5).toFixed(1) + '" height="' + Math.max(0, yv(0) - yv(d.forecast_wh) - 0.75).toFixed(1) + '" rx="2"/>';
      s += '<rect class="mi-act" x="' + (cx + 1).toFixed(1) + '" y="' + yv(d.actual_wh).toFixed(1) + '" width="' + bw.toFixed(1) + '" height="' + (yv(0) - yv(d.actual_wh)).toFixed(1) + '" rx="2"/>';
      s += '<line class="mi-med" x1="' + (cx - bw - 4).toFixed(1) + '" x2="' + (cx + bw + 4).toFixed(1) + '" y1="' + yv(d.season_wh).toFixed(1) + '" y2="' + yv(d.season_wh).toFixed(1) + '"/>';
      s += '<circle class="mi-near' + (near ? ' is-fc' : '') + '" cx="' + cx.toFixed(1) + '" cy="' + (yv(0) + 9) + '" r="3"/>';
      const dt = new Date(d.day + 'T12:00:00');
      if (!narrow || i % 2 === 1) s += '<text class="mi-xlab" x="' + cx.toFixed(1) + '" y="' + (H - 6) + '">' + dt.getDate() + '.</text>';
      s += '</g>';
    }
    s += '</g></svg>';
    return { svg: s, geo };
  }

  // Uhrzeit HH:MM in Minuten seit Mitternacht, sonst null.
  function clock(v) {
    const m = /^(\d\d):(\d\d)$/.exec(String(v));
    return m ? Number(m[1]) * 60 + Number(m[2]) : null;
  }

  // Intervall [a, b) auf der 24-h-Achse in Prozent, ueber Mitternacht in zwei Teilen.
  const DAY_STEP = 14.4;
  const EPS = 1e-6;
  function segs(a, b) {
    const parts = b > a ? [[a, b]] : [[a, 1440], [0, b]];
    return parts.filter(([x, y]) => y > x).map(([x, y]) => ({ left: x / DAY_STEP, width: (y - x) / DAY_STEP }));
  }

  // Vergleich fuer Namen, standardmaessig einfach nach Zeichen. Die Sortierung der Oberflaeche kommt von I18n.compare.
  const plainCompare = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

  // Geraete aus den gelernten Werten, nach Name sortiert.
  function collectDevices(values, compare) {
    const by = new Map();
    for (const v of values || []) {
      if (!v.key || !v.key.startsWith('device/')) continue;
      const [, id, field] = v.key.split('/');
      if (!by.has(id)) by.set(id, { id, name: id, icon: 'ico-device', f: {} });
      const d = by.get(id);
      if (v.device_name) d.name = v.device_name;
      if (v.device_icon) d.icon = v.device_icon;
      d.f[field] = v;
    }
    const cmp = typeof compare === 'function' ? compare : plainCompare;
    return [...by.values()].sort((a, b) => cmp(a.name, b.name));
  }

  // Zeilen des ueblichen Tages: erst das Haus (Nachtruhe), dann Geraete.
  // kind: night (Haus), on (Fenster), point (nur Beginn oder Ende, 15 Minuten), free (nur Laufzeit).
  function dayRows(values, compare) {
    const by = new Map();
    for (const v of values || []) by.set(v.key, v);
    const nr = by.get('site/night_rest'); const ds = by.get('site/day_start');
    const rows = [];
    const a = nr ? clock(nr.value) : null; const b = ds ? clock(ds.value) : null;
    if (nr && ds && a != null && b != null) {
      const n = Math.min(nr.n || 0, ds.n || 0);
      rows.push({ id: 'house', kind: 'night', name: null, nameKey: 'monitoring.day.house', icon: 'ico-user', segs: segs(a, b), from: nr.value, to: ds.value,
        minutes: (b - a + 1440) % 1440, sure: n >= 10, n, keys: ['site/night_rest', 'site/day_start'], year: [nr, ds], entries: [nr, ds] });
    } else {
      const present = [nr, ds].filter(Boolean);
      rows.push({ id: 'house', kind: 'free', name: null, nameKey: 'monitoring.day.house', icon: 'ico-user', segs: [], from: null, to: null, minutes: null,
        sure: false, n: 0, keys: present.map((v) => v.key), year: null, entries: present });
    }
    for (const d of collectDevices(values, compare)) {
      const on = d.f.usual_on; const off = d.f.usual_off; const rt = d.f.usual_runtime_s;
      const base = { id: d.id, name: d.name, icon: d.icon };
      // Gleiche Minute fuer Beginn und Ende ist ein Punkt, kein volles Fenster.
      if (on && off && clock(on.value) !== clock(off.value)) {
        const s = clock(on.value); const e = clock(off.value);
        const n = Math.min(on.n || 0, off.n || 0);
        rows.push({ ...base, kind: 'on', segs: segs(s, e), from: on.value, to: off.value, minutes: (e - s + 1440) % 1440,
          sure: n >= 10, n, keys: [on.key, off.key], year: [on, off], entries: [on, off] });
      } else if (on || off) {
        const only = on || off; const t = clock(only.value);
        const s = on ? t : (t - 15 + 1440) % 1440;
        const ents = [on, off].filter(Boolean);
        const n = Math.min(...ents.map((x) => x.n || 0));
        rows.push({ ...base, kind: 'point', segs: segs(s, (s + 15) % 1440), from: on ? on.value : null, to: off ? off.value : null,
          minutes: null, sure: n >= 10, n, keys: ents.map((x) => x.key), year: null, entries: ents });
      } else if (rt) {
        rows.push({ ...base, kind: 'free', segs: [], from: null, to: null, minutes: Math.round(rt.value / 60),
          sure: (rt.n || 0) >= 10, n: rt.n || 0, keys: [rt.key], year: null, entries: [rt] });
      }
    }
    return rows;
  }

  // Welche Zeilen laufen zu einer Minute: Nachtruhe und die Namen der on-Zeilen.
  function onAt(rows, minute) {
    const inSeg = (r) => (r.segs || []).some((g) => minute >= g.left * DAY_STEP - EPS && minute < (g.left + g.width) * DAY_STEP - EPS);
    const list = rows || [];
    const house = list.find((r) => r.id === 'house');
    return {
      night: !!house && house.kind === 'night' && inSeg(house),
      on: list.filter((r) => r.kind === 'on' && inSeg(r)).map((r) => r.name),
    };
  }

  // Laufzeiten der Geraete mit Zyklus oder Leistung, relativ zum groessten Wert.
  function runRows(values, compare) {
    const list = [];
    for (const d of collectDevices(values, compare)) {
      const e = d.f.cycle_wh; const p = d.f.op_power_w; const rt = d.f.usual_runtime_s;
      if (!e && !p) continue;
      const wh = e ? e.value : null;
      const seconds = rt ? rt.value : (e && p && p.value ? e.value / p.value * 3600 : null);
      list.push({ id: d.id, name: d.name, icon: d.icon, wh, powerW: p ? p.value : null, seconds,
        programs: (e && e.programs) || [], sure: ((e || p).n || 0) >= 10 });
    }
    const max = Math.max(1, ...list.flatMap((r) => [r.wh || 0, ...r.programs]));
    return list.map((r) => ({ id: r.id, name: r.name, icon: r.icon, wh: r.wh, powerW: r.powerW, seconds: r.seconds,
      pct: r.wh ? r.wh / max * 100 : 0, ticks: r.programs.map((x) => x / max * 100), sure: r.sure }));
  }

  // Jahreslauf eines Fensters: je Woche das uebliche Band zwischen Beginn und Ende, nur gemeinsame Wochen.
  function yearBand(onEntry, offEntry, night) {
    const weekMap = (entry) => new Map(((entry && entry.year) || []).map((y) => [y.week, y.value]));
    const on = weekMap(onEntry); const off = weekMap(offEntry);
    const weeks = [...on.keys()].filter((k) => off.has(k)).sort();
    const pts = [];
    for (const k of weeks) {
      const x = clock(on.get(k)); const y = clock(off.get(k));
      if (x == null || y == null) continue;
      pts.push({ k, x, y: y < x ? y + 1440 : y });
    }
    const n = pts.length;
    if (n < 4) return null;
    const W = 300; const H = 64; const P = 4;
    const lo = Math.floor((Math.min(...pts.map((p) => p.x)) - 30) / 60) * 60;
    const hi = Math.ceil((Math.max(...pts.map((p) => p.y)) + 30) / 60) * 60;
    const X = (i) => P + i / (n - 1) * (W - 2 * P);
    const Y = (m) => P + (m - lo) / (hi - lo) * (H - 2 * P);
    const top = pts.map((p, i) => (i ? 'L' : 'M') + X(i).toFixed(1) + ' ' + Y(p.x).toFixed(1)).join(' ');
    const bot = pts.slice().reverse().map((p, j) => 'L' + X(n - 1 - j).toFixed(1) + ' ' + Y(p.y).toFixed(1)).join(' ');
    const line = (useEnd) => pts.map((p, i) => (i ? 'L' : 'M') + X(i).toFixed(1) + ' ' + Y(useEnd ? p.y : p.x).toFixed(1)).join(' ');
    const svg = '<svg class="mi-yr" viewBox="0 0 ' + W + ' ' + H + '" preserveAspectRatio="none" aria-hidden="true">'
      + '<path class="mi-yr-band' + (night ? ' is-night' : '') + '" d="' + top + ' ' + bot + ' Z"/>'
      + '<path class="mi-yr-line" d="' + line(false) + '"/><path class="mi-yr-line" d="' + line(true) + '"/></svg>';
    return { svg, from: pts[0].k, to: pts[n - 1].k, lo, hi };
  }

  window.MonitoringInsight = {
    METRICS,
    seasonSeries,
    seasonSummary,
    seasonChart,
    forecastState,
    forecastChart,
    dayRows,
    onAt,
    runRows,
    yearBand,
  };
})();

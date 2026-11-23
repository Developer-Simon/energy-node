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

  window.MonitoringInsight = {
    METRICS,
    seasonSeries,
    seasonSummary,
    seasonChart,
    forecastState,
    forecastChart,
  };
})();

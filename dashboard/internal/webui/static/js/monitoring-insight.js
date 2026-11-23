// Ueberwachung: Prognose und Jahresvergleich. Die Daten kommen aus
// GET /api/v1/monitoring/forecast (forecast und season), die Zahlen und Zeichnungen
// baut window.MonitoringInsight, die Texte kommen aus dem Katalog.
//
// i18n-keys: monitoring.forecast.load_failed, monitoring.forecast.progress,
// monitoring.forecast.use, monitoring.forecast.use_none, monitoring.forecast.mae.caption,
// monitoring.forecast.mae.forecast, monitoring.forecast.mae.median,
// monitoring.forecast.status.learning.title, monitoring.forecast.status.learning.text,
// monitoring.forecast.status.median.title, monitoring.forecast.status.median.text,
// monitoring.forecast.status.forecast.title, monitoring.forecast.status.forecast.text,
// monitoring.forecast.verdict.forecast, monitoring.forecast.verdict.median,
// monitoring.season.note.off, monitoring.season.note.later,
// monitoring.season.note.stats, monitoring.season.legend.gap, monitoring.season.range, monitoring.season.week,
// monitoring.season.sum.lead, monitoring.season.sum.lead_mean, monitoring.season.sum.prev,
// monitoring.season.sum.skipped, monitoring.season.metric.pv, monitoring.season.metric.load,
// monitoring.season.metric.export, monitoring.season.metric.base, monitoring.season.metric.temp
// i18n-keys (Tagesbild): monitoring.day.fact.days, monitoring.day.fact.ago, monitoring.day.fact.changed, monitoring.day.fact.learned, monitoring.day.fact.prev, monitoring.day.fact.prev_value, monitoring.day.fact.runs, monitoring.day.fact.sure, monitoring.day.fact.sure_no, monitoring.day.fact.sure_yes, monitoring.day.allday, monitoring.day.allday_short, monitoring.day.free, monitoring.day.free_short, monitoring.day.house, monitoring.day.night, monitoring.day.point, monitoring.day.read.night, monitoring.day.read.night_on, monitoring.day.read.none, monitoring.day.read.on, monitoring.day.state.active, monitoring.day.state.quiet, monitoring.day.unsure, monitoring.day.window, monitoring.run.power_for, monitoring.site.reserve, monitoring.site.reserve_est, monitoring.site.surplus_note, monitoring.site.to_forecast, monitoring.learned.short.pv_reserve_w, monitoring.learned.short.surplus_left_wh, status.uptime.hours_minutes, status.uptime.minutes
(() => {
  const EASE_OUT = 'cubic-bezier(0.23, 1, 0.32, 1)';
  const FORECAST_PATH = '/api/v1/monitoring/forecast';
  const METRIC_KEY = 'monitoring.season.metric';
  const DEFAULT_METRIC = 'pv';
  const FORECAST_ICONS = { learning: 'ico-history', median: 'ico-calendar', forecast: 'ico-learning' };
  const FORECAST_TONES = { learning: 'wait', median: 'warn', forecast: 'ok' };
  const STATUS_KEYS = {
    learning: { title: 'monitoring.forecast.status.learning.title', text: 'monitoring.forecast.status.learning.text' },
    median: { title: 'monitoring.forecast.status.median.title', text: 'monitoring.forecast.status.median.text' },
    forecast: { title: 'monitoring.forecast.status.forecast.title', text: 'monitoring.forecast.status.forecast.text' },
  };
  const METRIC_LABEL_KEYS = {
    pv: 'monitoring.season.metric.pv',
    load: 'monitoring.season.metric.load',
    export: 'monitoring.season.metric.export',
    base: 'monitoring.season.metric.base',
    temp: 'monitoring.season.metric.temp',
  };

  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const base = () => window.__DASHBOARD_BASE_PATH__ || '';
  const reduced = () => Boolean(window.matchMedia) && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  const narrow = () => Boolean(window.matchMedia) && window.matchMedia('(max-width: 640px)').matches;
  const num = (v, d) => (window.I18n && window.I18n.formatNumber ? window.I18n.formatNumber(v, d) : Number(v).toFixed(d));
  // Datum und Monatsnamen nur ueber I18n, damit Sprache und Zahlenformat der Oberflaeche gelten.
  // Ohne I18n bleibt ein schlichter Text (Jahr-Monat-Tag, Monatsnummer), kein eigenes Format.
  const fmtDate = (date, opts) => {
    if (window.I18n && window.I18n.formatDate) return window.I18n.formatDate(date, opts);
    const p2 = (v) => String(v).padStart(2, '0');
    return date.getFullYear() + '-' + p2(date.getMonth() + 1) + '-' + p2(date.getDate());
  };
  const metricList = () => window.MonitoringInsight.METRICS;

  // Monatsnamen kurz, aus der Sprache der Oberflaeche
  function monthLabels() {
    if (!(window.I18n && window.I18n.formatDate)) return Array.from({ length: 12 }, (_, m) => String(m + 1).padStart(2, '0'));
    return Array.from({ length: 12 }, (_, m) => fmtDate(new Date(2026, m, 15), { month: 'short' }));
  }

  // ISO-Woche: Montag der Woche w im Jahr y
  function weekMonday(y, w) {
    const jan4 = new Date(y, 0, 4);
    const mon = new Date(jan4);
    mon.setDate(jan4.getDate() - ((jan4.getDay() + 6) % 7) + (w - 1) * 7);
    return mon;
  }

  function reveal(el) {
    if (!el || !el.animate || reduced()) return;
    el.animate([{ clipPath: 'inset(0 100% 0 0)' }, { clipPath: 'inset(0 0 0 0)' }], { duration: 280, easing: EASE_OUT });
  }
  function crossfade(el) {
    if (!el || !el.animate) return;
    const frames = reduced()
      ? [{ opacity: 0.35 }, { opacity: 1 }]
      : [{ opacity: 0.35, transform: 'translateY(4px)' }, { opacity: 1, transform: 'translateY(0)' }];
    el.animate(frames, { duration: 160, easing: EASE_OUT });
  }
  function onFirstView(el, fn) {
    if (!el) return;
    if (!('IntersectionObserver' in window)) { fn(); return; }
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) { io.disconnect(); fn(); }
    }, { threshold: 0.4 });
    io.observe(el);
  }
  // Zeiger auf eine Spalte abbilden, mit Zeiger-Capture. touch-action: pan-y laesst
  // senkrechtes Scrollen beim Browser, dann kommt pointercancel und die Auswahl bleibt.
  function scrubber(el, pick) {
    let down = false;
    el.addEventListener('pointerdown', (e) => { down = true; try { el.setPointerCapture(e.pointerId); } catch (err) { /* egal */ } pick(e.clientX); });
    el.addEventListener('pointermove', (e) => { if (down || e.pointerType === 'mouse') pick(e.clientX); });
    const up = () => { down = false; };
    el.addEventListener('pointerup', up);
    el.addEventListener('pointercancel', up);
  }

  // Prognose: Tagesbalken gegen Messung, Lernphase, Median oder Prognose.
  function monitoringForecast() {
    return {
      data: null,
      failed: false,
      loaded: false,
      loading: false,
      sel: -1,
      width: 0,
      geo: null,
      get check() { return (this.data && this.data.forecast) || null; },
      get days() { return (this.check && this.check.days) || []; },
      get readers() { return (this.check && this.check.readers) || []; },
      get state() { return this.check ? window.MonitoringInsight.forecastState(this.check) : null; },
      get learning() { return Boolean(this.state) && this.state.kind === 'learning'; },
      get failedText() { return t('monitoring.forecast.load_failed'); },
      kwh(wh) { return num(wh / 1000, 1) + ' kWh'; },
      get status() {
        const s = this.state; if (!s) return null;
        const params = s.kind === 'learning'
          ? { needed: s.needed, left: s.needed - s.have }
          : { days: s.have, forecast: this.kwh(s.maeF), median: this.kwh(s.maeM) };
        return {
          kind: s.kind,
          icon: FORECAST_ICONS[s.kind],
          tone: FORECAST_TONES[s.kind],
          title: t(STATUS_KEYS[s.kind].title),
          text: t(STATUS_KEYS[s.kind].text, params),
        };
      },
      get bars() {
        const s = this.state; if (!s) return [];
        const max = Math.max(s.maeF, s.maeM) || 1;
        const winF = s.kind !== 'median';
        return [
          { id: 'f', label: t('monitoring.forecast.mae.forecast'), value: this.kwh(s.maeF), pct: s.maeF / max * 100, win: winF },
          { id: 'm', label: t('monitoring.forecast.mae.median'), value: this.kwh(s.maeM), pct: s.maeM / max * 100, win: !winF },
        ];
      },
      get caption() {
        const s = this.state; if (!s) return '';
        return t('monitoring.forecast.mae.caption', { hits: s.hits, days: s.have });
      },
      get useText() {
        const names = this.readers.map((r) => r.name || r.id);
        return names.length ? t('monitoring.forecast.use', { rules: names.join(', ') }) : t('monitoring.forecast.use_none');
      },
      get progress() {
        const s = this.state; if (!s) return '';
        return t('monitoring.forecast.progress', { have: s.have, needed: s.needed });
      },
      get selIndex() { return this.sel < 0 ? this.days.length - 1 : this.sel; },
      get selDay() { return this.days[this.selIndex]; },
      get readTitle() {
        const d = this.selDay; if (!d) return '';
        return fmtDate(new Date(d.day + 'T12:00:00'), { weekday: 'short', day: 'numeric', month: 'short' });
      },
      get readVerdict() {
        const d = this.selDay; if (!d) return '';
        const ef = Math.abs(d.forecast_wh - d.actual_wh);
        const em = Math.abs(d.season_wh - d.actual_wh);
        return t(ef < em ? 'monitoring.forecast.verdict.forecast' : 'monitoring.forecast.verdict.median',
          { forecast: this.kwh(ef), median: this.kwh(em) });
      },

      async load() {
        if (this.loaded || this.loading) return;
        this.loading = true;
        this.failed = false;
        try {
          const res = await fetch(base() + FORECAST_PATH, { credentials: 'same-origin', headers: { Accept: 'application/json' } });
          if (!res.ok) throw new Error('status ' + res.status);
          this.data = await res.json();
          this.loaded = true;
        } catch (e) {
          this.failed = true;
        } finally {
          this.loading = false;
        }
      },

      init() {
        const el = this.$refs.plot;
        if ('ResizeObserver' in window) {
          new ResizeObserver(() => { const w = el.clientWidth; if (w && w !== this.width) { this.width = w; this.draw(); } }).observe(el);
        }
        onFirstView(el, () => reveal(el.querySelector('.mi-cols')));
        this.$watch('data', () => { this.sel = -1; this.draw(); });
        scrubber(el, (x) => this.pickX(x));
      },
      draw() {
        const el = this.$refs && this.$refs.plot; if (!el) return;
        const s = this.state;
        if (!s) { el.innerHTML = ''; this.geo = null; return; }
        const chart = window.MonitoringInsight.forecastChart(this.days, s.needed, this.width || 600, narrow());
        el.innerHTML = chart.svg;
        this.geo = chart.geo;
        this.mark();
      },
      // Ohne Wahl tritt nichts zurueck, die Lesezeile zeigt den letzten Tag
      mark() {
        const el = this.$refs && this.$refs.plot; const svg = el && el.firstChild;
        if (!svg || !svg.querySelectorAll) return;
        svg.querySelectorAll('.mi-col').forEach((c) => c.classList.toggle('is-dim', this.sel >= 0 && Number(c.dataset.i) !== this.selIndex));
      },
      pickX(clientX) {
        if (!this.geo) return;
        const r = this.$refs.plot.getBoundingClientRect();
        this.setDay(Math.floor((clientX - r.left - this.geo.pad.l) / this.geo.cw));
      },
      setDay(i) {
        if (!this.days.length) return;
        i = Math.max(0, Math.min(this.days.length - 1, i));
        if (i === this.selIndex) return;
        this.sel = i;
        this.mark();
      },
      key(e) {
        const map = { ArrowLeft: -1, ArrowRight: 1 };
        if (map[e.key]) { e.preventDefault(); this.setDay(this.selIndex + map[e.key]); }
      },
    };
  }

  // Jahresvergleich: Kennzahl, Kopfzahl, Lesezeile und Diagramm. getSeason liefert data.season.
  function monitoringYear(getSeason) {
    const storedMetric = () => {
      try {
        const v = window.localStorage.getItem(METRIC_KEY);
        return metricList().some((m) => m.id === v) ? v : DEFAULT_METRIC;
      } catch (e) {
        return DEFAULT_METRIC;
      }
    };
    return {
      metric: storedMetric(),
      sel: null,
      width: 0,
      geo: null,
      get season() { return (typeof getSeason === 'function' && getSeason()) || null; },
      get metrics() { return metricList().map((m) => ({ id: m.id, label: t(METRIC_LABEL_KEYS[m.id]) })); },
      get m() { return metricList().find((x) => x.id === this.metric) || metricList()[0]; },
      get years() {
        const s = this.season; if (!s) return [];
        const old = s.year - 2;
        const three = (s.weeks || []).some((w) => String(w.key).startsWith(old + '-W'));
        return three ? [s.year, s.year - 1, s.year - 2] : [s.year, s.year - 1];
      },
      // Mindestens eine Woche der gezeigten Jahre stammt aus der Energiestatistik
      get hasStats() {
        const s = this.season; if (!s) return false;
        return this.years.some((yr) => window.MonitoringInsight.seasonSeries(s.weeks, yr, this.metric).some((p) => p && p.src === 'stats'));
      },
      // Im aktuellen Jahr fehlt mindestens eine Woche zwischen KW 1 und der aktuellen Woche
      get hasGaps() {
        const s = this.season; if (!s) return false;
        const cur = window.MonitoringInsight.seasonSeries(s.weeks, s.year, this.metric);
        for (let w = 1; w <= s.week; w++) if (!cur[w]) return true;
        return false;
      },
      get selWeek() { return this.sel != null ? this.sel : (this.season ? this.season.week : 1); },
      get empty() { return this.summary === null; },
      fmt(v) {
        const m = this.m;
        return m.sum && v >= 1000 ? num(v / 1000, 2) + ' MWh' : num(v, m.d) + ' ' + m.unit;
      },
      // Temperatur als Gradunterschied, sonst als Prozent
      diffText(v) {
        const sign = v >= 0 ? '+' : '−';
        return this.m.id === 'temp' ? sign + num(Math.abs(v), 1) + ' °C' : sign + num(Math.abs(v), 0) + ' %';
      },
      diffPair(a, b) {
        if (this.m.id === 'temp') return this.diffText(a - b);
        return b ? this.diffText((a - b) / b * 100) : '';
      },
      tone(a, b) {
        const g = this.m.good * Math.sign(a - b);
        return g > 0 ? 'is-up' : g < 0 ? 'is-down' : '';
      },
      get summary() {
        const s = this.season; if (!s) return null;
        const r = window.MonitoringInsight.seasonSummary(s, this.metric);
        if (!r) return null;
        return {
          lead: t(this.m.sum ? 'monitoring.season.sum.lead' : 'monitoring.season.sum.lead_mean', { week: s.week }),
          cur: this.fmt(r.cur),
          prev: t('monitoring.season.sum.prev', { value: this.fmt(r.prev) }),
          diff: this.diffText(r.diff),
          tone: r.tone,
          skipped: r.skipped > 0 ? t('monitoring.season.sum.skipped', { n: r.skipped }) : '',
        };
      },
      get readTitle() { return t('monitoring.season.week', { week: this.selWeek }); },
      get readRange() {
        const s = this.season; if (!s) return '';
        const a = weekMonday(s.year, this.selWeek);
        const b = new Date(a); b.setDate(a.getDate() + 6);
        const opts = { day: 'numeric', month: 'short' };
        return t('monitoring.season.range', { from: fmtDate(a, opts), to: fmtDate(b, opts) });
      },
      get readRows() {
        const s = this.season; if (!s) return [];
        const ys = this.years; const cur = this.selWeek;
        const series = (yr) => window.MonitoringInsight.seasonSeries(s.weeks, yr, this.metric);
        const base = series(ys[1])[cur];
        return ys.map((yr, i) => {
          const p = series(yr)[cur];
          let note = '';
          if (!p && yr === s.year && cur < s.week) note = t('monitoring.season.note.off');
          else if (!p && yr === s.year) note = t('monitoring.season.note.later');
          else if (p && p.src === 'stats') note = t('monitoring.season.note.stats');
          const compare = i === 0 && p && base;
          return {
            yr,
            i,
            value: p ? this.fmt(p.v) : '',
            note,
            diff: compare ? this.diffPair(p.v, base.v) : '',
            tone: compare ? this.tone(p.v, base.v) : '',
          };
        });
      },

      setMetric(id) {
        if (id === this.metric || !metricList().some((m) => m.id === id)) return;
        this.metric = id;
        try { window.localStorage.setItem(METRIC_KEY, id); } catch (e) { /* ohne Speicher */ }
        this.draw(true);
      },
      init() {
        const el = this.$refs.plot;
        if ('ResizeObserver' in window) {
          new ResizeObserver(() => { const w = el.clientWidth; if (w && w !== this.width) { this.width = w; this.draw(false); } }).observe(el);
        }
        onFirstView(el, () => reveal(el.querySelector('.mi-cur')));
        this.$watch('season', () => this.draw(false));
        scrubber(el, (x) => this.pickX(x));
      },
      draw(fade) {
        const el = this.$refs && this.$refs.plot; if (!el) return;
        const s = this.season;
        if (!s || !this.years.length) { el.innerHTML = ''; this.geo = null; return; }
        const chart = window.MonitoringInsight.seasonChart(s, this.metric, this.years.length, this.width || 600, narrow(), monthLabels());
        el.innerHTML = chart.svg;
        this.geo = chart.geo;
        this.moveCursor();
        if (fade) crossfade(el.firstChild);
      },
      moveCursor() {
        const g = this.geo; const el = this.$refs && this.$refs.plot; const svg = el && el.firstChild; const s = this.season;
        if (!g || !svg || !s || !svg.querySelector) return;
        const w = this.selWeek;
        const X = g.x(w).toFixed(1);
        const hair = svg.querySelector('.mi-hair'); hair.setAttribute('x1', X); hair.setAttribute('x2', X);
        this.years.forEach((yr, i) => {
          const p = window.MonitoringInsight.seasonSeries(s.weeks, yr, this.metric)[w];
          const dot = svg.querySelector('.mi-dot-' + i);
          if (!p) { dot.setAttribute('visibility', 'hidden'); return; }
          dot.setAttribute('visibility', 'visible');
          dot.setAttribute('cx', X);
          dot.setAttribute('cy', g.y(p.v).toFixed(1));
        });
      },
      pickX(clientX) {
        if (!this.geo) return;
        const r = this.$refs.plot.getBoundingClientRect();
        const g = this.geo;
        this.setWeek(Math.round((clientX - r.left - g.pad.l) / (g.W - g.pad.l - g.pad.r) * 51) + 1);
      },
      setWeek(w) {
        const s = this.season; if (!s) return;
        w = Math.max(1, Math.min(52, w));
        if (w === this.selWeek) return;
        this.sel = w;
        this.moveCursor();
      },
      key(e) {
        const s = this.season; if (!s) return;
        const map = { ArrowLeft: -1, ArrowRight: 1 };
        if (map[e.key]) { e.preventDefault(); this.setWeek(this.selWeek + map[e.key]); }
        if (e.key === 'Home') { e.preventDefault(); this.setWeek(1); }
        if (e.key === 'End') { e.preventDefault(); this.setWeek(s.week); }
      },
    };
  }

  // Ein ueblicher Tag: Zeilen aus dem Modell, Lesezeile und Anlagewerte. getValues liefert learned.values,
  // getFocus liefert learnedFocus (Schluessel aus dem Lernwert-Reiter, z. B. device/<id>/usual_on).
  const pad2 = (v) => String(v).padStart(2, '0');
  const hhmm = (m) => pad2(Math.floor(m / 60)) + ':' + pad2(m % 60);
  const DAY_SECONDS = 86400;
  const LONG_RUN_SECONDS = 20 * 3600;
  // Dauer aus Sekunden, Einheiten aus dem Katalog (status.uptime)
  function durText(seconds) {
    const min = Math.round(seconds / 60);
    if (min >= 60) return t('status.uptime.hours_minutes', { hours: num(Math.floor(min / 60), 0), minutes: num(min % 60, 0) });
    return t('status.uptime.minutes', { minutes: num(min, 0) });
  }
  // Zeile zu einem Fokus: device/<id>/... oeffnet <id>, die Anlagewerte oeffnen das Haus
  const SITE_HOUSE_KEYS = ['site/night_rest', 'site/day_start', 'site/house_state'];
  function rowForFocus(key) {
    const parts = String(key || '').split('/');
    if (parts[0] === 'device' && parts[1]) return parts[1];
    return SITE_HOUSE_KEYS.includes(key) ? 'house' : '';
  }
  // Alter eines Lernwerts: Tage als Zahl, sonst das Datum der Aenderung
  function agoText(changedAt) {
    const days = Math.floor((Date.now() / 1000 - changedAt) / DAY_SECONDS);
    const age = days >= 1
      ? t('monitoring.day.fact.days', { n: num(days, 0) })
      : fmtDate(new Date(changedAt * 1000), { day: 'numeric', month: 'short' });
    return t('monitoring.day.fact.ago', { age });
  }
  const valueText = (entry, v) => (entry.kind === 'duration' ? durText(Number(v)) : String(v));

  function factsOf(row) {
    const out = [];
    const entries = row.entries || [];
    const n = row.n || 0;
    if (n) {
      const runs = entries.some((e) => e.kind === 'duration');
      out.push([t('monitoring.day.fact.learned'), t(runs ? 'monitoring.day.fact.runs' : 'monitoring.day.fact.days', { n: num(n, 0) })]);
    }
    out.push([t('monitoring.day.fact.sure'), t(row.sure ? 'monitoring.day.fact.sure_yes' : 'monitoring.day.fact.sure_no')]);
    const changed = Math.max(0, ...entries.map((e) => e.changed_at || 0));
    if (changed) out.push([t('monitoring.day.fact.changed'), agoText(changed)]);
    entries.filter((e) => e.prev != null).forEach((e) => {
      out.push([t('monitoring.day.fact.prev'), t('monitoring.day.fact.prev_value', { prev: valueText(e, e.prev), value: valueText(e, e.value) })]);
    });
    return out;
  }

  // Zeile aus dem Modell mit Anzeigetexten, dazu der Jahreslauf als SVG (yr) wo es ein Fenster gibt
  function decorateRow(r, band) {
    const out = { id: r.id, kind: r.kind, name: r.name || t(r.nameKey), icon: r.icon, segs: r.segs, sure: r.sure,
      minutes: r.minutes, label: '', short: '', side: '', facts: factsOf(r), yr: null };
    const duration = r.minutes != null ? durText(r.minutes * 60) : '';
    if (r.kind === 'night') {
      out.label = t('monitoring.day.night', { from: r.from, to: r.to });
      out.side = duration;
    } else if (r.kind === 'on') {
      out.label = t('monitoring.day.window', { from: r.from, to: r.to });
      out.side = duration;
    } else if (r.kind === 'point') {
      out.label = t('monitoring.day.point', { time: r.from || r.to });
    } else if (r.kind === 'free' && r.id !== 'house') {
      const long = r.minutes * 60 > LONG_RUN_SECONDS;
      out.label = t(long ? 'monitoring.day.allday' : 'monitoring.day.free', { duration });
      out.short = long ? t('monitoring.day.allday_short') : t('monitoring.day.free_short', { duration });
    } else {
      out.label = t('monitoring.day.unsure');
    }
    if (r.year) out.yr = band(r.year[0], r.year[1], r.kind === 'night');
    return out;
  }

  function monitoringDay(getValues, getFocus) {
    const values = () => (typeof getValues === 'function' && getValues()) || [];
    const focusKey = () => (typeof getFocus === 'function' && getFocus()) || '';
    const compare = () => (window.I18n && window.I18n.compare) || undefined;
    const find = (key) => values().find((v) => v.key === key);
    return {
      open: '',
      t: null,
      moved: false,
      revealed: false,
      get nowMin() { const d = new Date(); return d.getHours() * 60 + d.getMinutes(); },
      get at() { return this.t == null ? this.nowMin : this.t; },
      get readTime() { return hhmm(this.at); },
      get rows() {
        const M = window.MonitoringInsight;
        return M.dayRows(values(), compare()).map((r) => decorateRow(r, M.yearBand));
      },
      get house() {
        const nr = find('site/night_rest'); const st = find('site/house_state');
        const active = Boolean(st) && st.value === 'active';
        return {
          n: nr ? nr.n || 0 : 0,
          state: active ? 'active' : 'quiet',
          stateText: t(active ? 'monitoring.day.state.active' : 'monitoring.day.state.quiet'),
        };
      },
      get runs() {
        return window.MonitoringInsight.runRows(values(), compare()).map((r) => {
          const power = r.powerW ? num(r.powerW / 1000, 1) + ' kW' : '';
          const time = r.seconds ? durText(r.seconds) : '';
          const sub = power && time ? t('monitoring.run.power_for', { power, duration: time }) : (power || time);
          const value = r.wh ? num(r.wh / 1000, r.wh < 1000 ? 2 : 1) + ' kWh' : '';
          return { ...r, value, sub };
        });
      },
      get site() {
        const reserve = find('site/pv_reserve_w'); const left = find('site/surplus_left_wh');
        const tiles = [];
        if (reserve) tiles.push({ id: 'r', label: t('monitoring.learned.short.pv_reserve_w'), value: num(reserve.value, 0) + ' W',
          note: t(reserve.estimate ? 'monitoring.site.reserve_est' : 'monitoring.site.reserve'), icon: 'ico-gauge' });
        if (left) tiles.push({ id: 's', label: t('monitoring.learned.short.surplus_left_wh'), value: num(left.value / 1000, 1) + ' kWh',
          note: t('monitoring.site.surplus_note'), icon: 'ico-flash', link: true });
        return tiles;
      },
      get readText() {
        const o = window.MonitoringInsight.onAt(this.rows, this.at);
        if (o.on.length) return t(o.night ? 'monitoring.day.read.night_on' : 'monitoring.day.read.on', { names: o.on.join(', ') });
        return t(o.night ? 'monitoring.day.read.night' : 'monitoring.day.read.none');
      },

      // Ein Tipp ohne Bewegung klappt auf, nach einem Wisch ist der naechste Klick kein Tipp
      toggle(id) {
        if (this.moved) { this.moved = false; return; }
        this.open = this.open === id ? '' : id;
      },
      // Fokus aus dem Lernwert-Reiter: Zeile oeffnen und zur Zeile scrollen
      follow() {
        const id = rowForFocus(focusKey());
        if (!id) return;
        this.open = id;
        const go = () => {
          const rows = this.$refs && this.$refs.rows;
          const el = rows && rows.querySelector('[data-row="' + id + '"]');
          if (el && el.scrollIntoView) el.scrollIntoView({ block: 'nearest', behavior: reduced() ? 'auto' : 'smooth' });
        };
        if (this.$nextTick) this.$nextTick(go); else go();
      },
      // Balken einmal je Seitenaufruf aufziehen
      reveal() {
        if (this.revealed) return;
        this.revealed = true;
        const rows = this.$refs && this.$refs.rows; if (!rows) return;
        rows.querySelectorAll('.mi-track').forEach((tr) => reveal(tr));
      },
      pick(clientX) {
        const rows = this.$refs && this.$refs.rows;
        const tr = rows && rows.querySelector('.mi-track'); if (!tr) return;
        const r = tr.getBoundingClientRect();
        const f = Math.max(0, Math.min(0.9999, (clientX - r.left) / r.width));
        this.t = Math.round(f * 1440 / 15) * 15 % 1440;
        this.place();
      },
      place() {
        const rows = this.$refs && this.$refs.rows; const hair = this.$refs && this.$refs.hair; const nowl = this.$refs && this.$refs.now;
        const tr = rows && rows.querySelector('.mi-track');
        if (!tr || !hair || !nowl) return;
        const box = rows.getBoundingClientRect(); const r = tr.getBoundingClientRect();
        hair.style.transform = 'translateX(' + (r.left - box.left + this.at / 1440 * r.width).toFixed(1) + 'px)';
        nowl.style.transform = 'translateX(' + (r.left - box.left + this.nowMin / 1440 * r.width).toFixed(1) + 'px)';
      },
      key(e) {
        const map = { ArrowLeft: -15, ArrowRight: 15 };
        if (map[e.key]) { e.preventDefault(); this.t = (this.at + map[e.key] + 1440) % 1440; this.place(); }
      },
      init() {
        const el = this.$refs.rows;
        onFirstView(el, () => this.reveal());
        this.$watch(() => focusKey(), () => this.follow());
        this.follow();
        let down = false; let x0 = 0;
        el.addEventListener('pointerdown', (e) => { down = true; x0 = e.clientX; this.moved = false; this.pick(e.clientX); });
        el.addEventListener('pointermove', (e) => {
          if (down && !this.moved && Math.abs(e.clientX - x0) > 6) {
            this.moved = true;
            try { el.setPointerCapture(e.pointerId); } catch (err) { /* egal */ }
          }
          if (down || e.pointerType === 'mouse') this.pick(e.clientX);
        });
        const up = () => { down = false; };
        el.addEventListener('pointerup', up);
        el.addEventListener('pointercancel', up);
        if ('ResizeObserver' in window) new ResizeObserver(() => this.place()).observe(el);
        if (this.$nextTick) this.$nextTick(() => this.place()); else this.place();
      },
    };
  }

  const register = () => {
    if (!window.Alpine) return;
    window.Alpine.data('monitoringForecast', monitoringForecast);
    window.Alpine.data('monitoringYear', monitoringYear);
    window.Alpine.data('monitoringDay', monitoringDay);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, { once: true });
})();

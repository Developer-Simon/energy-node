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
  const locale = () => (window.I18n && window.I18n.locale) || undefined;
  const fmtDate = (date, opts) => (window.I18n && window.I18n.formatDate
    ? window.I18n.formatDate(date, opts)
    : date.toLocaleDateString(locale(), opts));
  const metricList = () => window.MonitoringInsight.METRICS;

  // Monatsnamen kurz, aus der Sprache der Oberflaeche
  function monthLabels() {
    const f = new Intl.DateTimeFormat(locale(), { month: 'short' });
    return Array.from({ length: 12 }, (_, m) => f.format(new Date(2026, m, 15)));
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

  const register = () => {
    if (!window.Alpine) return;
    window.Alpine.data('monitoringForecast', monitoringForecast);
    window.Alpine.data('monitoringYear', monitoringYear);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, { once: true });
})();

// Seite "Statistik": Energie je Rolle fuer einen Tag, eine Woche, einen
// Monat oder ein Jahr. Gestapelte Balken, Erzeugung oben, Verbrauch
// gespiegelt unten. Zurueck und Vor kommen als prev/next vom Server, der
// Browser rechnet kein Datum.
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const PAGE_KPIS = ['consumption', 'pv', 'grid_import', 'grid_export', 'battery_charge', 'battery_discharge', 'autarky', 'self_consumption'];

  // Wie HistoryChart.themeMode, ohne history-chart.js zu laden.
  function themeMode() {
    const attr = (document.documentElement.dataset.theme || '').toLowerCase();
    if (attr === 'light' || attr === 'dark') return attr;
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  }

  // ApexCharts blendet sich überlappende Beschriftungen bei Balken mit
  // rotate 0 nicht aus. Passen sie nicht in die Zeichenfläche, bekommt nur
  // jede n-te Kategorie ihren Text. Breite 0 heißt unbekannt (jsdom, noch
  // nicht gelayoutet), dann bleiben alle.
  const Y_AXIS_PX = 70;
  function thinLabels(labels, width) {
    if (!width || !labels.length) return labels;
    const longest = Math.max(...labels.map(label => String(label).length));
    const labelPx = longest * 8 + 12;
    const step = Math.max(1, Math.ceil(labels.length * labelPx / Math.max(width - Y_AXIS_PX, 1)));
    return labels.map((label, index) => (index % step === 0 ? label : ''));
  }

  function chartOptions(result, width = 0) {
    const model = window.EnergyStatsModel;
    const theme = window.DashboardTheme;
    const series = model.chartSeries(result);
    const labels = result.buckets.map(bucket => model.bucketLabel(result.period, bucket.local));
    // Ein Array: ApexCharts faerbt bei einem einzelnen String nur die ersten Marken.
    const labelColor = theme.color('text-muted');
    const labelStyle = {colors: Array(24).fill(labelColor), fontSize: '12px'};
    return {
      chart: {type: 'bar', stacked: true, height: 340, toolbar: {show: false}, animations: {enabled: false}, background: 'transparent', fontFamily: 'inherit'},
      theme: {mode: themeMode()},
      series: series.map(s => ({name: s.name, data: s.data, color: s.color, role: s.role})),
      colors: series.map(s => s.color),
      fill: {
        type: series.map(s => (s.hatched ? 'pattern' : 'solid')),
        opacity: 1,
        pattern: {style: 'slantedLines', width: 6, height: 6, strokeWidth: 2},
      },
      stroke: {width: 0},
      plotOptions: {bar: {columnWidth: '72%', borderRadius: 2}},
      dataLabels: {enabled: false},
      legend: {
        position: 'bottom', fontSize: '12px', labels: {colors: theme.color('text-muted')}, itemMargin: {horizontal: 10},
        markers: {
          size: 5, shape: 'circle',
          customHTML: series.map(s => () => `<span class="stats-swatch${s.hatched ? ' is-hatched' : ''}" style="background-color:${s.color}"></span>`),
        },
      },
      xaxis: {
        categories: thinLabels(labels, width),
        labels: {style: labelStyle, rotate: 0, hideOverlappingLabels: true},
        axisBorder: {color: theme.color('border-soft')},
        axisTicks: {color: theme.color('border-soft')},
      },
      yaxis: {forceNiceScale: true, labels: {style: labelStyle, showDuplicates: true, formatter: value => window.I18n.formatNumber(Math.abs(value), Math.abs(value) < 10 ? 1 : 0)}, title: {text: 'kWh', style: {color: theme.color('text-muted'), fontWeight: 400}}},
      tooltip: {
        x: {formatter: (value, opts) => (opts && labels[opts.dataPointIndex]) || value},
        y: {formatter: value => model.formatKWh(value == null ? null : Math.abs(value))},
      },
      grid: {borderColor: theme.color('border-soft'), strokeDashArray: 3},
      noData: {text: t('energy_stats.empty')},
    };
  }

  const statsPanel = () => ({
    period: 'day',
    at: '',
    data: null,
    loading: false,
    error: '',
    title: '',
    kpis: [],
    coverage: '',
    coverageParts: [],
    rows: [],
    labels: [],
    empty: false,
    chart: null,
    seq: 0,
    // Die zuletzt im Ereignis gesehene Revision. Jedes Registry-Ereignis
    // traegt den Zweig energy_stats, auch im Leerlauf.
    seenRevision: null,

    get hasPrev() { return Boolean(this.data && this.data.prev); },
    get hasNext() { return Boolean(this.data && this.data.next); },

    init() {
      window.addEventListener('energy-stats-focus', event => this.focus(event.detail || {}));
      this.themeOff = window.DashboardTheme.onChange(() => this.renderChart());
      this.revisionListener = event => this.onRevision(event.detail && event.detail.energy_stats);
      window.addEventListener('registry-updated', this.revisionListener);
      // Die Kachel kann die Seite oeffnen, bevor dieses Skript geladen ist.
      const request = window.__statsFocusRequest__;
      delete window.__statsFocusRequest__;
      if (request) return this.focus(request);
      return this.load();
    },

    focus({period, at}) {
      this.period = window.EnergyStatsModel.PERIODS.includes(period) ? period : 'day';
      this.at = at || '';
      return this.load();
    },

    // Laedt bei neuer Revision nur den laufenden Zeitraum neu (next leer).
    // Ein vergangener Zeitraum bleibt stehen, auch wenn der Companion dafuer
    // Stunden nachliefert. Er wird beim naechsten Wechsel frisch geladen.
    onRevision(branch) {
      const revision = branch && branch.rev;
      if (typeof revision !== 'number' || revision === this.seenRevision) return Promise.resolve();
      this.seenRevision = revision;
      if (!this.data || this.data.next || this.data.revision === revision) return Promise.resolve();
      return this.load();
    },

    async load() {
      const model = window.EnergyStatsModel;
      const seq = ++this.seq;
      this.loading = true;
      try {
        const data = await model.fetchStats(this.period, this.at);
        if (seq !== this.seq) return;
        this.data = data;
        this.error = '';
        this.title = model.periodTitle(data);
        this.kpis = model.kpis(data.total, PAGE_KPIS);
        this.coverage = model.coverageText(data.total && data.total.hours);
        this.coverageParts = model.coverageParts(data.total && data.total.hours);
        this.rows = model.tableRows(data);
        this.labels = data.buckets.map(bucket => model.bucketLabel(data.period, bucket.local));
        const hours = (data.total && data.total.hours) || {};
        this.empty = !hours.ha && !hours.own;
        this.renderChart();
      } catch (error) {
        if (seq === this.seq) this.error = error.message;
      } finally {
        if (seq === this.seq) this.loading = false;
      }
    },

    selectPeriod(period) {
      this.period = period;
      this.at = '';
      return this.load();
    },

    prev() {
      if (!this.hasPrev) return Promise.resolve();
      this.at = this.data.prev;
      return this.load();
    },

    next() {
      if (!this.hasNext) return Promise.resolve();
      this.at = this.data.next;
      return this.load();
    },

    renderChart() {
      if (!this.data || !this.$refs.chart || !window.ApexCharts) return;
      if (this.chart) this.chart.destroy();
      this.chart = new window.ApexCharts(this.$refs.chart, chartOptions(this.data, this.$refs.chart.clientWidth));
      this.chart.render();
    },

    destroy() {
      if (this.chart) this.chart.destroy();
      if (this.themeOff) this.themeOff();
      if (this.revisionListener) window.removeEventListener('registry-updated', this.revisionListener);
    },
  });

  statsPanel.chartOptions = chartOptions;
  statsPanel.thinLabels = thinLabels;
  statsPanel.PAGE_KPIS = PAGE_KPIS;

  const register = () => {
    if (window.Alpine) window.Alpine.data('statsPanel', statsPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();

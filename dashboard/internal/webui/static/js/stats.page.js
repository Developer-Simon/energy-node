// Seite "Statistik": Energie je Rolle fuer einen Tag, eine Woche, einen
// Monat oder ein Jahr. Gestapelte Balken, Erzeugung oben, Verbrauch
// gespiegelt unten. Zurueck und Vor kommen als prev/next vom Server, der
// Browser rechnet kein Datum.
(() => {
  const PAGE_KPIS = ['consumption', 'pv', 'grid_import', 'grid_export', 'battery_charge', 'battery_discharge', 'autarky', 'self_consumption'];

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
      // Unsichtbar (anderer Reiter) ist clientWidth 0, dann gibt die
      // Fensterbreite die Größenordnung vor.
      const width = this.$refs.chart.clientWidth || window.innerWidth || 0;
      this.chart = new window.ApexCharts(this.$refs.chart, window.EnergyStatsModel.chartOptions(this.data, width));
      this.chart.render();
    },

    destroy() {
      if (this.chart) this.chart.destroy();
      if (this.themeOff) this.themeOff();
      if (this.revisionListener) window.removeEventListener('registry-updated', this.revisionListener);
    },
  });

  statsPanel.chartOptions = (...args) => window.EnergyStatsModel.chartOptions(...args);
  statsPanel.thinLabels = (...args) => window.EnergyStatsModel.thinLabels(...args);
  statsPanel.PAGE_KPIS = PAGE_KPIS;

  const register = () => {
    if (window.Alpine) window.Alpine.data('statsPanel', statsPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();

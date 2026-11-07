// Seite "Statistik": Energie je Rolle fuer einen Tag, eine Woche, einen
// Monat oder ein Jahr. Gestapelte Balken, Erzeugung oben, Verbrauch
// gespiegelt unten. Zurueck und Vor kommen als prev/next vom Server, der
// Browser rechnet kein Datum.
(() => {
  const PAGE_KPIS = ['consumption', 'pv', 'grid_import', 'grid_export', 'battery_charge', 'battery_discharge', 'autarky', 'self_consumption'];
  // Unter 640px zeigt die Tabelle nur einen Abschnitt und die Summe
  // ('bucket') oder das volle Raster ohne Rollennamen ('grid').
  const VIEW_KEY = 'stats.table_view';
  const NARROW = '(max-width: 640px)';

  const readView = () => {
    try { return window.localStorage.getItem(VIEW_KEY) === 'grid' ? 'grid' : 'bucket'; } catch (error) { return 'bucket'; }
  };

  // Schalter "Verluste einblenden", im Browser gemerkt. Ohne Speicher gilt
  // er nur fuer diese Sitzung und ist aus.
  const LOSSES_KEY = 'stats.show_losses';
  const readLosses = () => {
    try { return window.localStorage.getItem(LOSSES_KEY) === 'true'; } catch (error) { return false; }
  };

  // i18n-keys: energy_stats.losses.failed
  const t = key => (window.I18n ? window.I18n.t(key) : key);

  // Rasterzelle auf schmalen Schirmen: nur die Zahl in kWh, die Einheit
  // steht einmal im Kopf. Ohne MWh-Wechsel, damit die Spalten vergleichbar
  // bleiben.
  const compactKWh = kwh => {
    if (kwh == null || !Number.isFinite(kwh)) return '';
    return window.I18n.formatNumber(kwh, Math.abs(kwh) < 10 ? 1 : 0);
  };

  // Laufender Zeitraum: der juengste Abschnitt mit Werten, also der gerade
  // laufende. Der Browser rechnet dafuer kein Datum.
  const latestIndex = buckets => {
    for (let i = buckets.length - 1; i >= 0; i--) {
      if (buckets[i].kwh && Object.keys(buckets[i].kwh).length) return i;
    }
    return Math.max(buckets.length - 1, 0);
  };

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
    selected: 0,
    view: readView(),
    narrow: false,
    chart: null,
    seq: 0,
    // Verluste: Schalter, ob die Kette sichtbar ist (hasChains kommt erst
    // mit der ersten Antwort), Fehlertext und die Ketten der letzten Antwort.
    showLosses: readLosses(),
    hasChains: false,
    lossError: '',
    chainStats: null,
    // Die zuletzt im Ereignis gesehene Revision. Jedes Registry-Ereignis
    // traegt den Zweig energy_stats, auch im Leerlauf.
    seenRevision: null,

    get hasPrev() { return Boolean(this.data && this.data.prev); },
    get hasNext() { return Boolean(this.data && this.data.next); },
    get selectedLabel() { return this.labels[this.selected] || ''; },
    get showBucket() { return this.narrow && this.view === 'bucket'; },

    init() {
      window.addEventListener('energy-stats-focus', event => this.focus(event.detail || {}));
      this.themeOff = window.DashboardTheme.onChange(() => this.renderChart());
      this.revisionListener = event => this.onRevision(event.detail && event.detail.energy_stats);
      window.addEventListener('registry-updated', this.revisionListener);
      if (window.matchMedia) {
        this.narrowQuery = window.matchMedia(NARROW);
        this.narrow = this.narrowQuery.matches;
        this.narrowListener = event => { this.narrow = event.matches; this.placeBand(); };
        this.narrowQuery.addEventListener('change', this.narrowListener);
      }
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

    // Verluste fuer denselben Zeitraum wie die Statistik. Scheitert die
    // Anfrage, bleibt die Statistik stehen, nur der Fehlertext kommt dazu.
    async fetchLosses(period, at) {
      try {
        return {chains: await window.EnergyStatsModel.fetchChainStats(period, at)};
      } catch (error) {
        return {failed: true};
      }
    },

    setShowLosses(show) {
      this.showLosses = Boolean(show);
      try { window.localStorage.setItem(LOSSES_KEY, this.showLosses ? 'true' : 'false'); } catch (error) { /* gilt dann nur fuer diesen Aufruf */ }
      return this.load();
    },

    async load() {
      const model = window.EnergyStatsModel;
      const seq = ++this.seq;
      // Zeitraum und Datum gelten fuer beide Anfragen, auch wenn der Nutzer
      // waehrenddessen blaettert. Die Antwort einer alten Runde wird dann
      // ueber seq verworfen.
      const {period, at} = this;
      this.loading = true;
      try {
        let data;
        let losses = null;
        if (this.showLosses && this.hasChains) {
          [data, losses] = await Promise.all([model.fetchStats(period, at), this.fetchLosses(period, at)]);
        } else {
          data = await model.fetchStats(period, at);
        }
        if (seq !== this.seq) return;
        // has_chains kommt erst mit dieser Antwort. Beim ersten Laden holt
        // der Verlustabruf deshalb erst danach.
        this.hasChains = Boolean(data.has_chains);
        if (this.showLosses && this.hasChains && !losses) {
          losses = await this.fetchLosses(period, at);
          if (seq !== this.seq) return;
        }
        const losing = this.showLosses && this.hasChains;
        const chains = losing && losses && losses.chains ? losses.chains : null;
        const chainList = (chains && chains.chains) || [];
        // Verlustzeilen brauchen dieselben Abschnitte wie die Statistik. Bei
        // einer anderen Anzahl (Sommerzeitwechsel) fehlen sie in der Tabelle.
        const aligned = chainList.every(chain => chain.buckets.length === data.buckets.length);
        this.lossError = losing && losses && losses.failed ? t('energy_stats.losses.failed') : '';
        this.chainStats = chains;
        // Beim Blaettern im selben Zeitraum bleibt der Abschnitt stehen, so
        // laesst sich dieselbe Stunde ueber mehrere Tage vergleichen.
        const samePeriod = this.data && this.data.period === data.period;
        const last = Math.max(data.buckets.length - 1, 0);
        if (samePeriod) this.selected = Math.min(this.selected, last);
        else this.selected = data.next ? 0 : latestIndex(data.buckets);
        this.data = data;
        this.error = '';
        this.title = model.periodTitle(data);
        this.kpis = model.kpis(data.total, PAGE_KPIS);
        if (chains) this.kpis = [...this.kpis, ...model.lossKpis(chains)];
        this.coverage = model.coverageText(data.total && data.total.hours);
        this.coverageParts = model.coverageParts(data.total && data.total.hours);
        const roleRows = model.tableRows(data).map(row => ({
          ...row,
          compact: data.buckets.map(bucket => compactKWh(bucket.kwh && bucket.kwh[row.role])),
        }));
        // Die Rasterzelle eines Verlusts zeigt die Zahl in kWh, der
        // Wirkungsgrad bleibt als Prozent wie in der Tabelle.
        const lossCompact = new Map(chainList.map(chain => [
          `chain:${chain.id}:loss`, chain.buckets.map(cell => (cell.has_data ? compactKWh(cell.loss_kwh) : '')),
        ]));
        const lossRows = chains && aligned
          ? model.lossRows(chains).map(row => ({...row, compact: lossCompact.get(row.role) || row.values}))
          : [];
        this.rows = [...roleRows, ...lossRows];
        this.labels = data.buckets.map(bucket => model.bucketLabel(data.period, bucket.local));
        const hours = (data.total && data.total.hours) || {};
        this.empty = !hours.ha && !hours.own;
        this.renderChart();
        this.$nextTick(() => this.revealChip());
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

    selectBucket(index) {
      if (!this.data || !Number.isInteger(index) || index < 0 || index >= this.data.buckets.length) return;
      this.selected = index;
      this.placeBand();
      this.$nextTick(() => this.revealChip());
    },

    setView(view) {
      this.view = view === 'grid' ? 'grid' : 'bucket';
      try { window.localStorage.setItem(VIEW_KEY, this.view); } catch (error) { /* gilt dann nur fuer diesen Aufruf */ }
      this.placeBand();
      if (this.view === 'bucket') this.$nextTick(() => this.revealChip());
    },

    // Holt den gewaehlten Abschnitts-Chip in die Mitte der Chipleiste.
    revealChip() {
      const strip = this.$refs.buckets;
      const chip = strip && strip.querySelector('[aria-pressed="true"]');
      if (!chip || !strip.clientWidth) return;
      const reduce = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      const left = chip.offsetLeft - (strip.clientWidth - chip.offsetWidth) / 2;
      strip.scrollTo({left, behavior: reduce ? 'auto' : 'smooth'});
    },

    // Hinterlegt den gewaehlten Abschnitt im Diagramm. Kategorien liegen
    // gleich breit nebeneinander, die Lage kommt aus der Zeichenflaeche.
    placeBand() {
      const band = this.$refs.band;
      const globals = this.chart && this.chart.w && this.chart.w.globals;
      if (!band) return;
      const count = this.data ? this.data.buckets.length : 0;
      if (!this.showBucket || !globals || !count || !globals.gridWidth) {
        band.hidden = true;
        return;
      }
      const step = globals.gridWidth / count;
      band.hidden = false;
      band.style.width = `${step}px`;
      band.style.height = `${globals.gridHeight}px`;
      band.style.transform = `translate(${globals.translateX + step * this.selected}px, ${globals.translateY}px)`;
    },

    // Abschnitt unter dem Finger aus der Lage in der Zeichenflaeche, null
    // ausserhalb.
    bucketAt(event) {
      const globals = this.chart && this.chart.w && this.chart.w.globals;
      const count = this.data ? this.data.buckets.length : 0;
      if (!event || !globals || !count || !globals.gridWidth || !this.$refs.chart) return null;
      const x = event.clientX - this.$refs.chart.getBoundingClientRect().left - globals.translateX;
      const index = Math.floor(x / (globals.gridWidth / count));
      return index >= 0 && index < count ? index : null;
    },

    renderChart() {
      if (!this.data || !this.$refs.chart || !window.ApexCharts) return;
      if (this.chart) this.chart.destroy();
      // Unsichtbar (anderer Reiter) ist clientWidth 0, dann gibt die
      // Fensterbreite die Größenordnung vor.
      const width = this.$refs.chart.clientWidth || window.innerWidth || 0;
      // withLosses liefert bei fremder Bucket-Anzahl eine Kopie ohne Verluste.
      const result = this.chainStats ? window.EnergyStatsModel.withLosses(this.data, this.chainStats) : this.data;
      const options = window.EnergyStatsModel.chartOptions(result, width);
      // Antippen waehlt den Abschnitt fuer die Tabelle, ueberall in seiner
      // Spalte, nicht nur auf dem Balken. Neben einem Balken meldet
      // ApexCharts dataPointIndex null.
      options.chart.events = {
        click: (event, _context, config) => {
          const index = config && Number.isInteger(config.dataPointIndex) && config.dataPointIndex >= 0 ? config.dataPointIndex : this.bucketAt(event);
          if (index !== null) this.selectBucket(index);
        },
        mounted: () => this.placeBand(),
        updated: () => this.placeBand(),
      };
      this.chart = new window.ApexCharts(this.$refs.chart, options);
      this.chart.render();
    },

    destroy() {
      if (this.chart) this.chart.destroy();
      if (this.themeOff) this.themeOff();
      if (this.revisionListener) window.removeEventListener('registry-updated', this.revisionListener);
      if (this.narrowQuery) this.narrowQuery.removeEventListener('change', this.narrowListener);
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

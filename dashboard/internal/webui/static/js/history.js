(() => {
  // i18n helper functions - called at runtime only, never on module level
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const tn = (key, n, params) => (window.I18n ? window.I18n.tn(key, n, params) : key);

  // Diagrammlogik und Vorgaben teilt sich das Panel mit der Verlaufskachel
  // (history-chart.js, in base.html davor geladen).
  const {MAX_POINTS, RANGE_PRESETS, AGGREGATES, DERIVED_HAUSVERBRAUCH_SERIES} = window.HistoryChart;

  // Ab dieser Zeilenzahl wird zurueckgefragt. Ein Export der vollen Historie
  // kann dreistellige Megabyte erreichen; das soll niemand versehentlich
  // ausloesen.
  const EXPORT_CONFIRM_ROWS = 200000;

  const TIER_LABEL_KEYS = {
    raw: 'history.resolution.raw',
    '1m': 'history.resolution.1m',
    '5m': 'history.resolution.5m',
  };
  // i18n-keys: history.resolution.raw, history.resolution.1m, history.resolution.5m

  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  const requestJSON = async (url, options) => {
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(apiError(body));
    return body;
  };

  const historyPanel = () => ({
    MAX_POINTS,
    EXPORT_CONFIRM_ROWS,
    RANGE_PRESETS,
    AGGREGATES,
    views: [],
    viewName: '',
    selectedViewId: '',
    rows: [],
    seriesOptions: [],
    selectedSeries: [],
    rangeHours: 6,
    // 'relative' liest rangeHours ab jetzt; 'custom' liest customFrom/customTo
    // aus der flatpickr-Auswahl. Beide Felder bleiben immer belegt, damit ein
    // Wechsel zurueck zu 'relative' (clearCustomRange) ohne Nachfrage den
    // zuletzt aktiven Preset wiederherstellen kann.
    rangeMode: 'relative',
    customFrom: null,
    customTo: null,
    rangePicker: null,
    advancedOpen: false,
    aggregate: 'avg',
    tier: 'raw',
    interpretation: null,
    loading: false,
    error: '',
    initialized: false,
    chart: null,
    // Luecken je Serie, in load() bestimmt. Sie hier zu halten statt sie im
    // Chart aus den fertigen Punkten zurueckzurechnen ist Absicht: nach dem
    // Fuellen stehen 10s-Rohpunkte neben 60s-Mittelwerten, und detectGaps()
    // schaetzt seine Schwelle aus dem Median der Punktabstaende.
    gapsBySeries: new Map(),
    // Hat der Nutzer im Chart gezoomt oder geschoben, halten wir den
    // Ausschnitt fest, damit nachgeladene Daten ihn nicht zuruecksetzen.
    // Der Home-Knopf (beforeResetZoom) loescht beides wieder.
    userZoomed: false,
    zoomWindow: null,
    themeOff: null,
    panelChanged: null,
    historyUpdated: null,
    historyExchanged: null,
    // Messwerte, die dieses Geraet von einem anderen erhalten hat. Ohne
    // diese Notiz erschienen im Diagramm ploetzlich Punkte, die hier nie
    // aufgezeichnet wurden - der Nutzer soll erfahren, woher sie stammen.
    exchangeNotice: '',
    exchangedRows: 0,

    init() {
      this.panelChanged = event => {
        if (event.detail?.panel === 'history-panel') this.activate();
      };
      window.addEventListener('dashboard-panel-changed', this.panelChanged);
      this.historyUpdated = () => {
        if (this.initialized) this.load().then(() => this.renderChart());
      };
      window.addEventListener('dashboard-history-updated', this.historyUpdated);
      // Ein Theme-Wechsel aendert die Serienfarben; der Chart muss sie neu
      // bekommen, sonst behaelt er die Palette des vorigen Themes.
      this.themeOff = window.DashboardTheme.onChange(() => this.renderChart());
      this.historyExchanged = event => {
        const detail = event.detail || {};
        const rows = Number(detail.rows) || 0;
        if (!rows) return;
        this.exchangedRows += rows;
        this.exchangeNotice = tn('history.exchange_notice', this.exchangedRows, {count: this.exchangedRows});
        this.renderChart();
      };
      window.addEventListener('dashboard-history-exchanged', this.historyExchanged);
      this.initRangePicker();
      this.activate();
      this.loadViews();
      this.loadInterpretation();
    },

    // flatpickr ist ein optionaler Fortschritt gegenueber den Zeitraum-Chips,
    // kein Ersatz - ohne window.flatpickr (Skript noch nicht geladen, oder
    // ein Test ohne Vendor-Skripte) bleiben die Presets voll nutzbar, nur der
    // eigene Zeitraum faellt weg. Gleicher Guard-Stil wie renderChart() bei
    // window.ApexCharts. Die Sprachdatei laedt base.html je Sprache mit
    // (FlatpickrLocaleScript), Englisch ist flatpickrs eingebauter Standard.
    initRangePicker() {
      if (!window.flatpickr || !this.$refs?.rangeInput) return;
      const locale = (window.flatpickr.l10ns && window.flatpickr.l10ns[window.I18n.lang]) || 'default';
      this.rangePicker = window.flatpickr(this.$refs.rangeInput, {
        mode: 'range',
        enableTime: true,
        time_24hr: true,
        dateFormat: 'Y-m-d H:i',
        maxDate: new Date(),
        locale,
        onClose: selectedDates => {
          if (selectedDates.length < 2) return;
          this.rangeMode = 'custom';
          this.customFrom = selectedDates[0].getTime();
          this.customTo = selectedDates[1].getTime();
          this.applyRange();
        },
      });
    },

    activate() {
      if (this.initialized) return;
      this.initialized = true;
      this.load().then(() => this.renderChart());
    },

    destroy() {
      if (this.panelChanged) window.removeEventListener('dashboard-panel-changed', this.panelChanged);
      if (this.historyUpdated) window.removeEventListener('dashboard-history-updated', this.historyUpdated);
      if (this.historyExchanged) window.removeEventListener('dashboard-history-exchanged', this.historyExchanged);
      if (this.themeOff) this.themeOff();
      if (this.rangePicker) {
        this.rangePicker.destroy();
        this.rangePicker = null;
      }
      if (this.chart) {
        this.chart.destroy();
        this.chart = null;
      }
    },

    // Einzige Stelle, die rangeHours/rangeMode/customFrom/customTo in
    // Zeitgrenzen uebersetzt - load(), exportMeta() und saveView() lesen alle
    // von hier statt den relativen Fall jeweils selbst nachzurechnen.
    rangeBounds() {
      return window.HistoryChart.rangeBounds({range_mode: this.rangeMode, range_hours: this.rangeHours, range_from: this.customFrom, range_to: this.customTo});
    },

    get recorderConfig() {
      return window.dashboardHistorizer ? window.dashboardHistorizer.config() : {rawWindowHours: 24, minuteWindowDays: 7};
    },

    get recorderStatus() {
      return window.dashboardHistorizer ? window.dashboardHistorizer.status() : {paused: false, persisted: null, reason: ''};
    },

    async load() {
      this.loading = true;
      this.error = '';
      try {
        const {from, to} = this.rangeBounds();
        const result = await window.HistoryChart.loadRows({from, to, interpretation: this.interpretation, recorderConfig: this.recorderConfig});
        this.tier = result.tier;
        this.gapsBySeries = result.gaps;
        this.rows = result.rows;
        this.seriesOptions = [...new Set(this.rows.map(row => row.series))].sort();
        // Beim ersten Laden alles zeigen; eine spaetere Auswahl bleibt.
        if (!this.selectedSeries.length) this.selectedSeries = [...this.seriesOptions];
      } catch (error) {
        this.error = error.message;
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
      }
    },

    get visibleRows() {
      if (!this.selectedSeries.length) return [];
      const selected = new Set(this.selectedSeries);
      return this.rows.filter(row => selected.has(row.series));
    },

    get isEmpty() {
      return !this.loading && this.visibleRows.length === 0;
    },

    get tierLabel() {
      const key = TIER_LABEL_KEYS[this.tier];
      return key ? t(key) : this.tier;
    },

    get statusText() {
      if (this.loading) return t('history.status.loading');
      if (this.error) return this.error;
      const status = this.recorderStatus;
      if (status.paused) return status.reason || t('history.status.paused');
      if (this.isEmpty) {
        return t('history.status.empty');
      }
      const params = {tier: this.tierLabel, points: this.visibleRows.length, series: this.selectedSeries.length};
      if (status.persisted === false) {
        return t('history.status.summary_not_persisted', params);
      }
      return t('history.status.summary', params);
    },

    // Der Wert, der gezeichnet wird. Bei Rohdaten sind min/max/avg gleich,
    // erst ab der Minutenstufe unterscheiden sie sich.
    //
    // Heisst bewusst NICHT valueOf: Alpine legt die Komponente hinter einer
    // reaktiven Proxy an, und ein Methodenname, der Object.prototype.valueOf
    // ueberschreibt, wird bei impliziter Typumwandlung ohne Argument
    // aufgerufen - dann liefert er die Komponente selbst statt row.min/avg,
    // und der anschliessende .toFixed() schlaegt fehl. Im Browser reproduziert
    // (jsdom/vm-Tests bilden diese Proxy-Koerzion nicht ab).
    metricValue(row) {
      return window.HistoryChart.metricValue(row, this.aggregate);
    },

    // Anzeigename einer Serie
    seriesLabel(name) {
      return window.HistoryChart.seriesLabel(name);
    },

    // Stabile Farbe je Serie - fuer den Chart genauso wie fuer den Farbpunkt
    // im Serien-Chip, damit beide immer uebereinstimmen. Der Index fuer die
    // zyklische Palette zaehlt ueber seriesOptions (alle bekannten Serien),
    // nicht nur die gerade sichtbaren - sonst verschieben sich Farben, sobald
    // eine Serie aus- oder eingeblendet wird.
    seriesColor(name) {
      return window.HistoryChart.seriesColor(name, this.seriesOptions);
    },

    get chartOptions() {
      return window.HistoryChart.buildOptions({
        rows: this.visibleRows,
        gaps: this.gapsBySeries,
        aggregate: this.aggregate,
        knownSeries: this.seriesOptions,
        // Gezoomt/geschoben: Ausschnitt merken, damit renderChart() ihn
        // beim Nachladen neuer Punkte wieder festhalten kann. Der
        // Home-Knopf feuert beforeResetZoom und raeumt beides ab.
        events: {
          zoomed: (ctx, opts) => {
            this.userZoomed = true;
            if (opts && opts.xaxis && Number.isFinite(opts.xaxis.min) && Number.isFinite(opts.xaxis.max)) {
              this.zoomWindow = {min: opts.xaxis.min, max: opts.xaxis.max};
            }
          },
          scrolled: (ctx, opts) => {
            if (opts && opts.xaxis && Number.isFinite(opts.xaxis.min) && Number.isFinite(opts.xaxis.max)) {
              this.userZoomed = true;
              this.zoomWindow = {min: opts.xaxis.min, max: opts.xaxis.max};
            }
          },
          beforeResetZoom: () => { this.userZoomed = false; this.zoomWindow = null; },
        },
      });
    },

    renderChart(preserveZoom = true) {
      const element = this.$refs?.chart;
      if (!element || !window.ApexCharts) return;
      const options = this.chartOptions;
      if (this.chart) {
        // updateOptions() setzt den Zoom sonst zurueck; mit den
        // festgehaltenen Achsengrenzen bleibt der Ausschnitt beim Nachladen
        // neuer Punkte stehen.
        if (preserveZoom && this.userZoomed && this.zoomWindow) {
          options.xaxis = {...options.xaxis, min: this.zoomWindow.min, max: this.zoomWindow.max};
        }
        this.chart.updateOptions(options, false, false);
        return;
      }
      this.chart = new window.ApexCharts(element, options);
      this.chart.render();
    },

    async applyRange() {
      // Ein neu gewaehlter Zeitraum hebt einen festgehaltenen Zoom auf.
      this.userZoomed = false;
      this.zoomWindow = null;
      await this.load();
      this.renderChart(false);
    },

    selectPreset(hours) {
      this.rangeMode = 'relative';
      this.rangeHours = hours;
      if (this.rangePicker) this.rangePicker.clear();
      this.applyRange();
    },

    clearCustomRange() {
      this.rangeMode = 'relative';
      this.customFrom = null;
      this.customTo = null;
      if (this.rangePicker) this.rangePicker.clear();
      this.applyRange();
    },

    // Beschriftung des Kalender-Chips, solange ein eigener Zeitraum aktiv
    // ist - sonst bleibt das Eingabefeld leer und zeigt den Platzhalter.
    get customRangeLabel() {
      if (this.rangeMode !== 'custom' || !this.customFrom || !this.customTo) return '';
      const options = {day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit'};
      return `${window.I18n.formatDateTime(this.customFrom, options)} – ${window.I18n.formatDateTime(this.customTo, options)}`;
    },

    toggleSeries(name) {
      this.selectedSeries = this.selectedSeries.includes(name)
        ? this.selectedSeries.filter(item => item !== name)
        : [...this.selectedSeries, name];
      this.renderChart();
    },

    get exportRowCount() {
      return this.visibleRows.length;
    },

    exportMeta() {
      const {from, to} = this.rangeBounds();
      return {
        from,
        to,
        tier: this.tier,
        tierLabel: this.tierLabel,
        aggregate: this.aggregate,
        intervalSeconds: Number(this.recorderConfig.intervalSeconds) || 10,
        // Die Zeitstempel im Export sind UTC; die Zeitzone steht daneben,
        // damit sich die Werte spaeter noch einordnen lassen.
        timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || t('history.export.unknown_timezone'),
        source: 'browser',
      };
    },

    async exportAs(format) {
      const rows = this.visibleRows;
      if (!rows.length) {
        this.$store.toasts.push(t('history.export.empty'), 'critical');
        return;
      }
      if (rows.length > EXPORT_CONFIRM_ROWS) {
        const confirmed = await this.$store.modal.confirm({
          title: t('history.export.large_title'),
          message: t('history.export.large_message', {rows: rows.length}),
        });
        if (!confirmed) return;
      }
      try {
        const meta = this.exportMeta();
        const chunks = format === 'json'
          ? window.HistoryExport.toJSON({rows, meta})
          : window.HistoryExport.toCSV({rows, meta});
        const mime = format === 'json' ? 'application/json' : 'text/csv;charset=utf-8';
        window.HistoryExport.download(chunks, window.HistoryExport.filename(meta, format), mime);
      } catch (error) {
        this.$store.toasts.push(t('history.export.failed', {message: error.message}), 'critical');
      }
    },

    async loadViews() {
      try {
        const value = await requestJSON('/api/v1/settings');
        this.views = Array.isArray(value.history_views) ? value.history_views : [];
      } catch (error) {
        this.views = [];
      }
    },

    // Wird einmal beim Aktivieren des Panels geladen, nicht bei jedem
    // load() - deshalb eigenstaendig statt Teil von load(). Liegen schon
    // Zeilen vor (init() lief parallel zu einem laufenden ersten load()),
    // wird die berechnete Serie mit der frisch eingetroffenen Einstellung
    // neu abgeleitet und der Chart aktualisiert.
    async loadInterpretation() {
      try {
        this.interpretation = await requestJSON('/api/v1/energy/interpretation');
      } catch (error) {
        this.interpretation = null;
      }
      if (this.rows.length) {
        const base = this.rows.filter(row => row.series !== DERIVED_HAUSVERBRAUCH_SERIES);
        this.rows = base.concat(window.HistoryChart.deriveHausverbrauchRows(base, this.interpretation));
        this.renderChart();
      }
    },

    // Lesen-Aendern-Schreiben ueber das gesamte Settings-Objekt: /api/v1/settings
    // nimmt nur vollstaendige Dokumente entgegen, ein Teil-PUT wuerde jede
    // andere Einstellung auf ihren Default zuruecksetzen.
    async persistViews(views) {
      const current = await requestJSON('/api/v1/settings');
      const next = {...current, history_views: views};
      await requestJSON('/api/v1/settings', {
        method: 'PUT',
        headers: {'Content-Type': 'application/json'}, // i18n-ignore
        body: JSON.stringify(next),
      });
      this.views = views;
    },

    async saveView() {
      const name = String(this.viewName || '').trim();
      if (!name) {
        this.$store.toasts.push(t('history.view.needs_name'), 'critical');
        return;
      }
      const view = {
        id: `view-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        name,
        series: [...this.selectedSeries],
        // range_hours bleibt immer gesetzt, auch im custom-Modus - eine
        // aeltere Dashboard-Version, die range_mode/range_from/range_to noch
        // nicht kennt, faellt sonst auf einen ungueltigen Zeitraum zurueck
        // statt auf einen sinnvollen relativen.
        range_hours: Number(this.rangeHours),
        range_mode: this.rangeMode,
        aggregate: this.aggregate,
      };
      if (this.rangeMode === 'custom' && this.customFrom && this.customTo) {
        view.range_from = this.customFrom;
        view.range_to = this.customTo;
      }
      try {
        await this.persistViews([...this.views, view]);
        this.viewName = '';
        this.$store.toasts.push(t('history.view.saved', {name: name}));
      } catch (error) {
        this.$store.toasts.push(t('history.view.save_failed', {message: error.message}), 'critical');
      }
    },

    async applyView(id) {
      const view = this.views.find(item => item.id === id);
      if (!view) return;
      if (view.range_mode === 'custom' && view.range_from && view.range_to) {
        this.rangeMode = 'custom';
        this.customFrom = view.range_from;
        this.customTo = view.range_to;
        if (this.rangePicker) this.rangePicker.setDate([new Date(view.range_from), new Date(view.range_to)], false);
      } else {
        this.rangeMode = 'relative';
        this.rangeHours = view.range_hours;
        if (this.rangePicker) this.rangePicker.clear();
      }
      this.aggregate = view.aggregate;
      // Eine Sicht bringt ihren eigenen Zeitraum mit - ein festgehaltener
      // Zoom aus der vorigen Ansicht passt nicht mehr.
      this.userZoomed = false;
      this.zoomWindow = null;
      await this.load();
      // Nach load(), weil load() bei leerer Auswahl alles vorbelegt - die
      // Sicht soll aber genau ihre Serien zeigen, auch wenn eine davon in
      // diesem Browser gar nicht aufgezeichnet wurde.
      this.selectedSeries = [...view.series];
      this.renderChart(false);
    },

    async deleteView(id) {
      try {
        await this.persistViews(this.views.filter(view => view.id !== id));
        if (this.selectedViewId === id) this.selectedViewId = '';
      } catch (error) {
        this.$store.toasts.push(t('history.view.delete_failed', {message: error.message}), 'critical');
      }
    },
  });

  const register = () => {
    if (window.Alpine) window.Alpine.data('historyPanel', historyPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();

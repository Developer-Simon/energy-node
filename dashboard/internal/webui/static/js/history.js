(() => {
  // Mehr Punkte kann ein Bildschirm nicht aufloesen, und ApexCharts wird
  // darueber auf ARM-Hardware spuerbar traege.
  const MAX_POINTS = 2000;

  // Ab so vielen Luecken je Serie wird die blasse Luecken-"Geist"-Serie
  // weggelassen: die einzelnen Stummel waeren nur noch Subpixel und kosten
  // auf ARM-Hardware bloss Zeit. Die Linie bricht auch ohne sie ab.
  const MAX_STUB_GAPS = 40;

  // Ab dieser Zeilenzahl wird zurueckgefragt. Ein Export der vollen Historie
  // kann dreistellige Megabyte erreichen; das soll niemand versehentlich
  // ausloesen.
  const EXPORT_CONFIRM_ROWS = 200000;

  const TIER_LABELS = {
    raw: 'Rohdaten',
    '1m': 'Minutenmittel',
    '5m': 'Fünf-Minuten-Mittel',
  };

  // Energie-Rollen bekommen dieselbe Farbe wie ihre Kachel in der Übersicht
  // (siehe COLOR_TOKENS in energy-model.js) - history.js dupliziert die
  // Zuordnung statt energy-model.js zu laden, weil das Verlauf-Panel auch
  // ohne Energie-Karten im Layout funktionieren muss.
  const ROLE_COLOR_TOKENS = {
    pv: 'flow-pv',
    battery: 'flow-battery',
    grid: 'flow-grid',
    load: 'flow-load',
    wallbox: 'flow-wallbox',
    heat_pump: 'flow-heatpump',
  };

  // Serienname der aus den role:*-Verlaeufen rekonstruierten Hausverbrauch-
  // Zeile - derselbe Wert, den die Energiekacheln als "Hausverbrauch"
  // zeigen, nur ueber die Zeit.
  const DERIVED_HAUSVERBRAUCH_SERIES = 'berechnet:hausverbrauch';

  // Unsichtbarer Anhang (INVISIBLE SEPARATOR) an den Namen der blassen
  // Luecken-"Geist"-Serie. Sie teilt sich die Farbe mit ihrer echten Serie,
  // taucht aber weder in Legende noch Tooltip auf.
  const GAP_SERIES_SUFFIX = '⁣';

  // ApexCharts baut seine Farbstrings selbst zusammen und kennt kein
  // separates Alpha je Serie - die Luecken-Serie bekommt ihre Deckkraft
  // deshalb direkt in die Farbe gerechnet.
  const withAlpha = (color, alpha) => {
    if (typeof color !== 'string') return color;
    const value = color.trim();
    const short = value.match(/^#([0-9a-f])([0-9a-f])([0-9a-f])$/i);
    const long = value.match(/^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i);
    const rgb = value.match(/^rgba?\(\s*([0-9.]+)[,\s]+([0-9.]+)[,\s]+([0-9.]+)/i);
    let parts;
    if (short) parts = [short[1] + short[1], short[2] + short[2], short[3] + short[3]].map(hex => parseInt(hex, 16));
    else if (long) parts = [long[1], long[2], long[3]].map(hex => parseInt(hex, 16));
    else if (rgb) parts = [rgb[1], rgb[2], rgb[3]].map(Number);
    else return value;
    return `rgba(${parts[0]}, ${parts[1]}, ${parts[2]}, ${alpha})`;
  };

  // ApexCharts' theme.mode faerbt die Symbolleiste (Home, Zoom, Hand). Fest
  // auf 'dark' verdrahtet blieben die Symbole im Light-Theme unsichtbar -
  // hier kommt der Wert aus dem tatsaechlich aktiven Theme.
  const themeMode = () => {
    const attr = (document.documentElement.dataset.theme || '').toLowerCase();
    if (attr === 'light' || attr === 'dark') return attr;
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  };

  const requestJSON = async (url, options) => {
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json();
    if (!response.ok) throw new Error(body.message || 'Anfrage fehlgeschlagen');
    return body;
  };

  // Rekonstruiert die "berechnet:hausverbrauch"-Zeile aus den bereits
  // aufgezeichneten role:*-Verlaufsserien: gruppiert sie je Zeitstempel zu
  // einem Punkt, laesst EnergyModel.deriveBalanceCore() denselben Wert
  // ausrechnen, den die Energiekacheln als "Hausverbrauch" zeigen - separat
  // fuer min/avg/max, damit der bestehende Kennwert-Umschalter weiter
  // funktioniert. energy-model.js ist nur geladen, wenn Energiekacheln im
  // Layout stehen oder das Verlauf-Panel es selbst nachlaedt (base.html) -
  // ohne window.EnergyModel bleibt die Serie einfach weg.
  const deriveHausverbrauchRows = (rows, interpretation) => {
    if (!window.EnergyModel) return [];
    const model = window.EnergyModel;
    const cfg = interpretation || model.DEFAULT_INTERPRETATION;
    const groups = new Map();
    const roles = new Set();
    rows.forEach(row => {
      if (!row.series.startsWith('role:')) return;
      const role = row.series.slice('role:'.length);
      if (!model.HISTORY_ROLES.includes(role)) return;
      roles.add(role);
      if (!groups.has(row.ts)) groups.set(row.ts, {ts: row.ts, u: row.u, n: row.n || 1});
      const group = groups.get(row.ts);
      group[role] = row;
      if (!group.u) group.u = row.u;
      group.n = Math.max(group.n, row.n || 1);
    });

    const loadTotalFor = (group, metric) => {
      const point = {};
      for (const role of model.HISTORY_ROLES) {
        if (group[role] && Number.isFinite(group[role][metric])) point[role] = group[role][metric];
      }
      return model.deriveBalanceCore(model.snapshotFromPoint(point), cfg).load_total;
    };

    // Nur Zeitpunkte, an denen jede im Fenster ueberhaupt vorkommende Rolle
    // einen Wert hat. Eine fehlende Rolle geht in deriveBalanceCore() sonst
    // als 0 ein, und der Hausverbrauch springt an dieser Stelle. Solange nur
    // Rohdaten gezeichnet wurden, konnte das nicht passieren - der Rekorder
    // schreibt alle Rollen im selben Takt. Mit den aus der 1m-Stufe
    // ergaenzten Saetzen stehen Rollen-Punkte jetzt auf verschiedenen
    // Rastern nebeneinander: eine Serie, die dieses Geraet nie selbst
    // gemessen hat, liegt komplett auf Minutengrenzen, die eigenen
    // Rohpunkte auf dem 10s-Takt - ohne diesen Filter waere praktisch jede
    // Gruppe unvollstaendig.
    return [...groups.values()]
      .filter(group => [...roles].every(role => group[role]))
      .map(group => ({
        series: DERIVED_HAUSVERBRAUCH_SERIES,
        ts: group.ts,
        min: loadTotalFor(group, 'min'),
        max: loadTotalFor(group, 'max'),
        avg: loadTotalFor(group, 'avg'),
        n: group.n,
        u: group.u || 'W',
      }));
  };

  // Feste Zeitraum- und Kennwert-Vorgaben fuer die Chip-Gruppen im Panel.
  // Als Konstante statt Komponenten-State: sie aendern sich nie zur Laufzeit,
  // eine reaktive Kopie je Instanz waere reine Verschwendung.
  const RANGE_PRESETS = [
    {hours: 1, label: '1 h'},
    {hours: 6, label: '6 h'},
    {hours: 24, label: 'Tag'},
    {hours: 168, label: 'Woche'},
    {hours: 720, label: 'Monat'},
  ];

  const AGGREGATES = [
    {value: 'avg', label: 'Mittelwert'},
    {value: 'min', label: 'Minimum'},
    {value: 'max', label: 'Maximum'},
  ];

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
        const werte = this.exchangedRows === 1 ? '1 Messwert' : `${this.exchangedRows} Messwerte`;
        this.exchangeNotice = `${werte} von einem anderen Gerät ergänzt.`;
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
    // window.ApexCharts.
    initRangePicker() {
      if (!window.flatpickr || !this.$refs?.rangeInput) return;
      const locale = (window.flatpickr.l10ns && window.flatpickr.l10ns.de) || 'default';
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
      if (this.rangeMode === 'custom' && this.customFrom && this.customTo) {
        return {from: this.customFrom, to: this.customTo};
      }
      const to = Date.now();
      return {from: to - Number(this.rangeHours) * 3600 * 1000, to};
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
        const spanMs = to - from;
        const config = this.recorderConfig;
        this.tier = window.HistoryRollup.selectTier(spanMs, {
          rawWindowMs: Number(config.rawWindowHours) * 3600 * 1000,
          minuteWindowMs: Number(config.minuteWindowDays) * 24 * 3600 * 1000,
        });
        const rows = await window.HistoryStore.readRange(this.tier, null, from, to);
        let normalized = window.HistoryRollup.normalize(rows);
        let gaps;
        if (this.tier === 'raw') {
          // Nur bei der Rohstufe: eigene Aufzeichnungsluecken mit
          // Minutenmitteln fuellen. Die liegen unabhaengig von
          // rawWindowHours schon vor (siehe exchange_compacted_until in
          // history-maintenance.js) - bei 1m/5m-Ansichten braucht es das
          // nicht, dort liest load() ohnehin schon direkt aus derselben
          // Tabelle.
          const rollupRows = window.HistoryRollup.normalize(await window.HistoryStore.readRange('1m', null, from, to));
          const filled = window.HistoryRollup.fillRawGaps(normalized, rollupRows, from, to);
          normalized = filled.rows;
          gaps = filled.gaps;
        } else {
          gaps = window.HistoryRollup.gapsBySeries(normalized, from, to);
        }
        const derived = deriveHausverbrauchRows(normalized, this.interpretation);
        // Die abgeleitete Zeile lebt genau auf den Zeitpunkten der
        // Rollen-Serien und erbt deren Luecken samt und sonders. Die
        // Vereinigung aller Rollen-Luecken muss zu disjunkten Intervallen
        // verschmolzen werden - sonst stehen dort Dutzende deckungsgleicher
        // Spannen, und gapStubs() verkettet ihre Stummel zu Strichen quer
        // ueber den ganzen Chart.
        if (derived.length) {
          const roleGaps = [];
          gaps.forEach((list, series) => { if (series.startsWith('role:')) roleGaps.push(...list); });
          gaps.set(DERIVED_HAUSVERBRAUCH_SERIES, window.HistoryRollup.mergeIntervals(roleGaps));
        }
        this.gapsBySeries = gaps;
        this.rows = normalized.concat(derived);
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
      return TIER_LABELS[this.tier] || this.tier;
    },

    get statusText() {
      if (this.loading) return 'Verläufe werden geladen ...';
      if (this.error) return this.error;
      const status = this.recorderStatus;
      if (status.paused) return status.reason || 'Die Aufzeichnung ist angehalten.';
      if (this.isEmpty) {
        return 'Für diesen Zeitraum liegen in diesem Browser keine Daten. Die Historie wird pro Gerät aufgezeichnet — auf einem Gerät, das noch nicht aufgezeichnet hat, ist sie leer.';
      }
      const persisted = status.persisted === false ? ' · Browser-Speicher nicht als dauerhaft zugesagt' : '';
      return `${this.tierLabel} · ${this.visibleRows.length} Punkte · ${this.selectedSeries.length} Serien${persisted}`;
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
      if (this.aggregate === 'min') return row.min;
      if (this.aggregate === 'max') return row.max;
      return row.avg;
    },

    // Stabile Farbe je Serie - fuer den Chart genauso wie fuer den Farbpunkt
    // im Serien-Chip, damit beide immer uebereinstimmen. Der Index fuer die
    // zyklische Palette zaehlt ueber seriesOptions (alle bekannten Serien),
    // nicht nur die gerade sichtbaren - sonst verschieben sich Farben, sobald
    // eine Serie aus- oder eingeblendet wird.
    seriesColor(name) {
      const theme = window.DashboardTheme;
      if (name === DERIVED_HAUSVERBRAUCH_SERIES) return theme.color('flow-load');
      const role = name.startsWith('role:') ? name.slice('role:'.length) : null;
      const roleToken = role && ROLE_COLOR_TOKENS[role];
      if (roleToken) return theme.color(roleToken);
      const tokens = ['series-1', 'series-2', 'series-3', 'series-4', 'series-5', 'series-6'];
      const index = this.seriesOptions.indexOf(name);
      return theme.color(tokens[(index < 0 ? 0 : index) % tokens.length]);
    },

    get chartOptions() {
      const theme = window.DashboardTheme;
      const grouped = new Map();
      this.visibleRows.forEach(row => {
        if (!grouped.has(row.series)) grouped.set(row.series, []);
        grouped.get(row.series).push(row);
      });
      const names = [...grouped.keys()].sort();
      // Zeitraeume ohne echte Aufzeichnung (Tab inaktiv, Geraet im Schlaf)
      // sollen keine erfundene Gerade zeichnen: die Linie bricht dort ab
      // (withGapBreaks). Statt den Bereich grau zu hinterlegen, zieht eine
      // blasse "Geist"-Serie (gapStubs) den letzten bekannten Wert an jedem
      // Rand ein kurzes Stueck weiter und laesst ihn auslaufen.
      let minTs = Infinity;
      let maxTs = -Infinity;
      grouped.forEach(rows => rows.forEach(row => {
        if (row.ts < minTs) minTs = row.ts;
        if (row.ts > maxTs) maxTs = row.ts;
      }));
      const stubReach = maxTs > minTs ? Math.min((maxTs - minTs) * 0.012, 15 * 60 * 1000) : 0;
      const realSeries = [];
      const ghostSeries = [];
      names.forEach(name => {
        // Sortieren erst hier, je Serie und rein numerisch. Vorher lief ein
        // einziger sort() ueber alle Saetze zusammen und verglich fuer jedes
        // Element zusaetzlich die Seriennamen als Zeichenketten.
        const points = grouped.get(name).sort((left, right) => left.ts - right.ts);
        const thinned = window.HistoryRollup.thin(points, MAX_POINTS);
        // Die Luecken stehen aus load() fest, statt hier aus der geduennten
        // Reihe zurueckgerechnet zu werden - siehe gapsBySeries oben. Einmal
        // je Serie zu disjunkten Intervallen verschmelzen: das haelt sowohl
        // withGapBreaks als auch gapStubs schlank.
        const gaps = window.HistoryRollup.mergeIntervals(this.gapsBySeries.get(name) || []);
        const pairs = thinned.map(row => [row.ts, Number(this.metricValue(row).toFixed(2))]);
        realSeries.push({name, data: window.HistoryRollup.withGapBreaks(pairs, gaps)});
        // Ab sehr vielen Luecken ist jeder Stummel nur noch ein Subpixel -
        // die Geist-Serie kostet dann nur Rechenzeit. Die Linie bricht ohne
        // sie trotzdem sauber ab (withGapBreaks).
        if (gaps.length && gaps.length <= MAX_STUB_GAPS) {
          const stubs = window.HistoryRollup.gapStubs(pairs, gaps, stubReach);
          if (stubs.length) ghostSeries.push({name: `${name}${GAP_SERIES_SUFFIX}`, data: stubs, ghostOf: name});
        }
      });
      const realCount = realSeries.length;
      const series = [...realSeries, ...ghostSeries];
      const unit = this.visibleRows.length ? (this.visibleRows[0].u || '') : '';

      // Einheit je Serie, in der Reihenfolge ihres ersten Auftretens. Solange
      // alles W ist, bleibt es bei der einen Achse. Kommt eine zweite Einheit
      // dazu - der Batterie-Fuellstand role:battery_soc in %, neben den
      // Leistungsserien -, bekommt jede Einheit ihre eigene Y-Achse: die
      // erste links, jede weitere rechts. Ohne das teilten sich W und % eine
      // Skala und der Fuellstand waere als flache Linie am unteren Rand
      // unlesbar.
      const unitBySeries = new Map();
      this.visibleRows.forEach(row => {
        if (!unitBySeries.has(row.series)) unitBySeries.set(row.series, row.u || '');
      });
      const units = [];
      names.forEach(name => {
        const rowUnit = unitBySeries.get(name) || '';
        if (!units.includes(rowUnit)) units.push(rowUnit);
      });
      // Der Fuellstand gehoert nach rechts, die Leistung bleibt die
      // Hauptachse links - unabhaengig davon, wie die Seriennamen sortiert
      // sind. sort() ist stabil, die uebrigen Einheiten behalten also ihre
      // Reihenfolge des ersten Auftretens.
      units.sort((left, right) => (left === '%' ? 1 : 0) - (right === '%' ? 1 : 0));
      const axisLabelStyle = {colors: theme.color('text-muted')};
      const percentScale = axisUnit => (axisUnit === '%' ? {min: 0, max: 100} : {});
      // Eine Achse gehoert zu ihrer Einheit; ihre Geist-Serien bindet sie
      // ueber denselben Namen mit, sonst zeichnete ApexCharts deren Stummel
      // auf der ersten Achse und damit im falschen Massstab.
      const yAxisForUnit = axisUnit => ({
        seriesName: [
          ...names.filter(name => (unitBySeries.get(name) || '') === axisUnit),
          ...ghostSeries.filter(ghost => (unitBySeries.get(ghost.ghostOf) || '') === axisUnit).map(ghost => ghost.name),
        ],
        opposite: units.indexOf(axisUnit) > 0,
        ...percentScale(axisUnit),
        labels: {
          style: axisLabelStyle,
          formatter: value => `${Number(value).toFixed(0)}${axisUnit ? ` ${axisUnit}` : ''}`,
        },
      });
      const singleYAxis = {
        ...percentScale(unit),
        labels: {
          style: axisLabelStyle,
          formatter: value => `${Number(value).toFixed(0)}${unit ? ` ${unit}` : ''}`,
        },
      };
      // Bei gemischten Einheiten nennt der geteilte Tooltip jeden Wert mit der
      // Einheit seiner eigenen Serie statt pauschal mit der ersten.
      const tooltipValue = (value, opts) => {
        const index = opts && typeof opts.seriesIndex === 'number' ? opts.seriesIndex : -1;
        const seriesName = index >= 0 && opts.w && opts.w.globals && opts.w.globals.seriesNames
          ? opts.w.globals.seriesNames[index] : null;
        const rowUnit = (seriesName && unitBySeries.get(seriesName)) || unit;
        return `${Number(value).toFixed(1)}${rowUnit ? ` ${rowUnit}` : ''}`;
      };

      return {
        chart: {
          type: 'line',
          height: 360,
          // Animationen kosten auf ARM-Hardware spuerbar Zeit und bringen
          // bei einem Messverlauf keinen Erkenntnisgewinn.
          animations: {enabled: false},
          toolbar: {show: true, tools: {download: false, selection: true, zoom: true, pan: true, reset: true}},
          zoom: {enabled: true, type: 'x'},
          background: 'transparent',
          fontFamily: 'inherit',
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
        },
        theme: {mode: themeMode()},
        series,
        colors: [
          ...names.map(name => this.seriesColor(name)),
          ...ghostSeries.map(ghost => withAlpha(this.seriesColor(ghost.ghostOf), 0.28)),
        ],
        stroke: {
          curve: 'straight',
          width: series.map(() => 2),
          dashArray: series.map((_, index) => (index < realCount ? 0 : 4)),
        },
        markers: {size: 0},
        dataLabels: {enabled: false},
        xaxis: {type: 'datetime', labels: {datetimeUTC: false, style: {colors: theme.color('text-muted')}}},
        yaxis: units.length > 1 ? units.map(yAxisForUnit) : singleYAxis,
        grid: {borderColor: theme.color('border-soft')},
        annotations: {xaxis: []},
        // Die Serienauswahl ueber dem Chart ist bereits die Legende; die
        // eingebaute Chart-Legende waere nur eine platzraubende Dopplung.
        legend: {show: false},
        tooltip: {
          shared: true,
          enabledOnSeries: realSeries.map((_, index) => index),
          x: {format: 'dd.MM.yyyy HH:mm:ss'},
          y: {formatter: units.length > 1
            ? tooltipValue
            : value => `${Number(value).toFixed(1)}${unit ? ` ${unit}` : ''}`},
        },
        noData: {text: 'Keine Daten im gewählten Zeitraum'},
      };
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
      const format = new Intl.DateTimeFormat('de-DE', {day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit'});
      return `${format.format(this.customFrom)} – ${format.format(this.customTo)}`;
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
        timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'unbekannt',
        source: 'browser',
      };
    },

    async exportAs(format) {
      const rows = this.visibleRows;
      if (!rows.length) {
        this.$store.toasts.push('Für den gewählten Zeitraum gibt es nichts zu exportieren.', 'critical');
        return;
      }
      if (rows.length > EXPORT_CONFIRM_ROWS) {
        const confirmed = await this.$store.modal.confirm({
          title: 'Großer Export',
          message: `Der Export umfasst ${rows.length} Zeilen und kann mehrere hundert Megabyte groß werden. Fortfahren?`,
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
        this.$store.toasts.push(`Export fehlgeschlagen: ${error.message}`, 'critical');
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
        this.rows = base.concat(deriveHausverbrauchRows(base, this.interpretation));
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
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(next),
      });
      this.views = views;
    },

    async saveView() {
      const name = String(this.viewName || '').trim();
      if (!name) {
        this.$store.toasts.push('Die Sicht braucht einen Namen.', 'critical');
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
        this.$store.toasts.push(`Sicht „${name}" gespeichert.`);
      } catch (error) {
        this.$store.toasts.push(`Sicht konnte nicht gespeichert werden: ${error.message}`, 'critical');
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
        this.$store.toasts.push(`Sicht konnte nicht gelöscht werden: ${error.message}`, 'critical');
      }
    },
  });

  const register = () => {
    if (window.Alpine) window.Alpine.data('historyPanel', historyPanel);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();

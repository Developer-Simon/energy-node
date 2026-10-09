// Gemeinsames Diagramm-Modul der Verlaufsseite (history.js) und der
// Verlaufskachel auf der Uebersicht (history-view-card.js). Kein Alpine,
// kein Zustand: jede Funktion bekommt, was sie braucht, als Argument.
// Vorher steckte alles in der Alpine-Komponente historyPanel - eine zweite
// Darstellung haette es kopieren muessen und waere auseinandergelaufen.
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const HOUR = 3600 * 1000;

  // Mehr Punkte kann ein Bildschirm nicht aufloesen, und ApexCharts wird
  // darueber auf ARM-Hardware spuerbar traege.
  const MAX_POINTS = 2000;

  // Ab so vielen Luecken je Serie wird die blasse Luecken-"Geist"-Serie
  // weggelassen: die einzelnen Stummel waeren nur noch Subpixel und kosten
  // auf ARM-Hardware bloss Zeit. Die Linie bricht auch ohne sie ab.
  const MAX_STUB_GAPS = 40;

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

  // Anzeigename einer Serie. Die Kennung (role:pv, berechnet:hausverbrauch)
  // bleibt der Schluessel im Browser-Speicher und erscheint nie selbst.
  // i18n-keys: energy.role_label.pv, energy.role_label.battery, energy.role_label.battery_charge, energy.role_label.battery_discharge, energy.role_label.battery_soc, energy.role_label.grid, energy.role_label.grid_import, energy.role_label.grid_export, energy.role_label.load, energy.role_label.wallbox, energy.role_label.heat_pump
  // Namen eigener Kategorien (role:custom:<basis>:<id>), von loadRows() aus
  // dem Energie-Snapshot geholt. Ohne Eintrag bleibt die Kennung sichtbar.
  let categoryLabels = {};
  const CUSTOM_SERIES = /^role:custom:[a-z]+:([a-z0-9_]+)$/;

  async function loadCategoryLabels() {
    try {
      const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}/api/v1/energy`);
      if (!response.ok) return;
      const categories = ((await response.json()) || {}).categories || {};
      categoryLabels = Object.fromEntries(Object.entries(categories)
        .filter(([, def]) => def && typeof def === 'object' && def.label)
        .map(([id, def]) => [id, def.label]));
    } catch (error) {
      // Ohne Snapshot zeigt die Serie ihre Kennung.
    }
  }

  function seriesLabel(name) {
    if (name === DERIVED_HAUSVERBRAUCH_SERIES) return t('history.series.derived_load');
    const custom = CUSTOM_SERIES.exec(name);
    if (custom && categoryLabels[custom[1]]) return categoryLabels[custom[1]];
    if (name.startsWith('role:')) {
      const key = `energy.role_label.${name.slice('role:'.length)}`;
      const label = t(key);
      if (label !== key) return label;
    }
    return name;
  }

  // ApexCharts ships English month and day names. Instead of bundling one
  // locale file per language, the names come from Intl for the catalog's
  // meta.locale, the toolbar titles from the catalog.
  function apexLocale() {
    const I18n = window.I18n;
    const monthName = (month, style) => I18n.formatDate(Date.UTC(2021, month, 15), {month: style, timeZone: 'UTC'});
    // 2021-01-03 is a Sunday, ApexCharts starts its weeks on Sunday.
    const dayName = (day, style) => I18n.formatDate(Date.UTC(2021, 0, 3 + day), {weekday: style, timeZone: 'UTC'});
    const range = count => Array.from({length: count}, (_, index) => index);
    return {
      name: I18n.lang,
      options: {
        months: range(12).map(month => monthName(month, 'long')),
        shortMonths: range(12).map(month => monthName(month, 'short')),
        days: range(7).map(day => dayName(day, 'long')),
        shortDays: range(7).map(day => dayName(day, 'short')),
        toolbar: {
          exportToSVG: I18n.t('chart.toolbar.export_svg'),
          exportToPNG: I18n.t('chart.toolbar.export_png'),
          exportToCSV: I18n.t('chart.toolbar.export_csv'),
          menu: I18n.t('chart.toolbar.menu'),
          selection: I18n.t('chart.toolbar.selection'),
          selectionZoom: I18n.t('chart.toolbar.selection_zoom'),
          zoomIn: I18n.t('chart.toolbar.zoom_in'),
          zoomOut: I18n.t('chart.toolbar.zoom_out'),
          pan: I18n.t('chart.toolbar.pan'),
          reset: I18n.t('chart.toolbar.reset'),
        },
      },
    };
  }

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
    {hours: 24, labelKey: 'history.range.day'},
    {hours: 168, labelKey: 'history.range.week'},
    {hours: 720, labelKey: 'history.range.month'},
  ];
  // i18n-keys: history.range.day, history.range.week, history.range.month

  const AGGREGATES = [
    {value: 'avg', labelKey: 'history.aggregate.average'},
    {value: 'min', labelKey: 'history.aggregate.minimum'},
    {value: 'max', labelKey: 'history.aggregate.maximum'},
  ];
  // i18n-keys: history.aggregate.average, history.aggregate.minimum, history.aggregate.maximum

  const SERIES_TOKENS = ['series-1', 'series-2', 'series-3', 'series-4', 'series-5', 'series-6'];

  // Der Index fuer die zyklische Palette zaehlt ueber knownSeries (alle
  // bekannten Serien), nicht nur die sichtbaren - sonst verschieben sich
  // Farben, sobald eine Serie aus- oder eingeblendet wird.
  function seriesColor(name, knownSeries) {
    const theme = window.DashboardTheme;
    if (name === DERIVED_HAUSVERBRAUCH_SERIES) return theme.color('flow-load');
    const role = name.startsWith('role:') ? name.slice('role:'.length) : null;
    const roleToken = role && ROLE_COLOR_TOKENS[role];
    if (roleToken) return theme.color(roleToken);
    const index = (knownSeries || []).indexOf(name);
    return theme.color(SERIES_TOKENS[(index < 0 ? 0 : index) % SERIES_TOKENS.length]);
  }

  // Bei Rohdaten sind min/max/avg gleich, erst ab der Minutenstufe
  // unterscheiden sie sich. (Zur Namenswahl siehe metricValue in history.js.)
  function metricValue(row, aggregate) {
    if (aggregate === 'min') return row.min;
    if (aggregate === 'max') return row.max;
    return row.avg;
  }

  // view ist eine gespeicherte Sicht oder die Kachel-Konfiguration aus
  // historyview.go - beide mit range_mode/range_hours/range_from/range_to.
  function rangeBounds(view, now = Date.now()) {
    if (view.range_mode === 'custom' && view.range_from && view.range_to) {
      return {from: view.range_from, to: view.range_to};
    }
    return {from: now - Number(view.range_hours) * HOUR, to: now};
  }

  function rangeLabel(view) {
    if (view.range_mode === 'custom' && view.range_from && view.range_to) {
      const options = {day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit'};
      return `${window.I18n.formatDateTime(view.range_from, options)} – ${window.I18n.formatDateTime(view.range_to, options)}`;
    }
    const hours = Number(view.range_hours);
    const preset = RANGE_PRESETS.find(item => item.hours === hours);
    if (preset) return preset.labelKey ? t(preset.labelKey) : preset.label;
    return `${hours} h`;
  }

  // Der Rumpf von historyPanel.load() ohne Komponentenzustand: Stufe
  // waehlen, lesen, Rohluecken aus der 1m-Stufe fuellen, Hausverbrauch
  // ableiten. Fehler wirft die Funktion weiter, die Aufrufer melden sie.
  async function loadRows({from, to, interpretation, recorderConfig}) {
    const config = recorderConfig || {rawWindowHours: 24, minuteWindowDays: 7};
    const tier = window.HistoryRollup.selectTier(to - from, {
      rawWindowMs: Number(config.rawWindowHours) * HOUR,
      minuteWindowMs: Number(config.minuteWindowDays) * 24 * HOUR,
    });
    const [rows] = await Promise.all([window.HistoryStore.readRange(tier, null, from, to), loadCategoryLabels()]);
    let normalized = window.HistoryRollup.normalize(rows);
    let gaps;
    if (tier === 'raw') {
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
    const derived = deriveHausverbrauchRows(normalized, interpretation);
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
    return {tier, rows: normalized.concat(derived), gaps};
  }

  // Der Rumpf des chartOptions-Getters, mit
  //   this.visibleRows      -> rows
  //   this.gapsBySeries     -> gaps
  //   this.metricValue(row) -> metricValue(row, aggregate)
  //   this.seriesColor(n)   -> seriesColor(n, knownSeries)
  // und den zwei Unterschieden fuer compact unten.
  function buildOptions({rows, gaps, aggregate, knownSeries, compact = false, events}) {
    const theme = window.DashboardTheme;
    const grouped = new Map();
    rows.forEach(row => {
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
      const gapsList = window.HistoryRollup.mergeIntervals(gaps.get(name) || []);
      const pairs = thinned.map(row => [row.ts, Number(metricValue(row, aggregate).toFixed(2))]);
      realSeries.push({name, data: window.HistoryRollup.withGapBreaks(pairs, gapsList)});
      // Ab sehr vielen Luecken ist jeder Stummel nur noch ein Subpixel -
      // die Geist-Serie kostet dann nur Rechenzeit. Die Linie bricht ohne
      // sie trotzdem sauber ab (withGapBreaks).
      if (gapsList.length && gapsList.length <= MAX_STUB_GAPS) {
        const stubs = window.HistoryRollup.gapStubs(pairs, gapsList, stubReach);
        if (stubs.length) ghostSeries.push({name: `${name}${GAP_SERIES_SUFFIX}`, data: stubs, ghostOf: name});
      }
    });
    const realCount = realSeries.length;
    const series = [...realSeries, ...ghostSeries];
    const unit = rows.length ? (rows[0].u || '') : '';

    // Einheit je Serie, in der Reihenfolge ihres ersten Auftretens. Solange
    // alles W ist, bleibt es bei der einen Achse. Kommt eine zweite Einheit
    // dazu - der Batterie-Fuellstand role:battery_soc in %, neben den
    // Leistungsserien -, bekommt jede Einheit ihre eigene Y-Achse: die
    // erste links, jede weitere rechts. Ohne das teilten sich W und % eine
    // Skala und der Fuellstand waere als flache Linie am unteren Rand
    // unlesbar.
    const unitBySeries = new Map();
    rows.forEach(row => {
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
        formatter: value => `${window.I18n.formatNumber(Number(value), 0)}${axisUnit ? ` ${axisUnit}` : ''}`,
      },
    });
    const singleYAxis = {
      ...percentScale(unit),
      labels: {
        style: axisLabelStyle,
        formatter: value => `${window.I18n.formatNumber(Number(value), 0)}${unit ? ` ${unit}` : ''}`,
      },
    };
    // Bei gemischten Einheiten nennt der geteilte Tooltip jeden Wert mit der
    // Einheit seiner eigenen Serie statt pauschal mit der ersten.
    const tooltipValue = (value, opts) => {
      const index = opts && typeof opts.seriesIndex === 'number' ? opts.seriesIndex : -1;
      const seriesName = index >= 0 && opts.w && opts.w.globals && opts.w.globals.seriesNames
        ? opts.w.globals.seriesNames[index] : null;
      const rowUnit = (seriesName && unitBySeries.get(seriesName)) || unit;
      return `${window.I18n.formatNumber(Number(value), 1)}${rowUnit ? ` ${rowUnit}` : ''}`;
    };

    const options = {
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
        locales: [apexLocale()],
        defaultLocale: window.I18n.lang,
        events: events || {},
      },
      theme: {mode: themeMode()},
      series,
      colors: [
        ...names.map(name => seriesColor(name, knownSeries)),
        ...ghostSeries.map(ghost => withAlpha(seriesColor(ghost.ghostOf, knownSeries), 0.28)),
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
        x: {formatter: value => window.I18n.formatDateTime(value)},
        y: {title: {formatter: seriesName => seriesLabel(seriesName)}, formatter: units.length > 1
          ? tooltipValue
          : value => `${window.I18n.formatNumber(Number(value), 1)}${unit ? ` ${unit}` : ''}`},
      },
      noData: {text: t('history.status.no_data')},
    };
    if (compact) {
      options.chart.height = '100%';
      options.chart.toolbar = {show: false};
      options.chart.zoom = {enabled: false};
      // Die Kachel hat keine Serien-Chips - hier ist die Legende die
      // einzige Zuordnung von Farbe zu Serie. customLegendItems laesst die
      // Geist-Serien weg, die Farben stimmen, weil die echten Serien vorn
      // stehen. Nur lesen: Klick und Hover aendern nichts.
      options.legend = {
        show: true,
        position: 'bottom',
        fontSize: '11px',
        customLegendItems: names.map(name => seriesLabel(name)),
        labels: {colors: theme.color('text-muted')},
        onItemClick: {toggleDataSeries: false},
        onItemHover: {highlightDataSeries: false},
      };
    }
    return options;
  }

  window.HistoryChart = {
    MAX_POINTS, RANGE_PRESETS, AGGREGATES, DERIVED_HAUSVERBRAUCH_SERIES,
    seriesLabel, apexLocale, themeMode, withAlpha, seriesColor, metricValue,
    deriveHausverbrauchRows, rangeBounds, rangeLabel, loadRows, buildOptions,
  };
})();

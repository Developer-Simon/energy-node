// "Datentafel": no diagram at all - two stacked balance bars, then one row
// per role with direction, a proportional bar, the value, freshness and a
// sparkline. Port of Vorschlag C from the six-proposals exploration; its
// four layout-editor options (sort, spark_window, dense, show_inactive) are
// read from the layout item's data-* attributes, see
// knowhow/dashboard/energiegrafiken-konfiguration-backlog.md.
//
// Sparklines read real samples via window.dashboardHistorizer.readSamples()
// (the role:<role> pseudo-entities history-recorder.js now also records),
// not the prototype's synthetic noise profile.
(() => {
  const SPARK_WINDOW_MS = {15: 15 * 60 * 1000, 60: 60 * 60 * 1000};

  // Modul-Ebene, nicht je Instanz - dieselbe Begruendung wie energy-day.js:
  // dashboard.js's refreshLiveFragment() tauscht diese Kachel bei jedem
  // SSE-Takt per htmx outerHTML aus und erzeugt die Alpine-Komponente neu.
  // Ohne Startwert stuende die frisch gemountete Instanz ohne Sparklines da,
  // bis ihr eigener IndexedDB-Lesevorgang zurueckkommt - sichtbar als
  // Flackern der ganzen Spalte im Sekundentakt.
  let cachedSamples = null;

  // role id -> which snapshot roles feed its stale/freshness check. "battery"
  // and "grid" are shown as one net row but can be backed by split roles.
  const STALE_ROLES = {
    pv: ['pv'],
    battery: ['battery', 'battery_charge', 'battery_discharge'],
    grid: ['grid', 'grid_import', 'grid_export'],
    load: ['load'],
    wallbox: ['wallbox'],
    heat_pump: ['heat_pump'],
  };

  function isRowStale(snapshot, rowID) {
    const model = window.EnergyModel;
    return (STALE_ROLES[rowID] || []).some(role => model.isStale(snapshot, role));
  }

  // Fixed row order: pv, battery, grid, load, wallbox, heat_pump, plus an
  // optional "Nicht zugeordnet" row when sources/sinks don't balance.
  function boardRows(snapshot, balance) {
    const model = window.EnergyModel;
    const pv = model.roleValue(snapshot, 'pv');
    const batteryNet = balance.charge - balance.discharge;
    const batteryValue = Math.abs(batteryNet);
    const batteryDir = batteryNet > 0.5 ? 'Laden' : batteryNet < -0.5 ? 'Entladen' : 'Ruhe';
    const gridNet = balance.gridImport - balance.gridExport;
    const gridDir = balance.gridImport > 0.5 ? 'Bezug' : balance.gridExport > 0.5 ? 'Einspeisung' : 'Ruhe';
    const wallbox = model.roleValue(snapshot, 'wallbox');
    const heatPump = model.roleValue(snapshot, 'heat_pump');

    const rows = [
      {id: 'pv', label: 'PV', value: pv, color: model.COLORS.pv, dir: pv > 0.5 ? 'Erzeugung' : '—'},
      {id: 'battery', label: 'Batterie', value: batteryValue, color: model.COLORS.batteryCharge, dir: batteryDir},
      {id: 'grid', label: 'Netz', value: Math.abs(gridNet), color: model.COLORS.gridImport, dir: gridDir},
      {id: 'load', label: balance.loadSource === 'calculated' || balance.loadSource === 'combined' ? 'Hausverbrauch (berechnet)' : 'Hausverbrauch',
        value: balance.load, color: model.COLORS.base, dir: 'Verbrauch'},
      // Die gemessenen Teilverbraucher stehen unter dem Hausverbrauch, nicht
      // daneben: sie sind ein Teil von ihm, kein weiterer Summand. Alle
      // beziehen ihre Frische aus der Rolle "load", aus der sie stammen.
      ...balance.measuredFlows.map(flow => ({
        id: flow.id, label: flow.label, value: flow.value, color: flow.color,
        dir: 'Gemessen', staleRole: 'load',
      })),
      {id: 'wallbox', label: 'Wallbox', value: wallbox, color: model.COLORS.wallbox, dir: wallbox > 0.5 ? 'Lädt' : 'Ruhe'},
      {id: 'heat_pump', label: 'Wärmepumpe', value: heatPump, color: model.COLORS.heatPump, dir: heatPump > 0.5 ? 'Verbrauch' : 'Ruhe'},
    ];
    if (balance.unbalanced) {
      rows.push({
        id: 'rest', label: 'Nicht zugeordnet', value: Math.abs(balance.gap), color: model.COLORS.rest,
        dir: balance.gap > 0 ? 'Fehlt rechts' : 'Fehlt links', quality: 'gap',
      });
    } else if (Math.abs(balance.gapAbsorbed) > 0.5) {
      rows.push({
        id: 'rest', label: 'Nicht zugeordnet (eingerechnet)', value: Math.abs(balance.gapAbsorbed), color: model.COLORS.rest,
        dir: balance.gapAbsorbed > 0 ? 'Im Hausverbrauch' : 'Unbekannte Erzeugung', quality: 'gap-absorbed',
      });
    }
    for (const row of rows) {
      row.stale = row.quality === 'gap' || row.quality === 'gap-absorbed' ? false : isRowStale(snapshot, row.staleRole || row.id);
      row.fresh = row.quality === 'gap' ? 'gap' : row.quality === 'gap-absorbed' ? 'gap-absorbed' : row.stale ? 'stale' : 'fresh';
    }
    return rows;
  }

  // Bar-width fraction relative to the largest row value, floored so a
  // present-but-tiny value still shows a sliver instead of nothing.
  function barFraction(row, maxValue) {
    if (row.value <= 0.5) return 0;
    return Math.max(row.value / maxValue, 0.015);
  }

  // Two stacked-bar summaries ("Woher"/"Wohin"), each segment's flex-basis
  // is its share of the side's total; a label is only shown once a segment
  // is wide enough to hold it (>12%), same threshold as the prototype.
  function summaryBar(flows) {
    const model = window.EnergyModel;
    const total = flows.reduce((sum, flow) => sum + flow.value, 0) || 1;
    return {
      total,
      segments: flows.map(flow => ({
        ...flow,
        fraction: flow.value / total,
        text: flow.value / total > 0.12 ? `${flow.label} ${model.formatPercent(flow.value / total)}` : '',
      })),
    };
  }

  // show_inactive off drops rows at or below the 0.5 W noise floor; sort
  // "power" then orders what remains by value, descending. The "rest" row
  // (a real balance gap) is only ever added by boardRows() when its value is
  // meaningfully non-zero, so it is never dropped by the inactivity filter.
  function visibleRows(rows, showInactive, sort) {
    const visible = showInactive ? rows : rows.filter(row => row.value > 0.5);
    return sort === 'power' ? [...visible].sort((a, b) => b.value - a.value) : visible;
  }

  function roleSeries(samples, role, now, windowMs) {
    const cutoff = now - windowMs;
    return samples
      .filter(sample => sample.entity_id === `role:${role}` && Date.parse(sample.timestamp) >= cutoff)
      .sort((a, b) => Date.parse(a.timestamp) - Date.parse(b.timestamp))
      .map(sample => Number(sample.value))
      .filter(Number.isFinite);
  }

  // Die Zeile "Hausverbrauch" zeigt balance.load, also Balance.LoadTotal aus
  // internal/energy/balance.go - keine Rolle, sondern das Ergebnis von
  // load_mode/gap_mode. Ihre Sparkline muss denselben Weg gehen, sonst zeigt
  // sie auf jeder Anlage ohne Hausverbrauchszaehler eine leere Linie neben
  // einer gefuellten Zahl. Rekonstruiert wird je Verlaufspunkt, nicht aus
  // einer eigenen aufgezeichneten Pseudo-Rolle: das wirkt rueckwirkend auf
  // die bereits vorhandene Historie und folgt einer spaeter geaenderten
  // Einstellung sofort.
  function loadSeries(samples, interpretation, now, windowMs) {
    const model = window.EnergyModel;
    const cutoff = now - windowMs;
    return model.groupRoleSamples(samples)
      .filter(point => Date.parse(point.timestamp) >= cutoff)
      .map(point => model.deriveBalance(model.snapshotFromPoint(point), interpretation).load)
      .filter(Number.isFinite);
  }

  // Turns a time-ordered list of (possibly signed) values into a normalized
  // 0..100 x / 0..26 y sparkline path, scaled to the largest magnitude in
  // the window. Returns null when there is nothing to draw yet.
  function sparklineGeometry(values) {
    if (values.length < 2) return null;
    const max = Math.max(...values.map(v => Math.abs(v)), 1);
    const step = 100 / (values.length - 1);
    const y = v => 24 - (Math.abs(v) / max) * 21;
    let line = '';
    values.forEach((v, i) => { line += `${i ? ' L ' : 'M '}${(i * step).toFixed(1)} ${y(v).toFixed(1)}`; });
    return {line, area: `${line} L 100 26 L 0 26 Z`, lastY: y(values[values.length - 1])};
  }

  const energyBoardCard = () => ({
    snapshot: null,
    rows: [],
    summarySource: {total: 0, segments: []},
    summarySink: {total: 0, segments: []},
    sparklines: {},
    options: {sort: 'fixed', sparkWindow: '15', dense: 'off', showInactive: 'on', measuredSplit: 'sum'},
    presenter: null,

    async init() {
      const data = this.$root.closest('[data-layout-item-id]')?.dataset || {};
      this.options = {
        sort: data.sort || 'fixed',
        sparkWindow: data.sparkWindow || '15',
        dense: data.dense || 'off',
        showInactive: data.showInactive || 'on',
        measuredSplit: data.measuredSplit || 'sum',
      };
      const raw = window.EnergyModel.readEmbeddedSnapshot('energy-board-initial');
      const reducedMotion = !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
      this.presenter = window.EnergyPresentation.present({
        key: data.layoutItemId || 'energy-board',
        raw,
        reducedMotion,
        alive: () => !!(this.$root && this.$root.isConnected),
        apply: presented => { this.snapshot = presented; this.compute(); },
      });

      // Synchroner Startwert aus der vorigen Instanz, siehe cachedSamples
      // oben - zeichnet sofort statt bis zum Ende des Lesevorgangs leer zu
      // bleiben.
      if (cachedSamples && this.options.sparkWindow !== 'off') {
        this.updateSparklines(cachedSamples);
      }

      if (window.dashboardHistorizer && this.options.sparkWindow !== 'off') {
        try {
          const samples = await window.dashboardHistorizer.readSamples();
          cachedSamples = samples;
          this.updateSparklines(samples);
        } catch (error) {
          if (!cachedSamples) this.sparklines = {};
        }
      }
      this.themeOff = window.DashboardTheme.onChange(() => this.compute());
    },

    destroy() {
      if (this.presenter) this.presenter.release();
      if (this.themeOff) this.themeOff();
    },

    compute() {
      if (!this.snapshot) return;
      const model = window.EnergyModel;
      const balance = model.balanceOf(this.snapshot, {measuredSplit: this.options.measuredSplit});
      const allRows = boardRows(this.snapshot, balance);
      this.rows = visibleRows(allRows, this.options.showInactive !== 'off', this.options.sort);
      this.summarySource = summaryBar(balance.sources);
      this.summarySink = summaryBar(balance.sinks);
    },

    updateSparklines(samples) {
      const windowMs = SPARK_WINDOW_MS[this.options.sparkWindow] || SPARK_WINDOW_MS[15];
      const now = Date.now();
      const interpretation = this.snapshot && this.snapshot.interpretation;
      const geometries = {};
      for (const row of this.rows) {
        if (row.id === 'rest') continue;
        // Einzelzeilen je Entitaet haben keine aufgezeichnete Historie: der
        // Verlaufsspeicher kennt nur Rollensummen. Die Sammelzeile dagegen
        // ist genau die Rolle "load".
        if (row.id.startsWith('load_measured:') || row.id === 'load_measured_rest') continue;
        const values = row.id === 'load'
          ? loadSeries(samples, interpretation, now, windowMs)
          : roleSeries(samples, row.id === 'load_measured' ? 'load' : row.id, now, windowMs);
        geometries[row.id] = sparklineGeometry(values);
      }
      this.sparklines = geometries;
    },

    get maxRowValue() {
      return Math.max(...this.rows.map(row => row.value), 1);
    },

    barWidth(row) {
      return `${(barFraction(row, this.maxRowValue) * 100).toFixed(1)}%`;
    },

    freshDotColor(row) {
      if (row.fresh === 'gap') return window.DashboardTheme.color('bad');
      if (row.fresh === 'gap-absorbed') return window.DashboardTheme.color('warn');
      if (row.fresh === 'stale') return window.DashboardTheme.color('text-faint');
      return window.DashboardTheme.color('ok');
    },

    freshLabel(row) {
      if (row.fresh === 'gap') return 'Bilanzlücke';
      if (row.fresh === 'gap-absorbed') return 'eingerechnet';
      if (row.fresh === 'stale') return 'veraltet';
      return 'aktuell';
    },

    format(value) {
      return window.EnergyModel.formatPower(value);
    },
  });

  energyBoardCard.boardRows = boardRows;
  energyBoardCard.visibleRows = visibleRows;
  energyBoardCard.barFraction = barFraction;
  energyBoardCard.summaryBar = summaryBar;
  energyBoardCard.roleSeries = roleSeries;
  energyBoardCard.loadSeries = loadSeries;
  energyBoardCard.sparklineGeometry = sparklineGeometry;

  const register = () => {
    if (window.Alpine) window.Alpine.data('energyBoardCard', energyBoardCard);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();

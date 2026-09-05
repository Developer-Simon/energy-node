// Shared read-only helpers for the six alternative energy-flow graphics
// (energy-band.js / energy-ring.js / energy-board.js / energy-day.js /
// energy-schema.js / energy-status.js). Exposed as window.EnergyModel, same
// namespacing pattern as window.dashboardHistorizer.
//
// The snapshot basics (roleValue/hasValue/hasPower/isStale/battery*Power)
// are a deliberate duplicate of the ones energy-flow.js already ports from
// internal/energy.Snapshot - not factored out of that file, so its existing
// tests/comments stay untouched. Everything below deriveBalance() is new:
// it ports the prototype's derive() from the six-proposals exploration into
// a shape driven by a real energy.Snapshot instead of slider state.
(() => {
  function roleValue(snapshot, role) {
    return (snapshot && snapshot.values && snapshot.values[role]) || 0;
  }

  function hasValue(snapshot, role) {
    return !!(snapshot && snapshot.values && Object.prototype.hasOwnProperty.call(snapshot.values, role));
  }

  function hasPower(snapshot, role) {
    return hasValue(snapshot, role) && Math.abs(roleValue(snapshot, role)) > 0.000001;
  }

  function isStale(snapshot, role) {
    return (snapshot && snapshot.roles || []).some(state => state.role === role && state.freshness === 'stale');
  }

  function batteryChargePower(snapshot) {
    if (hasValue(snapshot, 'battery_charge')) return Math.abs(roleValue(snapshot, 'battery_charge'));
    const battery = roleValue(snapshot, 'battery');
    return hasValue(snapshot, 'battery') && battery > 0 ? battery : 0;
  }

  function batteryDischargePower(snapshot) {
    if (hasValue(snapshot, 'battery_discharge')) return Math.abs(roleValue(snapshot, 'battery_discharge'));
    const battery = roleValue(snapshot, 'battery');
    return hasValue(snapshot, 'battery') && battery < 0 ? Math.abs(battery) : 0;
  }

  // Same fallback shape as the battery helpers above: prefer the split
  // grid_import/grid_export roles, fall back to the sign of a combined
  // "grid" role.
  function gridImportPower(snapshot) {
    if (hasValue(snapshot, 'grid_import')) return Math.abs(roleValue(snapshot, 'grid_import'));
    const grid = roleValue(snapshot, 'grid');
    return hasValue(snapshot, 'grid') && grid > 0 ? grid : 0;
  }

  function gridExportPower(snapshot) {
    if (hasValue(snapshot, 'grid_export')) return Math.abs(roleValue(snapshot, 'grid_export'));
    const grid = roleValue(snapshot, 'grid');
    return hasValue(snapshot, 'grid') && grid < 0 ? Math.abs(grid) : 0;
  }

  // "1.234 W" below 1000, "1,23 kW" from 1000 up - 1:1 with the prototype's
  // fmt(w, unit). unit defaults to 'auto' (threshold at 1000 W); energy-
  // band.js's "Einheit" option ('w'/'kw') can force one or the other.
  function formatPower(value, unit = 'auto') {
    const abs = Math.abs(value);
    const mode = unit === 'auto' ? (abs >= 1000 ? 'kw' : 'w') : unit;
    if (mode === 'kw') return `${(value / 1000).toFixed(2).replace('.', ',')} kW`;
    return `${Math.round(value).toLocaleString('de-DE')} W`;
  }

  function formatPercent(fraction) {
    const value = fraction * 100;
    return `${value.toFixed(value < 10 ? 1 : 0).replace('.', ',')} %`;
  }

  // Card-local color palette. Die Werte kommen aus den Theme-Tokens in
  // base.css (siehe theme.js) - als Getter, weil ein Themewechsel zur
  // Laufzeit passiert und die Karten dasselbe Objekt weiterbenutzen.
  const COLOR_TOKENS = {
    pv: 'flow-pv',
    gridImport: 'flow-grid',
    gridExport: 'flow-grid',
    batteryCharge: 'flow-battery',
    batteryDischarge: 'flow-battery',
    base: 'flow-load',
    wallbox: 'flow-wallbox',
    heatPump: 'flow-heatpump',
    loadMeasured: 'flow-measured',
    rest: 'flow-rest',
  };

  const COLORS = {};
  for (const key of Object.keys(COLOR_TOKENS)) {
    Object.defineProperty(COLORS, key, {
      enumerable: true,
      get: () => window.DashboardTheme.color(COLOR_TOKENS[key]),
    });
  }

  const LABELS = {
    pv: 'PV',
    gridImport: 'Netzbezug',
    gridExport: 'Einspeisung',
    batteryCharge: 'Batterie lädt',
    batteryDischarge: 'Batterie entlädt',
    base: 'Übriger Verbrauch',
    baseCalculated: 'Übriger Verbrauch (berechnet)',
    wallbox: 'Wallbox',
    heatPump: 'Wärmepumpe',
    loadMeasured: 'Gemessene Verbraucher',
    loadMeasuredRest: 'Gemessene Verbraucher (Rest)',
    rest: 'Nicht zugeordnet',
    unknownGeneration: 'Unbekannte Erzeugung',
  };

  // Farben der Einzelpositionen bei measured_split "entities". Die
  // series-Tokens statt der flow-Tokens: die gemessenen Teilverbraucher sind
  // eine offene, anlagenspezifische Menge ohne feste Rollenbedeutung - genau
  // der Fall, fuer den die kategorialen Serienfarben da sind. Zyklisch, damit
  // auch die siebte Entitaet eine Farbe bekommt.
  const MEASURED_SERIES_TOKENS = ['series-1', 'series-2', 'series-3', 'series-4', 'series-5', 'series-6'];

  // Default balance interpretation - mirrors energy.DefaultInterpretation()
  // in internal/energy/interpretation.go. Used whenever a snapshot carries
  // no server-computed balance/interpretation of its own (deriveBalance()
  // called without one, e.g. from a test or an old cached snapshot).
  const DEFAULT_INTERPRETATION = {
    gap_mode: 'unknown_consumer',
    load_mode: 'auto',
    gap_tolerance_mode: 'absolute',
    gap_tolerance_w: 25,
    gap_tolerance_percent: 2,
  };

  // Fills in defaults for whatever normalizeInterpretation's caller left
  // unset - the JS mirror of Interpretation.Normalized().
  function normalizeInterpretation(cfg) {
    cfg = cfg || {};
    const toleranceW = typeof cfg.gap_tolerance_w === 'number' ? cfg.gap_tolerance_w : 0;
    const tolerancePercent = typeof cfg.gap_tolerance_percent === 'number' ? cfg.gap_tolerance_percent : 0;
    const bothZero = toleranceW === 0 && tolerancePercent === 0;
    return {
      gap_mode: cfg.gap_mode || DEFAULT_INTERPRETATION.gap_mode,
      load_mode: cfg.load_mode || DEFAULT_INTERPRETATION.load_mode,
      gap_tolerance_mode: cfg.gap_tolerance_mode || DEFAULT_INTERPRETATION.gap_tolerance_mode,
      gap_tolerance_w: bothZero ? DEFAULT_INTERPRETATION.gap_tolerance_w : toleranceW,
      gap_tolerance_percent: bothZero ? DEFAULT_INTERPRETATION.gap_tolerance_percent : tolerancePercent,
    };
  }

  // JS mirror of internal/energy/balance.go's DeriveBalance() - same
  // arithmetic, same field names (snake_case, matching the Go JSON tags) so
  // composeBalance() below can render either this or the server's real
  // snapshot.balance through one code path. Only used for energy-day.js's
  // history points, reconstructed client-side from IndexedDB; every other
  // card prefers the server-computed balance via balanceOf(). See
  // knowhow/dashboard/energie-interpretation.md and
  // internal/energy/testdata/balance-cases.json, the fixture shared with
  // test/energy-model.test.mjs that keeps this from silently drifting away
  // from the Go original.
  function deriveBalanceCore(snapshot, interpretation) {
    const cfg = normalizeInterpretation(interpretation);
    const pv = roleValue(snapshot, 'pv');
    const charge = batteryChargePower(snapshot);
    const discharge = batteryDischargePower(snapshot);
    const gridImport = gridImportPower(snapshot);
    const gridExport = gridExportPower(snapshot);
    const wallbox = Math.max(roleValue(snapshot, 'wallbox'), 0);
    const heatPump = Math.max(roleValue(snapshot, 'heat_pump'), 0);

    const calculated = Math.max(pv + gridImport + discharge - gridExport - charge, 0);
    const hasMeasured = hasValue(snapshot, 'load');
    const measured = hasMeasured ? Math.max(roleValue(snapshot, 'load'), 0) : 0;

    let loadTotal;
    let loadSource;
    let loadMeasured = 0;
    if (cfg.load_mode === 'calculated') {
      loadTotal = calculated;
      loadSource = 'calculated';
    } else if (cfg.load_mode === 'combined') {
      // Spiegelt LoadModeCombined in internal/energy/balance.go: Gesamtwert
      // aus der Bilanz, gemessene Teilverbraucher separat.
      loadTotal = calculated;
      loadSource = 'combined';
      loadMeasured = measured;
    } else if (cfg.load_mode === 'measured') {
      loadTotal = hasMeasured ? measured : 0;
      loadSource = hasMeasured ? 'measured' : 'missing';
    } else {
      loadTotal = hasMeasured ? measured : calculated;
      loadSource = hasMeasured ? 'measured' : 'calculated';
    }

    let base = Math.max(loadTotal - wallbox - heatPump - loadMeasured, 0);
    const gapRaw = (pv + gridImport + discharge) - (base + loadMeasured + wallbox + heatPump + gridExport + charge);

    const sumSources = pv + gridImport + discharge;
    const sumSinks = base + loadMeasured + wallbox + heatPump + gridExport + charge;
    const total = Math.max(sumSources, sumSinks);

    const toleranceW = cfg.gap_tolerance_mode === 'percent' ? (cfg.gap_tolerance_percent / 100) * total : cfg.gap_tolerance_w;
    const gapIgnored = Math.abs(gapRaw) <= toleranceW;
    const gapApplied = gapIgnored ? 0 : gapRaw;

    let gapAbsorbed = 0;
    let unbalanced = false;
    if (cfg.gap_mode === 'diagnostic') {
      unbalanced = gapApplied !== 0;
    } else {
      gapAbsorbed = gapApplied;
      if (gapApplied > 0) {
        loadTotal += gapApplied;
        base += gapApplied;
      }
    }

    const autarkie = loadTotal > 0 ? Math.max(0, Math.min(1, 1 - gridImport / loadTotal)) : 0;
    const eigenverbrauch = pv > 0 ? Math.max(0, Math.min(1, 1 - gridExport / pv)) : 0;

    return {
      pv, grid_import: gridImport, grid_export: gridExport,
      battery_charge: charge, battery_discharge: discharge,
      wallbox, heat_pump: heatPump,
      load_total: loadTotal, load_source: loadSource, load_measured: loadMeasured, base,
      gap_raw: gapRaw, gap_applied: gapApplied, gap_absorbed: gapAbsorbed,
      gap_tolerance_w: toleranceW, gap_ignored: gapIgnored, unbalanced,
      autarkie, eigenverbrauch, netz: gridImport - gridExport, total,
      // Durchreichungen wie in Go. Fuer aus IndexedDB rekonstruierte
      // Verlaufspunkte fehlen die Werte und bleiben 0 - dort ist kein
      // Fuellstand gespeichert (Verlauf ist Spec C).
      battery_soc: roleValue(snapshot, 'battery_soc'),
      battery_capacity_kwh: roleValue(snapshot, 'battery_capacity_kwh'),
      battery_energy_kwh: roleValue(snapshot, 'battery_energy_kwh'),
    };
  }

  // Die Senkenpositionen fuer den im Kombiniert-Modus abgetrennten gemessenen
  // Verbrauch. Ausserhalb dieses Modus ist load_measured 0 und die Liste
  // leer, so dass jeder Aufrufer sie bedingungslos einstreuen kann.
  //
  // split === 'entities' zerlegt die Summe in eine Position je load-Entitaet
  // (Layout-Option measured_split, pro Kachel). Was unter der 0,5-W-Grenze
  // liegt oder - bei invertierten Zuordnungen - negativ ist, bekommt keine
  // eigene Position; die Differenz zur Summe wird als eine Restposition
  // nachgereicht, damit die Senkenseite trotzdem aufgeht.
  function measuredLoadFlows(n, snapshot, split) {
    const measured = n.load_measured || 0;
    if (measured <= 0.5) return [];
    if (split !== 'entities') {
      return [{id: 'load_measured', label: LABELS.loadMeasured, value: measured, color: COLORS.loadMeasured}];
    }
    const flows = ((snapshot && snapshot.entities) || [])
      .filter(entity => entity.role && entity.role.role === 'load' && entity.value > 0.5)
      .map((entity, index) => ({
        id: `load_measured:${entity.entity_id}`,
        label: entity.label || entity.entity_id,
        value: entity.value,
        color: window.DashboardTheme.color(MEASURED_SERIES_TOKENS[index % MEASURED_SERIES_TOKENS.length]),
      }));
    const shown = flows.reduce((sum, flow) => sum + flow.value, 0);
    if (measured - shown > 0.5) {
      flows.push({id: 'load_measured_rest', label: LABELS.loadMeasuredRest, value: measured - shown, color: COLORS.loadMeasured});
    }
    return flows;
  }

  // Turns a Balance-shaped object (either the server's real snapshot.balance
  // or deriveBalanceCore()'s JS mirror - both use the same snake_case field
  // names) into the render shape the six cards actually consume: source/sink
  // lists with color+label, plus the KPI numbers. Kept separate from
  // deriveBalanceCore so balanceOf() can feed it the canonical server
  // numbers without ever recomputing them in JS.
  function composeBalance(n, options) {
    options = options || {};
    const calculatedLoad = n.load_source === 'calculated' || n.load_source === 'combined';
    const baseLabel = calculatedLoad ? LABELS.baseCalculated : LABELS.base;
    const measuredFlows = measuredLoadFlows(n, options.snapshot, options.measuredSplit);

    const sources = [
      {id: 'pv', label: LABELS.pv, value: n.pv, color: COLORS.pv},
      {id: 'grid_import', label: LABELS.gridImport, value: n.grid_import, color: COLORS.gridImport},
      {id: 'battery_discharge', label: LABELS.batteryDischarge, value: n.battery_discharge, color: COLORS.batteryDischarge},
    ].filter(f => f.value > 0.5);

    const sinks = [
      {id: 'base', label: baseLabel, value: n.base, color: COLORS.base},
      ...measuredFlows,
      {id: 'wallbox', label: LABELS.wallbox, value: n.wallbox, color: COLORS.wallbox},
      {id: 'heat_pump', label: LABELS.heatPump, value: n.heat_pump, color: COLORS.heatPump},
      {id: 'grid_export', label: LABELS.gridExport, value: n.grid_export, color: COLORS.gridExport},
      {id: 'battery_charge', label: LABELS.batteryCharge, value: n.battery_charge, color: COLORS.batteryCharge},
    ].filter(f => f.value > 0.5);

    // diagnostic mode: an unresolved gap becomes a grey "Nicht zugeordnet"
    // entry on whichever side is short - unchanged from before. unknown_consumer
    // mode never adds a "rest" position (a positive gap is already folded into
    // "base" above); a negative gap draws a distinctly-labeled "Unbekannte
    // Erzeugung" source instead, since that is a finding, not a data gap.
    if (n.unbalanced) {
      if (n.gap_applied > 0.5) sinks.push({id: 'rest', label: LABELS.rest, value: n.gap_applied, color: COLORS.rest, rest: true});
      else if (n.gap_applied < -0.5) sources.push({id: 'rest', label: LABELS.rest, value: -n.gap_applied, color: COLORS.rest, rest: true});
    } else if (n.gap_absorbed < -0.5) {
      sources.push({id: 'unknown_generation', label: LABELS.unknownGeneration, value: -n.gap_absorbed, color: COLORS.rest, rest: true});
    }

    return {
      sources, sinks, total: Math.max(n.total, 1), load: n.load_total, base: n.base,
      measured: n.load_measured || 0, measuredFlows,
      wallbox: n.wallbox, heatPump: n.heat_pump,
      charge: n.battery_charge, discharge: n.battery_discharge, gridImport: n.grid_import, gridExport: n.grid_export,
      gap: n.gap_applied, unbalanced: n.unbalanced, gapAbsorbed: n.gap_absorbed, loadSource: n.load_source,
      kpi: {autarkie: n.autarkie, eigen: n.eigenverbrauch, netz: n.netz, last: n.load_total},
    };
  }

  // Zuordnung Quelle -> Verbraucher fuer die Verbraucherbalken in
  // energy-schema.js (siehe Spec "Anlagenschema: fliessende Skalierung",
  // Abschnitt 5). "merit" schneidet zwei nach Prioritaet geordnete Achsen
  // (Quellen PV -> Speicherentladung -> Netz, Verbraucher Direktverbrauch ->
  // Speicherladung -> Einspeisung): jeder Verbraucher bekommt den saubersten
  // noch verfuegbaren Strom, dieselbe Logik wie bei Eigenverbrauchsquoten.
  // "prorata" gibt jedem Verbraucher denselben Mix - nur die Balkenlaenge
  // traegt dann noch Information, keine Segmentfarbe.
  const SOURCE_PRIORITY = {pv: 0, battery_discharge: 1, grid_import: 2};
  const SINK_PRIORITY = {battery_charge: 1, grid_export: 2};

  function orderByPriority(list, priorityOf) {
    return list
      .map((item, index) => ({item, index}))
      .sort((a, b) => (priorityOf(a.item) - priorityOf(b.item)) || (a.index - b.index))
      .map(entry => entry.item);
  }

  function allocate(split, mode) {
    const sources = (split && split.sources) || [];
    const sinks = (split && split.sinks) || [];

    if (mode === 'prorata') {
      const sourceTotal = sources.reduce((sum, s) => sum + s.value, 0) || 1;
      return sinks.map(sink => ({
        id: sink.id, label: sink.label, v: sink.value,
        mix: sources.map(s => ({label: s.label, color: s.color, v: sink.value * (s.value / sourceTotal)})),
      }));
    }

    // "merit": Quellen und Verbraucher liegen auf derselben Achse (0 ..
    // Durchsatz), jeweils in Prioritaetsreihenfolge aneinandergereiht.
    // Ein Verbraucher-Intervall bekommt als Mix genau die Ueberlappung mit
    // jedem Quell-Intervall - dieselbe Technik wie ein Sankey-Schnitt.
    const orderedSources = orderByPriority(sources, s => SOURCE_PRIORITY[s.id] ?? 3);
    const orderedSinks = orderByPriority(sinks, s => SINK_PRIORITY[s.id] ?? 0);

    let cursor = 0;
    const sourceSpans = orderedSources.map(source => {
      const span = {source, y0: cursor, y1: cursor + source.value};
      cursor += source.value;
      return span;
    });

    cursor = 0;
    const bySinkID = new Map();
    for (const sink of orderedSinks) {
      const y0 = cursor;
      const y1 = y0 + sink.value;
      cursor = y1;
      const mix = [];
      for (const span of sourceSpans) {
        const overlap = Math.min(y1, span.y1) - Math.max(y0, span.y0);
        if (overlap > 0.0001) mix.push({label: span.source.label, color: span.source.color, v: overlap});
      }
      bySinkID.set(sink.id, {id: sink.id, label: sink.label, v: sink.value, mix});
    }
    return sinks.map(sink => bySinkID.get(sink.id));
  }

  // deriveBalance() is the JS-only path (composeBalance(deriveBalanceCore(...))),
  // kept for energy-day.js's synthetic history-point snapshots and for tests.
  // balanceOf() is what every live card should call instead: it prefers the
  // server's real snapshot.balance and only falls back to the JS mirror when
  // a snapshot carries none (i.e. synthetic snapshots without one).
  function deriveBalance(snapshot, interpretation, options) {
    return composeBalance(deriveBalanceCore(snapshot, interpretation), {snapshot, ...(options || {})});
  }

  function balanceOf(snapshot, options) {
    const n = snapshot && snapshot.balance;
    const composeOptions = {snapshot, ...(options || {})};
    return n ? composeBalance(n, composeOptions) : composeBalance(deriveBalanceCore(snapshot, snapshot && snapshot.interpretation), composeOptions);
  }

  // Die Rollen, die history-recorder.js als "role:<name>" aufzeichnet. Als
  // Liste hier, damit groupRoleSamples() fremde Eintraege aus demselben
  // IndexedDB-Speicher (die Entitaets-Samples des Verlauf-Tabs) nicht
  // versehentlich als Rolle einsammelt.
  const HISTORY_ROLES = ['pv', 'battery', 'grid', 'load', 'wallbox', 'heat_pump'];

  // Gruppiert die flache role:<role>-Sampleliste zurueck zu einem Punkt je
  // Abfrage - die Samples einer collect()-Runde teilen sich snapshot.at als
  // Zeitstempel. Frueher dayPoints() in energy-day.js; hierher gezogen, weil
  // energy-board.js dieselbe Rekonstruktion braucht, energy-day.js aber nur
  // geladen wird, wenn das Tagesband im Layout steht.
  //
  // Kein Vollstaendigkeitsfilter auf "pv" mehr: seit history-recorder.js nur
  // noch zugeordnete Rollen schreibt, waere das Fehlen von PV eine Aussage
  // ueber die Anlage, kein Zeichen fuer eine halb geschriebene Abfrage.
  function groupRoleSamples(samples) {
    const groups = new Map();
    for (const sample of samples || []) {
      if (!sample.entity_id || !sample.entity_id.startsWith('role:')) continue;
      const role = sample.entity_id.slice('role:'.length);
      if (!HISTORY_ROLES.includes(role)) continue;
      if (!groups.has(sample.timestamp)) groups.set(sample.timestamp, {timestamp: sample.timestamp});
      groups.get(sample.timestamp)[role] = Number(sample.value);
    }
    return [...groups.values()]
      .filter(point => HISTORY_ROLES.some(role => Number.isFinite(point[role])))
      .sort((a, b) => Date.parse(a.timestamp) - Date.parse(b.timestamp));
  }

  // Macht aus einem Verlaufspunkt den synthetischen Schnappschuss, den
  // deriveBalance() erwartet. Setzt bewusst nur die Rollen, die der Punkt
  // wirklich traegt: ein vorhandener, aber nullter "load"-Schluessel macht
  // hasMeasured in deriveBalanceCore() wahr und laesst load_total im Modus
  // "auto" auf 0 fallen, obwohl gar nichts gemessen wurde.
  function snapshotFromPoint(point) {
    const values = {};
    for (const role of HISTORY_ROLES) {
      if (Number.isFinite(point[role])) values[role] = point[role];
    }
    return {values, roles: []};
  }

  // Reads and JSON-parses one of the <script type="application/json"> tags
  // energySnapshotJSON() (webui.go) embeds, falling back to the single shared
  // node #energy-snapshot-initial that overview.html emits since the
  // Serverlast-Spec. Shared by all six cards' init() so a malformed/missing
  // tag degrades to null (empty-state rendering) instead of throwing during
  // mount.
  //
  // Die karten-eigene ID hat bewusst Vorrang: die sieben
  // energy-*.page.test.mjs injizieren "energy-band-initial" und Geschwister
  // direkt ins Test-DOM. Wird die Reihenfolge umgedreht, fallen sie still auf
  // den gemeinsamen Knoten zurueck und pruefen nichts mehr.
  function readEmbeddedSnapshot(elementId) {
    const node = document.getElementById(elementId) || document.getElementById('energy-snapshot-initial');
    if (!node) return null;
    try {
      return JSON.parse(node.textContent || '{}');
    } catch (error) {
      return null;
    }
  }

  // Removes everything drawn into an <svg> by a previous render() call while
  // keeping its <title>/<desc> - those carry the accessible name/description
  // and must stay the same DOM nodes across re-renders (screen readers
  // watch them), same reasoning as the prototype's clearSvg().
  function clearSvgChildren(svg) {
    [...svg.children].forEach(node => {
      if (node.localName !== 'title' && node.localName !== 'desc') svg.removeChild(node);
    });
  }

  const SVG_NS = 'http://www.w3.org/2000/svg';

  // Small createElementNS + setAttribute(s) + textContent helper, used by
  // every card's imperative SVG rendering to keep that code terse.
  function svgEl(tag, attributes, text) {
    const node = document.createElementNS(SVG_NS, tag);
    for (const key in attributes) node.setAttribute(key, attributes[key]);
    if (text != null) node.textContent = text;
    return node;
  }

  window.EnergyModel = {
    roleValue, hasValue, hasPower, isStale,
    batteryChargePower, batteryDischargePower, gridImportPower, gridExportPower,
    formatPower, formatPercent, deriveBalance, deriveBalanceCore, composeBalance, balanceOf, readEmbeddedSnapshot,
    groupRoleSamples, snapshotFromPoint, HISTORY_ROLES,
    clearSvgChildren, svgEl, SVG_NS,
    measuredLoadFlows, allocate, COLORS, LABELS, DEFAULT_INTERPRETATION,
  };
})();

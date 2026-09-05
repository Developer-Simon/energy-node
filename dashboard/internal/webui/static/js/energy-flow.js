// Draws the energy-flow graphic client-side: unverzerrtes, leistungs-
// proportionales SVG statt der früheren acht fest kodierten Server-Pfade
// (siehe knowhow/dashboard/energiefluss-grafik.md). This used to be an
// opt-in "Beta" mode next to a server-rendered
// fallback; the fallback and its toggle have been retired, this is now the
// only rendering path.
//
// Bis 2026-08 stimmte hier: "die Karte ist ein normaler Teil von
// #overview-live, dashboard.js's periodischer htmx-Tausch rendert sie also
// bei jedem Update ohnehin komplett neu". Seit dashboard.js diesen Tausch
// bei reinen Push-abgedeckten Rastern ueberspringt (liveGridPushCovers()),
// stimmt das nicht mehr - die Karte abonniert darum in init() zusaetzlich
// EnergyPresentation.present(), genau wie die sechs anderen Energiekarten,
// nur mit dem rohen statt dem schon gefederten Wert (siehe dort).
(() => {
  const SVG_NS = 'http://www.w3.org/2000/svg';

  // Die moeglichen Icon-Zustandsklassen, eine davon setzt step() je Knoten aus
  // nodeText().dir (battery: charging/discharging; pv/grid/home/load: siehe
  // dort). 'idle' hat keine Klasse. base.css haengt die Animationen daran.
  const ICON_STATE_CLASSES = [
    'is-charging', 'is-discharging', 'is-producing',
    'is-importing', 'is-exporting', 'is-live', 'is-drawing',
  ];

  // Die Lasten-Kachel zeigt genau ein Icon, passend zum aktiven Verbraucher
  // mit der hoechsten Leistung. step() tauscht das SVG-Innere nur, wenn
  // nodeText('load').icon wechselt - nicht pro Bild. Die efn-*-Klassen und
  // ihre Animationen stehen in base.css unter .energy-flow-load-icon.
  const LOAD_ICON_MARKUP = {
    generic:
      '<circle class="efn-ring" cx="11" cy="6" r="4.6" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-dasharray="3.4 3.8257"></circle>' +
      '<path class="efn-bolt" d="M11.8 1.8 L8.2 6.7 L10.7 6.7 L10.2 10.2 L13.8 5.3 L11.3 5.3 Z"></path>',
    wallbox:
      '<path class="efn-car" d="M7.6 9.4 L7.6 7.7 Q7.6 7.05 8.3 6.95 L9.7 6.75 L11.5 4.35 Q11.85 3.95 12.5 3.95 L16.2 3.95 Q16.9 3.95 17.3 4.45 L18.9 6.75 L20.3 6.95 Q21 7.05 21 7.85 L21 9.4 Z"></path>' +
      '<circle class="efn-car" cx="10" cy="9.6" r="1.3"></circle><circle class="efn-car" cx="18.5" cy="9.6" r="1.3"></circle>' +
      '<path class="efn-car" d="M7.6 7.9 L6.6 7.9"></path>' +
      '<g class="efn-feed"><path d="M4.5 4.9 Q3.4 5.6 4 6.9 Q4.7 8.2 7.1 7.95"></path>' +
      '<rect x="2.4" y="4" width="2.4" height="1.8" rx=".55"></rect>' +
      '<path d="M4.8 4.55 L5.7 4.55 M4.8 5.25 L5.7 5.25"></path></g>' +
      '<circle class="efn-fx" cx="0" cy="0" r=".95" style="offset-path:path(\'M4.5 4.9 Q3.4 5.6 4 6.9 Q4.7 8.2 7.1 7.95\')"></circle>',
    heatpump:
      '<rect class="efn-out" x="4.6" y="1.4" width="12.8" height="9.2" rx="1.3"></rect>' +
      '<line class="efn-thin" x1="5.5" y1="3.6" x2="6.9" y2="3.6"></line><line class="efn-thin" x1="5.5" y1="6" x2="6.9" y2="6"></line><line class="efn-thin" x1="5.5" y1="8.4" x2="6.9" y2="8.4"></line>' +
      '<line class="efn-thin" x1="15.1" y1="3.6" x2="16.5" y2="3.6"></line><line class="efn-thin" x1="15.1" y1="6" x2="16.5" y2="6"></line><line class="efn-thin" x1="15.1" y1="8.4" x2="16.5" y2="8.4"></line>' +
      '<circle class="efn-out" cx="11" cy="6" r="3.4"></circle>' +
      '<g class="efn-rotor">' +
      '<path class="efn-blade" d="M11 5.9 C10.05 4.75 10.2 3.35 11 2.7 C11.8 3.35 11.95 4.75 11 5.9 Z"></path>' +
      '<path class="efn-blade" d="M11 5.9 C10.05 4.75 10.2 3.35 11 2.7 C11.8 3.35 11.95 4.75 11 5.9 Z" transform="rotate(120 11 6)"></path>' +
      '<path class="efn-blade" d="M11 5.9 C10.05 4.75 10.2 3.35 11 2.7 C11.8 3.35 11.95 4.75 11 5.9 Z" transform="rotate(240 11 6)"></path>' +
      '</g><circle class="efn-hub" cx="11" cy="6" r=".95"></circle>',
  };

  const NODE_IDS = ['pv', 'grid', 'home', 'battery', 'load'];

  // Normierte (0..1) Knotenmittelpunkte. Zwei Anordnungen; die Achse waehlt
  // geometry() nach dem Seitenverhaeltnis: im Querformat sitzen PV/Speicher
  // links und Netz/Verbrauch rechts, im Hochformat oben/unten. Die Zuordnung
  // (welcher Knoten wohin) bleibt beim Drehen gleich, nur die Achse kippt -
  // so laufen die Linien immer entlang der langen Kante.
  const NODE_LAYOUT = {
    h: {
      pv: {x: .15, y: .25}, battery: {x: .15, y: .75},
      grid: {x: .85, y: .25}, load: {x: .85, y: .75},
      home: {x: .5, y: .5},
    },
    v: {
      pv: {x: .25, y: .15}, grid: {x: .75, y: .15},
      battery: {x: .25, y: .85}, load: {x: .75, y: .85},
      home: {x: .5, y: .5},
    },
  };
  const nodeLayoutFor = (width, height) => NODE_LAYOUT[width >= height ? 'h' : 'v'];

  // Ab diesem Seitenverhaeltnis (nur im Querformat) kippt die Kruemmung aller
  // vier Linien nach aussen: bei einer sehr breiten Kachel wachsen die
  // einwaerts gebogenen Kurven sonst so tief, dass PV/Speicher links und
  // Netz/Verbrauch rechts einander vor dem Gebaeude schneiden. Nach aussen
  // gebogen laufen sie auseinander. Im normalen Quer- und im Hochformat
  // bleibt es bei der Einwaerts-Biegung.
  const WIDE_BOW_RATIO = 3.5;

  // Approximate rendered node footprint in px, mirrors the fixed rem sizing
  // in base.css (.energy-flow-node / .energy-flow-node-home). Kept as
  // constants rather than a DOM measurement so geometry() stays a pure,
  // easily testable function.
  const NODE_SIZE = {w: 152, h: 82};
  const HOME_NODE_SIZE = {w: 168, h: 96};
  const sizeFor = id => (id === 'home' ? HOME_NODE_SIZE : NODE_SIZE);

  // Ab wieviel Watt eine Linie ueberhaupt animiert. Darunter gilt der Fluss
  // als ruhend: entweder "zugeordnet, steht still" (neutral, sichtbare
  // Standlinie) oder "gar nicht da" (idle, ausgegraut).
  const MIN_FLOW_W = 5;

  // Vier Verbindungen, je genau eine Linie. Import/Export bzw. Laden/Entladen
  // teilen sich dieselbe Linie - die Richtung zeigt allein die Laufrichtung
  // der Marschanimation (state().reversed -> is-reversed -> animation-direction:
  // reverse). `sign` biegt die Kurve; die Vorzeichen liegen so, dass die vier
  // Linien spiegelsymmetrisch um das Gebaeude sitzen. `from`->`to` ist die
  // Vorwaertsrichtung (nicht reversed): PV/Netz/Speicher zeigen zum Gebaeude,
  // der Verbrauch davon weg.
  const CONNECTIONS = [
    {
      id: 'pv', from: 'pv', to: 'home', colorClass: 'energy-flow-pv', sign: 1,
      state: s => {
        const p = Math.abs(roleValue(s, 'pv'));
        return p > MIN_FLOW_W ? {kind: 'active', value: p, reversed: false} : idleState();
      },
    },
    {
      id: 'grid', from: 'grid', to: 'home', colorClass: 'energy-flow-grid', sign: -1,
      state: s => {
        const net = gridImportPower(s) - gridExportPower(s); // + = Bezug (grid->home)
        if (Math.abs(net) > MIN_FLOW_W) return {kind: 'active', value: Math.abs(net), reversed: net < 0};
        if (hasValue(s, 'grid') || hasValue(s, 'grid_import') || hasValue(s, 'grid_export')) return neutralState();
        return idleState();
      },
    },
    {
      id: 'battery', from: 'battery', to: 'home', colorClass: 'energy-flow-battery', sign: -1,
      state: s => {
        const net = batteryNetPower(s); // + = Laden (home->battery), - = Entladen
        if (Math.abs(net) > MIN_FLOW_W) return {kind: 'active', value: Math.abs(net), reversed: net > 0};
        if (hasValue(s, 'battery') || hasValue(s, 'battery_charge') || hasValue(s, 'battery_discharge')) return neutralState();
        return idleState();
      },
    },
    {
      id: 'load', from: 'home', to: 'load', colorClass: 'energy-flow-load', sign: -1,
      state: s => {
        const p = Math.abs(loadTotal(s));
        return p > MIN_FLOW_W && loadSource(s) !== 'missing'
          ? {kind: 'active', value: p, reversed: false}
          : idleState();
      },
    },
  ];
  const idleState = () => ({kind: 'idle', value: 0, reversed: false});
  const neutralState = () => ({kind: 'neutral', value: 0, reversed: false});

  // --- Snapshot helpers, 1:1 ports of internal/energy.Snapshot's methods ---

  function roleValue(snapshot, role) {
    return (snapshot && snapshot.values && snapshot.values[role]) || 0;
  }

  function hasValue(snapshot, role) {
    return !!(snapshot && snapshot.values && Object.prototype.hasOwnProperty.call(snapshot.values, role));
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

  // Positive = net charging, negative = net discharging - the single number
  // the flow graphic and the battery node label both animate/render, so two
  // split sensors reporting at the same time collapse to one direction
  // instead of animating both battery_charge and battery_discharge at once.
  function batteryNetPower(snapshot) {
    return batteryChargePower(snapshot) - batteryDischargePower(snapshot);
  }

  // Same shape as the battery helpers above: prefer the split
  // grid_import/grid_export roles, fall back to the sign of a combined
  // "grid" role. Without this fallback an installation that only reports a
  // signed grid meter falls through to the deliberately unanimated
  // grid_neutral flow (base.css .energy-flow-line.is-neutral) - the line is
  // drawn, but it stands still even under load.
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

  // The household consumption every card labels "Hausverbrauch" is
  // Balance.LoadTotal (internal/energy/balance.go), not the raw "load" role:
  // whether it is measured, calculated from the other roles, or topped up by
  // an absorbed balance gap is decided on the Energie page via
  // load_mode/gap_mode. The server puts the result into snapshot.balance,
  // part of the very snapshot this card already reads - so no dependency on
  // window.EnergyModel, which base.html only loads conditionally.
  //
  // The fallback to the raw role covers snapshots without a balance at all:
  // hand-written test fixtures and anything cached before this field existed.
  function loadSource(snapshot) {
    const balance = snapshot && snapshot.balance;
    if (balance && typeof balance.load_total === 'number') return balance.load_source || 'calculated';
    return hasValue(snapshot, 'load') ? 'measured' : 'missing';
  }

  function loadTotal(snapshot) {
    const balance = snapshot && snapshot.balance;
    if (balance && typeof balance.load_total === 'number') return balance.load_total;
    return roleValue(snapshot, 'load');
  }

  // Der im Kombiniert-Modus abgetrennte gemessene Teilverbrauch. Wie
  // loadSource/loadTotal direkt aus snapshot.balance, ohne window.EnergyModel:
  // base.html laedt das Modell nur bedingt.
  function loadMeasured(snapshot) {
    const balance = snapshot && snapshot.balance;
    return (balance && typeof balance.load_measured === 'number') ? balance.load_measured : 0;
  }

  // A measured load is exactly as stale as its own role. A calculated one is
  // as stale as the roles it was derived from - reporting it as fresh would
  // claim more than the data supports.
  const CALCULATED_LOAD_ROLES = ['pv', 'grid', 'grid_import', 'grid_export', 'battery', 'battery_charge', 'battery_discharge'];

  function loadStale(snapshot) {
    if (loadSource(snapshot) === 'measured') return isStale(snapshot, 'load');
    return CALCULATED_LOAD_ROLES.some(role => isStale(snapshot, role));
  }

  // Ursprung war ein 1:1-Port von webui.go's flowDuration() mit fest 3000W
  // Referenz. Die Referenz ist inzwischen ein Parameter (Item.
  // SpeedReferenceMode/-Watts pro Kachel, siehe render()), aus demselben
  // Grund wie strokeWidth()'s reference-Parameter: eine feste Referenz macht
  // Geschwindigkeitsunterschiede bei kleinen Anlagen (z.B. Balkonkraftwerk-
  // Groessenordnung) kaum sichtbar, weil alle realen Werte weit darunter
  // bleiben. Duration min 0.35s, max 2.2s.
  function flowDuration(power, reference) {
    const minimum = 0.35;
    const maximum = 2.2;
    const relative = Math.min(Math.abs(power) / Math.max(reference, 1), 1);
    return maximum - relative * (maximum - minimum);
  }

  const lerp = (min, max, t) => min + (max - min) * t;

  // stroke-width for the "width" scale mode: 2..14px, sqrt curve so small
  // flows stay visible instead of vanishing next to a much bigger one.
  function strokeWidth(power, reference) {
    return lerp(2, 14, Math.sqrt(Math.min(Math.abs(power) / Math.max(reference, 1), 1)));
  }

  const formatPower = value => `${Math.round(value)} W`;

  // Ports overview.html's per-node <output> text/stale-class conditionals.
  //
  // `dir` traegt fuer jeden Knoten den Zustand, den sein Icon anzeigt - genau
  // wie bei 'battery' seit dem Lade-/Entladeicon. step() macht daraus eine
  // Klasse am Icon-Element (is-producing / is-importing / is-exporting /
  // is-live / is-drawing), 'idle' heisst "keine Klasse".
  //
  // Seit Netz und Lasten ein eigenes Icon fuer Richtung bzw. Verbraucher
  // haben, zeigt der sichtbare `text` dort nur noch die Leistung - das Wort
  // ("Bezug"/"Einspeisung", "Wallbox"/"Waermepumpe"/"Gemessen") wandert wie bei
  // der Batterie ins `full`, das step() als aria-label ans <output> haengt.
  function nodeText(id, snapshot) {
    if (id === 'pv') {
      if (!hasValue(snapshot, 'pv')) return {text: '--', stale: false, dir: 'idle'};
      const power = roleValue(snapshot, 'pv');
      return {
        text: formatPower(power),
        stale: isStale(snapshot, 'pv'),
        dir: Math.abs(power) > 0.000001 ? 'producing' : 'idle',
      };
    }
    if (id === 'grid') {
      const importPower = gridImportPower(snapshot);
      const exportPower = gridExportPower(snapshot);
      const stale = isStale(snapshot, 'grid') || isStale(snapshot, 'grid_import') || isStale(snapshot, 'grid_export');
      const dir = importPower > 0 ? 'importing' : exportPower > 0 ? 'exporting' : 'idle';
      if (importPower > 0) return {text: formatPower(importPower), stale, dir, full: `Bezug ${formatPower(importPower)}`};
      if (exportPower > 0) return {text: formatPower(exportPower), stale, dir, full: `Einspeisung ${formatPower(exportPower)}`};
      if (hasValue(snapshot, 'grid')) return {text: formatPower(0), stale, dir};
      if (hasValue(snapshot, 'grid_import')) return {text: formatPower(0), stale, dir, full: `Bezug ${formatPower(0)}`};
      if (hasValue(snapshot, 'grid_export')) return {text: formatPower(0), stale, dir, full: `Einspeisung ${formatPower(0)}`};
      return {text: '--', stale: false, dir: 'idle'};
    }
    if (id === 'home') {
      // Die stille Icon-Variante (Umriss steht, gestrichelt) gibt es nur, wenn
      // gar kein Hausverbrauch ableitbar ist. Gemessen, berechnet UND
      // kombiniert lassen den Umriss-Lauf laufen.
      if (loadSource(snapshot) === 'missing') return {text: '--', stale: false, dir: 'idle'};
      return {text: formatPower(loadTotal(snapshot)), stale: loadStale(snapshot), dir: 'live'};
    }
    if (id === 'battery') {
      // Nets charge/discharge into one signed value first (batteryNetPower),
      // same as the FLOWS entries above - two split sensors reporting at once
      // must produce one direction here too, or the label would contradict
      // whichever single arrow is actually animating.
      const net = batteryNetPower(snapshot);
      let stale = isStale(snapshot, 'battery') || isStale(snapshot, 'battery_charge') || isStale(snapshot, 'battery_discharge');
      // `dir` drives the battery-flow-icon's fill animation (see render());
      // `word` is what used to be the leading text ("Laden"/"Entladen") and
      // now only survives in `full`, the aria-label for the icon - the word
      // itself is no longer part of the visible `text`.
      let dir = 'idle';
      let word = '';
      let text;
      if (net < 0) { dir = 'discharging'; word = 'Entladen'; text = formatPower(-net); }
      else if (net > 0) { dir = 'charging'; word = 'Laden'; text = formatPower(net); }
      else if (hasValue(snapshot, 'battery')) text = formatPower(0);
      else if (hasValue(snapshot, 'battery_discharge')) { dir = 'discharging'; word = 'Entladen'; text = formatPower(0); }
      else if (hasValue(snapshot, 'battery_charge')) { dir = 'charging'; word = 'Laden'; text = formatPower(0); }
      else text = '--';
      // battery_soc (Fuellstand) is independent of the power flow above - only
      // present in snapshot.values when at least one entity carries a
      // capacity_kwh (see energie-interpretation.md). Returned as its own
      // `soc` field and rendered on a dedicated line under the power value
      // (.energy-flow-battery-soc), not appended to `text` - the power
      // reading stays a clean number.
      let soc = '';
      if (hasValue(snapshot, 'battery_soc')) {
        soc = `${Math.round(roleValue(snapshot, 'battery_soc'))} %`;
        stale = stale || isStale(snapshot, 'battery_soc');
      }
      const label = word ? `${word} ${text}` : text;
      const full = soc ? (text === '--' ? `Füllstand ${soc}` : `${label} · Füllstand ${soc}`) : label;
      return {text, stale, dir, soc, full};
    }
    // load node ("Lasten")
    // Text UND Icon folgen dem aktiven Verbraucher mit der hoechsten Leistung.
    // `dir` ist 'drawing', sobald er wirklich Leistung zieht; `icon` waehlt die
    // Grafik ('wallbox' / 'heatpump' / 'generic'), die step() ins SVG setzt.
    // (Idee, alle drei Grafiken zugleich zu zeigen: dashboard-ideenliste P2.8.)
    const drawingIf = power => (Math.abs(power) > 0.000001 ? 'drawing' : 'idle');
    // Der gemessene Teilverbrauch (Kombiniert-Modus) hat Vorrang: er steht
    // sonst nirgends, Wallbox und Waermepumpe haben ihre eigenen Knoten.
    if (loadMeasured(snapshot) > 0.5) {
      const power = loadMeasured(snapshot);
      return {text: formatPower(power), stale: isStale(snapshot, 'load'), dir: 'drawing', icon: 'generic', full: `Gemessen ${formatPower(power)}`};
    }
    const named = [
      {label: 'Wallbox', role: 'wallbox', icon: 'wallbox'},
      {label: 'Wärmepumpe', role: 'heat_pump', icon: 'heatpump'},
    ].filter(consumer => hasValue(snapshot, consumer.role));
    if (named.length) {
      const top = named.reduce((a, b) =>
        (Math.abs(roleValue(snapshot, b.role)) > Math.abs(roleValue(snapshot, a.role)) ? b : a));
      const power = roleValue(snapshot, top.role);
      return {text: formatPower(power), stale: isStale(snapshot, top.role), dir: drawingIf(power), icon: top.icon, full: `${top.label} ${formatPower(power)}`};
    }
    if (hasValue(snapshot, 'load')) {
      // Kein eigener Verbraucher, nur die rohe load-Rolle: hier stand noch nie
      // eine Zahl (die traegt der Haus-Knoten), das Wort bleibt.
      return {text: 'Hausverbrauch', stale: isStale(snapshot, 'load'), dir: drawingIf(roleValue(snapshot, 'load')), icon: 'generic'};
    }
    return {text: '--', stale: false, dir: 'idle', icon: 'generic'};
  }

  // Die Verbraucher, die die Lasten-Kachel als Liste zeigt: die gemessenen
  // Teilverbraucher (Kombiniert-Modus) bzw. die benannten Rollen Wallbox und
  // Waermepumpe, dazu eine Sammelzeile "Uebriger Verbrauch" fuer den Rest.
  // `icon` waehlt eine der LOAD_ICON_MARKUP-Varianten. hideInactive wirft
  // Zeilen unter MIN_FLOW_W raus. Sortiert nach Leistung - die groesste Zeile
  // steht oben, das ist dieselbe Auswahl, die nodeText('load') als Einzelwert
  // liefert.
  function loadConsumers(snapshot, hideInactive) {
    const rows = [];
    const add = (label, role, icon, value) => {
      const power = Math.abs(value);
      const drawing = power > MIN_FLOW_W;
      if (hideInactive && !drawing) return;
      rows.push({label, icon, power: drawing ? power : 0, dir: drawing ? 'drawing' : 'idle', stale: isStale(snapshot, role)});
    };
    const measured = loadMeasured(snapshot);
    if (measured > 0.5) add('Gemessen', 'load', 'generic', measured);
    const named = [
      {label: 'Wallbox', role: 'wallbox', icon: 'wallbox'},
      {label: 'Wärmepumpe', role: 'heat_pump', icon: 'heatpump'},
    ].filter(consumer => hasValue(snapshot, consumer.role));
    let namedSum = 0;
    for (const consumer of named) {
      const value = roleValue(snapshot, consumer.role);
      namedSum += Math.abs(value);
      add(consumer.label, consumer.role, consumer.icon, value);
    }
    if (loadSource(snapshot) !== 'missing') {
      const rest = loadTotal(snapshot) - namedSum - Math.max(measured, 0);
      if (rows.length === 0) add('Hausverbrauch', 'load', 'generic', loadTotal(snapshot));
      else if (rest > MIN_FLOW_W) add('Übriger Verbrauch', 'load', 'generic', rest);
    }
    if (rows.length === 0) rows.push({label: 'Hausverbrauch', icon: 'generic', power: 0, dir: 'idle', stale: false, missing: true});
    rows.sort((a, b) => b.power - a.power);
    return rows;
  }

  // Baut die <li>-Zeilen der Lasten-Kachel aus loadConsumers() nach und haelt
  // sie zwischen zwei Bildern in Deckung - dieselbe Spar-Logik wie der
  // Einzel-Icon-Pfad in step(): SVG nur bei Variantenwechsel neu setzen,
  // Text/Klassen nur bei echter Aenderung.
  function renderLoadList(listEl, rows) {
    while (listEl.children.length > rows.length) listEl.removeChild(listEl.lastChild);
    while (listEl.children.length < rows.length) {
      const li = document.createElement('li');
      li.className = 'energy-flow-load-row';
      li.innerHTML = '<svg class="energy-flow-node-icon energy-flow-load-icon" viewBox="0 0 22 12" aria-hidden="true" focusable="false"></svg><span class="energy-flow-node-text"></span>';
      listEl.appendChild(li);
    }
    rows.forEach((row, index) => {
      const li = listEl.children[index];
      const icon = li.querySelector('.energy-flow-load-icon');
      const textEl = li.querySelector('.energy-flow-node-text');
      if (icon.dataset.loadIcon !== row.icon && LOAD_ICON_MARKUP[row.icon]) {
        icon.innerHTML = LOAD_ICON_MARKUP[row.icon];
        icon.dataset.loadIcon = row.icon;
      }
      for (const state of ICON_STATE_CLASSES) icon.classList.toggle(state, state === `is-${row.dir}`);
      const text = row.missing ? '--' : formatPower(row.power);
      if (textEl.textContent !== text) textEl.textContent = text;
      li.classList.toggle('energy-flow-value-stale', row.stale);
      const label = row.dir === 'drawing' ? `${row.label} ${formatPower(row.power)}` : row.label;
      if (li.getAttribute('aria-label') !== label) li.setAttribute('aria-label', label);
    });
  }

  // Point where the line from `center` towards `towards` crosses the
  // rectangle boundary of a node sized `size` around `center` - i.e. "cut at
  // the node's edge, not its middle".
  function edgePoint(center, size, towards) {
    const dx = towards.x - center.x;
    const dy = towards.y - center.y;
    if (!dx && !dy) return {x: center.x, y: center.y};
    const hw = size.w / 2;
    const hh = size.h / 2;
    const scaleX = dx ? Math.abs(hw / dx) : Infinity;
    const scaleY = dy ? Math.abs(hh / dy) : Infinity;
    const scale = Math.min(scaleX, scaleY);
    return {x: center.x + dx * scale, y: center.y + dy * scale};
  }

  // Statt fester px-Konstanten: die Kartenregel ist die einzige Wahrheit ueber
  // die Knotengroesse. Bei groesserer Grundschrift wachsen Knoten und
  // Linienendpunkte damit gemeinsam.
  function nodeSize(cardEl) {
    const style = getComputedStyle(cardEl);
    const px = name => parseFloat(style.getPropertyValue(name)) * (parseFloat(getComputedStyle(document.documentElement).fontSize) || 16);
    return { w: px('--flow-node-w'), h: px('--flow-node-h') };
  }

  function nodeSizeFor(id, cardEl) {
    if (!cardEl) return sizeFor(id);
    const baseSize = nodeSize(cardEl);
    if (id === 'home') {
      const style = getComputedStyle(cardEl);
      const px = name => parseFloat(style.getPropertyValue(name)) * (parseFloat(getComputedStyle(document.documentElement).fontSize) || 16);
      return { w: px('--flow-node-home-w'), h: px('--flow-node-home-h') };
    }
    return baseSize;
  }

  // Haelt einen Knotenmittelpunkt so weit vom Rand weg, dass seine ganze
  // Flaeche im Kartenbereich bleibt - die Aufgabe, die frueher die festen
  // 15%/85% von Hand erledigten, jetzt aus der echten Knotengroesse abgeleitet
  // (haelt also auch bei groesserer Grundschrift oder sehr flacher Karte).
  function clampCenter(cx, cy, size, width, height) {
    const hw = size.w / 2 + 4;
    const hh = size.h / 2 + 4;
    return {
      x: width >= 2 * hw ? Math.max(hw, Math.min(width - hw, cx)) : width / 2,
      y: height >= 2 * hh ? Math.max(hh, Math.min(height - hh, cy)) : height / 2,
    };
  }

  // Pure function: normalized node coordinates -> pixel geometry for a
  // width x height container. Returns node centers plus one quadratic-bezier
  // path per connection, running edge-to-edge between node and Gebaeude. The
  // control point sits on the perpendicular bisector, offset by `sign` - that
  // single gentle bow is the whole routing.
  function geometry(width, height, cardEl) {
    const layout = nodeLayoutFor(width, height);
    // Einwaerts-Biegung, deren *absolute* Tiefe (bowFactor * length) mit der
    // Breite nicht mitwaechst: 0.22 im normalen Querformat, sanft flacher, je
    // breiter die Kachel wird - sonst laufen PV/Speicher (bzw. Netz/Verbrauch)
    // vor dem Gebaeude ineinander. Ab WIDE_BOW_RATIO kippt sie ganz nach aussen.
    const bowFactor = width > height * WIDE_BOW_RATIO
      ? -0.14
      : Math.min(0.22, 0.42 * height / width);
    const nodes = NODE_IDS.map(id => {
      const clamped = clampCenter(layout[id].x * width, layout[id].y * height, nodeSizeFor(id, cardEl), width, height);
      return {id, x: clamped.x, y: clamped.y};
    });
    const centerOf = id => nodes.find(node => node.id === id);
    const paths = {};
    for (const conn of CONNECTIONS) {
      const fromCenter = centerOf(conn.from);
      const toCenter = centerOf(conn.to);
      const start = edgePoint(fromCenter, nodeSizeFor(conn.from, cardEl), toCenter);
      const end = edgePoint(toCenter, nodeSizeFor(conn.to, cardEl), fromCenter);
      const length = Math.hypot(end.x - start.x, end.y - start.y) || 1;
      const nx = -(end.y - start.y) / length;
      const ny = (end.x - start.x) / length;
      const k = bowFactor * length * (conn.sign || 0);
      const ctrlX = (start.x + end.x) / 2 + nx * k;
      const ctrlY = (start.y + end.y) / 2 + ny * k;
      paths[conn.id] = `M ${start.x.toFixed(1)} ${start.y.toFixed(1)} Q ${ctrlX.toFixed(1)} ${ctrlY.toFixed(1)} ${end.x.toFixed(1)} ${end.y.toFixed(1)}`;
    }
    return {nodes, paths};
  }

  // --- Darstellungszustand, der den htmx-Tausch ueberlebt -----------------
  //
  // #overview-live wird bei jedem SSE-registry-Event komplett per outerHTML
  // ersetzt (dashboard.js refreshLiveFragment()), diese Karte also mehrmals
  // pro Minute abgerissen und neu gebaut. Alles, was am Alpine-Objekt haengt,
  // faengt darum bei jedem Update wieder bei null an: jede Zahl springt auf
  // ihren neuen Wert, und die Laufschrift der Flusslinien schnappt zurueck
  // auf Phase 0. Apples Regel dafuer lautet, immer vom *Darstellungswert* zu
  // animieren - dem, was in diesem Moment auf dem Schirm steht - nie vom
  // Zielwert. Deshalb liegen die Darstellungswerte hier draussen im Modul,
  // pro Kachel abgelegt, und die frisch montierte Komponente macht da weiter,
  // wo ihre Vorgaengerin aufgehoert hat.
  //
  // Die Federmathematik und die Umlaufphasen-Buchhaltung liegen seit 2026-08
  // im gemeinsamen Modul energy-presentation.js - dieselbe Mechanik gilt fuer
  // alle sieben Energiekacheln. Was hier bleibt, ist reine Kartenstruktur:
  // die Federn selbst (Werte UND Deckkraft, beide muessen den Tausch
  // ueberleben) und die Bildschleife, die sie vorruecken - test/energyflow.
  // page.test.mjs treibt step() direkt und synchron, ohne rAF, das gemeinsame
  // Modul stellt dafuer keinen Zugriff bereit.
  const cardState = new Map();
  const presentation = () => window.EnergyPresentation;

  const clock = () => (window.performance && window.performance.now ? window.performance.now() : Date.now());

  function stateFor(key) {
    let state = cardState.get(key);
    if (!state) {
      // `springs` sind die Zahlen aus dem Schnappschuss, `opacity` die
      // Sichtbarkeit je Fluss. Zwei Karten, weil sync() alles wegraeumt, was
      // der neue Schnappschuss nicht mehr nennt - die Deckkraft-Federn
      // gehoeren aber zu den acht festen Fluessen, nicht zu den Rollen.
      state = {springs: new Map(), opacity: new Map(), seeded: false, tickedAt: -1, moving: false};
      cardState.set(key, state);
    }
    return state;
  }

  // Die Deckkraft einer Linie laeuft ueber eine eigene Feder, nicht ueber den
  // Leistungsbetrag: die Strichstaerke traegt schon die Leistung, die
  // Deckkraft traegt allein "da oder nicht da". Getrennt gehalten heisst,
  // dass ein Fluss, der die Richtung wechselt (Batterie, Netz), sauber
  // ueberblendet - der alte Pfeil geht aus, waehrend der neue aufkommt.
  // Aus dem Betrag abgeleitet ginge das nicht: beim Wechsel federt der Wert
  // in unter einem Einzelbild durch die Null, der neue Pfeil stuende also
  // sofort voll da.
  const IDLE_OPACITY = .16;
  const NEUTRAL_OPACITY = .8;
  const ACTIVE_OPACITY = .95;
  // Eigene Ruheschwellen: die Voreinstellung ist in Watt gedacht und wuerde
  // den ganzen Deckkraftbereich in einem Schritt ueberspringen.
  const SETTLE_OPACITY = .002;
  const SETTLE_OPACITY_VELOCITY = .01;

  // Jeder Schreibzugriff hier laeuft, solange ein Wert einschwingt, bis zu
  // 60-mal pro Sekunde. Die Waechter halten den Browser davon ab, fuer eine
  // Zeichenkette, die er schon hat, neu zu parsen oder neu zu layouten.
  const setAttr = (el, name, value) => { if (el.getAttribute(name) !== value) el.setAttribute(name, value); };
  const setStyle = (el, name, value) => { if (el.style.getPropertyValue(name) !== value) el.style.setProperty(name, value); };

  // Eine Bildschleife fuer alle montierten Flusskarten. Sie laeuft nur,
  // solange irgendeine Feder noch unterwegs ist, und haelt an, sobald alles
  // eingeschwungen ist - eine Kachel, die still steht, kostet nichts.
  const instances = new Set();
  let frameHandle = null;
  let frameAt = 0;

  function scheduleFrame() {
    if (frameHandle !== null || typeof window.requestAnimationFrame !== 'function') return;
    frameAt = clock();
    frameHandle = window.requestAnimationFrame(runFrame);
  }

  function runFrame() {
    frameHandle = null;
    const now = clock();
    // Gedeckelt: ein Tab, der eine Minute im Hintergrund lag, bekommt ein
    // einzelnes Bild mit einer Minute dt. Ohne Deckel wuerde jede Feder in
    // diesem einen Schritt auf ihr Ziel springen - genau der Sprung, gegen
    // den diese ganze Mechanik gebaut ist. Mit Deckel federt die Grafik
    // stattdessen sichtbar an ihren Platz.
    const dt = Math.min(Math.max((now - frameAt) / 1000, 0), .25);
    frameAt = now;
    let moving = false;
    for (const instance of instances) {
      // Nach dem outerHTML-Tausch haengt die alte Karte noch hier, ist aber
      // nicht mehr im Dokument. Sie muss *vor* dem Tick raus, sonst schiebt
      // sie dieselben Federn im selben Bild ein zweites Mal weiter.
      if (!instance.alive()) { instances.delete(instance); continue; }
      if (instance.step(dt, now)) moving = true;
    }
    if (moving) scheduleFrame();
  }

  const energyFlowCard = () => {
    // Kept outside the reactive Alpine object on purpose, same reasoning as
    // devicemap.page.js's `let cy = null`: these are plain platform handles
    // and per-mount scratch state, not view state.
    let resizeObserver = null;
    let container = null;
    let svg = null;
    let nodeElements = new Map();
    let pathElements = new Map();
    let layout = null;          // letztes geometry(), aendert sich nur bei Resize
    let targets = new Map();    // Zielwerte des aktuellen Schnappschusses
    let options = null;         // Kachel-Einstellungen, einmal beim Mount gelesen
    let reducedMotion = false;
    // Welche Fluesse an *diesem* Element schon auf die laufende Phase
    // eingehaengt wurden, und mit welcher Umlaufzeit. Pro Mount, nicht pro
    // Kachel: die Phase ueberlebt den Tausch (energy-presentation.js), das
    // <path>, das sie anzeigt, nicht - ein neu erzeugtes Element muss also
    // einmal neu verankert werden, aber nicht bei jedem Bild neu.
    const anchored = new Map();
    let key = 'energy-flow';
    let handle = null;
    let presenter = null;

    const alive = () => !!(container && container.isConnected);

    return {
      snapshot: null,

      init() {
        // Bewusst nicht ueber EnergyModel.readEmbeddedSnapshot: base.html
        // laedt energy-flow.js unbedingt, energy-model.js aber nur, wenn eine
        // der sechs neueren Energiekarten im Layout steht. Ein Layout mit
        // ausschliesslich der Flusskarte haette dann kein window.EnergyModel.
        // Der Rueckfall ist derselbe wie dort.
        const initial = document.getElementById('energy-flow-initial') || document.getElementById('energy-snapshot-initial');
        if (initial) {
          try {
            this.snapshot = JSON.parse(initial.textContent || '{}');
          } catch (error) {
            this.snapshot = null;
          }
        }
        container = this.$refs.map || null;
        svg = this.$refs.svg || null;
        reducedMotion = !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);

        // FlowScale ("width"/"speed") und die Geschwindigkeitsreferenz stehen
        // beide auf der umgebenden Rasterkachel (Layout-Editor, "Leistungs-
        // Darstellung"), nicht auf der Wurzel dieser Karte. Reduzierte
        // Bewegung erzwingt "width", unabhaengig von dieser Einstellung.
        const layoutItem = this.$root.closest('[data-layout-item-id]')?.dataset;
        key = layoutItem?.layoutItemId || 'energy-flow';
        options = {
          scaleMode: reducedMotion ? 'width' : layoutItem?.flowScale || 'width',
          speedReferenceMode: layoutItem?.speedReferenceMode,
          speedReferenceWatts: Number(layoutItem?.speedReferenceWatts) || 1000,
          hideInactiveConsumers: layoutItem?.hideInactive === 'on',
        };

        this.sync();
        if (container && window.ResizeObserver) {
          resizeObserver = new ResizeObserver(() => this.render());
          resizeObserver.observe(container);
        }
        handle = {alive, step: (dt, now) => this.step(dt, now)};
        instances.add(handle);
        // dashboard.js ueberspringt den vollen #overview-live-Tausch, sobald
        // der SSE-Push jede Kachel im Raster bedient (liveGridPushCovers()) -
        // fuer ein reines Energie-Layout ist das der Regelfall, nicht die
        // Ausnahme. Ohne dieses Abo saehe diese Karte danach nie wieder einen
        // frischen Schnappschuss: init() liest ihn nur einmal. `raw` statt der
        // schon gefederten Praesentation (siehe energy-presentation.js), weil
        // diese Karte ihre Werte selbst federt (eigene Opacity-Federn, eigene
        // Phasen-Verankerung) - eine zweite Federung obendrauf waere falsch.
        presenter = presentation().present({
          key,
          raw: this.snapshot,
          reducedMotion,
          alive,
          apply: (_presented, raw) => {
            if (!raw || raw === this.snapshot) return;
            this.snapshot = raw;
            this.sync();
            this.render();
          },
        });
        this.render();
      },

      // Richtet die Federn auf den soeben empfangenen Schnappschuss aus. Die
      // Federn selbst bleiben unangetastet - genau das ist der Sinn: ein Wert,
      // der mitten im Flug eintrifft, zielt die Bewegung neu, statt sie
      // abzuschneiden.
      sync() {
        const state = stateFor(key);
        targets = presentation().animatable(this.snapshot);
        for (const [name, target] of targets) {
          if (state.springs.has(name)) continue;
          // Eine Rolle, die eben noch nicht auf dem Schirm war. Beim allerersten
          // Mount ist alles neu und muss einfach dastehen - sonst waere der
          // Seitenaufbau selbst eine Animation. Taucht eine Rolle spaeter zum
          // ersten Mal auf, steigt sie aus der Null hoch, was sich wie ein
          // Ankommen liest statt wie ein Aufblitzen.
          state.springs.set(name, {value: state.seeded && !reducedMotion ? 0 : target, velocity: 0});
        }
        for (const name of [...state.springs.keys()]) {
          if (!targets.has(name)) state.springs.delete(name);
        }
        // Reduzierte Bewegung heisst hier: der Messwert steht sofort richtig
        // da. Fuer eine Zahlenanzeige ist das die passende, nicht-vestibulaere
        // Entsprechung - eine Ersatzbewegung waere hier keine Hilfe.
        if (reducedMotion) {
          for (const [name, target] of targets) Object.assign(state.springs.get(name), {value: target, velocity: 0});
        }
        state.seeded = true;
      },

      // Misst neu und zeichnet: init() und der ResizeObserver rufen das, die
      // Bildschleife nicht - die Geometrie haengt allein an der Containergroesse.
      render() {
        if (!this.snapshot || !container || !svg) return;
        const width = container.clientWidth;
        const height = container.clientHeight;
        if (!width || !height) return;
        setAttr(svg, 'viewBox', `0 0 ${width} ${height}`);

        if (!svg.dataset.flowsBuilt) {
          for (const conn of CONNECTIONS) {
            const path = document.createElementNS(SVG_NS, 'path');
            path.dataset.flow = conn.id;
            svg.appendChild(path);
          }
          svg.dataset.flowsBuilt = 'true';
        }
        // Einmal pro Mount nachschlagen statt fuenfmal pro Bild: das DOM
        // dieser Karte steht zwischen zwei htmx-Tauschen fest.
        //
        // Jeder Knoten traegt inzwischen ein animiertes Icon im <output>
        // (.energy-flow-node-icon) mit eigenem Textknoten
        // (.energy-flow-node-text) statt reinem output.textContent. Das
        // Batterie-Icon fuehrt zusaetzlich seine eigenen Klassen
        // (.energy-flow-battery-icon/-text), die hier aber nicht mehr gebraucht
        // werden. textEl faellt auf output selbst zurueck, falls ein Knoten
        // (aeltere Fixtures) doch keinen Textknoten hat.
        nodeElements = new Map(NODE_IDS.map(id => {
          const el = container.querySelector(`[data-node="${id}"]`);
          if (!el) return [id, null];
          const output = el.querySelector('output');
          const icon = output && output.querySelector('.energy-flow-node-icon');
          const textEl = (output && output.querySelector('.energy-flow-node-text')) || output;
          // Lasten-Knoten: statt eines festen Icons eine Liste, die step() aus
          // loadConsumers() nachzieht (Wallbox / Waermepumpe / Sammelzeile).
          const list = output && output.querySelector('.energy-flow-load-list');
          const socEl = output && output.querySelector('.energy-flow-battery-soc');
          return [id, {el, output, icon, textEl, list, socEl}];
        }));
        pathElements = new Map(CONNECTIONS.map(conn => [conn.id, svg.querySelector(`[data-flow="${conn.id}"]`)]));

        layout = geometry(width, height, container);
        // dt 0: die geschlossene Federform laesst Wert und Geschwindigkeit
        // dabei exakt unveraendert, es ist also dieselbe Schreibphase wie im
        // Bild der Schleife, nur ohne Zeitfortschritt.
        if (this.step(0, clock())) scheduleFrame();
      },

      // Federn vorruecken, dann zeichnen. Gibt zurueck, ob noch etwas
      // unterwegs ist - daran haengt, ob die Schleife weiterlaeuft.
      step(dt, now) {
        if (!this.snapshot || !layout) return false;
        const state = stateFor(key);
        // Die Federn gehoeren der Kachel, nicht dieser Instanz: pro Bild darf
        // nur einmal vorgerueckt werden, sonst schoebe eine zweite lebende
        // Karte dieselben Werte im selben Bild ein zweites Mal weiter.
        const advancing = dt > 0 && state.tickedAt !== now;
        if (advancing) state.tickedAt = now;
        // `moving` wird aus dem Federzustand abgelesen statt aus dem Ergebnis
        // des Vorrueckens: render() ruft mit dt 0, rueckt also nichts vor,
        // muss aber trotzdem melden, dass noch etwas aussteht - sonst spraenge
        // die Schleife nach einem Tausch gar nicht erst an. advanceSpring()
        // rastet beim Einschwingen exakt auf das Ziel, der Vergleich taugt
        // also als Abbruchbedingung.
        let moving = false;
        for (const [name, target] of targets) {
          const spring = state.springs.get(name);
          if (!spring) continue;
          if (advancing) presentation().advanceSpring(spring, target, dt);
          if (spring.value !== target) moving = true;
        }
        // Ab hier arbeitet alles auf den *dargestellten* Werten, nicht auf dem
        // Schnappschuss - nodeText() und die FLOWS-Praedikate eingeschlossen.
        // Ein Richtungswechsel der Batterie laeuft dadurch als Bewegung ab:
        // der Wert federt durch die Null, Beschriftung und Strichstaerke
        // folgen ihm, und die Deckkraftfeder unten blendet dabei den einen
        // Pfeil aus und den anderen auf.
        const presented = presentation().withPresentedValues(this.snapshot, state.springs);

        for (const node of layout.nodes) {
          const entry = nodeElements.get(node.id);
          if (!entry) continue;
          const {el, output, icon, textEl} = entry;
          setStyle(el, 'left', `${node.x}px`);
          setStyle(el, 'top', `${node.y}px`);
          if (!output) continue;
          if (node.id === 'load' && entry.list) {
            renderLoadList(entry.list, loadConsumers(presented, options.hideInactiveConsumers));
            continue;
          }
          const {text, stale, dir, full, soc, icon: iconVariant} = nodeText(node.id, presented);
          if (textEl.textContent !== text) textEl.textContent = text;
          // Batterie-Fuellstand: eigene Zeile unter dem Wert, ausgeblendet
          // solange kein battery_soc bekannt ist (nodeText liefert soc = '').
          if (entry.socEl) {
            const socText = soc || '';
            if (entry.socEl.textContent !== socText) entry.socEl.textContent = socText;
            entry.socEl.hidden = socText === '';
          }
          output.classList.toggle('energy-flow-value-stale', stale);
          // Separate CSS-class channel rather than appending to `text`
          // itself: nodeText('home', ...) is asserted verbatim elsewhere
          // (formatPower output only) - a text suffix would break that
          // contract. Same technique already used for `stale` above.
          const source = loadSource(presented);
          output.classList.toggle('energy-flow-value-calculated', node.id === 'home' && (source === 'calculated' || source === 'combined'));
          // Jedes Knoten-Icon: die Animation in base.css haengt an einer
          // Zustandsklasse (is-charging/-discharging/-producing/-importing/
          // -exporting/-live/-drawing), genau eine davon ist gesetzt, 'idle'
          // hat keine. Das fruehere Wort ("Laden"/"Entladen") steht bei der
          // Batterie nur noch im aria-label.
          if (icon) {
            // Lasten-Knoten: das SVG-Innere wechselt mit dem aktiven
            // Verbraucher, aber nur wenn sich die Auswahl wirklich aendert.
            if (iconVariant && icon.dataset.loadIcon !== iconVariant && LOAD_ICON_MARKUP[iconVariant]) {
              icon.innerHTML = LOAD_ICON_MARKUP[iconVariant];
              icon.dataset.loadIcon = iconVariant;
            }
            const wanted = `is-${dir}`;
            for (const state of ICON_STATE_CLASSES) icon.classList.toggle(state, state === wanted);
            const label = full === undefined ? text : full;
            if (output.getAttribute('aria-label') !== label) output.setAttribute('aria-label', label);
            // Fuellstand als Ruhe-Pose des Batterie-Icons: auf 5 Stufen
            // gerundet, damit "eingefroren" klar von der laufenden Animation
            // unterscheidbar ist (base.css .energy-flow-battery-icon:not(...)).
            if (node.id === 'battery') {
              const soc = Math.max(0, Math.min(100, roleValue(presented, 'battery_soc')));
              setStyle(icon, '--energy-battery-soc', hasValue(presented, 'battery_soc') ? (Math.round(soc / 20) / 5).toFixed(2) : '0');
            }
          }
        }

        const states = CONNECTIONS.map(conn => [conn, conn.state(presented)]);
        const activeValues = states.filter(([, st]) => st.kind === 'active').map(([, st]) => st.value);
        const reference = Math.max(1000, ...activeValues, 0);
        // Item.SpeedReferenceMode/-Watts, vom Server als data-Attribute auf
        // dieselbe Kachel gerendert wie data-flow-scale. "relative" spiegelt
        // reference oben, nur mit einstellbarem statt fest 1000W Boden.
        const speedReference = options.speedReferenceMode === 'fixed'
          ? options.speedReferenceWatts
          : Math.max(options.speedReferenceWatts, ...activeValues, 0);

        for (const [conn, st] of states) {
          const path = pathElements.get(conn.id);
          if (!path) continue;
          const active = st.kind === 'active';
          const neutral = st.kind === 'neutral';
          // Eine Linie je Verbindung: is-reversed dreht die Marschanimation um
          // (animation-direction: reverse), das ist der ganze Unterschied
          // zwischen Bezug/Einspeisung bzw. Laden/Entladen.
          setAttr(path, 'class', `energy-flow-line ${conn.colorClass}`
            + (active ? ' is-active' : neutral ? ' is-neutral' : '')
            + (active && st.reversed ? ' is-reversed' : ''));
          setAttr(path, 'd', layout.paths[conn.id]);

          const target = active ? ACTIVE_OPACITY : neutral ? NEUTRAL_OPACITY : IDLE_OPACITY;
          let opacity = state.opacity.get(conn.id);
          if (!opacity) {
            opacity = {value: target, velocity: 0};
            state.opacity.set(conn.id, opacity);
          } else if (reducedMotion) {
            Object.assign(opacity, {value: target, velocity: 0});
          } else {
            if (advancing) presentation().advanceSpring(opacity, target, dt, SETTLE_OPACITY, SETTLE_OPACITY_VELOCITY);
            if (opacity.value !== target) moving = true;
          }
          // Kritisch gedaempft heisst kein Ueberschwingen ins Ziel, aber eine
          // Feder, die mitten in der Gegenbewegung umgelenkt wird, laeuft ein
          // Stueck weiter - geklemmt, damit daraus nie eine ungueltige
          // Deckkraft wird.
          setStyle(path, '--energy-flow-opacity', Math.max(0, Math.min(1, opacity.value)).toFixed(3));

          const animates = active && !reducedMotion;
          if (active && options.scaleMode === 'speed') {
            path.style.removeProperty('stroke-width');
            setStyle(path, '--energy-flow-duration', `${flowDuration(st.value, speedReference).toFixed(2)}s`);
          } else if (active) {
            setStyle(path, 'stroke-width', strokeWidth(st.value, reference).toFixed(2));
            path.style.removeProperty('--energy-flow-duration');
          } else {
            path.style.removeProperty('stroke-width');
            path.style.removeProperty('--energy-flow-duration');
          }
          // Die Phase haengt an der Umlaufzeit, die die Linie gerade wirklich
          // faehrt: im Modus "speed" die gerechnete, sonst der CSS-Rueckfall.
          // Nur schreiben, wenn wirklich neu verankert werden muss (frischer
          // Fluss oder Umlaufzeit geaendert) - sonst laeuft die CSS-Animation
          // frei weiter, und ein Neuschreiben jedes Bild wuerde sie neu
          // starten lassen.
          if (!animates) {
            presentation().pauseDash(key, conn.id);
            anchored.delete(conn.id);
          } else {
            const duration = options.scaleMode === 'speed'
              ? Number(flowDuration(st.value, speedReference).toFixed(2))
              : presentation().DEFAULT_DASH_SECONDS;
            if (anchored.get(conn.id) !== duration) {
              // dashDelay() erhaelt bewusst das Vorzeichen von -0 (siehe
              // energy-presentation.test.mjs) - fuer diese Karte aber, deren
              // eigener Bestandstest von vor diesem Modul stammt, ist "0.000s"
              // die vereinbarte Form. Beides meint dieselbe Verzoegerung.
              const delay = presentation().dashDelay(key, conn.id, duration);
              path.style.setProperty('animation-delay', delay === '-0.000s' ? '0.000s' : delay);
              anchored.set(conn.id, duration);
            }
          }
        }

        state.moving = moving;
        return moving;
      },

      destroy() {
        if (resizeObserver) resizeObserver.disconnect();
        if (handle) instances.delete(handle);
        if (presenter) presenter.release();
      },
    };
  };

  // Exposed for test/energyflow.page.test.mjs, which loads this file via vm
  // and reaches these through the factory Alpine.data() was called with -
  // same reasoning as any other pure-function unit test, just without a
  // module system to import them through.
  energyFlowCard.geometry = geometry;
  energyFlowCard.strokeWidth = strokeWidth;
  energyFlowCard.flowDuration = flowDuration;
  energyFlowCard.nodeText = nodeText;
  energyFlowCard.loadConsumers = loadConsumers;

  const register = () => {
    if (window.Alpine) window.Alpine.data('energyFlowCard', energyFlowCard);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();

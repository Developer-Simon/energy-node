(() => {
  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json();
    if (!response.ok) throw new Error(body.message || "Anfrage fehlgeschlagen");
    return body;
  };
  const newID = prefix => `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`;

  // GridStack's default renderCB writes node.content via textContent (plain
  // text, XSS-safe by construction but useless for our interactive widgets).
  // Overriding it to innerHTML is GridStack's own documented pattern for
  // custom widget markup (see their html-content demo) - safe here because
  // widgetHTML() escapes every piece of dynamic text it interpolates.
  if (window.GridStack) window.GridStack.renderCB = (el, node) => { if (el && node?.content !== undefined) el.innerHTML = node.content; };

  const escapeHTML = value => String(value).replace(/[&<>"']/g, char => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'}[char]));

  // jsdom 25 kennt die <dialog>-API nicht (showModal/close sind undefined,
  // siehe notify.test.mjs). Im echten Browser gibt es das native modale
  // Verhalten samt Fokusfalle, ::backdrop und Escape; im Test faellt es auf
  // das open-Attribut zurueck - das reicht fuer die Zusicherungen.
  const openDialog = d => { if (typeof d.showModal === 'function') d.showModal(); else d.open = true; };
  const closeDialog = d => { if (typeof d.close === 'function') d.close(); else d.open = false; };

  // Der Kartentyp-Katalog aus GET /api/v1/layout (card_types). Modulweit und
  // ausserhalb des reaktiven Alpine-Objekts, weil Gridstack-Widget-HTML
  // synchron daraus gebaut wird und Alpine dort nichts zu beobachten hat.
  let cardTypes = {};
  const FALLBACK_CARD_TYPE = {min_span: 1, min_width: '14rem', min_height: '6rem', fills_height: false, default_span: '1'};
  const cardType = type => cardTypes[type] || FALLBACK_CARD_TYPE;

  // Spiegelt cardVariantKey() aus cardcatalog.go. Beide muessen denselben
  // String bilden, sonst rechnet der Editor mit einer anderen Mindesthoehe
  // als die Uebersicht.
  const cardKey = item => {
    if (item?.type === 'device' && item?.display === 'compact') return 'device:compact';
    if (item?.type === 'battery_status' && item?.display === 'trajectory') return 'battery_status:trajectory';
    return item?.type || '';
  };

  // Wie cardTypes: das Entitaeten-Auswahlfeld der entity_group-Karte wird
  // synchron aus Widget-HTML gebaut (siehe widgetHTML()), ausserhalb von
  // Alpines reaktivem Zugriff auf this.devices.
  let allDevices = [];
  const allEntityOptions = () => allDevices.flatMap(device =>
    (device.entities || []).map(entity => ({ref: entity.unique_id, label: `${device.name || device.id} / ${entity.name || entity.object_id}`})));

  // Choices.js-Instanzen der entity_group-Mehrfachauswahl, je Grid-Node-ID -
  // dieselbe Bibliothek wie Settings > Darstellung (siehe deren
  // initChoices()/settings.page.js), hier aber auf ein <select> angewendet,
  // das ausserhalb von Alpines Reaktivitaet als rohes widgetHTML() entsteht:
  // init/destroy laeuft darum imperativ an jeder Stelle, die den Inhalt eines
  // entity_group-Widgets neu rendert (renderGroup, addEntityGroup,
  // applyColumns), statt einmalig ueber $refs wie im Settings-Panel.
  const entityChoicesInstances = new Map();

  const destroyEntityChoicesFor = id => {
    const instance = entityChoicesInstances.get(id);
    if (!instance) return;
    instance.destroy();
    entityChoicesInstances.delete(id);
  };

  const initEntityChoicesFor = (id, contentEl) => {
    if (!window.Choices) return;
    const select = contentEl?.querySelector?.('[data-role="entity-refs"]');
    if (!select) return;
    destroyEntityChoicesFor(id);
    entityChoicesInstances.set(id, new window.Choices(select, {
      removeItemButton: true,
      shouldSort: false,
      searchResultLimit: 30,
      placeholderValue: 'Entität suchen ...',
      noResultsText: 'Keine Treffer',
      noChoicesText: 'Keine Entitäten verfügbar',
      itemSelectText: '',
    }));
  };

  const SPAN_OPTIONS = ['1', '2', '3', '4', '5', '6', 'full'];
  const spanLabel = span => (span === 'full' ? 'voll' : span);

  // Spiegelt repeat(auto-fill, minmax(18rem, 1fr)) mit gap .8rem. Der Editor
  // zeigt damit dieselbe Spaltenzahl wie die Uebersicht bei gleicher Breite -
  // nicht dieselbe wie der Monitor des Betrachters, sondern die des
  // Editorbereichs.
  const columnsFor = width => {
    const rem = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
    return Math.max(1, Math.floor((width + .8 * rem) / (18 * rem + .8 * rem)));
  };

  // Zielbreite mit inlinierter Rechnung (rem=16): columnCount bei echter Geraetebreite.
  const targetColumns = width => Math.max(1, Math.floor((width + 12.8) / 300.8));

  // Setzt --target-w und --target-zoom auf .layout-canvas-stage fuer optische
  // Verkleinerung bei echter Pixelrechnung. width=0 ist die Editorbreite: sie
  // raeumt beide Eigenschaften wieder ab, sonst liesse sich der Schalter nur
  // in eine Richtung bedienen.
  //
  // zoom wird bei 1 gekappt: passt die Zielbreite in den Editorbereich, steht
  // die Buehne in ihrer echten Groesse (mittig, mit Rahmen). Ohne die Kappung
  // wurde die 390px-Vorschau auf 1376px Editorbreite um das 3,5-fache
  // aufgeblasen - eine Handyansicht in Plakatgroesse.
  const applyTargetWidth = (width, available) => {
    const stage = document.querySelector('.layout-canvas-stage');
    if (!width) {
      if (stage) {
        stage.style.removeProperty('--target-w');
        stage.style.removeProperty('--target-zoom');
      }
      return {};
    }
    const style = { '--target-w': width + 'px', '--target-zoom': String(Math.min(1, available / width)) };
    if (stage) {
      stage.style.setProperty('--target-w', style['--target-w']);
      stage.style.setProperty('--target-zoom', style['--target-zoom']);
    }
    return style;
  };

  // Liefert den Hinweistext fuer die Zielbreitenvorschau: Breite, Spalten und
  // Verkleinerung in Prozent.
  const widthNote = (width, available) => {
    if (!width) return '';
    const cols = targetColumns(width);
    let note = width + ' px · ' + cols + (cols === 1 ? ' Spalte' : ' Spalten');
    if (available < width) {
      const pct = Math.round((1 - available / width) * 100);
      note += ' · ' + pct + ' % verkleinert';
    }
    return note;
  };

  // Gridstack-Breite einer Groessenklasse. 'full' und jede Klasse oberhalb
  // der Spaltenzahl belegen alle Spalten - dieselbe Vorschau-Naeherung, die
  // die Container-Queries im echten Raster erzeugen.
  const tracksFor = (span, columns) => (span === 'full' ? columns : Math.min(Number(span) || 1, columns));

  const clamp = (value, min, max) => Math.min(max, Math.max(min, value));

  // Hoeheneinheit -> rem, identisch zu forcedHeightRem() in webui.go: n Zeilen
  // a 7rem plus (n-1) Rinnen a .8rem. 0 heisst "automatisch" (kein --card-height).
  const heightRem = units => units * 7 + (units - 1) * 0.8;

  // Der ganze Zug am Groessengriff in einer Rechnung: Pixel-Delta -> die
  // beiden Werte, die das Layout wirklich speichert. Volle Breite wird zu
  // 'full' statt zur Spaltenzahl, weil nur 1/-1 auch nach einem Fensterwechsel
  // noch "so breit wie da ist" bedeutet.
  const resizeTo = (start, dx, dy, geom) => {
    const columns = Math.max(1, geom.columns || 1);
    const minSpan = clamp(geom.minSpan || 1, 1, columns);
    const startTracks = start.span === 'full' ? columns : clamp(Number(start.span) || 1, 1, columns);
    const tracks = clamp(startTracks + Math.round(dx / geom.trackWidth), minSpan, columns);
    return {
      span: tracks >= columns ? 'full' : String(tracks),
      height: clamp(Math.round((start.height || 0) + dy / geom.rowHeight), 0, 12),
    };
  };

  // Vor welche Kachel gehoert ein Drop an (x, y)? Die Rechtecke kommen von
  // aussen, damit die Regel ohne Layout-Engine pruefbar bleibt. null heisst
  // ans Ende. Gelesen als: erste Kachel, die rechts von oder unter dem Zeiger
  // beginnt.
  const insertionPoint = (boxes, x, y) => {
    for (const {el, rect} of boxes) {
      if (y > rect.bottom) continue;
      if (y < rect.top || x < rect.left + rect.width / 2) return el;
    }
    return null;
  };

  // Rumpf einer frisch eingefuegten Karte. Die echten Karten rendert der
  // Server aus overview.html - im Editor gibt es sie erst nach dem Speichern
  // (Spec 5: kein serverseitiges Rendern im Editor-Fragment). Bis dahin steht
  // hier die Miniatur aus der Toolbox, damit Platz und Groesse stimmen. `hint`
  // wechselt den Untertext: neu abgelegte Karten "erscheinen nach dem
  // Speichern", ausgeblendete (die der Server gar nicht erst rendert) tragen
  // stattdessen den Hinweis, sie ueber das Auge wieder einzublenden.
  const placeholderHTML = (item, label, hint = 'erscheint nach dem Speichern') =>
    `<div class="layout-card-placeholder">`
    + `<span class="thumb">${THUMB[item.type] || THUMB.entity_value}</span>`
    + `<b>${escapeHTML(label ?? '')}</b>`
    + `<small>${escapeHTML(hint)}</small>`
    + `</div>`;

  // Katalog-Mindesthoehe ('25rem') -> Gridstack-Zeilen a 7rem, aufgerundet.
  // Damit stimmen die Hoehenverhaeltnisse der Vorschau ungefaehr, ohne dass
  // die Hoehe gespeichert wird.
  const heightUnits = minHeight => Math.max(1, Math.ceil((parseFloat(minHeight) || 7) / 7));

  const refMissing = (item, devices) => {
    if (item.type === 'device') return !devices.some(device => device.id === item.ref);
    if (item.type === 'entity_value') return !devices.some(device => (device.entities || []).some(entity => entity.unique_id === item.ref));
    if (item.type === 'entity_group') return !(item.entityRefs || []).some(ref => devices.some(device => (device.entities || []).some(entity => entity.unique_id === ref)));
    return false;
  };

  const CATEGORY_OPTIONS = [
    {key: 'controls', label: 'Steuerungen'},
    {key: 'measurements', label: 'Messwerte'},
    {key: 'configuration', label: 'Konfiguration'},
    {key: 'diagnostics', label: 'Diagnose'},
  ];

  const ICON = {
    grab: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="9" cy="6" r="1.3"/><circle cx="15" cy="6" r="1.3"/><circle cx="9" cy="12" r="1.3"/><circle cx="15" cy="12" r="1.3"/><circle cx="9" cy="18" r="1.3"/><circle cx="15" cy="18" r="1.3"/></svg>',
    eye: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><path d="M2 12s3.6-6.5 10-6.5S22 12 22 12s-3.6 6.5-10 6.5S2 12 2 12Z"/><circle cx="12" cy="12" r="2.6"/></svg>',
    eyeOff: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><path d="M2 12s3.6-6.5 10-6.5S22 12 22 12s-3.6 6.5-10 6.5S2 12 2 12Z"/><circle cx="12" cy="12" r="2.6"/><path d="m4 4 16 16"/></svg>',
    dots: '<svg viewBox="0 0 24 24" fill="currentColor"><circle cx="5" cy="12" r="1.7"/><circle cx="12" cy="12" r="1.7"/><circle cx="19" cy="12" r="1.7"/></svg>',
    trash: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><path d="M4 7h16M10 7V5h4v2M6 7l1 13h10l1-13"/></svg>',
    grip: '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M17 7 7 17M17 13l-4 4"/></svg>'
  };

  // Miniaturen der Toolbox-Eintraege. Farbig ueber CSS-Variablen statt fester
  // Hex-Werte: dieselben Rollenfarben/Tokens, die auch die echten Karten
  // benutzen (--flow-pv, --flow-battery, --track, --accent, --text-* ...),
  // sind pro Farbschema in base.css definiert - "tageslicht" hat fuer jedes
  // davon einen eigenen, hellen Wert. Vorher standen hier die Rollenfarben
  // des Dark-Themes als feste Hex-Werte - auf "tageslicht" war die "42" der
  // Wert-Karte weiss auf hellem Grund; ein rein monochromer currentColor-
  // Entwurf loeste das, verlor aber die Wiedererkennbarkeit der Farben.
  // var(--token) behaelt die Farben und passt sie pro Theme an. Fallback auf
  // entity_value, wenn ein Typ keine eigene hat.
  const THUMB = {
    energy_flow:  '<svg viewBox="0 0 40 20"><path d="M6 15 C6 6 20 6 20 10 S34 6 34 15" fill="none" stroke="var(--flow-pv)" stroke-width="1.6"/><circle cx="6" cy="15" r="2" fill="var(--flow-pv)"/><circle cx="34" cy="15" r="2" fill="var(--flow-load)"/></svg>',
    energy_ring:  '<svg viewBox="0 0 40 20"><circle cx="20" cy="10" r="6.5" fill="none" stroke="var(--track)" stroke-width="2.4"/><circle cx="20" cy="10" r="6.5" fill="none" stroke="var(--flow-battery)" stroke-width="2.4" stroke-linecap="round" stroke-dasharray="30 41" transform="rotate(-90 20 10)"/></svg>',
    energy_band:  '<svg viewBox="0 0 40 20"><g fill="var(--flow-grid)"><rect x="7" y="11" width="4" height="6"/><rect x="13" y="7" width="4" height="10"/><rect x="19" y="4" width="4" height="13"/><rect x="25" y="8" width="4" height="9"/><rect x="31" y="12" width="4" height="5"/></g></svg>',
    energy_day:   '<svg viewBox="0 0 40 20"><g fill="var(--flow-pv)"><rect x="6" y="13" width="3" height="4"/><rect x="11" y="10" width="3" height="7"/><rect x="16" y="6" width="3" height="11"/><rect x="21" y="4" width="3" height="13"/><rect x="26" y="8" width="3" height="9"/><rect x="31" y="12" width="3" height="5"/></g></svg>',
    energy_board: '<svg viewBox="0 0 40 20"><g fill="var(--track)"><rect x="5" y="5" width="9" height="10" rx="1.5"/><rect x="16" y="5" width="9" height="10" rx="1.5"/><rect x="27" y="5" width="9" height="10" rx="1.5"/></g><g fill="var(--accent)"><rect x="7" y="8" width="5" height="1.8"/><rect x="18" y="8" width="5" height="1.8"/><rect x="29" y="8" width="5" height="1.8"/></g></svg>',
    energy_schema:'<svg viewBox="0 0 40 20"><g fill="none" stroke="var(--flow-grid)" stroke-width="1.4" stroke-linecap="round"><path d="M5 10h30"/><path d="M12 10V5.5M20 10v4.5M28 10V5.5"/></g><circle cx="12" cy="4" r="2" fill="var(--flow-pv)"/><circle cx="28" cy="4" r="2" fill="var(--accent)"/><rect x="17.6" y="15" width="4.8" height="3.2" rx="1" fill="var(--flow-load)"/></svg>',
    energy_status:'<svg viewBox="0 0 40 20"><path d="M9 17a11 11 0 0 1 22 0" fill="none" stroke="var(--track)" stroke-width="2.6" stroke-linecap="round"/><path d="M9 17a11 11 0 0 1 5.9-9.7" fill="none" stroke="var(--accent)" stroke-width="2.6" stroke-linecap="round"/><path d="M20 17 25.4 9.6" fill="none" stroke="var(--flow-pv)" stroke-width="1.6" stroke-linecap="round"/><circle cx="20" cy="17" r="1.5" fill="var(--flow-pv)"/></svg>',
    // Die Saeule der Batteriekarte: ein Zellenumriss mit Fahne und einem
    // gefuellten unteren Teil, derselbe Aufbau wie die echte Karte.
    battery_status:'<svg viewBox="0 0 40 20"><rect x="14" y="4" width="6" height="2" rx="1" fill="var(--track)"/><rect x="10" y="6" width="14" height="12" rx="2" fill="none" stroke="var(--track)" stroke-width="2"/><rect x="11.5" y="12.5" width="11" height="4.2" rx="1" fill="var(--accent)"/></svg>',
    // Als einzige Miniatur mit Schrift: die Wert-Karte ist die eine, deren
    // ganzer Inhalt eine Zahl mit Einheit ist - jede Abstraktion daraus sah
    // aus wie ein Diagramm.
    entity_value: '<svg viewBox="0 0 40 20"><text x="6" y="13.5" font-family="system-ui, sans-serif" font-size="12" font-weight="700" fill="var(--text-strong)">42</text><text x="21" y="13.5" font-family="system-ui, sans-serif" font-size="7" fill="var(--accent)">W</text><rect x="6" y="16" width="13" height="1.8" rx=".9" fill="var(--text-faint)"/></svg>',
    entity_group: '<svg viewBox="0 0 40 20"><g fill="var(--text-subtle)"><rect x="6" y="5" width="19" height="2.2" rx="1"/><rect x="6" y="9" width="24" height="2.2" rx="1"/><rect x="6" y="13" width="15" height="2.2" rx="1"/></g><g fill="var(--accent)"><rect x="31" y="5" width="3" height="2.2" rx="1"/><rect x="31" y="9" width="3" height="2.2" rx="1"/></g></svg>',
    device:       '<svg viewBox="0 0 40 20"><rect x="8" y="5" width="14" height="10" rx="2" fill="none" stroke="var(--text-subtle)" stroke-width="1.4"/><rect x="26" y="7" width="8" height="5" rx="2.5" fill="var(--accent)"/></svg>',
    diagnostics:  '<svg viewBox="0 0 40 20"><path d="M5 12h6l3-6 4 10 3-5h14" fill="none" stroke="var(--accent)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  };

  // Konfigurationsoptionen der sechs Energiegrafiken-Alternativen plus
  // energy_flow's Animationsgeschwindigkeit-Referenz (siehe
  // knowhow/dashboard/energiegrafiken.md). Eine einzige Tabelle ist die
  // Wahrheit ueber role<->camelCase-Feld<->snake_case-JSON-Name<->
  // Steuerungsart - toGridNode/fromGridNode, handleWidgetChange, load() und
  // save() lesen alle aus ihr statt 20 Felder von Hand zu wiederholen.
  const ENERGY_OPTIONS = [
    {role: 'height-reference', field: 'heightReference', json: 'height_reference', kind: 'select'},
    {role: 'scale-mode', field: 'scaleMode', json: 'scale_mode', kind: 'select'},
    {role: 'unit', field: 'unit', json: 'unit', kind: 'select'},
    {role: 'bundle-threshold', field: 'bundleThreshold', json: 'bundle_threshold', kind: 'select'},
    {role: 'animate', field: 'animate', json: 'animate', kind: 'checkbox'},
    {role: 'kpi', field: 'kpi', json: 'kpi', kind: 'select'},
    {role: 'label-mode', field: 'labelMode', json: 'label_mode', kind: 'select'},
    {role: 'sort', field: 'sort', json: 'sort', kind: 'select'},
    {role: 'spark-window', field: 'sparkWindow', json: 'spark_window', kind: 'select'},
    {role: 'dense', field: 'dense', json: 'dense', kind: 'checkbox'},
    {role: 'show-inactive', field: 'showInactive', json: 'show_inactive', kind: 'checkbox'},
    {role: 'measured-split', field: 'measuredSplit', json: 'measured_split', kind: 'select'},
    {role: 'display-mode', field: 'displayMode', json: 'display_mode', kind: 'select'},
    {role: 'show-now', field: 'showNow', json: 'show_now', kind: 'checkbox'},
    {role: 'stroke-mode', field: 'strokeMode', json: 'stroke_mode', kind: 'select'},
    {role: 'entity-labels', field: 'entityLabels', json: 'entity_labels', kind: 'select'},
    {role: 'hide-inactive', field: 'hideInactive', json: 'hide_inactive', kind: 'checkbox'},
    {role: 'display-size', field: 'displaySize', json: 'display_size', kind: 'select'},
    {role: 'beam-span', field: 'beamSpan', json: 'beam_span', kind: 'select'},
    {role: 'show-advice', field: 'showAdvice', json: 'show_advice', kind: 'checkbox'},
    {role: 'battery-window', field: 'batteryWindow', json: 'battery_window', kind: 'select'},
    {role: 'battery-projection-window', field: 'batteryProjectionWindow', json: 'battery_projection_window', kind: 'select'},
    {role: 'speed-reference-mode', field: 'speedReferenceMode', json: 'speed_reference_mode', kind: 'select'},
    {role: 'speed-reference-watts', field: 'speedReferenceWatts', json: 'speed_reference_watts', kind: 'number'},
  ];
  const ENERGY_OPTION_BY_ROLE = Object.fromEntries(ENERGY_OPTIONS.map(o => [o.role, o]));
  const ENERGY_OPTION_FIELDS = ENERGY_OPTIONS.map(o => o.field);

  // Typ-Defaults fuers Anlegen im Editor (defaultPages(), addItem()) - vor
  // dem ersten Speichern gibt es noch keine server-normalisierten Werte.
  // Muessen mit normalizeEnergyGraphicOptions() in settings.go
  // uebereinstimmen.
  const ENERGY_OPTION_DEFAULTS = {
    energy_flow: {speedReferenceMode: 'relative', speedReferenceWatts: 1000, hideInactive: 'off'},
    energy_band: {heightReference: 'fill', scaleMode: 'linear', unit: 'auto', bundleThreshold: '0', animate: 'on', measuredSplit: 'sum'},
    energy_ring: {animate: 'on', kpi: 'autarkie', labelMode: 'both', measuredSplit: 'sum'},
    energy_board: {sort: 'fixed', sparkWindow: '15', dense: 'off', showInactive: 'on', measuredSplit: 'sum'},
    energy_day: {displayMode: 'mirror', showNow: 'on'},
    energy_schema: {strokeMode: 'power', entityLabels: 'power', hideInactive: 'off', displaySize: 'm', animate: 'off'},
    energy_status: {beamSpan: '6000', showAdvice: 'on'},
    // Nur die Trajektorie nutzt es (siehe Panel unten); die Saeule bekommt
    // den Wert zwar mit, zeigt aber kein Feld dafuer.
    battery_status: {batteryWindow: '6'},
  };
  const defaultEnergyOptions = type => ({...ENERGY_OPTION_DEFAULTS[type]});

  function selectFieldHTML(role, label, value, options) {
    const opts = options.map(([v, text]) =>
      `<option value="${v}"${value === v ? ' selected' : ''}>${escapeHTML(text)}</option>`).join('');
    return `<div class="layout-modal-field"><label>${escapeHTML(label)}</label><select data-role="${role}">${opts}</select></div>`;
  }

  // Wiederverwendet .settings-toggle/-track/-thumb aus base.css (siehe deren
  // Einsatz in settings.html) statt einer nativen Checkbox - einheitliche
  // Schalter-Optik fuer alle Ein/Aus-Felder im Dashboard, nicht nur die
  // Einstellungsseite. Kein aria-checked: anders als dort ist der Zustand
  // hier nicht Alpine-reaktiv, das native checked-Attribut traegt ihn allein.
  function toggleHTML(role, checked, extraAttr = '') {
    return `<span class="settings-toggle"><input type="checkbox" role="switch" data-role="${role}"${extraAttr}${checked ? ' checked' : ''}><span class="settings-toggle-track" aria-hidden="true"><span class="settings-toggle-thumb"></span></span></span>`;
  }

  // <label>, nicht <div>: das <input> im Schalter ist 1px gross und
  // durchsichtig (.settings-toggle in base.css). Ohne umschliessendes Label
  // rendert der Schalter zwar, laesst sich aber nicht umlegen.
  function checkboxFieldHTML(role, label, checked) {
    return `<label class="layout-modal-switchrow">${escapeHTML(label)} ${toggleHTML(role, checked)}</label>`;
  }

  function numberFieldHTML(role, label, value, min) {
    return `<div class="layout-modal-field"><label>${escapeHTML(label)}</label><input type="number" data-role="${role}" min="${min}" value="${escapeHTML(String(value))}"></div>`;
  }

  // Eine Renderfunktion je Energiegrafik-Alternative, gebunden per Typname
  // in ENERGY_OPTION_PANELS - Auswahl/Reihenfolge/Beschriftung spiegeln das
  // Artifact-Mockup, aus dem die sechs Kartentypen urspruenglich stammen.
  const ENERGY_OPTION_PANELS = {
    // Nur sichtbar bei Leistungs-Darstellung "Animationsgeschwindigkeit" -
    // im Modus "Linienstärke" hat die Referenzleistung keine Wirkung (siehe
    // energy-flow.js' render()).
    energy_flow: item =>
      checkboxFieldHTML('hide-inactive', 'Inaktive Verbraucher ausblenden', item.hideInactive === 'on') +
      (item.flowScale !== 'speed' ? '' :
        selectFieldHTML('speed-reference-mode', 'Bezug für die Animationsgeschwindigkeit', item.speedReferenceMode || 'relative',
          [['relative', 'relativ (größter aktiver Fluss)'], ['fixed', 'fest']]) +
        numberFieldHTML('speed-reference-watts', 'Referenzleistung (W)', item.speedReferenceWatts || 1000, 1)),

    energy_band: item =>
      selectFieldHTML('height-reference', 'Höhenbezug', item.heightReference || 'fill',
        [['fill', 'anteilig (füllt die Höhe)'], ['abs', 'absolut (Bezug 10 kW)']]) +
      selectFieldHTML('scale-mode', 'Skalierung', item.scaleMode || 'linear',
        [['linear', 'linear (echte Anteile)'], ['sqrt', 'Wurzel (kleine sichtbar)']]) +
      selectFieldHTML('unit', 'Einheit', item.unit || 'auto',
        [['auto', 'automatisch'], ['w', 'immer W'], ['kw', 'immer kW']]) +
      selectFieldHTML('bundle-threshold', 'Kleinstflüsse', item.bundleThreshold || '0',
        [['0', 'alle einzeln'], ['0.03', 'unter 3 % bündeln'], ['0.08', 'unter 8 % bündeln']]) +
      checkboxFieldHTML('animate', 'Fluss animieren', item.animate !== 'off') +
      selectFieldHTML('measured-split', 'Gemessene Verbraucher', item.measuredSplit || 'sum',
        [['sum', 'gesammelt'], ['entities', 'einzeln je Entität']]),

    energy_ring: item =>
      selectFieldHTML('kpi', 'Kennzahl in der Mitte', item.kpi || 'autarkie',
        [['autarkie', 'Autarkiegrad'], ['eigen', 'Eigenverbrauchsquote'], ['netz', 'Netzbilanz'], ['last', 'Hausverbrauch']]) +
      selectFieldHTML('label-mode', 'Beschriftung', item.labelMode || 'both',
        [['pct', 'Prozent'], ['abs', 'Absolutwerte'], ['both', 'beides']]) +
      checkboxFieldHTML('animate', 'Richtung animieren', item.animate !== 'off') +
      selectFieldHTML('measured-split', 'Gemessene Verbraucher', item.measuredSplit || 'sum',
        [['sum', 'gesammelt'], ['entities', 'einzeln je Entität']]),

    energy_board: item =>
      selectFieldHTML('sort', 'Sortierung', item.sort || 'fixed',
        [['fixed', 'feste Reihenfolge'], ['power', 'nach Leistung']]) +
      selectFieldHTML('spark-window', 'Verlauf', item.sparkWindow || '15',
        [['15', '15 Minuten'], ['60', '60 Minuten'], ['off', 'aus']]) +
      checkboxFieldHTML('dense', 'Kompakt', item.dense === 'on') +
      checkboxFieldHTML('show-inactive', 'Inaktive Rollen zeigen', item.showInactive !== 'off') +
      selectFieldHTML('measured-split', 'Gemessene Verbraucher', item.measuredSplit || 'sum',
        [['sum', 'gesammelt'], ['entities', 'einzeln je Entität']]),

    energy_day: item =>
      selectFieldHTML('display-mode', 'Darstellung', item.displayMode || 'mirror',
        [['mirror', 'gespiegelt (Deckung / Verwendung)'], ['supply', 'nur Deckung'], ['demand', 'nur Verwendung']]) +
      checkboxFieldHTML('show-now', 'Jetzt-Kante beschriften', item.showNow !== 'off'),

    energy_schema: item =>
      selectFieldHTML('stroke-mode', 'Leitungsstärke', item.strokeMode || 'power',
        [['const', 'konstant'], ['power', 'nach Leistung']]) +
      selectFieldHTML('entity-labels', 'Beschriftung', item.entityLabels || 'power',
        [['power', 'nur Leistung'], ['entity', 'Leistung + Entität']]) +
      checkboxFieldHTML('hide-inactive', 'Inaktive Abzweige ausblenden', item.hideInactive === 'on') +
      selectFieldHTML('display-size', 'Darstellungsgröße', item.displaySize || 'm',
        [['xs', 'XS'], ['s', 'S'], ['m', 'M (Vorgabe)'], ['l', 'L'], ['xl', 'XL']]) +
      checkboxFieldHTML('animate', 'Pfeil animieren', item.animate === 'on'),

    energy_status: item =>
      selectFieldHTML('beam-span', 'Waagen-Endwert', item.beamSpan || '6000',
        [['3000', '± 3 kW'], ['6000', '± 6 kW'], ['11000', '± 11 kW']]) +
      checkboxFieldHTML('show-advice', 'Handlungsempfehlung', item.showAdvice !== 'off'),

    // Nur die Trajektorie hat eine Zeitachse - die Saeule bekommt kein Feld
    // (dieselbe Bedingung wie energy_flow bei der Speed-Referenz). Das
    // Verlaufsfenster gilt fuer beide Haelften, die Projektion ueberschreibt
    // optional nur die Fortschreibung ("" = wie Verlauf).
    battery_status: item => item.display !== 'trajectory' ? '' :
      selectFieldHTML('battery-window', 'Zeitfenster', item.batteryWindow || '6',
        [['3', '3 h'], ['6', '6 h'], ['12', '12 h'], ['24', '24 h']]) +
      selectFieldHTML('battery-projection-window', 'Projektion (optional)', item.batteryProjectionWindow || '',
        [['', 'wie Zeitfenster'], ['3', '3 h'], ['6', '6 h'], ['12', '12 h'], ['24', '24 h']]),
  };

  // Karten mit Typ-Minimum >= 2 (die fuenf Energiegrafik-Alternativen ausser
  // der Statuskarte) sind im Editor breit genug fuer eine mehrspaltige
  // Anordnung ihrer Optionen statt der einspaltigen Standardliste.
  function energyOptionsHTML(item) {
    const panel = ENERGY_OPTION_PANELS[item.type];
    const html = panel && panel(item);
    return html || '';
  }

  // Pure function: builds the innerHTML for one widget's content div. No
  // Alpine binding in here - Gridstack owns this DOM subtree once mounted,
  // so interaction goes through event delegation on the grid container
  // (see attachWidgetEvents()) instead of x-on directives.
  // Das Auswahlfeld ersetzt das Ziehen: nur so ist ein erzwungenes
  // Typ-Minimum ueberhaupt erklaerbar - ein Ziehen, das bei 2 stehenbleibt,
  // ohne zu sagen warum, waere schlechter.
  //
  // Gesperrt wird unterhalb von min_span, und der Grund nennt die
  // Mindestbreite der *Karte*, nicht die der Spannweite. Klassen oberhalb
  // der Editor-Spaltenzahl bleiben waehlbar: der Editor misst seine eigene
  // Breite, nicht die des Bildschirms, auf dem die Uebersicht spaeter steht.
  // Wer am Laptop drei Spalten sieht, muss trotzdem 5 fuer den externen
  // Monitor einstellen koennen.
  function spanSelectHTML(item, label, columns) {
    const card = cardType(item.type);
    const options = SPAN_OPTIONS.map(span => {
      const tracks = span === 'full' ? Infinity : Number(span);
      const tooSmall = tracks < card.min_span;
      const overColumns = !tooSmall && span !== 'full' && tracks > columns;
      const title = tooSmall
        ? `${label} braucht mindestens ${card.min_width}, das ist Spannweite ${card.min_span}`
        : overColumns ? 'wirkt bei dieser Fensterbreite wie voll' : '';
      return `<option value="${span}"`
        + (item.span === span ? ' selected' : '')
        + (tooSmall ? ' disabled' : '')
        + (title ? ` title="${escapeHTML(title)}"` : '')
        + `>${escapeHTML(spanLabel(span))}</option>`;
    }).join('');
    return `<div class="layout-modal-field"><label>Breite</label><select data-role="span">${options}</select></div>`;
  }

  // Zwangshoehe: leer = keine, sonst 1-12 Einheiten a 7rem. Sie hebt an, sie
  // deckelt nicht (min-height im CSS) - bei Typen ohne fills_height kommt der
  // Zugewinn als Leerraum an. Einstellbar bleibt es trotzdem.
  function heightFieldHTML(item) {
    return `<div class="layout-modal-field"><label>Zwangshoehe</label>`
      + `<input type="number" data-role="height" min="1" max="12" placeholder="auto" value="${item.height ? String(item.height) : ''}">`
      + `</div>`;
  }

  function heightHintHTML(item) {
    return cardType(item.type).fills_height
      ? ''
      : '<p class="layout-modal-hint">Zusaetzliche Hoehe bleibt bei dieser Karte Leerraum.</p>';
  }

  // Breite und Zwangshoehe stehen bei Karten mit Typ-Minimum >= 2
  // nebeneinander statt gestapelt - derselbe Schwellwert wie
  // energyOptionsHTML()'s Wide-Modifier, dieselbe Begruendung: genug Breite,
  // um die Felder in einer Zeile zu zeigen. Der Leerraum-Hinweis bleibt eine
  // eigene, volle Zeile darunter. Der Sichtbar-Schalter gehoert seit Task 7
  // nicht mehr hierher, sondern in die Gruppe Sichtbarkeit von
  // optionsSheetHTML().
  function sizingHTML(item, label, columns) {
    const span = spanSelectHTML(item, label, columns);
    const height = heightFieldHTML(item);
    const hint = heightHintHTML(item);
    return `<div class="layout-modal-field-row">${span}${height}</div>${hint}`;
  }

  // Nur das Kachel-Chrome des Uebersichts-Bearbeitungsmodus: Verschiebegriff,
  // Titelblock mit Typ und "Referenz fehlt"-Hinweis, das Auge (Sichtbarkeit
  // als Knopf, kein Formularfeld), der Optionen-Knopf und der
  // Groessen-Griff. Kein <select> und kein <input> - alle echten
  // Eingabefelder liegen im Optionen-Modal (optionsSheetHTML), an dessen
  // data-role-Namen handleWidgetChange() weiter haengt. label und missingRef
  // sind reine Anzeige und optional; der GridStack-Pfad reicht sie wie bisher
  // durch.
  // Nur die drei Chip-Knoepfe. mount() setzt genau das in die schon
  // vorhandene .layout-card-chrome-Huelle der Live-Kachel; der GridStack-Pfad
  // (chromeHTML) rahmt es zusaetzlich mit .layout-card-chrome, Titelblock und
  // Groessen-Griff.
  function chromeButtonsHTML(item) {
    const visible = item.visible !== false;
    return `<button class="layout-chip-btn grab" type="button" aria-label="Element verschieben" title="Element verschieben">${ICON.grab}</button>`
      + `<button class="layout-chip-btn" type="button" data-role="visible-toggle" aria-pressed="${visible ? 'true' : 'false'}" aria-label="Sichtbarkeit umschalten" title="Sichtbarkeit umschalten">${visible ? ICON.eye : ICON.eyeOff}</button>`
      + `<button class="layout-chip-btn" type="button" data-role="options" aria-label="Optionen" title="Optionen">${ICON.dots}</button>`
      + `<button class="layout-chip-btn danger" type="button" data-role="remove" aria-label="Kachel entfernen" title="Kachel entfernen">${ICON.trash}</button>`;
  }

  function chromeHTML(item, label, missingRef) {
    const missingBadge = missingRef ? '<span class="layout-item-missing-ref">Referenz fehlt</span>' : '';
    return `<div class="layout-card-chrome">${chromeButtonsHTML(item)}</div>`
      + `<div class="card-title"><b>${escapeHTML(label ?? '')}</b><small>${escapeHTML(item.type)}</small>${missingBadge}</div>`
      + `<span class="layout-resize-grip" aria-hidden="true">${ICON.grip}</span>`;
  }

  // Baut eine Item-Form (wie fromGridNode) aus den data-*-Attributen einer
  // server-gerenderten .layout-grid-item. mount() nutzt sie als Rueckfall,
  // wenn das Item (noch) nicht in this.pages steht - im Normalfall liefert
  // load() this.pages, und mount() nimmt das echte Objekt, damit
  // Modal-Aenderungen dort landen und save() sie mitnimmt (Weg A).
  const SPAN_CLASS_RE = /layout-grid-item-(\d+|full)/;
  function datasetToItem(el) {
    const d = el.dataset;
    const type = d.layoutItemKind || '';
    const item = {
      id: d.layoutItemId,
      type,
      ref: d.layoutItemRef || '',
      span: (el.className.match(SPAN_CLASS_RE) || [])[1] || cardType(type).default_span,
      visible: true,
      visibleCategories: [],
      flowScale: d.flowScale || '',
      display: d.display || '',
      height: 0,
      title: '',
      entityRefs: [],
    };
    // data-speed-reference-mode -> dataset.speedReferenceMode === ENERGY_OPTION.field
    for (const {field} of ENERGY_OPTIONS) item[field] = d[field] || '';
    return item;
  }

  // Helper-Funktionen fuer die Inline-Bausteine in optionsSheetHTML.
  function flowScaleHTML(item) {
    if (item.type !== 'energy_flow') return '';
    return `<div class="layout-modal-field"><label>Leistungs-Darstellung</label><select data-role="flow-scale"><option value="width"${item.flowScale === 'speed' ? '' : ' selected'}>Linienstärke</option><option value="speed"${item.flowScale === 'speed' ? ' selected' : ''}>Animationsgeschwindigkeit</option></select></div>`;
  }

  function deviceDisplayHTML(item) {
    if (item.type !== 'device') return '';
    return selectFieldHTML('display', 'Darstellung', item.display === 'compact' ? 'compact' : 'detail',
      [['detail', 'Detail (alle Entitäten)'], ['compact', 'Kompakt (bis zu drei Werte)']]);
  }

  function batteryDisplayHTML(item) {
    if (item.type !== 'battery_status') return '';
    return selectFieldHTML('display', 'Darstellung', item.display === 'trajectory' ? 'trajectory' : 'column',
      [['column', 'Säule (Vorrat und Restlaufzeit)'], ['trajectory', 'Trajektorie (Verlauf und Fortschreibung)']]);
  }

  // Die Kategorieschalter wirken nur auf die Detailkachel: die kompakte
  // Karte waehlt ihre bis zu drei Zeilen selbst (priorityEntities in
  // webui.go), Kategorien haetten dort keine Wirkung. Statt eines toten
  // Schalters steht dort der Grund.
  function categoryHTML(item) {
    if (item.type !== 'device') return '';
    if (item.display === 'compact') {
      return '<p class="layout-modal-hint">Die kompakte Kachel wählt ihre bis zu drei Zeilen selbst — die Kategorien wirken nur in der Detailansicht.</p>';
    }
    return `<div class="layout-modal-field">${CATEGORY_OPTIONS.map(cat => `<label class="layout-modal-switchrow">${escapeHTML(cat.label)} ${toggleHTML('category', (item.visibleCategories || []).includes(cat.key), ` value="${cat.key}"`)}</label>`).join('')}</div>`;
  }

  function entityGroupHTML(item, devices) {
    if (item.type !== 'entity_group') return '';
    const entitySource = devices && devices.length ? devices : allDevices;
    const entityOptions = entitySource.flatMap(device =>
      (device.entities || []).map(entity => ({ref: entity.unique_id, label: `${device.name || device.id} / ${entity.name || entity.object_id}`})));
    return `<div class="layout-modal-field"><label>Titel</label><input type="text" data-role="entity-group-title" value="${escapeHTML(item.title || '')}"></div>`
      + `<div class="layout-modal-field"><label id="layout-item-entity-refs-label-${escapeHTML(item.id || '')}">Entitäten in dieser Liste</label>`
      + `<select multiple data-role="entity-refs" aria-labelledby="layout-item-entity-refs-label-${escapeHTML(item.id || '')}">${entityOptions.map(entity => `<option value="${escapeHTML(entity.ref)}"${(item.entityRefs || []).includes(entity.ref) ? ' selected' : ''}>${escapeHTML(entity.label)}</option>`).join('')}</select></div>`;
  }

  function entityValuePickerHTML(item, devices) {
    if (item.type !== 'entity_value') return '';
    const entitySource = devices && devices.length ? devices : allDevices;
    const entityOptions = entitySource.flatMap(device =>
      (device.entities || []).map(entity => ({ref: entity.unique_id, label: `${device.name || device.id} / ${entity.name || entity.object_id}`})));
    return `<div class="layout-modal-field"><label>Entität</label><select data-role="entity-value-ref"><option value="">– wählen –</option>${entityOptions.map(entity => `<option value="${escapeHTML(entity.ref)}"${item.ref === entity.ref ? ' selected' : ''}>${escapeHTML(entity.label)}</option>`).join('')}</select></div>`;
  }

  // Das Gegenstueck zu entityValuePickerHTML fuer die Geraetekachel: bis
  // 2026-09 war der Ref einer abgelegten Kachel unveraenderlich - ein
  // vertipptes Geraet hiess loeschen und neu ablegen.
  function deviceRefPickerHTML(item, devices) {
    if (item.type !== 'device') return '';
    const source = devices && devices.length ? devices : allDevices;
    return `<div class="layout-modal-field"><label>Gerät</label><select data-role="device-ref"><option value="">– wählen –</option>${source.map(device => `<option value="${escapeHTML(device.id)}"${item.ref === device.id ? ' selected' : ''}>${escapeHTML(device.name || device.id)}</option>`).join('')}</select></div>`;
  }

  // Die feste Zeilenauswahl der Kompaktkachel: bis zu drei Entitaeten des
  // eigenen Geraets, in der gewaehlten Reihenfolge. Leer heisst
  // "priorityEntities-Automatik" (der Server-Default). Dieselbe data-role wie
  // entity_group, damit applyOptionChange() und initEntityChoicesFor() ohne
  // Sonderfall weiterlaufen; anders als dort sind die Optionen aber auf das
  // Geraet item.ref beschraenkt.
  function compactRowsPickerHTML(item, devices) {
    if (item.type !== 'device' || item.display !== 'compact') return '';
    const source = devices && devices.length ? devices : allDevices;
    const device = source.find(d => d.id === item.ref);
    if (!device) {
      return '<p class="layout-modal-hint">Erst ein Gerät wählen, dann lassen sich bis zu drei seiner Werte fest anzeigen.</p>';
    }
    const labelID = `layout-item-compact-rows-label-${escapeHTML(item.id || '')}`;
    const options = (device.entities || []).map(entity => {
      const ref = entity.unique_id;
      const selected = (item.entityRefs || []).includes(ref) ? ' selected' : '';
      return `<option value="${escapeHTML(ref)}"${selected}>${escapeHTML(entity.name || entity.object_id || ref)}</option>`;
    }).join('');
    return `<div class="layout-modal-field"><label id="${labelID}">Angezeigte Werte (bis zu drei)</label>`
      + `<select multiple data-role="entity-refs" aria-labelledby="${labelID}">${options}</select></div>`;
  }

  // Das Optionen-Modal einer Kachel: drei Gruppen mit Ueberschrift - Platz,
  // Darstellung, Ort. Die Feld-Bausteine (spanSelectHTML, heightFieldHTML,
  // energyOptionsHTML, die Kategorie- und Entitaetenauswahl) wandern hierher;
  // nur die Gruppierung mit Ueberschriften kommt hinzu, damit
  // handleWidgetChange() ueber dieselben data-role-Namen weiterlaeuft. devices
  // ist die Geraeteliste fuer die Entitaetenauswahl - leer faellt es auf den
  // Modulcache allDevices zurueck, genau wie widgetHTML() es heute tut. label
  // und columns brauchen nur die Groessenfelder und sind optional.
  //
  // Eine Gruppe "Sichtbarkeit" mit eigenem Schalter gibt es hier nicht mehr:
  // die Sichtbarkeit haengt am Auge im Kachel-Chrome (data-role="visible-
  // toggle"), ein zweiter Schalter im Modal war dieselbe Einstellung doppelt.
  function optionsSheetHTML(item, devices, label, columns) {
    const g = (title, body) => `<div class="layout-modal-fieldgroup"><h6>${title}</h6>${body}</div>`;
    const platz = g('Platz', `<div class="layout-modal-field-row">${spanSelectHTML(item, label, columns)}${heightFieldHTML(item)}</div>` + heightHintHTML(item));
    const darstellungBody = flowScaleHTML(item) + deviceDisplayHTML(item) + batteryDisplayHTML(item) + categoryHTML(item) + energyOptionsHTML(item);
    const darstellung = g('Darstellung', darstellungBody || '<p class="layout-modal-hint">Fuer diese Karte gibt es keine Darstellungsoptionen.</p>');
    const ortBody = entityGroupHTML(item, devices) + entityValuePickerHTML(item, devices) + deviceRefPickerHTML(item, devices) + compactRowsPickerHTML(item, devices);
    const ort = g('Ort', ortBody || '<p class="layout-modal-hint">Diese Karte hat keine eigene Datenquelle.</p>');
    return platz + darstellung + ort;
  }

  // Komposition aus Kachel-Chrome und Optionen-Modal. Bleibt die eine
  // Funktion, die der GridStack-Pfad (toGridNode/renderGroup/applyColumns) als
  // Widget-Inhalt einsetzt - die Aufteilung ist rein DOM-Gruppierung, die
  // data-roles und das Escaping bleiben identisch.
  function widgetHTML(item, label, missingRef, columns) {
    return chromeHTML(item, label, missingRef) + optionsSheetHTML(item, allDevices, label, columns);
  }

  // item (unsere Layout.Item-Form) -> GridStackWidget. x/y kommen nicht mehr
  // vor: die Position ist die Reihenfolge, und compact('list') stellt sie
  // nach jedem Laden her.
  function toGridNode(item, label, missingRef, columns) {
    const span = item.span || cardType(item.type).default_span;
    // cardKey statt item.type: die kompakte Geraetekachel hat ihren eigenen
    // Katalogeintrag mit kleinerer Mindesthoehe.
    const type = cardType(cardKey(item));
    const node = {
      id: item.id,
      w: tracksFor(span, columns),
      h: item.height || heightUnits(type.min_height),
      type: item.type, ref: item.ref || '', span, height: item.height || 0,
      visible: item.visible !== false,
      visibleCategories: item.visibleCategories || [],
      flowScale: item.flowScale || '',
      display: item.display || '',
      title: item.title || '',
      entityRefs: item.entityRefs || [],
    };
    for (const field of ENERGY_OPTION_FIELDS) node[field] = item[field] || '';
    node.content = widgetHTML({...item, span}, label, missingRef, columns);
    return node;
  }

  // GridStackNode (aus grid.save()) -> unsere Layout.Item-Form. Die
  // Groessenklasse kommt aus dem Knoten selbst, nicht aus w: w ist nur noch
  // die auf die Editor-Spaltenzahl gedeckelte Vorschau davon.
  function fromGridNode(node) {
    const item = {
      id: node.id, type: node.type, ref: node.ref || '',
      span: node.span || cardType(node.type).default_span,
      visible: node.visible !== false,
      visibleCategories: node.visibleCategories || [],
      flowScale: node.flowScale || '',
      display: node.display || '',
      height: node.height || 0,
      title: node.title || '',
      entityRefs: node.entityRefs || [],
    };
    for (const field of ENERGY_OPTION_FIELDS) item[field] = node[field] || '';
    return item;
  }

  // Reihenfolge auslesen. Nach compact('list') steht die Liste schon in
  // (y, x)-Ordnung; das Sortieren ist die Absicherung, kein Umsortieren.
  const itemsFromGrid = grid => grid.save(false)
    .slice()
    .sort((a, b) => (a.y || 0) - (b.y || 0) || (a.x || 0) - (b.x || 0))
    .map(fromGridNode);

  // Gridstack instances are plain platform handles, not view state - kept
  // outside the reactive Alpine object so Gridstack's own DOM mutation never
  // fights Alpine's mutation observer, same reasoning as devicemap.page.js's
  // `let cy = null`. Keyed by group.id.
  const grids = new Map();

  // Wie die Grids selbst: Plattform-Handles, kein View-Zustand. Der gemerkte
  // letzte Wert verhindert einen Neuaufbau bei unveraenderter Spaltenzahl -
  // ResizeObserver feuert bei jedem Pixel.
  const observers = new Map();
  const lastColumns = new Map();

  // Zweite Aufhaengstelle fuer Gridstack: nicht ein Raster je Gruppe wie in
  // grids, sondern das eine Live-Raster der Uebersicht selbst, an das sich
  // mount() haengt. Ebenfalls ein Plattform-Handle, kein View-Zustand.

  // Drei-Register-Katalog fuer die suchbare Toolbox: feste Kartentypen,
  // Geraete, Entitaeten.
  function catalog(devices) {
    const karten = [
      {id: 'energy-flow', type: 'energy_flow', ref: '', title: 'Energie: Energiefluss', desc: 'Energiefluss'},
      {id: 'diagnostics', type: 'diagnostics', ref: '', title: 'Diagnosen', desc: 'Diagnosen'},
      {id: 'energy-band', type: 'energy_band', ref: '', title: 'Energie: Bilanzband', desc: 'Bilanzband'},
      {id: 'energy-ring', type: 'energy_ring', ref: '', title: 'Energie: Autarkie-Ring', desc: 'Autarkie-Ring'},
      {id: 'energy-board', type: 'energy_board', ref: '', title: 'Energie: Datentafel', desc: 'Datentafel'},
      {id: 'energy-day', type: 'energy_day', ref: '', title: 'Energie: Tagesband', desc: 'Tagesband'},
      {id: 'energy-schema', type: 'energy_schema', ref: '', title: 'Energie: Anlagenschema', desc: 'Anlagenschema'},
      {id: 'energy-status', type: 'energy_status', ref: '', title: 'Energie: Statuskarte', desc: 'Statuskarte'},
      {id: 'battery-status', type: 'battery_status', ref: '', title: 'Speicher: Statuskarte', desc: 'Vorrat und Restlaufzeit'},
      {id: 'entity-value', type: 'entity_value', ref: '', title: 'Wert-Karte', desc: 'Eine Entitaet als grosser Wert'},
      // Die Entitaetenliste hatte bis 2026-09 keinen Katalogeintrag: sie war
      // nur ueber den alten Panel-Editor erreichbar und fehlte in der Toolbox
      // damit ganz. Wie 'entity-value' ist sie generisch - die Entitaeten
      // waehlt man danach im Optionen-Modal, darum eine frische ID je Karte
      // (siehe addFromCatalog).
      {id: 'entity-group', type: 'entity_group', ref: '', title: 'Entitätenliste', desc: 'Mehrere Entitäten in einer Karte'},
    ];
    const geraete = (devices || []).map(d => ({id: 'device:'+d.id, type: 'device', ref: d.id, title: d.name || d.id, desc: 'Geraet'}));
    // Ein Eintrag je Entitaet: die Wert-Karte. Bis 2026-09 stand daneben ein
    // zweiter fuer die aeltere 'entity'-Karte ("Entität mit technischen
    // Details", die Zeile aus der Geraetetafel samt Quelle/Freshness/Zuletzt
    // gesehen). Dieser Kartentyp ist abgeschafft - er zeigte Diagnosefelder an
    // der Werkstattwand und war die letzte Karte, deren Wert nur der volle
    // Fragment-Tausch nachzog. Siehe Spec 2026-08-23, Abschnitt 4;
    // normalizeLayout() schreibt bestehende Karten auf entity_value um.
    const entitaeten = (devices || []).flatMap(d => (d.entities || []).map(e => (
      {id: 'entity-value:'+e.unique_id, type: 'entity_value', ref: e.unique_id,
       title: (d.name || d.id) + ' / ' + (e.name || e.object_id), desc: 'Wert-Karte'}
    )));
    return { karten, geraete, entitaeten };
  }

  // Durchsucht alle drei Register des Katalogs mit case-insensitiver Substring-Suche.
  function filterCatalog(cat, query) {
    return [...cat.karten, ...cat.geraete, ...cat.entitaeten].filter(e => e.title.toLowerCase().includes(String(query || '').toLowerCase()));
  }

  const layoutEditor = () => ({
    pages: [],
    devices: [],
    loading: false,
    saving: false,
    unsaved: false,
    // Name der Seite, die die Uebersicht gerade rendert. Beim Mounten aus dem
    // gerenderten [data-layout-page] uebernommen, danach vom Seiten-Modal
    // gepflegt.
    activePage: '',

    // Jedes Feld, das diese Komponente je auf sich selbst setzt, steht hier -
    // auch die rein internen mit Unterstrich. Alpine legt `this` als
    // mergeProxies() ueber den ganzen Gueltigkeitsstapel: eine Zuweisung an
    // einen Namen, den KEIN Objekt im Stapel kennt, landet nicht hier, sondern
    // im aeussersten Bereich - dashboardShell() am <body>. Der ueberlebt jeden
    // Moduswechsel, und beim zweiten "Editieren" stand _optionsWired dort
    // schon auf true: die frische Komponente verdrahtete das frische Modal
    // nicht mehr, das Modal liess sich nicht mehr schliessen. Deklariert
    // gehoeren die Felder der Komponente und sterben mit ihr.
    editItems: null,
    _activeWidth: 0,
    _widthResetTimer: null,
    _gridObserver: null,
    _movedNodes: null,
    _modalHosts: null,
    _optionsItem: null,
    _optionsSlot: null,
    _optionsWired: false,
    _pageWired: false,
    _toolboxWired: false,
    _tbTab: 'karten',
    onEditorMount: null,
    onEditorUnmount: null,
    onEditorKeydown: null,
    onEditorToolbarClick: null,
    onGridClick: null,
    onGridDragOver: null,
    onGridDrop: null,
    onGridPointerDown: null,
    onOptionsChange: null,
    onScrimClick: null,
    onToolboxClick: null,
    onToolboxReflow: null,
    onWidthResize: null,

    // Alpine ruft init() beim Einhaengen der Komponente auf. Hier bindet es
    // die beiden document-Ereignisse, ueber die overviewShell() (overview.page
    // .js) den Bearbeitungsmodus der Uebersicht an- und abschaltet.
    init() {
      this.onEditorMount = () => this.mount();
      this.onEditorUnmount = () => this.unmount();
      document.addEventListener('layout-editor:mount', this.onEditorMount);
      document.addEventListener('layout-editor:unmount', this.onEditorUnmount);
      // Querverweis wie window.__dashboardShell__ aus Task 4: dashboardShell()
      // erreicht den Waechter (confirmLeave) ueber diesen Griff, bevor es den
      // Tab oder die Seite wechselt.
      window.__layoutEditor__ = this;
    },

    // Alpine ruft destroy() beim Entfernen von #layout-editor-root - also bei
    // jedem "Speichern & schliessen". Ohne das blieben die beiden
    // document-Ereignisse haengen und die alte Komponente montierte beim
    // naechsten Editieren neben der neuen mit.
    destroy() {
      if (this.onEditorMount) document.removeEventListener('layout-editor:mount', this.onEditorMount);
      if (this.onEditorUnmount) document.removeEventListener('layout-editor:unmount', this.onEditorUnmount);
      // Falls destroy() ohne vorheriges unmount() kommt: den document-weiten
      // Toolbox-Klick trotzdem loesen, sonst schaltet er in der naechsten
      // Sitzung die frische Toolbox wieder zu.
      if (this.onToolboxClick) document.removeEventListener('click', this.onToolboxClick);
      clearTimeout(this._widthResetTimer);
      if (window.__layoutEditor__ === this) window.__layoutEditor__ = null;
    },

    // Bearbeitungsmodus an: jede vorhandene .layout-grid-item der Uebersicht
    // bekommt eine .layout-edit-slot-Huelle mit Chrome und Groessen-Griff
    // (Klassen aus layout-editor.css), danach uebernimmt Gridstack die Huellen
    // auf dem bestehenden .layout-grid-Container. Der Chrome-Inhalt selbst und
    // das Optionen-Modal sind Aufgabe spaeterer Schritte - hier steht nur das
    // Geruest.
    mount() {
      const container = document.querySelector('.layout-grid');
      // id -> Item-Form. Steht das Item schon in this.pages (Normalfall nach
      // load()), ist es genau dasselbe Objekt - Modal-Aenderungen landen dort
      // und save() nimmt sie mit (Weg A). Sonst ein Rueckfall aus den
      // data-*-Attributen der Kachel.
      this.editItems = new Map();
      for (const card of document.querySelectorAll('.layout-grid-item')) {
        const id = card.dataset.layoutItemId;
        if (id && !this.editItems.has(id)) this.editItems.set(id, this.findPagesItem(id) || datasetToItem(card));
      }
      // Kein zweites Layoutmodell: die Kachel bleibt genau das Rasterkind, das
      // sie in der Ansicht ist - dieselbe span-Klasse, dieselbe Inhaltshoehe,
      // dieselben Container-Queries. Der Bearbeitungsmodus haengt nur Chrome
      // und Griff hinein und setzt .layout-edit-slot auf dieselbe Element.
      // Deshalb aendert der Moduswechsel die Geometrie nicht.
      for (const card of document.querySelectorAll('.layout-grid-item')) {
        if (card.classList.contains('layout-edit-slot')) continue;
        const item = this.editItems.get(card.dataset.layoutItemId) || datasetToItem(card);
        this.dressCard(card, item);
      }
      if (container) this.bindPointerInteractions(container);

      // Eine Delegation auf dem Raster: ⋯ oeffnet das Options-Modal, das Auge
      // schaltet Sichtbarkeit. Der Verschiebegriff .grab ist GridStacks handle.
      this.onGridClick = event => {
        const optsBtn = event.target.closest('[data-role="options"]');
        if (optsBtn) { this.openOptions(optsBtn.closest('.layout-edit-slot')); return; }
        const eyeBtn = event.target.closest('[data-role="visible-toggle"]');
        if (eyeBtn) { this.toggleVisible(eyeBtn.closest('.layout-edit-slot')); return; }
        const removeBtn = event.target.closest('[data-role="remove"]');
        if (removeBtn) this.removeCard(removeBtn.closest('.layout-edit-slot'));
      };
      container?.addEventListener('click', this.onGridClick);

      // Das Options-Modal ist statischer Fragment-Knoten - einmal verdrahten.
      const scrim = document.getElementById('layout-options-modal');
      if (scrim && !this._optionsWired) {
        this._optionsWired = true;
        this.onScrimClick = event => {
          if (event.target === scrim || event.target.closest('[data-close]')) this.closeOptions();
        };
        this.onOptionsChange = event => this.applyOptionChange(event);
        scrim.addEventListener('click', this.onScrimClick);
        scrim.querySelector('[data-modal-body]')?.addEventListener('change', this.onOptionsChange);
      }

      // Toolbox oeffnen/schliessen laeuft per Delegation auf document (der
      // Toggle-Knopf wandert in die Live-Werkzeugleiste, siehe unten). Anders
      // als die einmalige Verdrahtung von Suche/Registern/Liste muss dieser
      // Zuhoerer bei JEDEM Mount neu gesetzt und in unmount() wieder geloest
      // werden: nach "Speichern & schliessen" ist die naechste Editiersitzung
      // eine frische Komponente mit frischem Fragment. Ein an document
      // haengengebliebener Toggle-Zuhoerer der Vorsitzung schaltete die
      // Toolbox unmittelbar nach dem Aufgehen wieder zu - sie liess sich nicht
      // mehr oeffnen. Der aktuelle Toolbox-Knoten wird darum im Handler frisch
      // aufgeloest, nicht ueber eine Closure gehalten.
      if (this.onToolboxClick) document.removeEventListener('click', this.onToolboxClick);
      this.onToolboxClick = event => {
        if (event.target.closest('[data-toolbox-toggle]')) {
          this.setToolbox(!document.getElementById('toolbox')?.classList.contains('open'));
          return;
        }
        if (event.target.closest('[data-toolbox-close]')) this.setToolbox(false);
      };
      document.addEventListener('click', this.onToolboxClick);

      // Suche, drei Register, Trefferliste: statische Fragment-Knoten, die
      // innerhalb einer Sitzung stehen bleiben - einmal verdrahten.
      const toolbox = document.getElementById('toolbox');
      if (toolbox && !this._toolboxWired) {
        this._toolboxWired = true;
        this._tbTab = 'karten';
        const list = toolbox.querySelector('[data-tb-list]');
        const search = toolbox.querySelector('[data-tb-search]');
        search?.addEventListener('input', () => this.renderToolbox());
        for (const tab of toolbox.querySelectorAll('[data-tb-tab]')) {
          tab.addEventListener('click', () => {
            this._tbTab = tab.dataset.tbTab;
            toolbox.querySelectorAll('[data-tb-tab]').forEach(el => el.classList.toggle('active', el === tab));
            if (search) search.value = '';
            this.renderToolbox();
          });
        }
        list?.addEventListener('dragstart', event => {
          const btn = event.target.closest?.('[data-add]');
          if (!btn) return;
          const entry = JSON.parse(list.dataset.hits || '[]')[Number(btn.dataset.add)];
          if (!entry) return;
          const payload = JSON.stringify(entry);
          event.dataTransfer?.setData('application/x-layout-card', payload);
          event.dataTransfer?.setData('text/plain', payload);
          if (event.dataTransfer) event.dataTransfer.effectAllowed = 'copy';
        });
        list?.addEventListener('click', event => {
          const btn = event.target.closest('[data-add]');
          if (!btn) return;
          const hits = JSON.parse(list.dataset.hits || '[]');
          const entry = hits[Number(btn.dataset.add)];
          if (!entry) return;
          this.addFromCatalog(entry);
          const fresh = document.querySelector('.layout-edit-slot.is-entering');
          if (fresh) {
            fresh.classList.add('is-selected');
            fresh.scrollIntoView?.({block: 'nearest'});
            setTimeout(() => fresh.classList.remove('is-selected'), 1100);
          }
        });
      }

      this.syncActivePage();

      // Das Seiten-Modal ist ebenfalls statischer Fragment-Knoten.
      const pageScrim = document.getElementById('layout-page-modal');
      if (pageScrim && !this._pageWired) {
        this._pageWired = true;
        pageScrim.addEventListener('click', event => {
          if (event.target === pageScrim || event.target.closest('[data-page-close]')) { this.closePageOptions(); return; }
          const move = event.target.closest('[data-page-move]');
          if (move) { this.movePageBy(Number(move.dataset.pageMove)); return; }
          if (event.target.closest('[data-page-remove]')) this.removeActivePage();
        });
        pageScrim.querySelector('[data-page-name]')?.addEventListener('input', event => this.renamePage(event.target.value));
      }

      // Escape schliesst zuerst das Modal, sonst die Toolbox.
      this.onEditorKeydown = event => {
        if (event.key !== 'Escape') return;
        const pageOpen = document.getElementById('layout-page-modal');
        if (pageOpen?.classList.contains('open')) { event.preventDefault(); this.closePageOptions(); return; }
        const openScrim = document.getElementById('layout-options-modal');
        if (openScrim?.classList.contains('open')) { event.preventDefault(); this.closeOptions(); return; }
        if (document.getElementById('toolbox')?.classList.contains('open')) { event.preventDefault(); this.setToolbox(false); }
      };
      document.addEventListener('keydown', this.onEditorKeydown);

      // Die edit-only-Bedienelemente aus [data-editor-toolbar] wandern in die
      // sichtbare Live-Werkzeugleiste der Uebersicht. Der Container selbst
      // bleibt (versteckt) als Herkunftsort stehen; beim Unmount werden die
      // verschobenen Knoten entfernt (das Fragment loescht overviewShell()).
      const bar = document.querySelector('.layout-toolbar');
      const toolbarSrc = document.querySelector('[data-editor-toolbar]');
      if (bar && toolbarSrc && !this._movedNodes) {
        this._movedNodes = [...toolbarSrc.children];
        this._movedNodes.forEach(node => bar.appendChild(node));
        toolbarSrc.hidden = true;
      }

      // Beide Modale ziehen an den <body>. Grund: section.panel.active traegt
      // eine Animation mit transform (base.css), deren Endzustand als
      // `matrix(1,0,0,1,0,0)` stehen bleibt - und ein Vorfahr mit transform
      // ist Bezugsrahmen auch fuer position:fixed. Das Modal zentrierte sich
      // daher im Panel statt im Fenster und landete bei langer Uebersicht
      // weit unterhalb des Sichtbereichs. Am <body> gilt wieder das Fenster.
      // Die Toolbox faehrt aus demselben Grund mit: sie stand als
      // position:absolute im Panel und damit im Dokumentfluss - wer nach dem
      // Ablegen einer Karte nach unten scrollte und sie erneut oeffnete, sah
      // nichts, weil sie oben am Panelanfang aufging. Am <body> gilt wieder
      // das Fenster, und position:fixed haelt sie im Blick.
      if (!this._modalHosts) {
        this._modalHosts = [...document.querySelectorAll('.layout-modal-scrim, .layout-toolbox')]
          .filter(node => node.parentElement !== document.body)
          .map(node => ({node, home: node.parentElement}));
        this._modalHosts.forEach(({node}) => document.body.appendChild(node));
      }
      this.onToolboxReflow = () => this.syncToolboxOffset();
      window.addEventListener('scroll', this.onToolboxReflow, {passive: true});
      window.addEventListener('resize', this.onToolboxReflow);
      this.syncToolboxOffset();

      // Zielbreiten-Chip: aktiven Chip markieren, Buehne und Hinweis setzen.
      this._activeWidth = 0;
      // Gemessen wird der Traeger der Buehne, nicht das Raster darin: das
      // Raster steht bereits auf der zuletzt gewaehlten Zielbreite, ein
      // Wechsel haette sich also an sich selbst gemessen und erst der zweite
      // Klick auf denselben Chip haette gestimmt.
      const widthAvailable = () => {
        const stage = document.querySelector('.layout-canvas-stage');
        return Math.max(120, stage?.parentElement?.clientWidth || stage?.clientWidth || 0);
      };
      const applyWidth = width => {
        const avail = widthAvailable();
        clearTimeout(this._widthResetTimer);
        if (!width) {
          // Zurueck auf Editorbreite. width:auto laesst sich nicht
          // uebergangsanimieren - stand vorher eine Zielbreite in Pixeln, faehrt
          // die Buehne erst auf die volle verfuegbare Pixelbreite (das
          // animiert, siehe transition:width in layout-editor.css) und faellt
          // nach dem Uebergang auf auto zurueck, damit sie wieder frei
          // mitwaechst. Ohne vorher gesetzte Zielbreite gibt es nichts zu
          // animieren.
          const stage = document.querySelector('.layout-canvas-stage');
          if (stage && stage.style.getPropertyValue('--target-w')) {
            stage.style.setProperty('--target-w', avail + 'px');
            stage.style.setProperty('--target-zoom', '1');
            this._widthResetTimer = setTimeout(() => applyTargetWidth(0), 320);
          } else {
            applyTargetWidth(0, avail);
          }
        } else {
          applyTargetWidth(width, avail);
        }
        const note = document.querySelector('[data-widthnote]');
        if (note) note.textContent = widthNote(width, avail);
        this._activeWidth = width;
      };
      this.onEditorToolbarClick = event => {
        const chip = event.target.closest('[data-w]');
        if (chip) {
          document.querySelectorAll('[data-w]').forEach(el => el.classList.toggle('active', el === chip));
          applyWidth(Number(chip.dataset.w));
          return;
        }
        if (event.target.closest('[data-mode-discard]')) { this.discardChanges(); return; }
        if (event.target.closest('[data-pageopts]')) { this.openPageOptions(); return; }
        if (event.target.closest('[data-addpage]')) {
          this.addPage();
          // announcePages() statt eines nackten Ereignisses: nur so erfaehrt
          // die Tab-Leiste auch, welche Seite jetzt die aktive ist.
          this.announcePages();
          return;
        }
        if (event.target.closest('[data-mode-save]')) {
          Promise.resolve(this.save()).then(() =>
            document.dispatchEvent(new CustomEvent('layout-editor:request-leave')));
        }
      };
      document.addEventListener('click', this.onEditorToolbarClick);
      this.onWidthResize = () => { if (this._activeWidth) applyWidth(this._activeWidth); };
      window.addEventListener('resize', this.onWidthResize);
      this.updateStatus();
      // Ausgeblendete Kacheln stehen nicht im server-gerenderten Raster - im
      // Editor gehoeren sie sichtbar (gedimmt) dazu. Steht load() noch aus,
      // holt dessen finally den Aufruf nach.
      this.syncHiddenCards();

    },

    // Der Server rendert nur sichtbare Kacheln (overview.html:
    // {{if and .Visible ...}}). Im Bearbeitungsmodus sollen auch die
    // ausgeblendeten dastehen - gedimmt und mit "ausgeblendet"-Fahne (CSS
    // .layout-edit-slot.is-hidden) -, sonst gaebe es nach dem Speichern und
    // Neuladen keinen Weg, sie ueber das Auge wieder einzublenden. Je
    // fehlendem Item ein Platzhalter, ans Ende der aktiven Seite; unmount()
    // raeumt ihn mit den anderen editor-injizierten Kacheln wieder ab, damit
    // der Ansichtsmodus server-treu bleibt.
    syncHiddenCards() {
      if (!this.editItems) return;
      const page = this.pages?.[this.activePageIndex()];
      if (!page) return;
      const host = document.querySelector('.layout-grid [data-layout-page]') || document.querySelector('.layout-grid');
      if (!host) return;
      const present = new Set([...host.querySelectorAll('[data-layout-item-id]')].map(el => el.dataset.layoutItemId));
      for (const group of page.groups || []) {
        for (const item of group.items || []) {
          if (item.visible !== false || present.has(item.id)) continue;
          const card = this.buildCard(item, 'über das Auge wieder einblenden');
          host.appendChild(card);
          present.add(item.id);
          this.editItems.set(item.id, item);
          this.dressCard(card, item);
        }
      }
    },

    // Chrome und Groessengriff in eine vorhandene Kachel haengen und sie zum
    // .layout-edit-slot machen. Ein Ort fuer beide Wege: die server-gerenderten
    // Kacheln beim Mounten und die frisch aus der Toolbox eingefuegten.
    dressCard(card, item) {
      card.classList.add('layout-edit-slot');
      const chrome = document.createElement('div');
      chrome.className = 'layout-card-chrome';
      chrome.innerHTML = chromeButtonsHTML(item);
      const grip = document.createElement('div');
      grip.className = 'layout-resize-grip';
      grip.innerHTML = ICON.grip;
      card.append(chrome, grip);
      if (item.visible === false) card.classList.add('is-hidden');
      return card;
    },

    // Bearbeitungsmodus aus: Chrome und Griffe entfernen, Huellen aufloesen,
    // jede .layout-grid-item bleibt an ihrem Platz als reine Ansicht stehen.
    unmount() {
      this._gridObserver?.disconnect?.();
      this._gridObserver = null;
      // destroy(false): GridStack-Verhalten lösen, aber das echte .layout-grid
      // und die Karten im DOM lassen - der Ansichtsmodus braucht sie weiter.
      const container = document.querySelector('.layout-grid');
      container?.removeEventListener('click', this.onGridClick);
      container?.classList.remove('grid-stack');
      if (container) container.style.minHeight = '';
      if (this.onEditorKeydown) document.removeEventListener('keydown', this.onEditorKeydown);
      if (this.onEditorToolbarClick) document.removeEventListener('click', this.onEditorToolbarClick);
      // Bei jedem Mount frisch gesetzt (siehe mount()) - hier wieder loesen,
      // damit die naechste Editiersitzung nicht gegen einen alten Zuhoerer
      // anlaeuft.
      if (this.onToolboxClick) { document.removeEventListener('click', this.onToolboxClick); this.onToolboxClick = null; }
      clearTimeout(this._widthResetTimer);
      if (this.onWidthResize) window.removeEventListener('resize', this.onWidthResize);
      if (this.onToolboxReflow) {
        window.removeEventListener('scroll', this.onToolboxReflow);
        window.removeEventListener('resize', this.onToolboxReflow);
        this.onToolboxReflow = null;
      }
      this.setToolbox?.(false);
      this.closeOptions();
      // Zurueck an ihren Herkunftsort statt geloescht: das Fragment bleibt
      // damit wieder montierbar. Der Uebersichts-Fragmenttausch (Seitenwechsel,
      // Live-Auffrischung) reisst .layout-toolbar und Raster weg, waehrend
      // #layout-editor-root stehen bleibt - overviewShell() montiert danach
      // neu, und die Knoten muessen dafuer noch da sein. Verlaesst der Nutzer
      // den Modus ganz, loescht leaveEdit() das Fragment mitsamt allem.
      const toolbarHome = document.querySelector('[data-editor-toolbar]');
      this._movedNodes?.forEach(node => (toolbarHome || document.createDocumentFragment()).appendChild(node));
      this._movedNodes = null;
      // Dasselbe fuer die an den <body> gezogenen Modale: die haengen nicht
      // mehr unter #layout-editor-root und wuerden sonst dort ueberleben.
      this._modalHosts?.forEach(({node, home}) => (home || document.createDocumentFragment()).appendChild(node));
      this._modalHosts = null;
      // Das .layout-grid ueberlebt den Moduswechsel - seine Listener duerfen
      // sich beim naechsten "Editieren" nicht ein zweites Mal stapeln.
      if (container && this.onGridDragOver) {
        container.removeEventListener('dragover', this.onGridDragOver);
        container.removeEventListener('drop', this.onGridDrop);
        container.removeEventListener('pointerdown', this.onGridPointerDown);
        this.onGridDragOver = this.onGridDrop = this.onGridPointerDown = null;
      }
      // Editor-injizierte Kacheln (frisch aus der Toolbox, lokal gerenderte
      // Seiten, nachgezogene ausgeblendete Kacheln) tragen kein
      // Server-Markup - im Ansichtsmodus haben sie nichts zu suchen. Nach dem
      // Speichern rendert der Server sie ohnehin selbst neu.
      for (const injected of document.querySelectorAll('.layout-grid-item[data-editor-injected]')) injected.remove();
      for (const extra of document.querySelectorAll('.layout-card-chrome, .layout-resize-grip')) extra.remove();
      // Die Kachel selbst war der Slot - es gibt keine Huelle aufzuloesen,
      // nur die Bearbeitungsklassen abzustreifen. Was danach steht, ist Zeichen
      // fuer Zeichen das server-gerenderte Rasterkind.
      for (const slot of document.querySelectorAll('.layout-edit-slot')) {
        slot.classList.remove('layout-edit-slot', 'is-selected', 'is-hidden', 'is-entering', 'is-dragging');
      }
      document.querySelector('.layout-drop-marker')?.remove();
      this.editItems = null;
    },

    // Alle Zeigergesten des Editors an einem Ort: Ziehen zum Umsortieren,
    // Ziehen am Griff zum Groesseaendern, Ablegen aus der Toolbox. Kein
    // Fremdcode - das Raster bleibt das echte CSS-Grid, es bewegen sich nur
    // Klassen und zwei Custom Properties.
    bindPointerInteractions(container) {
      const cardsIn = () => [...container.querySelectorAll('.layout-grid-item')];
      const boxesFor = skip => cardsIn()
        .filter(el => el !== skip)
        .map(el => ({el, rect: el.getBoundingClientRect()}));

      this.onGridDragOver = event => { event.preventDefault(); };
      this.onGridDrop = event => {
        const raw = event.dataTransfer?.getData('application/x-layout-card')
          || event.dataTransfer?.getData('text/plain');
        if (!raw) return;
        let entry;
        try { entry = JSON.parse(raw); } catch { return; }
        if (!entry?.type) return;
        event.preventDefault();
        this.addFromCatalog(entry, insertionPoint(boxesFor(null), event.clientX, event.clientY));
        this.setToolbox(false);
      };
      container.addEventListener('dragover', this.onGridDragOver);
      container.addEventListener('drop', this.onGridDrop);

      // Ein pointerdown, zwei Gesten - unterschieden am getroffenen Griff.
      this.onGridPointerDown = event => {
        const card = event.target.closest?.('.layout-edit-slot');
        if (!card) return;
        if (event.target.closest('.layout-resize-grip')) this.beginResize(event, card);
        else if (event.target.closest('.layout-chip-btn.grab')) this.beginReorder(event, card, boxesFor);
        else return;
        event.preventDefault();
      };
      container.addEventListener('pointerdown', this.onGridPointerDown);
    },

    // Ziehen am Griff unten rechts. Die Kachel folgt nicht dem Pixel, sondern
    // rastet auf die Werte, die auch gespeichert werden - was man waehrend des
    // Zugs sieht, ist exakt das Ergebnis.
    beginResize(event, card) {
      const item = this.itemForSlot(card);
      if (!item) return;
      const rect = card.getBoundingClientRect();
      const columns = this.gridColumns();
      const rem = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
      const geom = {
        columns,
        minSpan: cardType(item.type).min_span || 1,
        trackWidth: (container => (container ? (container.clientWidth + .8 * rem) / columns : 18.8 * rem))(document.querySelector('.layout-grid')),
        rowHeight: 7.8 * rem,
      };
      // Aus der gemessenen Hoehe starten, wenn die Karte auf 'automatisch'
      // steht - sonst springt sie beim ersten Pixel auf die Katalog-Mindesthoehe.
      const start = {span: item.span, height: item.height || Math.max(1, Math.round((rect.height + .8 * rem) / geom.rowHeight))};
      const from = {x: event.clientX, y: event.clientY};
      card.classList.add('is-selected');

      const move = ev => this.applyResize(card, resizeTo(start, ev.clientX - from.x, ev.clientY - from.y, geom));
      const stop = () => {
        document.removeEventListener('pointermove', move);
        document.removeEventListener('pointerup', stop);
        card.classList.remove('is-selected');
      };
      document.addEventListener('pointermove', move);
      document.addEventListener('pointerup', stop);
    },

    // Ziehen am Verschiebegriff. Ein Platzhalter wandert durch das Raster,
    // die Kachel selbst bleibt stehen und wird nur gedimmt - so bleibt der
    // Umbruch des echten Rasters waehrend des Zugs sichtbar.
    beginReorder(event, card, boxesFor) {
      card.classList.add('is-dragging');
      const marker = document.createElement('div');
      marker.className = `layout-drop-marker ${card.className.match(SPAN_CLASS_RE)?.[0] || ''}`;
      card.parentElement?.insertBefore(marker, card);

      let target = null;
      const move = ev => {
        target = insertionPoint(boxesFor(card), ev.clientX, ev.clientY);
        if (target === marker) return;
        if (target) target.parentElement?.insertBefore(marker, target);
        else card.parentElement?.appendChild(marker);
      };
      const stop = () => {
        document.removeEventListener('pointermove', move);
        document.removeEventListener('pointerup', stop);
        marker.remove();
        card.classList.remove('is-dragging');
        this.moveCardBefore(card, target);
      };
      document.addEventListener('pointermove', move);
      document.addEventListener('pointerup', stop);
    },

    // Neue Groesse in die Kachel schreiben: span als Rasterklasse, height als
    // --card-height. Beides genau die Traeger, die overview.html serverseitig
    // setzt - nach dem Speichern rendert der Server Zeichen fuer Zeichen
    // dasselbe.
    applyResize(card, size) {
      const item = this.itemForSlot(card);
      if (!item) return;
      card.className = card.className.replace(SPAN_CLASS_RE, `layout-grid-item-${size.span}`);
      if (size.height > 0) card.style.setProperty('--card-height', `${heightRem(size.height)}rem`);
      else card.style.removeProperty('--card-height');
      item.span = size.span;
      item.height = size.height;
      card.dataset.layoutItemSpan = size.span;
      this.markUnsaved();
    },

    // Kachel entfernen: aus dem DOM und aus this.pages. Kein Rueckfrage-Modal -
    // der Weg zurueck ist "Verwerfen" beim Verlassen oder die Revisionsablage,
    // und beides ist billiger als eine Bestaetigung bei jeder Kachel.
    removeCard(card) {
      const id = card?.dataset?.layoutItemId;
      if (!id) return;
      if (this._optionsSlot === card) this.closeOptions();
      for (const page of this.pages || []) {
        for (const group of page.groups) {
          const at = group.items.findIndex(item => item.id === id);
          if (at >= 0) group.items.splice(at, 1);
        }
      }
      this.editItems?.delete(id);
      card.remove();
      this.markUnsaved();
    },

    // Position ist Reihenfolge (Weg A): der Zug schiebt die Kachel im DOM und
    // dasselbe Item in seiner Gruppe. before === null heisst ans Ende.
    moveCardBefore(card, before) {
      if (!card || card === before) return;
      const parent = card.parentElement;
      if (!parent) return;
      if (before && before.parentElement === parent) parent.insertBefore(card, before);
      else parent.appendChild(card);

      const id = card.dataset.layoutItemId;
      const beforeID = before?.dataset?.layoutItemId || null;
      for (const page of this.pages || []) {
        for (const group of page.groups) {
          const from = group.items.findIndex(item => item.id === id);
          if (from < 0) continue;
          const [moved] = group.items.splice(from, 1);
          const to = beforeID ? group.items.findIndex(item => item.id === beforeID) : -1;
          if (to < 0) group.items.push(moved); else group.items.splice(to, 0, moved);
        }
      }
      this.markUnsaved();
    },

    // Spaltenzahl des Rasters - dieselbe auto-fill-Rechnung, die base.css
    // fuer repeat(auto-fill, minmax(18rem, 1fr)) anstellt.
    gridColumns() {
      return columnsFor(document.querySelector('.layout-grid')?.clientWidth || 0) || 1;
    },

    // Sucht ein Item ueber alle Seiten/Gruppen. mount() bindet damit die
    // Live-Kachel an ihr echtes this.pages-Objekt.
    findPagesItem(id) {
      for (const page of this.pages) {
        for (const group of page.groups) {
          const hit = group.items.find(item => item.id === id);
          if (hit) return hit;
        }
      }
      return null;
    },

    // Erst this.pages, dann der Rueckfall. Die Reihenfolge ist der Unterschied
    // zwischen "die Aenderung wird gespeichert" und "sie ist beim Speichern
    // weg": mount() laeuft direkt nach Alpine.initTree(), load() ist aber
    // asynchron und hat this.pages dann noch nicht gefuellt. editItems stand
    // in diesem Augenblick voller datasetToItem()-Rueckfaelle - loser Objekte,
    // die in keiner Gruppe haengen. Jede Modal- oder Groessenaenderung landete
    // dort und payload() sah sie nie. relinkEditItems() zieht die Karte am
    // Ende von load() an ihr echtes Item, und diese Abfrage tut dasselbe fuer
    // jeden Zugriff dazwischen.
    itemForSlot(slot) {
      const id = slot?.dataset?.layoutItemId;
      if (!id) return null;
      const real = this.findPagesItem(id);
      if (real) {
        this.editItems?.set(id, real);
        return real;
      }
      return this.editItems?.get(id) || null;
    },

    // Nach load() jede schon eingekleidete Kachel an ihr Item in this.pages
    // binden. Ohne das bliebe die Karte an ihrem datasetToItem()-Rueckfall
    // haengen (siehe itemForSlot).
    relinkEditItems() {
      if (!this.editItems) return;
      for (const id of [...this.editItems.keys()]) {
        const real = this.findPagesItem(id);
        if (real) this.editItems.set(id, real);
      }
    },

    // ⋯ -> Modal. Titel/Typ in den Kopf, optionsSheetHTML() in den Body, Kachel
    // als ausgewaehlt markieren. Der gemerkte this._optionsItem ist das Item,
    // das applyOptionChange() mutiert.
    openOptions(slot) {
      const item = this.itemForSlot(slot);
      const scrim = document.getElementById('layout-options-modal');
      if (!item || !scrim) return;
      const label = this.itemLabel(item);
      const columns = this.gridColumns();
      scrim.querySelector('[data-modal-title]').textContent = label;
      scrim.querySelector('[data-modal-kind]').textContent = item.type;
      const body = scrim.querySelector('[data-modal-body]');
      body.innerHTML = optionsSheetHTML(item, this.devices, label, columns);
      initEntityChoicesFor(item.id, body);
      document.querySelectorAll('.layout-edit-slot.is-selected').forEach(el => el.classList.remove('is-selected'));
      slot.classList.add('is-selected');
      scrim.classList.add('open');
      this._optionsItem = item;
      this._optionsSlot = slot;
    },

    closeOptions() {
      const scrim = document.getElementById('layout-options-modal');
      scrim?.classList.remove('open');
      document.querySelectorAll('.layout-edit-slot.is-selected').forEach(el => el.classList.remove('is-selected'));
      if (this._optionsItem) destroyEntityChoicesFor(this._optionsItem.id);
      this._optionsItem = null;
      this._optionsSlot = null;
    },

    // Body nach einer Auswahl neu aufbauen, die andere Felder ein-/ausblendet
    // (flow-scale, entity-value-ref) - dasselbe Muster wie handleWidgetChange().
    refillOptionsBody() {
      const scrim = document.getElementById('layout-options-modal');
      const body = scrim?.querySelector('[data-modal-body]');
      if (!body || !this._optionsItem) return;
      const label = this.itemLabel(this._optionsItem);
      const columns = this.gridColumns();
      body.innerHTML = optionsSheetHTML(this._optionsItem, this.devices, label, columns);
      initEntityChoicesFor(this._optionsItem.id, body);
    },

    // change im Modal-Body: dieselbe Rollen-Tabelle wie handleWidgetChange(),
    // aber auf ein reines this.pages-Item statt einen GridStack-Node (Weg A).
    applyOptionChange(event) {
      const item = this._optionsItem;
      const role = event.target?.dataset.role;
      if (!item || !role) return;
      const t = event.target;
      // 'visible' hat hier keinen Zweig mehr: die Sichtbarkeit steuert allein
      // das Auge im Chrome (toggleVisible), das Modal fuehrt sie nicht mehr.
      if (role === 'category') {
        const set = new Set(item.visibleCategories || []);
        if (t.checked) set.add(t.value); else set.delete(t.value);
        item.visibleCategories = [...set];
      } else if (role === 'entity-group-title') {
        item.title = t.value;
      } else if (role === 'entity-refs') {
        item.entityRefs = Array.from(t.selectedOptions).map(option => option.value);
        // Die Kompaktkachel zeigt hoechstens drei Zeilen - mehr passen
        // optisch nicht, und der Server schneidet sie beim Speichern ohnehin
        // ab (normalizeLayout).
        if (item.type === 'device') item.entityRefs = item.entityRefs.slice(0, 3);
      } else if (role === 'entity-value-ref') {
        item.ref = t.value;
        this.syncSlotForItem(item);
        this.refillOptionsBody();
      } else if (role === 'device-ref') {
        item.ref = t.value;
        // Die bisherige Werteauswahl gehoerte zum alten Geraet - ihre Refs
        // gibt es am neuen nicht.
        if (item.type === 'device') item.entityRefs = [];
        this.syncSlotForItem(item);
        this.refillOptionsBody();
      } else if (role === 'flow-scale') {
        item.flowScale = t.value;
        this.refillOptionsBody();
      } else if (role === 'display') {
        item.display = item.type === 'battery_status'
          ? (t.value === 'trajectory' ? 'trajectory' : 'column')
          : (t.value === 'compact' ? 'compact' : 'detail');
        // Die Detailkachel kennt keine feste Zeilenauswahl.
        if (item.type === 'device' && item.display !== 'compact') item.entityRefs = [];
        this.syncSlotForItem(item);
        this.refillOptionsBody();
      } else if (role === 'span') {
        item.span = t.value;
      } else if (role === 'height') {
        const raw = Number(t.value);
        item.height = Number.isFinite(raw) && raw >= 1 ? Math.min(12, Math.round(raw)) : 0;
      } else if (ENERGY_OPTION_BY_ROLE[role]) {
        const {field, kind} = ENERGY_OPTION_BY_ROLE[role];
        item[field] = kind === 'checkbox' ? (t.checked ? 'on' : 'off')
          : kind === 'number' ? Number(t.value)
          : t.value;
      } else {
        return;
      }
      this.markUnsaved();
    },

    syncEyeButton(slot, item) {
      const btn = slot?.querySelector('[data-role="visible-toggle"]');
      if (!btn) return;
      const visible = item.visible !== false;
      btn.setAttribute('aria-pressed', visible ? 'true' : 'false');
      btn.innerHTML = visible ? ICON.eye : ICON.eyeOff;
    },

    // Haelt die ausgewaehlte Kachel mit dem Item im Modal gleich. Der
    // *Inhalt* der server-gerenderten Karte bleibt stehen - er stimmt erst
    // nach dem Speichern wieder, wie bei jeder Ref-Aenderung. Was hier
    // nachgezogen wird, ist alles, was der Editor selbst liest: die data-
    // Attribute und der Name in Kachel und Modalkopf.
    syncSlotForItem(item) {
      const slot = this._optionsSlot;
      if (!slot || !item) return;
      slot.dataset.layoutItemRef = item.ref || '';
      if (item.type === 'device') slot.dataset.display = item.display || 'detail';
      if (item.type === 'battery_status') slot.dataset.display = item.display || 'column';
      const label = this.itemLabel(item);
      const title = slot.querySelector('.card-title b') || slot.querySelector('.layout-card-placeholder b');
      if (title) title.textContent = label;
      const modalTitle = document.querySelector('#layout-options-modal [data-modal-title]');
      if (modalTitle) modalTitle.textContent = label;
    },

    toggleVisible(slot) {
      const item = this.itemForSlot(slot);
      if (!item) return;
      item.visible = item.visible === false;
      slot.classList.toggle('is-hidden', item.visible === false);
      this.syncEyeButton(slot, item);
      this.markUnsaved();
    },

    markUnsaved() {
      this.unsaved = true;
      this.updateStatus();
    },

    // Spiegelt this.unsaved in die Werkzeugleiste: die "Ungespeichert"-Anzeige
    // und der "Änderungen verwerfen"-Knopf stehen nur bei offenen Aenderungen
    // dort. Beide werden imperativ ueber [hidden] geschaltet - sie sind aus
    // dem Fragment in die Live-Leiste verschoben und damit ausserhalb von
    // Alpines Reaktivitaet.
    updateStatus() {
      for (const el of document.querySelectorAll('.layout-status[data-status], [data-mode-discard]')) el.hidden = !this.unsaved;
    },

    // "Änderungen verwerfen" aus der Werkzeugleiste: fragt ueber denselben
    // Waechter wie das Verlassen (confirmLeave oeffnet #layout-guard-modal in
    // den Projektfarben) und laedt bei Bestaetigung den gespeicherten Stand
    // neu - der Editor bleibt offen. confirmLeave() leert unsaved schon und
    // stoesst load() an; das layout-saved-Ereignis laesst zusaetzlich die
    // Uebersicht ihr Fragment neu holen, damit auch lokale Umbauten (neue
    // Karten, Umsortierungen) aus dem Raster verschwinden.
    async discardChanges() {
      if (!this.unsaved) return;
      const discarded = await this.confirmLeave('discard');
      if (!discarded) return;
      window.dispatchEvent(new CustomEvent('layout-saved'));
    },

    setToolbox(open) {
      const toolbox = document.getElementById('toolbox');
      if (!toolbox) return;
      toolbox.classList.toggle('open', open);
      toolbox.setAttribute('aria-hidden', open ? 'false' : 'true');
      for (const toggle of document.querySelectorAll('[data-toolbox-toggle]')) {
        toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
        toggle.classList.toggle('on', open);
      }
      if (open) {
        this.syncToolboxOffset();
        this.renderToolbox();
        toolbox.querySelector('[data-tb-search]')?.focus?.({preventScroll: true});
      }
    },

    // Die Toolbox steht position:fixed am <body> und reicht von --toolbox-top
    // bis zur Fensterunterkante. Ihr Deckel ist nicht 0, sondern die
    // Unterkante der klebenden Werkzeugleiste: oben auf der Seite steht die
    // noch unter Kopf und Tab-Leiste, nach unten gescrollt klebt sie am
    // Fensterrand. So bleibt die Leiste samt "Baustein"-Knopf immer sichtbar,
    // und die Toolbox nutzt trotzdem die ganze uebrige Hoehe - auch auf einer
    // leeren Seite.
    syncToolboxOffset() {
      const toolbox = document.getElementById('toolbox');
      // Geschlossen steht sie hinter der Fensterkante - dann kostet jedes
      // Scrollereignis nichts.
      if (!toolbox || !toolbox.classList.contains('open')) return;
      const bar = document.querySelector('.layout-toolbar');
      const bottom = bar?.getBoundingClientRect?.().bottom;
      toolbox.style.setProperty('--toolbox-top', `${Math.max(0, Math.round(bottom || 0))}px`);
    },

    // Baut [data-tb-list] aus dem Katalog: bei leerer Suche das aktive
    // Register, sonst der Volltreffer ueber alle drei. Das Trefferarray liegt
    // als data-hits am Listencontainer, damit der Klick-Handler den Eintrag
    // ueber den Index findet (wie im Entwurf).
    renderToolbox() {
      const toolbox = document.getElementById('toolbox');
      const list = toolbox?.querySelector('[data-tb-list]');
      if (!list) return;
      // Wohin der naechste Baustein geht, steht im Fuss der Toolbox. Das war
      // ein fester Beispieltext aus dem Entwurf; jetzt nennt er die Seite, auf
      // der man wirklich gerade steht.
      const target = toolbox.querySelector('[data-tb-target]');
      if (target) target.textContent = this.activePage || 'Übersicht';
      const query = (toolbox.querySelector('[data-tb-search]')?.value || '').trim();
      const cat = catalog(this.devices);
      const hits = query ? filterCatalog(cat, query) : (cat[this._tbTab] || []);
      if (!hits.length) {
        list.innerHTML = `<p class="layout-toolbox-empty">Kein Baustein passt zu „${escapeHTML(query)}“.</p>`;
        list.dataset.hits = '[]';
        return;
      }
      list.innerHTML = hits.map((entry, index) =>
        `<button class="layout-toolbox-item" type="button" data-add="${index}">`
        + `<span class="thumb">${THUMB[entry.type] || THUMB.entity_value}</span>`
        + `<span class="tx"><b>${escapeHTML(entry.title)}</b><span>${escapeHTML(entry.desc || '')}</span></span>`
        + `</button>`).join('');
      // Klick und Ziehen fuehren zum selben addFromCatalog(); der Klick legt
      // ans Ende ab, das Ziehen an die Zeigerposition.
      for (const btn of list.querySelectorAll('[data-add]')) btn.draggable = true;
      list.dataset.hits = JSON.stringify(hits);
    },

    // energy_summary fehlt hier absichtlich: der Kartentyp wurde entfernt
    // (design.md Abschnitt 1), bleibt aber in cardCatalog, damit ein vor der
    // Aenderung gespeichertes Layout weiter laedt - overview.html rendert
    // ein vorhandenes energy_summary-Item darum still, statt es abzulehnen.
    get itemOptions() {
      const options = [
        {id: 'energy-flow', type: 'energy_flow', ref: '', label: 'Energie: Energiefluss'},
        {id: 'diagnostics', type: 'diagnostics', ref: '', label: 'Diagnosen'},
        {id: 'energy-band', type: 'energy_band', ref: '', label: 'Energie: Bilanzband'},
        {id: 'energy-ring', type: 'energy_ring', ref: '', label: 'Energie: Autarkie-Ring'},
        {id: 'energy-board', type: 'energy_board', ref: '', label: 'Energie: Datentafel'},
        {id: 'energy-day', type: 'energy_day', ref: '', label: 'Energie: Tagesband'},
        {id: 'energy-schema', type: 'energy_schema', ref: '', label: 'Energie: Anlagenschema'},
        {id: 'energy-status', type: 'energy_status', ref: '', label: 'Energie: Statuskarte'},
        {id: 'battery-status', type: 'battery_status', ref: '', label: 'Speicher: Statuskarte'},
        // Kein Ref hier: eine einzelne, generische Auswahl fuer alle
        // Entitaeten statt einer Option je Entitaet - seit der Abschaffung
        // des Kartentyps 'entity' die einzige Entitaetenkarte mit einem
        // einzelnen Ref. Die konkrete Entitaet waehlt man danach im
        // Widget selbst (siehe entityValuePicker in widgetHTML()) - addItem()
        // erzeugt darum fuer diese Option jedes Mal eine frische Karten-ID
        // statt die Option-ID wiederzuverwenden.
        {id: 'entity-value', type: 'entity_value', ref: '', label: 'Wert-Karte'},
      ];
      for (const device of this.devices) {
        options.push({id: `device:${device.id}`, type: 'device', ref: device.id, label: `Gerät: ${device.name || device.id}`});
      }
      return options;
    },

    // entity_group hat kein Gegenstueck in itemOptions - der Titel kommt vom
    // Nutzer, nicht von einem Ref, siehe addEntityGroup(). entity_value hat
    // seit der generischen 'entity-value'-Option ebenfalls keine passende
    // option.id mehr, sobald eine Entitaet gewaehlt wurde (die Karten-ID ist
    // dann eine frische newID(), keine der itemOptions-IDs) - der Name kommt
    // stattdessen aus dem gewaehlten ref selbst.
    itemLabel(item) {
      if (item.type === 'entity_group') return item.title || 'Entitätenliste';
      if (item.type === 'entity_value') {
        const entity = allEntityOptions().find(option => option.ref === item.ref);
        return entity ? entity.label : 'Wert-Karte';
      }
      // Typ und Ref statt der ID: jede aus der Toolbox gelegte Karte bekommt
      // eine frische ID (siehe addFromCatalog), die in itemOptions gar nicht
      // vorkommt. Die ID bleibt der letzte Rueckfall - fuer Layouts, die noch
      // aus der Zeit der Katalog-IDs stammen.
      const byKind = this.itemOptions.find(option => option.type === item.type && (option.ref || '') === (item.ref || ''));
      return byKind?.label
        || this.itemOptions.find(option => option.id === item.id)?.label
        || item.ref || item.id;
    },

    async load() {
      this.loading = true;
      try {
        const [layout, devices] = await Promise.all([
          requestJSON('/api/v1/layout'),
          requestJSON('/api/v1/devices'),
        ]);
        this.devices = devices;
        allDevices = devices;
        cardTypes = layout.card_types || {};
        this.pages = (layout.pages || []).map((page, pageIndex) => ({
          ...page,
          id: page.id || newID('page'),
          order: page.order ?? pageIndex,
          groups: (page.groups || []).map(group => ({
            ...group,
            id: group.id || newID('group'),
            items: (group.items || (group.entity_ids || []).map(entityID => ({id: `entity:${entityID}`, type: 'entity_value', ref: entityID, span: '1', visible: true})))
              .map(item => ({
                ...item, visibleCategories: item.visible_categories || [], flowScale: item.flow_scale || '', display: item.display || '', height: item.height || 0,
                title: item.title || '', entityRefs: item.entity_refs || [],
                ...Object.fromEntries(ENERGY_OPTIONS.map(({field, json}) => [field, item[json] || ''])),
              })),
          })),
        }));
        if (!this.pages.length) this.pages = this.defaultPages();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
        // Der Bearbeitungsmodus haengt in der Regel schon, wenn diese Antwort
        // eintrifft - erst jetzt gibt es echte Items zum Verbinden.
        this.relinkEditItems();
        this.syncActivePage();
        // Erst jetzt stehen alle Items (auch die ausgeblendeten, die der
        // Server nicht rendert) - jetzt koennen sie ins Editor-Raster.
        this.syncHiddenCards();
        this.$nextTick(() => this.renderGrids());
      }
    },

    defaultPages() {
      const items = ['energy-flow', 'diagnostics'];
      for (const device of this.devices) items.push(`device:${device.id}`);
      return [{
        id: newID('page'),
        name: 'Übersicht',
        order: 0,
        groups: [{
          id: newID('group'),
          name: 'Dashboard',
          items: items.map(id => {
            const option = this.itemOptions.find(item => item.id === id);
            return {...option, span: cardType(option?.type).default_span, visible: true, visibleCategories: [], flowScale: option?.type === 'energy_flow' ? 'width' : '', height: 0, ...defaultEnergyOptions(option?.type)};
          }),
        }],
      }];
    },

    // (Re)mounts every group's Gridstack instance from current state. Used
    // after load() and after any structural change (add/remove page/group)
    // whose new/changed .grid-stack containers need a live instance -
    // mirrors devicemap.page.js's destroy/recreate-on-change pattern.
    renderGrids() {
      for (const page of this.pages) {
        for (const group of page.groups) this.renderGroup(group);
      }
    },

    // Kein Template rendert derzeit noch ein `#layout-grid-<groupID>`-Element
    // (der Editor laeuft ueber mount()/applyOptionChange() auf #overview-live,
    // siehe test/layout-editor.test.mjs: "kein #layout-grid-group-1-Container
    // vorhanden, renderGrids() no-opt harmlos") - der `if (!el ...) return;`
    // unten macht diesen Pfad und handleWidgetChange() damit unerreichbaren,
    // ungetesteten Legacy-Code. Deshalb kennt handleWidgetChange() (unten)
    // weder role 'display' noch 'device-ref', und cardType() dort haengt noch
    // an node.type statt cardKey(node) - vor einer echten Reaktivierung dieses
    // Pfads muessten beide wie applyOptionChange() nachgezogen werden.
    renderGroup(group) {
      grids.get(group.id)?.destroy();
      grids.delete(group.id);
      observers.get(group.id)?.disconnect();
      observers.delete(group.id);
      const el = document.getElementById(`layout-grid-${group.id}`);
      if (!el || !window.GridStack) return;
      const columns = columnsFor(el.clientWidth);
      lastColumns.set(group.id, columns);
      const grid = window.GridStack.init({
        column: columns,
        cellHeight: '7rem',
        float: false,
        disableResize: true,   // Groesse kommt aus dem Auswahlfeld, nicht aus dem Ziehen
        acceptWidgets: true,   // Ziehen zwischen Gruppen bleibt
        handle: '.layout-item-handle',
      }, el);
      grid.load(group.items.map(item => toGridNode(item, this.itemLabel(item), refMissing(item, this.devices), columns)));
      for (const itemEl of grid.getGridItems()) {
        const node = itemEl.gridstackNode;
        if (node?.type === 'entity_group') initEntityChoicesFor(node.id, itemEl);
      }
      grid.compact('list');

      // compact('list') loest selbst ein change aus. Das Flag laesst den
      // verschachtelten Aufruf sofort umkehren; die Auswertung uebernimmt
      // der aeussere.
      let packing = false;
      const settle = () => {
        if (packing) return;
        packing = true;
        grid.compact('list');
        packing = false;
        group.items = itemsFromGrid(grid);
        this.unsaved = true;
      };
      grid.on('change added removed', settle);
      el.addEventListener('change', event => this.handleWidgetChange(event, grid, group));
      el.addEventListener('click', event => this.handleWidgetClick(event, grid));
      grids.set(group.id, grid);

      if (window.ResizeObserver) {
        const observer = new ResizeObserver(entries => {
          const next = columnsFor(entries[0].contentRect.width);
          if (next === lastColumns.get(group.id)) return;
          lastColumns.set(group.id, next);
          this.applyColumns(grid, group, next);
        });
        observer.observe(el);
        observers.set(group.id, observer);
      }
    },

    // Spaltenwechsel: Gridstack umstellen, dann jede Kachel neu vermessen
    // *und* neu beschriften - welche Groessenklassen den Hinweis "wirkt hier
    // wie voll" tragen, haengt an der Spaltenzahl.
    applyColumns(grid, group, columns) {
      grid.column(columns, 'list');
      for (const itemEl of grid.getGridItems()) {
        const node = itemEl.gridstackNode;
        if (!node) continue;
        const item = fromGridNode(node);
        grid.update(itemEl, {
          w: tracksFor(item.span, columns),
          content: widgetHTML(item, this.itemLabel(item), refMissing(item, this.devices), columns),
        });
        if (item.type === 'entity_group') initEntityChoicesFor(item.id, itemEl);
      }
      grid.compact('list');
      group.items = itemsFromGrid(grid);
    },

    handleWidgetChange(event, grid, group) {
      const el = event.target.closest('.grid-stack-item');
      const node = el?.gridstackNode;
      if (!node) return;
      const role = event.target.dataset.role;
      if (role === 'visible') {
        node.visible = event.target.checked;
      } else if (role === 'category') {
        const categories = new Set(node.visibleCategories || []);
        if (event.target.checked) categories.add(event.target.value); else categories.delete(event.target.value);
        node.visibleCategories = [...categories];
      } else if (role === 'entity-group-title') {
        node.title = event.target.value;
      } else if (role === 'entity-refs') {
        // Choices.js haelt das zugrundeliegende <select multiple> als Quelle
        // der Wahrheit synchron (siehe initEntityChoicesFor()) - selectedOptions
        // spiegelt darum die per Choices getroffene Auswahl direkt wider.
        node.entityRefs = Array.from(event.target.selectedOptions).map(option => option.value);
      } else if (role === 'entity-value-ref') {
        node.ref = event.target.value;
        // Neu rendern, damit Kartenname/„Referenz fehlt"-Hinweis sofort die
        // gewaehlte Entitaet widerspiegeln - dasselbe Muster wie flow-scale.
        const item = fromGridNode(node);
        grid.update(el, {content: widgetHTML(item, this.itemLabel(item), refMissing(item, this.devices), grid.getColumn())});
      } else if (role === 'flow-scale') {
        node.flowScale = event.target.value;
        // The speed-reference fields only show up in "speed" mode - re-render
        // so picking either option immediately shows/hides them.
        const item = fromGridNode(node);
        grid.update(el, {content: widgetHTML(item, this.itemLabel(item), refMissing(item, this.devices), grid.getColumn())});
      } else if (role === 'span') {
        node.span = event.target.value;
        grid.update(el, {w: tracksFor(node.span, grid.getColumn())});
        grid.compact('list');
      } else if (role === 'height') {
        const raw = Number(event.target.value);
        node.height = Number.isFinite(raw) && raw >= 1 ? Math.min(12, Math.round(raw)) : 0;
        grid.update(el, {h: node.height || heightUnits(cardType(node.type).min_height)});
        grid.compact('list');
      } else if (ENERGY_OPTION_BY_ROLE[role]) {
        const {field, kind} = ENERGY_OPTION_BY_ROLE[role];
        node[field] = kind === 'checkbox' ? (event.target.checked ? 'on' : 'off')
          : kind === 'number' ? Number(event.target.value)
          : event.target.value;
      } else {
        return;
      }
      group.items = itemsFromGrid(grid);
      this.unsaved = true;
    },

    handleWidgetClick(event, grid) {
      const button = event.target.closest('[data-role="remove-item"]');
      if (!button) return;
      const el = button.closest('.grid-stack-item');
      if (!el) return;
      if (el.gridstackNode?.type === 'entity_group') destroyEntityChoicesFor(el.gridstackNode.id);
      grid.removeWidget(el);
    },

    // Index der aktiven Seite; faellt auf die erste zurueck, wenn der Name
    // nicht (mehr) passt.
    activePageIndex() {
      const at = (this.pages || []).findIndex(page => page.name === this.activePage);
      return at < 0 ? 0 : at;
    },

    // Welche Layout-Seite ist gemeint? Gewaehlt wird sie in der Tab-Leiste
    // (dashboardShell.activePage), gerendert hat sie der Server in
    // [data-layout-page]. Bis 2026-09 uebernahm mount() den Namen nur beim
    // allerersten Mal ("this.activePage || ..."); nach einem Seitenwechsel
    // zeigte der Editor darum weiter auf die alte Seite - "Seite bearbeiten"
    // benannte die falsche um, und neue Bausteine landeten auf ihr.
    //
    // Auseinanderlaufen duerfen die beiden trotzdem: eine mit "+ Seite"
    // angelegte Seite kennt der Server noch nicht und liefert auf ihren Namen
    // die erste gespeicherte zurueck. Dann rendert der Editor sie selbst.
    syncActivePage() {
      const host = document.querySelector('[data-layout-page]');
      const chosen = window.__dashboardShell__?.activePage
        || host?.dataset.layoutPage
        || this.pages?.[0]?.name || '';
      const known = !this.pages?.length || this.pages.some(page => page.name === chosen);
      this.activePage = known ? chosen : (host?.dataset.layoutPage || this.pages[0].name);
      if (host && this.pages?.length && host.dataset.layoutPage !== this.activePage) this.renderLocalPage();
    },

    // Das Raster auf this.activePage umstellen, ohne den Server zu fragen.
    // Nur fuer Seiten, die es dort noch nicht gibt: die Karten stehen als
    // dieselben Platzhalter da, die auch eine frisch abgelegte Karte bekommt
    // ("erscheint nach dem Speichern"). Auch ausgeblendete Items kommen mit -
    // gedimmt ueber dressCard(); im Editor sollen sie sichtbar bleiben, damit
    // man sie ueber das Auge wieder einblenden kann.
    renderLocalPage() {
      const page = this.pages?.[this.activePageIndex()];
      if (!page) return;
      let host = document.querySelector('.layout-grid [data-layout-page]');
      if (!host) {
        // Leerzustand: im Raster steht bis jetzt nur "Noch keine Seite
        // eingerichtet". Mit der ersten Seite braucht es den Traeger, den der
        // Server sonst rendert.
        const grid = document.querySelector('.layout-grid');
        if (!grid) return;
        grid.textContent = '';
        host = document.createElement('div');
        grid.appendChild(host);
      }
      host.dataset.layoutPage = page.name;
      host.textContent = '';
      this.editItems = this.editItems || new Map();
      for (const group of page.groups || []) {
        for (const item of group.items || []) {
          const card = this.buildCard(item, item.visible === false ? 'über das Auge wieder einblenden' : undefined);
          host.appendChild(card);
          this.editItems.set(item.id, item);
          this.dressCard(card, item);
        }
      }
    },

    // Der Kartenrumpf einer noch nicht gespeicherten Kachel. Ein Ort fuer die
    // Wege dorthin: frisch aus der Toolbox (addFromCatalog), lokaler
    // Seitenwechsel (renderLocalPage) und nachgezogene ausgeblendete Kacheln
    // (syncHiddenCards). data-editor-injected markiert sie fuer unmount(): im
    // Ansichtsmodus haben sie ohne Server-Markup nichts zu suchen. `hint`
    // reicht placeholderHTML() den Untertext durch.
    buildCard(item, hint) {
      // cardKey statt item.type: die kompakte Geraetekachel hat ihren eigenen
      // Katalogeintrag mit kleinerer Mindesthoehe.
      const type = cardType(cardKey(item));
      const card = document.createElement('div');
      card.className = `layout-grid-item layout-grid-item-${item.span}`;
      card.dataset.layoutItemId = item.id;
      card.dataset.layoutItemKind = item.type;
      card.dataset.layoutItemRef = item.ref || '';
      if (item.type === 'device') card.dataset.display = item.display || 'detail';
      if (item.type === 'battery_status') card.dataset.display = item.display || 'column';
      card.dataset.editorInjected = 'true';
      card.style.setProperty('--card-min-height', type.min_height);
      if (item.height) card.style.setProperty('--card-height', `${heightRem(item.height)}rem`);
      card.innerHTML = placeholderHTML(item, this.itemLabel(item), hint);
      return card;
    },

    // Ein Ort, an dem die Navigation von Seitenaenderungen erfaehrt. `active`
    // ist neu gegenueber addPage(): beim Umbenennen muss der Tab mitwandern,
    // sonst zeigte die Leiste den alten und den neuen Namen nebeneinander.
    announcePages() {
      document.dispatchEvent(new CustomEvent('layout-pages-changed', {
        detail: {pages: this.pages.map(page => page.name), active: this.activePage},
      }));
    },

    openPageOptions() {
      const scrim = document.getElementById('layout-page-modal');
      const page = this.pages?.[this.activePageIndex()];
      if (!scrim || !page) return;
      this.activePage = page.name;
      const input = scrim.querySelector('[data-page-name]');
      if (input) input.value = page.name;
      // Die letzte Seite bleibt: ohne Seite gaebe es nach Spec 4.3 keinen
      // Editieren-Knopf mehr und damit keinen Weg zurueck in den Editor.
      const remove = scrim.querySelector('[data-page-remove]');
      if (remove) remove.disabled = this.pages.length < 2;
      scrim.classList.add('open');
    },

    closePageOptions() {
      document.getElementById('layout-page-modal')?.classList.remove('open');
    },

    renamePage(value) {
      const name = String(value ?? '').trim();
      if (!name) return;
      const page = this.pages?.[this.activePageIndex()];
      if (!page || page.name === name) return;
      page.name = name;
      this.activePage = name;
      const host = document.querySelector('[data-layout-page]');
      if (host) host.dataset.layoutPage = name;
      this.announcePages();
      this.markUnsaved();
    },

    movePageBy(delta) {
      const at = this.activePageIndex();
      if (at + delta < 0 || at + delta >= (this.pages?.length || 0)) return;
      this.movePage(at, delta);
      this.announcePages();
      this.markUnsaved();
    },

    removeActivePage() {
      if ((this.pages?.length || 0) < 2) return;
      this.removePage(this.activePageIndex());
      this.activePage = this.pages[0]?.name || '';
      // Im Raster stehen noch die Kacheln der geloeschten Seite - der Server
      // weiss von ihr ja nichts. Das Nachziehen macht der Editor selbst.
      this.renderLocalPage();
      this.closePageOptions();
      this.announcePages();
      this.markUnsaved();
    },

    // Die neue Seite kommt gleich mit einer Gruppe: ohne die haette
    // addFromCatalog() nichts, woran es die erste Kachel haengt. Der Name wird
    // durchnummeriert, damit zwei neue Seiten nicht denselben Tab teilen -
    // die Tab-Leiste fuehrt Seiten ueber ihren Namen.
    addPage() {
      let name = 'Neue Seite';
      for (let n = 2; this.pages.some(page => page.name === name); n++) name = `Neue Seite ${n}`;
      this.pages.push({id: newID('page'), name, order: this.pages.length, groups: [{id: newID('group'), name: 'Dashboard', items: []}]});
      this.activePage = name;
      // Der Server kennt die Seite noch nicht - ein Fragment-Aufruf mit ihrem
      // Namen brachte die erste gespeicherte zurueck. Also selbst rendern.
      this.renderLocalPage();
      this.markUnsaved();
    },

    movePage(pageIndex, delta) {
      const targetIndex = pageIndex + delta;
      if (targetIndex < 0 || targetIndex >= this.pages.length) return;
      [this.pages[pageIndex], this.pages[targetIndex]] = [this.pages[targetIndex], this.pages[pageIndex]];
      this.pages.forEach((page, index) => page.order = index);
    },

    removePage(pageIndex) {
      const page = this.pages[pageIndex];
      for (const group of page.groups) {
        for (const item of group.items) if (item.type === 'entity_group') destroyEntityChoicesFor(item.id);
        grids.get(group.id)?.destroy();
        grids.delete(group.id);
        observers.get(group.id)?.disconnect();
        observers.delete(group.id);
        lastColumns.delete(group.id);
      }
      this.pages.splice(pageIndex, 1);
      this.pages.forEach((p, index) => p.order = index);
    },

    addGroup(page) {
      page.groups.push({id: newID('group'), name: 'Neue Gruppe', items: []});
      this.$nextTick(() => this.renderGrids());
    },

    removeGroup(page, groupIndex) {
      const group = page.groups[groupIndex];
      for (const item of group.items) if (item.type === 'entity_group') destroyEntityChoicesFor(item.id);
      grids.get(group.id)?.destroy();
      grids.delete(group.id);
      observers.get(group.id)?.disconnect();
      observers.delete(group.id);
      lastColumns.delete(group.id);
      page.groups.splice(groupIndex, 1);
    },

    addItem(group, optionId) {
      const option = this.itemOptions.find(item => item.id === optionId);
      const grid = grids.get(group.id);
      if (!option || !grid) return;
      // 'entity-value' ist die einzige generische Option (kein Ref, siehe
      // itemOptions): mehrere Wert-Karten in derselben Gruppe muessen
      // moeglich sein, darum eine frische ID statt der Dedup-Pruefung
      // unten - dieselbe Begruendung wie addEntityGroup().
      const generic = option.type === 'entity_value';
      if (!generic && group.items.some(item => item.id === option.id)) return;
      const item = {
        id: generic ? newID('entity-value') : option.id, type: option.type, ref: option.ref, visible: true, visibleCategories: [],
        flowScale: option.type === 'energy_flow' ? 'width' : '',
        span: cardType(option.type).default_span, height: 0,
        ...defaultEnergyOptions(option.type),
      };
      grid.addWidget(toGridNode(item, this.itemLabel(item), refMissing(item, this.devices), grid.getColumn()));
    },

    // entity_group durchlaeuft addItem() nicht: es hat keinen festen Ref und
    // damit keine natuerliche eindeutige option.id - addItem()'s Dedup-Check
    // (ein Item pro option.id je Gruppe) wuerde eine zweite Entitaetenliste
    // in derselben Gruppe verhindern. Jeder Aufruf legt stattdessen eine neue
    // Karte mit frischer ID an.
    addEntityGroup(group) {
      const grid = grids.get(group.id);
      if (!grid) return;
      const item = {
        id: newID('entity-group'), type: 'entity_group', ref: '', visible: true, visibleCategories: [],
        flowScale: '', span: cardType('entity_group').default_span, height: 0,
        title: 'Entitäten', entityRefs: [],
        ...defaultEnergyOptions('entity_group'),
      };
      const el = grid.addWidget(toGridNode(item, this.itemLabel(item), refMissing(item, this.devices), grid.getColumn()));
      initEntityChoicesFor(item.id, el);
    },

    // Fuegt ein aus dem Katalog gewaehltes Item zur ersten Gruppe der aktiven
    // Seite hinzu - und ins DOM, an derselben Stelle. Ohne das zweite wuerde
    // die Karte erst nach einem Neuladen auftauchen. `before` ist die Kachel,
    // vor der eingefuegt wird (null = ans Ende).
    addFromCatalog(entry, before = null) {
      // Die *aktive* Seite, nicht die erste: bis 2026-09 stand hier
      // pages[0].groups[0]. Auf einer zweiten Seite landete jeder Baustein
      // damit unsichtbar auf der ersten - und eine frisch angelegte Seite hat
      // ueberhaupt keine Gruppe, dort fiel der Aufruf still durch.
      const page = this.pages?.[this.activePageIndex()];
      if (!page) return null;
      if (!page.groups?.length) (page.groups = page.groups || []).push({id: newID('group'), name: 'Dashboard', items: []});
      const beforeID = before?.dataset?.layoutItemId || null;
      // In die Gruppe, in der die Nachbarkachel steht - sonst in die letzte,
      // damit die Reihenfolge im Modell der im DOM entspricht (die Uebersicht
      // rendert die Gruppen hintereinander in eine einzige Kachelfolge).
      const group = (beforeID && page.groups.find(g => (g.items || []).some(item => item.id === beforeID)))
        || page.groups[page.groups.length - 1];
      const type = cardType(entry.type);
      // Immer eine frische ID, nie die des Katalogeintrags. Sonst trugen die
      // Diagnose-Karte auf Seite 1 und die auf Seite 2 beide die ID
      // 'diagnostics': findPagesItem() fand die erste, das Optionen-Modal der
      // zweiten aenderte also die erste mit, und Entfernen loeschte beide.
      // Der Kartenname haengt seit derselben Aenderung an Typ und Ref
      // (itemLabel), nicht mehr an der ID.
      const item = {
        id: newID('item'),
        type: entry.type, ref: entry.ref || '',
        span: type.default_span, height: 0, visible: true,
        ...(entry.type === 'entity_group' ? {title: 'Entitäten', entityRefs: []} : {}),
      };

      const host = document.querySelector('.layout-grid [data-layout-page]') || document.querySelector('.layout-grid');
      if (host) {
        const card = this.buildCard(item);
        if (before && before.parentElement === host) host.insertBefore(card, before);
        else host.appendChild(card);
        this.editItems?.set(item.id, item);
        this.dressCard(card, item);
        card.classList.add('is-entering');
        card.addEventListener('animationend', () => card.classList.remove('is-entering'), {once: true});
      }

      const at = beforeID ? group.items.findIndex(entry2 => entry2.id === beforeID) : -1;
      if (at < 0) group.items.push(item); else group.items.splice(at, 0, item);
      this.markUnsaved();
      return item;
    },

    payload() {
      const pages = this.pages.map(page => ({
        ...page,
        groups: page.groups.map(group => ({
          ...group,
          items: group.items.map(source => {
            const {visibleCategories, flowScale, height, entityRefs, display, ...item} = source;
            for (const field of ENERGY_OPTION_FIELDS) delete item[field];
            return {
              ...item,
              ...(visibleCategories && visibleCategories.length ? {visible_categories: visibleCategories} : {}),
              ...(flowScale ? {flow_scale: flowScale} : {}),
              ...(height ? {height} : {}),
              ...(entityRefs && entityRefs.length ? {entity_refs: entityRefs} : {}),
              ...(display ? {display} : {}),
              ...Object.fromEntries(ENERGY_OPTIONS.filter(({field}) => source[field]).map(({field, json}) => [json, source[field]])),
            };
          }),
        })),
      }));
      return {version: 3, pages};
    },

    revisionConfig() {
      return {
        basePath: '/api/v1/layout',
        current: () => this.payload(),
        reload: () => this.load(),
        label: 'Revisionen des Layouts',
      };
    },

    async save() {
      this.saving = true;
      try {
        await requestJSON('/api/v1/layout', {
          method: 'PUT',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify(this.payload()),
        });
        this.$store.toasts.push('Layout gespeichert.');
        this.unsaved = false;
        // Die Uebersicht holt ein neues Layout nicht mehr beilaeufig beim
        // naechsten Fragment-Tausch ab - der faellt bei reinen Energierastern
        // weg. Ohne dieses Ereignis wuerde ein gespeichertes Layout dort nie
        // sichtbar.
        window.dispatchEvent(new CustomEvent('layout-saved'));
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.saving = false;
      }
    },

    confirmUnsavedUnload(event) {
      if (!this.unsaved) return;
      event.preventDefault();
      event.returnValue = '';
    },

    // Fragt vor einem Tab- oder Moduswechsel nach, wenn es ungespeicherte
    // Aenderungen gibt - ueber das Themen-<dialog> #layout-guard-modal, nicht
    // ueber window.confirm. Loest auf true auf, wenn verworfen werden darf,
    // auf false, wenn der Editor festgehalten wird. reason ('tab' | 'mode')
    // koennte den Text abwandeln, wird aber nicht ausgewertet. beforeunload
    // (confirmUnsavedUnload) bleibt zusaetzlich bestehen - es faengt den
    // echten Tab-Schluss, den ein <dialog> nicht aufhalten kann.
    confirmLeave(reason) {
      void reason;
      if (!this.unsaved) return Promise.resolve(true);
      const dialog = document.getElementById('layout-guard-modal');
      if (!dialog) return Promise.resolve(true);
      return new Promise(resolve => {
        const finish = ok => {
          dialog.removeEventListener('click', onClick);
          closeDialog(dialog);
          resolve(ok);
        };
        const onClick = e => {
          if (e.target.closest('[data-guard-discard]')) {
            // "Verwerfen" heisst verwerfen: ohne das Zuruecksetzen blieb
            // unsaved stehen, der Waechter fragte beim naechsten Tab-Klick
            // erneut - und die weggeworfenen Aenderungen standen weiter in
            // this.pages und wanderten beim naechsten Speichern doch noch mit.
            this.unsaved = false;
            this.updateStatus();
            Promise.resolve(this.load());
            finish(true);
            return;
          }
          if (e.target.closest('[data-guard-cancel]')) finish(false);
        };
        dialog.addEventListener('click', onClick);
        openDialog(dialog);
      });
    },
  });

  // Exposed for test/layout-editor.test.mjs, which loads this file via vm and
  // reaches these through the factory Alpine.data() was called with - same
  // reasoning as any other pure-function unit test, just without a module
  // system to import them through.
  layoutEditor.catalog = catalog;
  layoutEditor.filterCatalog = filterCatalog;
  layoutEditor.toGridNode = toGridNode;
  layoutEditor.fromGridNode = fromGridNode;
  layoutEditor.widgetHTML = widgetHTML;
  layoutEditor.chromeHTML = chromeHTML;
  layoutEditor.optionsSheetHTML = optionsSheetHTML;
  layoutEditor.refMissing = refMissing;
  layoutEditor.columnsFor = columnsFor;
  layoutEditor.tracksFor = tracksFor;
  layoutEditor.heightUnits = heightUnits;
  layoutEditor.resizeTo = resizeTo;
  layoutEditor.insertionPoint = insertionPoint;
  layoutEditor.heightRem = heightRem;
  layoutEditor.cardType = cardType;
  layoutEditor.cardKey = cardKey;
  layoutEditor.energyOptionsHTML = energyOptionsHTML;
  layoutEditor.ENERGY_OPTIONS = ENERGY_OPTIONS;
  layoutEditor.ENERGY_OPTION_BY_ROLE = ENERGY_OPTION_BY_ROLE;
  layoutEditor.defaultEnergyOptions = defaultEnergyOptions;
  layoutEditor.allEntityOptions = allEntityOptions;
  layoutEditor.initEntityChoicesFor = initEntityChoicesFor;
  layoutEditor.destroyEntityChoicesFor = destroyEntityChoicesFor;
  layoutEditor.applyTargetWidth = applyTargetWidth;
  layoutEditor.widthNote = widthNote;
  layoutEditor.THUMB = THUMB;

  const register = () => {
    if (window.Alpine) window.Alpine.data("layoutEditor", layoutEditor);
  };
  if (window.Alpine) register(); else document.addEventListener("alpine:init", register, {once: true});
})();

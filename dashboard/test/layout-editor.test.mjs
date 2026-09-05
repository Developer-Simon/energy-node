// Regression tests for layout-editor.js's Gridstack integration: the pure
// item<->GridStackWidget conversion helpers (toGridNode/fromGridNode),
// widgetHTML()'s escaping and per-type controls, and the imperative
// Gridstack-driving methods (renderGroup/addItem/handleWidgetChange/
// handleWidgetClick/save/confirmUnsavedUnload). Gridstack itself is never
// instantiated here (jsdom has no real drag/resize), same approach as
// devicemap.page.test.mjs's fakeCytoscapeFactory: a minimal fake GridStack
// class captures what the component asks it to do.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { attachStores } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'layout-editor.js'),
  'utf8',
);

// A minimal stand-in for a GridStack instance: enough surface for
// renderGroup()/addItem()/handleWidgetChange()/handleWidgetClick() to drive
// it and for tests to inspect what was loaded/added/removed/destroyed.
// `nodes` mirrors GridStack's internal engine node list; `save()` strips
// `content` the same way real GridStack does when called with `false`.
function fakeGridStackClass(initCalls) {
  return class FakeGridStack {
    static init(options, el) {
      const instance = {
        options, el, nodes: [], listeners: [], destroyed: false,
        // columnCount, nicht column: `column` ist bei Gridstack eine *Methode*
        // (grid.column(n, 'list')), die Zahl liest man ueber getColumn().
        columnCount: options.column,
        compactCalls: [],
        load(items) { this.nodes = items.map(item => ({...item})); },
        addWidget(item) {
          const node = {...item};
          this.nodes.push(node);
          this.listeners.forEach(cb => cb());
          return {gridstackNode: node};
        },
        removeWidget(target) {
          const node = target?.gridstackNode;
          this.nodes = this.nodes.filter(n => n !== node);
          this.listeners.forEach(cb => cb());
        },
        save(includeContent) {
          return this.nodes.map(({content, ...rest}) => (includeContent ? {...rest, content} : {...rest}));
        },
        on(_events, cb) { this.listeners.push(cb); },
        destroy() { this.destroyed = true; },
        column(count) { this.columnCount = count; },
        getColumn() { return this.columnCount; },
        // mount() ruft makeWidget() auf schon vorhandenen .grid-stack-item-DOM-
        // Knoten (server-gerenderte Karten). load() dagegen laesst GridStack die
        // DOM bauen - dann haben die Nodes kein .el.
        makeWidget(elem) {
          const node = {
            el: elem,
            w: Number(elem?.getAttribute?.('gs-w')) || 1,
            h: Number(elem?.getAttribute?.('gs-h')) || 1,
            x: 0, y: this.nodes.length,
          };
          if (elem) elem.gridstackNode = node;
          this.nodes.push(node);
          return elem;
        },
        getGridItems() {
          return this.nodes.map(node => (node.el ? node.el : {gridstackNode: node}));
        },
        update(target, changes) {
          const node = target?.gridstackNode || target;
          Object.assign(node, changes);
          return this;
        },
        compact(layout) { this.compactCalls.push(layout); },
      };
      initCalls.push(instance);
      return instance;
    }
  };
}

function loadLayoutPage({ fetchImpl, gridstack = true } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  const initCalls = [];
  if (gridstack) dom.window.GridStack = fakeGridStackClass(initCalls);
  vm.runInContext(scriptSource, context);
  const component = factory();
  component.$nextTick = fn => fn();
  const stores = attachStores(component);
  return { component, window: dom.window, document: dom.window.document, initCalls, factory, stores };
}

// Wie loadLayoutPage(), aber fuer den Bearbeitungsmodus auf dem echten
// Uebersichts-DOM: bodyHTML (das vorhandene .layout-grid samt Karten) kommt in
// den JSDOM-Body, init() bindet die document-Listener fuer
// layout-editor:mount/-unmount, und dom wird mitgegeben, damit der Test seine
// Ereignisse an genau dieses document schickt. global.document wird auf
// dasselbe document gesetzt, weil die Tests es blank ansprechen.
function createEditor(bodyHTML) {
  const dom = new JSDOM(`<!doctype html><html><body>${bodyHTML}</body></html>`, { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  dom.window.fetch = async () => { throw new Error('fetch should not be called'); };
  const initCalls = [];
  dom.window.GridStack = fakeGridStackClass(initCalls);
  vm.runInContext(scriptSource, context);
  global.document = dom.window.document;
  const editor = factory();
  editor.$nextTick = fn => fn();
  const stores = attachStores(editor);
  editor.init();
  return { dom, editor, window: dom.window, document: dom.window.document, initCalls, factory, stores };
}

// Die statischen Teile des Editor-Fragments (layout-editor.html) als
// Geschwister des Uebersichtsrasters: Werkzeugleisten-Herkunft, Toolbox,
// Options-Modal, Waechter-Modal. Ohne die Revisionen (eigenes Template) und
// ohne #overview-panel-Huelle - layout-editor.js sucht alles per
// document.querySelector, nicht innerhalb eines Panels.
const FRAGMENT_MARKUP = `
<div data-editor-toolbar hidden>
  <div class="layout-widths" role="group" aria-label="Zielbreite" data-edit-only>
    <button class="layout-width-chip active" type="button" data-w="0">Editorbreite</button>
    <button class="layout-width-chip" type="button" data-w="390">Handy</button>
    <button class="layout-width-chip" type="button" data-w="834">Tablet</button>
    <button class="layout-width-chip" type="button" data-w="1280">Monitor</button>
  </div>
  <span class="layout-modehint" data-widthnote data-edit-only></span>
  <button class="layout-btn" type="button" data-edit-only data-pageopts>Seite bearbeiten</button>
  <button class="layout-btn" type="button" data-edit-only data-addpage>Seite</button>
  <button class="layout-btn" type="button" data-edit-only data-toolbox-toggle aria-expanded="false" aria-controls="toolbox">Baustein</button>
  <span class="layout-status" data-status data-edit-only><i></i> Ungespeichert</span>
  <button class="layout-btn danger" type="button" data-edit-only data-mode-discard hidden>Änderungen verwerfen</button>
  <button class="layout-btn primary" type="button" data-edit-only data-mode-save>Speichern &amp; schließen</button>
</div>
<aside class="layout-toolbox" id="toolbox" aria-hidden="true" aria-label="Bausteine">
  <div class="layout-toolbox-head"><b>Bausteine</b><span class="layout-spacer"></span>
    <button class="layout-chip-btn" type="button" data-toolbox-close aria-label="Toolbox schließen">x</button></div>
  <div class="layout-toolbox-search"><input type="search" data-tb-search aria-label="Bausteine durchsuchen"></div>
  <div class="layout-toolbox-tabs" role="group" aria-label="Art">
    <button class="layout-toolbox-tab active" type="button" data-tb-tab="karten">Karten</button>
    <button class="layout-toolbox-tab" type="button" data-tb-tab="geraete">Geräte</button>
    <button class="layout-toolbox-tab" type="button" data-tb-tab="entitaeten">Entitäten</button>
  </div>
  <div class="layout-toolbox-list" data-tb-list></div>
  <div class="layout-toolbox-foot">Klicken oder ziehen — landet auf <b data-tb-target>der aktiven Seite</b>.</div>
</aside>
<div class="layout-modal-scrim" id="layout-options-modal" data-modal>
  <div class="layout-modal-sheet" role="dialog" aria-modal="true" aria-label="Kartenoptionen">
    <div class="layout-modal-sheet-head"><div><b data-modal-title>Speicher</b><div class="kind" data-modal-kind>energy_ring</div></div>
      <span class="layout-spacer"></span>
      <button class="layout-chip-btn" type="button" data-close aria-label="Schließen">x</button></div>
    <div class="layout-modal-sheet-body" data-modal-body></div>
    <div class="layout-modal-sheet-foot"><button class="layout-btn" type="button" data-close>Abbrechen</button>
      <button class="layout-btn primary" type="button" data-close>Übernehmen</button></div>
  </div>
</div>
<div class="layout-modal-scrim" id="layout-page-modal" data-modal>
  <div class="layout-modal-sheet" role="dialog" aria-modal="true" aria-label="Seite">
    <div class="layout-modal-sheet-head">
      <div><b>Seite</b><div class="kind">Layout-Seite der Uebersicht</div></div>
      <span class="layout-spacer"></span>
      <button class="layout-chip-btn" type="button" data-page-close aria-label="Schliessen">x</button>
    </div>
    <div class="layout-modal-sheet-body">
      <div class="layout-modal-field"><label for="layout-page-name">Name</label>
        <input id="layout-page-name" type="text" data-page-name autocomplete="off"></div>
      <div class="layout-modal-field"><label>Reihenfolge</label>
        <div class="layout-page-order">
          <button class="layout-btn" type="button" data-page-move="-1" aria-label="Seite nach vorn">Nach vorn</button>
          <button class="layout-btn" type="button" data-page-move="1" aria-label="Seite nach hinten">Nach hinten</button>
        </div></div>
    </div>
    <div class="layout-modal-sheet-foot">
      <button class="layout-btn danger" type="button" data-page-remove>Seite loeschen</button>
      <span class="layout-spacer"></span>
      <button class="layout-btn primary" type="button" data-page-close>Fertig</button>
    </div>
  </div>
</div>
<dialog id="layout-guard-modal" class="app-modal app-modal-danger" aria-labelledby="layout-guard-title">
  <h2 class="app-modal-title" id="layout-guard-title">Ungespeicherte Änderungen</h2>
  <div class="app-modal-actions">
    <button type="button" data-guard-cancel>Weiter bearbeiten</button>
    <button type="button" class="app-modal-danger-button" data-guard-discard>Verwerfen</button>
  </div>
</dialog>`;

// Wie createEditor(), aber mit dem Editor-Fragment neben dem Raster - fuer die
// Tests von mount()s Verdrahtung (Options-Modal, Toolbox, Werkzeugleiste).
// opts.devices fuellt editor.devices (Katalogquelle der Toolbox).
function createEditorWithFragment(gridHTML, opts = {}) {
  const built = createEditor(gridHTML + FRAGMENT_MARKUP);
  if (opts.devices) built.editor.devices = opts.devices;
  return built;
}

function jsonResponse(body) {
  return { ok: true, json: async () => body };
}

// Eine geladene Komponente samt Katalog. Der Katalog kommt in der Anwendung
// aus GET /api/v1/layout mit; hier aus derselben Antwort, damit der Test den
// Weg nimmt, den der Editor auch nimmt - kein Testhaken ins Modul hinein.
const CARD_TYPES = {
  energy_flow:    {min_span: 2, min_width: '32rem', min_height: '25rem', fills_height: true,  default_span: '2'},
  energy_band:    {min_span: 2, min_width: '34rem', min_height: '33rem', fills_height: true,  default_span: '2'},
  energy_schema:  {min_span: 2, min_width: '24rem', min_height: '16rem', fills_height: false, default_span: '4'},
  energy_status:  {min_span: 1, min_width: '14rem', min_height: '18rem', fills_height: false, default_span: '1'},
  energy_summary: {min_span: 1, min_width: '10rem', min_height: '6rem',  fills_height: false, default_span: 'full'},
  device:         {min_span: 1, min_width: '18rem', min_height: '10rem', fills_height: false, default_span: '1'},
  'device:compact': {min_span: 1, min_width: '14rem', min_height: '7rem', fills_height: false, default_span: '1'},
  diagnostics:    {min_span: 1, min_width: '12rem', min_height: '6rem',  fills_height: false, default_span: '1'},
};

const LAYOUT_RESPONSE = {
  version: 3,
  card_types: CARD_TYPES,
  pages: [{
    id: 'p', name: 'Übersicht', order: 0,
    groups: [{id: 'g', name: 'Dashboard', items: [
      {id: 'energy-band', type: 'energy_band', ref: '', span: '2', visible: true},
      {id: 'device:dev1', type: 'device', ref: 'dev1', span: '1', visible: true},
    ]}],
  }],
};

// Gibt dieselben Schluessel zurueck wie loadLayoutPage (component, window,
// document, initCalls, factory, stores), nur eben nach einem durchlaufenen
// load(). factory hier ist die Fabrik *dieser* Instanz - ihre Hilfsfunktionen
// sehen den geladenen Katalog.
async function loadedComponent({record} = {}) {
  const fetchImpl = async (url, options) => {
    if (record) record.push({url, options});
    const payload = url.endsWith('/api/v1/devices')
      ? [{id: 'dev1', name: 'Gerät 1', entities: []}]
      : LAYOUT_RESPONSE;
    return jsonResponse(payload);
  };
  const loaded = loadLayoutPage({fetchImpl});
  // Das Grid-Element muss existieren, bevor renderGrids() es sucht.
  loaded.document.body.innerHTML = '<div id="layout-grid-g"></div>';
  await loaded.component.load();
  return loaded;
}

test('toGridNode() carries type/ref/visible/visibleCategories/flowScale and never returns x/y', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const node = factory.toGridNode({id: 'a', type: 'device', ref: 'dev1', span: '1', visible: true, visibleCategories: ['controls']}, 'Gerät A', false, 4);
  assert.equal('x' in node, false, 'x is not part of the flow-layout node - order is position');
  assert.equal('y' in node, false);
  assert.deepEqual(node.visibleCategories, ['controls']);

  const flow = factory.toGridNode({id: 'b', type: 'energy_flow', span: 'full', visible: true, flowScale: 'speed'}, 'Energiefluss', false, 4);
  assert.equal(flow.flowScale, 'speed');

  const group = factory.toGridNode({id: 'c', type: 'entity_group', span: '1', visible: true, title: 'Sensoren', entityRefs: ['a', 'b']}, 'Sensoren', false, 4);
  assert.equal(group.title, 'Sensoren');
  assert.deepEqual(group.entityRefs, ['a', 'b']);
});

test('fromGridNode() defaults a missing span to the item-type default and height to 0', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const item = factory.fromGridNode({id: 'a', type: 'device', ref: 'dev1', visible: true});
  assert.equal(item.span, '1');
  assert.equal(item.height, 0);
  for (const key of ['x', 'y', 'w', 'h']) {
    assert.ok(!(key in item), `fromGridNode gab ${key} zurueck`);
  }
});

test('refMissing() flags an unknown device/entity ref but leaves other types alone', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const devices = [{id: 'dev1', entities: [{unique_id: 'ent1'}]}];
  assert.equal(factory.refMissing({type: 'device', ref: 'dev1'}, devices), false);
  assert.equal(factory.refMissing({type: 'device', ref: 'gone'}, devices), true);
  assert.equal(factory.refMissing({type: 'energy_flow', ref: ''}, devices), false);
  // entity_value ist seit der Abschaffung von 'entity' die einzige Karte mit
  // einem einzelnen Entitaets-Ref.
  assert.equal(factory.refMissing({type: 'entity_value', ref: 'ent1'}, devices), false);
  assert.equal(factory.refMissing({type: 'entity_value', ref: 'gone'}, devices), true);
  // entity_group ist erst dann "missing", wenn KEIN Ref der Liste trifft -
  // ein einzelner geloeschter Ref neben gueltigen ist kein Fehlerzustand,
  // dieselbe Toleranz wie beim serverseitigen Rendering (entityGroupForItem).
  assert.equal(factory.refMissing({type: 'entity_group', entityRefs: ['ent1', 'gone']}, devices), false);
  assert.equal(factory.refMissing({type: 'entity_group', entityRefs: ['gone']}, devices), true);
  assert.equal(factory.refMissing({type: 'entity_group', entityRefs: []}, devices), true);
});

test('widgetHTML() escapes the label and shows the missing-ref badge only when asked', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const html = factory.widgetHTML({type: 'device', visible: true}, '<script>alert(1)</script>', true);
  assert.ok(!html.includes('<script>alert(1)</script>'), 'label must be HTML-escaped');
  assert.ok(html.includes('&lt;script&gt;'));
  assert.ok(html.includes('layout-item-missing-ref'));
  const withoutBadge = factory.widgetHTML({type: 'device', visible: true}, 'Gerät A', false);
  assert.ok(!withoutBadge.includes('layout-item-missing-ref'));
});

test('widgetHTML() renders category checkboxes for device items and a flow-scale select for energy_flow items, not both', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const device = factory.widgetHTML({type: 'device', visible: true, visibleCategories: ['controls']}, 'Gerät A', false);
  assert.ok(device.includes('data-role="category"'));
  assert.ok(device.includes('value="controls" checked'));
  assert.ok(!device.includes('data-role="flow-scale"'));

  const flow = factory.widgetHTML({type: 'energy_flow', visible: true, flowScale: 'speed'}, 'Energiefluss', false);
  assert.ok(flow.includes('data-role="flow-scale"'));
  assert.ok(flow.includes('value="speed" selected'));
  assert.ok(!flow.includes('data-role="category"'));
});

test('widgetHTML() renders a title field and a multi-select option per known entity for entity_group items', async () => {
  const devices = [{id: 'dev1', name: 'Gerät 1', entities: [
    {unique_id: 'ent1', name: 'Werkstatt', object_id: 'temp_werkstatt'},
    {unique_id: 'ent2', name: 'Lager', object_id: 'temp_lager'},
  ]}];
  const { component, factory } = loadLayoutPage({
    fetchImpl: async url => (url.endsWith('/api/v1/devices') ? jsonResponse(devices) : jsonResponse(LAYOUT_RESPONSE)),
  });
  await component.load(); // populates the module-level allDevices cache widgetHTML() reads from

  const html = factory.widgetHTML({type: 'entity_group', visible: true, title: 'Sensoren', entityRefs: ['ent2']}, 'Sensoren', false, 4);
  assert.ok(html.includes('data-role="entity-group-title"'));
  assert.ok(html.includes('value="Sensoren"'));
  assert.ok(html.includes('<select multiple data-role="entity-refs"'));
  assert.ok(html.includes('Gerät 1 / Werkstatt'));
  assert.ok(html.includes('value="ent2" selected'), 'a ref already in entityRefs must render selected');
  assert.ok(!html.includes('value="ent1" selected'), 'a ref not in entityRefs must render unselected');
  assert.ok(!html.includes('data-role="category"'), 'entity_group must not render the device-tile category fieldset');
});

test('widgetHTML() renders a single entity select for entity_value items, with the current ref selected', async () => {
  const devices = [{id: 'dev1', name: 'Gerät 1', entities: [
    {unique_id: 'ent1', name: 'Werkstatt', object_id: 'temp_werkstatt'},
    {unique_id: 'ent2', name: 'Lager', object_id: 'temp_lager'},
  ]}];
  const { component, factory } = loadLayoutPage({
    fetchImpl: async url => (url.endsWith('/api/v1/devices') ? jsonResponse(devices) : jsonResponse(LAYOUT_RESPONSE)),
  });
  await component.load();

  const html = factory.widgetHTML({type: 'entity_value', visible: true, ref: 'ent2'}, 'Wert-Karte', false, 4);
  assert.ok(html.includes('data-role="entity-value-ref"'));
  assert.ok(html.includes('Gerät 1 / Werkstatt'));
  assert.ok(html.includes('Gerät 1 / Lager'));
  assert.ok(html.includes('value="ent2" selected'));
  assert.ok(!html.includes('value="ent1" selected'));

  const empty = factory.widgetHTML({type: 'entity_value', visible: true, ref: ''}, 'Wert-Karte', true, 4);
  assert.ok(empty.includes('<option value="">– wählen –</option>'));
});

test('widgetHTML()/energyOptionsHTML() only show the speed-reference fields for energy_flow items in "speed" scale mode', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const width = factory.widgetHTML({type: 'energy_flow', visible: true, flowScale: 'width'}, 'Energiefluss', false);
  assert.ok(!width.includes('data-role="speed-reference-mode"'), 'width mode must not show the reference fields');
  assert.ok(!width.includes('data-role="speed-reference-watts"'));

  const speed = factory.widgetHTML({type: 'energy_flow', visible: true, flowScale: 'speed', speedReferenceMode: 'fixed', speedReferenceWatts: 2500}, 'Energiefluss', false);
  assert.ok(speed.includes('data-role="speed-reference-mode"'));
  assert.ok(speed.includes('value="fixed" selected'));
  assert.ok(speed.includes('data-role="speed-reference-watts"'));
  assert.ok(speed.includes('value="2500"'));
});

// Konfigurationsoptionen der sechs Energiegrafiken-Alternativen (Port der
// sechs-Varianten-Exploration, siehe
// knowhow/dashboard/energiegrafiken-konfiguration-backlog.md). Diese Tests
// decken die generische Verdrahtung (energyOptionsHTML()-Dispatch,
// defaultEnergyOptions(), handleWidgetChange()-Rollen, save()-Serialisierung)
// ab, nicht jedes der 18 Felder einzeln - die Kartenlogik selbst hat eigene
// Tests in energy-band/-ring/-board/-day/-schema/-status.page.test.mjs.
test('energyOptionsHTML() renders the right panel per energy card type and nothing for other types', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const band = factory.energyOptionsHTML({type: 'energy_band', heightReference: 'abs', animate: 'off'});
  assert.ok(band.includes('data-role="height-reference"'));
  assert.ok(band.includes('value="abs" selected'));
  assert.ok(band.includes('data-role="animate"'));
  assert.ok(!band.includes('checked'), 'animate off must not render the checkbox as checked');
  assert.ok(!band.includes('data-role="kpi"'), 'a band panel must not leak ring controls');

  const ring = factory.energyOptionsHTML({type: 'energy_ring', kpi: 'netz'});
  assert.ok(ring.includes('data-role="kpi"'));
  assert.ok(ring.includes('value="netz" selected'));
  assert.ok(!ring.includes('data-role="height-reference"'));

  assert.equal(factory.energyOptionsHTML({type: 'device'}), '');

  // energy_flow zeigt den hide-inactive-Schalter immer, die Speed-Referenz nur
  // im Modus "Animationsgeschwindigkeit".
  const flowWidth = factory.energyOptionsHTML({type: 'energy_flow', flowScale: 'width'});
  assert.ok(flowWidth.includes('data-role="hide-inactive"'));
  assert.ok(!flowWidth.includes('data-role="speed-reference-mode"'), 'width mode hides the speed-reference fields');

  const flow = factory.energyOptionsHTML({type: 'energy_flow', flowScale: 'speed', speedReferenceMode: 'fixed'});
  assert.ok(flow.includes('data-role="hide-inactive"'));
  assert.ok(flow.includes('data-role="speed-reference-mode"'));
  assert.ok(flow.includes('data-role="speed-reference-watts"'));
});

test('energyOptionsHTML() returns energy options as modal fields', async () => {
  const { factory } = await loadedComponent(); // populates the module-level card_types catalog
  const wide = factory.energyOptionsHTML({type: 'energy_band', heightReference: 'fill'});
  const narrow = factory.energyOptionsHTML({type: 'energy_status', beamSpan: '6000'});
  assert.match(wide, /class="layout-modal-field"/);
  assert.match(narrow, /class="layout-modal-field"/);
});

test('defaultEnergyOptions() returns the documented per-type defaults, and {} for non-energy types', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  assert.deepEqual(JSON.parse(JSON.stringify(factory.defaultEnergyOptions('energy_flow'))),
    {speedReferenceMode: 'relative', speedReferenceWatts: 1000, hideInactive: 'off'});
  assert.deepEqual(JSON.parse(JSON.stringify(factory.defaultEnergyOptions('energy_band'))),
    {heightReference: 'fill', scaleMode: 'linear', unit: 'auto', bundleThreshold: '0', animate: 'on', measuredSplit: 'sum'});
  assert.deepEqual(JSON.parse(JSON.stringify(factory.defaultEnergyOptions('energy_status'))),
    {beamSpan: '6000', showAdvice: 'on'});
  assert.deepEqual(JSON.parse(JSON.stringify(factory.defaultEnergyOptions('device'))), {});
});

test('ENERGY_OPTION_BY_ROLE maps a select role to its camelCase field and a checkbox role to on/off', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  assert.deepEqual(
    JSON.parse(JSON.stringify(factory.ENERGY_OPTION_BY_ROLE['scale-mode'])),
    {role: 'scale-mode', field: 'scaleMode', json: 'scale_mode', kind: 'select'},
  );
  assert.deepEqual(
    JSON.parse(JSON.stringify(factory.ENERGY_OPTION_BY_ROLE['show-inactive'])),
    {role: 'show-inactive', field: 'showInactive', json: 'show_inactive', kind: 'checkbox'},
  );
  assert.deepEqual(
    JSON.parse(JSON.stringify(factory.ENERGY_OPTION_BY_ROLE['speed-reference-watts'])),
    {role: 'speed-reference-watts', field: 'speedReferenceWatts', json: 'speed_reference_watts', kind: 'number'},
  );
});

test('toGridNode()/fromGridNode() round-trip an energy card option through the GridStack node', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const node = factory.toGridNode({id: 'a', type: 'energy_board', span: '2', visible: true, sort: 'power', sparkWindow: '60'}, 'Datentafel', false, 4);
  assert.equal(node.sort, 'power');
  assert.equal(node.sparkWindow, '60');
  const item = factory.fromGridNode(node);
  assert.equal(item.sort, 'power');
  assert.equal(item.sparkWindow, '60');
});

test('toGridNode()/fromGridNode() round-trip an entity_group title and entityRefs', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const node = factory.toGridNode({id: 'a', type: 'entity_group', span: '1', visible: true, title: 'Sensoren', entityRefs: ['ent1', 'ent2']}, 'Sensoren', false, 4);
  const item = factory.fromGridNode(node);
  assert.equal(item.title, 'Sensoren');
  assert.deepEqual(item.entityRefs, ['ent1', 'ent2']);

  // Andere Typen tragen keine leeren entity_group-Felder mit sich - dieselbe
  // Erwartung wie visibleCategories/flowScale bei fromGridNode() ohne
  // gesetzte Felder.
  const other = factory.fromGridNode({id: 'b', type: 'device', ref: 'dev1', visible: true});
  assert.equal(other.title, '');
  // other.entityRefs is a fresh [] built inside the vm-executed fromGridNode,
  // so it is a foreign Array from this realm's point of view - round-trip
  // through JSON first, same workaround the file already uses for initCalls
  // options crossing the jsdom vm realm.
  assert.deepEqual(JSON.parse(JSON.stringify(other.entityRefs)), []);
});

test('load() fetches layout+devices, hydrates pages, and mounts a GridStack instance per group', async () => {
  const layout = {
    pages: [{id: 'page-1', name: 'Übersicht', order: 0, groups: [{
      id: 'group-1', name: 'Dashboard',
      items: [{id: 'energy-flow', type: 'energy_flow', ref: '', span: 'full', visible: true, flow_scale: 'width'}],
    }]}],
  };
  const devices = [{id: 'dev1', name: 'Node', entities: []}];
  const { component, document, initCalls } = loadLayoutPage({
    fetchImpl: async url => (url === '/api/v1/layout' ? jsonResponse(layout) : jsonResponse(devices)),
  });
  const container = document.createElement('div');
  container.id = 'layout-grid-group-1';
  document.body.appendChild(container);

  await component.load();

  assert.equal(component.devices.length, 1);
  assert.equal(component.pages[0].groups[0].items[0].flowScale, 'width');
  assert.equal(initCalls.length, 1, 'one GridStack instance per group');
  // initCalls[0].options crosses the jsdom vm realm, so its plain-object
  // prototype differs from this realm's Object - round-trip through JSON
  // before deepEqual, same workaround devicemap.page.test.mjs uses.
  // jsdom gibt clientWidth 0 zurueck (kein echtes Layout) - columnsFor(0) ist 1.
  assert.deepEqual(JSON.parse(JSON.stringify(initCalls[0].options)), {column: 1, cellHeight: '7rem', float: false, disableResize: true, acceptWidgets: true, handle: '.layout-item-handle'});
  assert.equal(initCalls[0].nodes.length, 1);
  assert.equal(initCalls[0].nodes[0].id, 'energy-flow');
  assert.ok(initCalls[0].compactCalls.includes('list'), 'renderGroup packs as a list after loading');
});

test('itemOptions lists a single generic entity_value option (no per-entity ref), and itemLabel falls back to entity_group\'s own title / entity_value\'s picked ref', async () => {
  const devices = [{id: 'dev1', name: 'Gerät 1', entities: [{unique_id: 'ent1', name: 'Leistung', object_id: 'power'}]}];
  const { component } = loadLayoutPage({
    fetchImpl: async url => (url.endsWith('/api/v1/devices') ? jsonResponse(devices) : jsonResponse(LAYOUT_RESPONSE)),
  });
  await component.load();

  // Seit der Abschaffung des Kartentyps 'entity' gibt es ueberhaupt keine
  // Option je Entitaet mehr - weder als entity- noch als entity_value-Karte.
  assert.equal(component.itemOptions.some(option => option.type === 'entity'), false, 'entity option must be gone');
  assert.equal(component.itemOptions.some(option => option.id === 'entity:ent1'), false, 'no more per-entity options');
  // Der Kartentyp entity_value ist eine einzelne, ref-lose Option - die
  // konkrete Entitaet waehlt man im Widget selbst (siehe entity-value-ref).
  assert.equal(component.itemOptions.filter(option => option.type === 'entity_value').length, 1);
  const entityValue = component.itemOptions.find(option => option.id === 'entity-value');
  assert.ok(entityValue, 'generic entity-value option missing');
  assert.equal(entityValue.ref, '');
  assert.equal(component.itemOptions.some(option => option.id === 'entity_value:ent1'), false, 'no more per-entity entity_value options');

  // entity_group hat keine option.id in itemOptions (siehe addEntityGroup) -
  // itemLabel() muss trotzdem etwas Sinnvolles liefern: den eigenen Titel.
  assert.equal(component.itemLabel({type: 'entity_group', id: 'entity-group-123', title: 'Sensoren'}), 'Sensoren');
  assert.equal(component.itemLabel({type: 'entity_group', id: 'entity-group-123', title: ''}), 'Entitätenliste');

  // entity_value hat nach dem Anlegen ebenfalls keine passende option.id mehr
  // (frische newID() statt 'entity-value') - der Name kommt aus dem Ref.
  assert.equal(component.itemLabel({type: 'entity_value', id: 'entity-value-123', ref: 'ent1'}), 'Gerät 1 / Leistung');
  assert.equal(component.itemLabel({type: 'entity_value', id: 'entity-value-123', ref: ''}), 'Wert-Karte');
});

function componentWithMountedGroup(items = []) {
  const { component, document, initCalls } = loadLayoutPage();
  const group = {id: 'group-1', name: 'Dashboard', items};
  const page = {id: 'page-1', name: 'Übersicht', order: 0, groups: [group]};
  component.pages = [page];
  component.devices = [];
  const container = document.createElement('div');
  container.id = 'layout-grid-group-1';
  document.body.appendChild(container);
  component.renderGroup(group);
  return { component, page, group, document, grid: initCalls[0] };
}

test('a remove on the mounted grid resyncs group.items from grid.save() and marks unsaved', () => {
  const { component, group, grid } = componentWithMountedGroup([
    {id: 'a', type: 'device', ref: 'dev1', span: '1', visible: true, x: 0, y: 0, w: 1, h: 2, visibleCategories: []},
  ]);
  assert.equal(component.unsaved, false);
  assert.equal(group.items.length, 1);
  const node = grid.nodes[0];
  // Same path GridStack takes when the user drags a widget out or the
  // remove button fires grid.removeWidget(): the registered 'change added
  // removed' listener rebuilds group.items from the grid's own node list.
  grid.removeWidget({gridstackNode: node});
  assert.equal(group.items.length, 0);
  assert.equal(component.unsaved, true);
});

test('addItem() adds an auto-positioned widget to the live grid and refuses a duplicate or a missing grid', () => {
  const { component, group } = componentWithMountedGroup([]);
  component.addItem(group, 'energy-flow');
  assert.equal(group.items.length, 1);
  assert.equal(group.items[0].type, 'energy_flow');
  assert.equal(component.unsaved, true);

  // duplicate id is a no-op
  component.addItem(group, 'energy-flow');
  assert.equal(group.items.length, 1);

  // no live grid for this group -> no-op, no throw
  const orphanGroup = {id: 'no-grid', name: 'Orphan', items: []};
  component.addItem(orphanGroup, 'diagnostics');
  assert.equal(orphanGroup.items.length, 0);
});

// Wie addEntityGroup(): die generische 'entity-value'-Option hat keinen Ref
// und darum keine natuerliche eindeutige Karten-ID - addItem()s Dedup-Check
// darf eine zweite Wert-Karte in derselben Gruppe nicht verhindern.
test('addItem() creates a fresh id for the generic entity-value option on every call, bypassing the dedup check', () => {
  const { component, group } = componentWithMountedGroup([]);
  component.addItem(group, 'entity-value');
  component.addItem(group, 'entity-value');
  assert.equal(group.items.length, 2);
  assert.equal(group.items[0].type, 'entity_value');
  assert.equal(group.items[1].type, 'entity_value');
  assert.notEqual(group.items[0].id, group.items[1].id);
  assert.equal(group.items[0].ref, '');
});

// addEntityGroup() geht nicht ueber addItem()/itemOptions: entity_group hat
// keinen Ref und damit keine natuerliche eindeutige option.id, die addItem()s
// Dedup-Check (ein Item pro option.id je Gruppe) bedienen koennte. Jeder
// Aufruf muss also eine eigene, neue Karte anlegen - auch ein zweiter Aufruf
// direkt hintereinander.
test('addEntityGroup() adds a new entity_group widget with a fresh id on every call', () => {
  const { component, group } = componentWithMountedGroup([]);
  component.addEntityGroup(group);
  component.addEntityGroup(group);
  assert.equal(group.items.length, 2);
  assert.equal(group.items[0].type, 'entity_group');
  assert.equal(group.items[1].type, 'entity_group');
  assert.notEqual(group.items[0].id, group.items[1].id);
  assert.equal(group.items[0].title, 'Entitäten');
  assert.deepEqual(JSON.parse(JSON.stringify(group.items[0].entityRefs)), []);
  assert.equal(component.unsaved, true);
});

test('handleWidgetChange() updates the gridstackNode for visible/category/flow-scale roles and ignores others', () => {
  const { component, group } = componentWithMountedGroup([
    {id: 'a', type: 'device', ref: 'dev1', span: '1', visible: true, x: 0, y: 0, w: 1, h: 1, visibleCategories: []},
  ]);
  const node = {visible: true, visibleCategories: [], flowScale: ''};
  const itemEl = {gridstackNode: node};
  const grid = {
    save: () => [{...node, id: 'a', type: 'device', ref: 'dev1', x: 0, y: 0, w: 1, h: 1}],
    getColumn: () => 4,
    update(target, changes) { Object.assign(target.gridstackNode, changes); },
  };
  const fakeTarget = role => ({dataset: {role}, closest: () => itemEl, checked: true, value: 'measurements'});

  component.handleWidgetChange({target: fakeTarget('visible')}, grid, group);
  assert.equal(node.visible, true);
  assert.equal(component.unsaved, true);

  component.handleWidgetChange({target: fakeTarget('category')}, grid, group);
  assert.deepEqual(JSON.parse(JSON.stringify(node.visibleCategories)), ['measurements']);

  component.handleWidgetChange({target: fakeTarget('flow-scale')}, grid, group);
  // flow-scale role reads .value ('measurements' from the shared fake), just checking it's applied
  assert.equal(node.flowScale, 'measurements');

  component.unsaved = false;
  component.handleWidgetChange({target: {dataset: {}, closest: () => itemEl}}, grid, group);
  assert.equal(component.unsaved, false, 'unrelated change events are ignored');
});

test('handleWidgetChange() re-renders the widget content on a flow-scale change, so the speed-reference fields show/hide immediately', () => {
  const { component, group } = componentWithMountedGroup([
    {id: 'b', type: 'energy_flow', ref: '', span: 'full', visible: true, x: 0, y: 0, w: 4, h: 4, flowScale: 'width'},
  ]);
  const node = {id: 'b', type: 'energy_flow', ref: '', span: 'full', visible: true, flowScale: 'width'};
  const itemEl = {gridstackNode: node};
  const grid = {
    save: () => [{...node, x: 0, y: 0, w: 4, h: 4}],
    getColumn: () => 4,
    update(target, changes) { Object.assign(target.gridstackNode, changes); },
  };
  const target = {dataset: {role: 'flow-scale'}, closest: () => itemEl, value: 'speed'};

  component.handleWidgetChange({target}, grid, group);

  assert.equal(node.flowScale, 'speed');
  assert.ok(node.content.includes('data-role="speed-reference-mode"'), 'switching to speed mode must re-render the reference fields in');
});

test('handleWidgetChange() coerces a number-kind energy option to a JS number', () => {
  const { component, group } = componentWithMountedGroup([
    {id: 'a', type: 'energy_flow', span: 'full', visible: true, x: 0, y: 0, w: 4, h: 4, speedReferenceWatts: 1000},
  ]);
  const node = {visible: true, speedReferenceWatts: 1000};
  const itemEl = {gridstackNode: node};
  const grid = {save: () => [{...node, id: 'a', type: 'energy_flow', x: 0, y: 0, w: 4, h: 4}]};

  component.handleWidgetChange({target: {dataset: {role: 'speed-reference-watts'}, closest: () => itemEl, value: '2500'}}, grid, group);

  assert.equal(node.speedReferenceWatts, 2500);
  assert.equal(typeof node.speedReferenceWatts, 'number');
});

test('handleWidgetChange() applies an energy-option select as its raw value and a checkbox as on/off', () => {
  const { component, group } = componentWithMountedGroup([
    {id: 'a', type: 'energy_band', span: '2', visible: true, x: 0, y: 0, w: 2, h: 1, heightReference: 'fill', animate: 'on'},
  ]);
  const node = {visible: true, heightReference: 'fill', animate: 'on'};
  const itemEl = {gridstackNode: node};
  const grid = {save: () => [{...node, id: 'a', type: 'energy_band', x: 0, y: 0, w: 2, h: 1}]};

  component.handleWidgetChange({target: {dataset: {role: 'height-reference'}, closest: () => itemEl, value: 'abs'}}, grid, group);
  assert.equal(node.heightReference, 'abs');

  component.handleWidgetChange({target: {dataset: {role: 'animate'}, closest: () => itemEl, checked: false}}, grid, group);
  assert.equal(node.animate, 'off');

  component.handleWidgetChange({target: {dataset: {role: 'animate'}, closest: () => itemEl, checked: true}}, grid, group);
  assert.equal(node.animate, 'on');
});

test('handleWidgetChange() updates the gridstackNode for entity-group-title/entity-refs roles', () => {
  const { component, group } = componentWithMountedGroup([
    {id: 'a', type: 'entity_group', span: '1', visible: true, x: 0, y: 0, w: 1, h: 2, title: 'Sensoren', entityRefs: []},
  ]);
  const node = {title: 'Sensoren', entityRefs: []};
  const itemEl = {gridstackNode: node};
  const grid = {
    save: () => [{...node, id: 'a', type: 'entity_group', x: 0, y: 0, w: 1, h: 2}],
    getColumn: () => 4,
    update(target, changes) { Object.assign(target.gridstackNode, changes); },
  };

  component.handleWidgetChange({target: {dataset: {role: 'entity-group-title'}, closest: () => itemEl, value: 'Neuer Titel'}}, grid, group);
  assert.equal(node.title, 'Neuer Titel');
  assert.equal(component.unsaved, true);

  // Choices.js haelt das <select multiple> synchron - handleWidgetChange
  // liest darum ueber selectedOptions statt eines einzelnen checked/value
  // wie beim vorherigen Checkbox-Fieldset.
  const selectedOptions = values => values.map(value => ({value}));
  component.handleWidgetChange({target: {dataset: {role: 'entity-refs'}, closest: () => itemEl, selectedOptions: selectedOptions(['ent1'])}}, grid, group);
  assert.deepEqual(JSON.parse(JSON.stringify(node.entityRefs)), ['ent1']);

  component.handleWidgetChange({target: {dataset: {role: 'entity-refs'}, closest: () => itemEl, selectedOptions: selectedOptions(['ent1', 'ent2'])}}, grid, group);
  assert.deepEqual(JSON.parse(JSON.stringify(node.entityRefs)), ['ent1', 'ent2']);

  component.handleWidgetChange({target: {dataset: {role: 'entity-refs'}, closest: () => itemEl, selectedOptions: selectedOptions(['ent2'])}}, grid, group);
  assert.deepEqual(JSON.parse(JSON.stringify(node.entityRefs)), ['ent2']);
});

test('handleWidgetChange() updates ref and re-renders content for the entity-value-ref role', () => {
  const { component, group } = componentWithMountedGroup([
    {id: 'a', type: 'entity_value', ref: '', span: '1', visible: true, x: 0, y: 0, w: 1, h: 1},
  ]);
  const node = {type: 'entity_value', ref: ''};
  const itemEl = {gridstackNode: node};
  let updated = null;
  const grid = {
    save: () => [{...node, id: 'a', x: 0, y: 0, w: 1, h: 1}],
    getColumn: () => 4,
    update(target, changes) { updated = changes; Object.assign(target.gridstackNode, changes); },
  };

  component.handleWidgetChange({target: {dataset: {role: 'entity-value-ref'}, closest: () => itemEl, value: 'ent1'}}, grid, group);
  assert.equal(node.ref, 'ent1');
  assert.ok(updated?.content, 'must re-render content so the missing-ref badge/label update immediately');
  assert.equal(component.unsaved, true);
});

test('handleWidgetClick() removes the widget only when the remove-item button was clicked', () => {
  const { component } = componentWithMountedGroup([
    {id: 'a', type: 'device', ref: 'dev1', span: '1', visible: true, x: 0, y: 0, w: 1, h: 1, visibleCategories: []},
  ]);
  let removed = null;
  const grid = {removeWidget: el => { removed = el; }};
  const itemEl = {};
  const removeButton = {closest: selector => (selector === '[data-role="remove-item"]' ? removeButton : itemEl)};
  component.handleWidgetClick({target: removeButton}, grid);
  assert.equal(removed, itemEl);

  removed = null;
  const otherButton = {closest: () => null};
  component.handleWidgetClick({target: otherButton}, grid);
  assert.equal(removed, null);
});

test('save() renames visibleCategories/flowScale to snake_case, PUTs the layout, and clears unsaved', async () => {
  let sentBody = null;
  const { component, stores } = loadLayoutPage({
    fetchImpl: async (url, options) => {
      if (options?.method === 'PUT') { sentBody = JSON.parse(options.body); return jsonResponse({}); }
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.unsaved = true;
  component.pages = [{
    id: 'page-1', name: 'Übersicht', order: 0,
    groups: [{id: 'group-1', name: 'Dashboard', items: [
      {id: 'a', type: 'device', ref: 'dev1', span: '1', visible: true, visibleCategories: ['controls'], flowScale: ''},
      {id: 'b', type: 'energy_flow', ref: '', span: 'full', visible: true, visibleCategories: [], flowScale: 'speed'},
    ]}],
  }];

  await component.save();

  assert.equal(component.unsaved, false);
  assert.equal(stores.toasts.last(), 'Layout gespeichert.');
  assert.equal(sentBody.version, 3);
  const items = sentBody.pages[0].groups[0].items;
  assert.deepEqual(items[0].visible_categories, ['controls']);
  assert.equal('visibleCategories' in items[0], false);
  assert.equal(items[1].flow_scale, 'speed');
  assert.equal('flowScale' in items[1], false);
  assert.equal('visible_categories' in items[1], false, 'empty visibleCategories is not serialized');
});

test('save() renames entityRefs to entity_refs, keeps title as-is, and omits an empty entityRefs', async () => {
  let sentBody = null;
  const { component } = loadLayoutPage({
    fetchImpl: async (url, options) => {
      if (options?.method === 'PUT') { sentBody = JSON.parse(options.body); return jsonResponse({}); }
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.pages = [{
    id: 'page-1', name: 'Übersicht', order: 0,
    groups: [{id: 'group-1', name: 'Dashboard', items: [
      {id: 'a', type: 'entity_group', ref: '', span: '1', visible: true, visibleCategories: [], flowScale: '', title: 'Sensoren', entityRefs: ['ent1', 'ent2']},
      {id: 'b', type: 'entity_group', ref: '', span: '1', visible: true, visibleCategories: [], flowScale: '', title: '', entityRefs: []},
    ]}],
  }];

  await component.save();

  const items = sentBody.pages[0].groups[0].items;
  assert.deepEqual(items[0].entity_refs, ['ent1', 'ent2']);
  assert.equal('entityRefs' in items[0], false);
  assert.equal(items[0].title, 'Sensoren');
  assert.equal('entity_refs' in items[1], false, 'an empty entityRefs is not serialized, same as empty visibleCategories');
});

test('save() renames energy-option camelCase fields to snake_case and omits empty ones', async () => {
  let sentBody = null;
  const { component } = loadLayoutPage({
    fetchImpl: async (url, options) => {
      if (options?.method === 'PUT') { sentBody = JSON.parse(options.body); return jsonResponse({}); }
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.pages = [{
    id: 'page-1', name: 'Übersicht', order: 0,
    groups: [{id: 'group-1', name: 'Dashboard', items: [
      {
        id: 'a', type: 'energy_band', ref: '', span: '2', visible: true, visibleCategories: [], flowScale: '',
        heightReference: 'abs', scaleMode: 'sqrt', unit: 'w', bundleThreshold: '0.03', animate: 'off',
        kpi: '', labelMode: '', sort: '', sparkWindow: '', dense: '', showInactive: '',
      },
    ]}],
  }];

  await component.save();

  const item = sentBody.pages[0].groups[0].items[0];
  assert.equal(item.height_reference, 'abs');
  assert.equal(item.scale_mode, 'sqrt');
  assert.equal(item.unit, 'w');
  assert.equal(item.bundle_threshold, '0.03');
  assert.equal(item.animate, 'off');
  for (const leaked of ['heightReference', 'scaleMode', 'bundleThreshold', 'kpi', 'labelMode', 'sort', 'sparkWindow', 'dense', 'showInactive']) {
    assert.equal(leaked in item, false, `${leaked} must not leak into the saved JSON`);
  }
});

test('save() serializes speedReferenceWatts as a JSON number and omits it when unset', async () => {
  let sentBody = null;
  const { component } = loadLayoutPage({
    fetchImpl: async (url, options) => {
      if (options?.method === 'PUT') { sentBody = JSON.parse(options.body); return jsonResponse({}); }
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.pages = [{
    id: 'page-1', name: 'Übersicht', order: 0,
    groups: [{id: 'group-1', name: 'Dashboard', items: [
      {id: 'a', type: 'energy_flow', ref: '', span: 'full', visible: true, visibleCategories: [], flowScale: 'speed', speedReferenceMode: 'fixed', speedReferenceWatts: 2500},
      {id: 'b', type: 'energy_flow', ref: '', span: 'full', visible: true, visibleCategories: [], flowScale: 'width', speedReferenceMode: '', speedReferenceWatts: ''},
    ]}],
  }];

  await component.save();

  const items = sentBody.pages[0].groups[0].items;
  assert.equal(items[0].speed_reference_mode, 'fixed');
  assert.equal(items[0].speed_reference_watts, 2500);
  assert.equal(typeof items[0].speed_reference_watts, 'number');
  assert.equal('speed_reference_mode' in items[1], false, 'unset mode is not serialized');
  assert.equal('speed_reference_watts' in items[1], false, 'unset watts is not serialized');
});

test('load() maps snake_case energy-option fields from the server back to camelCase item state', async () => {
  const layout = {
    version: 3,
    pages: [{id: 'page-1', name: 'Übersicht', order: 0, groups: [{
      id: 'group-1', name: 'Dashboard',
      items: [{id: 'a', type: 'energy_ring', ref: '', span: '2', visible: true, kpi: 'eigen', label_mode: 'pct', animate: 'off'}],
    }]}],
  };
  const { component } = loadLayoutPage({
    fetchImpl: async url => (url === '/api/v1/layout' ? jsonResponse(layout) : jsonResponse([])),
  });
  await component.load(); // no #layout-grid-group-1 container exists, so renderGrids() no-ops harmlessly
  const item = component.pages[0].groups[0].items[0];
  assert.equal(item.kpi, 'eigen');
  assert.equal(item.labelMode, 'pct');
  assert.equal(item.animate, 'off');
});

test('load() maps the server-sent speed_reference_watts number back into speedReferenceWatts', async () => {
  const layout = {
    version: 3,
    pages: [{id: 'page-1', name: 'Übersicht', order: 0, groups: [{
      id: 'group-1', name: 'Dashboard',
      items: [{id: 'energy-flow', type: 'energy_flow', ref: '', span: 'full', visible: true, flow_scale: 'speed', speed_reference_mode: 'fixed', speed_reference_watts: 2500}],
    }]}],
  };
  const { component } = loadLayoutPage({
    fetchImpl: async url => (url === '/api/v1/layout' ? jsonResponse(layout) : jsonResponse([])),
  });
  await component.load();
  const item = component.pages[0].groups[0].items[0];
  assert.equal(item.speedReferenceMode, 'fixed');
  assert.equal(item.speedReferenceWatts, 2500);
});

test('load() maps entity_refs/title from the server back into entityRefs/title, defaulting a missing entity_refs to []', async () => {
  const layout = {
    version: 3,
    pages: [{id: 'page-1', name: 'Übersicht', order: 0, groups: [{
      id: 'group-1', name: 'Dashboard',
      items: [
        {id: 'a', type: 'entity_group', ref: '', span: '1', visible: true, title: 'Sensoren', entity_refs: ['ent1', 'ent2']},
        {id: 'b', type: 'device', ref: 'dev1', span: '1', visible: true},
      ],
    }]}],
  };
  const { component } = loadLayoutPage({
    fetchImpl: async url => (url === '/api/v1/layout' ? jsonResponse(layout) : jsonResponse([])),
  });
  await component.load();
  const [group, device] = component.pages[0].groups[0].items;
  assert.equal(group.title, 'Sensoren');
  assert.deepEqual(JSON.parse(JSON.stringify(group.entityRefs)), ['ent1', 'ent2']);
  assert.equal(device.title, '');
  assert.deepEqual(JSON.parse(JSON.stringify(device.entityRefs)), []);
});

test('confirmUnsavedUnload() only blocks the tab close when there are unsaved changes', () => {
  const { component } = loadLayoutPage({ gridstack: false });
  let prevented = false;
  const event = {preventDefault: () => { prevented = true; }, returnValue: undefined};

  component.unsaved = false;
  component.confirmUnsavedUnload(event);
  assert.equal(prevented, false);

  component.unsaved = true;
  component.confirmUnsavedUnload(event);
  assert.equal(prevented, true);
  assert.equal(event.returnValue, '');
});

test('removeGroup() destroys the live GridStack instance for that group', () => {
  const { component, page, grid } = componentWithMountedGroup([]);
  assert.equal(grid.destroyed, false);
  component.removeGroup(page, 0);
  assert.equal(grid.destroyed, true);
  assert.equal(page.groups.length, 0);
});

test('removePage() destroys the live GridStack instance for every group on that page', () => {
  const { component, grid } = componentWithMountedGroup([]);
  component.pages.push({id: 'page-2', name: 'Zweite Seite', order: 1, groups: []});
  assert.equal(grid.destroyed, false);
  component.removePage(0);
  assert.equal(grid.destroyed, true);
  assert.equal(component.pages.length, 1);
  assert.equal(component.pages[0].id, 'page-2');
  assert.equal(component.pages[0].order, 0, 'remaining pages are renumbered');
});

test('columnsFor spiegelt repeat(auto-fill, minmax(18rem, 1fr)) mit gap .8rem', () => {
  const {factory} = loadLayoutPage({gridstack: false});
  assert.equal(factory.columnsFor(1180), 3);
  assert.equal(factory.columnsFor(1872), 6);
  assert.equal(factory.columnsFor(200), 1);
});

test('columnsFor kippt genau am auto-fill-Umschaltpunkt', () => {
  const {factory} = loadLayoutPage({gridstack: false});
  // Die vierte Spur passt ab 4 * 288 + 3 * 12,8 = 1190,4 px.
  assert.equal(factory.columnsFor(1190), 3);
  assert.equal(factory.columnsFor(1191), 4);
});

test('tracksFor deckelt jede Klasse auf die vorhandene Spaltenzahl', () => {
  const {factory} = loadLayoutPage({gridstack: false});
  assert.equal(factory.tracksFor('2', 6), 2);
  assert.equal(factory.tracksFor('6', 3), 3);
  assert.equal(factory.tracksFor('full', 4), 4);
});

test('heightUnits rundet die Katalog-Mindesthoehe auf 7-rem-Einheiten auf', () => {
  const {factory} = loadLayoutPage({gridstack: false});
  assert.equal(factory.heightUnits('25rem'), 4);
  assert.equal(factory.heightUnits('6rem'), 1);
  assert.equal(factory.heightUnits('14rem'), 2);
});

test('toGridNode setzt h aus height, ersatzweise aus der Katalog-Mindesthoehe', async () => {
  const {factory} = await loadedComponent();
  // device: min_height 10rem -> 2 Einheiten, wenn keine Zwangshoehe gesetzt ist.
  assert.equal(factory.toGridNode({id: 'a', type: 'device', span: '1'}, 'A', false, 4).h, 2);
  assert.equal(factory.toGridNode({id: 'a', type: 'device', span: '1', height: 5}, 'A', false, 4).h, 5);
  // span 6 auf vierspurigem Editor-Raster: w ist die Spaltenzahl.
  assert.equal(factory.toGridNode({id: 'b', type: 'device', span: '6'}, 'B', false, 4).w, 4);
});

test('der Editor startet mehrspaltig, packt als Liste und laesst nicht ziehen', async () => {
  const {initCalls} = await loadedComponent();
  const grid = initCalls[0];
  assert.equal(grid.options.disableResize, true);
  assert.equal(typeof grid.options.column, 'number');
  assert.ok(grid.compactCalls.includes('list'));
});

test('die Reihenfolge nach einem Zug entspricht der Listenpackung', async () => {
  const {component, initCalls} = await loadedComponent();
  const grid = initCalls[0];
  // Zug simulieren: das zweite Item wandert vor das erste.
  const [first, second] = grid.nodes;
  grid.nodes = [{...second, x: 0, y: 0}, {...first, x: 0, y: 1}];
  grid.listeners.forEach(cb => cb());
  const ids = component.pages[0].groups[0].items.map(item => item.id);
  assert.deepEqual(ids, [second.id, first.id]);
});

test('der PUT-Rumpf schickt version 3 und keine Koordinaten', async () => {
  const requests = [];
  const {component} = await loadedComponent({record: requests});
  await component.save();
  const put = requests.find(request => request.options?.method === 'PUT');
  const body = JSON.parse(put.options.body);
  assert.equal(body.version, 3);
  const item = body.pages[0].groups[0].items[0];
  for (const key of ['x', 'y', 'w', 'h']) {
    assert.ok(!(key in item), `PUT-Rumpf enthaelt ${key}`);
  }
  assert.equal(item.span, '2');
});

// Kleiner Helfer: widgetHTML() in ein Element haengen und abfragbar machen.
const renderWidget = (loaded, item, label, columns) => {
  const host = loaded.document.createElement('div');
  host.innerHTML = loaded.factory.widgetHTML(item, label, false, columns);
  return host;
};

test('Optionen unter dem Typ-Minimum sind gesperrt und nennen die Mindestbreite', async () => {
  const loaded = await loadedComponent();
  const host = renderWidget(loaded, {id: 'energy-band', type: 'energy_band', span: '2', visible: true}, 'Energie: Bilanzband', 6);
  const options = [...host.querySelectorAll('[data-role="span"] option')];
  const byValue = value => options.find(option => option.value === value);
  assert.equal(options.length, 7);
  assert.equal(byValue('1').disabled, true);
  assert.equal(byValue('2').disabled, false);
  assert.match(byValue('1').title, /34rem/);
  assert.match(byValue('1').title, /Spannweite 2/);
});

test('Klassen oberhalb der Editor-Spaltenzahl bleiben waehlbar, tragen aber den Hinweis', async () => {
  const loaded = await loadedComponent();
  const host = renderWidget(loaded, {id: 'device:dev1', type: 'device', span: '1', visible: true}, 'Gerät 1', 3);
  const options = [...host.querySelectorAll('[data-role="span"] option')];
  for (const value of ['4', '5', '6']) {
    const option = options.find(candidate => candidate.value === value);
    assert.equal(option.disabled, false, `Option ${value} ist gesperrt`);
    assert.match(option.title, /voll/);
  }
});

test('Typen ohne fills_height tragen den Leerraum-Hinweis am Hoehenfeld', async () => {
  const loaded = await loadedComponent();
  const withFill = renderWidget(loaded, {id: 'f', type: 'energy_flow', span: '2'}, 'Energiefluss', 6);
  const withoutFill = renderWidget(loaded, {id: 'd', type: 'device', span: '1'}, 'Gerät 1', 6);
  // Look in the first Platz fieldgroup for the height hint
  const getPlatzHint = (host) => {
    const platzFieldgroup = host.querySelector('.layout-modal-fieldgroup');
    return platzFieldgroup?.querySelector('.layout-modal-hint');
  };
  assert.equal(getPlatzHint(withFill), null);
  assert.match(getPlatzHint(withoutFill)?.textContent, /Leerraum/);
});

test('das Auswahlfeld setzt die Groessenklasse und packt neu', async () => {
  const loaded = await loadedComponent();
  const {component, initCalls, document} = loaded;
  const grid = initCalls[0];
  const node = grid.nodes.find(candidate => candidate.type === 'device');
  const el = document.createElement('div');
  el.className = 'grid-stack-item';
  el.gridstackNode = node;
  const select = document.createElement('select');
  select.dataset.role = 'span';
  select.innerHTML = '<option value="3" selected></option>';
  el.appendChild(select);
  document.body.appendChild(el);

  grid.compactCalls.length = 0;
  component.handleWidgetChange({target: select}, grid, component.pages[0].groups[0]);
  assert.equal(node.span, '3');
  assert.equal(node.w, Math.min(3, grid.getColumn()));
  assert.ok(grid.compactCalls.includes('list'));
});

test('das Zahlenfeld setzt die Zwangshoehe, leer heisst keine', async () => {
  const loaded = await loadedComponent();
  const {component, initCalls, document} = loaded;
  const grid = initCalls[0];
  const node = grid.nodes.find(candidate => candidate.type === 'device');
  const el = document.createElement('div');
  el.className = 'grid-stack-item';
  el.gridstackNode = node;
  const input = document.createElement('input');
  input.dataset.role = 'height';
  input.value = '4';
  el.appendChild(input);
  document.body.appendChild(el);

  component.handleWidgetChange({target: input}, grid, component.pages[0].groups[0]);
  assert.equal(node.height, 4);
  assert.equal(node.h, 4);

  input.value = '';
  component.handleWidgetChange({target: input}, grid, component.pages[0].groups[0]);
  assert.equal(node.height, 0);
  // Ohne Zwangshoehe faellt die Vorschauhoehe auf die Katalog-Mindesthoehe
  // zurueck: device 10rem -> 2 Einheiten.
  assert.equal(node.h, 2);
});

test('der Layout-Editor bietet measured_split nur bei Band, Ring und Board an', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const withOption = ['energy_band', 'energy_ring', 'energy_board'];
  const withoutOption = ['energy_day', 'energy_schema', 'energy_flow', 'energy_status'];
  for (const type of withOption) {
    const html = factory.energyOptionsHTML({type, measuredSplit: 'sum'});
    assert.match(html, /data-role="measured-split"/, type);
  }
  for (const type of withoutOption) {
    const html = factory.energyOptionsHTML({type, measuredSplit: ''});
    assert.equal(/data-role="measured-split"/.test(html), false, type);
  }
});

test('energyOptionsHTML() zeigt das Batterie-Zeitfenster nur für die Trajektorie', () => {
  const { factory } = loadLayoutPage({ gridstack: false });

  const traj = factory.energyOptionsHTML({type: 'battery_status', display: 'trajectory', batteryWindow: '12', batteryProjectionWindow: '3'});
  assert.ok(traj.includes('data-role="battery-window"'), 'Zeitfenster-Feld fehlt');
  assert.ok(traj.includes('value="12" selected'), 'Zeitfenster steht nicht auf 12');
  assert.ok(traj.includes('data-role="battery-projection-window"'), 'Projektionsfeld fehlt');
  assert.ok(traj.includes('value="3" selected'), 'Projektion steht nicht auf 3');

  const column = factory.energyOptionsHTML({type: 'battery_status', display: 'column'});
  assert.equal(column, '', 'die Säule hat kein Zeitfenster');
});

test('das Batterie-Zeitfenster fährt über ENERGY_OPTIONS zwischen Feld und JSON hin und her', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const byField = Object.fromEntries(factory.ENERGY_OPTIONS.map(o => [o.field, o]));
  assert.equal(byField.batteryWindow.json, 'battery_window');
  assert.equal(byField.batteryWindow.kind, 'select');
  assert.equal(byField.batteryProjectionWindow.json, 'battery_projection_window');
});

test('initEntityChoicesFor()/destroyEntityChoicesFor() create and tear down a Choices instance for the entity-refs select', () => {
  const { factory, window, document } = loadLayoutPage({ gridstack: false });
  const calls = { constructed: [], destroyed: 0 };
  window.Choices = class FakeChoices {
    constructor(select) { calls.constructed.push(select); this.select = select; }
    destroy() { calls.destroyed += 1; }
  };
  const el = document.createElement('div');
  el.innerHTML = '<select multiple data-role="entity-refs"><option value="ent1">Ent 1</option></select>';
  document.body.appendChild(el);

  factory.initEntityChoicesFor('node-1', el);
  assert.equal(calls.constructed.length, 1);
  assert.equal(calls.constructed[0], el.querySelector('[data-role="entity-refs"]'));

  // Re-init for the same id must tear down the previous instance first -
  // applyColumns()/renderGroup() re-render/replace widget content on every
  // column change or reload, and a stale Choices instance left attached to a
  // now-detached <select> would leak both the instance and its own
  // document-level event listeners.
  factory.initEntityChoicesFor('node-1', el);
  assert.equal(calls.destroyed, 1);
  assert.equal(calls.constructed.length, 2);

  factory.destroyEntityChoicesFor('node-1');
  assert.equal(calls.destroyed, 2);

  // destroying an untracked id is a no-op, no throw
  assert.doesNotThrow(() => factory.destroyEntityChoicesFor('unknown'));
  assert.equal(calls.destroyed, 2);
});

test('initEntityChoicesFor() is a no-op without window.Choices or without a matching select', () => {
  const { factory, document } = loadLayoutPage({ gridstack: false });
  const el = document.createElement('div');
  el.innerHTML = '<input data-role="entity-group-title">';
  document.body.appendChild(el);
  // window.Choices is undefined in this test - must not throw.
  assert.doesNotThrow(() => factory.initEntityChoicesFor('node-2', el));
});

test('der Katalog zerfaellt in drei Register statt in ein Dropdown', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const cat = factory.catalog([{id: 'wallbox', name: 'Wallbox', entities: [{unique_id: 'e1', name: 'Leistung'}]}]);
  assert.ok(cat.karten.length >= 8, 'die festen Kartentypen');
  assert.deepEqual(cat.geraete.map(e => e.title), ['Wallbox']);
  // Ein Eintrag je Entitaet: die Wert-Karte. Der zweite ("Entität mit
  // technischen Details", Kartentyp 'entity') ist seit 2026-09 abgeschafft und
  // darf im Register nicht wieder auftauchen.
  assert.deepEqual(cat.entitaeten.map(e => e.type), ['entity_value']);
  assert.deepEqual(cat.entitaeten.map(e => e.title), ['Wallbox / Leistung']);
  assert.deepEqual(cat.entitaeten.map(e => e.desc), ['Wert-Karte']);
  assert.equal([...cat.karten, ...cat.geraete, ...cat.entitaeten].some(e => e.type === 'entity'), false,
    'der abgeschaffte Kartentyp darf in keinem der drei Register stehen');
});

test('die Entitaetenliste steht als Baustein im Kartenregister', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const cat = factory.catalog([]);
  const entry = cat.karten.find(card => card.type === 'entity_group');
  assert.ok(entry, 'ohne Katalogeintrag ist die Entitaetenliste in der Toolbox unerreichbar');
  assert.equal(entry.title, 'Entitätenliste');
});

test('die Energiekarten heissen im Katalog durchgehend "Energie: ..."', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const cat = factory.catalog([]);
  const flow = cat.karten.find(card => card.type === 'energy_flow');
  assert.equal(flow.title, 'Energie: Energiefluss');
});

test('die Suche geht ueber alle Register', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const cat = factory.catalog([{id: 'wallbox', name: 'Wallbox', entities: []}]);
  assert.deepEqual(JSON.parse(JSON.stringify(factory.filterCatalog(cat, 'wallbox').map(e => e.title))), ['Wallbox']);
  assert.ok(factory.filterCatalog(cat, 'ring').some(e => /Ring/.test(e.title)));
});

test('der Editor haengt sich an das vorhandene Uebersichtsraster', () => {
  const { dom, editor } = createEditor('<div class="layout-grid"><div class="layout-grid-item" data-layout-item-id="a"></div></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  const slot = dom.window.document.querySelector('.layout-edit-slot');
  assert.ok(slot, 'jede Karte bekommt eine Editier-Huelle');
  assert.ok(slot.querySelector('.layout-card-chrome'), 'die Huelle traegt das Chrome');
});

test('beim Verlassen bleibt das Raster als reine Ansicht zurueck', () => {
  const { dom } = createEditor('<div class="layout-grid"><div class="layout-grid-item" data-layout-item-id="a"></div></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:unmount'));
  assert.equal(dom.window.document.querySelector('.layout-card-chrome'), null);
  assert.ok(dom.window.document.querySelector('.layout-grid-item'), 'die Karte selbst bleibt stehen');
});

test('jedes Feld der Komponente ist deklariert, damit nichts in den Aussenbereich faellt', () => {
  // Alpine legt `this` als mergeProxies() ueber den ganzen Gueltigkeitsstapel:
  // eine Zuweisung an einen Namen, den KEIN Objekt im Stapel kennt, landet im
  // aeussersten Bereich (dashboardShell() am <body>) und ueberlebt jeden
  // Moduswechsel. So stand _optionsWired beim zweiten "Editieren" schon auf
  // true, die frische Komponente verdrahtete das frische Modal nicht mehr,
  // und das Modal liess sich nicht mehr schliessen.
  const { component } = loadLayoutPage({ gridstack: false });
  const assigned = new Set(
    [...scriptSource.matchAll(/this\.([A-Za-z_][A-Za-z0-9_]*)\s*=[^=]/g)].map(m => m[1]),
  );
  const missing = [...assigned].filter(name => !(name in component));
  assert.deepEqual(missing, [], `nicht deklarierte Felder: ${missing.join(', ')}`);
});

test('destroy() meldet die Montage-Ereignisse wieder ab', () => {
  // Ohne das montiert die alte Komponente beim naechsten "Editieren" neben
  // der neuen mit und verdrahtet deren Knoten quer.
  const { dom, editor } = createEditor('<div class="layout-grid"><div class="layout-grid-item" data-layout-item-id="a"></div></div>');
  editor.destroy();
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  assert.equal(dom.window.document.querySelector('.layout-card-chrome'), null);
});

test('die Modale ziehen beim Mounten an den <body>', () => {
  // section.panel.active traegt eine Animation mit transform, deren Endzustand
  // stehen bleibt - ein Vorfahr mit transform ist Bezugsrahmen auch fuer
  // position:fixed. Im Panel zentrierte sich das Modal deshalb irgendwo im
  // Scrollbereich statt im Fenster.
  const { dom } = createEditor('<div id="layout-editor-root"><div class="layout-grid"><div class="layout-grid-item" data-layout-item-id="a"></div></div>' + FRAGMENT_MARKUP + '</div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  const body = dom.window.document.body;
  for (const id of ['layout-options-modal', 'layout-page-modal']) {
    assert.equal(dom.window.document.getElementById(id).parentElement, body, id);
  }
  // Beim Unmount zurueck an ihren Herkunftsort: der Uebersichts-Fragmenttausch
  // (Seitenwechsel) montiert gleich neu, und leaveEdit() loescht spaeter das
  // ganze #layout-editor-root - am <body> haengengeblieben wuerden sie das
  // ueberleben.
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:unmount'));
  for (const id of ['layout-options-modal', 'layout-page-modal']) {
    assert.equal(dom.window.document.getElementById(id).closest('#layout-editor-root')?.id, 'layout-editor-root', id);
  }
});

test('ein Fragment-Tausch mitten im Editor laesst sich neu montieren', () => {
  // Ein Seitenwechsel tauscht #overview-live samt .layout-toolbar und Raster;
  // #layout-editor-root bleibt stehen. Wandern die Werkzeugleisten-Knoepfe
  // beim Unmount nicht an ihren Herkunftsort zurueck, ist die Leiste nach dem
  // zweiten Mount leer.
  const { dom } = createEditor('<div id="overview-live"><div class="layout-toolbar"></div><div class="layout-grid"><div class="layout-grid-item" data-layout-item-id="a"></div></div></div><div id="layout-editor-root">' + FRAGMENT_MARKUP + '</div>');
  const doc = dom.window.document;
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  assert.ok(doc.querySelector('.layout-toolbar [data-mode-save]'), 'erster Mount fuellt die Leiste');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:unmount'));
  doc.getElementById('overview-live').innerHTML = '<div class="layout-toolbar"></div><div class="layout-grid"><div class="layout-grid-item" data-layout-item-id="a"></div></div>';
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  assert.ok(doc.querySelector('.layout-toolbar [data-mode-save]'), 'nach dem Tausch steht die Leiste wieder');
  assert.ok(doc.querySelector('.layout-edit-slot > .layout-card-chrome'), 'die frischen Karten bekommen wieder Chrome');
});

// --- Task 5a: echtes Chrome, Options-Modal, Auge ---------------------------

test('nach dem Mount traegt jede Kachel echtes Chrome', () => {
  const { dom } = createEditor('<div class="layout-grid"><div class="layout-grid-item" data-layout-item-id="a" data-layout-item-kind="energy_ring"></div></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  const chrome = dom.window.document.querySelector('.layout-edit-slot > .layout-card-chrome');
  assert.ok(chrome.querySelector('[data-role="options"]'));
  assert.ok(chrome.querySelector('.layout-chip-btn.grab'));
});

test('Klick auf ⋯ oeffnet das Options-Modal mit gefuelltem Body', () => {
  const { dom } = createEditorWithFragment('<div class="layout-grid"><div class="layout-grid-item" data-layout-item-id="a" data-layout-item-kind="energy_ring"></div></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  dom.window.document.querySelector('[data-role="options"]').click();
  const scrim = dom.window.document.getElementById('layout-options-modal');
  assert.ok(scrim.classList.contains('open'));
  assert.ok(scrim.querySelector('[data-modal-body] .layout-modal-fieldgroup'));
  assert.equal(scrim.querySelector('[data-modal-kind]').textContent, 'energy_ring');
});

test('Scrim-Klick schliesst das Modal', () => {
  const { dom } = createEditorWithFragment('<div class="layout-grid"><div class="layout-grid-item" data-layout-item-id="a" data-layout-item-kind="energy_ring"></div></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  dom.window.document.querySelector('[data-role="options"]').click();
  const scrim = dom.window.document.getElementById('layout-options-modal');
  scrim.dispatchEvent(new dom.window.MouseEvent('click', {bubbles: true}));
  assert.equal(scrim.classList.contains('open'), false);
});

test('das Auge toggelt Sichtbarkeit und markiert die Aenderung', () => {
  const { dom, editor } = createEditorWithFragment('<div class="layout-grid"><div class="layout-grid-item" data-layout-item-id="a" data-layout-item-kind="energy_ring"></div></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  dom.window.document.querySelector('[data-role="visible-toggle"]').click();
  assert.ok(dom.window.document.querySelector('.layout-edit-slot.is-hidden'));
  assert.equal(editor.unsaved, true);
});

test('eine Option im Modal aendert das this.pages-Item und markiert ungespeichert (Weg A)', () => {
  const { dom, editor } = createEditorWithFragment('<div class="layout-grid"><div class="layout-grid-item" data-layout-item-id="ring-1" data-layout-item-kind="energy_ring"></div></div>');
  editor.pages = [{id: 'p', name: 'Übersicht', groups: [{id: 'g', name: 'Energie', items: [
    {id: 'ring-1', type: 'energy_ring', ref: '', span: '2', visible: true, kpi: 'autarkie', visibleCategories: [], entityRefs: []},
  ]}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  dom.window.document.querySelector('[data-role="options"]').click();
  const kpi = dom.window.document.querySelector('[data-modal-body] [data-role="kpi"]');
  kpi.value = 'netz';
  kpi.dispatchEvent(new dom.window.Event('change', {bubbles: true}));
  assert.equal(editor.pages[0].groups[0].items[0].kpi, 'netz');
  assert.equal(editor.unsaved, true);
});

// --- Task 5b: Toolbox ----------------------------------------------------

test('die Toolbox oeffnet und listet das aktive Register', () => {
  const { dom } = createEditorWithFragment('<div class="layout-grid"></div>', {devices: [{id: 'wallbox', name: 'Wallbox', entities: []}]});
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  dom.window.document.querySelector('[data-toolbox-toggle]').click();
  const box = dom.window.document.getElementById('toolbox');
  assert.ok(box.classList.contains('open'));
  assert.ok(box.querySelector('[data-tb-list] .layout-toolbox-item'));
});

test('die Toolbox-Suche geht ueber alle Register', () => {
  const { dom } = createEditorWithFragment('<div class="layout-grid"></div>', {devices: [{id: 'wallbox', name: 'Wallbox', entities: []}]});
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  dom.window.document.querySelector('[data-toolbox-toggle]').click();
  const search = dom.window.document.querySelector('[data-tb-search]');
  search.value = 'wallbox';
  search.dispatchEvent(new dom.window.Event('input', {bubbles: true}));
  const titles = [...dom.window.document.querySelectorAll('[data-tb-list] .layout-toolbox-item b')].map(b => b.textContent);
  assert.ok(titles.some(t => /Wallbox/.test(t)));
});

test('Klick auf einen Baustein haengt ihn an die erste Gruppe der aktiven Seite', () => {
  const { dom, editor } = createEditorWithFragment('<div class="layout-grid"></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  editor.pages = [{id: 'p1', name: 'Zuhause', groups: [{id: 'g1', name: 'Energie', items: []}]}];
  dom.window.document.querySelector('[data-toolbox-toggle]').click();
  dom.window.document.querySelector('[data-tb-list] .layout-toolbox-item').click();
  assert.equal(editor.pages[0].groups[0].items.length, 1);
  assert.equal(editor.unsaved, true);
});

// --- Task 5c: Werkzeugleiste umhaengen, Zielbreite, Seite, Speichern ------

test('beim Mount stehen die edit-only-Knoepfe in der Live-Werkzeugleiste', () => {
  const { dom } = createEditorWithFragment('<div class="layout-toolbar"></div><div class="layout-grid"></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  const bar = dom.window.document.querySelector('.layout-toolbar');
  assert.ok(bar.querySelector('[data-mode-save]'));
  assert.ok(bar.querySelector('[data-w="390"]'));
});

test('ein Zielbreiten-Chip setzt Buehne und Hinweis', () => {
  const { dom } = createEditorWithFragment('<div class="layout-toolbar"></div><div class="layout-grid"><div class="layout-canvas-stage"></div></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  dom.window.document.querySelector('[data-w="1280"]').click();
  const stage = dom.window.document.querySelector('.layout-canvas-stage');
  assert.equal(stage.style.getPropertyValue('--target-w'), '1280px');
  assert.match(dom.window.document.querySelector('[data-widthnote]').textContent, /1280 px/);
});

test('Speichern & schliessen speichert und bittet um Verlassen', async () => {
  const { dom, editor } = createEditorWithFragment('<div class="layout-toolbar"></div><div class="layout-grid"></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  let left = 0;
  dom.window.document.addEventListener('layout-editor:request-leave', () => { left += 1; });
  editor.save = async () => { editor.unsaved = false; };
  await dom.window.document.querySelector('[data-mode-save]').click();
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(left, 1);
});

test('+ Seite ruft addPage und meldet die Aenderung der Navigation', () => {
  const { dom, editor } = createEditorWithFragment('<div class="layout-toolbar"></div><div class="layout-grid"></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  let changed = 0;
  dom.window.document.addEventListener('layout-pages-changed', () => { changed += 1; });
  const before = editor.pages.length;
  dom.window.document.querySelector('[data-addpage]').click();
  assert.equal(editor.pages.length, before + 1);
  assert.equal(changed, 1);
});

test('der Verwerfen-Knopf steht nur bei Aenderungen und fuehrt durch den Waechter', async () => {
  const { dom, editor } = createEditorWithFragment('<div class="layout-toolbar"></div><div class="layout-grid"></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  const btn = dom.window.document.querySelector('.layout-toolbar [data-mode-discard]');
  assert.ok(btn, 'der Knopf ist in die Live-Werkzeugleiste verschoben');
  assert.ok(btn.hidden, 'ohne Aenderung steht er nicht in der Leiste');
  editor.load = async () => {};
  editor.markUnsaved();
  assert.equal(btn.hidden, false, 'bei Aenderungen erscheint er');
  let saved = 0;
  dom.window.addEventListener('layout-saved', () => { saved += 1; });
  btn.click();
  await Promise.resolve();
  dom.window.document.querySelector('[data-guard-discard]').click();
  for (let i = 0; i < 5; i++) await Promise.resolve();
  assert.equal(editor.unsaved, false, 'der Waechter hat verworfen');
  assert.equal(saved, 1, 'die Uebersicht holt ihr Fragment neu');
});

test('der Wechsel zurueck auf Editorbreite animiert ueber die Pixelbreite statt auf auto zu springen', () => {
  const { dom } = createEditorWithFragment('<div class="layout-toolbar"></div><div class="layout-grid"><div class="layout-canvas-stage"></div></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  const stage = dom.window.document.querySelector('.layout-canvas-stage');
  dom.window.document.querySelector('[data-w="834"]').click();
  assert.equal(stage.style.getPropertyValue('--target-w'), '834px');
  dom.window.document.querySelector('[data-w="0"]').click();
  // width:auto laesst sich nicht uebergangsanimieren - erst eine Pixelbreite,
  // ein Timer raeumt danach auf auto zurueck.
  assert.match(stage.style.getPropertyValue('--target-w'), /^\d+px$/);
  assert.notEqual(stage.style.getPropertyValue('--target-w'), '834px');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:unmount'));
});

test('nach Speichern & schliessen laesst sich die Toolbox in der naechsten Sitzung wieder oeffnen', () => {
  const { dom, editor, factory } = createEditorWithFragment('<div class="layout-toolbar"></div><div class="layout-grid"></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  // "Speichern & schliessen": das Fragment wird geloescht, die Komponente
  // zerstoert. Ein am document haengengebliebener Toolbox-Klick der Vorsitzung
  // schlug die frisch geoeffnete Toolbox der naechsten sofort wieder zu.
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:unmount'));
  editor.destroy();
  const next = factory();
  next.$nextTick = fn => fn();
  attachStores(next);
  next.init();
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  dom.window.document.querySelector('[data-toolbox-toggle]').click();
  assert.ok(dom.window.document.getElementById('toolbox').classList.contains('open'));
});

// --- Der Editor arbeitet auf dem echten Raster, ohne GridStack ------------
// Die Uebersicht rendert die aktive Seite in einen [data-layout-page]-Traeger;
// die Fixtures bilden das ab, damit sie dieselbe DOM-Form pruefen, die
// overview.html tatsaechlich erzeugt.
const PAGE_GRID = cards => `<div class="layout-grid"><div data-layout-page="Zuhause">${cards}</div></div>`;
const CARD = (id, kind, span) =>
  `<div class="layout-grid-item layout-grid-item-${span}" data-layout-item-id="${id}" data-layout-item-kind="${kind}"></div>`;

test('mount() laesst jede Kachel als Rasterkind an ihrem Platz', () => {
  const { dom } = createEditor(PAGE_GRID(CARD('a', 'energy_flow', 'full') + CARD('b', 'device', '1')));
  const page = dom.window.document.querySelector('[data-layout-page]');
  const before = [...page.children];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  assert.deepEqual([...page.children], before, 'weder Huelle noch Umsortierung');
  for (const card of before) {
    assert.ok(card.classList.contains('layout-edit-slot'), 'die Kachel selbst ist der Slot');
    assert.ok(card.classList.contains('layout-grid-item'), 'sie bleibt Rasterkind');
  }
  assert.match(before[0].className, /layout-grid-item-full/, 'die span-Klasse bleibt unangetastet');
});

test('mount() haengt Chrome und Griff in die Kachel statt sie zu umhuellen', () => {
  const { dom } = createEditor(PAGE_GRID(CARD('a', 'device', '1')));
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  const card = dom.window.document.querySelector('.layout-grid-item');
  assert.ok(card.querySelector(':scope > .layout-card-chrome'), 'Chrome ist Kind der Kachel');
  assert.ok(card.querySelector(':scope > .layout-resize-grip'), 'Griff ist Kind der Kachel');
  assert.equal(card.querySelectorAll('.layout-resize-grip').length, 1, 'genau ein Griff');
});

test('mount() ruehrt GridStack nicht an', () => {
  const { dom, initCalls } = createEditor(PAGE_GRID(CARD('a', 'device', '1')));
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  assert.equal(initCalls.length, 0, 'kein GridStack.init auf dem Uebersichtsraster');
  assert.equal(dom.window.document.querySelector('.grid-stack-item, .grid-stack-item-content'), null);
  assert.equal(dom.window.document.querySelector('.layout-grid').classList.contains('grid-stack'), false);
});

test('unmount() gibt die Kachel unveraendert zurueck', () => {
  const { dom } = createEditor(PAGE_GRID(CARD('a', 'device', '1')));
  const card = dom.window.document.querySelector('.layout-grid-item');
  const classesBefore = card.className;
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:unmount'));

  assert.equal(card.className, classesBefore, 'die Klassenliste ist wieder die alte');
  assert.equal(card.querySelector('.layout-card-chrome, .layout-resize-grip'), null);
  assert.equal(card.parentElement.getAttribute('data-layout-page'), 'Zuhause', 'die Kachel haengt noch in der Seite');
});

test('ausgeblendete Kacheln der aktiven Seite stehen im Editor, im Ansichtsmodus nicht', () => {
  // Der Server rendert nur sichtbare Kacheln - nach dem Speichern und
  // Neuladen fehlte die ausgeblendete im Editor ganz, es gab keinen Weg, sie
  // ueber das Auge wieder einzublenden.
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(CARD('a', 'energy_ring', '1')));
  editor.pages = [{id: 'p', name: 'Zuhause', groups: [{id: 'g', name: 'D', items: [
    {id: 'a', type: 'energy_ring', span: '1', visible: true},
    {id: 'b', type: 'diagnostics', span: '1', visible: false},
  ]}]}];
  editor.activePage = 'Zuhause';
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  const hidden = dom.window.document.querySelector('[data-layout-item-id="b"]');
  assert.ok(hidden, 'die unsichtbare Kachel wird im Editor nachgezogen');
  assert.ok(hidden.classList.contains('is-hidden'), 'gedimmt, nicht regulaer');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:unmount'));
  assert.equal(dom.window.document.querySelector('[data-layout-item-id="b"]'), null, 'im Ansichtsmodus ist sie wieder weg');
});

// Task 7: Chrome und Optionen-Modal sind getrennte Bausteine, beide an der
// Fabrik der geladenen Komponente - wie widgetHTML().
const { factory: task7Factory } = loadLayoutPage({ gridstack: false });
const chromeHTML = task7Factory.chromeHTML;
const optionsSheetHTML = task7Factory.optionsSheetHTML;

test('die Kachel traegt kein Formularfeld mehr', () => {
  const html = chromeHTML({id: 'a', type: 'energy_ring', span: '2'});
  assert.doesNotMatch(html, /<select|<input/, 'Optionen gehoeren ins Modal');
  assert.match(html, /data-role="options"/);
});

test('das Modal gruppiert die Energie-Optionen statt sie aufzureihen', () => {
  const html = optionsSheetHTML({id: 'a', type: 'energy_ring', span: '2'}, []);
  for (const heading of ['Platz', 'Darstellung', 'Ort']) {
    assert.ok(html.includes(heading), `Gruppe ${heading} fehlt`);
  }
  // Keine eigene Sichtbarkeit-Gruppe mehr: die steuert das Auge im Chrome.
  assert.doesNotMatch(html, />Sichtbarkeit<\/h6>/);
  assert.doesNotMatch(html, /data-role="visible"/);
  assert.match(html, /data-role="span"/);
  assert.match(html, /data-role="height"/);
});

test('das Kachel-Chrome spricht das uebersetzte Vokabular', () => {
  const html = chromeHTML({id: 'a', type: 'energy_ring', span: '2', visible: true}, 'Speicher', false);
  assert.match(html, /class="layout-card-chrome"/);
  assert.match(html, /class="layout-chip-btn grab"/);
  assert.match(html, /class="layout-chip-btn"[^>]*data-role="visible-toggle"/);
  assert.match(html, /class="layout-chip-btn"[^>]*data-role="options"/);
  assert.match(html, /class="layout-resize-grip"/);
  assert.doesNotMatch(html, /layout-item-handle|layout-card-eye|layout-card-options/);
});

test('das Options-Modal nutzt die Modal-Feldklassen und keine mini-Silhouetten', () => {
  const html = optionsSheetHTML({id: 'a', type: 'energy_ring', span: '2'}, [], 'Speicher', 3);
  for (const heading of ['Platz', 'Darstellung', 'Ort']) assert.ok(html.includes('>' + heading + '</h6>'), heading);
  assert.match(html, /class="layout-modal-fieldgroup"/);
  assert.match(html, /class="layout-modal-field"/);
  assert.match(html, /class="layout-modal-hint"/);
  assert.match(html, /class="layout-modal-switchrow"/);
  assert.doesNotMatch(html, /mini-toggle|layout-options-sheet|layout-item-option\b/);
  assert.match(html, /data-role="span"/);
  assert.match(html, /data-role="height"/);
  assert.match(html, /data-role="kpi"/);
});

test('jede Schalterzeile im Modal ist ein <label>', () => {
  // Das <input> des Apple-Schalters ist 1px gross und durchsichtig
  // (.settings-toggle in base.css). Steht die Zeile als <div> da, rendert der
  // Schalter, laesst sich aber weder ueber die Spur noch ueber den Text
  // umlegen - genau das war bis 2026-09 im Optionsmodal der Fall.
  const html = optionsSheetHTML({id: 'a', type: 'device', ref: 'd1', span: '2'}, [], 'Kachel', 3);
  assert.doesNotMatch(html, /<div class="layout-modal-switchrow"/);
  for (const row of html.matchAll(/<(\w+) class="layout-modal-switchrow"/g)) assert.equal(row[1], 'label');
  assert.match(html, /<label class="layout-modal-switchrow">[^<]*<span class="settings-toggle">/);
});

test('die Zielbreite bleibt in echten Pixeln und wird nur optisch verkleinert', () => {
  const { applyTargetWidth } = loadLayoutPage({ gridstack: false }).factory;
  // zoom statt Verschmaelerung: sonst rechnen die Container-Queries auf die
  // Editorbreite und die Vorschau loege genauso wie heute.
  const stage = applyTargetWidth(1280, 640);
  assert.equal(stage['--target-w'], '1280px');
  assert.equal(stage['--target-zoom'], '0.5');
});

test('der Hinweis nennt Breite, Spaltenzahl und Verkleinerung', () => {
  const { widthNote } = loadLayoutPage({ gridstack: false }).factory;
  assert.equal(widthNote(390, 900), '390 px · 1 Spalte');
  assert.equal(widthNote(1280, 640), '1280 px · 4 Spalten · 50 % verkleinert');
});

test('Editorbreite ist die Voreinstellung und setzt nichts', () => {
  const { applyTargetWidth, widthNote } = loadLayoutPage({ gridstack: false }).factory;
  assert.deepEqual(JSON.parse(JSON.stringify(applyTargetWidth(0, 900))), {});
  assert.equal(widthNote(0, 900), '');
});

test('Editorbreite raeumt eine gesetzte Zielbreite wieder ab', () => {
  // Ohne das Abraeumen liess sich der Schalter nur in eine Richtung bedienen:
  // die Buehne blieb auf der zuletzt gewaehlten Geraetebreite stehen.
  const { factory, document } = createEditor('<div class="layout-canvas-stage"></div>');
  const stage = document.querySelector('.layout-canvas-stage');
  factory.applyTargetWidth(834, 1376);
  assert.equal(stage.style.getPropertyValue('--target-w'), '834px');
  factory.applyTargetWidth(0, 1376);
  assert.equal(stage.style.getPropertyValue('--target-w'), '');
  assert.equal(stage.style.getPropertyValue('--target-zoom'), '');
});

test('die Zielbreite wird nie vergroessert', () => {
  // Passt die Geraetebreite in den Editorbereich, steht die Buehne in ihrer
  // echten Groesse. Ohne die Kappung blies ein 390px-Handy auf 1376px
  // Editorbreite auf das 3,5-fache auf.
  const { applyTargetWidth } = loadLayoutPage({ gridstack: false }).factory;
  assert.equal(applyTargetWidth(390, 1376)['--target-zoom'], '1');
});

// --- Groessengriff: Pixelzug -> span/height ------------------------------
// Das gespeicherte Modell kennt nur Groessenklasse und Hoeheneinheit, keine
// Pixel. resizeTo() ist die ganze Rechnung dazwischen und darum der Ort, an
// dem sie geprueft wird - der Zeigerteil darueber ist nur Buchhaltung.
const GEOM = { trackWidth: 300.8, rowHeight: 124.8, columns: 4, minSpan: 1 };

test('resizeTo rastet die Breite auf ganze Spuren', () => {
  const { factory } = createEditor('');
  assert.equal(factory.resizeTo({span: '1', height: 0}, 0, 0, GEOM).span, '1');
  assert.equal(factory.resizeTo({span: '1', height: 0}, 310, 0, GEOM).span, '2');
  assert.equal(factory.resizeTo({span: '1', height: 0}, 140, 0, GEOM).span, '1', 'unter der halben Spur bleibt es');
});

test('resizeTo deckelt auf die vorhandene Spaltenzahl und nennt das voll', () => {
  const { factory } = createEditor('');
  // Volle Breite muss 'full' werden, nicht die Zahl 4: nur 1/-1 stimmt auch
  // dann noch, wenn der Betrachter das Fenster schmaler zieht.
  assert.equal(factory.resizeTo({span: '2', height: 0}, 9999, 0, GEOM).span, 'full');
});

test('resizeTo faellt unter die Mindestgroesse des Kartentyps nicht zurueck', () => {
  const { factory } = createEditor('');
  assert.equal(factory.resizeTo({span: '2', height: 0}, -9999, 0, {...GEOM, minSpan: 2}).span, '2');
});

test('resizeTo rastet die Hoehe auf 7.8rem-Einheiten und kennt auto', () => {
  const { factory } = createEditor('');
  assert.equal(factory.resizeTo({span: '1', height: 2}, 0, 130, GEOM).height, 3);
  assert.equal(factory.resizeTo({span: '1', height: 2}, 0, -130, GEOM).height, 1);
  assert.equal(factory.resizeTo({span: '1', height: 1}, 0, -130, GEOM).height, 0, '0 ist die automatische Hoehe');
});

test('der Griff schreibt die neue Groesse in die Kachel und nach this.pages', () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(CARD('a', 'device', '1')));
  editor.pages = [{id: 'p', name: 'Zuhause', groups: [{id: 'g', items: [
    {id: 'a', type: 'device', ref: 'd', span: '1', height: 0, visible: true},
  ]}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  const card = dom.window.document.querySelector('.layout-grid-item');
  editor.applyResize(card, {span: '3', height: 2});

  assert.match(card.className, /layout-grid-item-3/);
  assert.doesNotMatch(card.className, /layout-grid-item-1\b/);
  assert.equal(card.style.getPropertyValue('--card-height'), '14.8rem', 'height 2 = 2*7 + 1*.8 rem');
  assert.equal(editor.pages[0].groups[0].items[0].span, '3');
  assert.equal(editor.pages[0].groups[0].items[0].height, 2);
  assert.equal(editor.unsaved, true);
});

// --- Ziehen: Position ist Reihenfolge ------------------------------------

test('moveCardBefore ordnet die Kachel im DOM und in this.pages um', () => {
  const { dom, editor } = createEditorWithFragment(
    PAGE_GRID(CARD('a', 'device', '1') + CARD('b', 'device', '1') + CARD('c', 'device', '1')));
  editor.pages = [{id: 'p', name: 'Zuhause', groups: [{id: 'g', items: [
    {id: 'a', type: 'device', span: '1', height: 0, visible: true},
    {id: 'b', type: 'device', span: '1', height: 0, visible: true},
    {id: 'c', type: 'device', span: '1', height: 0, visible: true},
  ]}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  const doc = dom.window.document;
  editor.moveCardBefore(doc.querySelector('[data-layout-item-id="c"]'), doc.querySelector('[data-layout-item-id="a"]'));

  assert.deepEqual([...doc.querySelectorAll('.layout-grid-item')].map(el => el.dataset.layoutItemId), ['c', 'a', 'b']);
  assert.deepEqual(editor.pages[0].groups[0].items.map(item => item.id), ['c', 'a', 'b']);
  assert.equal(editor.unsaved, true);
});

test('moveCardBefore ans Ende haengt die Kachel hinten an', () => {
  const { dom, editor } = createEditorWithFragment(
    PAGE_GRID(CARD('a', 'device', '1') + CARD('b', 'device', '1')));
  editor.pages = [{id: 'p', name: 'Zuhause', groups: [{id: 'g', items: [
    {id: 'a', type: 'device', span: '1', height: 0, visible: true},
    {id: 'b', type: 'device', span: '1', height: 0, visible: true},
  ]}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  const doc = dom.window.document;
  editor.moveCardBefore(doc.querySelector('[data-layout-item-id="a"]'), null);

  assert.deepEqual([...doc.querySelectorAll('.layout-grid-item')].map(el => el.dataset.layoutItemId), ['b', 'a']);
  assert.deepEqual(editor.pages[0].groups[0].items.map(item => item.id), ['b', 'a']);
});

// --- Einfuegepunkt aus Zeigerkoordinaten ---------------------------------
// jsdom rechnet kein Layout, darum bekommt insertionPoint die Rechtecke
// gereicht statt sie zu messen. Das ist zugleich die ganze Logik.
const box = (el, left, top, width, height) =>
  ({el, rect: {left, top, right: left + width, bottom: top + height, width, height}});

test('insertionPoint trifft die linke Haelfte einer Kachel', () => {
  const { factory } = createEditor('');
  const boxes = [box('a', 0, 0, 100, 50), box('b', 100, 0, 100, 50)];
  assert.equal(factory.insertionPoint(boxes, 20, 25), 'a', 'links von a');
  assert.equal(factory.insertionPoint(boxes, 120, 25), 'b', 'links von b');
  assert.equal(factory.insertionPoint(boxes, 180, 25), null, 'rechts von allem = ans Ende');
});

test('insertionPoint springt bei mehreren Zeilen in die richtige Zeile', () => {
  const { factory } = createEditor('');
  const boxes = [box('a', 0, 0, 100, 50), box('b', 0, 60, 100, 50)];
  assert.equal(factory.insertionPoint(boxes, 20, 70), 'b', 'zweite Zeile');
  assert.equal(factory.insertionPoint(boxes, 80, 70), null, 'hinter der letzten Kachel');
});

// --- Aus der Toolbox ins Raster ------------------------------------------

test('addFromCatalog setzt eine bedienbare Kachel ins Raster', () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(CARD('a', 'device', '1')));
  editor.cardTypes = CARD_TYPES;
  editor.pages = [{id: 'p', name: 'Zuhause', groups: [{id: 'g', items: [
    {id: 'a', type: 'device', span: '1', height: 0, visible: true},
  ]}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  editor.addFromCatalog({id: 'energy-flow', type: 'energy_flow', label: 'Energiefluss'});

  const cards = [...dom.window.document.querySelectorAll('.layout-grid-item')];
  assert.equal(cards.length, 2, 'die neue Karte steht im Raster, nicht nur im Modell');
  const fresh = cards[1];
  assert.equal(fresh.dataset.layoutItemKind, 'energy_flow');
  assert.ok(fresh.classList.contains('layout-edit-slot'), 'sie ist sofort bedienbar');
  assert.ok(fresh.querySelector('.layout-card-chrome'), 'mit Chrome');
  assert.ok(fresh.classList.contains('is-entering'), 'und faehrt ein');
  assert.equal(editor.pages[0].groups[0].items[1].type, 'energy_flow');
  assert.equal(editor.unsaved, true);
});

test('addFromCatalog fuegt an der Zeigerposition ein, nicht nur hinten', () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(CARD('a', 'device', '1')));
  editor.cardTypes = CARD_TYPES;
  editor.pages = [{id: 'p', name: 'Zuhause', groups: [{id: 'g', items: [
    {id: 'a', type: 'device', span: '1', height: 0, visible: true},
  ]}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  const first = dom.window.document.querySelector('[data-layout-item-id="a"]');
  editor.addFromCatalog({id: 'diagnostics', type: 'diagnostics', label: 'Diagnose'}, first);

  const ids = [...dom.window.document.querySelectorAll('.layout-grid-item')].map(el => el.dataset.layoutItemKind);
  assert.deepEqual(ids, ['diagnostics', 'device']);
  assert.deepEqual(editor.pages[0].groups[0].items.map(item => item.type), ['diagnostics', 'device']);
});

test('ein Drop aus der Toolbox legt die Karte ab', () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(CARD('a', 'device', '1')));
  editor.cardTypes = CARD_TYPES;
  editor.pages = [{id: 'p', name: 'Zuhause', groups: [{id: 'g', items: [
    {id: 'a', type: 'device', span: '1', height: 0, visible: true},
  ]}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  const grid = dom.window.document.querySelector('.layout-grid');
  const event = new dom.window.Event('drop', {bubbles: true, cancelable: true});
  event.clientX = 9999; event.clientY = 9999;
  event.dataTransfer = {getData: () => JSON.stringify({id: 'diagnostics', type: 'diagnostics', label: 'Diagnose'})};
  grid.dispatchEvent(event);

  assert.equal(dom.window.document.querySelectorAll('.layout-grid-item').length, 2);
  assert.equal(event.defaultPrevented, true, 'der Browser darf den Drop nicht selbst deuten');
});

test('die Toolbox-Eintraege sind ziehbar und geben den Katalogeintrag mit', () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(CARD('a', 'device', '1')));
  editor.cardTypes = CARD_TYPES;
  editor.pages = [{id: 'p', name: 'Zuhause', groups: [{id: 'g', items: []}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  editor.setToolbox(true);

  const entry = dom.window.document.querySelector('[data-tb-list] [data-add]');
  assert.equal(entry.draggable, true, 'ohne draggable feuert kein dragstart');

  const stored = {};
  const event = new dom.window.Event('dragstart', {bubbles: true});
  event.dataTransfer = {setData: (type, value) => { stored[type] = value; }, effectAllowed: '', setDragImage() {}};
  entry.dispatchEvent(event);

  const payload = JSON.parse(stored['application/x-layout-card'] || stored['text/plain']);
  assert.equal(typeof payload.type, 'string');
  assert.ok(payload.type.length > 0, 'der Kartentyp reist mit');
});

test('ein zweiter Bearbeitungsmodus verdoppelt die Zeigergesten nicht', () => {
  // Das .layout-grid ueberlebt den Moduswechsel - es ist das echte Raster der
  // Ansicht. Wer beim Mounten Listener anhaengt, muss sie beim Unmounten
  // wieder loesen, sonst legt ein Drop nach dem zweiten "Editieren" zwei
  // Karten ab.
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(CARD('a', 'device', '1')));
  editor.cardTypes = CARD_TYPES;
  editor.pages = [{id: 'p', name: 'Zuhause', groups: [{id: 'g', items: [
    {id: 'a', type: 'device', span: '1', height: 0, visible: true},
  ]}]}];
  for (const name of ['mount', 'unmount', 'mount']) {
    document.dispatchEvent(new dom.window.CustomEvent(`layout-editor:${name}`));
  }

  const grid = dom.window.document.querySelector('.layout-grid');
  const event = new dom.window.Event('drop', {bubbles: true, cancelable: true});
  event.clientX = 9999; event.clientY = 9999;
  event.dataTransfer = {getData: () => JSON.stringify({id: 'diagnostics', type: 'diagnostics', label: 'Diagnose'})};
  grid.dispatchEvent(event);

  assert.equal(dom.window.document.querySelectorAll('.layout-grid-item').length, 2, 'genau eine neue Karte');
});

// --- Kachel entfernen -----------------------------------------------------

test('das Chrome bietet einen Knopf zum Entfernen', () => {
  const { dom } = createEditor(PAGE_GRID(CARD('a', 'device', '1')));
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  const card = dom.window.document.querySelector('.layout-grid-item');
  assert.ok(card.querySelector('.layout-card-chrome [data-role="remove"]'), 'kein Entfernen-Knopf');
});

test('Entfernen nimmt die Kachel aus DOM und Modell', () => {
  const { dom, editor } = createEditorWithFragment(
    PAGE_GRID(CARD('a', 'device', '1') + CARD('b', 'device', '1')));
  editor.pages = [{id: 'p', name: 'Zuhause', groups: [{id: 'g', items: [
    {id: 'a', type: 'device', span: '1', height: 0, visible: true},
    {id: 'b', type: 'device', span: '1', height: 0, visible: true},
  ]}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  const doc = dom.window.document;
  doc.querySelector('[data-layout-item-id="a"] [data-role="remove"]').click();

  assert.equal(doc.querySelectorAll('.layout-grid-item').length, 1);
  assert.equal(doc.querySelector('[data-layout-item-id="a"]'), null);
  assert.deepEqual(editor.pages[0].groups[0].items.map(item => item.id), ['b']);
  assert.equal(editor.unsaved, true);
});

test('Entfernen schliesst ein offenes Modal derselben Kachel', () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(CARD('a', 'device', '1')));
  editor.pages = [{id: 'p', name: 'Zuhause', groups: [{id: 'g', items: [
    {id: 'a', type: 'device', span: '1', height: 0, visible: true},
  ]}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  const doc = dom.window.document;
  doc.querySelector('[data-layout-item-id="a"] [data-role="options"]').click();
  assert.equal(doc.getElementById('layout-options-modal').classList.contains('open'), true);
  doc.querySelector('[data-layout-item-id="a"] [data-role="remove"]').click();

  assert.equal(doc.getElementById('layout-options-modal').classList.contains('open'), false,
    'ein Modal zu einer geloeschten Kachel darf nicht stehenbleiben');
});

// --- Seiten-Modal: umbenennen, verschieben, loeschen ----------------------

function editorWithPages(names) {
  const built = createEditorWithFragment(PAGE_GRID(CARD('a', 'device', '1')));
  built.editor.pages = names.map((name, i) => ({
    id: `p${i}`, name, order: i,
    groups: [{id: `g${i}`, name: 'Dashboard', items: []}],
  }));
  built.editor.activePage = names[0];
  document.dispatchEvent(new built.dom.window.CustomEvent('layout-editor:mount'));
  return built;
}

test('das Seiten-Modal zeigt den Namen der aktiven Seite', () => {
  const { dom, editor } = editorWithPages(['Zuhause', 'Werkstatt']);
  editor.openPageOptions();
  const scrim = dom.window.document.getElementById('layout-page-modal');
  assert.equal(scrim.classList.contains('open'), true);
  assert.equal(scrim.querySelector('[data-page-name]').value, 'Zuhause');
});

test('Umbenennen schreibt den Namen nach this.pages und meldet es der Navigation', () => {
  const { dom, editor } = editorWithPages(['Zuhause', 'Werkstatt']);
  let announced = null;
  document.addEventListener('layout-pages-changed', event => { announced = event.detail.pages; });
  editor.openPageOptions();

  const input = dom.window.document.querySelector('[data-page-name]');
  input.value = 'Keller';
  input.dispatchEvent(new dom.window.Event('input', {bubbles: true}));

  assert.equal(editor.pages[0].name, 'Keller');
  assert.equal(editor.activePage, 'Keller', 'die aktive Seite heisst jetzt auch so');
  assert.deepEqual(announced, ['Keller', 'Werkstatt']);
  assert.equal(editor.unsaved, true);
});

test('ein leerer Name wird nicht uebernommen', () => {
  const { dom, editor } = editorWithPages(['Zuhause']);
  editor.openPageOptions();
  const input = dom.window.document.querySelector('[data-page-name]');
  input.value = '   ';
  input.dispatchEvent(new dom.window.Event('input', {bubbles: true}));
  assert.equal(editor.pages[0].name, 'Zuhause');
});

test('die Reihenfolge laesst sich im Modal verschieben', () => {
  const { dom, editor } = editorWithPages(['Zuhause', 'Werkstatt']);
  editor.openPageOptions();
  dom.window.document.querySelector('[data-page-move="1"]').click();
  assert.deepEqual(editor.pages.map(page => page.name), ['Werkstatt', 'Zuhause']);
  assert.equal(editor.unsaved, true);
});

test('die letzte Seite laesst sich nicht loeschen', () => {
  const { dom, editor } = editorWithPages(['Zuhause']);
  editor.openPageOptions();
  const remove = dom.window.document.querySelector('[data-page-remove]');
  assert.equal(remove.disabled, true, 'ohne Seite gaebe es keinen Weg zurueck in den Editor');
  remove.click();
  assert.equal(editor.pages.length, 1);
});

test('Loeschen entfernt die Seite und schaltet auf die erste um', () => {
  const { dom, editor } = editorWithPages(['Zuhause', 'Werkstatt']);
  editor.openPageOptions();
  dom.window.document.querySelector('[data-page-remove]').click();
  assert.deepEqual(editor.pages.map(page => page.name), ['Werkstatt']);
  assert.equal(editor.activePage, 'Werkstatt');
  assert.equal(dom.window.document.getElementById('layout-page-modal').classList.contains('open'), false);
});

// --- Seiten, Bindung an this.pages, Toolbox-Verankerung -------------------

test('nach load() haengt jede Kachel an ihrem Item in this.pages', async () => {
  // mount() laeuft direkt nach Alpine.initTree(), load() ist asynchron: in
  // diesem Augenblick ist this.pages leer und editItems voller loser
  // datasetToItem()-Rueckfaelle. Aenderungen daran sah payload() nie.
  const layout = {version: 3, card_types: CARD_TYPES, pages: [{
    id: 'p1', name: 'Zuhause', groups: [{id: 'g1', items: [
      {id: 'a', type: 'entity_value', ref: '', span: '1', visible: true},
    ]}],
  }]};
  const devices = [{id: 'wallbox', name: 'Wallbox', entities: [{unique_id: 'e1', name: 'Leistung'}]}];
  const fetchImpl = async url => jsonResponse(String(url).includes('/devices') ? devices : layout);

  const { dom, editor } = createEditorWithFragment(PAGE_GRID(CARD('a', 'entity_value', '1')));
  dom.window.fetch = fetchImpl;
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  await editor.load();

  const slot = dom.window.document.querySelector('.layout-edit-slot');
  assert.equal(editor.itemForSlot(slot), editor.pages[0].groups[0].items[0], 'die Kachel zeigt auf das echte Item');

  slot.querySelector('[data-role="options"]').click();
  const select = dom.window.document.querySelector('[data-modal-body] [data-role="entity-value-ref"]');
  select.value = 'e1';
  select.dispatchEvent(new dom.window.Event('change', {bubbles: true}));

  assert.equal(editor.payload().pages[0].groups[0].items[0].ref, 'e1', 'die gewaehlte Entitaet wird gespeichert');
});

test('eine neue Seite bekommt eine Gruppe und nimmt die Bausteine auf', () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(CARD('a', 'device', '1')));
  editor.cardTypes = CARD_TYPES;
  editor.pages = [{id: 'p1', name: 'Zuhause', groups: [{id: 'g1', items: [
    {id: 'a', type: 'device', ref: 'x', span: '1', height: 0, visible: true},
  ]}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  dom.window.document.querySelector('[data-addpage]').click();
  assert.equal(editor.pages.length, 2);
  assert.equal(editor.activePage, 'Neue Seite');
  const host = dom.window.document.querySelector('[data-layout-page]');
  assert.equal(host.dataset.layoutPage, 'Neue Seite');
  assert.equal(host.children.length, 0, 'die neue Seite startet leer');

  editor.addFromCatalog({id: 'diagnostics', type: 'diagnostics', title: 'Diagnosen'});
  assert.equal(editor.pages[1].groups[0].items.length, 1, 'der Baustein landet auf der neuen Seite');
  assert.equal(editor.pages[0].groups[0].items.length, 1, 'und nicht auf der ersten');
  assert.equal(host.children.length, 1);
});

test('zwei neue Seiten teilen sich keinen Namen', () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(''));
  editor.cardTypes = CARD_TYPES;
  editor.pages = [{id: 'p1', name: 'Zuhause', groups: []}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  editor.addPage();
  editor.addPage();
  assert.deepEqual(editor.pages.map(page => page.name), ['Zuhause', 'Neue Seite', 'Neue Seite 2']);
});

test('mount() uebernimmt die Seite der Tab-Leiste, auch beim zweiten Mal', () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(CARD('a', 'device', '1')));
  editor.cardTypes = CARD_TYPES;
  editor.pages = [
    {id: 'p1', name: 'Zuhause', groups: [{id: 'g1', items: [{id: 'a', type: 'device', ref: 'x', span: '1', visible: true}]}]},
    {id: 'p2', name: 'Keller', groups: [{id: 'g2', items: []}]},
  ];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  assert.equal(editor.activePage, 'Zuhause');

  // Der Seitenwechsel: die Tab-Leiste waehlt, der Server liefert das
  // Fragment, danach montiert die Huelle den Editor neu.
  dom.window.__dashboardShell__ = {activePage: 'Keller'};
  dom.window.document.querySelector('[data-layout-page]').dataset.layoutPage = 'Keller';
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:unmount'));
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  assert.equal(editor.activePage, 'Keller');

  editor.addFromCatalog({id: 'diagnostics', type: 'diagnostics', title: 'Diagnosen'});
  assert.equal(editor.pages[1].groups[0].items.length, 1, 'der Baustein landet auf der gewaehlten Seite');
});

test('die Toolbox haengt am <body> und folgt der Werkzeugleiste', () => {
  // position:absolute im Panel liess sie nach dem Scrollen ausserhalb des
  // Sichtbereichs aufgehen - sichtbar als "die Toolbox oeffnet nicht mehr".
  const { dom, editor } = createEditor(
    '<div class="layout-toolbar"></div>' + PAGE_GRID('')
    // Wie in der Anwendung: das Fragment haengt unter #layout-editor-root,
    // nicht direkt am <body>.
    + `<div id="layout-editor-root">${FRAGMENT_MARKUP}</div>`);
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  const box = dom.window.document.getElementById('toolbox');
  assert.equal(box.parentElement, dom.window.document.body, 'sonst gilt der Bezugsrahmen des Panels');

  dom.window.document.querySelector('.layout-toolbar').getBoundingClientRect = () => ({bottom: 48});
  editor.setToolbox(true);
  assert.equal(box.style.getPropertyValue('--toolbox-top'), '48px');

  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:unmount'));
  assert.notEqual(box.parentElement, dom.window.document.body, 'beim Verlassen zieht sie zurueck ins Fragment');
});

test('der Fuss der Toolbox nennt die aktive Seite', () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(''));
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  editor.pages = [{id: 'p1', name: 'Keller', groups: [{id: 'g1', items: []}]}];
  editor.activePage = 'Keller';
  editor.setToolbox(true);
  assert.equal(dom.window.document.querySelector('[data-tb-target]').textContent, 'Keller');
});

test('eine Entitaetenliste aus der Toolbox bekommt eine eigene ID', () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(''));
  editor.cardTypes = CARD_TYPES;
  editor.pages = [{id: 'p1', name: 'Zuhause', groups: [{id: 'g1', items: []}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  editor.addFromCatalog({id: 'entity-group', type: 'entity_group', title: 'Entitätenliste'});
  editor.addFromCatalog({id: 'entity-group', type: 'entity_group', title: 'Entitätenliste'});
  const items = editor.pages[0].groups[0].items;
  assert.equal(items.length, 2, 'zwei Listen muessen nebeneinander stehen koennen');
  assert.notEqual(items[0].id, items[1].id);
  assert.equal(items[0].entityRefs.length, 0);
});

test('Verwerfen setzt den Editor zurueck statt nur weiterzulassen', async () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(''));
  let loaded = 0;
  editor.load = async () => { loaded++; };
  editor.unsaved = true;
  const pending = editor.confirmLeave('tab');
  dom.window.document.querySelector('[data-guard-discard]').click();
  assert.equal(await pending, true);
  assert.equal(editor.unsaved, false, 'sonst fragt der Waechter beim naechsten Wechsel erneut');
  assert.equal(loaded, 1, 'und die verworfenen Aenderungen stehen weiter in this.pages');
});

test('dieselbe Karte auf zwei Seiten teilt sich keine ID', () => {
  // Bis 2026-09 uebernahm addFromCatalog() die Katalog-ID: die Diagnose-Karte
  // auf Seite 1 und die auf Seite 2 hiessen beide 'diagnostics'.
  // findPagesItem() fand die erste - das Optionen-Modal der zweiten aenderte
  // die erste mit, und Entfernen loeschte beide.
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(''));
  editor.cardTypes = CARD_TYPES;
  editor.pages = [
    {id: 'p1', name: 'Zuhause', groups: [{id: 'g1', items: []}]},
    {id: 'p2', name: 'Keller', groups: [{id: 'g2', items: []}]},
  ];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  editor.activePage = 'Zuhause';
  const first = editor.addFromCatalog({id: 'diagnostics', type: 'diagnostics', title: 'Diagnosen'});
  editor.activePage = 'Keller';
  const second = editor.addFromCatalog({id: 'diagnostics', type: 'diagnostics', title: 'Diagnosen'});

  assert.notEqual(first.id, second.id);
  assert.equal(editor.findPagesItem(second.id), second);
  assert.equal(editor.itemLabel(second), 'Diagnosen', 'der Name kommt aus dem Typ, nicht aus der ID');
});

test('die erste Seite im Leerzustand bekommt einen Traeger im Raster', () => {
  const { dom, editor } = createEditorWithFragment(
    '<div class="layout-grid"><p class="panel-empty">Noch keine Seite eingerichtet.</p></div>');
  editor.cardTypes = CARD_TYPES;
  editor.pages = [];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  dom.window.document.querySelector('[data-addpage]').click();

  const host = dom.window.document.querySelector('.layout-grid [data-layout-page]');
  assert.ok(host, 'ohne Traeger haette die neue Seite keinen Platz fuer Kacheln');
  assert.equal(host.dataset.layoutPage, 'Neue Seite');
  assert.equal(dom.window.document.querySelector('.panel-empty'), null);

  editor.addFromCatalog({id: 'diagnostics', type: 'diagnostics', title: 'Diagnosen'});
  assert.equal(host.children.length, 1);
});

test('cardKey() bildet die kompakte Geraetekachel auf ihren Varianteneintrag ab', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  assert.equal(factory.cardKey({type: 'device', display: 'compact'}), 'device:compact');
  assert.equal(factory.cardKey({type: 'device', display: 'detail'}), 'device');
  assert.equal(factory.cardKey({type: 'device'}), 'device');
  assert.equal(factory.cardKey({type: 'energy_ring', display: 'compact'}), 'energy_ring');
});

test('das Optionen-Modal einer Geraetekachel bietet die Darstellung an und blendet die Kategorien im Kompaktmodus aus', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const devices = [{id: 'dev1', name: 'Gerät 1', entities: []}];

  const detail = factory.optionsSheetHTML({id: 'a', type: 'device', ref: 'dev1', span: '1', visible: true, display: 'detail', visibleCategories: []}, devices, 'Gerät 1', 4);
  assert.match(detail, /data-role="display"/);
  assert.match(detail, /<option value="compact"(?! selected)/);
  assert.match(detail, /value="measurements"/, 'die Kategorieschalter fehlen in der Detailansicht');

  const compact = factory.optionsSheetHTML({id: 'a', type: 'device', ref: 'dev1', span: '1', visible: true, display: 'compact', visibleCategories: []}, devices, 'Gerät 1', 4);
  assert.match(compact, /<option value="compact" selected/);
  assert.equal(/value="measurements"/.test(compact), false, 'die Kategorieschalter wirken kompakt nicht und duerfen nicht dastehen');
  assert.match(compact, /layout-modal-hint/, 'der Hinweis auf die Kompaktauswahl fehlt');
});

test('toGridNode/fromGridNode tragen display, und die kompakte Kachel startet flacher', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const node = factory.toGridNode({id: 'a', type: 'device', ref: 'dev1', span: '1', visible: true, display: 'compact'}, 'Gerät 1', false, 4);
  assert.equal(node.display, 'compact');
  assert.equal(factory.fromGridNode(node).display, 'compact');
  assert.equal(factory.fromGridNode({id: 'b', type: 'device', ref: 'dev1', visible: true}).display, '');
});

test('display steht im Payload nur, wenn es gesetzt ist', async () => {
  const { component } = await loadedComponent();
  const item = component.pages[0].groups[0].items.find(entry => entry.type === 'device');
  item.display = 'compact';
  const band = component.pages[0].groups[0].items.find(entry => entry.type === 'energy_band');
  const sent = component.payload().pages[0].groups[0].items;
  assert.equal(sent.find(entry => entry.type === 'device').display, 'compact');
  assert.equal('display' in sent.find(entry => entry.id === band.id), false);
});

test('das Optionen-Modal einer Geraetekachel bietet die Geraeteauswahl an', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const devices = [{id: 'dev1', name: 'Gerät 1', entities: []}, {id: 'dev2', name: 'Gerät 2', entities: []}];
  const html = factory.optionsSheetHTML({id: 'a', type: 'device', ref: 'dev2', span: '1', visible: true, display: 'detail', visibleCategories: []}, devices, 'Gerät 2', 4);
  assert.match(html, /data-role="device-ref"/);
  assert.match(html, /<option value="dev2" selected>Gerät 2<\/option>/);
  assert.equal(/keine eigene Datenquelle/.test(html), false, 'die Geraetekachel hat sehr wohl eine Datenquelle');
});

test('ein Geraetewechsel im Modal zieht Ref, Name und data-display an der Kachel nach', async () => {
  const { component, document } = await loadedComponent();
  component.devices = [{id: 'dev1', name: 'Gerät 1', entities: []}, {id: 'dev2', name: 'Gerät 2', entities: []}];
  const item = component.pages[0].groups[0].items.find(entry => entry.type === 'device');

  const slot = document.createElement('div');
  slot.className = 'layout-grid-item layout-edit-slot';
  slot.dataset.layoutItemId = item.id;
  slot.dataset.layoutItemKind = 'device';
  slot.dataset.layoutItemRef = item.ref;
  slot.innerHTML = '<div class="card-title"><b>Gerät 1</b><small>device</small></div>';
  document.body.appendChild(slot);
  component._optionsItem = item;
  component._optionsSlot = slot;

  const select = document.createElement('select');
  select.dataset.role = 'device-ref';
  select.innerHTML = '<option value="dev1"></option><option value="dev2"></option>';
  select.value = 'dev2';
  component.applyOptionChange({target: select});

  assert.equal(item.ref, 'dev2');
  assert.equal(slot.dataset.layoutItemRef, 'dev2');
  assert.equal(slot.dataset.display, 'detail');
  assert.equal(slot.querySelector('.card-title b').textContent, 'Gerät: Gerät 2');
  assert.equal(component.unsaved, true);
});

// applyOptionChange()'s eigener 'display'-Zweig, nicht nur optionsSheetHTML()s
// Markup: das Select-Change-Ereignis ist der Weg, auf dem data-display an der
// Kachel im laufenden Editor tatsaechlich veraendert wird - derselbe Pfad,
// den dashboard.js liest, um die kompakte Kachel im SSE-Push zu erkennen.
test('ein Darstellungswechsel im Modal setzt item.display und data-display an der Kachel', async () => {
  const { component, document } = await loadedComponent();
  const item = component.pages[0].groups[0].items.find(entry => entry.type === 'device');

  const slot = document.createElement('div');
  slot.className = 'layout-grid-item layout-edit-slot';
  slot.dataset.layoutItemId = item.id;
  slot.dataset.layoutItemKind = 'device';
  slot.dataset.layoutItemRef = item.ref;
  slot.dataset.display = 'detail';
  slot.innerHTML = '<div class="card-title"><b>Gerät 1</b><small>device</small></div>';
  document.body.appendChild(slot);
  component._optionsItem = item;
  component._optionsSlot = slot;

  const select = document.createElement('select');
  select.dataset.role = 'display';
  select.innerHTML = '<option value="detail"></option><option value="compact"></option>';
  select.value = 'compact';
  component.applyOptionChange({target: select});

  assert.equal(item.display, 'compact');
  assert.equal(slot.dataset.display, 'compact');
});

// Farben kommen ausschliesslich aus den Theme-Tokens (--flow-pv, --accent,
// --text-subtle ...), nie aus einem festen Hex/rgb/hsl-Wert oder einer
// benannten CSS-Farbe - sonst bricht dieselbe Miniatur wieder auf einem
// hellen Theme wie "tageslicht", wie es vor dieser Umstellung mit den harten
// Dark-Theme-Hex-Werten der Fall war. Prueft zusaetzlich, dass jeder
// verwendete Token auch tatsaechlich in base.css deklariert ist - ein
// Tippfehler im Token waere sonst schwarz (der CSS-Fallback von var() ohne
// zweites Argument) und auf den dunklen Themes unsichtbar, derselbe Fehler
// nur eine Ebene tiefer verschoben.
test('keine Toolbox-Miniatur traegt eine feste Farbe', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  const baseCSS = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'css', 'base.css'), 'utf8');
  for (const [type, svg] of Object.entries(factory.THUMB)) {
    assert.equal(/#[0-9a-fA-F]{3,8}\b/.test(svg), false, `${type} enthaelt einen Hex-Wert`);
    assert.equal(/rgb\(|hsl\(/.test(svg), false, `${type} enthaelt eine rgb/hsl-Farbe`);
    const paints = [...svg.matchAll(/\b(?:fill|stroke)="([^"]*)"/g)].map(m => m[1]);
    assert.ok(paints.length > 0, `${type} hat kein fill/stroke-Attribut`);
    for (const paint of paints) {
      assert.ok(paint === 'none' || paint.startsWith('var(--'), `${type}: "${paint}" ist weder "none" noch ein Theme-Token`);
      if (paint.startsWith('var(--')) {
        const token = paint.slice('var('.length, -1);
        assert.match(baseCSS, new RegExp(`${token}\\s*:`), `${type}: Token ${token} ist in base.css nicht deklariert`);
      }
    }
  }
});

test('cardKey bildet die Trajektorie auf ihren Variantenschlüssel ab', () => {
  const { factory } = loadLayoutPage({ gridstack: false });
  assert.equal(factory.cardKey({ type: 'battery_status', display: 'trajectory' }), 'battery_status:trajectory');
  assert.equal(factory.cardKey({ type: 'battery_status', display: 'column' }), 'battery_status');
  assert.equal(factory.cardKey({ type: 'battery_status' }), 'battery_status');
  assert.equal(factory.cardKey({ type: 'device', display: 'compact' }), 'device:compact');
});

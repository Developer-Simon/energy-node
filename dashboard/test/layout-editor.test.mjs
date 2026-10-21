// Regression tests for layout-editor.js: the live editor path (Weg A).
// Pages, cards and the options modal are plain this.pages items. The tests
// cover the item<->payload round trip, the options modal (optionsSheetHTML and
// applyOptionChange), card chrome (dressCard), toolbox catalog, mount/unmount
// on the real overview DOM, and the page and card actions. jsdom has no real
// drag or resize, so pointer gestures are not driven here.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { attachStores } from './helpers/notify-stores.mjs';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'layout-editor.js'),
  'utf8',
);

function loadLayoutPage({ fetchImpl } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  installI18n(dom.window);
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  vm.runInContext(scriptSource, context);
  const component = factory();
  component.$nextTick = fn => fn();
  const stores = attachStores(component);
  return { component, window: dom.window, document: dom.window.document, factory, stores };
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
  installI18n(dom.window);
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  dom.window.fetch = async () => { throw new Error('fetch should not be called'); };
  vm.runInContext(scriptSource, context);
  global.document = dom.window.document;
  const editor = factory();
  editor.$nextTick = fn => fn();
  const stores = attachStores(editor);
  editor.init();
  return { dom, editor, window: dom.window, document: dom.window.document, factory, stores };
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
  <div class="layout-toolbox-foot"><span data-tb-target>Klicken oder ziehen — landet auf der aktiven Seite.</span></div>
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
    id: 'p', name: '', order: 0,
    groups: [{id: 'g', name: 'Dashboard', items: [
      {id: 'energy-band', type: 'energy_band', ref: '', span: '2', visible: true},
      {id: 'device:dev1', type: 'device', ref: 'dev1', span: '1', visible: true},
    ]}],
  }],
};

// Gibt dieselben Schluessel zurueck wie loadLayoutPage (component, window,
// document, factory, stores), nur eben nach einem durchlaufenen
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
  await loaded.component.load();
  return loaded;
}

test('mount() gibt einer Kachel ohne span-Klasse den Typ-Default und Hoehe 0', () => {
  const { dom, editor } = createEditor('<div class="layout-grid"><div class="layout-grid-item" data-layout-item-id="a" data-layout-item-kind="device" data-layout-item-ref="dev1"></div></div>');
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  const item = editor.itemForSlot(dom.window.document.querySelector('.layout-edit-slot'));
  assert.equal(item.span, '1');
  assert.equal(item.height, 0);
});

test('die Kachel escaped den Namen im Platzhalter', () => {
  const { component } = loadLayoutPage();
  component.devices = [{id: 'dev1', name: '<script>alert(1)</script>', entities: []}];
  const card = component.buildCard({id: 'a', type: 'device', ref: 'dev1', span: '1', visible: true});
  assert.equal(card.querySelector('script'), null, 'der Name muss escaped sein');
  assert.ok(card.innerHTML.includes('&lt;script&gt;'));
});

test('optionsSheetHTML() renders category checkboxes for device items and a flow-scale select for energy_flow items, not both', () => {
  const { factory } = loadLayoutPage();
  const device = factory.optionsSheetHTML({type: 'device', visible: true, visibleCategories: ['controls']}, [], 'Gerät A', 4);
  assert.ok(device.includes('data-role="category"'));
  assert.ok(device.includes('value="controls" checked'));
  assert.ok(!device.includes('data-role="flow-scale"'));

  const flow = factory.optionsSheetHTML({type: 'energy_flow', visible: true, flowScale: 'speed'}, [], 'Energiefluss', 4);
  assert.ok(flow.includes('data-role="flow-scale"'));
  assert.ok(flow.includes('value="speed" selected'));
  assert.ok(!flow.includes('data-role="category"'));
});

test('optionsSheetHTML() renders a title field and a multi-select option per known entity for entity_group items', async () => {
  const devices = [{id: 'dev1', name: 'Gerät 1', entities: [
    {unique_id: 'ent1', name: 'Werkstatt', object_id: 'temp_werkstatt'},
    {unique_id: 'ent2', name: 'Lager', object_id: 'temp_lager'},
  ]}];
  const { component, factory } = loadLayoutPage({
    fetchImpl: async url => (url.endsWith('/api/v1/devices') ? jsonResponse(devices) : jsonResponse(LAYOUT_RESPONSE)),
  });
  await component.load(); // populates the module-level allDevices cache the sheet falls back to

  const html = factory.optionsSheetHTML({type: 'entity_group', visible: true, title: 'Sensoren', entityRefs: ['ent2']}, [], 'Sensoren', 4);
  assert.ok(html.includes('data-role="entity-group-title"'));
  assert.ok(html.includes('value="Sensoren"'));
  assert.ok(html.includes('<select multiple data-role="entity-refs"'));
  assert.ok(html.includes('Gerät 1 / Werkstatt'));
  assert.ok(html.includes('value="ent2" selected'), 'a ref already in entityRefs must render selected');
  assert.ok(!html.includes('value="ent1" selected'), 'a ref not in entityRefs must render unselected');
  assert.ok(!html.includes('data-role="category"'), 'entity_group must not render the device-tile category fieldset');
});

test('optionsSheetHTML() renders a single entity select for entity_value items, with the current ref selected', async () => {
  const devices = [{id: 'dev1', name: 'Gerät 1', entities: [
    {unique_id: 'ent1', name: 'Werkstatt', object_id: 'temp_werkstatt'},
    {unique_id: 'ent2', name: 'Lager', object_id: 'temp_lager'},
  ]}];
  const { component, factory } = loadLayoutPage({
    fetchImpl: async url => (url.endsWith('/api/v1/devices') ? jsonResponse(devices) : jsonResponse(LAYOUT_RESPONSE)),
  });
  await component.load();

  const html = factory.optionsSheetHTML({type: 'entity_value', visible: true, ref: 'ent2'}, [], 'Wert-Karte', 4);
  assert.ok(html.includes('data-role="entity-value-ref"'));
  assert.ok(html.includes('Gerät 1 / Werkstatt'));
  assert.ok(html.includes('Gerät 1 / Lager'));
  assert.ok(html.includes('value="ent2" selected'));
  assert.ok(!html.includes('value="ent1" selected'));

  const empty = factory.optionsSheetHTML({type: 'entity_value', visible: true, ref: ''}, [], 'Wert-Karte', 4);
  assert.ok(empty.includes('<option value="">Bitte wählen</option>'));
});

test('optionsSheetHTML()/energyOptionsHTML() only show the speed-reference fields for energy_flow items in "speed" scale mode', () => {
  const { factory } = loadLayoutPage();
  const width = factory.optionsSheetHTML({type: 'energy_flow', visible: true, flowScale: 'width'}, [], 'Energiefluss', 4);
  assert.ok(!width.includes('data-role="speed-reference-mode"'), 'width mode must not show the reference fields');
  assert.ok(!width.includes('data-role="speed-reference-watts"'));

  const speed = factory.optionsSheetHTML({type: 'energy_flow', visible: true, flowScale: 'speed', speedReferenceMode: 'fixed', speedReferenceWatts: 2500}, [], 'Energiefluss', 4);
  assert.ok(speed.includes('data-role="speed-reference-mode"'));
  assert.ok(speed.includes('value="fixed" selected'));
  assert.ok(speed.includes('data-role="speed-reference-watts"'));
  assert.ok(speed.includes('value="2500"'));
});


// Konfigurationsoptionen der sechs Energiegrafiken-Alternativen (Port der
// sechs-Varianten-Exploration, siehe
// docs/dashboard/overview-cards.md). Diese Tests
// decken die generische Verdrahtung (energyOptionsHTML()-Dispatch,
// defaultEnergyOptions(), applyOptionChange()-Rollen, save()-Serialisierung)
// ab, nicht jedes der 18 Felder einzeln - die Kartenlogik selbst hat eigene
// Tests in energy-band/-ring/-board/-day/-schema/-status.page.test.mjs.
test('energyOptionsHTML() renders the right panel per energy card type and nothing for other types', () => {
  const { factory } = loadLayoutPage();
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
  const { factory } = loadLayoutPage();
  assert.deepEqual(JSON.parse(JSON.stringify(factory.defaultEnergyOptions('energy_flow'))),
    {speedReferenceMode: 'relative', speedReferenceWatts: 1000, hideInactive: 'off'});
  assert.deepEqual(JSON.parse(JSON.stringify(factory.defaultEnergyOptions('energy_band'))),
    {heightReference: 'fill', scaleMode: 'linear', unit: 'auto', bundleThreshold: '0', animate: 'on'});
  assert.deepEqual(JSON.parse(JSON.stringify(factory.defaultEnergyOptions('energy_status'))),
    {beamSpan: '6000', showAdvice: 'on'});
  assert.deepEqual(JSON.parse(JSON.stringify(factory.defaultEnergyOptions('device'))), {});
});

test('ENERGY_OPTION_BY_ROLE maps a select role to its camelCase field and a checkbox role to on/off', () => {
  const { factory } = loadLayoutPage();
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

test('load() fetches layout+devices and hydrates pages', async () => {
  const layout = {
    pages: [{id: 'page-1', name: '', order: 0, groups: [{
      id: 'group-1', name: 'Dashboard',
      items: [{id: 'energy-flow', type: 'energy_flow', ref: '', span: 'full', visible: true, flow_scale: 'width'}],
    }]}],
  };
  const devices = [{id: 'dev1', name: 'Node', entities: []}];
  const { component } = loadLayoutPage({
    fetchImpl: async url => (url === '/api/v1/layout' ? jsonResponse(layout) : jsonResponse(devices)),
  });

  await component.load();

  assert.equal(component.devices.length, 1);
  assert.equal(component.pages[0].groups[0].items[0].flowScale, 'width');
  assert.equal(component.pages[0].groups[0].items[0].id, 'energy-flow');
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

  // entity_group hat keine option.id in itemOptions (siehe addFromCatalog) -
  // itemLabel() muss trotzdem etwas Sinnvolles liefern: den eigenen Titel.
  assert.equal(component.itemLabel({type: 'entity_group', id: 'entity-group-123', title: 'Sensoren'}), 'Sensoren');
  assert.equal(component.itemLabel({type: 'entity_group', id: 'entity-group-123', title: ''}), 'Entitätenliste');

  // entity_value hat nach dem Anlegen ebenfalls keine passende option.id mehr
  // (frische newID() statt 'entity-value') - der Name kommt aus dem Ref.
  assert.equal(component.itemLabel({type: 'entity_value', id: 'entity-value-123', ref: 'ent1'}), 'Gerät 1 / Leistung');
  assert.equal(component.itemLabel({type: 'entity_value', id: 'entity-value-123', ref: ''}), 'Wert-Karte');
});

// Die generische 'entity-value'-Option hat keinen Ref und darum keine
// natuerliche eindeutige Karten-ID. Jeder Aufruf legt eine neue Karte an.
test('addFromCatalog() creates a fresh id for the generic entity-value entry on every call', () => {
  const { component } = loadLayoutPage();
  component.pages = [{id: 'page-1', name: '', order: 0, groups: [{id: 'group-1', name: 'Dashboard', items: []}]}];
  const entry = {id: 'entity-value', type: 'entity_value', ref: ''};
  component.addFromCatalog(entry);
  component.addFromCatalog(entry);
  const items = component.pages[0].groups[0].items;
  assert.equal(items.length, 2);
  assert.equal(items[0].type, 'entity_value');
  assert.equal(items[1].type, 'entity_value');
  assert.notEqual(items[0].id, items[1].id);
  assert.equal(items[0].ref, '');
});

// entity_group hat keinen Ref und damit keine natuerliche eindeutige ID.
// Jeder Aufruf muss also eine eigene, neue Karte anlegen.
test('addFromCatalog() adds a new entity_group card with a fresh id on every call', () => {
  const { component } = loadLayoutPage();
  component.pages = [{id: 'page-1', name: '', order: 0, groups: [{id: 'group-1', name: 'Dashboard', items: []}]}];
  const entry = {id: 'entity-group', type: 'entity_group', ref: ''};
  component.addFromCatalog(entry);
  component.addFromCatalog(entry);
  const items = component.pages[0].groups[0].items;
  assert.equal(items.length, 2);
  assert.equal(items[0].type, 'entity_group');
  assert.equal(items[1].type, 'entity_group');
  assert.notEqual(items[0].id, items[1].id);
  assert.equal(items[0].title, 'Entitäten');
  assert.deepEqual(JSON.parse(JSON.stringify(items[0].entityRefs)), []);
  assert.equal(component.unsaved, true);
});

// applyOptionChange() schreibt aus dem Modal direkt in das Item der Kachel,
// das gerade offen ist (_optionsItem). Ein Fake-Ziel genuegt dafuer.
function optionChange(component, item, target) {
  component._optionsItem = item;
  component.applyOptionChange({target});
  return item;
}

test('applyOptionChange() updates a category on the item and ignores unknown roles', () => {
  const { component } = loadLayoutPage();
  const item = {id: 'a', type: 'device', ref: 'dev1', span: '1', visible: true, visibleCategories: []};
  optionChange(component, item, {dataset: {role: 'category'}, value: 'measurements', checked: true});
  assert.deepEqual(JSON.parse(JSON.stringify(item.visibleCategories)), ['measurements']);
  assert.equal(component.unsaved, true);

  component.unsaved = false;
  optionChange(component, item, {dataset: {}, checked: true});
  assert.equal(component.unsaved, false, 'unrelated change events are ignored');
});

test('applyOptionChange() ignores the visible role, the eye button in the chrome owns visibility', () => {
  const { component } = loadLayoutPage();
  const item = {id: 'a', type: 'device', ref: 'dev1', span: '1', visible: true};
  optionChange(component, item, {dataset: {role: 'visible'}, checked: false});
  assert.equal(item.visible, true);
  assert.equal(component.unsaved, false);
});

test('applyOptionChange() re-renders the modal body on a flow-scale change, so the speed-reference fields show/hide immediately', () => {
  const { dom, editor } = createEditorWithFragment('');
  const item = {id: 'b', type: 'energy_flow', ref: '', span: 'full', visible: true, flowScale: 'width'};
  editor.pages = [{id: 'p', name: '', order: 0, groups: [{id: 'g', name: 'D', items: [item]}]}];
  editor.devices = [];
  editor._optionsItem = item;
  editor.applyOptionChange({target: {dataset: {role: 'flow-scale'}, value: 'speed'}});

  assert.equal(item.flowScale, 'speed');
  const body = dom.window.document.querySelector('[data-modal-body]');
  assert.ok(body.innerHTML.includes('data-role="speed-reference-mode"'), 'switching to speed mode must re-render the reference fields in');
});

test('applyOptionChange() coerces a number-kind energy option to a JS number', () => {
  const { component } = loadLayoutPage();
  const item = {id: 'a', type: 'energy_flow', span: 'full', visible: true, speedReferenceWatts: 1000};
  optionChange(component, item, {dataset: {role: 'speed-reference-watts'}, value: '2500'});
  assert.equal(item.speedReferenceWatts, 2500);
  assert.equal(typeof item.speedReferenceWatts, 'number');
});

test('applyOptionChange() applies an energy-option select as its raw value and a checkbox as on/off', () => {
  const { component } = loadLayoutPage();
  const item = {id: 'a', type: 'energy_band', span: '2', visible: true, heightReference: 'fill', animate: 'on'};
  optionChange(component, item, {dataset: {role: 'height-reference'}, value: 'abs'});
  assert.equal(item.heightReference, 'abs');

  optionChange(component, item, {dataset: {role: 'animate'}, checked: false});
  assert.equal(item.animate, 'off');

  optionChange(component, item, {dataset: {role: 'animate'}, checked: true});
  assert.equal(item.animate, 'on');
});

test('applyOptionChange() updates title and entityRefs of an entity_group card', () => {
  const { component } = loadLayoutPage();
  const item = {id: 'a', type: 'entity_group', span: '1', visible: true, title: 'Sensoren', entityRefs: []};
  optionChange(component, item, {dataset: {role: 'entity-group-title'}, value: 'Neuer Titel'});
  assert.equal(item.title, 'Neuer Titel');
  assert.equal(component.unsaved, true);

  // Choices.js haelt das <select multiple> synchron, gelesen wird darum ueber
  // selectedOptions.
  const selectedOptions = values => values.map(value => ({value}));
  optionChange(component, item, {dataset: {role: 'entity-refs'}, selectedOptions: selectedOptions(['ent1'])});
  assert.deepEqual(JSON.parse(JSON.stringify(item.entityRefs)), ['ent1']);

  optionChange(component, item, {dataset: {role: 'entity-refs'}, selectedOptions: selectedOptions(['ent1', 'ent2'])});
  assert.deepEqual(JSON.parse(JSON.stringify(item.entityRefs)), ['ent1', 'ent2']);

  optionChange(component, item, {dataset: {role: 'entity-refs'}, selectedOptions: selectedOptions(['ent2'])});
  assert.deepEqual(JSON.parse(JSON.stringify(item.entityRefs)), ['ent2']);
});

test('applyOptionChange() sets ref on an entity_value card and marks it unsaved', () => {
  const { component } = loadLayoutPage();
  const item = {id: 'a', type: 'entity_value', ref: '', span: '1', visible: true};
  optionChange(component, item, {dataset: {role: 'entity-value-ref'}, value: 'ent1'});
  assert.equal(item.ref, 'ent1');
  assert.equal(component.unsaved, true);
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
    id: 'page-1', name: '', order: 0,
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
    id: 'page-1', name: '', order: 0,
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
    id: 'page-1', name: '', order: 0,
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
    id: 'page-1', name: '', order: 0,
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
    pages: [{id: 'page-1', name: '', order: 0, groups: [{
      id: 'group-1', name: 'Dashboard',
      items: [{id: 'a', type: 'energy_ring', ref: '', span: '2', visible: true, kpi: 'eigen', label_mode: 'pct', animate: 'off'}],
    }]}],
  };
  const { component } = loadLayoutPage({
    fetchImpl: async url => (url === '/api/v1/layout' ? jsonResponse(layout) : jsonResponse([])),
  });
  await component.load();
  const item = component.pages[0].groups[0].items[0];
  assert.equal(item.kpi, 'eigen');
  assert.equal(item.labelMode, 'pct');
  assert.equal(item.animate, 'off');
});

test('load() maps the server-sent speed_reference_watts number back into speedReferenceWatts', async () => {
  const layout = {
    version: 3,
    pages: [{id: 'page-1', name: '', order: 0, groups: [{
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
    pages: [{id: 'page-1', name: '', order: 0, groups: [{
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
  const { component } = loadLayoutPage();
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

test('removePage() drops the page and renumbers the remaining pages', () => {
  const { component } = loadLayoutPage();
  component.pages = [
    {id: 'page-1', name: '', order: 0, groups: [{id: 'group-1', name: 'Dashboard', items: [
      {id: 'a', type: 'entity_group', title: 'Sensoren', entityRefs: []},
    ]}]},
    {id: 'page-2', name: 'Zweite Seite', order: 1, groups: []},
  ];
  component.removePage(0);
  assert.equal(component.pages.length, 1);
  assert.equal(component.pages[0].id, 'page-2');
  assert.equal(component.pages[0].order, 0, 'remaining pages are renumbered');
});

test('columnsFor spiegelt repeat(auto-fill, minmax(18rem, 1fr)) mit gap .8rem', () => {
  const {factory} = loadLayoutPage();
  assert.equal(factory.columnsFor(1180), 3);
  assert.equal(factory.columnsFor(1872), 6);
  assert.equal(factory.columnsFor(200), 1);
});

test('columnsFor kippt genau am auto-fill-Umschaltpunkt', () => {
  const {factory} = loadLayoutPage();
  // Die vierte Spur passt ab 4 * 288 + 3 * 12,8 = 1190,4 px.
  assert.equal(factory.columnsFor(1190), 3);
  assert.equal(factory.columnsFor(1191), 4);
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

// Kleiner Helfer: das Optionen-Modal einer Kachel in ein Element haengen.
const renderWidget = (loaded, item, label, columns) => {
  const host = loaded.document.createElement('div');
  host.innerHTML = loaded.factory.optionsSheetHTML(item, [], label, columns);
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

test('das Auswahlfeld setzt die Groessenklasse am Item', async () => {
  const loaded = await loadedComponent();
  const {component, document} = loaded;
  const item = component.pages[0].groups[0].items.find(candidate => candidate.type === 'device');
  const select = document.createElement('select');
  select.dataset.role = 'span';
  select.innerHTML = '<option value="3" selected></option>';
  component._optionsItem = item;
  component.applyOptionChange({target: select});
  assert.equal(item.span, '3');
  assert.equal(component.unsaved, true);
});

test('das Zahlenfeld setzt die Zwangshoehe, leer heisst keine', async () => {
  const loaded = await loadedComponent();
  const {component, document} = loaded;
  const item = component.pages[0].groups[0].items.find(candidate => candidate.type === 'device');
  const input = document.createElement('input');
  input.dataset.role = 'height';
  input.value = '4';
  component._optionsItem = item;
  component.applyOptionChange({target: input});
  assert.equal(item.height, 4);

  input.value = '';
  component.applyOptionChange({target: input});
  assert.equal(item.height, 0);
});


test('energyOptionsHTML() zeigt das Batterie-Zeitfenster nur für die Trajektorie', () => {
  const { factory } = loadLayoutPage();

  const traj = factory.energyOptionsHTML({type: 'battery_status', display: 'trajectory', batteryWindow: '12', batteryProjectionWindow: '3'});
  assert.ok(traj.includes('data-role="battery-window"'), 'Zeitfenster-Feld fehlt');
  assert.ok(traj.includes('value="12" selected'), 'Zeitfenster steht nicht auf 12');
  assert.ok(traj.includes('data-role="battery-projection-window"'), 'Projektionsfeld fehlt');
  assert.ok(traj.includes('value="3" selected'), 'Projektion steht nicht auf 3');

  const column = factory.energyOptionsHTML({type: 'battery_status', display: 'column'});
  assert.equal(column, '', 'die Säule hat kein Zeitfenster');
});

test('das Batterie-Zeitfenster fährt über ENERGY_OPTIONS zwischen Feld und JSON hin und her', () => {
  const { factory } = loadLayoutPage();
  const byField = Object.fromEntries(factory.ENERGY_OPTIONS.map(o => [o.field, o]));
  assert.equal(byField.batteryWindow.json, 'battery_window');
  assert.equal(byField.batteryWindow.kind, 'select');
  assert.equal(byField.batteryProjectionWindow.json, 'battery_projection_window');
});

test('initEntityChoicesFor()/destroyEntityChoicesFor() create and tear down a Choices instance for the entity-refs select', () => {
  const { factory, window, document } = loadLayoutPage();
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
  // refillOptionsBody() re-renders the modal body on every
  // change, and a stale Choices instance left attached to a
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
  const { factory, document } = loadLayoutPage();
  const el = document.createElement('div');
  el.innerHTML = '<input data-role="entity-group-title">';
  document.body.appendChild(el);
  // window.Choices is undefined in this test - must not throw.
  assert.doesNotThrow(() => factory.initEntityChoicesFor('node-2', el));
});

test('der Katalog zerfaellt in drei Register statt in ein Dropdown', () => {
  const { factory } = loadLayoutPage();
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
  const { factory } = loadLayoutPage();
  const cat = factory.catalog([]);
  const entry = cat.karten.find(card => card.type === 'entity_group');
  assert.ok(entry, 'ohne Katalogeintrag ist die Entitaetenliste in der Toolbox unerreichbar');
  assert.equal(entry.title, 'Entitätenliste');
});

test('die Energiekarten heissen im Katalog durchgehend "Energie: ..."', () => {
  const { factory } = loadLayoutPage();
  const cat = factory.catalog([]);
  const flow = cat.karten.find(card => card.type === 'energy_flow');
  assert.equal(flow.title, 'Energie: Energiefluss');
});

test('die Suche geht ueber alle Register', () => {
  const { factory } = loadLayoutPage();
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
  const { component } = loadLayoutPage();
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
  editor.pages = [{id: 'p', name: '', groups: [{id: 'g', name: 'Energie', items: [
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

// --- Der Editor arbeitet auf dem echten Raster ----------------------------
// Die Uebersicht rendert die aktive Seite in einen [data-layout-page]-Traeger;
// die Fixtures bilden das ab, damit sie dieselbe DOM-Form pruefen, die
// overview.html tatsaechlich erzeugt.
const PAGE_GRID = cards => `<div class="layout-grid"><div data-layout-page="p">${cards}</div></div>`;
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

test('mount() bringt keine fremden Raster-Klassen mit', () => {
  const { dom } = createEditor(PAGE_GRID(CARD('a', 'device', '1')));
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));

  assert.equal(dom.window.document.querySelector('.grid-stack-item, .grid-stack-item-content'), null);
  assert.equal(dom.window.document.querySelector('.layout-grid').classList.contains('grid-stack'), false);
});

test('unmount() gibt die Kachel unveraendert zurueck', () => {
  const { dom } = createEditor(PAGE_GRID(CARD('a', 'device', '1')));
  const card = dom.window.document.querySelector('.layout-grid-item');
  const classesBefore = card.className;
  dom.window.document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  dom.window.document.dispatchEvent(new dom.window.CustomEvent('layout-editor:unmount'));

  assert.equal(card.className, classesBefore, 'die Klassenliste ist wieder die alte');
  assert.equal(card.querySelector('.layout-card-chrome, .layout-resize-grip'), null);
  assert.equal(card.parentElement.getAttribute('data-layout-page'), 'p', 'die Kachel haengt noch in der Seite');
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
// Fabrik der geladenen Komponente. Das Chrome ist, was dressCard() an die
// server-gerenderte Kachel haengt.
const task7 = loadLayoutPage();
const task7Factory = task7.factory;
const chromeHTML = item => {
  const card = task7.document.createElement('div');
  task7.component.dressCard(card, item);
  return card.innerHTML;
};
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
  const { applyTargetWidth } = loadLayoutPage().factory;
  // zoom statt Verschmaelerung: sonst rechnen die Container-Queries auf die
  // Editorbreite und die Vorschau loege genauso wie heute.
  const stage = applyTargetWidth(1280, 640);
  assert.equal(stage['--target-w'], '1280px');
  assert.equal(stage['--target-zoom'], '0.5');
});

test('der Hinweis nennt Breite, Spaltenzahl und Verkleinerung', () => {
  const { widthNote } = loadLayoutPage().factory;
  assert.equal(widthNote(390, 900), '390 px · 1 Spalte');
  assert.equal(widthNote(1280, 640), '1280 px · 4 Spalten · 50 % verkleinert');
});

test('Editorbreite ist die Voreinstellung und setzt nichts', () => {
  const { applyTargetWidth, widthNote } = loadLayoutPage().factory;
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
  const { applyTargetWidth } = loadLayoutPage().factory;
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
  built.editor.activePage = `p0`;
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
  dom.window.document.addEventListener('layout-pages-changed', event => { announced = event.detail.pages; });
  editor.openPageOptions();

  const input = dom.window.document.querySelector('[data-page-name]');
  input.value = 'Keller';
  input.dispatchEvent(new dom.window.Event('input', {bubbles: true}));

  assert.equal(editor.pages[0].name, 'Keller');
  assert.equal(editor.activePage, 'p0', 'die aktive Seite ist weiterhin die ID');
  assert.ok(announced && announced.length === 2);
  assert.equal(announced[0].id, 'p0');
  assert.equal(announced[0].name, 'Keller');
  assert.equal(announced[1].id, 'p1');
  assert.equal(announced[1].name, 'Werkstatt');
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

test('eine Seite mit leerem Name zeigt "Übersicht" im Rename-Feld und Speichern des defaults haelt name leer', () => {
  // This tests the fix: a page with empty name should show the default name
  // in the input, and submitting that default value should keep the name empty.
  const { dom, editor } = editorWithPages(['']);
  editor.pages[0].name = ''; // Explicitly set empty name

  // Mock I18n for this test
  dom.window.I18n = {
    t: (key) => key === 'overview.page.default_name' ? 'Übersicht' : key,
  };

  editor.openPageOptions();
  const input = dom.window.document.querySelector('[data-page-name]');
  assert.equal(input.value, 'Übersicht', 'leerer name zeigt default im feld');

  // Typing the default name and submitting should keep it empty
  input.value = 'Übersicht';
  input.dispatchEvent(new dom.window.Event('input', {bubbles: true}));
  assert.equal(editor.pages[0].name, '', 'submitting default name haelt name leer');
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
  assert.equal(editor.activePage, 'p1');
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
  assert.equal(editor.activePage, editor.pages[1].id);
  const host = dom.window.document.querySelector('[data-layout-page]');
  assert.equal(host.dataset.layoutPage, editor.pages[1].id);
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
    {id: 'p', name: 'Zuhause', groups: [{id: 'g1', items: [{id: 'a', type: 'device', ref: 'x', span: '1', visible: true}]}]},
    {id: 'p2', name: 'Keller', groups: [{id: 'g2', items: []}]},
  ];
  dom.window.document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  assert.equal(editor.activePage, 'p');

  // Der Seitenwechsel: die Tab-Leiste waehlt, der Server liefert das
  // Fragment, danach montiert die Huelle den Editor neu.
  dom.window.__dashboardShell__ = {activePage: 'p2'};
  dom.window.document.querySelector('[data-layout-page]').dataset.layoutPage = 'p2';
  dom.window.document.dispatchEvent(new dom.window.CustomEvent('layout-editor:unmount'));
  dom.window.document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  assert.equal(editor.activePage, 'p2');

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
  editor.activePage = 'p1';
  editor.setToolbox(true);
  assert.equal(dom.window.document.querySelector('[data-tb-target]').textContent, 'Klicken oder ziehen, der Baustein landet auf Keller.');
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

  editor.activePage = 'p1';
  const first = editor.addFromCatalog({id: 'diagnostics', type: 'diagnostics', title: 'Diagnosen'});
  editor.activePage = 'p2';
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
  assert.equal(host.dataset.layoutPage, editor.pages[0].id);
  assert.equal(dom.window.document.querySelector('.panel-empty'), null);

  editor.addFromCatalog({id: 'diagnostics', type: 'diagnostics', title: 'Diagnosen'});
  assert.equal(host.children.length, 1);
});

test('cardKey() bildet die kompakte Geraetekachel auf ihren Varianteneintrag ab', () => {
  const { factory } = loadLayoutPage();
  assert.equal(factory.cardKey({type: 'device', display: 'compact'}), 'device:compact');
  assert.equal(factory.cardKey({type: 'device', display: 'detail'}), 'device');
  assert.equal(factory.cardKey({type: 'device'}), 'device');
  assert.equal(factory.cardKey({type: 'energy_ring', display: 'compact'}), 'energy_ring');
});

test('das Optionen-Modal einer Geraetekachel bietet die Darstellung an und blendet die Kategorien im Kompaktmodus aus', () => {
  const { factory } = loadLayoutPage();
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

test('die kompakte Geraetekachel startet flacher als die Detailkachel', async () => {
  const { component } = await loadedComponent();
  const compact = component.buildCard({id: 'a', type: 'device', ref: 'dev1', display: 'compact'});
  const detail = component.buildCard({id: 'b', type: 'device', ref: 'dev1', display: 'detail'});
  assert.equal(compact.style.getPropertyValue('--card-min-height'), '7rem');
  assert.equal(detail.style.getPropertyValue('--card-min-height'), '10rem');
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
  const { factory } = loadLayoutPage();
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
  const { factory } = loadLayoutPage();
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
  const { factory } = loadLayoutPage();
  assert.equal(factory.cardKey({ type: 'battery_status', display: 'trajectory' }), 'battery_status:trajectory');
  assert.equal(factory.cardKey({ type: 'battery_status', display: 'column' }), 'battery_status');
  assert.equal(factory.cardKey({ type: 'battery_status' }), 'battery_status');
  assert.equal(factory.cardKey({ type: 'device', display: 'compact' }), 'device:compact');
});

test('das Optionen-Modal der Kompaktkachel bietet die Werteauswahl des eigenen Geraets', () => {
  const { factory } = loadLayoutPage();
  const devices = [
    {id: 'dev1', name: 'Gerät 1', entities: [
      {unique_id: 'dev1_power', name: 'Leistung', object_id: 'power'},
      {unique_id: 'dev1_temp', name: 'Temperatur', object_id: 'temp'},
    ]},
    {id: 'dev2', name: 'Gerät 2', entities: [{unique_id: 'dev2_x', name: 'X', object_id: 'x'}]},
  ];

  const compact = factory.optionsSheetHTML(
    {id: 'a', type: 'device', ref: 'dev1', span: '1', visible: true, display: 'compact', entityRefs: ['dev1_temp'], visibleCategories: []},
    devices, 'Gerät 1', 4);
  assert.match(compact, /data-role="entity-refs"/);
  assert.match(compact, /<option value="dev1_temp" selected/);
  assert.match(compact, /<option value="dev1_power"(?! selected)/);
  assert.equal(/value="dev2_x"/.test(compact), false, 'Entitaeten fremder Geraete stehen in der Auswahl');

  const detail = factory.optionsSheetHTML(
    {id: 'a', type: 'device', ref: 'dev1', span: '1', visible: true, display: 'detail', entityRefs: [], visibleCategories: []},
    devices, 'Gerät 1', 4);
  assert.equal(/data-role="entity-refs"/.test(detail), false, 'die Detailkachel zeigt die Werteauswahl');
});

test('applyOptionChange: die Werteauswahl der Kompaktkachel wird auf drei gedeckelt', () => {
  const { component } = loadLayoutPage();
  component._optionsItem = {id: 'a', type: 'device', ref: 'dev1', display: 'compact', entityRefs: []};
  const select = {dataset: {role: 'entity-refs'}, selectedOptions: [
    {value: 'e1'}, {value: 'e2'}, {value: 'e3'}, {value: 'e4'},
  ]};
  component.applyOptionChange({target: select});
  // JSON-Roundtrip zieht das Array aus dem vm-Realm in den Testrealm - sonst
  // scheitert deepEqual an der Prototyp-Identitaet (wie an den Nachbartests).
  assert.deepEqual(JSON.parse(JSON.stringify(component._optionsItem.entityRefs)), ['e1', 'e2', 'e3']);
});

test('applyOptionChange: der Wechsel auf Detail und der Geraetewechsel leeren die Werteauswahl', () => {
  const { component } = loadLayoutPage();
  component.devices = [{id: 'dev1', name: 'G1', entities: []}, {id: 'dev2', name: 'G2', entities: []}];
  component._optionsItem = {id: 'a', type: 'device', ref: 'dev1', display: 'compact', entityRefs: ['e1', 'e2']};
  component._optionsSlot = null;

  component.applyOptionChange({target: {dataset: {role: 'display'}, value: 'detail'}});
  assert.deepEqual(JSON.parse(JSON.stringify(component._optionsItem.entityRefs)), []);

  component._optionsItem.display = 'compact';
  component._optionsItem.entityRefs = ['e1'];
  component.applyOptionChange({target: {dataset: {role: 'device-ref'}, value: 'dev2'}});
  assert.deepEqual(JSON.parse(JSON.stringify(component._optionsItem.entityRefs)), []);
});

test('catalog() resolves card titles per language', () => {
  // Create a DOM with German catalog
  const domDE = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const contextDE = domDE.getInternalVMContext();
  installI18n(domDE.window, { lang: 'de' });
  let factoryDE;
  domDE.window.Alpine = { data: (_name, fn) => { factoryDE = fn; } };
  domDE.window.fetch = async () => { throw new Error('fetch should not be called'); };
  vm.runInContext(scriptSource, contextDE);

  // Create a DOM with English catalog
  const domEN = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const contextEN = domEN.getInternalVMContext();
  installI18n(domEN.window, { lang: 'en' });
  let factoryEN;
  domEN.window.Alpine = { data: (_name, fn) => { factoryEN = fn; } };
  domEN.window.fetch = async () => { throw new Error('fetch should not be called'); };
  vm.runInContext(scriptSource, contextEN);

  // Get catalogs from both contexts
  const catDE = factoryDE.catalog([]);
  const catEN = factoryEN.catalog([]);

  // Check energy_band card title in both languages
  const energyBandDE = catDE.karten.find(k => k.type === 'energy_band');
  const energyBandEN = catEN.karten.find(k => k.type === 'energy_band');

  // Verify titles match the loaded catalog values
  const deCatalogValue = domDE.window.I18n.t('layout_editor.card.energy_band.title');
  const enCatalogValue = domEN.window.I18n.t('layout_editor.card.energy_band.title');

  assert.equal(energyBandDE.title, deCatalogValue, 'German catalog entry title should equal de catalog value');
  assert.equal(energyBandEN.title, enCatalogValue, 'English catalog entry title should equal en catalog value');
});

test('toolbox renders .layout-toolbox-item buttons with data-add and correct title', () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(CARD('a', 'device', '1')));
  editor.cardTypes = CARD_TYPES;
  editor.pages = [{id: 'p', name: 'Zuhause', groups: [{id: 'g', items: []}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  editor.setToolbox(true);

  // Find toolbox item with data-add attribute
  const entry = dom.window.document.querySelector('[data-tb-list] .layout-toolbox-item[data-add="0"]');
  assert(entry, 'should have .layout-toolbox-item[data-add="0"] button');
  
  // Check that the title is rendered
  const title = entry.querySelector('b');
  assert(title, 'should have <b> with title');
  assert(title.textContent.length > 0, 'title should not be empty');
});

test('toolbox renders .layout-toolbox-empty for no matching search', () => {
  const { dom, editor } = createEditorWithFragment(PAGE_GRID(CARD('a', 'device', '1')));
  editor.cardTypes = CARD_TYPES;
  editor.pages = [{id: 'p', name: 'Zuhause', groups: [{id: 'g', items: []}]}];
  document.dispatchEvent(new dom.window.CustomEvent('layout-editor:mount'));
  editor.setToolbox(true);

  // Simulate search with no results
  const search = dom.window.document.querySelector('[data-tb-search]');
  if (search) {
    search.value = 'xyz_no_match_xyz';
    search.dispatchEvent(new dom.window.Event('input'));
  }

  // Find empty state message
  const empty = dom.window.document.querySelector('[data-tb-list] .layout-toolbox-empty');
  assert(empty, 'should show .layout-toolbox-empty when no results match');
  assert(empty.textContent.includes('xyz_no_match_xyz'), 'empty message should include query');
});

// Helper for JSON round-trip
const plain = value => JSON.parse(JSON.stringify(value));

const VIEWS = [
  {id: 'v1', name: 'PV', series: ['role:pv'], range_hours: 6, aggregate: 'max'},
  {id: 'v2', name: 'Fest', series: ['role:grid'], range_hours: 6, range_mode: 'custom', range_from: 0, range_to: 30 * 3600 * 1000, aggregate: 'min'},
];

async function editorWithHistoryTile(item) {
  const layout = {version: 3, card_types: {history_view: {min_span: 1, min_width: '16rem', min_height: '14rem', fills_height: true, default_span: '2'}},
    pages: [{id: 'p', name: '', order: 0, groups: [{id: 'g', name: 'Dashboard', items: [item]}]}]};
  const fetchImpl = async url => jsonResponse(
    url.endsWith('/api/v1/devices') ? [] : url.endsWith('/api/v1/settings') ? {history_views: VIEWS} : layout);
  const loaded = loadLayoutPage({fetchImpl});
  await loaded.component.load();
  return loaded;
}

test('the catalog offers a history_view card with its own thumbnail', () => {
  const {factory} = loadLayoutPage();
  assert.ok(factory.THUMB.history_view.includes('var(--flow-pv)'));
  assert.ok(factory.THUMB.history_view.includes('var(--flow-load)'));
});

test('load() maps history fields and fetches the saved views once', async () => {
  const {component} = await editorWithHistoryTile({id: 'h', type: 'history_view', ref: '', span: '2', visible: true, title: 'Netz', history_series: ['role:grid'], history_range_hours: '168', history_aggregate: 'min'});
  const item = component.pages[0].groups[0].items[0];
  assert.deepEqual(plain(item.historySeries), ['role:grid']);
  assert.equal(item.historyRangeHours, '168');
  assert.equal(item.historyAggregate, 'min');
  assert.equal(component.itemLabel(item), 'Netz');
});

test('itemLabel of a bound tile is the view name', async () => {
  const {component} = await editorWithHistoryTile({id: 'h', type: 'history_view', ref: 'v1', span: '2', visible: true});
  assert.equal(component.itemLabel(component.pages[0].groups[0].items[0]), 'PV');
});

test('the source picker lists the saved views and own settings', async () => {
  const {factory, component} = await editorWithHistoryTile({id: 'h', type: 'history_view', ref: 'v1', span: '2', visible: true});
  const html = factory.optionsSheetHTML(component.pages[0].groups[0].items[0], [], 'PV', 2);
  assert.match(html, /data-role="history-source"/);
  assert.match(html, /<option value="v1" selected>PV<\/option>/);
  assert.match(html, /<option value="">/);
  assert.doesNotMatch(html, /data-role="history-range-hours"/, 'gebunden: keine eigenen Felder');
});

test('deleted view stays selectable as "Gelöschte Sicht"', async () => {
  const {factory, component, window} = await editorWithHistoryTile({id: 'h', type: 'history_view', ref: 'weg', span: '2', visible: true});
  const html = factory.optionsSheetHTML(component.pages[0].groups[0].items[0], [], 'x', 2);
  assert.ok(html.includes(`<option value="weg" selected>${window.I18n.t('layout_editor.history_view.deleted')}</option>`));
});

test('switching from a view to own settings copies series, range and aggregate', async () => {
  const {component} = await editorWithHistoryTile({id: 'h', type: 'history_view', ref: 'v1', span: '2', visible: true});
  const item = component.pages[0].groups[0].items[0];
  component._optionsItem = item;
  component.applyOptionChange({target: {dataset: {role: 'history-source'}, value: ''}});
  assert.equal(item.ref, '');
  assert.deepEqual(plain(item.historySeries), ['role:pv']);
  assert.equal(item.historyRangeHours, '6');
  assert.equal(item.historyAggregate, 'max');
  assert.equal(component.unsaved, true);
});

test('nearestPreset rounds a fixed range up to the next preset, at most 2160 h', () => {
  const {factory} = loadLayoutPage();
  assert.equal(factory.nearestPreset({range_mode: 'custom', range_from: 0, range_to: 30 * 3600 * 1000, range_hours: 6}), '168');
  assert.equal(factory.nearestPreset({range_mode: 'custom', range_from: 0, range_to: 90 * 24 * 3600 * 1000, range_hours: 6}), '2160');
  assert.equal(factory.nearestPreset({range_mode: 'custom', range_from: 0, range_to: 120 * 24 * 3600 * 1000, range_hours: 6}), '2160');
  assert.equal(factory.nearestPreset({range_mode: 'relative', range_hours: 24}), '24');
  assert.equal(factory.nearestPreset({range_hours: 2}), '6');
});

test('switching to a view clears the own fields', async () => {
  const {component} = await editorWithHistoryTile({id: 'h', type: 'history_view', ref: '', span: '2', visible: true, history_series: ['role:pv'], history_range_hours: '6', history_aggregate: 'max'});
  const item = component.pages[0].groups[0].items[0];
  component._optionsItem = item;
  component.applyOptionChange({target: {dataset: {role: 'history-source'}, value: 'v2'}});
  assert.equal(item.ref, 'v2');
  assert.deepEqual(plain(item.historySeries), []);
  assert.equal(item.historyRangeHours, '');
  assert.equal(item.historyAggregate, '');
});

test('the series chooser updates historySeries in Choices order', async () => {
  const loaded = await editorWithHistoryTile({id: 'h', type: 'history_view', ref: '', span: '2', visible: true, history_series: ['role:pv']});
  const {component, factory, window} = loaded;
  const item = component.pages[0].groups[0].items[0];
  component._optionsItem = item;
  // Ohne Choices-Instanz zaehlt die Reihenfolge der <option>s.
  component.applyOptionChange({target: {dataset: {role: 'history-series'}, selectedOptions: [{value: 'role:grid'}]}});
  assert.deepEqual(plain(item.historySeries), ['role:grid']);

  window.Choices = class { getValue() { return ['role:grid', 'role:pv']; } destroy() {} };
  const body = window.document.createElement('div');
  body.innerHTML = '<select multiple data-role="history-series"></select>';
  factory.initEntityChoicesFor('h', body);
  component.applyOptionChange({target: {dataset: {role: 'history-series'}, selectedOptions: [{value: 'role:pv'}, {value: 'role:grid'}]}});
  assert.deepEqual(plain(item.historySeries), ['role:grid', 'role:pv']);
  factory.destroyEntityChoicesFor('h');
});

test('the series chooser lists chosen series first, in their order', async () => {
  const {component, factory} = await editorWithHistoryTile({id: 'h', type: 'history_view', ref: '', span: '2', visible: true, history_series: ['role:pv', 'role:grid']});
  const item = component.pages[0].groups[0].items[0];
  const html = factory.optionsSheetHTML(item, [], 'Verlauf', 4);
  assert.ok(html.includes('data-role="history-series"'), 'Serien-Auswahl fehlt');
  assert.ok(!html.includes('type="checkbox" role="switch" data-role="history-series"'), 'keine Schalter mehr');
  const pv = html.indexOf('value="role:pv" selected');
  const grid = html.indexOf('value="role:grid" selected');
  const derived = html.indexOf('value="berechnet:hausverbrauch"');
  assert.ok(pv >= 0 && grid > pv && derived > grid, 'Reihenfolge stimmt nicht');
  assert.ok(!html.includes('value="berechnet:hausverbrauch" selected'));
});

test('payload() writes history_series and the two option fields in snake_case', async () => {
  const {component} = await editorWithHistoryTile({id: 'h', type: 'history_view', ref: '', span: '2', visible: true, title: 'Netz', history_series: ['role:grid'], history_range_hours: '168', history_aggregate: 'min'});
  const item = component.payload().pages[0].groups[0].items[0];
  assert.deepEqual(plain(item.history_series), ['role:grid']);
  assert.equal(item.history_range_hours, '168');
  assert.equal(item.history_aggregate, 'min');
  assert.equal(item.title, 'Netz');
  assert.equal(item.historySeries, undefined, 'kein camelCase im PUT-Rumpf');
});

test('energyOptionsHTML() zeigt Zeitraum, Kennzahlen und Diagrammschalter fuer energy_stats', () => {
  const { factory } = loadLayoutPage();
  const html = factory.energyOptionsHTML({type: 'energy_stats', statsPeriod: 'month', statsKpis: ['autarky', 'pv'], statsChart: 'on'});
  assert.ok(html.includes('data-role="stats-period"'), 'Zeitraum fehlt');
  assert.ok(html.includes('value="month" selected'), 'Zeitraum steht nicht auf month');
  assert.ok(html.includes('data-role="stats-kpis"'), 'Kennzahlen fehlen');
  // Gewaehlte zuerst, in gewaehlter Reihenfolge.
  const autarky = html.indexOf('value="autarky" selected');
  const pv = html.indexOf('value="pv" selected');
  const consumption = html.indexOf('value="consumption"');
  assert.ok(autarky >= 0 && pv > autarky && consumption > pv, 'Reihenfolge der Kennzahlen stimmt nicht');
  assert.ok(!html.includes('value="consumption" selected'), 'consumption ist nicht gewaehlt');
  assert.match(html, /data-role="stats-chart" checked/);

  const fresh = factory.energyOptionsHTML({type: 'energy_stats'});
  assert.ok(fresh.includes('value="" selected'), 'ohne Angabe gilt der zuletzt gewaehlte Zeitraum');
  for (const id of ['consumption', 'pv', 'grid_import', 'grid_export', 'autarky']) assert.ok(fresh.includes(`value="${id}" selected`), `${id} ist vorgewaehlt`);
  assert.ok(!fresh.includes('value="battery_charge" selected'));
  assert.doesNotMatch(fresh, /data-role="stats-chart" checked/);
});

test('cardKey() nimmt mit Diagramm die Variante energy_stats:chart', () => {
  const { factory } = loadLayoutPage();
  assert.equal(factory.cardKey({type: 'energy_stats', statsChart: 'on'}), 'energy_stats:chart');
  assert.equal(factory.cardKey({type: 'energy_stats', statsChart: 'off'}), 'energy_stats');
});

test('load() und payload() fuehren die energy_stats-Optionen hin und zurueck', async () => {
  const {component} = await editorWithHistoryTile({id: 's', type: 'energy_stats', span: '1', visible: true, stats_period: 'week', stats_kpis: ['autarky', 'pv'], stats_chart: 'on'});
  const item = component.pages[0].groups[0].items[0];
  assert.equal(item.statsPeriod, 'week');
  assert.deepEqual(plain(item.statsKpis), ['autarky', 'pv']);
  assert.equal(item.statsChart, 'on');
  const sent = component.payload().pages[0].groups[0].items[0];
  assert.equal(sent.stats_period, 'week');
  assert.deepEqual(plain(sent.stats_kpis), ['autarky', 'pv']);
  assert.equal(sent.stats_chart, 'on');
  assert.equal(sent.statsKpis, undefined, 'kein camelCase im PUT-Rumpf');
});

test('stats-kpis nimmt die Reihenfolge aus Choices, stats-chart setzt den Schalter', async () => {
  const loaded = await editorWithHistoryTile({id: 's', type: 'energy_stats', span: '1', visible: true});
  const {component, factory, window} = loaded;
  const item = component.pages[0].groups[0].items[0];
  component._optionsItem = item;
  // Ohne Choices-Instanz zaehlt die Reihenfolge der <option>s.
  component.applyOptionChange({target: {dataset: {role: 'stats-kpis'}, selectedOptions: [{value: 'pv'}, {value: 'autarky'}]}});
  assert.deepEqual(plain(item.statsKpis), ['pv', 'autarky']);

  window.Choices = class { getValue() { return ['autarky', 'pv']; } destroy() {} };
  const body = window.document.createElement('div');
  body.innerHTML = '<select multiple data-role="stats-kpis"></select>';
  factory.initEntityChoicesFor('s', body);
  component.applyOptionChange({target: {dataset: {role: 'stats-kpis'}, selectedOptions: [{value: 'pv'}, {value: 'autarky'}]}});
  assert.deepEqual(plain(item.statsKpis), ['autarky', 'pv']);
  factory.destroyEntityChoicesFor('s');

  component.applyOptionChange({target: {dataset: {role: 'stats-chart'}, checked: true}});
  assert.equal(item.statsChart, 'on');
});

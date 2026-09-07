// A1: das Uebersichts-Fragment hat genau einen Besitzer. Vorher luden
// publishRegistryUpdate() *und* der registry-updated-Zuhoerer in
// devicesPanel() dasselbe Fragment - gemessen 15 Paare in 96,6 s, kein
// einziger Einzelabruf. Zusaetzlich: die Re-Entry-Waechter, damit ein
// zweiter init()-Aufruf keinen zweiten Zuhoerer und kein zweites Intervall
// hinterlaesst.
//
// dashboard.js registriert seine Komponenten erst im alpine:init-Event.
// Der Test stellt ein Alpine-Doppel bereit und loest das Event aus; so
// braucht der Produktivcode keine Test-Naht (gleiche Technik wie
// dashboard.nav.test.mjs und automations.page.test.mjs).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { attachStores } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');
const entitySource = read('entity-values.js');
const tileSource = read('device-tile.js');
const valuesSource = read('overview-values.js');
const tileValuesSource = read('device-tile-values.js');
const compactValuesSource = read('compact-card-values.js');
const source = read('dashboard.js');

function load({ html = '<div id="overview-panel" class="panel active"></div>' } = {}) {
  const dom = new JSDOM(`<!doctype html><html><body>${html}</body></html>`, { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, directive: () => {} };
  // dashboard.js startet beim Laden ein Modul-Level-setInterval (die
  // Zeitstempel-Aktualisierung) - ein echtes window.setInterval wuerde den
  // Testprozess sonst am Leben halten, gleiche Loesung wie in
  // dashboard.nav.test.mjs.
  const intervals = [];
  dom.window.setInterval = () => intervals.push(1);
  dom.window.fetch = async () => ({ ok: true, status: 200, json: async () => ({}) });
  // Set visibilityState to 'visible' so guards in components like refreshLiveFragment
  // don't return early during testing.
  Object.defineProperty(dom.window.document, 'visibilityState', { value: 'visible', writable: true });
  vm.runInContext(entitySource, context);
  vm.runInContext(tileSource, context);
  vm.runInContext(valuesSource, context);
  vm.runInContext(tileValuesSource, context);
  vm.runInContext(compactValuesSource, context);
  vm.runInContext(source, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  return { factories, window: dom.window, intervals };
}

test('publishRegistryUpdate verschickt registry-updated', () => {
  const { factories, window } = load();
  const shell = factories.dashboardShell();
  shell.activePanel = 'overview-panel';
  let received = null;
  window.addEventListener('registry-updated', event => { received = event.detail; });
  shell.publishRegistryUpdate({ source: 'event-stream' });
  assert.deepEqual(received, { source: 'event-stream' });
});

test('publishRegistryUpdate laedt das Fragment nicht selbst', () => {
  const { factories, window } = load();
  const calls = [];
  window.htmx = { ajax: (...args) => { calls.push(args); return Promise.resolve(); } };
  const shell = factories.dashboardShell();
  shell.activePanel = 'overview-panel';
  shell.publishRegistryUpdate({});
  assert.deepEqual(calls, []);
});

test('refreshLivePanel gibt es nicht mehr', () => {
  const { factories } = load();
  assert.equal(factories.dashboardShell().refreshLivePanel, undefined);
});

test('ein zweiter init() auf runtimeStatusPanel legt kein zweites Intervall an', () => {
  const { factories, window } = load({ html: '<div id="runtime-status" data-runtime-status-enabled="true"></div>' });
  const panel = factories.runtimeStatusPanel();
  panel.$root = window.document.getElementById('runtime-status');
  attachStores(panel);
  let started = 0;
  window.setInterval = () => { started += 1; return started; };
  panel.init();
  panel.init();
  assert.equal(started, 1);
});

test('ein zweiter init() auf devicesPanel registriert keinen zweiten Zuhoerer', () => {
  const { factories, window } = load({ html: '<div id="overview-panel" class="panel active" data-device-view-mode="control"></div>' });
  const panel = factories.devicesPanel();
  panel.$root = window.document.getElementById('overview-panel');
  panel.$refs = {};
  attachStores(panel);
  let registryListeners = 0;
  const addEventListener = window.addEventListener.bind(window);
  window.addEventListener = (type, handler, options) => {
    if (type === 'registry-updated') registryListeners += 1;
    return addEventListener(type, handler, options);
  };
  panel.init();
  panel.init();
  assert.equal(registryListeners, 1);
});

const overviewHtml = '<div id="overview-panel" class="panel active"></div><div id="overview-live"></div>';

// Die Karten in #overview-live bekommen ihre Hoehe erst per JavaScript.
// Waehrend des outerHTML-Tausches ist das Dokument darum kurz rund 880px
// flacher (gemessen 4344px -> 3461px), der Browser klemmt scrollTop auf das
// kleinere Maximum, und die Scrollposition ist weg. Der Panelknoten bleibt
// beim Tausch stehen - haelt er seine Hoehe, gibt es nichts zu klemmen.
test('refreshLiveFragment haelt die Hoehe des Panels ueber den Tausch fest', async () => {
  const { factories, window } = load({ html: overviewHtml });
  const panel = factories.devicesPanel();
  const root = window.document.getElementById('overview-panel');
  panel.$root = root;
  root.getBoundingClientRect = () => ({ height: 4344 });
  let pinnedDuringSwap = null;
  window.htmx = { ajax: async () => { pinnedDuringSwap = root.style.minHeight; } };
  await panel.refreshLiveFragment();
  assert.equal(pinnedDuringSwap, '4344px');
  assert.equal(root.style.minHeight, '');
});

// Bleibt die Stuetze nach einem Fehler stehen, waechst die Seite bei jedem
// weiteren Versuch weiter und laesst sich nie wieder verkleinern.
test('refreshLiveFragment gibt die Hoehe auch frei, wenn der Tausch scheitert', async () => {
  const { factories, window } = load({ html: overviewHtml });
  const panel = factories.devicesPanel();
  const root = window.document.getElementById('overview-panel');
  panel.$root = root;
  root.getBoundingClientRect = () => ({ height: 4344 });
  window.htmx = { ajax: async () => { throw new Error('Netzwerk weg'); } };
  await assert.rejects(() => panel.refreshLiveFragment(), /Netzwerk weg/);
  assert.equal(root.style.minHeight, '');
});

// Die Uebersicht rendert genau eine Layout-Seite (activePage in webui.go).
// Ohne page= in der Anfrage laeuft jede Auffrischung auf die erste Seite
// zurueck - ein Klick auf einen Seiten-Tab zeigte dann weiter dieselbe Seite.
test('refreshLiveFragment fragt die aktive Layout-Seite mit an', async () => {
  const { factories, window } = load({ html: overviewHtml });
  const panel = factories.devicesPanel();
  const root = window.document.getElementById('overview-panel');
  panel.$root = root;
  root.getBoundingClientRect = () => ({ height: 100 });
  window.__dashboardShell__ = { activePage: 'Werkstatt & Hof' };
  let requested = '';
  window.htmx = { ajax: async (_method, url) => { requested = url; } };
  await panel.refreshLiveFragment();
  assert.match(requested, /[?&]page=Werkstatt%20%26%20Hof/);
  window.__dashboardShell__ = { activePage: '' };
  await panel.refreshLiveFragment();
  assert.doesNotMatch(requested, /page=/);
});

// Der frueher noetige Reparaturaufruf darf nicht zurueckkommen: er liess den
// Sprung sichtbar, verwarf jedes Scrollen waehrend der Anfrage und brach auf
// dem Handy den Touch-Schwung ab.
test('refreshLiveFragment fasst die Scrollposition nicht mehr an', async () => {
  const { factories, window } = load({ html: overviewHtml });
  const panel = factories.devicesPanel();
  const root = window.document.getElementById('overview-panel');
  panel.$root = root;
  root.getBoundingClientRect = () => ({ height: 4344 });
  const scrolled = [];
  window.scrollTo = (x, y) => scrolled.push(y);
  window.htmx = { ajax: async () => {} };
  await panel.refreshLiveFragment();
  assert.deepEqual(scrolled, []);
});

// Der Schnappschuss reist im SSE-Ereignis mit. Kommt er an, brauchen die
// Energiekarten dafuer keinen eigenen Abruf und keinen Neuaufbau.
test('registry-updated speist einen mitgelieferten Schnappschuss ein', async () => {
  const { factories, window } = load({ html: overviewHtml });
  const panel = factories.devicesPanel();
  const root = window.document.getElementById('overview-panel');
  panel.$root = root;
  panel.$refs = {};
  root.getBoundingClientRect = () => ({ height: 100 });
  const published = [];
  window.EnergyPresentation = { publish: raw => published.push(raw) };
  window.htmx = { ajax: async () => {} };
  panel.init();

  window.dispatchEvent(new window.CustomEvent('registry-updated', {
    detail: { version: 5, energy: { values: { pv: 1234 } } },
  }));
  await new Promise(resolve => setTimeout(resolve, 0));

  assert.equal(published.length, 1);
  assert.equal(published[0].values.pv, 1234);
});

// Faellt SSE aus, laeuft der 30s-Rueckfall ohne Schnappschuss. Dann darf
// nichts eingespeist werden - sonst stuenden die Karten auf undefined.
test('registry-updated ohne Schnappschuss speist nichts ein', async () => {
  const { factories, window } = load({ html: overviewHtml });
  const panel = factories.devicesPanel();
  const root = window.document.getElementById('overview-panel');
  panel.$root = root;
  panel.$refs = {};
  root.getBoundingClientRect = () => ({ height: 100 });
  const published = [];
  window.EnergyPresentation = { publish: raw => published.push(raw) };
  window.htmx = { ajax: async () => {} };
  panel.init();

  window.dispatchEvent(new window.CustomEvent('registry-updated', {detail: {source: 'fallback'}}));
  await new Promise(resolve => setTimeout(resolve, 0));

  assert.deepEqual(published, []);
});

const energyOnlyHtml = '<div id="overview-panel" class="panel active">'
  + '<div id="overview-live"><div class="layout-grid">'
  + '<div data-layout-item-kind="energy_ring"></div>'
  + '<div data-layout-item-kind="energy_band"></div>'
  + '</div></div></div>';

const mixedHtml = '<div id="overview-panel" class="panel active">'
  + '<div id="overview-live"><div class="layout-grid">'
  + '<div data-layout-item-kind="energy_ring"></div>'
  + '<div data-layout-item-kind="device"></div>'
  + '</div></div></div>';

function armed(html) {
  const { factories, window } = load({ html });
  const panel = factories.devicesPanel();
  const root = window.document.getElementById('overview-panel');
  panel.$root = root;
  panel.$refs = {};
  root.getBoundingClientRect = () => ({ height: 100 });
  const swaps = [];
  window.EnergyPresentation = { publish: () => {} };
  window.htmx = { ajax: async () => { swaps.push(1); } };
  panel.init();
  return { panel, window, swaps };
}

const snapshotEvent = window => new window.CustomEvent('registry-updated', {
  detail: { version: 5, energy: { values: { pv: 1 } } },
});

// Der eigentliche Gewinn: stehen nur Energiekarten im Raster, traegt der
// Push die Aktualisierung allein. Gemessen sind das 62 % der Tab-CPU.
test('kein Fragment-Tausch, wenn nur Energiekarten im Raster stehen', async () => {
  const { window, swaps } = armed(energyOnlyHtml);
  window.dispatchEvent(snapshotEvent(window));
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.deepEqual(swaps, []);
});

// Geraete- und Entitaetskarten holen ihre Zahlen weiter aus dem Server-HTML.
// Fuer sie muss der Tausch bleiben, bis Stufe 3 sie abloest.
test('Fragment-Tausch bleibt, sobald eine Nicht-Energiekarte im Raster steht', async () => {
  const { window, swaps } = armed(mixedHtml);
  window.dispatchEvent(snapshotEvent(window));
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(swaps.length, 1);
});

// Ohne Schnappschuss (SSE weg, 30s-Rueckfall) ist der Tausch der einzige Weg.
test('Fragment-Tausch bleibt, wenn das Ereignis keinen Schnappschuss traegt', async () => {
  const { window, swaps } = armed(energyOnlyHtml);
  window.dispatchEvent(new window.CustomEvent('registry-updated', {detail: {source: 'fallback'}}));
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(swaps.length, 1);
});

// Ohne diesen Weg wuerde ein gespeichertes Layout aus lauter Energiekarten
// nie sichtbar: der Tausch, der es frueher beilaeufig mitgebracht hat, faellt
// ja gerade weg.
test('layout-saved erzwingt einen Tausch, auch bei reinem Energieraster', async () => {
  const { window, swaps } = armed(energyOnlyHtml);
  window.dispatchEvent(new window.CustomEvent('layout-saved'));
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(swaps.length, 1);
});

// Ein Raster aus Karten, die der Push bedient, darf nicht mehr getauscht
// werden - das ist die ganze Stufe 3 in einem Test.
const gridHTML = kinds => `<div id="overview-panel" class="panel active"><div id="overview-live"><div class="layout-grid">${
  kinds.map(kind => `<div data-layout-item-kind="${kind}"></div>`).join('')
}</div></div></div>`;

const fullDetail = {
  version: 5,
  energy: { values: {} },
  entities: {},
  diagnostics: { critical: 0, warning: 0, info: 0, status_class: 'ok' },
};

function overviewPanel(kinds, detail = fullDetail) {
  const { factories, window } = load({ html: gridHTML(kinds) });
  const calls = [];
  window.htmx = { ajax: (...args) => { calls.push(args); return Promise.resolve(); } };
  window.EnergyPresentation = { publish: () => {} };
  const panel = factories.devicesPanel();
  panel.$root = window.document.getElementById('overview-panel');
  panel.$refs = {};
  panel.init();
  window.dispatchEvent(new window.CustomEvent('registry-updated', { detail }));
  return { calls, window, panel };
}

test('reine Energiekarten tauschen weiterhin nicht', () => {
  const { calls } = overviewPanel(['energy_ring', 'energy_band']);
  assert.deepEqual(calls, []);
});

test('entity_value, entity_group und diagnostics tauschen jetzt auch nicht mehr', () => {
  const { calls } = overviewPanel(['entity_value', 'entity_group', 'diagnostics']);
  assert.deepEqual(calls, []);
});

test('gemischtes Raster aus Energie und Wertkarten tauscht nicht', () => {
  const { calls } = overviewPanel(['energy_ring', 'entity_value', 'diagnostics']);
  assert.deepEqual(calls, []);
});

// Ohne Fingerabdruck im Ereignis bleibt es beim Tausch - dieselbe Regel wie
// fuer jeden anderen fehlenden Zweig.
test('eine Geraetekachel ohne Fingerabdruck im Ereignis erzwingt den Tausch', () => {
  const { calls } = overviewPanel(['entity_value', 'device']);
  assert.equal(calls.length, 1);
});

// Faellt ein Zweig aus (Marshalling-Fehler im Server), darf die Karte nicht
// mit alten Zahlen stehenbleiben - dann ist der Tausch das Richtige.
test('fehlender diagnostics-Zweig laesst die Diagnose-Karte tauschen', () => {
  const { calls } = overviewPanel(['diagnostics'], { version: 5, entities: {} });
  assert.equal(calls.length, 1);
});

test('fehlender entities-Zweig laesst die Wertkarte tauschen', () => {
  const { calls } = overviewPanel(['entity_value'], { version: 5, energy: { values: {} } });
  assert.equal(calls.length, 1);
});

test('fehlender energy-Zweig laesst die Energiekarte tauschen', () => {
  const { calls } = overviewPanel(['energy_ring'], { version: 5, entities: {} });
  assert.equal(calls.length, 1);
});

test('leeres Raster (Geraete-Panel) tauscht wie bisher', () => {
  const { calls } = overviewPanel([]);
  assert.equal(calls.length, 1);
});

test('die Statustexte landen in commandStates', () => {
  const { factories, window } = load({ html: `<div id="overview-panel" class="panel active"><div class="layout-grid"><div data-layout-item-kind="entity_value"></div><article class="entity-value-card" data-entity-id="e1" data-device-class="" data-unit=""><div class="entity-value-num"><strong>-</strong></div><small class="entity-command-status"></small></article></div></div>` });
  window.htmx = { ajax: () => Promise.resolve() };
  window.EnergyPresentation = { publish: () => {} };
  const panel = factories.devicesPanel();
  panel.$root = window.document.getElementById('overview-panel');
  panel.$refs = {};
  panel.init();

  window.dispatchEvent(new window.CustomEvent('registry-updated', {
    detail: { version: 1, energy: { values: {} }, entities: { e1: { pending: true } }, diagnostics: { status_class: 'ok' } },
  }));

  assert.equal(panel.commandStates.e1, 'Warte auf Bestätigung über MQTT ...');
  assert.equal(window.document.querySelector('.entity-value-card').getAttribute('aria-busy'), null);
});

test('liveGridIsEnergyOnly gibt es nicht mehr', () => {
  const { factories } = load();
  assert.equal(factories.devicesPanel().liveGridIsEnergyOnly, undefined);
});

// Der Geraete-Tab: dasselbe Muster wie im Raster, nur haengt die Struktur
// hier an der Registry statt am Layout. Der Fingerabdruck aus
// registry.StructureFingerprint ist die Auskunft darueber, wann das Fragment
// veraltet ist.
const devicesHTML = (structure, mode = 'control', structureCompact = 'kompakt') => {
  const inner = mode === 'compact'
    ? `<div class="device-compact-grid"><button class="compact-card is-online" data-device-id="node">`
      + `<span class="compact-card-rows"><span class="compact-card-row" data-entity-id="e1" data-device-class="power" data-unit="W">`
      + `<span class="compact-card-row-label">Leistung</span><span class="compact-card-row-value">42 W</span>`
      + `</span></span></button></div>`
    : `<div class="device-tile-grid"><article class="device-tile" data-device-id="node">`
      + `<div class="device-tile-entity" data-entity-id="e1" data-component="sensor" data-device-class="power" data-unit="W">`
      + `<span class="device-tile-entity-value">42 W</span><small class="entity-command-status"></small>`
      + `</div></article></div>`;
  return `<div id="devices-panel" class="panel active" data-device-view-mode="${mode}">`
    + `<div id="devices-live" data-structure="${structure}" data-structure-compact="${structureCompact}">`
    + inner + `</div></div>`;
};

function devicesPanelWith(html, detail) {
  const { factories, window } = load({ html });
  const calls = [];
  window.htmx = { ajax: (...args) => { calls.push(args); return Promise.resolve(); } };
  window.EnergyPresentation = { publish: () => {} };
  const panel = factories.devicesPanel();
  panel.$root = window.document.getElementById('devices-panel');
  panel.$refs = {};
  panel.$nextTick = fn => fn();
  panel.init();
  window.dispatchEvent(new window.CustomEvent('registry-updated', { detail }));
  return { calls, window, panel };
}

const devicesDetail = (structure, extra = {}) => ({
  version: 5, energy: { values: {} }, entities: { e1: { value: '77', has_value: true } },
  diagnostics: { status_class: 'ok' }, structure, ...extra,
});

// Der eigentliche Gewinn dieses Plans.
test('gleicher Fingerabdruck: der Geraete-Tab tauscht nicht mehr', () => {
  const { calls, window } = devicesPanelWith(devicesHTML('abc'), devicesDetail('abc'));
  assert.deepEqual(calls, []);
  assert.equal(window.document.querySelector('.device-tile-entity-value').textContent, '77 W');
});

// Neue Entitaet, umbenanntes Geraet, neues Icon - alles wechselt den
// Fingerabdruck, und dafuer ist der Tausch da.
test('anderer Fingerabdruck: der Geraete-Tab tauscht wie frueher', () => {
  const { calls } = devicesPanelWith(devicesHTML('abc'), devicesDetail('def'));
  assert.equal(calls.length, 1);
});

// Faellt der Zweig aus (aelterer Server, Marshalling-Fehler), ist der Tausch
// das Richtige - lieber teuer neu bauen als stumm alte Zahlen zeigen.
test('fehlender structure-Zweig laesst den Geraete-Tab tauschen', () => {
  const { calls } = devicesPanelWith(devicesHTML('abc'), { version: 5, entities: {} });
  assert.equal(calls.length, 1);
});

test('fehlender entities-Zweig laesst den Geraete-Tab tauschen', () => {
  const { calls } = devicesPanelWith(devicesHTML('abc'), { version: 5, structure: 'abc' });
  assert.equal(calls.length, 1);
});

// Der Gewinn dieses Plans fuer die Kompakt-Ansicht: stehen beide
// Fingerabdruecke, zieht sie nach statt zu tauschen.
test('Kompakt-Ansicht, beide Fingerabdruecke gleich: kein Tausch', () => {
  const { calls, window } = devicesPanelWith(
    devicesHTML('abc', 'compact', 'k1'),
    devicesDetail('abc', { structure_compact: 'k1' }),
  );
  assert.deepEqual(calls, []);
  assert.equal(window.document.querySelector('.compact-card-row-value').textContent, '77 W');
});

// Zeilenauswahl gewachsen oder Geraete-Ampel gewechselt: der
// Kompakt-Fingerabdruck wandert, dafuer ist der Tausch da.
test('Kompakt-Ansicht, anderer Kompakt-Fingerabdruck: Tausch', () => {
  const { calls } = devicesPanelWith(
    devicesHTML('abc', 'compact', 'k1'),
    devicesDetail('abc', { structure_compact: 'k2' }),
  );
  assert.equal(calls.length, 1);
});

// Faellt der Kompakt-Zweig aus (aelterer Server), ist der Tausch das Richtige.
test('Kompakt-Ansicht, fehlender structure_compact-Zweig: Tausch', () => {
  const { calls } = devicesPanelWith(
    devicesHTML('abc', 'compact', 'k1'),
    devicesDetail('abc'),
  );
  assert.equal(calls.length, 1);
});

// Discovery hat sich geaendert: schon der geteilte Fingerabdruck stoppt das
// Nachziehen, egal was der Kompakt-Zweig sagt.
test('Kompakt-Ansicht, anderer geteilter Fingerabdruck: Tausch', () => {
  const { calls } = devicesPanelWith(
    devicesHTML('abc', 'compact', 'k1'),
    devicesDetail('def', { structure_compact: 'k1' }),
  );
  assert.equal(calls.length, 1);
});

test('die Statustexte der Kachel landen in commandStates', () => {
  const { panel } = devicesPanelWith(
    devicesHTML('abc'),
    devicesDetail('abc', { entities: { e1: { value: '1', has_value: true, pending: true } } }),
  );
  assert.equal(panel.commandStates.e1, 'Warte auf Bestätigung über MQTT ...');
});

// Die Gerätekachel im Raster: sie darf jetzt auch nachgezogen werden - aber
// nur, solange der Fingerabdruck steht. Ihre Entitaetenliste haengt an der
// Registry, nicht am Layout; eine neu entdeckte Entitaet bekaeme sonst nie
// ihre Zeile.
const gridDetail = (structure) => ({ ...fullDetail, structure });

const gridHTMLWithStructure = (kinds, structure) => `<div id="overview-panel" class="panel active"><div id="overview-live" data-structure="${structure}"><div class="layout-grid">${
  kinds.map(kind => `<div data-layout-item-kind="${kind}"></div>`).join('')
}</div></div></div>`;

function overviewPanelWithStructure(kinds, htmlStructure, detail) {
  const { factories, window } = load({ html: gridHTMLWithStructure(kinds, htmlStructure) });
  const calls = [];
  window.htmx = { ajax: (...args) => { calls.push(args); return Promise.resolve(); } };
  window.EnergyPresentation = { publish: () => {} };
  const panel = factories.devicesPanel();
  panel.$root = window.document.getElementById('overview-panel');
  panel.$refs = {};
  panel.init();
  window.dispatchEvent(new window.CustomEvent('registry-updated', { detail }));
  return { calls, window, panel };
}

test('Geraetekachel im Raster tauscht nicht mehr, solange die Struktur steht', () => {
  const { calls } = overviewPanelWithStructure(['energy_ring', 'device'], 'abc', gridDetail('abc'));
  assert.deepEqual(calls, []);
});

test('Geraetekachel im Raster tauscht, sobald sich die Struktur aendert', () => {
  const { calls } = overviewPanelWithStructure(['energy_ring', 'device'], 'abc', gridDetail('def'));
  assert.equal(calls.length, 1);
});

// Der Rueckfall fuer jeden Kartentyp, den branchForKind nicht kennt: lieber
// tauschen als eine Karte stehen lassen, die der Push nicht bedient. Bis 2026-09
// war 'entity' der Dauergast in diesem Zweig - der Typ ist abgeschafft, die
// Regel bleibt.
test('ein unbekannter Kartentyp im Raster erzwingt den Tausch', () => {
  const { calls } = overviewPanelWithStructure(['entity_value', 'kuenftige_karte'], 'abc', gridDetail('abc'));
  assert.equal(calls.length, 1);
});

test('ein entities_delta fasst nur die genannte Karte an und tauscht nicht', () => {
  const html = `<div id="overview-panel" class="panel active"><div id="overview-live" data-structure="fp1">
    <div class="layout-grid"><div data-layout-item-kind="entity_value"></div></div>
    <article class="entity-value-card" data-entity-id="e1" data-device-class="" data-unit="W"><div class="entity-value-num"><strong>1</strong></div></article>
    <article class="entity-value-card" data-entity-id="e2" data-device-class="" data-unit="W"><div class="entity-value-num"><strong>2</strong></div></article>
  </div></div>`;
  const { factories, window } = load({ html });
  const calls = [];
  window.htmx = { ajax: (...a) => { calls.push(a); return Promise.resolve(); } };
  window.EnergyPresentation = { publish: () => {} };
  const panel = factories.devicesPanel();
  panel.$root = window.document.getElementById('overview-panel');
  panel.$refs = {};
  panel.init();

  window.dispatchEvent(new window.CustomEvent('registry-updated', {
    detail: {
      version: 6, structure: 'fp1', entities_delta: true,
      energy: { values: {} }, diagnostics: { status_class: 'ok' },
      entities: { e1: { value: '9', has_value: true, available: true, has_availability: true } },
    },
  }));

  assert.equal(window.document.querySelector('[data-entity-id="e1"] strong').textContent, '9');
  assert.equal(window.document.querySelector('[data-entity-id="e2"] strong').textContent, '2'); // unangetastet
  assert.deepEqual(calls, []); // kein Fragment-Tausch
});

test('bei offenem Geraete-Modal ruht das Nachziehen des Rasters', async () => {
  const { calls, window, panel } = overviewPanel(['entity_value', 'device'], {
    version: 7, structure: 'x', energy: { values: {} }, entities: {}, diagnostics: { status_class: 'ok' },
  });
  // overviewPanel hat schon ein Ereignis geschickt; ['entity_value','device']
  // ohne passenden Fingerabdruck wuerde sonst tauschen.
  calls.length = 0;

  panel.selectedDeviceId = 'dev-1';
  panel.$refs.deviceModalDialog = { open: true, close() { this.open = false; } };

  window.dispatchEvent(new window.CustomEvent('registry-updated', {
    detail: { version: 8, structure: 'x', entities_delta: true, energy: { values: {} }, entities: {}, diagnostics: { status_class: 'ok' } },
  }));
  assert.deepEqual(calls, [], 'kein Tausch, solange das Modal offen ist');
  assert.equal(panel.livePatchStale, true);

  panel.closeDeviceDetail();
  // Ein Makrotask statt eines einzelnen Microtasks: der Aufbau dieses Tests
  // hat schon einen Fragment-Tausch angestossen, und refreshLiveFragment()
  // reiht seit 2026-09 hintereinander ein, statt zwei GETs nebeneinander
  // laufen zu lassen (wessen Antwort zuletzt kam, entschied sonst die Seite).
  // Der Nachhol-Refresh startet also erst, wenn der erste durch ist.
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(calls.length, 1, 'ein Nachhol-Refresh beim Schliessen');
  assert.equal(panel.livePatchStale, false);
});

// Ein Raster mit einer kompakten Geraetekachel. configured=true haengt die
// feste-Auswahl-Markierung an, wie sie der Server fuer eine Kachel mit
// entity_refs rendert.
const compactGridHTML = (structure, structureCompact, display = 'compact', {configured = false, availability = 'av1'} = {}) =>
  `<div id="overview-panel" class="panel active"><div id="overview-live" data-structure="${structure}" data-structure-compact="${structureCompact}" data-structure-availability="${availability}"><div class="layout-grid">`
  + `<div data-layout-item-kind="device" data-display="${display}" data-compact-configured="${configured}" data-layout-item-ref="dev1"></div>`
  + `</div></div></div>`;

// Wie overviewPanel(), nur mit fertigem Markup statt einer Kind-Liste.
function compactPanelCalls(html, detail) {
  const { factories, window } = load({ html });
  const calls = [];
  window.htmx = { ajax: (...args) => { calls.push(args); return Promise.resolve(); } };
  window.EnergyPresentation = { publish: () => {} };
  const panel = factories.devicesPanel();
  panel.$root = window.document.getElementById('overview-panel');
  panel.$refs = {};
  panel.init();
  window.dispatchEvent(new window.CustomEvent('registry-updated', { detail }));
  return calls;
}

// Welche bis zu drei Zeilen die kompakte Kachel zeigt, entscheidet
// priorityEntities() anhand der Werte - eine Auswahl, durch deren Raster der
// geteilte Fingerabdruck faellt. Ohne structure_compact zoege der Push stumm
// Werte in Zeilen nach, die es nicht mehr geben duerfte.
test('kompakte Geraetekachel: beide Fingerabdruecke passen, kein Tausch', () => {
  const calls = compactPanelCalls(compactGridHTML('s1', 'c1'), { ...fullDetail, structure: 's1', structure_compact: 'c1' });
  assert.deepEqual(calls, []);
});

test('kompakte Geraetekachel: geaenderte Zeilenauswahl tauscht', () => {
  const calls = compactPanelCalls(compactGridHTML('s1', 'c1'), { ...fullDetail, structure: 's1', structure_compact: 'c2' });
  assert.equal(calls.length, 1);
});

test('kompakte Geraetekachel: fehlendes structure_compact tauscht', () => {
  const calls = compactPanelCalls(compactGridHTML('s1', 'c1'), { ...fullDetail, structure: 's1' });
  assert.equal(calls.length, 1);
});

test('die Detailkachel bleibt beim geteilten Fingerabdruck', () => {
  const calls = compactPanelCalls(compactGridHTML('s1', 'c1', 'detail'), { ...fullDetail, structure: 's1' });
  assert.deepEqual(calls, []);
});

test('konfigurierte Kompaktkachel: structure + structure_availability passen, kein Tausch', () => {
  const calls = compactPanelCalls(
    compactGridHTML('s1', 'c1', 'compact', {configured: true, availability: 'av1'}),
    { ...fullDetail, structure: 's1', structure_availability: 'av1' });
  assert.deepEqual(calls, []);
});

test('konfigurierte Kompaktkachel: veraenderter structure_compact ist egal', () => {
  const calls = compactPanelCalls(
    compactGridHTML('s1', 'c1', 'compact', {configured: true, availability: 'av1'}),
    { ...fullDetail, structure: 's1', structure_compact: 'c2', structure_availability: 'av1' });
  assert.deepEqual(calls, []);
});

test('konfigurierte Kompaktkachel: Verfuegbarkeitswechsel tauscht', () => {
  const calls = compactPanelCalls(
    compactGridHTML('s1', 'c1', 'compact', {configured: true, availability: 'av1'}),
    { ...fullDetail, structure: 's1', structure_availability: 'av2' });
  assert.equal(calls.length, 1);
});

test('konfigurierte Kompaktkachel: fehlendes structure_availability tauscht', () => {
  const calls = compactPanelCalls(
    compactGridHTML('s1', 'c1', 'compact', {configured: true, availability: 'av1'}),
    { ...fullDetail, structure: 's1' });
  assert.equal(calls.length, 1);
});

test('gemischtes Raster: eine automatische Kompaktkachel zieht den strengeren Waechter', () => {
  const { factories, window } = load({ html:
    `<div id="overview-panel" class="panel active"><div id="overview-live" data-structure="s1" data-structure-compact="c1" data-structure-availability="av1"><div class="layout-grid">`
    + `<div data-layout-item-kind="device" data-display="compact" data-compact-configured="true" data-layout-item-ref="dev1"></div>`
    + `<div data-layout-item-kind="device" data-display="compact" data-compact-configured="false" data-layout-item-ref="dev2"></div>`
    + `</div></div></div>` });
  const calls = [];
  window.htmx = { ajax: (...args) => { calls.push(args); return Promise.resolve(); } };
  window.EnergyPresentation = { publish: () => {} };
  const panel = factories.devicesPanel();
  panel.$root = window.document.getElementById('overview-panel');
  panel.$refs = {};
  panel.init();
  window.dispatchEvent(new window.CustomEvent('registry-updated', { detail:
    { ...fullDetail, structure: 's1', structure_availability: 'av1' } }));
  assert.equal(calls.length, 1); // structure_compact fehlt -> die automatische Kachel erzwingt den Tausch
});

// device-structure.js: patcht Gerätekacheln aus dem SSE-Thema devices und
// meldet Strukturwechsel, ohne selbst Markup zu erfinden.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');

const rowTemplate = '<template data-compact-row><span class="compact-card-row" data-entity-id="" data-device-class="" data-unit=""><span class="compact-card-row-label"></span><span class="compact-card-row-value">–</span></span></template>';

const compactCard = ({ id = 'a', structure = 's1', auto = true, rows = 'a1' } = {}) =>
  `<button class="compact-card is-online" data-device-id="${id}" data-structure="${structure}"${auto ? ' data-compact-auto="true"' : ''}>`
  + '<span class="compact-card-status compact-card-status-online"><span class="compact-card-dot"></span>Online</span>'
  + `<span class="compact-card-rows" data-rows="${rows}">${rows.split(',').filter(Boolean).map(r => `<span class="compact-card-row" data-entity-id="${r}" data-unit="W"><span class="compact-card-row-label">${r}</span><span class="compact-card-row-value">1 W</span></span>`).join('')}</span></button>`;

const detailTile = ({ id = 'a', structure = 's1' } = {}) =>
  `<article class="device-tile" data-device-id="${id}" data-structure="${structure}"><span class="device-tile-dot device-tile-dot-ok" title="alle online"></span></article>`;

const overview = items => `<div id="overview-panel"><div id="overview-live">${rowTemplate}${items.join('')}</div></div>`;
const gridItem = ({ item = 'i1', ref = 'a', structure = 's1', inner = compactCard({ id: ref, structure }) } = {}) =>
  `<div class="layout-grid-item" data-layout-item-id="${item}" data-layout-item-kind="device" data-layout-item-ref="${ref}" data-structure="${structure}">${inner}</div>`;

function load(html) {
  const dom = new JSDOM(`<!doctype html><html><body>${html}</body></html>`, { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  installI18n(dom.window);
  for (const name of ['entity-values.js', 'compact-card-values.js', 'device-structure.js']) vm.runInContext(read(name), context);
  return { window: dom.window, document: dom.window.document };
}

const entry = (over = {}) => ({ structure: 's1', availability: 'online', status: { class: 'ok', rail: '', offline: 0 }, ...over });

test('ohne Geraetekacheln ist alles bedient', () => {
  const { window, document } = load('<div id="overview-panel"></div>');
  assert.equal(window.deviceStructure.apply(document.getElementById('overview-panel'), {}), true);
});

test('fehlt der Zweig devices, meldet apply false', () => {
  const { window, document } = load(overview([gridItem()]));
  assert.equal(window.deviceStructure.apply(document.getElementById('overview-panel'), { entities: {} }), false);
});

test('Ampelwechsel patcht Klasse und Text ohne replace', () => {
  const { window, document } = load(overview([gridItem()]));
  const replaced = [];
  const ok = window.deviceStructure.apply(document.getElementById('overview-panel'),
    { devices: { a: entry({ availability: 'offline' }) }, devices_delta: true },
    { replace: el => replaced.push(el) });
  assert.equal(ok, true);
  assert.equal(replaced.length, 0);
  const card = document.querySelector('.compact-card');
  assert.ok(card.classList.contains('is-offline'));
  assert.ok(!card.classList.contains('is-online'));
  const status = card.querySelector('.compact-card-status');
  assert.ok(status.classList.contains('compact-card-status-offline'));
  assert.equal(status.lastChild.nodeValue, 'Offline');
});

test('Status der Detailkachel patcht Rail, Punkt und Titel', () => {
  const { window, document } = load(overview([gridItem({ inner: detailTile() })]));
  window.deviceStructure.apply(document.getElementById('overview-panel'),
    { devices: { a: entry({ status: { class: 'bad', rail: 'is-offline', offline: 2 } }) }, devices_delta: true });
  const tile = document.querySelector('.device-tile');
  assert.ok(tile.classList.contains('is-offline'));
  const dot = tile.querySelector('.device-tile-dot');
  assert.ok(dot.classList.contains('device-tile-dot-bad'));
  assert.ok(!dot.classList.contains('device-tile-dot-ok'));
  assert.equal(dot.getAttribute('title'), '2 offline');
  window.deviceStructure.apply(document.getElementById('overview-panel'),
    { devices: { a: entry({ status: { class: 'unknown', rail: '', offline: 0 } }) }, devices_delta: true });
  assert.equal(dot.hasAttribute('title'), false);
  assert.ok(!tile.classList.contains('is-offline'));
});

test('Strukturwechsel ruft replace genau fuer diese Kachel', () => {
  const { window, document } = load(overview([gridItem({ item: 'i1', ref: 'a' }), gridItem({ item: 'i2', ref: 'b' })]));
  const replaced = [];
  window.deviceStructure.apply(document.getElementById('overview-panel'),
    { devices: { a: entry({ structure: 's2' }) }, devices_delta: true },
    { replace: el => replaced.push(el.dataset.layoutItemId) });
  assert.deepEqual(JSON.parse(JSON.stringify(replaced)), ['i1']);
});

test('ein Geraet, das in beiden fehlt, wird nicht nachgeladen', () => {
  const { window, document } = load(overview([gridItem({ ref: 'weg', structure: '', inner: '' })]));
  const replaced = [];
  window.deviceStructure.apply(document.getElementById('overview-panel'), { devices: {} }, { replace: el => replaced.push(el) });
  assert.equal(replaced.length, 0);
});

test('im vollen Rumpf heisst fehlend: Geraet weg, also replace', () => {
  const { window, document } = load(overview([gridItem()]));
  const replaced = [];
  window.deviceStructure.apply(document.getElementById('overview-panel'), { devices: {} }, { replace: el => replaced.push(el) });
  assert.equal(replaced.length, 1);
});

test('null im Delta heisst Geraet entfernt, also replace', () => {
  const { window, document } = load(overview([gridItem()]));
  const replaced = [];
  window.deviceStructure.apply(document.getElementById('overview-panel'), { devices: { a: null }, devices_delta: true }, { replace: el => replaced.push(el) });
  assert.equal(replaced.length, 1);
});

test('neue Zeilenauswahl baut Zeilen aus der Vorlage und fuellt Werte aus dem Cache', () => {
  const { window, document } = load(overview([gridItem()]));
  const rows = [
    { id: 'a1', label: 'Leistung', unit: 'W', device_class: 'power' },
    { id: 'a2', label: 'Spannung', unit: 'V', device_class: 'voltage' },
  ];
  const values = { a2: { value: '230', has_value: true, stale: false } };
  const replaced = [];
  window.deviceStructure.apply(document.getElementById('overview-panel'),
    { devices: { a: entry({ rows }) }, devices_delta: true },
    { values, replace: el => replaced.push(el) });
  assert.equal(replaced.length, 0);
  const container = document.querySelector('.compact-card-rows');
  assert.equal(container.dataset.rows, 'a1,a2');
  const built = [...container.querySelectorAll('.compact-card-row')];
  assert.deepEqual(built.map(r => r.dataset.entityId), ['a1', 'a2']);
  assert.equal(built[1].querySelector('.compact-card-row-label').textContent, 'Spannung');
  assert.equal(built[1].dataset.unit, 'V');
  assert.match(built[1].querySelector('.compact-card-row-value').textContent, /^230 V$/);
  assert.equal(built[0].querySelector('.compact-card-row-value').textContent, '–');
});

test('gleiche Zeilenauswahl laesst die Zeilen stehen', () => {
  const { window, document } = load(overview([gridItem()]));
  const before = document.querySelector('.compact-card-row');
  window.deviceStructure.apply(document.getElementById('overview-panel'),
    { devices: { a: entry({ rows: [{ id: 'a1', label: 'a1', unit: 'W', device_class: '' }] }) }, devices_delta: true });
  assert.equal(document.querySelector('.compact-card-row'), before);
});

test('konfigurierte Kompaktkarte ignoriert rows', () => {
  const { window, document } = load(overview([gridItem({ inner: compactCard({ auto: false }) })]));
  window.deviceStructure.apply(document.getElementById('overview-panel'),
    { devices: { a: entry({ rows: [{ id: 'zz', label: 'z', unit: '', device_class: '' }] }) }, devices_delta: true });
  assert.equal(document.querySelector('.compact-card-rows').dataset.rows, 'a1');
});

test('Wechsel auf keine Zeilen holt die Kachel neu', () => {
  const { window, document } = load(overview([gridItem()]));
  const replaced = [];
  window.deviceStructure.apply(document.getElementById('overview-panel'),
    { devices: { a: entry({ rows: [] }) }, devices_delta: true }, { replace: el => replaced.push(el) });
  assert.equal(replaced.length, 1);
});

test('Geraete-Tab: die Karte ist der Traeger, devices_list prueft die Reihenfolge', () => {
  const html = `<div id="devices-panel"><div id="devices-live">${rowTemplate}${compactCard({ id: 'a' })}${compactCard({ id: 'b', rows: 'b1' })}</div></div>`;
  const { window, document } = load(html);
  const root = document.getElementById('devices-panel');
  const replaced = [];
  assert.equal(window.deviceStructure.apply(root,
    { devices: { b: entry({ structure: 's9' }) }, devices_delta: true, devices_list: ['a', 'b'] },
    { replace: el => replaced.push(el.dataset.deviceId) }), true);
  assert.deepEqual(JSON.parse(JSON.stringify(replaced)), ['b']);
  assert.equal(window.deviceStructure.apply(root, { devices: {}, devices_delta: true, devices_list: ['a', 'b', 'c'] }), false);
});

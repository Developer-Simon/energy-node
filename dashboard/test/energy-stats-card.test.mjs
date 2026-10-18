// Kachel energy_stats. Die Uebersicht tauscht Karten bis zu einmal je
// Sekunde aus. Geprueft wird vor allem, dass das keine Abfrageflut ausloest
// und der gewaehlte Zeitraum den Tausch ueberlebt.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';
import { DAY_FIXTURE } from './helpers/energy-stats-fixtures.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');

function load({ storage, dataset = {}, apex = false } = {}) {
  const dom = new JSDOM('<!doctype html><html><body><div data-layout-item-id="stats-1"><section id="card"></section></div><div data-layout-item-id="stats-2"><section id="card2"></section></div></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  Object.assign(dom.window.document.querySelector('[data-layout-item-id="stats-1"]').dataset, dataset);
  const charts = [];
  if (apex) {
    dom.window.ApexCharts = class {
      constructor(el, options) { this.el = el; this.options = options; this.destroyed = false; charts.push(this); }
      render() {}
      destroy() { this.destroyed = true; }
    };
  }
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = {data: (_name, fn) => { factory = fn; }};
  if (storage !== undefined) Object.defineProperty(dom.window, 'localStorage', { configurable: true, get: storage });
  // Kein Zustand aus einem frueheren Test, falls jsdom den Speicher je
  // Origin teilt.
  else dom.window.localStorage.clear();
  installI18n(dom.window);
  const calls = [];
  const server = { revision: 1 };
  dom.window.fetch = async url => { calls.push(url); return {ok: true, status: 200, json: async () => ({...DAY_FIXTURE, period: new URL(url, 'http://x').searchParams.get('period'), revision: server.revision})}; };
  for (const name of ['theme.js', 'energy-model.js', 'energy-stats-model.js', 'energy-stats-card.js']) vm.runInContext(read(name), context);
  const make = (id = 'card') => {
    const card = factory();
    card.$root = dom.window.document.getElementById(id);
    return card;
  };
  return {dom, factory, make, calls, server, charts};
}

test('Kachel zeigt Kennzahlen und Datenlage fuer heute', async () => {
  const {dom, make} = load();
  const card = make();
  await card.init();
  assert.equal(card.period, 'day');
  assert.deepEqual(JSON.parse(JSON.stringify(card.kpis.map(k => k.id))), ['consumption', 'pv', 'grid_import', 'grid_export', 'autarky']);
  assert.equal(card.kpis[1].value, '2,50 kWh');
  assert.match(card.coverage, /^Aus Home Assistant 20 h/);
  dom.window.close();
});

test('Neue Instanz nach einem Kartentausch fragt nicht erneut und behaelt den Zeitraum', async () => {
  const {dom, make, calls} = load();
  const first = make();
  await first.init();
  await first.select('month');
  const second = make();
  await second.init();
  assert.equal(second.period, 'month');
  assert.equal(calls.filter(url => url.includes('period=month')).length, 1);
  assert.equal(calls.filter(url => url.includes('period=day')).length, 1);
  dom.window.close();
});

test('Nach 60 s fragt die Kachel neu', async () => {
  const {dom, factory, calls} = load();
  await factory.cached('day', 1_000);
  await factory.cached('day', 30_000);
  await factory.cached('day', 61_001);
  assert.equal(calls.length, 2);
  dom.window.close();
});

test('Fehler wird angezeigt und nicht gecacht', async () => {
  const {dom, make, calls} = load();
  let fail = true;
  dom.window.fetch = async url => {
    calls.push(url);
    if (fail) return {ok: false, status: 500, json: async () => ({code: 'energy_stats_failed', message: 'kaputt'})};
    return {ok: true, status: 200, json: async () => DAY_FIXTURE};
  };
  const card = make();
  await card.init();
  assert.ok(card.error);
  fail = false;
  await card.load();
  assert.equal(card.error, '');
  assert.equal(calls.length, 2);
  dom.window.close();
});

test('open() oeffnet die Seite mit demselben Zeitraum', async () => {
  const {dom, make} = load();
  const card = make();
  await card.init();
  await card.select('week');
  let detail;
  dom.window.addEventListener('dashboard-open-panel', event => { detail = event.detail; });
  card.open();
  assert.equal(detail.panel, 'stats-panel');
  assert.equal(detail.statsFocus.period, 'week');
  assert.equal(detail.statsFocus.at, '2026-10-12');
  dom.window.close();
});

test('gleiche Revision im Ereignis fragt nicht', async () => {
  const {dom, factory, make, calls} = load();
  const card = make();
  await card.init();
  const before = calls.length;
  factory.applyRevision(dom.window.document.body, {rev: 1});
  factory.applyRevision(dom.window.document.body, {rev: 1});
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(calls.length, before);
  dom.window.close();
});

test('neue Revision laedt genau einmal je Zeitraum', async () => {
  const {dom, factory, make, calls, server} = load();
  const a = make('card');
  await a.init();
  const b = make('card2');
  await b.init();
  await b.select('month');
  const c = make('card');
  await c.init();
  // a und c zeigen day, b zeigt month.
  calls.length = 0;
  server.revision = 2;
  factory.applyRevision(dom.window.document.body, {rev: 2});
  await new Promise(resolve => setTimeout(resolve, 0));
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(calls.filter(url => url.includes('period=day')).length, 1);
  assert.equal(calls.filter(url => url.includes('period=month')).length, 1);
  assert.equal(a.revision, 2);
  dom.window.close();
});

test('eine abgebaute Kachel laedt nicht mehr', async () => {
  const {dom, factory, make, calls, server} = load();
  const card = make();
  await card.init();
  card.destroy();
  calls.length = 0;
  server.revision = 2;
  factory.applyRevision(dom.window.document.body, {rev: 2});
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(calls.length, 0);
  dom.window.close();
});

test('der Zeitraum ueberlebt ein Neuladen ueber localStorage', async () => {
  const first = load();
  const card = first.make();
  await card.init();
  await card.select('year');
  const stored = first.dom.window.localStorage.getItem(first.factory.STORAGE_PREFIX + 'stats-1');
  assert.equal(stored, 'year');
  first.dom.window.close();

  const values = new Map([['energy-stats-period:stats-1', 'year']]);
  const second = load({ storage: () => ({ getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value) }) });
  const again = second.make();
  await again.init();
  assert.equal(again.period, 'year');
  second.dom.window.close();
});

test('ohne localStorage gilt heute und nichts wirft', async () => {
  const {dom, make} = load({ storage: () => { throw new Error('SecurityError'); } });
  const card = make();
  await card.init();
  assert.equal(card.period, 'day');
  await card.select('week');
  assert.equal(card.period, 'week');
  dom.window.close();
});

test('ein ungueltiger gespeicherter Zeitraum faellt auf heute zurueck', async () => {
  const {dom, make} = load({ storage: () => ({ getItem: () => 'decade', setItem: () => {} }) });
  const card = make();
  await card.init();
  assert.equal(card.period, 'day');
  dom.window.close();
});

test('ein fester Zeitraum gilt beim Laden und wird nicht gemerkt', async () => {
  const values = new Map([['energy-stats-period:stats-1', 'week']]);
  const {dom, make} = load({ dataset: {statsPeriod: 'month'}, storage: () => ({ getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value) }) });
  const card = make();
  await card.init();
  assert.equal(card.period, 'month');
  await card.select('year');
  assert.equal(card.period, 'year');
  assert.equal(values.get('energy-stats-period:stats-1'), 'week');
  dom.window.close();
});

test('ein unbekannter fester Zeitraum gilt als nicht festgelegt', async () => {
  const {dom, make} = load({ dataset: {statsPeriod: 'decade'} });
  const card = make();
  await card.init();
  assert.equal(card.preset, '');
  assert.equal(card.period, 'day');
  dom.window.close();
});

test('die Kachel zeigt die gewaehlten Kennzahlen in ihrer Reihenfolge', async () => {
  const {dom, make} = load({ dataset: {statsKpis: 'autarky,battery_charge,nope'} });
  const card = make();
  await card.init();
  assert.deepEqual(JSON.parse(JSON.stringify(card.kpis.map(k => k.id))), ['autarky', 'battery_charge']);
  dom.window.close();
});

test('mit Diagramm zeichnet die Kachel kompakt und ohne Legende', async () => {
  const {dom, make, charts} = load({ dataset: {statsChart: 'on'}, apex: true });
  const card = make();
  card.$refs = {chart: dom.window.document.createElement('div')};
  await card.init();
  assert.equal(charts.length, 1);
  assert.equal(charts[0].options.chart.height, 180);
  assert.equal(charts[0].options.legend.show, false);
  await card.select('week');
  assert.equal(charts.length, 2);
  assert.equal(charts[0].destroyed, true);
  card.destroy();
  assert.equal(charts[1].destroyed, true);
  dom.window.close();
});

test('ohne Diagramm entsteht kein Chart', async () => {
  const {dom, make, charts} = load({ apex: true });
  const card = make();
  card.$refs = {chart: dom.window.document.createElement('div')};
  await card.init();
  assert.equal(card.showChart, false);
  assert.equal(charts.length, 0);
  dom.window.close();
});

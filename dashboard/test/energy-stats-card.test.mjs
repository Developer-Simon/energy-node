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

function load() {
  const dom = new JSDOM('<!doctype html><html><body><div data-layout-item-id="stats-1"><section id="card"></section></div></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = {data: (_name, fn) => { factory = fn; }};
  installI18n(dom.window);
  const calls = [];
  dom.window.fetch = async url => { calls.push(url); return {ok: true, status: 200, json: async () => ({...DAY_FIXTURE, period: new URL(url, 'http://x').searchParams.get('period')})}; };
  for (const name of ['theme.js', 'energy-model.js', 'energy-stats-model.js', 'energy-stats-card.js']) vm.runInContext(read(name), context);
  const make = () => {
    const card = factory();
    card.$root = dom.window.document.getElementById('card');
    return card;
  };
  return {dom, factory, make, calls};
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

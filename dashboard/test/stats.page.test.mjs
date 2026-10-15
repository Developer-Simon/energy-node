// Seite "Statistik": Zeitraumwahl, Zurueck/Vor aus prev/next der Antwort,
// Uebernahme des Zeitraums aus der Kachel und Diagrammoptionen.
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

const EMPTY_YEAR = {
  period: 'year', at: '2026', prev: '2025', next: '',
  buckets: Array.from({length: 12}, (_, i) => ({start: '', local: `2026-${String(i + 1).padStart(2, '0')}-01T00:00`, kwh: {}, hours: {ha: 0, own: 0, none: 0}, autarky: null, self_consumption: null})),
  total: {kwh: {}, hours: {ha: 0, own: 0, none: 0}, autarky: null, self_consumption: null},
  status: {state: 'recording', mode: 'always'},
};

function load(responses = {}) {
  const dom = new JSDOM('<!doctype html><html><body><div id="chart"></div></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = {data: (_name, fn) => { factory = fn; }};
  installI18n(dom.window);
  const calls = [];
  const server = {revision: 1};
  dom.window.fetch = async url => {
    calls.push(url);
    const params = new URL(url, 'http://x').searchParams;
    const body = responses[`${params.get('period')}|${params.get('at') || ''}`] || DAY_FIXTURE;
    return {ok: true, status: 200, json: async () => ({...body, revision: server.revision})};
  };
  const charts = [];
  dom.window.ApexCharts = class {
    constructor(el, options) { this.options = options; this.destroyed = false; charts.push(this); }
    render() { return Promise.resolve(); }
    destroy() { this.destroyed = true; }
  };
  for (const name of ['theme.js', 'energy-model.js', 'energy-stats-model.js', 'stats.page.js']) vm.runInContext(read(name), context);
  const make = () => {
    const panel = factory();
    panel.$refs = {chart: dom.window.document.getElementById('chart')};
    panel.$nextTick = fn => fn();
    return panel;
  };
  return {dom, factory, make, calls, charts, server};
}

test('Startet mit dem laufenden Tag und fuellt Titel, Kennzahlen, Tabelle und Datenlage', async () => {
  const {dom, make, calls} = load();
  const panel = make();
  await panel.init();
  assert.equal(calls[0], '/api/v1/energy/stats?period=day');
  assert.match(panel.title, /Oktober 2026/);
  assert.deepEqual(JSON.parse(JSON.stringify(panel.kpis.map(k => k.id))), ['consumption', 'pv', 'grid_import', 'grid_export', 'battery_charge', 'battery_discharge', 'autarky', 'self_consumption']);
  assert.equal(panel.rows.length, 5);
  assert.equal(panel.labels.length, 2);
  assert.match(panel.coverage, /^Aus Home Assistant/);
  assert.equal(panel.hasNext, false);
  assert.equal(panel.hasPrev, true);
  dom.window.close();
});

test('Zurueck und Vor nutzen prev und next der Antwort', async () => {
  const {dom, make, calls} = load({'day|2026-10-11': {...DAY_FIXTURE, at: '2026-10-11', prev: '2026-10-10', next: '2026-10-12'}});
  const panel = make();
  await panel.init();
  await panel.prev();
  assert.equal(calls.at(-1), '/api/v1/energy/stats?period=day&at=2026-10-11');
  assert.equal(panel.hasNext, true);
  await panel.next();
  assert.equal(calls.at(-1), '/api/v1/energy/stats?period=day&at=2026-10-12');
  dom.window.close();
});

test('Zeitraumwechsel springt auf den laufenden Zeitraum', async () => {
  const {dom, make, calls} = load();
  const panel = make();
  await panel.init();
  await panel.prev();
  await panel.selectPeriod('month');
  assert.equal(calls.at(-1), '/api/v1/energy/stats?period=month');
  dom.window.close();
});

test('Uebernimmt Zeitraum und Datum aus der Kachel, auch wenn die Seite erst laedt', async () => {
  const {dom, make, calls} = load();
  dom.window.__statsFocusRequest__ = {period: 'week', at: '2026-10-12'};
  const panel = make();
  await panel.init();
  assert.equal(calls[0], '/api/v1/energy/stats?period=week&at=2026-10-12');
  assert.equal(dom.window.__statsFocusRequest__, undefined);
  dom.window.dispatchEvent(new dom.window.CustomEvent('energy-stats-focus', {detail: {period: 'year', at: '2026'}}));
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(calls.at(-1), '/api/v1/energy/stats?period=year&at=2026');
  dom.window.close();
});

test('Leeres Jahr zeigt den Leerzustand und zeichnet ohne Fehler', async () => {
  const {dom, make, charts} = load({'year|': EMPTY_YEAR});
  const panel = make();
  await panel.selectPeriod('year');
  assert.equal(panel.empty, true);
  assert.equal(panel.coverage, 'Noch keine Daten');
  assert.equal(panel.rows.length, 0);
  assert.ok(charts.length >= 1);
  assert.deepEqual(JSON.parse(JSON.stringify(charts.at(-1).options.series)), []);
  dom.window.close();
});

test('Neues Diagramm ersetzt das alte', async () => {
  const {dom, make, charts} = load();
  const panel = make();
  await panel.init();
  await panel.selectPeriod('week');
  assert.equal(charts.length, 2);
  assert.equal(charts[0].destroyed, true);
  dom.window.close();
});

test('chartOptions stapelt, beschriftet die Achse aus local und zeigt Betraege positiv', () => {
  const {dom, factory} = load();
  const options = factory.chartOptions(DAY_FIXTURE);
  assert.equal(options.chart.type, 'bar');
  assert.equal(options.chart.stacked, true);
  assert.match(options.xaxis.categories[0], /^14[:.]00/);
  assert.equal(options.series.find(s => s.role === 'load_total').data[0], -1.4);
  assert.equal(options.tooltip.y.formatter(-1.4), '1,40 kWh');
  dom.window.close();
});

const hours = Array.from({length: 24}, (_, h) => `${String(h).padStart(2, '0')}:00`);

test('thinLabels dünnt nur aus, wenn die Beschriftungen nicht passen', () => {
  const {dom, factory} = load();
  assert.deepEqual(Array.from(factory.thinLabels(hours, 0)), hours);
  assert.deepEqual(Array.from(factory.thinLabels(hours, 1600)), hours);
  const narrow = Array.from(factory.thinLabels(hours, 400));
  assert.equal(narrow.length, 24);
  assert.equal(narrow[0], '00:00');
  assert.ok(narrow.filter(Boolean).length <= 8, `zu viele Beschriftungen: ${narrow.filter(Boolean).length}`);
  assert.ok(narrow.filter(Boolean).length >= 4);
  dom.window.close();
});

test('chartOptions nennt im Tooltip den vollen Zeitpunkt auch bei ausgedünnter Achse', () => {
  const {dom, factory} = load();
  const result = {...DAY_FIXTURE, buckets: hours.map(h => ({...DAY_FIXTURE.buckets[0], local: `2026-10-15T${h}`}))};
  const options = factory.chartOptions(result, 400);
  const blank = options.xaxis.categories.findIndex(label => label === '');
  assert.ok(blank > 0, 'erwartet mindestens eine leere Beschriftung');
  assert.match(options.tooltip.x.formatter('', {dataPointIndex: blank}), /^\d\d[:.]00/);
  dom.window.close();
});

test('neue Revision laedt den laufenden Zeitraum neu', async () => {
  const {dom, make, calls, server} = load();
  const page = make();
  await page.init();
  calls.length = 0;
  server.revision = 2;
  await page.onRevision({rev: 2});
  assert.equal(calls.length, 1);
  assert.equal(page.data.revision, 2);
  dom.window.close();
});

test('gleiche Revision im Leerlauf laedt nicht', async () => {
  const {dom, make, calls} = load();
  const page = make();
  await page.init();
  calls.length = 0;
  await page.onRevision({rev: 1});
  await page.onRevision({rev: 1});
  assert.equal(calls.length, 0);
  dom.window.close();
});

test('ein vergangener Zeitraum bleibt bei neuer Revision stehen', async () => {
  const {dom, make, calls, server} = load();
  const page = make();
  await page.init();
  page.data = {...page.data, next: '2026-10-06'};
  calls.length = 0;
  server.revision = 3;
  await page.onRevision({rev: 3});
  assert.equal(calls.length, 0);
  dom.window.close();
});

test('registry-updated erreicht onRevision, destroy meldet ab', async () => {
  const {dom, make, calls, server} = load();
  const page = make();
  await page.init();
  calls.length = 0;
  server.revision = 4;
  dom.window.dispatchEvent(new dom.window.CustomEvent('registry-updated', {detail: {energy_stats: {rev: 4}}}));
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(calls.length, 1);
  page.destroy();
  server.revision = 5;
  dom.window.dispatchEvent(new dom.window.CustomEvent('registry-updated', {detail: {energy_stats: {rev: 5}}}));
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(calls.length, 1);
  dom.window.close();
});

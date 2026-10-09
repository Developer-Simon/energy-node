// dashboard/test/history-chart.test.mjs
// Das gemeinsame Diagramm-Modul von Verlaufsseite und Verlaufskachel.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { IDBFactory as FDBFactory, IDBKeyRange as FDBKeyRange } from 'fake-indexeddb';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');
const SOURCES = ['theme.js', 'energy-model.js', 'history-rollup.js', 'history-store.js', 'history-chart.js'].map(read);
const HOUR = 3600 * 1000;
const plain = value => JSON.parse(JSON.stringify(value));

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  installI18n(dom.window);
  dom.window.indexedDB = new FDBFactory();
  dom.window.IDBKeyRange = FDBKeyRange;
  const context = dom.getInternalVMContext();
  SOURCES.forEach(source => vm.runInContext(source, context));
  return {dom, chart: dom.window.HistoryChart};
}

test('rangeBounds rechnet relativ ab jetzt', () => {
  const {dom, chart} = load();
  assert.deepEqual(plain(chart.rangeBounds({range_mode: 'relative', range_hours: 6}, 10 * HOUR)), {from: 4 * HOUR, to: 10 * HOUR});
  dom.window.close();
});

test('rangeBounds nimmt einen festen Bereich unveraendert', () => {
  const {dom, chart} = load();
  assert.deepEqual(plain(chart.rangeBounds({range_mode: 'custom', range_hours: 6, range_from: 1000, range_to: 5000}, 10 * HOUR)), {from: 1000, to: 5000});
  dom.window.close();
});

test('rangeBounds faellt bei unvollstaendigem festen Bereich auf relativ zurueck', () => {
  const {dom, chart} = load();
  assert.deepEqual(plain(chart.rangeBounds({range_mode: 'custom', range_hours: 1, range_from: 1000}, 10 * HOUR)), {from: 9 * HOUR, to: 10 * HOUR});
  dom.window.close();
});

test('rangeLabel benennt Presets wie die Verlaufsseite', () => {
  const {dom, chart} = load();
  assert.equal(chart.rangeLabel({range_mode: 'relative', range_hours: 6}), '6 h');
  assert.equal(chart.rangeLabel({range_mode: 'relative', range_hours: 24}), dom.window.I18n.t('history.range.day'));
  assert.equal(chart.rangeLabel({range_mode: 'relative', range_hours: 48}), '48 h');
  dom.window.close();
});

test('rangeLabel zeigt bei festem Bereich beide Zeitpunkte', () => {
  const {dom, chart} = load();
  const from = Date.UTC(2026, 8, 1, 8, 0);
  const to = Date.UTC(2026, 8, 2, 8, 0);
  const label = chart.rangeLabel({range_mode: 'custom', range_hours: 24, range_from: from, range_to: to});
  const options = {day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit'};
  assert.ok(label.includes(dom.window.I18n.formatDateTime(from, options)), label);
  assert.ok(label.includes(dom.window.I18n.formatDateTime(to, options)), label);
  dom.window.close();
});

test('loadRows names custom category series after their label', async () => {
  const {dom, chart} = load();
  assert.equal(chart.seriesLabel('role:custom:consumer:werkstatt'), 'role:custom:consumer:werkstatt');
  dom.window.fetch = async () => ({ok: true, json: async () => ({categories: {werkstatt: {base: 'consumer', label: 'Werkstatt'}}})});
  const now = Date.now();
  await chart.loadRows({from: now - 6 * HOUR, to: now, interpretation: dom.window.EnergyModel.DEFAULT_INTERPRETATION, recorderConfig: {rawWindowHours: 24, minuteWindowDays: 7}});
  assert.equal(chart.seriesLabel('role:custom:consumer:werkstatt'), 'Werkstatt');
  assert.equal(chart.seriesLabel('role:custom:consumer:unbekannt'), 'role:custom:consumer:unbekannt');
  dom.window.close();
});

test('loadRows waehlt die Rohstufe und leitet den Hausverbrauch ab', async () => {
  const {dom, chart} = load();
  const ts = Date.now() - HOUR;
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts, v: 640, u: 'W'},
    {series: 'role:grid', ts, v: -120, u: 'W'},
  ]);
  const now = Date.now();
  const result = await chart.loadRows({from: now - 6 * HOUR, to: now, interpretation: dom.window.EnergyModel.DEFAULT_INTERPRETATION, recorderConfig: {rawWindowHours: 24, minuteWindowDays: 7}});
  assert.equal(result.tier, 'raw');
  assert.ok(result.rows.some(row => row.series === 'berechnet:hausverbrauch' && row.ts === ts));
  assert.ok(result.gaps instanceof dom.window.Map || typeof result.gaps.get === 'function');
  dom.window.close();
});

const ROWS = [
  {series: 'role:pv', ts: 1000, min: 1, max: 3, avg: 2, n: 1, u: 'W'},
  {series: 'role:battery_soc', ts: 1000, min: 50, max: 50, avg: 50, n: 1, u: '%'},
];

test('buildOptions ohne compact entspricht dem Panel: Werkzeugleiste, Zoom, keine Legende', () => {
  const {dom, chart} = load();
  const options = chart.buildOptions({rows: ROWS, gaps: new dom.window.Map(), aggregate: 'avg', knownSeries: ['role:battery_soc', 'role:pv']});
  assert.equal(options.chart.toolbar.show, true);
  assert.equal(options.chart.zoom.enabled, true);
  assert.equal(options.legend.show, false);
  assert.equal(options.chart.height, 360);
  assert.equal(options.yaxis.length, 2, 'W und % bekommen je eine Achse');
  dom.window.close();
});

test('buildOptions compact: nur lesen, Legende mit Anzeigenamen der echten Serien', () => {
  const {dom, chart} = load();
  const gaps = new dom.window.Map([['role:pv', [{from: 2000, to: 9000}]]]);
  const rows = [...ROWS, {series: 'role:pv', ts: 10000, min: 4, max: 4, avg: 4, n: 1, u: 'W'}];
  const options = chart.buildOptions({rows, gaps, aggregate: 'max', knownSeries: ['role:battery_soc', 'role:pv'], compact: true});
  assert.equal(options.chart.toolbar.show, false);
  assert.equal(options.chart.zoom.enabled, false);
  assert.equal(options.chart.height, '100%');
  assert.equal(options.legend.show, true);
  assert.equal(options.legend.onItemClick.toggleDataSeries, false);
  // Geist-Serien (Luecken-Stummel) tauchen nicht in der Legende auf
  assert.deepEqual(plain(options.legend.customLegendItems), ['role:battery_soc', 'role:pv'].map(name => chart.seriesLabel(name)));
  assert.ok(options.series.length > 2, 'die Luecke erzeugt eine Geist-Serie');
  // Kennwert max: der Punkt bei 1000 zeichnet 3
  const pv = options.series.find(item => item.name === 'role:pv');
  assert.equal(pv.data.find(point => point[0] === 1000)[1], 3);
  dom.window.close();
});

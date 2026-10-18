// Anzeige-Logik der Energiestatistik. Die Antwort kommt fertig gerechnet
// vom Server, geprueft wird nur Beschriftung, Format und Reihenbildung.
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

function loadStatsModel(lang = 'de') {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
  installI18n(dom.window, {lang});
  vm.runInContext(read('theme.js'), context);
  vm.runInContext(read('energy-model.js'), context);
  vm.runInContext(read('energy-stats-model.js'), context);
  return {dom, model: dom.window.EnergyStatsModel};
}


test('formatKWh waehlt die Stellen nach Groesse und wechselt zu MWh', () => {
  const {dom, model} = loadStatsModel();
  assert.equal(model.formatKWh(2.5), '2,50 kWh');
  assert.equal(model.formatKWh(42.25), '42,3 kWh');
  assert.equal(model.formatKWh(512), '512 kWh');
  assert.equal(model.formatKWh(4321), '4,32 MWh');
  assert.equal(model.formatKWh(null), '');
  assert.equal(model.formatKWh(Number.NaN), '');
  dom.window.close();
});

test('kpis liefert kWh und Prozent in der angefragten Reihenfolge', () => {
  const {dom, model} = loadStatsModel();
  const list = model.kpis(DAY_FIXTURE.total, ['consumption', 'pv', 'grid_import', 'grid_export', 'autarky']);
  assert.deepEqual(list.map(k => k.id), ['consumption', 'pv', 'grid_import', 'grid_export', 'autarky']);
  assert.equal(list[0].label, 'Verbrauch');
  assert.equal(list[0].value, '1,40 kWh');
  assert.equal(list[4].value, '93 %');
  const empty = model.kpis({kwh: {}, hours: {}, autarky: null}, ['pv', 'autarky']);
  assert.deepEqual(empty.map(k => k.value), ['', '']);
  dom.window.close();
});

test('coverageParts liefert je Quelle eine Pille mit kind und Grossbuchstaben', () => {
  const {model} = loadStatsModel();
  const parts = JSON.parse(JSON.stringify(model.coverageParts({ha: 20, own: 3, none: 1})));
  assert.deepEqual(parts.map(part => part.kind), ['ha', 'own', 'none']);
  assert.deepEqual(parts.map(part => part.text), ['Aus Home Assistant 20 h', 'Eigene Aufzeichnung 3 h', 'Keine Daten 1 h']);
  assert.deepEqual(JSON.parse(JSON.stringify(model.coverageParts({ha: 0, own: 0, none: 24}))), [{kind: 'empty', text: 'Noch keine Daten'}]);
});

test('coverageText nennt nur vorhandene Quellen und beginnt gross', () => {
  const {dom, model} = loadStatsModel();
  assert.equal(model.coverageText({ha: 20, own: 3, none: 1}), 'Aus Home Assistant 20 h, eigene Aufzeichnung 3 h, keine Daten 1 h');
  assert.equal(model.coverageText({ha: 0, own: 5, none: 0}), 'Eigene Aufzeichnung 5 h');
  assert.equal(model.coverageText({ha: 0, own: 0, none: 24}), 'Noch keine Daten');
  assert.equal(model.coverageText(undefined), 'Noch keine Daten');
  dom.window.close();
});

test('bucketLabel liest die Ortszeit des Pi aus local, nicht aus start', () => {
  const {dom, model} = loadStatsModel();
  // start waere in UTC 12:00, die Beschriftung muss 14:00 sein.
  assert.match(model.bucketLabel('day', '2026-10-12T14:00'), /^14[:.]00/);
  assert.equal(model.bucketLabel('month', '2026-10-07T00:00'), '7');
  assert.match(model.bucketLabel('year', '2026-03-01T00:00'), /^März|^Mär/);
  assert.match(model.bucketLabel('week', '2026-10-12T00:00'), /Mo/);
  dom.window.close();
});

test('periodTitle beschriftet jeden Zeitraum', () => {
  const {dom, model} = loadStatsModel();
  assert.match(model.periodTitle({period: 'day', at: '2026-10-12'}), /Montag.*12.*Oktober.*2026/);
  assert.equal(model.periodTitle({period: 'week', at: '2026-10-12'}), 'Woche ab 12.10.2026');
  assert.equal(model.periodTitle({period: 'month', at: '2026-10'}), 'Oktober 2026');
  assert.equal(model.periodTitle({period: 'year', at: '2026'}), '2026');
  dom.window.close();
});

test('roleMeta beschriftet custom-Rollen aus categories und faellt sonst auf die id zurueck', () => {
  const {dom, model} = loadStatsModel();
  assert.equal(model.roleMeta('custom:sauna', DAY_FIXTURE.categories).label, 'Sauna');
  assert.equal(model.roleMeta('custom:unbekannt', {}).label, 'unbekannt');
  assert.equal(model.roleMeta('pv', {}).label, 'PV');
  assert.ok(model.roleMeta('pv', {}).color);
  dom.window.close();
});

test('chartSeries legt Erzeugung nach oben und Verbrauch gespiegelt nach unten, Luecken bleiben null', () => {
  const {dom, model} = loadStatsModel();
  const series = model.chartSeries(DAY_FIXTURE);
  assert.deepEqual(JSON.parse(JSON.stringify(series.map(s => s.role))), ['pv', 'grid_import', 'load_total', 'grid_export']);
  const pv = series.find(s => s.role === 'pv');
  assert.deepEqual(JSON.parse(JSON.stringify(pv.data)), [2.5, null]);
  const load = series.find(s => s.role === 'load_total');
  assert.deepEqual(JSON.parse(JSON.stringify(load.data)), [-1.4, null]);
  dom.window.close();
});

test('tableRows ordnet feste Rollen zuerst und custom dahinter', () => {
  const {dom, model} = loadStatsModel();
  const rows = model.tableRows(DAY_FIXTURE);
  assert.deepEqual(JSON.parse(JSON.stringify(rows.map(r => r.role))), ['pv', 'grid_import', 'grid_export', 'load_total', 'custom:sauna']);
  assert.deepEqual(JSON.parse(JSON.stringify(rows[0].values)), ['2,50 kWh', '']);
  assert.equal(rows[4].label, 'Sauna');
  assert.equal(rows[4].total, '0,80 kWh');
  dom.window.close();
});

test('Einspeisung und Batterie lädt sind schraffiert, ihre Gegenrichtung nicht', () => {
  const {dom, model} = loadStatsModel();
  assert.deepEqual(Array.from(model.HATCHED_ROLES), ['grid_export', 'battery_charge']);
  const series = model.chartSeries(DAY_FIXTURE);
  assert.equal(series.find(s => s.role === 'grid_export').hatched, true);
  assert.equal(series.find(s => s.role === 'grid_import').hatched, false);
  const rows = model.tableRows(DAY_FIXTURE);
  assert.equal(rows.find(r => r.role === 'grid_export').hatched, true);
  assert.equal(rows.find(r => r.role === 'pv').hatched, false);
  dom.window.close();
});

test('fetchStats baut die URL mit Basispfad und laesst at weg, wenn leer', async () => {
  const {dom, model} = loadStatsModel();
  const urls = [];
  dom.window.__DASHBOARD_BASE_PATH__ = '/node';
  dom.window.fetch = async url => { urls.push(url); return {ok: true, status: 200, json: async () => DAY_FIXTURE}; };
  await model.fetchStats('day', '');
  await model.fetchStats('month', '2026-09');
  assert.deepEqual(urls, ['/node/api/v1/energy/stats?period=day', '/node/api/v1/energy/stats?period=month&at=2026-09']);
  dom.window.close();
});

test('chartOptions zeichnet fuer die Kachel niedriger und ohne Legende', () => {
  const {dom, model} = loadStatsModel();
  const page = model.chartOptions(DAY_FIXTURE, 0);
  assert.equal(page.chart.height, 340);
  assert.equal(page.legend.show, true);
  assert.equal(page.legend.position, 'bottom');
  const card = model.chartOptions(DAY_FIXTURE, 0, {height: 180, legend: false});
  assert.equal(card.chart.height, 180);
  assert.equal(card.legend.show, false);
  dom.window.close();
});

test('KPI_IDS nennt alle acht Kennzahlen in fester Reihenfolge', () => {
  const {dom, model} = loadStatsModel();
  assert.deepEqual([...model.KPI_IDS], ['consumption', 'pv', 'grid_import', 'grid_export', 'battery_charge', 'battery_discharge', 'autarky', 'self_consumption']);
  dom.window.close();
});

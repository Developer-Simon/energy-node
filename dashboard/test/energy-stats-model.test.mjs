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

test('coverageParts setzt den Ersatzwert direkt hinter die eigene Aufzeichnung', () => {
  const {model} = loadStatsModel();
  const parts = JSON.parse(JSON.stringify(model.coverageParts({own: 5, substitute: 2, none: 1})));
  assert.deepEqual(parts.map(part => part.kind), ['own', 'substitute', 'none']);
  assert.equal(parts[1].text, 'Davon 2 h mit Ersatzwert');
});

test('coverageText nennt den Ersatzwert, ohne ihn bleibt die Ausgabe gleich', () => {
  const {dom, model} = loadStatsModel();
  assert.equal(model.coverageText({own: 5, substitute: 2, none: 1}), 'Eigene Aufzeichnung 5 h, davon 2 h mit Ersatzwert, keine Daten 1 h');
  assert.equal(model.coverageText({ha: 20, own: 3, none: 1}), 'Aus Home Assistant 20 h, eigene Aufzeichnung 3 h, keine Daten 1 h');
  dom.window.close();
});

const CHAIN_STATS_FIXTURE = {
  period: 'day',
  at: '2026-10-12',
  chains: [
    {
      id: 'a', label: 'Akku',
      buckets: [
        {loss_kwh: 0.4, standby_kwh: 0, efficiency: 0.9, has_data: true},
        {loss_kwh: 0, standby_kwh: 0, efficiency: null, has_data: false},
      ],
      total: {loss_kwh: 1.25, standby_kwh: 0, efficiency: 0.93, has_data: true},
    },
    {
      id: 'b', label: 'Wallbox',
      buckets: [
        {loss_kwh: 0.1, standby_kwh: 0, efficiency: null, has_data: true},
        {loss_kwh: 0.05, standby_kwh: 0, efficiency: null, has_data: true},
      ],
      total: {loss_kwh: 0.15, standby_kwh: 0, efficiency: null, has_data: true},
    },
    {
      id: 'c', label: 'Pumpe',
      buckets: [
        {loss_kwh: 0.2, standby_kwh: 0, efficiency: 0.8, has_data: false},
        {loss_kwh: 0.3, standby_kwh: 0, efficiency: 0.8, has_data: false},
      ],
      total: {loss_kwh: 0.5, standby_kwh: 0, efficiency: 0.8, has_data: false},
    },
  ],
};

test('chartSeries adds losses below zero only with lossSeries', () => {
  const {dom, model} = loadStatsModel();
  const plain = model.chartSeries(DAY_FIXTURE);
  assert.equal(plain.some(s => s.role === 'chain_loss'), false);
  assert.deepEqual(JSON.parse(JSON.stringify(plain.map(s => s.role))), ['pv', 'grid_import', 'load_total', 'grid_export']);
  const series = model.chartSeries({...DAY_FIXTURE, lossSeries: [0.2, null]});
  const loss = series.find(s => s.role === 'chain_loss');
  assert.equal(loss.name, 'Verluste');
  assert.deepEqual(JSON.parse(JSON.stringify(loss.data)), [-0.2, null]);
  assert.equal(loss.color, dom.window.DashboardTheme.color('warn'));
  dom.window.close();
});

test('withLosses adds the chains per bucket and leaves result untouched', () => {
  const {dom, model} = loadStatsModel();
  const bucket = local => ({local, kwh: {}});
  const result = {period: 'day', at: '2026-10-12', buckets: [bucket('2026-10-12T00:00'), bucket('2026-10-12T01:00'), bucket('2026-10-12T02:00')], total: {kwh: {}}};
  const chainStats = {chains: [
    {id: 'a', label: 'A', buckets: [
      {loss_kwh: 0.1, has_data: true}, {loss_kwh: 0.07, has_data: false}, {loss_kwh: 0.5, has_data: false},
    ], total: {loss_kwh: 0.6, has_data: true}},
    {id: 'b', label: 'B', buckets: [
      {loss_kwh: 0.2, has_data: true}, {loss_kwh: 0.05, has_data: true}, {loss_kwh: 0, has_data: false},
    ], total: {loss_kwh: 0.25, has_data: true}},
  ]};
  const withLoss = model.withLosses(result, chainStats);
  assert.notEqual(withLoss, result);
  assert.deepEqual(JSON.parse(JSON.stringify(withLoss.lossSeries)), [0.3, 0.05, null]);
  assert.equal(result.lossSeries, undefined);
  assert.equal(withLoss.buckets, result.buckets);
  dom.window.close();
});

test('lossKpis', () => {
  const {dom, model} = loadStatsModel();
  const list = model.lossKpis(CHAIN_STATS_FIXTURE);
  assert.equal(list.length, 6);
  assert.deepEqual(list.slice(0, 2).map(k => k.id), ['chain:a:loss', 'chain:a:efficiency']);
  assert.deepEqual(JSON.parse(JSON.stringify(list[0])), {id: 'chain:a:loss', label: 'Verlust Akku', value: '1,25 kWh'});
  assert.deepEqual(JSON.parse(JSON.stringify(list[1])), {id: 'chain:a:efficiency', label: 'Wirkungsgrad Akku', value: '93 %'});
  assert.equal(list[2].label, 'Verlust Wallbox');
  assert.equal(list[2].value, '0,15 kWh');
  assert.equal(list[3].label, 'Wirkungsgrad Wallbox');
  assert.equal(list[3].value, '');
  assert.equal(list[4].value, '');
  assert.equal(list[5].value, '');
  dom.window.close();
});

test('lossRows', () => {
  const {dom, model} = loadStatsModel();
  const rows = model.lossRows(CHAIN_STATS_FIXTURE);
  assert.deepEqual(JSON.parse(JSON.stringify(rows.map(r => r.role))), ['chain:a:loss', 'chain:a:efficiency', 'chain:b:loss', 'chain:b:efficiency', 'chain:c:loss', 'chain:c:efficiency']);
  const loss = rows[0];
  assert.equal(loss.label, 'Verlust Akku');
  assert.deepEqual(JSON.parse(JSON.stringify(loss.values)), ['0,40 kWh', '']);
  assert.equal(loss.total, '1,25 kWh');
  assert.equal(loss.hatched, false);
  const efficiency = rows[1];
  assert.equal(efficiency.label, 'Wirkungsgrad Akku');
  assert.deepEqual(JSON.parse(JSON.stringify(efficiency.values)), ['90 %', '']);
  assert.equal(efficiency.total, '93 %');
  assert.deepEqual(JSON.parse(JSON.stringify(rows[2].values)), ['0,10 kWh', '0,05 kWh']);
  assert.deepEqual(JSON.parse(JSON.stringify(rows[4].values)), ['', '']);
  assert.equal(rows[5].total, '');
  dom.window.close();
});

test('fetchChainStats fragt die Ketten mit period und at ab', async () => {
  const {dom, model} = loadStatsModel();
  const urls = [];
  dom.window.fetch = async url => { urls.push(url); return {ok: true, status: 200, json: async () => CHAIN_STATS_FIXTURE}; };
  await model.fetchChainStats('month', '2026-09');
  assert.deepEqual(urls, ['/api/v1/energy/stats/chains?period=month&at=2026-09']);
  dom.window.close();
});

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

// chains: Antwort fuer /stats/chains, eine Funktion (params) bekommt die
// Suchparameter, 'fail' simuliert einen Fehler des Servers.
// Antwort von GET /api/v1/energy/stats/chains fuer DAY_FIXTURE, zwei Buckets.
const chainsWithLoss = loss => ({
  period: 'day', at: '2026-10-12', revision: 1,
  chains: [{
    id: 'wp', label: 'Wärmepumpe',
    buckets: [{loss_kwh: loss, standby_kwh: 0, efficiency: 0.8, source_kwh: 1, has_data: true}, {loss_kwh: 0, standby_kwh: 0, efficiency: null, source_kwh: 0, has_data: false}],
    total: {loss_kwh: loss, standby_kwh: 0, efficiency: 0.8, source_kwh: 1, has_data: true},
  }],
});
const CHAINS = chainsWithLoss(0.2);
const WITH_CHAINS = {...DAY_FIXTURE, has_chains: true};

function load(responses = {}, {chains = CHAINS} = {}) {
  const dom = new JSDOM('<!doctype html><html><body><div id="chart"></div></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = {data: (_name, fn) => { factory = fn; }};
  installI18n(dom.window);
  const calls = [];
  const server = {revision: 1};
  dom.window.fetch = async url => {
    calls.push(url);
    const parsed = new URL(url, 'http://x');
    const params = parsed.searchParams;
    if (parsed.pathname.endsWith('/stats/chains')) {
      const body = typeof chains === 'function' ? await chains(params) : chains;
      if (body === 'fail') return {ok: false, status: 500, json: async () => ({message: 'boom'})};
      return {ok: true, status: 200, json: async () => body};
    }
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

test('chartOptions füllt schraffierte Rollen mit Muster in ihrer Farbe', () => {
  const {dom, factory} = load();
  const options = factory.chartOptions(DAY_FIXTURE);
  const index = options.series.findIndex(s => s.role === 'grid_export');
  assert.equal(options.fill.type[index], 'pattern');
  assert.equal(options.fill.type[options.series.findIndex(s => s.role === 'grid_import')], 'solid');
  assert.equal(options.fill.pattern.style, 'slantedLines');
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

test('laufender Zeitraum waehlt den juengsten Abschnitt mit Werten', async () => {
  const {dom, make} = load();
  const page = make();
  await page.init();
  assert.equal(page.selected, 0);
  assert.equal(page.selectedLabel, page.labels[0]);
  dom.window.close();
});

test('vergangener Zeitraum beginnt beim ersten Abschnitt, Blaettern behaelt die Wahl', async () => {
  const past = {...DAY_FIXTURE, at: '2026-10-11', prev: '2026-10-10', next: '2026-10-12'};
  const {dom, make} = load({'day|2026-10-11': past, 'day|2026-10-10': {...past, at: '2026-10-10', next: '2026-10-11'}, 'week|': {...DAY_FIXTURE, period: 'week'}});
  const page = make();
  page.focus({period: 'day', at: '2026-10-11'});
  await page.load();
  assert.equal(page.selected, 0);
  page.selectBucket(1);
  await page.prev();
  assert.equal(page.selected, 1);
  page.selectBucket(9);
  assert.equal(page.selected, 1, 'ausserhalb der Abschnitte bleibt die Wahl');
  await page.selectPeriod('week');
  assert.equal(page.selected, 0);
  dom.window.close();
});

test('Tabellenansicht wird im Browser gemerkt', async () => {
  const {dom, make} = load();
  const page = make();
  await page.init();
  assert.equal(page.view, 'bucket');
  page.setView('grid');
  assert.equal(dom.window.localStorage.getItem('stats.table_view'), 'grid');
  assert.equal(make().view, 'grid');
  dom.window.close();
});

test('Antippen eines Balkens waehlt den Abschnitt', async () => {
  const {dom, make, charts} = load();
  const page = make();
  await page.init();
  charts.at(-1).options.chart.events.click(null, null, {dataPointIndex: 1});
  assert.equal(page.selected, 1);
  charts.at(-1).options.chart.events.click(null, null, {dataPointIndex: -1});
  assert.equal(page.selected, 1);
  dom.window.close();
});

test('Antippen neben dem Balken waehlt die Spalte unter dem Finger', async () => {
  const {dom, make, charts} = load();
  const page = make();
  await page.init();
  const chart = charts.at(-1);
  chart.w = {globals: {gridWidth: 200, gridHeight: 100, translateX: 50, translateY: 10}};
  page.$refs.chart.getBoundingClientRect = () => ({left: 0, top: 0});
  chart.options.chart.events.click({clientX: 160}, null, {dataPointIndex: null});
  assert.equal(page.selected, 1);
  chart.options.chart.events.click({clientX: 10}, null, {dataPointIndex: null});
  assert.equal(page.selected, 1, 'links der Zeichenflaeche bleibt die Wahl');
  dom.window.close();
});

test('Rasterzellen tragen eine kompakte Zahl ohne Einheit', async () => {
  const {dom, make} = load();
  const page = make();
  await page.init();
  const pv = page.rows.find(row => row.role === 'pv');
  assert.equal(pv.values[0], '2,50 kWh');
  assert.equal(pv.compact[0], '2,5');
  assert.equal(pv.compact[1], '');
  dom.window.close();
});

const isChainCall = url => url.includes('/stats/chains');

test('ohne has_chains gibt es keinen Verlustschalter und keine zweite Anfrage', async () => {
  const {dom, make, calls} = load();
  dom.window.localStorage.setItem('stats.show_losses', 'true');
  const panel = make();
  await panel.init();
  assert.equal(panel.hasChains, false);
  assert.equal(calls.some(isChainCall), false);
  assert.equal(panel.kpis.some(kpi => kpi.id.startsWith('chains:')), false);
  dom.window.close();
});

test('Verlustschalter ist standardmaessig aus: keine zweite Anfrage, Kennzahlen und Tabelle wie bisher', async () => {
  const {dom, make, calls} = load({'day|': WITH_CHAINS});
  const panel = make();
  await panel.init();
  assert.equal(panel.hasChains, true);
  assert.equal(panel.showLosses, false);
  assert.equal(calls.some(isChainCall), false);
  assert.equal(panel.kpis.some(kpi => kpi.id.startsWith('chains:')), false);
  assert.equal(panel.rows.length, 5);
  dom.window.close();
});

test('Schalter an ergaenzt Kennzahlen, Zeilen und Diagramm und merkt sich den Zustand', async () => {
  const {dom, make, calls, charts} = load({'day|': WITH_CHAINS});
  const panel = make();
  await panel.init();
  await panel.setShowLosses(true);
  assert.equal(calls.at(-1), '/api/v1/energy/stats/chains?period=day');
  assert.equal(panel.kpis.find(kpi => kpi.id === 'chains:loss').value, '0,20 kWh');
  assert.equal(panel.kpis.find(kpi => kpi.id === 'chains:efficiency').value, '80 %');
  // Verluste vor den Rollen, Kennzahlen und Zeilen.
  assert.deepEqual(JSON.parse(JSON.stringify(panel.kpis.slice(0, 2).map(kpi => kpi.id))), ['chains:loss', 'chains:efficiency']);
  assert.deepEqual(JSON.parse(JSON.stringify(panel.rows.slice(0, 2).map(row => row.role))), ['chains:loss', 'chains:efficiency']);
  assert.equal(panel.rows.length, 7);
  assert.equal(charts.at(-1).options.series.find(s => s.role === 'chain_loss').data[0], -0.2);
  assert.equal(dom.window.localStorage.getItem('stats.show_losses'), 'true');
  await panel.setShowLosses(false);
  assert.equal(panel.kpis.some(kpi => kpi.id.startsWith('chains:')), false);
  assert.equal(panel.rows.length, 5);
  assert.equal(dom.window.localStorage.getItem('stats.show_losses'), 'false');
  dom.window.close();
});

test('gespeicherter Schalter laedt beim Start Statistik und dann die Verluste nacheinander', async () => {
  const {dom, make, calls} = load({'day|': WITH_CHAINS});
  dom.window.localStorage.setItem('stats.show_losses', 'true');
  const panel = make();
  await panel.init();
  assert.deepEqual(calls, ['/api/v1/energy/stats?period=day', '/api/v1/energy/stats/chains?period=day']);
  assert.equal(panel.showLosses, true);
  assert.ok(panel.kpis.some(kpi => kpi.id === 'chains:loss'));
  dom.window.close();
});

test('eine alte zweite Antwort nach dem Blaettern wird verworfen', async () => {
  const past = {...WITH_CHAINS, at: '2026-10-11', prev: '2026-10-10', next: '2026-10-12'};
  let release;
  const pending = new Promise(resolve => { release = resolve; });
  const {dom, make, calls} = load(
    {'day|': WITH_CHAINS, 'day|2026-10-11': past},
    {chains: params => (params.get('at') === '2026-10-11' ? chainsWithLoss(0.5) : pending.then(() => chainsWithLoss(9)))},
  );
  const panel = make();
  await panel.init();
  const toggled = panel.setShowLosses(true);
  await panel.prev();
  release();
  await toggled;
  assert.equal(panel.at, '2026-10-11');
  assert.equal(panel.kpis.find(kpi => kpi.id === 'chains:loss').value, '0,50 kWh');
  assert.equal(calls.filter(isChainCall).length, 2);
  dom.window.close();
});

test('scheitert die zweite Anfrage, bleibt die normale Statistik stehen und lossError erscheint', async () => {
  const {dom, make} = load({'day|': WITH_CHAINS}, {chains: 'fail'});
  const panel = make();
  await panel.init();
  await panel.setShowLosses(true);
  assert.equal(panel.error, '');
  assert.equal(panel.kpis.length, 8);
  assert.equal(panel.rows.length, 5);
  assert.equal(panel.lossError, 'Die Verluste konnten nicht geladen werden.');
  await panel.setShowLosses(false);
  assert.equal(panel.lossError, '');
  dom.window.close();
});

test('Antwort mit anderer Bucket-Anzahl zeichnet keine Verluste', async () => {
  const threeBuckets = {
    period: 'day', at: '2026-10-12', revision: 1,
    chains: [{
      id: 'wp', label: 'Wärmepumpe',
      buckets: [0, 1, 2].map(() => ({loss_kwh: 0.1, standby_kwh: 0, efficiency: null, has_data: true})),
      total: {loss_kwh: 0.3, standby_kwh: 0, efficiency: null, has_data: true},
    }],
  };
  const {dom, make, charts} = load({'day|': WITH_CHAINS}, {chains: threeBuckets});
  const panel = make();
  await panel.init();
  await panel.setShowLosses(true);
  assert.equal(charts.at(-1).options.series.some(s => s.role === 'chain_loss'), false);
  assert.equal(panel.rows.some(row => row.role.startsWith('chains:')), false);
  dom.window.close();
});

test('Verlustzeilen tragen die kompakte Zahl, Wirkungsgrad als Prozent', async () => {
  const {dom, make} = load({'day|': WITH_CHAINS});
  const panel = make();
  await panel.init();
  await panel.setShowLosses(true);
  const loss = panel.rows.find(row => row.role === 'chains:loss');
  assert.equal(loss.values[0], '0,20 kWh');
  assert.equal(loss.compact[0], '0,2');
  assert.equal(loss.compact[1], '');
  const efficiency = panel.rows.find(row => row.role === 'chains:efficiency');
  assert.equal(efficiency.values[0], '80 %');
  assert.equal(efficiency.compact[0], '80 %');
  dom.window.close();
});

test('mehrere Ketten ergeben je eine Verlust- und eine Wirkungsgradzeile', async () => {
  const two = {
    period: 'day', at: '2026-10-12', revision: 1,
    chains: [
      {id: 'wp', label: 'Wärmepumpe', buckets: [{loss_kwh: 0.2, standby_kwh: 0, efficiency: 0.8, source_kwh: 1, has_data: true}, {loss_kwh: 0, standby_kwh: 0, efficiency: null, source_kwh: 0, has_data: false}],
        total: {loss_kwh: 0.2, standby_kwh: 0, efficiency: 0.8, source_kwh: 1, has_data: true}},
      {id: 'ww', label: 'Warmwasser', buckets: [{loss_kwh: 0.1, standby_kwh: 0, efficiency: 0.5, source_kwh: 1, has_data: true}, {loss_kwh: 0, standby_kwh: 0, efficiency: null, source_kwh: 0, has_data: false}],
        total: {loss_kwh: 0.1, standby_kwh: 0, efficiency: 0.5, source_kwh: 1, has_data: true}},
    ],
  };
  const {dom, make, charts} = load({'day|': WITH_CHAINS}, {chains: two});
  const panel = make();
  await panel.init();
  await panel.setShowLosses(true);
  assert.equal(panel.kpis.filter(kpi => kpi.id.startsWith('chains:')).length, 2);
  assert.equal(panel.kpis.find(kpi => kpi.id === 'chains:loss').value, '0,30 kWh');
  assert.equal(panel.kpis.find(kpi => kpi.id === 'chains:efficiency').value, '65 %');
  assert.equal(panel.rows.filter(row => row.role.startsWith('chains:')).length, 2);
  assert.equal(panel.rows.find(row => row.role === 'chains:loss').values[0], '0,30 kWh');
  assert.equal(charts.at(-1).options.series.filter(s => s.role === 'chain_loss').length, 1);
  assert.equal(charts.at(-1).options.series[0].role, 'chain_loss');
  dom.window.close();
});

test('scheitert die normale Statistik beim Blaettern, verschwindet ein alter lossError', async () => {
  const {dom, make} = load({'day|': WITH_CHAINS}, {chains: 'fail'});
  const panel = make();
  await panel.init();
  await panel.setShowLosses(true);
  assert.equal(panel.lossError, 'Die Verluste konnten nicht geladen werden.');
  const healthy = dom.window.fetch;
  dom.window.fetch = async url => (isChainCall(url)
    ? healthy(url)
    : {ok: false, status: 500, json: async () => ({message: 'Statistik nicht erreichbar'})});
  await panel.prev();
  assert.equal(panel.error, 'Statistik nicht erreichbar');
  assert.equal(panel.lossError, '');
  dom.window.close();
});

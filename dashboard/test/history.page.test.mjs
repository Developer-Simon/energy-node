// dashboard/test/history.page.test.mjs
// Das Verlaeufe-Panel. Geprueft wird die Bruecke zwischen Speicher und
// Chart - Stufenwahl, Punktdeckelung, Serienfarben, Beschriftung -, nicht
// die Zeichenarbeit von ApexCharts selbst: ApexCharts vermisst SVG-Knoten,
// was jsdom nicht leistet. window.ApexCharts wird deshalb durch ein Double
// ersetzt, das die uebergebene Konfiguration festhaelt.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { IDBFactory as FDBFactory, IDBKeyRange as FDBKeyRange } from 'fake-indexeddb';
import { attachStores } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');
const sources = ['theme.js', 'history-rollup.js', 'history-store.js', 'history.js'].map(read);
// energy-model.js wird nur geladen, wenn ein Test die abgeleitete
// Hausverbrauch-Serie prueft - ein eigener Test deckt gerade den Fall ab,
// dass das Panel auch ohne dieses Skript funktionieren muss (siehe
// history.js's window.EnergyModel-Guard).
const energyModelSource = read('energy-model.js');

const HOUR = 3600 * 1000;

function load({withEnergyModel = false} = {}) {
  const dom = new JSDOM('<!doctype html><html><body><div id="chart"></div></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
  dom.window.indexedDB = new FDBFactory();
  dom.window.IDBKeyRange = FDBKeyRange;
  const charts = [];
  dom.window.ApexCharts = class {
    constructor(element, options) { this.element = element; this.options = options; charts.push(this); }
    render() { this.rendered = true; return Promise.resolve(); }
    updateOptions(options) { this.options = {...this.options, ...options}; }
    destroy() { this.destroyed = true; }
  };
  dom.window.dashboardHistorizer = {
    config: () => ({rawWindowHours: 24, minuteWindowDays: 7, intervalSeconds: 10, retentionMode: 'time', retentionHours: 6}),
    status: () => ({paused: false, persisted: true, reason: ''}),
  };
  let factory;
  dom.window.Alpine = {data: (_name, fn) => { factory = fn; }};
  const scripts = withEnergyModel ? [sources[0], energyModelSource, ...sources.slice(1)] : sources;
  scripts.forEach(source => vm.runInContext(source, context));
  return {dom, factory, charts};
}

function panel(options) {
  const {dom, factory, charts} = load(options);
  const component = factory();
  attachStores(component);
  component.$refs = {chart: dom.window.document.getElementById('chart')};
  return {dom, component, charts};
}

test('load waehlt bei kurzem Zeitraum die Rohstufe', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([{series: 'role:pv', ts: now - HOUR, v: 640, u: 'W'}]);
  component.rangeHours = 6;
  await component.load();
  assert.equal(component.tier, 'raw');
  assert.match(component.tierLabel, /Rohdaten/);
  dom.window.close();
});

test('load waehlt bei langem Zeitraum die Fuenf-Minuten-Stufe', async () => {
  const {dom, component} = panel();
  component.rangeHours = 30 * 24;
  await component.load();
  assert.equal(component.tier, '5m');
  assert.match(component.tierLabel, /Fünf-Minuten|5-Minuten/);
  dom.window.close();
});

test('chartOptions baut eine Reihe je ausgewaehlter Serie', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts: now - HOUR, v: 640, u: 'W'},
    {series: 'role:grid', ts: now - HOUR, v: -120, u: 'W'},
  ]);
  await component.load();
  component.selectedSeries = ['role:pv', 'role:grid'];
  const options = component.chartOptions;
  assert.equal(options.series.length, 2);
  assert.deepEqual(JSON.parse(JSON.stringify(options.series.map(item => item.name).sort())), ['role:grid', 'role:pv']);
  assert.equal(options.chart.type, 'line');
  // Auf einem Pi ist jede Animation verschenkte Rechenzeit.
  assert.equal(options.chart.animations.enabled, false);
  assert.equal(options.markers.size, 0, 'die Linie bleibt markerlos');
  dom.window.close();
});

test('chartOptions faerbt Energie-Rollen wie ihre Kachel in der Uebersicht', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([{series: 'role:pv', ts: now - HOUR, v: 640, u: 'W'}]);
  await component.load();
  component.selectedSeries = ['role:pv'];
  assert.equal(component.chartOptions.colors[0], dom.window.DashboardTheme.color('flow-pv'));
  dom.window.close();
});

test('load leitet die Hausverbrauch-Serie aus den Rollen-Verlaeufen ab', async () => {
  const {dom, component} = panel({withEnergyModel: true});
  const now = Date.now();
  const ts = now - HOUR;
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts, v: 640, u: 'W'},
    {series: 'role:grid', ts, v: -120, u: 'W'},
  ]);
  component.interpretation = dom.window.EnergyModel.DEFAULT_INTERPRETATION;
  await component.load();

  const row = component.rows.find(item => item.series === 'berechnet:hausverbrauch' && item.ts === ts);
  assert.ok(row, 'die abgeleitete Serie fehlt');
  const snapshot = dom.window.EnergyModel.snapshotFromPoint({pv: 640, grid: -120});
  const expected = dom.window.EnergyModel.deriveBalanceCore(snapshot, component.interpretation).load_total;
  assert.equal(row.avg, expected);
  assert.equal(row.min, expected);
  assert.equal(row.max, expected);
  assert.equal(row.u, 'W');
  assert.ok(component.seriesOptions.includes('berechnet:hausverbrauch'));
  dom.window.close();
});

test('load funktioniert ohne window.EnergyModel, nur ohne die berechnete Serie', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([{series: 'role:pv', ts: now - HOUR, v: 640, u: 'W'}]);
  await component.load();
  assert.equal(component.error, '');
  assert.ok(!component.seriesOptions.includes('berechnet:hausverbrauch'));
  dom.window.close();
});

test('loadInterpretation laedt die Energie-Einstellung und leitet danach neu ab', async () => {
  const {dom, component} = panel({withEnergyModel: true});
  const now = Date.now();
  const ts = now - HOUR;
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts, v: 640, u: 'W'},
    {series: 'role:load', ts, v: 500, u: 'W'},
  ]);
  await component.load();
  const interpretation = {gap_mode: 'unknown_consumer', load_mode: 'measured', gap_tolerance_mode: 'absolute', gap_tolerance_w: 25, gap_tolerance_percent: 2};
  dom.window.fetch = () => Promise.resolve({ok: true, json: () => Promise.resolve(interpretation)});
  await component.loadInterpretation();

  assert.equal(component.interpretation.load_mode, 'measured');
  const row = component.rows.find(item => item.series === 'berechnet:hausverbrauch' && item.ts === ts);
  const snapshot = dom.window.EnergyModel.snapshotFromPoint({pv: 640, load: 500});
  const expected = dom.window.EnergyModel.deriveBalanceCore(snapshot, interpretation).load_total;
  assert.equal(row.avg, expected, 'die neu geladene Einstellung muss in die Ableitung einfliessen');
  dom.window.close();
});

test('loadInterpretation faellt bei Fehler auf null zurueck', async () => {
  const {dom, component} = panel({withEnergyModel: true});
  dom.window.fetch = () => Promise.reject(new Error('boom'));
  component.interpretation = {load_mode: 'measured'};
  await component.loadInterpretation();
  assert.equal(component.interpretation, null);
  dom.window.close();
});

test('chartOptions faerbt die berechnete Hausverbrauch-Serie wie role:load', async () => {
  const {dom, component} = panel();
  component.rows = [{series: 'berechnet:hausverbrauch', ts: 1, min: 500, max: 500, avg: 500, n: 1, u: 'W'}];
  component.selectedSeries = ['berechnet:hausverbrauch'];
  assert.equal(component.chartOptions.colors[0], dom.window.DashboardTheme.color('flow-load'));
  dom.window.close();
});

test('chartOptions nimmt fuer Nicht-Rollen-Serien die zyklische Serienpalette', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([{series: 'sensor:werkstatt/temp', ts: now - HOUR, v: 21, u: '°C'}]);
  await component.load();
  component.selectedSeries = ['sensor:werkstatt/temp'];
  assert.equal(component.chartOptions.colors[0], dom.window.DashboardTheme.color('series-1'));
  dom.window.close();
});

test('chartOptions behaelt eine einzelne Y-Achse, solange alle Serien dieselbe Einheit haben', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts: now - HOUR, v: 6400, u: 'W'},
    {series: 'role:grid', ts: now - HOUR, v: -120, u: 'W'},
  ]);
  await component.load();
  component.selectedSeries = ['role:pv', 'role:grid'];
  const yaxis = component.chartOptions.yaxis;
  assert.ok(!Array.isArray(yaxis), 'eine Einheit -> ein Achsenobjekt');
  assert.match(yaxis.labels.formatter(6400), /6400\s*W/);
  dom.window.close();
});

test('chartOptions gibt der Prozent-Serie eine eigene rechte Y-Achse mit fester 0-100-Skala', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts: now - HOUR, v: 6400, u: 'W'},
    {series: 'role:battery_soc', ts: now - HOUR, v: 55, u: '%'},
  ]);
  await component.load();
  component.selectedSeries = ['role:pv', 'role:battery_soc'];
  const yaxis = component.chartOptions.yaxis;
  assert.ok(Array.isArray(yaxis), 'gemischte Einheiten -> eine Achse je Einheit');
  assert.equal(yaxis.length, 2);
  const wAxis = yaxis.find(axis => !axis.opposite);
  const pctAxis = yaxis.find(axis => axis.opposite);
  assert.ok(wAxis && pctAxis, 'eine linke W-Achse, eine rechte %-Achse');
  assert.equal(pctAxis.min, 0);
  assert.equal(pctAxis.max, 100);
  assert.match(pctAxis.labels.formatter(55), /55\s*%/);
  assert.match(wAxis.labels.formatter(6400), /6400\s*W/);
  assert.ok([].concat(pctAxis.seriesName).includes('role:battery_soc'), 'die %-Achse haengt an der SoC-Serie');
  assert.ok([].concat(wAxis.seriesName).includes('role:pv'), 'die W-Achse haengt an den Leistungsserien');
  dom.window.close();
});

test('eine allein gezeigte Prozent-Serie bekommt ebenfalls die feste 0-100-Skala', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([{series: 'role:battery_soc', ts: now - HOUR, v: 55, u: '%'}]);
  await component.load();
  component.selectedSeries = ['role:battery_soc'];
  const yaxis = component.chartOptions.yaxis;
  assert.ok(!Array.isArray(yaxis));
  assert.equal(yaxis.min, 0);
  assert.equal(yaxis.max, 100);
  dom.window.close();
});

test('der Tooltip beschriftet jeden Wert mit der Einheit seiner eigenen Serie', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts: now - HOUR, v: 6400, u: 'W'},
    {series: 'role:battery_soc', ts: now - HOUR, v: 55, u: '%'},
  ]);
  await component.load();
  component.selectedSeries = ['role:pv', 'role:battery_soc'];
  const options = component.chartOptions;
  const w = {globals: {seriesNames: options.series.map(item => item.name)}};
  const indexOf = name => options.series.findIndex(item => item.name === name);
  const format = options.tooltip.y.formatter;
  assert.match(format(55, {seriesIndex: indexOf('role:battery_soc'), w}), /55.*%/);
  assert.match(format(6400, {seriesIndex: indexOf('role:pv'), w}), /6400.*W/);
  dom.window.close();
});

test('chartOptions deckelt die Punktzahl je Serie', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  const rows = [];
  for (let index = 0; index < 20000; index += 1) rows.push({series: 'role:pv', ts: now - HOUR + index * 100, v: index, u: 'W'});
  await dom.window.HistoryStore.writeRaw(rows);
  await component.load();
  component.selectedSeries = ['role:pv'];
  const points = component.chartOptions.series[0].data.length;
  assert.ok(points <= component.MAX_POINTS, `${points} Punkte, erwartet hoechstens ${component.MAX_POINTS}`);
  assert.ok(points > 0);
  dom.window.close();
});

test('renderChart haelt einen vom Nutzer gesetzten Zoom fest, bis der Home-Knopf ihn loest', async () => {
  const {dom, component} = panel();
  let captured = null;
  component.chart = {updateOptions: options => { captured = options; }};
  const events = component.chartOptions.chart.events;

  // Der Nutzer zoomt: das Fenster wird gemerkt und beim Nachladen wieder
  // als feste Achsengrenze gesetzt.
  events.zoomed(null, {xaxis: {min: 111, max: 222}});
  component.renderChart();
  assert.equal(captured.xaxis.min, 111);
  assert.equal(captured.xaxis.max, 222);

  // Ein neuer Zeitraum (renderChart(false)) ignoriert den alten Zoom.
  component.renderChart(false);
  assert.equal(captured.xaxis.min, undefined);

  // Home-Knopf: der festgehaltene Ausschnitt ist weg.
  events.zoomed(null, {xaxis: {min: 111, max: 222}});
  events.beforeResetZoom();
  component.renderChart();
  assert.equal(captured.xaxis.min, undefined);
  dom.window.close();
});

test('load fuellt eine echte Luecke der Rohstufe mit einem Minutenmittel aus dem Austausch', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  // Regelmaessige Rohpunkte, unterbrochen von einer klaren Luecke - siehe
  // history-rollup.test.mjs fuer denselben Punkte-Aufbau bei detectGaps().
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts: now - 6 * HOUR, v: 100, u: 'W'},
    {series: 'role:pv', ts: now - 6 * HOUR + 10000, v: 110, u: 'W'},
    {series: 'role:pv', ts: now - 6 * HOUR + 20000, v: 120, u: 'W'},
    {series: 'role:pv', ts: now - HOUR, v: 200, u: 'W'},
    {series: 'role:pv', ts: now - HOUR + 10000, v: 210, u: 'W'},
    {series: 'role:pv', ts: now - HOUR + 20000, v: 220, u: 'W'},
  ]);
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: now - 3 * HOUR, min: 150, max: 150, avg: 150, n: 6, u: 'W'},
  ]);
  component.rangeHours = 12;
  await component.load();
  assert.equal(component.tier, 'raw');
  const filled = component.rows.find(row => row.series === 'role:pv' && row.ts === now - 3 * HOUR);
  assert.ok(filled, 'der Luecken-Satz aus der 1m-Stufe fehlt in rows');
  assert.equal(filled.avg, 150);
  dom.window.close();
});

test('load fuellt auch die Luecke vor dem ersten eigenen Rohpunkt', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  // Der Fall, an dem die erste Fassung scheiterte: dieses Geraet lief nur
  // die letzte Stunde, ein anderes hat den Rest beigesteuert. Zwischen zwei
  // vorhandenen Rohpunkten gibt es dann gar keine Luecke.
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts: now - HOUR, v: 200, u: 'W'},
    {series: 'role:pv', ts: now - HOUR + 10000, v: 210, u: 'W'},
    {series: 'role:pv', ts: now - HOUR + 20000, v: 220, u: 'W'},
  ]);
  const exchanged = [];
  for (let index = 0; index < 60; index += 1) {
    exchanged.push({series: 'role:pv', ts: now - 4 * HOUR + index * 60000, min: 150, max: 150, avg: 150, n: 6, u: 'W'});
  }
  await dom.window.HistoryStore.writeRollup('1m', exchanged);
  component.rangeHours = 6;
  await component.load();
  assert.equal(component.tier, 'raw');
  const filled = component.rows.filter(row => row.series === 'role:pv' && row.ts < now - HOUR);
  assert.equal(filled.length, 60, 'alle ergaenzten Minuten vor dem ersten Rohpunkt fehlen');
  dom.window.close();
});

test('load laesst eine luecklos aufgezeichnete Rohserie unangetastet, obwohl 1m-Daten vorliegen', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([{series: 'role:pv', ts: now - HOUR, v: 640, u: 'W'}]);
  // Frueh verdichtete eigene Kopie derselben Minute (siehe
  // exchange_compacted_until in history-maintenance.js) - darf nicht
  // zusaetzlich als zweiter Punkt auftauchen.
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: now - HOUR, min: 640, max: 640, avg: 640, n: 1, u: 'W'},
  ]);
  component.rangeHours = 6;
  await component.load();
  const pvRows = component.rows.filter(row => row.series === 'role:pv');
  assert.equal(pvRows.length, 1, 'ohne erkannte Luecke darf kein zweiter Punkt entstehen');
  dom.window.close();
});

test('chartOptions zieht die Linie durch einen dicht gefuellten Abschnitt und laesst echte Luecken daneben stehen', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts: now - 6 * HOUR, v: 100, u: 'W'},
    {series: 'role:pv', ts: now - 6 * HOUR + 10000, v: 110, u: 'W'},
    {series: 'role:pv', ts: now - 6 * HOUR + 20000, v: 120, u: 'W'},
    {series: 'role:pv', ts: now - HOUR, v: 200, u: 'W'},
    {series: 'role:pv', ts: now - HOUR + 10000, v: 210, u: 'W'},
    {series: 'role:pv', ts: now - HOUR + 20000, v: 220, u: 'W'},
  ]);
  // Ein Peer, der zehn Minuten am Stueck aufgezeichnet hat, mitten in der
  // grossen Luecke zwischen den beiden eigenen Rohdaten-Clustern - links
  // und rechts davon bleibt trotzdem eine echte, ungefuellte Luecke.
  const filled = [];
  for (let index = 0; index < 10; index += 1) {
    filled.push({series: 'role:pv', ts: now - 4 * HOUR + index * 60000, min: 150, max: 150, avg: 150, n: 6, u: 'W'});
  }
  await dom.window.HistoryStore.writeRollup('1m', filled);
  component.rangeHours = 12;
  await component.load();
  component.selectedSeries = ['role:pv'];
  const options = component.chartOptions;
  // Der graue Luecken-Block ist weg.
  assert.equal(options.annotations.xaxis.length, 0, 'kein grauer Luecken-Block mehr');
  // Stattdessen zieht eine blasse Geist-Serie den letzten bekannten Wert an
  // jedem Luecken-Rand ein kurzes Stueck weiter.
  const ghost = options.series.find(item => item.ghostOf === 'role:pv');
  assert.ok(ghost, 'die Geist-Serie zu role:pv fehlt');
  const stubX = ghost.data.filter(point => point[1] !== null).map(point => point[0]);
  // Linker Rand der grossen Luecke zwischen den beiden eigenen Clustern ...
  assert.ok(stubX.includes(now - 6 * HOUR + 20000), 'kein Stummel am linken Rand der grossen Luecke');
  // ... und der Wiedereinstieg vor dem zweiten Cluster.
  assert.ok(stubX.includes(now - HOUR), 'kein Stummel am rechten Rand vor dem zweiten Cluster');
  // Der gehaltene Wert stammt vom Nachbarpunkt, er wird nicht erfunden.
  assert.equal(ghost.data.find(point => point[0] === now - HOUR)[1], 200);
  // Innerhalb des gefuellten Abschnitts darf die Linie nicht abreissen.
  const inside = options.series[0].data.filter(
    point => point[0] > now - 4 * HOUR && point[0] < now - 4 * HOUR + 9 * 60000,
  );
  assert.ok(inside.length, 'der gefuellte Abschnitt fehlt in den Chart-Daten');
  assert.ok(inside.every(point => point[1] !== null), 'im gefuellten Abschnitt darf kein Abbruch stehen');
  dom.window.close();
});

test('chartOptions zeigt einen einzelnen, isolierten Austausch-Punkt nicht als durchgehend gefuellte Luecke', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts: now - 6 * HOUR, v: 100, u: 'W'},
    {series: 'role:pv', ts: now - 6 * HOUR + 10000, v: 110, u: 'W'},
    {series: 'role:pv', ts: now - 6 * HOUR + 20000, v: 120, u: 'W'},
    {series: 'role:pv', ts: now - HOUR, v: 200, u: 'W'},
    {series: 'role:pv', ts: now - HOUR + 10000, v: 210, u: 'W'},
    {series: 'role:pv', ts: now - HOUR + 20000, v: 220, u: 'W'},
  ]);
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: now - 3 * HOUR, min: 150, max: 150, avg: 150, n: 6, u: 'W'},
  ]);
  component.rangeHours = 12;
  await component.load();
  component.selectedSeries = ['role:pv'];
  const options = component.chartOptions;
  assert.equal(options.annotations.xaxis.length, 0, 'kein grauer Luecken-Block mehr');
  // Ein einzelner Punkt mitten in einer mehrstuendigen Luecke darf die
  // Luecke nicht als "erledigt" ausgeben - die Linie bricht davor und
  // dahinter ab, und die Geist-Serie laeuft von beiden Seiten auf ihn zu.
  const ghost = options.series.find(item => item.ghostOf === 'role:pv');
  assert.ok(ghost, 'die Geist-Serie zu role:pv fehlt');
  const stubX = ghost.data.filter(item => item[1] !== null).map(item => item[0]);
  assert.ok(stubX.includes(now - 3 * HOUR), 'kein Stummel am isolierten Austausch-Punkt');
  const point = options.series[0].data.find(item => item[0] === now - 3 * HOUR);
  assert.ok(point, 'der externe Punkt fehlt in den Chart-Daten');
  dom.window.close();
});

test('load verwirft Zeitpunkte, an denen nicht jede Rolle einen Wert hat', async () => {
  const {dom, component} = panel({withEnergyModel: true});
  const now = Date.now();
  // role:pv misst dieses Geraet selbst, role:grid kommt komplett aus dem
  // Austausch und liegt deshalb auf dem Minutenraster. Ohne den
  // Vollstaendigkeits-Filter entstuenden aus JEDEM Zeitpunkt eigene
  // Hausverbrauch-Punkte, die halbe Bilanzen zeigen.
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts: now - HOUR, v: 1000, u: 'W'},
    {series: 'role:pv', ts: now - HOUR + 10000, v: 1000, u: 'W'},
  ]);
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:grid', ts: now - 3 * HOUR, min: 200, max: 200, avg: 200, n: 6, u: 'W'},
  ]);
  component.rangeHours = 6;
  await component.load();
  const derived = component.rows.filter(row => row.series === 'berechnet:hausverbrauch');
  assert.equal(derived.length, 0, 'kein Zeitpunkt traegt beide Rollen, also gibt es nichts abzuleiten');
  dom.window.close();
});

test('ohne Daten meldet das Panel Leere statt einer leeren Zeichenflaeche', async () => {
  const {dom, component} = panel();
  await component.load();
  assert.equal(component.isEmpty, true);
  assert.match(component.statusText, /keine|Keine/);
  dom.window.close();
});

test('eine angehaltene Aufzeichnung steht in der Statuszeile', async () => {
  const {dom, component} = panel();
  dom.window.dashboardHistorizer.status = () => ({paused: true, persisted: false, reason: 'Der Browser-Speicher ist voll.'});
  await component.load();
  assert.match(component.statusText, /voll/);
  dom.window.close();
});

test('renderChart erzeugt genau einen Chart und aktualisiert ihn danach', async () => {
  const {dom, component, charts} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([{series: 'role:pv', ts: now - HOUR, v: 640, u: 'W'}]);
  await component.load();
  component.selectedSeries = ['role:pv'];
  component.renderChart();
  component.renderChart();
  assert.equal(charts.length, 1, 'ein zweiter Aufruf darf keinen zweiten Chart bauen');
  dom.window.close();
});

function withExportDouble(dom) {
  const calls = [];
  dom.window.HistoryExport = {
    toCSV: args => { calls.push({format: 'csv', ...args}); return ['csv']; },
    toJSON: args => { calls.push({format: 'json', ...args}); return ['json']; },
    filename: (meta, extension) => `verlauf.${extension}`,
    download: (chunks, name, mime) => { calls.push({download: name, mime}); },
  };
  return calls;
}

test('exportAs uebergibt die sichtbaren Saetze samt Metadaten an den Serialisierer', async () => {
  const {dom, component} = panel();
  const calls = withExportDouble(dom);
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([{series: 'role:pv', ts: now - HOUR, v: 640, u: 'W'}]);
  await component.load();
  await component.exportAs('csv');

  const serialised = calls.find(call => call.format === 'csv');
  assert.ok(serialised, 'toCSV wurde nicht aufgerufen');
  assert.equal(serialised.rows.length, 1);
  assert.equal(serialised.meta.tier, 'raw');
  assert.equal(serialised.meta.aggregate, 'avg');
  assert.ok(serialised.meta.timezone, 'die Zeitzone gehoert in die Metadaten');
  assert.ok(calls.some(call => call.download === 'verlauf.csv'));
  dom.window.close();
});

test('exportAs json waehlt den JSON-Serialisierer und den passenden MIME-Typ', async () => {
  const {dom, component} = panel();
  const calls = withExportDouble(dom);
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([{series: 'role:pv', ts: now - HOUR, v: 640, u: 'W'}]);
  await component.load();
  await component.exportAs('json');
  assert.ok(calls.some(call => call.format === 'json'));
  assert.ok(calls.some(call => call.mime === 'application/json'));
  dom.window.close();
});

test('ein grosser Export fragt vorher nach', async () => {
  const {dom, component} = panel();
  withExportDouble(dom);
  const modal = component.$store.modal;
  modal.answer = false;
  // Deckelung umgehen: der Test setzt die Saetze direkt, statt 200k Zeilen
  // durch die IndexedDB zu schieben.
  component.rows = new Array(component.EXPORT_CONFIRM_ROWS + 1).fill(null)
    .map((_value, index) => ({series: 'role:pv', ts: index, min: 1, max: 1, avg: 1, n: 1, u: 'W'}));
  component.selectedSeries = ['role:pv'];
  await component.exportAs('csv');
  assert.equal(modal.calls.length, 1, 'ueber der Schwelle muss zurueckgefragt werden');
  assert.match(modal.calls[0].message || modal.calls[0].title || '', /\d/);
  dom.window.close();
});

test('ein abgelehnter grosser Export laedt nichts herunter', async () => {
  const {dom, component} = panel();
  const calls = withExportDouble(dom);
  component.$store.modal.answer = false;
  component.rows = new Array(component.EXPORT_CONFIRM_ROWS + 1).fill(null)
    .map((_value, index) => ({series: 'role:pv', ts: index, min: 1, max: 1, avg: 1, n: 1, u: 'W'}));
  component.selectedSeries = ['role:pv'];
  await component.exportAs('csv');
  assert.equal(calls.filter(call => call.download).length, 0);
  dom.window.close();
});

test('ein kleiner Export fragt nicht zurueck', async () => {
  const {dom, component} = panel();
  const calls = withExportDouble(dom);
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([{series: 'role:pv', ts: now - HOUR, v: 640, u: 'W'}]);
  await component.load();
  await component.exportAs('csv');
  assert.equal(component.$store.modal.calls.length, 0);
  assert.equal(calls.filter(call => call.download).length, 1);
  dom.window.close();
});

// Ein Double fuer /api/v1/settings, das den zuletzt gesendeten Koerper
// festhaelt - das Lesen-Aendern-Schreiben ist hier der heikle Teil.
function withSettingsDouble(dom, initial = {}) {
  const state = {value: {health_score_threshold: 3, sweep_interval_seconds: 300, history_views: [], ...initial}, puts: []};
  dom.window.fetch = (url, options) => {
    if (!options || options.method !== 'PUT') {
      return Promise.resolve({ok: true, json: () => Promise.resolve(state.value)});
    }
    const body = JSON.parse(options.body);
    state.puts.push(body);
    state.value = body;
    return Promise.resolve({ok: true, json: () => Promise.resolve(body)});
  };
  return state;
}

test('saveView legt die aktuelle Sicht an, ohne die uebrigen Einstellungen zu verlieren', async () => {
  const {dom, component} = panel();
  const state = withSettingsDouble(dom);
  component.selectedSeries = ['role:pv', 'role:grid'];
  component.rangeHours = 24;
  component.aggregate = 'max';
  component.viewName = 'Tagesüberblick';
  await component.saveView();

  assert.equal(state.puts.length, 1);
  const sent = state.puts[0];
  // Der entscheidende Punkt: der PUT geht ueber das gesamte Settings-Objekt.
  // Wer nur history_views sendet, loescht alles andere.
  assert.equal(sent.health_score_threshold, 3);
  assert.equal(sent.sweep_interval_seconds, 300);
  assert.equal(sent.history_views.length, 1);
  assert.equal(sent.history_views[0].name, 'Tagesüberblick');
  assert.deepEqual(sent.history_views[0].series, ['role:pv', 'role:grid']);
  assert.equal(sent.history_views[0].range_hours, 24);
  assert.equal(sent.history_views[0].aggregate, 'max');
  assert.ok(sent.history_views[0].id, 'eine Sicht braucht eine ID');
  dom.window.close();
});

test('saveView ohne Namen legt nichts an', async () => {
  const {dom, component} = panel();
  const state = withSettingsDouble(dom);
  component.viewName = '   ';
  await component.saveView();
  assert.equal(state.puts.length, 0);
  assert.match(component.$store.toasts.last('critical'), /Name/);
  dom.window.close();
});

test('applyView setzt Serien, Zeitraum und Kennwert', async () => {
  const {dom, component} = panel();
  withSettingsDouble(dom, {history_views: [
    {id: 'v1', name: 'Batterie', series: ['role:battery'], range_hours: 168, aggregate: 'min'},
  ]});
  await component.loadViews();
  await component.applyView('v1');
  assert.deepEqual(JSON.parse(JSON.stringify(component.selectedSeries)), ['role:battery']);
  assert.equal(component.rangeHours, 168);
  assert.equal(component.aggregate, 'min');
  dom.window.close();
});

test('eine Sicht ohne Daten in diesem Browser sagt das ausdruecklich', async () => {
  const {dom, component} = panel();
  withSettingsDouble(dom, {history_views: [
    {id: 'v1', name: 'Batterie', series: ['role:battery'], range_hours: 6, aggregate: 'avg'},
  ]});
  await component.loadViews();
  await component.applyView('v1');
  // Die IndexedDB ist leer - eine am Rechner gespeicherte Sicht geht am
  // Telefon genau so auf, und der Text muss den Grund nennen.
  assert.equal(component.isEmpty, true);
  assert.match(component.statusText, /Gerät/);
  dom.window.close();
});

test('deleteView entfernt genau eine Sicht', async () => {
  const {dom, component} = panel();
  const state = withSettingsDouble(dom, {history_views: [
    {id: 'v1', name: 'A', series: [], range_hours: 6, aggregate: 'avg'},
    {id: 'v2', name: 'B', series: [], range_hours: 6, aggregate: 'avg'},
  ]});
  await component.loadViews();
  await component.deleteView('v1');
  assert.deepEqual(state.puts[0].history_views.map(view => view.id), ['v2']);
  dom.window.close();
});

test('deleteView setzt selectedViewId zurueck, wenn die geloeschte Sicht ausgewaehlt war', async () => {
  const {dom, component} = panel();
  withSettingsDouble(dom, {history_views: [
    {id: 'v1', name: 'A', series: [], range_hours: 6, aggregate: 'avg'},
  ]});
  await component.loadViews();
  component.selectedViewId = 'v1';
  await component.deleteView('v1');
  assert.equal(component.selectedViewId, '');
  dom.window.close();
});

test('rangeBounds liefert im relativen Modus jetzt minus rangeHours', async () => {
  const {dom, component} = panel();
  component.rangeHours = 6;
  const before = Date.now();
  const {from, to} = component.rangeBounds();
  const after = Date.now();
  assert.ok(to >= before && to <= after, 'to sollte etwa "jetzt" sein');
  assert.equal(to - from, 6 * HOUR);
  dom.window.close();
});

test('rangeBounds liefert im custom-Modus die gesetzten Grenzen', async () => {
  const {dom, component} = panel();
  component.rangeMode = 'custom';
  component.customFrom = 1000;
  component.customTo = 2000;
  const bounds = component.rangeBounds();
  assert.equal(bounds.from, 1000);
  assert.equal(bounds.to, 2000);
  dom.window.close();
});

test('rangeBounds faellt auf relativ zurueck, solange kein vollstaendiger custom-Bereich gesetzt ist', async () => {
  const {dom, component} = panel();
  component.rangeHours = 6;
  component.rangeMode = 'custom';
  component.customFrom = 1000;
  component.customTo = null;
  const {from, to} = component.rangeBounds();
  assert.equal(to - from, 6 * HOUR);
  dom.window.close();
});

test('selectPreset wechselt in den relativen Modus und setzt rangeHours', async () => {
  const {dom, component} = panel();
  component.rangeMode = 'custom';
  component.customFrom = 1;
  component.customTo = 2;
  component.selectPreset(24);
  assert.equal(component.rangeMode, 'relative');
  assert.equal(component.rangeHours, 24);
  await new Promise(resolve => setTimeout(resolve, 10));
  dom.window.close();
});

test('clearCustomRange faellt auf den relativen Modus zurueck', async () => {
  const {dom, component} = panel();
  component.rangeHours = 6;
  component.rangeMode = 'custom';
  component.customFrom = 1;
  component.customTo = 2;
  component.clearCustomRange();
  assert.equal(component.rangeMode, 'relative');
  assert.equal(component.customFrom, null);
  assert.equal(component.customTo, null);
  await new Promise(resolve => setTimeout(resolve, 10));
  dom.window.close();
});

test('customRangeLabel ist nur im custom-Modus mit vollstaendigem Bereich gesetzt', async () => {
  const {dom, component} = panel();
  assert.equal(component.customRangeLabel, '');
  component.rangeMode = 'custom';
  component.customFrom = Date.UTC(2026, 7, 25, 8, 0);
  component.customTo = Date.UTC(2026, 7, 25, 14, 0);
  assert.notEqual(component.customRangeLabel, '');
  dom.window.close();
});

test('seriesColor bleibt stabil, wenn eine andere Serie abgewaehlt wird', async () => {
  const {dom, component} = panel();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([
    {series: 'sensor:a', ts: now - HOUR, v: 1, u: 'W'},
    {series: 'sensor:b', ts: now - HOUR, v: 2, u: 'W'},
  ]);
  await component.load();
  component.selectedSeries = ['sensor:a', 'sensor:b'];
  const colorBefore = component.seriesColor('sensor:b');
  component.selectedSeries = ['sensor:b'];
  const colorAfter = component.seriesColor('sensor:b');
  assert.equal(colorBefore, colorAfter);
  dom.window.close();
});

test('saveView im custom-Modus sichert range_mode, range_from und range_to', async () => {
  const {dom, component} = panel();
  const state = withSettingsDouble(dom);
  component.selectedSeries = ['role:pv'];
  component.rangeMode = 'custom';
  component.customFrom = 1000;
  component.customTo = 2000;
  component.viewName = 'Sturmtag';
  await component.saveView();
  const saved = state.puts[0].history_views[0];
  assert.equal(saved.range_mode, 'custom');
  assert.equal(saved.range_from, 1000);
  assert.equal(saved.range_to, 2000);
  dom.window.close();
});

test('applyView stellt einen custom-Zeitraum wieder her', async () => {
  const {dom, component} = panel();
  withSettingsDouble(dom, {history_views: [
    {id: 'v1', name: 'Sturmtag', series: ['role:pv'], range_hours: 6, range_mode: 'custom', range_from: 1000, range_to: 2000, aggregate: 'avg'},
  ]});
  await component.loadViews();
  await component.applyView('v1');
  assert.equal(component.rangeMode, 'custom');
  assert.equal(component.customFrom, 1000);
  assert.equal(component.customTo, 2000);
  dom.window.close();
});

test('eine eingegangene Lieferung erscheint als Notiz im Panel', async () => {
  const {dom, factory} = load();
  const component = factory();
  attachStores(component);
  await component.init();
  assert.equal(component.exchangeNotice, '');

  dom.window.dispatchEvent(new dom.window.CustomEvent('dashboard-history-exchanged', {
    detail: {peer: 'p-fremd', tier: '1m', rows: 180},
  }));
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.match(component.exchangeNotice, /180/);
  assert.match(component.exchangeNotice, /ergänzt/);
  dom.window.close();
});

test('mehrere Lieferungen werden in der Notiz aufsummiert', async () => {
  const {dom, factory} = load();
  const component = factory();
  attachStores(component);
  await component.init();
  const fire = rows => dom.window.dispatchEvent(new dom.window.CustomEvent('dashboard-history-exchanged', {
    detail: {peer: 'p-fremd', tier: '1m', rows},
  }));
  fire(100);
  fire(80);
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.match(component.exchangeNotice, /180/);
  dom.window.close();
});

test('eine Lieferung ohne Saetze erzeugt keine Notiz', async () => {
  const {dom, factory} = load();
  const component = factory();
  attachStores(component);
  await component.init();
  dom.window.dispatchEvent(new dom.window.CustomEvent('dashboard-history-exchanged', {
    detail: {peer: 'p-fremd', tier: '1m', rows: 0},
  }));
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.equal(component.exchangeNotice, '');
  dom.window.close();
});

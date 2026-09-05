// Regression tests for energy-board.js: boardRows()/visibleRows()/
// summaryBar()/roleSeries()/sparklineGeometry() are the pure ports of
// Vorschlag C ("Datentafel") from the six-proposals exploration, with the
// sparkline fed by real role:* history samples instead of the prototype's
// simulated noise profile. Its four layout-editor options (sort,
// spark_window, dense, show_inactive) are covered at the component level.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');
const themeSource = read('theme.js');
const energyModelSource = read('energy-model.js');
const energyPresentationSource = read('energy-presentation.js');
const scriptSource = read('energy-board.js');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  vm.runInContext(themeSource, context);
  vm.runInContext(energyModelSource, context);
  vm.runInContext(energyPresentationSource, context);
  vm.runInContext(scriptSource, context);
  return { factory, model: dom.window.EnergyModel, window: dom.window, document: dom.window.document };
}

test('boardRows keeps the fixed pv/battery/grid/load/wallbox/heat_pump order', () => {
  const { factory, model } = load();
  const snapshot = {values: {pv: 6400, battery: 2200, load: 620, grid: -3580, wallbox: 0, heat_pump: 0}, roles: []};
  const balance = model.deriveBalance(snapshot);
  const rows = factory.boardRows(snapshot, balance);
  assert.deepEqual(JSON.parse(JSON.stringify(rows.map(r => r.id))), ['pv', 'battery', 'grid', 'load', 'wallbox', 'heat_pump']);
  assert.equal(rows.find(r => r.id === 'battery').dir, 'Laden');
  assert.equal(rows.find(r => r.id === 'grid').dir, 'Einspeisung');
});

test('boardRows nets simultaneous charge and discharge into one battery balance instead of dropping one side', () => {
  const { factory, model } = load();
  // Two separate sensors (roles battery_charge/battery_discharge) both reporting
  // at once - e.g. residual/idle current on the inactive side. The true net flow
  // is 500 W charging; picking "whichever role is bigger" would show 800 W charging
  // and silently discard the 300 W discharge reading.
  const snapshot = {values: {pv: 0, battery_charge: 800, battery_discharge: 300, load: 0}, roles: []};
  const balance = model.deriveBalance(snapshot);
  const rows = factory.boardRows(snapshot, balance);
  const battery = rows.find(r => r.id === 'battery');
  assert.equal(battery.value, 500);
  assert.equal(battery.dir, 'Laden');
});

test('boardRows appends a red "Nicht zugeordnet" row when the balance is unbalanced (diagnostic mode)', () => {
  const { factory, model } = load();
  const snapshot = {values: {pv: 3100, load: 3100, heat_pump: 620, grid: 940}, roles: []};
  const balance = model.deriveBalance(snapshot, {gap_mode: 'diagnostic'});
  const rows = factory.boardRows(snapshot, balance);
  const rest = rows.find(r => r.id === 'rest');
  assert.ok(rest);
  assert.equal(rest.quality, 'gap');
  assert.equal(Math.round(rest.value), 940);
});

test('boardRows appends a grey "eingerechnet" info row when the gap was absorbed (unknown_consumer default)', () => {
  const { factory, model } = load();
  const snapshot = {values: {pv: 3100, load: 3100, heat_pump: 620, grid: 940}, roles: []};
  const balance = model.deriveBalance(snapshot);
  const rows = factory.boardRows(snapshot, balance);
  const rest = rows.find(r => r.id === 'rest');
  assert.ok(rest);
  assert.equal(rest.quality, 'gap-absorbed');
  assert.match(rest.label, /eingerechnet/);
  assert.equal(Math.round(rest.value), 940);
});

test('boardRows marks a row stale from the snapshot roles list', () => {
  const { factory, model } = load();
  const snapshot = {values: {pv: 500, load: 500}, roles: [{role: 'pv', freshness: 'stale'}]};
  const balance = model.deriveBalance(snapshot);
  const rows = factory.boardRows(snapshot, balance);
  assert.equal(rows.find(r => r.id === 'pv').stale, true);
  assert.equal(rows.find(r => r.id === 'load').stale, false);
});

test('visibleRows drops rows at or below the noise floor when show_inactive is off', () => {
  const { factory } = load();
  const rows = [{id: 'pv', value: 500}, {id: 'wallbox', value: 0}, {id: 'heat_pump', value: 0.2}];
  const visible = factory.visibleRows(rows, false, 'fixed');
  assert.deepEqual(visible.map(r => r.id), ['pv']);
});

test('visibleRows keeps every row when show_inactive is on (the default)', () => {
  const { factory } = load();
  const rows = [{id: 'pv', value: 500}, {id: 'wallbox', value: 0}];
  assert.equal(factory.visibleRows(rows, true, 'fixed').length, 2);
});

test('visibleRows sort "power" orders rows by value descending, "fixed" keeps document order', () => {
  const { factory } = load();
  const rows = [{id: 'pv', value: 100}, {id: 'battery', value: 900}, {id: 'grid', value: 500}];
  // JSON round-trip: visibleRows()'s [...visible].sort() allocates its result array in the
  // vm context's realm, whose prototype differs from this outer array literal's - deepEqual
  // would otherwise fail on "same structure, not reference-equal" even though the values match.
  assert.deepEqual(JSON.parse(JSON.stringify(factory.visibleRows(rows, true, 'power').map(r => r.id))), ['battery', 'grid', 'pv']);
  assert.deepEqual(JSON.parse(JSON.stringify(factory.visibleRows(rows, true, 'fixed').map(r => r.id))), ['pv', 'battery', 'grid']);
});

test('barFraction floors a present-but-tiny value to a visible sliver', () => {
  const { factory } = load();
  assert.equal(factory.barFraction({value: 5}, 10000), 0.015);
  assert.equal(factory.barFraction({value: 0}, 10000), 0);
});

test('summaryBar only labels segments wider than 12%', () => {
  const { factory } = load();
  const bar = factory.summaryBar([
    {id: 'pv', label: 'PV', value: 900, color: '#fff'},
    {id: 'grid_import', label: 'Netzbezug', value: 100, color: '#0f0'},
  ]);
  assert.equal(bar.total, 1000);
  assert.match(bar.segments[0].text, /^PV/);
  assert.equal(bar.segments[1].text, '');
});

test('roleSeries filters by role prefix and time window', () => {
  const { factory } = load();
  const now = Date.parse('2026-08-07T12:00:00Z');
  const samples = [
    {entity_id: 'role:pv', timestamp: '2026-08-07T11:50:00Z', value: 100},
    {entity_id: 'role:pv', timestamp: '2026-08-07T11:00:00Z', value: 50}, // outside the 15-minute window
    {entity_id: 'role:battery', timestamp: '2026-08-07T11:55:00Z', value: 200},
  ];
  const series = factory.roleSeries(samples, 'pv', now, 15 * 60 * 1000);
  assert.deepEqual(series, [100]);
});

test('sparklineGeometry returns null with fewer than two points', () => {
  const { factory } = load();
  assert.equal(factory.sparklineGeometry([]), null);
  assert.equal(factory.sparklineGeometry([42]), null);
});

test('sparklineGeometry scales the last point to the series maximum magnitude', () => {
  const { factory } = load();
  const geometry = factory.sparklineGeometry([0, -1000, 500]);
  assert.ok(geometry.line.startsWith('M 0.0'));
  assert.ok(geometry.lastY > 3 && geometry.lastY < 24);
});

function cardMarkup(initialSnapshot, dataAttrs) {
  const attrs = Object.entries(dataAttrs || {}).map(([k, v]) => ` data-${k}="${v}"`).join('');
  return `
    <div data-layout-item-id="energy-board"${attrs}>
      <section id="energy-board-card">
        <script type="application/json" id="energy-board-initial">${JSON.stringify(initialSnapshot)}</script>
      </section>
    </div>
  `;
}

function buildComponent(document, factory, initialSnapshot, dataAttrs) {
  document.body.innerHTML = cardMarkup(initialSnapshot, dataAttrs);
  const root = document.querySelector('#energy-board-card');
  const component = factory();
  component.$root = root;
  return component;
}

test('init() reads sort/show_inactive from the wrapper and compute() applies them', async () => {
  const { factory, document } = load();
  const snapshot = {values: {pv: 100, battery: 900, load: 500, grid: 0, wallbox: 0, heat_pump: 0}, roles: []};
  const component = buildComponent(document, factory, snapshot, {sort: 'power', 'show-inactive': 'off', 'spark-window': 'off'});
  await component.init();
  assert.equal(component.options.sort, 'power');
  assert.equal(component.options.showInactive, 'off');
  assert.ok(component.rows.every(row => row.value > 0.5), 'show_inactive off must drop zero-value rows');
  const values = JSON.parse(JSON.stringify(component.rows.map(r => r.value)));
  assert.deepEqual(values, [...values].sort((a, b) => b - a));
});

test('init() with spark_window "off" never populates sparklines', async () => {
  const { factory, document, window } = load();
  const snapshot = {values: {pv: 6400, battery: 2200, load: 620, grid: -3580}, roles: []};
  let called = false;
  window.dashboardHistorizer = { readSamples: async () => { called = true; return []; } };
  const component = buildComponent(document, factory, snapshot, {'spark-window': 'off'});
  await component.init();
  assert.equal(called, false, 'readSamples must not be called when the Verlauf column is off');
  assert.equal(Object.keys(component.sparklines).length, 0);
});

test('loadSeries rechnet den Hausverbrauch je Verlaufspunkt, statt role:load zu lesen', () => {
  const { factory } = load();
  const now = Date.parse('2026-08-17T15:05:00Z');
  const samples = [
    {entity_id: 'role:pv', timestamp: '2026-08-17T15:00:00Z', value: 640},
    {entity_id: 'role:grid', timestamp: '2026-08-17T15:00:00Z', value: 511},
    {entity_id: 'role:pv', timestamp: '2026-08-17T15:01:00Z', value: 800},
    {entity_id: 'role:grid', timestamp: '2026-08-17T15:01:00Z', value: 400},
  ];
  const series = factory.loadSeries(samples, undefined, now, 60 * 60 * 1000);
  assert.deepEqual(JSON.parse(JSON.stringify(series.map(Math.round))), [1151, 1200]);
});

test('loadSeries folgt dem eingestellten load_mode', () => {
  const { factory } = load();
  const now = Date.parse('2026-08-17T15:05:00Z');
  const samples = [
    {entity_id: 'role:pv', timestamp: '2026-08-17T15:00:00Z', value: 640},
    {entity_id: 'role:grid', timestamp: '2026-08-17T15:00:00Z', value: 511},
    {entity_id: 'role:load', timestamp: '2026-08-17T15:00:00Z', value: 900},
  ];
  // gap_mode "diagnostic" in beiden Faellen: der Standard unknown_consumer
  // wuerde die Differenz zwischen Messung (900) und Rechnung (1151) als
  // Bilanzluecke wieder auf den Hausverbrauch addieren, und beide Modi
  // laendeten bei 1151 - der Test koennte sie dann nicht unterscheiden.
  const calculated = factory.loadSeries(samples, {load_mode: 'calculated', gap_mode: 'diagnostic'}, now, 60 * 60 * 1000);
  const measured = factory.loadSeries(samples, {load_mode: 'measured', gap_mode: 'diagnostic'}, now, 60 * 60 * 1000);
  assert.deepEqual(JSON.parse(JSON.stringify(calculated.map(Math.round))), [1151]);
  assert.deepEqual(JSON.parse(JSON.stringify(measured.map(Math.round))), [900]);
});

test('loadSeries laesst Punkte ausserhalb des Sparkline-Fensters weg', () => {
  const { factory } = load();
  const now = Date.parse('2026-08-17T15:05:00Z');
  const samples = [
    {entity_id: 'role:pv', timestamp: '2026-08-17T13:00:00Z', value: 100},
    {entity_id: 'role:pv', timestamp: '2026-08-17T15:00:00Z', value: 640},
  ];
  assert.equal(factory.loadSeries(samples, undefined, now, 15 * 60 * 1000).length, 1);
});

test('die Zeile "Hausverbrauch" nennt die Herkunft der Zahl', () => {
  const { factory, model } = load();
  const calculated = {values: {pv: 640, grid: 511}, roles: []};
  const rowsCalculated = factory.boardRows(calculated, model.deriveBalance(calculated));
  assert.equal(rowsCalculated.find(row => row.id === 'load').label, 'Hausverbrauch (berechnet)');

  const measured = {values: {pv: 640, grid: 511, load: 900}, roles: []};
  const rowsMeasured = factory.boardRows(measured, model.deriveBalance(measured));
  assert.equal(rowsMeasured.find(row => row.id === 'load').label, 'Hausverbrauch');
});

test('eine neu gemountete Datentafel zeichnet die Sparklines sofort aus dem letzten Lesestand', async () => {
  const { factory, window, document } = load();
  // Zeitstempel relativ zu jetzt: die Sparkline zeigt per Vorgabe die
  // letzten 15 Minuten, feste Datumswerte wuerden je nach Uhrzeit des
  // Testlaufs aus dem Fenster fallen.
  const minutesAgo = n => new Date(Date.now() - n * 60 * 1000).toISOString();
  const samples = [
    {entity_id: 'role:pv', timestamp: minutesAgo(3), value: 640},
    {entity_id: 'role:pv', timestamp: minutesAgo(1), value: 800},
  ];
  const markup = `
    <section id="energy-board-card">
      <script type="application/json" id="energy-board-initial">${JSON.stringify({values: {pv: 800}, roles: [], interpretation: {}})}</script>
    </section>`;

  const mount = async () => {
    document.body.innerHTML = markup;
    const component = factory();
    component.$root = document.querySelector('#energy-board-card');
    window.DashboardTheme.onChange = () => () => {};
    const promise = component.init();
    // Zustand unmittelbar nach dem synchronen Teil von init(), also bevor
    // der IndexedDB-Lesevorgang zurueckkommt - genau der Moment, in dem die
    // Kachel bisher leer war.
    const before = component.sparklines;
    await promise;
    return { before, after: component.sparklines };
  };

  window.dashboardHistorizer = { readSamples: async () => samples };
  const first = await mount();
  assert.ok(first.after.pv, 'die erste Instanz liest die Samples selbst');

  const second = await mount();
  assert.ok(second.before.pv, 'die zweite Instanz zeichnet sofort aus dem Zwischenspeicher');
});

test('die Rollentafel zeigt den gemessenen Verbrauch gesammelt als eigene Zeile', () => {
  const { factory, model } = load();
  const snapshot = {values: {pv: 3000, load: 900, wallbox: 500}, roles: [], interpretation: {load_mode: 'combined'}};
  const balance = model.balanceOf(snapshot, {measuredSplit: 'sum'});
  const rows = factory.boardRows(snapshot, balance);
  const measured = rows.find(row => row.id === 'load_measured');
  assert.ok(measured, 'Zeile fehlt');
  assert.equal(measured.value, 900);
  assert.equal(measured.label, 'Gemessene Verbraucher');
  // Die Zeile "Hausverbrauch" bleibt der Gesamtwert, nicht der Rest.
  assert.equal(rows.find(row => row.id === 'load').value, 3000);
});

test('die Rollentafel zeigt bei measured_split "entities" eine Zeile je Entitaet', () => {
  const { factory, model } = load();
  const snapshot = {
    values: {pv: 3000, load: 900, wallbox: 500},
    roles: [],
    entities: [
      {entity_id: 'werkstatt_power', label: 'Werkstatt / Leistung', value: 520, role: {role: 'load'}},
      {entity_id: 'waschmaschine_power', label: 'Waschmaschine / Leistung', value: 380, role: {role: 'load'}},
    ],
    interpretation: {load_mode: 'combined'},
  };
  const balance = model.balanceOf(snapshot, {measuredSplit: 'entities'});
  const labels = JSON.parse(JSON.stringify(factory.boardRows(snapshot, balance)
    .filter(row => row.id.startsWith('load_measured'))
    .map(row => row.label)));
  assert.deepEqual(labels, ['Werkstatt / Leistung', 'Waschmaschine / Leistung']);
});

test('ohne Kombiniert-Modus hat die Rollentafel keine Zeile fuer gemessenen Verbrauch', () => {
  const { factory, model } = load();
  const snapshot = {values: {pv: 3000, load: 900, wallbox: 500}, roles: []};
  const balance = model.balanceOf(snapshot);
  assert.equal(factory.boardRows(snapshot, balance).some(row => row.id.startsWith('load_measured')), false);
});

test('die Datentafel zeigt beim zweiten Schnappschuss zunaechst den alten Wert', async () => {
  const { factory, window: win } = load();
  let now = 0;
  Object.defineProperty(win, 'performance', { value: { now: () => now }, configurable: true });
  win.requestAnimationFrame = () => {};
  win.DashboardTheme.onChange = () => () => {};

  const mount = async raw => {
    const markup = `
      <div data-layout-item-id="board-1">
        <section id="energy-board-card">
          <script type="application/json" id="energy-board-initial">${JSON.stringify(raw)}</script>
        </section>
      </div>
    `;
    win.document.body.innerHTML = markup;
    const root = win.document.querySelector('#energy-board-card');
    const card = factory();
    card.$root = root;
    card.$refs = {};
    await card.init();
    return card;
  };

  const first = await mount({ values: { pv: 1000 }, roles: [] });
  assert.equal(first.snapshot.values.pv, 1000, 'der erste Aufbau steht sofort richtig da');
  first.destroy();

  const second = await mount({ values: { pv: 3000 }, roles: [] });
  assert.equal(second.snapshot.values.pv, 1000, 'die neue Instanz startet beim sichtbaren Wert');
  second.destroy();
});

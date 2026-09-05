// Regression tests for energy-model.js's deriveBalance()/formatPower(),
// the shared balance-derivation logic the six alternative energy-flow cards
// (energy-band.js etc.) build on top of.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const themeSource = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'theme.js'), 'utf8');
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'energy-model.js'),
  'utf8',
);

function loadEnergyModel() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  vm.runInContext(themeSource, dom.getInternalVMContext());
  vm.runInContext(scriptSource, dom.getInternalVMContext());
  return dom.window.EnergyModel;
}

test('deriveBalance splits a sunny-surplus snapshot into sources/sinks with grid export', () => {
  const model = loadEnergyModel();
  // Balanced by construction: pv (6400) covers charge (2200) + base (620) + export (3580).
  const snapshot = {values: {pv: 6400, battery: 2200, load: 620, grid: -3580}, roles: []};
  const balance = model.deriveBalance(snapshot);
  assert.equal(balance.gridExport, 3580);
  assert.equal(balance.gridImport, 0);
  assert.equal(balance.charge, 2200);
  assert.equal(balance.discharge, 0);
  assert.equal(Math.abs(balance.gap) < 0.01, true);
  const sourceIds = JSON.parse(JSON.stringify(balance.sources.map(f => f.id)));
  const sinkIds = JSON.parse(JSON.stringify(balance.sinks.map(f => f.id))).sort();
  assert.deepEqual(sourceIds, ['pv']);
  assert.deepEqual(sinkIds, ['base', 'battery_charge', 'grid_export'].sort());
});

test('deriveBalance derives grid import/export from a signed combined grid role', () => {
  const model = loadEnergyModel();
  const snapshot = {values: {pv: 0, battery: -760, load: 760, grid: 450}, roles: []};
  const balance = model.deriveBalance(snapshot);
  assert.equal(balance.gridImport, 450);
  assert.equal(balance.gridExport, 0);
  assert.equal(balance.discharge, 760);
});

test('deriveBalance subtracts wallbox/heat_pump from load to get "base"', () => {
  const model = loadEnergyModel();
  const snapshot = {values: {pv: 180, load: 8290, wallbox: 7400, heat_pump: 350, grid: 7810}, roles: []};
  const balance = model.deriveBalance(snapshot);
  const base = balance.sinks.find(f => f.id === 'base');
  assert.ok(base);
  assert.equal(Math.round(base.value), 540);
});

test('deriveBalance adds a "rest" entry when sources and sinks do not balance in diagnostic mode', () => {
  const model = loadEnergyModel();
  // sources: pv 3100 + grid_import 940 = 4040. sinks: base (load 3100 - heat_pump 620) + heat_pump = 3100.
  // gap = 4040 - 3100 = 940, added to the sinks side as "nicht zugeordnet" - only happens in
  // diagnostic mode; the new unknown_consumer default folds it into "base" instead (see below).
  const snapshot = {values: {pv: 3100, load: 3100, heat_pump: 620, grid: 940}, roles: []};
  const balance = model.deriveBalance(snapshot, {gap_mode: 'diagnostic'});
  const rest = balance.sinks.find(f => f.id === 'rest');
  assert.ok(rest);
  assert.equal(rest.rest, true);
  assert.equal(Math.round(balance.gap), 940);
  assert.equal(Math.round(rest.value), 940);
  assert.equal(balance.unbalanced, true);
});

test('deriveBalance folds the gap into "base" under the unknown_consumer default instead of a "rest" entry', () => {
  const model = loadEnergyModel();
  const snapshot = {values: {pv: 3100, load: 3100, heat_pump: 620, grid: 940}, roles: []};
  const balance = model.deriveBalance(snapshot);
  assert.equal(balance.sinks.find(f => f.id === 'rest'), undefined);
  assert.equal(balance.unbalanced, false);
  assert.equal(balance.gapAbsorbed, 940);
  const base = balance.sinks.find(f => f.id === 'base');
  assert.equal(Math.round(base.value), 3420);
});

test('deriveBalance returns empty sources/sinks and zero KPIs when everything is zero', () => {
  const model = loadEnergyModel();
  const balance = model.deriveBalance({values: {pv: 0, load: 0, grid: 0}, roles: []});
  assert.deepEqual(JSON.parse(JSON.stringify(balance.sources)), []);
  assert.deepEqual(JSON.parse(JSON.stringify(balance.sinks)), []);
  assert.equal(balance.kpi.autarkie, 0);
  assert.equal(balance.kpi.eigen, 0);
});

test('deriveBalance computes autarkie/eigen KPIs in diagnostic mode', () => {
  const model = loadEnergyModel();
  // load=1000, gridImport=250 -> autarkie 0.75; pv=1000, gridExport=100 -> eigen 0.9
  // (diagnostic mode leaves load_total untouched by the 150 W gap this
  // snapshot has - unknown_consumer would absorb it and change autarkie).
  const snapshot = {values: {pv: 1000, load: 1000, grid_import: 250, grid_export: 100}, roles: []};
  const balance = model.deriveBalance(snapshot, {gap_mode: 'diagnostic'});
  assert.equal(Math.round(balance.kpi.autarkie * 100), 75);
  assert.equal(Math.round(balance.kpi.eigen * 100), 90);
});

test('formatPower switches from W to comma-decimal kW at 1000', () => {
  const model = loadEnergyModel();
  assert.equal(model.formatPower(95), '95 W');
  assert.equal(model.formatPower(999), '999 W');
  assert.equal(model.formatPower(1234), '1,23 kW');
});

test('formatPercent uses one decimal below 10% and none above', () => {
  const model = loadEnergyModel();
  assert.equal(model.formatPercent(0.05), '5,0 %');
  assert.equal(model.formatPercent(0.42), '42 %');
});

test('readEmbeddedSnapshot parses the embedded JSON script tag', () => {
  const dom = new JSDOM(
    '<!doctype html><html><body><script type="application/json" id="energy-status-initial">{"values":{"pv":42}}</script></body></html>',
    { runScripts: 'outside-only', url: 'http://localhost/' },
  );
  vm.runInContext(themeSource, dom.getInternalVMContext());
  vm.runInContext(scriptSource, dom.getInternalVMContext());
  const snapshot = dom.window.EnergyModel.readEmbeddedSnapshot('energy-status-initial');
  assert.equal(snapshot.values.pv, 42);
});

test('readEmbeddedSnapshot returns null for a missing or malformed tag', () => {
  const dom = new JSDOM(
    '<!doctype html><html><body><script type="application/json" id="broken">not json</script></body></html>',
    { runScripts: 'outside-only', url: 'http://localhost/' },
  );
  vm.runInContext(themeSource, dom.getInternalVMContext());
  vm.runInContext(scriptSource, dom.getInternalVMContext());
  assert.equal(dom.window.EnergyModel.readEmbeddedSnapshot('missing'), null);
  assert.equal(dom.window.EnergyModel.readEmbeddedSnapshot('broken'), null);
});

test('clearSvgChildren removes drawn nodes but keeps title/desc', () => {
  const dom = new JSDOM(
    '<!doctype html><html><body><svg id="s"><title>T</title><desc>D</desc><path></path><circle></circle></svg></body></html>',
    { runScripts: 'outside-only', url: 'http://localhost/' },
  );
  vm.runInContext(themeSource, dom.getInternalVMContext());
  vm.runInContext(scriptSource, dom.getInternalVMContext());
  const svg = dom.window.document.getElementById('s');
  dom.window.EnergyModel.clearSvgChildren(svg);
  const tags = [...svg.children].map(n => n.localName);
  assert.deepEqual(tags, ['title', 'desc']);
});

test('svgEl creates a namespaced SVG element with attributes and text', () => {
  const model = loadEnergyModel();
  const node = model.svgEl('text', {x: '10', y: '20'}, 'hello');
  assert.equal(node.namespaceURI, model.SVG_NS);
  assert.equal(node.getAttribute('x'), '10');
  assert.equal(node.textContent, 'hello');
});

test('isStale reflects a stale role entry', () => {
  const model = loadEnergyModel();
  const snapshot = {values: {pv: 100}, roles: [{role: 'pv', freshness: 'stale'}]};
  assert.equal(model.isStale(snapshot, 'pv'), true);
  assert.equal(model.isStale(snapshot, 'grid'), false);
});

test('balanceOf prefers the server-computed balance even when it is deliberately wrong', () => {
  const model = loadEnergyModel();
  // Real values would put load_total at ~1000 W; the embedded balance below
  // claims 42 - balanceOf() must pass that through verbatim, proving it never
  // recomputes from .values when a server balance is present.
  const snapshot = {
    values: {pv: 1000, load: 1000},
    balance: {
      pv: 1000, grid_import: 0, grid_export: 0, battery_charge: 0, battery_discharge: 0,
      wallbox: 0, heat_pump: 0, load_total: 42, load_source: 'measured', base: 42,
      gap_raw: 0, gap_applied: 0, gap_absorbed: 0, gap_tolerance_w: 25, gap_ignored: true,
      unbalanced: false, autarkie: 1, eigenverbrauch: 1, netz: 0, total: 1000,
    },
  };
  const balance = model.balanceOf(snapshot);
  assert.equal(balance.load, 42);
});

test('balanceOf falls back to the JS mirror when a snapshot carries no server balance', () => {
  const model = loadEnergyModel();
  const snapshot = {values: {pv: 1000, load: 1000, grid_import: 250, grid_export: 100}, interpretation: {gap_mode: 'diagnostic'}};
  const balance = model.balanceOf(snapshot);
  assert.equal(Math.round(balance.kpi.autarkie * 100), 75);
});

test('deriveBalance labels "base" as calculated when load_mode falls back to calculated', () => {
  const model = loadEnergyModel();
  const snapshot = {values: {pv: 2000, grid_import: 500, wallbox: 300, heat_pump: 200}, roles: []};
  const balance = model.deriveBalance(snapshot, {load_mode: 'calculated'});
  const base = balance.sinks.find(f => f.id === 'base');
  assert.ok(base);
  assert.match(base.label, /berechnet/);
});

test('deriveBalanceCore matches the shared Go/JS balance-cases fixture', async () => {
  const model = loadEnergyModel();
  const fixturePath = path.join(here, '..', 'internal', 'energy', 'testdata', 'balance-cases.json');
  const cases = JSON.parse(fs.readFileSync(fixturePath, 'utf8'));
  assert.ok(cases.length > 0);
  for (const testCase of cases) {
    const snapshot = {values: testCase.values};
    const got = model.deriveBalanceCore(snapshot, testCase.interpretation);
    for (const [key, want] of Object.entries(testCase.expect)) {
      if (typeof want === 'number') {
        assert.ok(Math.abs(got[key] - want) < 1e-6, `${testCase.name}.${key} = ${got[key]}, want ${want}`);
      } else {
        assert.equal(got[key], want, `${testCase.name}.${key} = ${got[key]}, want ${want}`);
      }
    }
  }
});

test('groupRoleSamples fasst die Samples einer Abfrage zu einem Punkt zusammen', () => {
  const model = loadEnergyModel();
  const samples = [
    {entity_id: 'role:pv', timestamp: '2026-08-17T15:00:10Z', value: 700},
    {entity_id: 'role:grid', timestamp: '2026-08-17T15:00:00Z', value: 500},
    {entity_id: 'role:pv', timestamp: '2026-08-17T15:00:00Z', value: 640},
    {entity_id: 'sensor.something', timestamp: '2026-08-17T15:00:00Z', value: 12},
  ];
  const points = model.groupRoleSamples(samples);
  assert.equal(points.length, 2);
  assert.equal(points[0].timestamp, '2026-08-17T15:00:00Z');
  assert.equal(points[0].pv, 640);
  assert.equal(points[0].grid, 500);
  assert.equal(points[0]['sensor.something'], undefined);
  assert.equal(points[1].pv, 700);
});

test('snapshotFromPoint setzt nur die Rollen, die der Punkt wirklich enthaelt', () => {
  const model = loadEnergyModel();
  const snapshot = model.snapshotFromPoint({timestamp: '2026-08-17T15:00:00Z', pv: 640, grid: 511});
  assert.deepEqual(Object.keys(snapshot.values).sort(), ['grid', 'pv']);
  assert.equal(model.hasValue(snapshot, 'load'), false);
});

test('ein Verlaufspunkt ohne gemessenen Hausverbrauch wird gerechnet, nicht auf 0 gesetzt', () => {
  const model = loadEnergyModel();
  // Genau die Lage der Zielanlage: PV, Netzbezug, Batterie laedt, kein
  // Hausverbrauchszaehler. load_mode "auto" muss hier "calculated" waehlen.
  const snapshot = model.snapshotFromPoint({timestamp: '2026-08-17T15:00:00Z', pv: 640, grid: 511, battery: 130});
  const balance = model.deriveBalance(snapshot);
  assert.equal(balance.loadSource, 'calculated');
  assert.equal(Math.round(balance.load), 1021);
});

test('load_mode "measured" ohne Hausverbrauchsrolle meldet das im Verlauf ehrlich als fehlend', () => {
  const model = loadEnergyModel();
  const snapshot = model.snapshotFromPoint({timestamp: '2026-08-17T15:00:00Z', pv: 640, grid: 511});
  const balance = model.deriveBalance(snapshot, {load_mode: 'measured', gap_mode: 'diagnostic'});
  assert.equal(balance.loadSource, 'missing');
});

test('deriveBalanceCore trennt im Kombiniert-Modus den gemessenen Verbrauch vom Rest', () => {
  const model = loadEnergyModel();
  const snapshot = {values: {pv: 3000, load: 900, wallbox: 500}, roles: []};
  const n = model.deriveBalanceCore(snapshot, {load_mode: 'combined'});
  assert.equal(n.load_source, 'combined');
  assert.equal(n.load_total, 3000);
  assert.equal(n.load_measured, 900);
  assert.equal(n.base, 1600);
  assert.ok(Math.abs(n.gap_raw) < 1e-9);
});

test('composeBalance zeigt den gemessenen Verbrauch standardmaessig als eine Sammelposition', () => {
  const model = loadEnergyModel();
  const snapshot = {values: {pv: 3000, load: 900, wallbox: 500}, roles: []};
  const balance = model.deriveBalance(snapshot, {load_mode: 'combined'});
  const ids = JSON.parse(JSON.stringify(balance.sinks.map(f => f.id))).sort();
  assert.deepEqual(ids, ['base', 'load_measured', 'wallbox'].sort());
  const measured = balance.sinks.find(f => f.id === 'load_measured');
  assert.equal(measured.value, 900);
  assert.equal(measured.label, 'Gemessene Verbraucher');
  assert.equal(balance.measured, 900);
});

test('measured_split "entities" zerlegt den gemessenen Verbrauch in eine Position je load-Entitaet', () => {
  const model = loadEnergyModel();
  const snapshot = {
    values: {pv: 3000, load: 900, wallbox: 500},
    roles: [],
    entities: [
      {entity_id: 'werkstatt_power', label: 'Werkstatt / Leistung', value: 520, role: {role: 'load'}},
      {entity_id: 'waschmaschine_power', label: 'Waschmaschine / Leistung', value: 380, role: {role: 'load'}},
      {entity_id: 'wallbox_power', label: 'Wallbox / Ladeleistung', value: 500, role: {role: 'wallbox'}},
    ],
  };
  const balance = model.deriveBalance(snapshot, {load_mode: 'combined'}, {snapshot, measuredSplit: 'entities'});
  const measured = balance.sinks.filter(f => f.id.startsWith('load_measured'));
  assert.deepEqual(JSON.parse(JSON.stringify(measured.map(f => f.label))), ['Werkstatt / Leistung', 'Waschmaschine / Leistung']);
  assert.deepEqual(JSON.parse(JSON.stringify(measured.map(f => f.value))), [520, 380]);
  assert.notEqual(measured[0].color, measured[1].color);
});

test('measured_split "entities" ergaenzt eine Restposition, wenn die Einzelwerte die Summe nicht ausschoepfen', () => {
  const model = loadEnergyModel();
  // Eine der beiden load-Entitaeten liegt unter der 0,5-W-Rauschgrenze und
  // bekommt keine eigene Position - ihr Anteil darf trotzdem nicht aus der
  // Bilanz verschwinden.
  const snapshot = {
    values: {pv: 3000, load: 900, wallbox: 500},
    roles: [],
    entities: [
      {entity_id: 'werkstatt_power', label: 'Werkstatt / Leistung', value: 520, role: {role: 'load'}},
      {entity_id: 'rest_power', label: 'Rest / Leistung', value: 0.2, role: {role: 'load'}},
    ],
  };
  const balance = model.deriveBalance(snapshot, {load_mode: 'combined'}, {snapshot, measuredSplit: 'entities'});
  const rest = balance.sinks.find(f => f.id === 'load_measured_rest');
  assert.ok(rest, 'Restposition fehlt');
  assert.ok(Math.abs(rest.value - 380) < 0.01);
});

test('ausserhalb des Kombiniert-Modus gibt es keine Position fuer gemessenen Verbrauch', () => {
  const model = loadEnergyModel();
  const snapshot = {values: {pv: 3000, load: 900, wallbox: 500}, roles: []};
  for (const mode of ['measured', 'calculated', 'auto']) {
    const balance = model.deriveBalance(snapshot, {load_mode: mode});
    assert.equal(balance.measured, 0, mode);
    assert.equal(balance.sinks.some(f => f.id.startsWith('load_measured')), false, mode);
  }
});

test('deriveBalance beschriftet "base" im Kombiniert-Modus als berechnet', () => {
  const model = loadEnergyModel();
  const snapshot = {values: {pv: 3000, load: 900, wallbox: 500}, roles: []};
  const balance = model.deriveBalance(snapshot, {load_mode: 'combined'});
  assert.match(balance.sinks.find(f => f.id === 'base').label, /berechnet/);
});

// allocate() ordnet Quellen Verbrauchern zu (Spec "Anlagenschema:
// fliessende Skalierung", Abschnitt 5) - fuer die Verbraucherbalken in
// energy-schema.js. Drei Invarianten muessen in jedem Modus gelten.
function assertAllocateInvariants(balance, result) {
  for (const entry of result) {
    const sink = balance.sinks.find(f => f.id === entry.id);
    const mixSum = entry.mix.reduce((sum, m) => sum + m.v, 0);
    assert.ok(Math.abs(mixSum - sink.value) < 0.01, `mix sum for ${entry.id}`);
  }
  const total = result.reduce((sum, entry) => sum + entry.mix.reduce((s, m) => s + m.v, 0), 0);
  const throughput = balance.sources.reduce((sum, f) => sum + f.value, 0);
  assert.ok(Math.abs(total - throughput) < 0.01, 'sum of all mix values equals throughput');
}

test('allocate("merit") gibt dem ersten Verbraucher den saubersten Strom (PV vor Netzbezug)', () => {
  const model = loadEnergyModel();
  // PV deckt genau den Direktverbrauch, der Rest kommt vom Netz.
  const snapshot = {values: {pv: 2000, load: 3000, grid: 1000}, roles: []};
  const balance = model.deriveBalance(snapshot);
  const result = model.allocate(balance, 'merit');
  assertAllocateInvariants(balance, result);
  const base = result.find(e => e.id === 'base');
  const pvShare = base.mix.find(m => m.label === 'PV');
  const gridShare = base.mix.find(m => m.label === 'Netzbezug');
  assert.ok(pvShare && Math.abs(pvShare.v - 2000) < 0.01, 'PV deckt zuerst');
  assert.ok(gridShare && Math.abs(gridShare.v - 1000) < 0.01, 'Netz deckt den Rest');
});

test('allocate("merit") bevorzugt Direktverbrauch vor Speicherladung, wenn die saubere Quelle knapp ist', () => {
  const model = loadEnergyModel();
  // Zwei Quellen (Speicherentladung 1000, Netzbezug 3000), zwei Senken
  // (Direktverbrauch 2000, Speicherladung 2000) - konstruiert direkt ueber
  // composeBalance() statt eines physikalisch plausiblen Snapshots, um
  // allocate() isoliert zu pruefen.
  const balance = model.composeBalance({
    pv: 0, grid_import: 3000, grid_export: 0,
    battery_charge: 2000, battery_discharge: 1000,
    wallbox: 0, heat_pump: 0,
    load_total: 2000, load_source: 'measured', load_measured: 0, base: 2000,
    gap_raw: 0, gap_applied: 0, gap_absorbed: 0, gap_tolerance_w: 0, gap_ignored: true, unbalanced: false,
    autarkie: 0, eigenverbrauch: 0, netz: 0, total: 4000,
    battery_soc: 0, battery_capacity_kwh: 0, battery_energy_kwh: 0,
  }, {});
  const result = model.allocate(balance, 'merit');
  assertAllocateInvariants(balance, result);
  const base = result.find(e => e.id === 'base');
  const charge = result.find(e => e.id === 'battery_charge');
  assert.ok(base.mix.some(m => m.label === 'Batterie entlädt'), 'Direktverbrauch bekommt die knappe Speicherentladung mit ab');
  assert.ok(!charge.mix.some(m => m.label === 'Batterie entlädt'), 'Speicherladung bekommt keine Speicherentladung mehr ab, die ist schon verteilt');
  assert.ok(charge.mix.every(m => m.label === 'Netzbezug'), 'Speicherladung bekommt nur noch den Netzbezug-Rest');
});

test('allocate("prorata") gibt jedem Verbraucher denselben Mix', () => {
  const model = loadEnergyModel();
  const snapshot = {values: {pv: 2000, load: 3000, grid: 1000}, roles: []};
  const balance = model.deriveBalance(snapshot);
  const result = model.allocate(balance, 'prorata');
  assertAllocateInvariants(balance, result);
  const base = result.find(e => e.id === 'base');
  const pvFraction = base.mix.find(m => m.label === 'PV').v / base.v;
  assert.ok(Math.abs(pvFraction - 2000 / 3000) < 0.01, 'prorata mix spiegelt den Quellenanteil am Durchsatz');
});

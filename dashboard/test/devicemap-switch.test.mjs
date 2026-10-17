import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');

function loadSwitch() {
  const dom = new JSDOM('<!doctype html><html><head></head><body></body></html>', { runScripts: 'outside-only' });
  installI18n(dom.window);
  const context = dom.getInternalVMContext();
  for (const name of ['devicemap-model.js', 'devicemap-switch.js']) {
    vm.runInContext(fs.readFileSync(path.join(jsDir, name), 'utf8'), context);
  }
  return dom.window.DeviceMapSwitch;
}

const sw = (device_id, entity_id, state = 'on') => ({ device_id, entity_id, name: entity_id, state });
const dev = (id, units) => ({ id, entities: units.map((unit, i) => ({ entity_id: `${id}_${i}`, unit_of_measurement: unit })) });
const output = (entity_id, device_id) => ({ entity_id, device_id, function: 'output' });
const none = (entity_id, device_id) => ({ entity_id, device_id, function: 'none' });
const edge = (parent, child, extra = {}) => ({ data: { source: parent, target: child, connType: 'energy', ...extra } });
const plain = value => JSON.parse(JSON.stringify(value));
const plainPair = (parent, child) => ({ parent, child });

// Picks the fields a test asserts on out of a resolveOutputs entry.
const pick = (entry, fields) => Object.fromEntries(fields.map(field => [field, entry[field]]));

test('erkennt den einzigen Schalter eines Geräts mit Leistungsentity', () => {
  const S = loadSwitch();
  const outputs = S.resolveOutputs({ switches: [sw('plug', 'p1')], switchOutputs: [], devices: [dev('plug', ['W'])] });
  assert.equal(outputs.get('plug').entityId, 'p1');
  assert.equal(outputs.get('plug').source, 'heuristic');
});

test('kW zählt wie W', () => {
  const S = loadSwitch();
  const outputs = S.resolveOutputs({ switches: [sw('plug', 'p1')], switchOutputs: [], devices: [dev('plug', ['kW'])] });
  assert.equal(outputs.get('plug').entityId, 'p1');
  assert.equal(outputs.get('plug').source, 'heuristic');
});

test('kein Ausgang ohne Leistungsentity', () => {
  const S = loadSwitch();
  const outputs = S.resolveOutputs({ switches: [sw('plug', 'p1')], switchOutputs: [], devices: [dev('plug', ['%'])] });
  assert.equal(outputs.has('plug'), false);
});

test('kein Ausgang bei zwei Schaltern', () => {
  const S = loadSwitch();
  const outputs = S.resolveOutputs({
    switches: [sw('plug', 'p1'), sw('plug', 'p2')],
    switchOutputs: [],
    devices: [dev('plug', ['W'])],
  });
  assert.equal(outputs.has('plug'), false);
});

test('Konfigurationsschalter zählen nicht', () => {
  const S = loadSwitch();
  // The server only delivers switches without entity_category. A config LED
  // in the device entities must not count as a second switch.
  const device = { id: 'plug', entities: [
    { entity_id: 'plug_0', unit_of_measurement: 'W' },
    { entity_id: 'plug_led', unit_of_measurement: '', entity_category: 'config', component: 'switch' },
  ] };
  const outputs = S.resolveOutputs({ switches: [sw('plug', 'p1')], switchOutputs: [], devices: [device] });
  assert.equal(outputs.get('plug').entityId, 'p1');
  assert.equal(outputs.get('plug').source, 'heuristic');
});

test('Override none entfernt den erkannten Ausgang', () => {
  const S = loadSwitch();
  const outputs = S.resolveOutputs({
    switches: [sw('plug', 'p1')],
    switchOutputs: [none('p1', 'plug')],
    devices: [dev('plug', ['W'])],
  });
  assert.equal(outputs.has('plug'), false);
});

test('Override output gewinnt bei zwei Schaltern', () => {
  const S = loadSwitch();
  const outputs = S.resolveOutputs({
    switches: [sw('plug', 'p1'), sw('plug', 'p2')],
    switchOutputs: [output('p2', 'plug')],
    devices: [dev('plug', ['W'])],
  });
  assert.equal(outputs.get('plug').entityId, 'p2');
  assert.equal(outputs.get('plug').source, 'override');
});

test('Override output schaltet die Erkennung ab', () => {
  const S = loadSwitch();
  // No power entity, so detection would find nothing. The override still
  // makes the switch the output, and the source is the override, not the
  // heuristic.
  const outputs = S.resolveOutputs({
    switches: [sw('plug', 'p1')],
    switchOutputs: [output('p1', 'plug')],
    devices: [dev('plug', ['%'])],
  });
  assert.equal(outputs.get('plug').entityId, 'p1');
  assert.equal(outputs.get('plug').source, 'override');
});

test('Override auf verschwundene Entity ergibt keinen Ausgang', () => {
  const S = loadSwitch();
  const outputs = S.resolveOutputs({
    switches: [sw('plug', 'p1')],
    switchOutputs: [output('gone', 'plug')],
    devices: [dev('plug', ['W'])],
  });
  assert.equal(outputs.has('plug'), false);
});

test('setFunction output ersetzt den bisherigen Ausgang des Geräts', () => {
  const S = loadSwitch();
  const next = S.setFunction([output('a', 'd')], { deviceId: 'd', entityId: 'b', fn: 'output' });
  assert.deepEqual(JSON.parse(JSON.stringify(next)), [{ entity_id: 'b', device_id: 'd', function: 'output' }]);
});

test('setFunction none speichert einen expliziten Eintrag', () => {
  const S = loadSwitch();
  const next = S.setFunction([], { deviceId: 'd', entityId: 'b', fn: 'none' });
  assert.deepEqual(JSON.parse(JSON.stringify(next)), [{ entity_id: 'b', device_id: 'd', function: 'none' }]);
});

test('rows markiert Abweichungen vom gespeicherten Stand als draft, sonst heuristic, override oder none', () => {
  const S = loadSwitch();
  const switches = [sw('plug', 'p1'), sw('plug', 'p2')];
  const devices = [dev('plug', ['W'])];
  const sourceOf = (rows, entityId) => rows.find(row => row.entityId === entityId).source;

  // Two switches, nothing saved, nothing drafted: both are plain none.
  const unchanged = S.rows({ deviceId: 'plug', switches, switchOutputs: [], savedSwitchOutputs: [], devices });
  assert.equal(sourceOf(unchanged, 'p1'), 'none');
  assert.equal(sourceOf(unchanged, 'p2'), 'none');

  // One switch with power: detected, shown as heuristic.
  const detected = S.rows({ deviceId: 'plug', switches: [sw('plug', 'p1')], switchOutputs: [], savedSwitchOutputs: [], devices });
  assert.equal(detected.find(row => row.entityId === 'p1').fn, 'output');
  assert.equal(sourceOf(detected, 'p1'), 'heuristic');

  // Saved override output on p2, unchanged in the draft: override.
  const saved = [output('p2', 'plug')];
  const overridden = S.rows({ deviceId: 'plug', switches, switchOutputs: saved, savedSwitchOutputs: saved, devices });
  assert.equal(overridden.find(row => row.entityId === 'p2').fn, 'output');
  assert.equal(sourceOf(overridden, 'p2'), 'override');

  // Draft moves the output from p2 to p1: both entries differ from the saved state.
  const drafted = S.rows({ deviceId: 'plug', switches, switchOutputs: [output('p1', 'plug')], savedSwitchOutputs: saved, devices });
  assert.equal(drafted.find(row => row.entityId === 'p1').fn, 'output');
  assert.equal(sourceOf(drafted, 'p1'), 'draft');
  assert.equal(sourceOf(drafted, 'p2'), 'draft');
});

test('deadArea: Kette markiert alle Knoten hinter dem Plug', () => {
  const S = loadSwitch();
  const dead = S.deadArea({
    pairs: [plainPair('grid', 'P'), plainPair('P', 'wr'), plainPair('wr', 'akku')],
    offIds: new Set(['P']),
  });
  assert.deepEqual(plain(dead.get('wr')), { plug: 'P', hops: 1 });
  assert.deepEqual(plain(dead.get('akku')), { plug: 'P', hops: 2 });
  assert.equal(dead.has('P'), false, 'the plug itself is never dead');
  assert.equal(dead.has('grid'), false);
  assert.equal(dead.size, 2);
});

test('deadArea: Abzweig ohne Plug bleibt aktiv', () => {
  const S = loadSwitch();
  const dead = S.deadArea({
    pairs: [plainPair('grid', 'P'), plainPair('P', 'a'), plainPair('grid', 'b')],
    offIds: new Set(['P']),
  });
  assert.deepEqual(plain([...dead.keys()]), ['a']);
  assert.deepEqual(plain(dead.get('a')), { plug: 'P', hops: 1 });
});

test('deadArea: zweite Einspeisung hält den Knoten am Strom', () => {
  const S = loadSwitch();
  const dead = S.deadArea({
    pairs: [plainPair('grid', 'P'), plainPair('P', 'wr'), plainPair('grid2', 'wr')],
    offIds: new Set(['P']),
  });
  assert.equal(dead.size, 0);
});

test('deadArea: Plug als Blatt ist leer, status meldet ihn als leafOff', () => {
  const S = loadSwitch();
  const pairs = [plainPair('grid', 'P')];
  assert.equal(S.deadArea({ pairs, offIds: new Set(['P']) }).size, 0);

  const devices = [dev('P', ['W'])];
  const status = S.status({ switches: [sw('P', 'p1', 'off')], switchOutputs: [], devices, pairs });
  assert.equal(status.leafOff.has('P'), true);
  assert.equal(status.off.has('P'), true);
});

test('deadArea: zwei Plugs in Reihe, nächster Plug gewinnt', () => {
  const S = loadSwitch();
  const dead = S.deadArea({
    pairs: [plainPair('grid', 'A'), plainPair('A', 'B'), plainPair('B', 'c')],
    offIds: new Set(['A', 'B']),
  });
  assert.deepEqual(plain([...dead.keys()]), ['c']);
  assert.deepEqual(plain(dead.get('c')), { plug: 'B', hops: 1 });
});

test('deadArea: Zyklus terminiert und markiert nur den Teil hinter dem Plug', () => {
  const S = loadSwitch();
  const dead = S.deadArea({
    pairs: [plainPair('r', 'a'), plainPair('a', 'P'), plainPair('P', 'b'), plainPair('b', 'a')],
    offIds: new Set(['P']),
  });
  assert.deepEqual(plain([...dead.keys()]), ['b']);
  assert.deepEqual(plain(dead.get('b')), { plug: 'P', hops: 1 });
});

test('pathPairs: Gruppen und Mitgliedskanten zählen nicht', () => {
  const S = loadSwitch();
  const G = 'group:g1';
  const pairs = S.pathPairs([
    edge('grid', 'P'),
    edge('grid', 'data-only', { connType: 'data' }),
    edge(G, 'x'),
    edge('y', G),
    edge(G, 'z', { membership: { group: 'g1', member: 'z' } }),
    edge('grid', 'm', { membership: { group: 'g1', member: 'm' } }),
  ]);
  assert.deepEqual(plain(pairs), [plainPair('grid', 'P')]);
});

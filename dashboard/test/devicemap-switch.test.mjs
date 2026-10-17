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
  for (const name of ['devicemap-model.js', 'devicemap-graph.js', 'devicemap-switch.js']) {
    vm.runInContext(fs.readFileSync(path.join(jsDir, name), 'utf8'), context);
  }
  return dom.window.DeviceMapSwitch;
}

const sw = (device_id, entity_id, state = 'on') => ({ device_id, entity_id, name: entity_id, state });
const dev = (id, units) => ({ id, entities: units.map((unit, i) => ({ entity_id: `${id}_${i}`, unit_of_measurement: unit })) });
const edge = (parent, child, extra = {}) => ({ data: { source: parent, target: child, connType: 'energy', ...extra } });
const plain = value => JSON.parse(JSON.stringify(value));
const pair = (parent, child, channel = 1) => ({ parent, child, channel });
const role = (entity_id, channel) => ({ role: 'switch', channel });
// Keys of the cut edges: the same relation key the graph uses for pairs.
const keyOf = (parent, child) => [parent, child].sort().join('::');

test('erkennt den einzigen Schalter eines Geräts mit Leistungsentity', () => {
  const S = loadSwitch();
  const byDevice = S.resolveSwitches({ switches: [sw('plug', 'p1')], assignments: {}, devices: [dev('plug', ['W'])] });
  assert.deepEqual(plain(byDevice.get('plug')), [{ entityId: 'p1', name: 'p1', state: 'on', channel: 1, source: 'heuristic' }]);
});

test('kW zählt wie W', () => {
  const S = loadSwitch();
  const byDevice = S.resolveSwitches({ switches: [sw('plug', 'p1')], assignments: {}, devices: [dev('plug', ['kW'])] });
  assert.equal(byDevice.get('plug')[0].source, 'heuristic');
});

test('kein Schalter ohne Leistungsentity', () => {
  const S = loadSwitch();
  const byDevice = S.resolveSwitches({ switches: [sw('plug', 'p1')], assignments: {}, devices: [dev('plug', ['%'])] });
  assert.equal(byDevice.has('plug'), false);
});

test('keine Erkennung bei zwei Schaltern', () => {
  const S = loadSwitch();
  const byDevice = S.resolveSwitches({
    switches: [sw('plug', 'p1'), sw('plug', 'p2')],
    assignments: {},
    devices: [dev('plug', ['W'])],
  });
  assert.equal(byDevice.has('plug'), false);
});

test('Konfigurationsschalter zählen nicht', () => {
  const S = loadSwitch();
  // The server only delivers switches without entity_category. A config LED
  // in the device entities must not count as a second switch.
  const device = { id: 'plug', entities: [
    { entity_id: 'plug_0', unit_of_measurement: 'W' },
    { entity_id: 'plug_led', unit_of_measurement: '', entity_category: 'config', component: 'switch' },
  ] };
  const byDevice = S.resolveSwitches({ switches: [sw('plug', 'p1')], assignments: {}, devices: [device] });
  assert.equal(byDevice.get('plug')[0].entityId, 'p1');
});

test('a single switch on a device with power is detected on channel 1', () => {
  const S = loadSwitch();
  const byDevice = S.resolveSwitches({ switches: [sw('plug', 's')], assignments: {}, devices: [dev('plug', ['W'])] });
  assert.deepEqual(plain(byDevice.get('plug')), [{ entityId: 's', name: 's', state: 'on', channel: 1, source: 'heuristic' }]);
});

test('a saved empty role turns detection off', () => {
  const S = loadSwitch();
  const byDevice = S.resolveSwitches({
    switches: [sw('plug', 's')],
    assignments: { s: { role: '' } },
    devices: [dev('plug', ['W'])],
  });
  assert.equal(byDevice.has('plug'), false);
});

test('any saved switch role on a device replaces detection for all its switches', () => {
  const S = loadSwitch();
  const byDevice = S.resolveSwitches({
    switches: [sw('plug', 's1'), sw('plug', 's2')],
    assignments: { s2: { role: 'switch' } },
    devices: [dev('plug', ['W'])],
  });
  assert.deepEqual(plain(byDevice.get('plug')), [{ entityId: 's2', name: 's2', state: 'on', channel: 1, source: 'override' }]);
});

test('two switch roles on one device give each its channel, sorted by channel', () => {
  const S = loadSwitch();
  const byDevice = S.resolveSwitches({
    switches: [sw('pm', 's1'), sw('pm', 's2', 'off')],
    assignments: { s1: role('s1', 1), s2: role('s2', 2) },
    devices: [dev('pm', ['W'])],
  });
  assert.deepEqual(plain(byDevice.get('pm').map(entry => [entry.entityId, entry.channel, entry.state, entry.source])), [
    ['s1', 1, 'on', 'override'],
    ['s2', 2, 'off', 'override'],
  ]);
});

test('a switch role without channel counts as channel 1', () => {
  const S = loadSwitch();
  const byDevice = S.resolveSwitches({
    switches: [sw('plug', 's')],
    assignments: { s: { role: 'switch' } },
    devices: [dev('plug', [])],
  });
  assert.equal(byDevice.get('plug')[0].channel, 1);
});

test('a switch role for a missing entity switches nothing', () => {
  const S = loadSwitch();
  const { byDevice, edges } = S.status({
    switches: [],
    assignments: { gone: { role: 'switch' } },
    devices: [],
    pairs: [pair('gone', 'x')],
  });
  assert.equal(byDevice.size, 0);
  assert.equal(edges.size, 0);
});

test('two channels, one off: only that edge is cut', () => {
  const S = loadSwitch();
  const { edges, off, dead } = S.status({
    switches: [sw('pm', 's1', 'on'), sw('pm', 's2', 'off')],
    assignments: { s1: role('s1', 1), s2: role('s2', 2) },
    devices: [dev('pm', ['W'])],
    pairs: [pair('pm', 'a', 1), pair('pm', 'b', 2), pair('b', 'c')],
  });
  assert.equal(edges.has(keyOf('pm', 'a')), true);
  assert.equal(edges.has(keyOf('pm', 'b')), true);
  assert.equal(edges.get(keyOf('pm', 'b')).state, 'off');
  assert.equal(edges.get(keyOf('pm', 'b')).plug, 'pm');
  assert.deepEqual(plain([...dead.keys()].sort()), ['b', 'c']);
  assert.equal(dead.get('b').plug, 'pm');
  assert.equal(dead.get('c').plug, 'pm');
  assert.equal(dead.has('a'), false, 'the channel that is on keeps its branch live');
  assert.equal(off.has('pm'), false, 'a device with one channel on is not off');
});

test('an edge on a channel without a switch is not switched', () => {
  const S = loadSwitch();
  const { edges, dead } = S.status({
    switches: [sw('pm', 's1', 'off')],
    assignments: { s1: role('s1', 1) },
    devices: [dev('pm', ['W'])],
    pairs: [pair('pm', 'b', 2)],
  });
  assert.equal(edges.has(keyOf('pm', 'b')), false);
  assert.equal(dead.size, 0);
});

test('leaf lists switch devices without a switched edge', () => {
  const S = loadSwitch();
  const { leaf, off } = S.status({
    switches: [sw('plug', 'p1', 'off')],
    assignments: { p1: role('p1', 1) },
    devices: [dev('plug', ['W'])],
    pairs: [],
  });
  assert.equal(leaf.get('plug'), 'off');
  assert.equal(off.has('plug'), true);
});

test('setFunction and rows are gone, the module exposes the new API', () => {
  const S = loadSwitch();
  assert.equal(S.setFunction, undefined);
  assert.equal(S.rows, undefined);
  assert.equal(S.resolveOutputs, undefined);
  for (const name of ['resolveSwitches', 'edgeChannel', 'pathPairs', 'deadArea', 'status']) {
    assert.equal(typeof S[name], 'function', name);
  }
});

test('edgeChannel is 1 without an entry and reads the saved channel otherwise', () => {
  const S = loadSwitch();
  assert.equal(S.edgeChannel([], 'pm', 'b'), 1);
  assert.equal(S.edgeChannel([{ parent_id: 'pm', child_id: 'b', type: 'energy', channel: 2 }], 'b', 'pm'), 2);
  assert.equal(S.edgeChannel([{ parent_id: 'pm', child_id: 'b', type: 'energy' }], 'pm', 'b'), 1);
});

test('pathPairs carries the channel of each edge', () => {
  const S = loadSwitch();
  const props = [{ parent_id: 'pm', child_id: 'b', type: 'energy', channel: 2 }];
  const pairs = S.pathPairs([edge('pm', 'a'), edge('pm', 'b')], props);
  assert.deepEqual(plain(pairs), [pair('pm', 'a', 1), pair('pm', 'b', 2)]);
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
  ], []);
  assert.deepEqual(plain(pairs), [pair('grid', 'P', 1)]);
});

test('deadArea: Kette markiert alle Knoten hinter dem Plug', () => {
  const S = loadSwitch();
  const dead = S.deadArea({
    pairs: [pair('grid', 'P'), pair('P', 'wr'), pair('wr', 'akku')],
    cutKeys: new Set([keyOf('P', 'wr')]),
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
    pairs: [pair('grid', 'P'), pair('P', 'a'), pair('grid', 'b')],
    cutKeys: new Set([keyOf('P', 'a')]),
  });
  assert.deepEqual(plain([...dead.keys()]), ['a']);
  assert.deepEqual(plain(dead.get('a')), { plug: 'P', hops: 1 });
});

test('deadArea: zweite Einspeisung hält den Knoten am Strom', () => {
  const S = loadSwitch();
  const dead = S.deadArea({
    pairs: [pair('grid', 'P'), pair('P', 'wr'), pair('grid2', 'wr')],
    cutKeys: new Set([keyOf('P', 'wr')]),
  });
  assert.equal(dead.size, 0);
});

test('deadArea: Plug als Blatt ist leer', () => {
  const S = loadSwitch();
  const dead = S.deadArea({ pairs: [pair('grid', 'P')], cutKeys: new Set() });
  assert.equal(dead.size, 0);
});

test('deadArea: zwei Plugs in Reihe, nächster Plug gewinnt', () => {
  const S = loadSwitch();
  const dead = S.deadArea({
    pairs: [pair('grid', 'A'), pair('A', 'B'), pair('B', 'c')],
    cutKeys: new Set([keyOf('A', 'B'), keyOf('B', 'c')]),
  });
  assert.deepEqual(plain([...dead.keys()]), ['c']);
  assert.deepEqual(plain(dead.get('c')), { plug: 'B', hops: 1 });
});

test('deadArea: Zyklus terminiert und markiert nur den Teil hinter dem Plug', () => {
  const S = loadSwitch();
  const dead = S.deadArea({
    pairs: [pair('r', 'a'), pair('a', 'P'), pair('P', 'b'), pair('b', 'a')],
    cutKeys: new Set([keyOf('P', 'b')]),
  });
  assert.deepEqual(plain([...dead.keys()]), ['b']);
  assert.deepEqual(plain(dead.get('b')), { plug: 'P', hops: 1 });
});

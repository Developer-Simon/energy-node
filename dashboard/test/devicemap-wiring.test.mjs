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

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  installI18n(dom.window);
  const context = dom.getInternalVMContext();
  for (const name of ['devicemap-model.js', 'devicemap-graph.js', 'devicemap-wiring.js']) {
    vm.runInContext(fs.readFileSync(path.join(jsDir, name), 'utf8'), context);
  }
  return dom.window.DeviceMapWiring;
}
const plain = value => JSON.parse(JSON.stringify(value));

const energy = new Map([
  ['wr', { power: -800 }], ['akku', { power: 300 }], ['shelly', { power: null }],
]);

test('a saved type wins over the heuristic', () => {
  const w = load();
  const index = w.propsIndex([{ parent_id: 'akku', child_id: 'wr', type: 'info' }]);
  assert.deepEqual(plain(w.resolve({ parent: 'wr', child: 'akku' }, index, energy)), { type: 'info', direction: 'auto', heuristic: false });
});

test('heuristic: energy when both ends carry a power role, data otherwise', () => {
  const w = load();
  const index = w.propsIndex([]);
  assert.equal(w.resolve({ parent: 'wr', child: 'akku' }, index, energy).type, 'energy');
  assert.equal(w.resolve({ parent: 'wr', child: 'shelly' }, index, energy).type, 'data');
  assert.equal(w.resolve({ parent: 'wr', child: 'akku' }, index, energy).heuristic, true);
});

test('energy_node never counts as an energy end, a group always does', () => {
  const w = load();
  const withOwn = new Map([...energy, ['energy_node', { power: 500 }]]);
  assert.equal(w.resolve({ parent: 'energy_node', child: 'wr' }, w.propsIndex([]), withOwn).type, 'data');
  assert.equal(w.resolve({ parent: 'wr', child: 'group:uv' }, w.propsIndex([]), energy).type, 'energy');
  assert.deepEqual(plain(w.resolve({ parent: 'group:uv', child: 'shelly', membership: { group: 'uv' } }, w.propsIndex([]), energy)), { type: 'energy', direction: 'auto', heuristic: false });
});

test('applyDirection', () => {
  const w = load();
  assert.equal(w.applyDirection(-200, 'auto'), -200);
  assert.equal(w.applyDirection(-200, 'reversed'), 200);
  assert.equal(w.applyDirection(-200, 'fixed_down'), 200);
  assert.equal(w.applyDirection(200, 'fixed_up'), -200);
});

test('setProp replaces a pair in either orientation and drops direction for non energy', () => {
  const w = load();
  const start = [{ parent_id: 'a', child_id: 'b', type: 'energy', direction: 'reversed' }];
  assert.deepEqual(plain(w.setProp(start, { parent: 'b', child: 'a', type: 'data', direction: 'reversed' })), [{ parent_id: 'b', child_id: 'a', type: 'data' }]);
  assert.deepEqual(plain(w.setProp([], { parent: 'a', child: 'b', type: 'energy', direction: 'auto' })), [{ parent_id: 'a', child_id: 'b', type: 'energy', direction: 'auto' }]);
});

test('prune drops props of pairs that are gone', () => {
  const w = load();
  const props = [{ parent_id: 'a', child_id: 'b', type: 'info' }, { parent_id: 'c', child_id: 'd', type: 'info' }];
  assert.deepEqual(plain(w.prune(props, new Set(['a::b']))), [props[0]]);
});

test('energyPairs keeps only energy edges with their direction', () => {
  const w = load();
  const index = w.propsIndex([{ parent_id: 'wr', child_id: 'akku', type: 'energy', direction: 'reversed' }]);
  const pairs = [{ parent: 'wr', child: 'akku' }, { parent: 'wr', child: 'shelly' }];
  assert.deepEqual(plain(w.energyPairs(pairs, index, energy)), [{ parent: 'wr', child: 'akku', direction: 'reversed' }]);
});

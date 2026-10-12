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

export function loadGraph() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  installI18n(dom.window);
  const context = dom.getInternalVMContext();
  for (const name of ['devicemap-model.js', 'devicemap-node-svg.js', 'devicemap-graph.js', 'devicemap-wiring.js']) {
    vm.runInContext(fs.readFileSync(path.join(jsDir, name), 'utf8'), context);
  }
  return dom.window;
}

const plain = value => JSON.parse(JSON.stringify(value));

test('wiringElements prefers override ids and memberships over discovery relations', () => {
  const { DeviceMapGraph } = loadGraph();
  const edges = DeviceMapGraph.wiringElements({
    devices: [{ id: 'netz', relations: [{ kind: 'child', id: 'pv' }] }, { id: 'pv', relations: [{ kind: 'parent', id: 'netz' }] }, { id: 'wb' }],
    deviceMap: { edges: [{ id: 'o1', child_id: 'pv', parent_id: 'netz' }] },
    groups: { uv: { members: { devices: ['wb'], groups: [] } } },
    nodeIds: new Set(['netz', 'pv', 'wb', 'group:uv']),
  });
  assert.deepEqual(plain(edges.map(edge => edge.data)), [
    { id: 'edge-group:uv::wb', source: 'group:uv', target: 'wb', membership: { group: 'uv', member: 'wb' }, connType: 'energy', direction: 'auto', heuristic: false },
    { id: 'edge-netz::pv', source: 'netz', target: 'pv', overrideId: 'o1', connType: 'data', direction: 'auto', heuristic: true },
  ]);
});

test('wiring edges carry their connection type as class and data', () => {
  const { DeviceMapGraph } = loadGraph();
  const devices = [
    { id: 'wr', relations: [{ id: 'akku', kind: 'child' }] },
    { id: 'akku', relations: [{ id: 'wr', kind: 'parent' }] },
    { id: 'shelly', relations: [{ id: 'wr', kind: 'parent' }] },
  ];
  const energy = new Map([['wr', { power: -800 }], ['akku', { power: 300 }]]);
  const elements = DeviceMapGraph.wiringElements({ devices, deviceMap: { edges: [], edge_props: [] }, groups: {}, nodeIds: new Set(['wr', 'akku', 'shelly']), energy });
  const byTarget = Object.fromEntries(elements.map(element => [element.data.target, element]));
  assert.equal(byTarget.akku.data.connType, 'energy');
  assert.match(byTarget.akku.classes, /devicemap-conn-energy/);
  assert.match(byTarget.akku.classes, /devicemap-conn-detected/);
  assert.equal(byTarget.shelly.data.connType, 'data');
});

test('flows run only over energy edges and follow the direction', () => {
  const { DeviceMapGraph } = loadGraph();
  const pairs = [{ parent: 'wr', child: 'akku' }, { parent: 'wr', child: 'shelly' }];
  const energy = new Map([['wr', { power: -800, roles: [], meta: {}, byRole: {} }], ['akku', { power: 300, roles: [], meta: {}, byRole: {} }], ['shelly', { power: 40, roles: [], meta: {}, byRole: {} }]]);
  const flows = DeviceMapGraph.flowElements({ pairs, energy, view: { width_by_power: false }, edgeProps: [
    { parent_id: 'wr', child_id: 'akku', type: 'energy', direction: 'reversed' },
    { parent_id: 'wr', child_id: 'shelly', type: 'data' },
  ] });
  assert.equal(flows.length, 1);
  assert.equal(flows[0].data.source, 'akku');
  assert.equal(flows[0].data.reverse, true);
});

test('style keeps the wiring rules of the draft', () => {
  const { DeviceMapGraph } = loadGraph();
  const theme = { line: '#111', accent: '#222', labelStrong: '#333', panel: '#444' };
  assert.deepEqual(plain(DeviceMapGraph.wiringEdgeStyle({ layers: { wiring: false, energy: false } }, theme)), { display: 'none' });
  const style = DeviceMapGraph.style({ view: { edge_style: 'elbow', layers: { wiring: true, energy: true } }, theme, color: () => '#000', reducedMotion: false });
  assert.ok(style.some(rule => rule.selector === 'edge.devicemap-flow'));
});

test('data edge elements carry their category class and a prefixed id', () => {
  const { DeviceMapGraph } = loadGraph();
  const [edge] = DeviceMapGraph.dataEdgeElements([{ id: 'svc:a', cat: 'service', from: 'bms', to: 'soc' }]);
  assert.equal(edge.data.id, 'data-svc:a');
  assert.equal(edge.classes, 'devicemap-data devicemap-data-service');
});

test('virtual elements get the virtual and kind classes and keep known positions', () => {
  const { DeviceMapGraph } = loadGraph();
  const positions = new Map([['balance', { x: 10, y: 20 }]]);
  const [balance, rule] = DeviceMapGraph.virtualElements({
    kinds: [{ id: 'balance', kind: 'balance', name: 'Bilanz' }, { id: 'rule:r1', kind: 'rule', name: 'Regel' }],
    positions,
    colorOf: () => '#000',
  });
  assert.equal(balance.classes, 'devicemap-virtual devicemap-balance');
  assert.deepEqual(JSON.parse(JSON.stringify(balance.position)), { x: 10, y: 20 });
  assert.equal(rule.classes, 'devicemap-virtual devicemap-rule');
  assert.equal(rule.position, undefined);
});

test('the flow animator runs one loop and stops itself', () => {
  const { DeviceMapGraph } = loadGraph();
  const frames = [];
  let animate = true;
  const offsets = [];
  const edge = { data: () => 'mid', style: (key, value) => offsets.push([key, value]) };
  const cy = { batch: fn => fn(), edges: () => ({ forEach: fn => fn(edge) }) };
  const animator = DeviceMapGraph.createFlowAnimator({ getCy: () => cy, shouldAnimate: () => animate, requestFrame: fn => { frames.push(fn); return frames.length; }, cancelFrame: () => {} });
  animator.start();
  animator.start();
  assert.equal(frames.length, 1, 'a second start does not stack loops');
  frames.shift()(1000);
  assert.equal(offsets.length, 1);
  assert.equal(offsets[0][0], 'line-dash-offset');
  animate = false;
  frames.shift()(1100);
  assert.equal(frames.length, 0, 'the loop ends when nothing should move');
});

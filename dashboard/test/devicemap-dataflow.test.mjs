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
  for (const name of ['devicemap-model.js', 'devicemap-graph.js', 'devicemap-dataflow.js']) {
    vm.runInContext(fs.readFileSync(path.join(jsDir, name), 'utf8'), context);
  }
  return dom.window.DeviceMapDataflow;
}
const plain = value => JSON.parse(JSON.stringify(value));
const ALL = { wiring: true, energy: true, balance: true, data: true };

test('visibility follows the layer table of the spec', () => {
  const df = load();
  const off = { wiring: true, energy: true, balance: false, data: false };
  assert.equal(df.nodeVisible('balance', off), false);
  assert.equal(df.nodeVisible('balance', { ...off, balance: true }), true);
  assert.equal(df.nodeVisible('rule', { ...off, balance: true }), false);
  assert.equal(df.nodeVisible('stub', { ...off, data: true }), true);
  assert.equal(df.nodeVisible('group', off), true);
  const role = { cat: 'role', from: 'pv', to: 'balance' };
  const balanceRule = { cat: 'automation', from: 'balance', to: 'rule:r1', viaBalance: true };
  const service = { cat: 'service', from: 'bms', to: 'soc' };
  assert.equal(df.edgeVisible(role, { ...off, balance: true }), true);
  assert.equal(df.edgeVisible(role, { ...off, data: true }), false);
  assert.equal(df.edgeVisible(balanceRule, { ...off, data: true }), true, 'balance conditions follow the data layer alone');
  assert.equal(df.edgeVisible(balanceRule, { ...off, balance: true }), false);
  assert.equal(df.edgeVisible(balanceRule, ALL), true);
  assert.equal(df.edgeVisible(service, { ...off, data: true }), true);
});

test('node kinds and ids', () => {
  const df = load();
  assert.equal(df.nodeKind('balance'), 'balance');
  assert.equal(df.nodeKind('group:uv'), 'group');
  assert.equal(df.nodeKind('rule:a%20b'), 'rule');
  assert.equal(df.nodeKind('service:battery_soc_devices/bank'), 'service');
  assert.equal(df.nodeKind('stub:svc:x'), 'stub');
  assert.equal(df.nodeKind('wallbox'), 'device');
  assert.equal(df.isPersistedVirtual('stub:svc:x'), false);
  assert.equal(df.ruleId('Überschuss in Wallbox/2'), 'rule:%C3%9Cberschuss%20in%20Wallbox%2F2');
});

test('flowEdges maps refs to node ids and drops edges with unknown ends', () => {
  const df = load();
  const graph = { edges: [
    { id: 'svc:a', cat: 'service', from: { device_id: 'bms', entity_id: 'v' }, to: { device_id: 'soc' }, title: 'Voltage bank A', title_key: 'dataflow.x', details: { topic: 't' }, link: { tab: 'config' } },
    { id: 'svc:gone', cat: 'service', from: { device_id: 'removed' }, to: { device_id: 'soc' }, title: 'x', details: {}, link: {} },
    { id: 'auto:r/c0', cat: 'automation', from: { virtual_id: 'balance' }, to: { virtual_id: 'rule:r' }, title: 'y', details: {}, link: {} },
  ] };
  const edges = df.flowEdges(graph, new Set(['bms', 'soc', 'balance', 'rule:r']));
  assert.deepEqual(edges.map(edge => [edge.id, edge.from, edge.to, edge.fromEntity || '']), [
    ['svc:a', 'bms', 'soc', 'v'],
    ['auto:r/c0', 'balance', 'rule:r', ''],
  ]);
});

test('stubs attach unresolved inputs to their target and count unreadable files', () => {
  const df = load();
  const graph = { unresolved: [
    { id: 'svc:b/neu/bank_a_voltage_topic', reason: 'no_producer', direction: 'input', target: { virtual_id: 'service:b/neu' }, topic: 'x' },
    { id: 'auto:rule:r/a0', reason: 'no_consumer', direction: 'output', target: { virtual_id: 'rule:r' }, topic: 'y' },
    { id: 'config:kaputt', reason: 'config_invalid', target: {} },
    { id: 'svc:c/x', reason: 'no_producer', direction: 'input', target: { device_id: 'unbekannt' } },
  ] };
  const result = df.stubs(graph, new Set(['service:b/neu', 'rule:r']));
  assert.deepEqual(plain(result.edges.map(edge => [edge.from, edge.to])), [
    ['stub:svc:b/neu/bank_a_voltage_topic', 'service:b/neu'],
    ['rule:r', 'stub:auto:rule:r/a0'],
  ]);
  assert.equal(result.nodes.length, 2);
  assert.equal(result.invalid, 1);
});

test('roleEdges connect every device with a role to the balance', () => {
  const df = load();
  const edges = df.roleEdges({ entities: [
    { device_id: 'pv', entity_id: 'pv1', role: { role: 'pv', source: 'override' } },
    { device_id: 'pv', entity_id: 'pv2', role: { role: 'pv', source: 'heuristic' } },
    { device_id: 'energy_node', entity_id: 'own', role: { role: 'grid', source: 'override' } },
    { device_id: 'x', entity_id: 'x1', role: null },
  ] });
  assert.deepEqual(plain(edges), [{ id: 'role:pv', cat: 'role', from: 'pv', to: 'balance', roles: ['pv'], entities: ['pv1', 'pv2'], heuristic: true }]);
});

test('relatedFocus walks visible data edges both ways and adds direct wiring neighbours', () => {
  const df = load();
  const dataEdges = [
    { id: 'd1', cat: 'service', from: 'bms_a', to: 'soc' },
    { id: 'd2', cat: 'automation', from: 'soc', to: 'rule:r2' },
    { id: 'd3', cat: 'automation', from: 'rule:r2', to: 'wp' },
    { id: 'r1', cat: 'role', from: 'soc', to: 'balance' },
  ];
  const wiringPairs = [{ parent: 'bat', child: 'bms_a' }, { parent: 'netz', child: 'bat' }];
  const focus = df.relatedFocus('soc', { wiringPairs, dataEdges, layers: { wiring: true, energy: false, balance: false, data: true } });
  assert.deepEqual([...focus.nodes].sort(), ['bms_a', 'rule:r2', 'soc', 'wp']);
  assert.deepEqual([...focus.edges].sort(), ['d1', 'd2', 'd3']);
  const fromBms = df.relatedFocus('bms_a', { wiringPairs, dataEdges, layers: ALL });
  assert.ok(fromBms.nodes.has('bat'), 'direct wiring neighbour');
  assert.ok(!fromBms.nodes.has('netz'), 'wiring is not walked transitively');
  assert.ok(fromBms.edges.has('bat::bms_a'));
});

test('placement: balance right of the arrangement, flow nodes in a band below sorted by input x', () => {
  const df = load();
  const snap = value => Math.round(value / 20) * 20;
  const all = [{ x: 100, y: 80 }, { x: 600, y: 80 }, { x: 300, y: 400 }];
  assert.deepEqual(plain(df.placeBalance({ rolePositions: [{ x: 100, y: 80 }, { x: 300, y: 400 }], allPositions: all, snap })), { x: 740, y: 240 });
  const placed = df.placeFlowNodes({ nodes: [{ id: 'rule:b', inputXs: [600] }, { id: 'rule:a', inputXs: [100, 200] }, { id: 'service:c', inputXs: [] }], allPositions: all, snap });
  assert.deepEqual(plain(placed), [
    { id: 'rule:a', x: 100, y: 520 },
    { id: 'rule:b', x: 240, y: 520 },
    { id: 'service:c', x: 380, y: 520 },
  ]);
});

test('rules with an anchor go in a row centred below it', () => {
  const df = load();
  const spots = plain(df.placeFlowNodes({
    nodes: [{ id: 'rule:a', inputXs: [], anchor: { x: 280, y: 0 } }, { id: 'rule:b', inputXs: [], anchor: { x: 280, y: 0 } }],
    allPositions: [{ x: 0, y: 0 }, { x: 280, y: 0 }], snap: v => v,
  }));
  assert.deepEqual(spots, [{ id: 'rule:a', x: 210, y: 120 }, { id: 'rule:b', x: 350, y: 120 }]);
});

test('an anchored spot that is taken moves right', () => {
  const df = load();
  const spots = plain(df.placeFlowNodes({
    nodes: [{ id: 'rule:a', inputXs: [], anchor: { x: 0, y: 0 } }],
    allPositions: [{ x: 0, y: 0 }, { x: 0, y: 120 }], snap: v => v,
  }));
  assert.deepEqual(spots, [{ id: 'rule:a', x: 140, y: 120 }]);
});

test('edgeView builds the popover rows of a service edge', () => {
  const df = load();
  const labelOf = id => ({ bms: 'BMS Bank A', soc: 'Batterie SoC' }[id] || id);
  const view = df.edgeView({ id: 'svc:a', cat: 'service', from: 'bms', to: 'soc', fromEntity: 'bank_a_voltage', title: 'Voltage bank A', titleKey: 'dataflow.battery_soc_devices.bank_a_voltage',
    details: { topic: 'bms/bank_a/state', json_key: 'voltage', unit: 'A', field: 'bank_a_voltage_topic' }, link: { tab: 'config', target: 'battery_soc_devices', item: 'bank' } }, labelOf);
  assert.equal(view.eyebrowKey, 'devicemap.pop.eyebrow.service_input');
  assert.deepEqual(plain(view.rows), [
    ['devicemap.pop.row.source', 'BMS Bank A, bank_a_voltage'],
    ['devicemap.pop.row.topic', 'bms/bank_a/state'],
    ['devicemap.pop.row.json_key', 'voltage'],
    ['devicemap.pop.row.unit', 'A'],
    ['devicemap.pop.row.target', 'Batterie SoC, bank_a_voltage_topic'],
  ]);
  assert.equal(view.linkKey, 'devicemap.pop.link.config');
});

test('deviceFlows lists inputs and outputs of one device without role edges', () => {
  const df = load();
  const labelOf = id => ({ bms: 'BMS Bank A', soc: 'Batterie SoC', 'rule:r': 'Akku leer' }[id] || id);
  const rows = df.deviceFlows('soc', [
    { id: 'd1', cat: 'service', from: 'bms', to: 'soc', title: 'Spannung Bank A' },
    { id: 'd2', cat: 'automation', from: 'soc', to: 'rule:r', title: 'Bedingung Entitätswert' },
    { id: 'r', cat: 'role', from: 'soc', to: 'balance', title: '' },
  ], labelOf);
  assert.deepEqual(plain(rows), [
    { title: 'Spannung Bank A', text: 'von BMS Bank A' },
    { title: 'Bedingung Entitätswert', text: 'nach Akku leer' },
  ]);
});

test('balanceNodeId prefers the energy_node device and falls back to the virtual node', () => {
  const df = load();
  assert.equal(df.balanceNodeId(new Set(['pv', 'energy_node'])), 'energy_node');
  assert.equal(df.balanceNodeId(new Set(['pv'])), 'balance');
});

test('flowEdges and roleEdges point balance refs at the resolved balance node', () => {
  const df = load();
  const graph = { edges: [
    { id: 'auto:rule:r/c0', cat: 'automation', from: { virtual_id: 'balance' }, to: { virtual_id: 'rule:r' }, details: {} },
    { id: 'svc', cat: 'service', from: { device_id: 'bms' }, to: { virtual_id: 'service:soc' }, details: {} },
  ] };
  const ids = new Set(['energy_node', 'rule:r', 'bms', 'service:soc']);
  const edges = plain(df.flowEdges(graph, ids, 'energy_node'));
  assert.equal(edges[0].from, 'energy_node');
  assert.equal(edges[0].viaBalance, true);
  assert.equal(edges[1].viaBalance, false);
  const snapshot = { entities: [
    { entity_id: 'pv_p', device_id: 'pv', role: { role: 'pv', source: 'override' } },
    { entity_id: 'own', device_id: 'energy_node', role: { role: 'pv', source: 'heuristic' } },
  ] };
  const roles = plain(df.roleEdges(snapshot, 'energy_node'));
  assert.equal(roles.length, 1);
  assert.equal(roles[0].to, 'energy_node');
});

test('without energy_node the balance refs keep the virtual node', () => {
  const df = load();
  const graph = { edges: [{ id: 'e', cat: 'automation', from: { virtual_id: 'balance' }, to: { virtual_id: 'rule:r' }, details: {} }] };
  const edges = plain(df.flowEdges(graph, new Set(['balance', 'rule:r'])));
  assert.equal(edges[0].from, 'balance');
  assert.equal(edges[0].viaBalance, true);
});

test('an edge from the balance device follows the data layer', () => {
  const df = load();
  const edge = { cat: 'automation', from: 'energy_node', to: 'rule:r', viaBalance: true };
  assert.equal(df.edgeVisible(edge, { wiring: true, energy: true, balance: false, data: true }), true);
  assert.equal(df.edgeVisible(edge, { wiring: true, energy: true, balance: true, data: false }), false);
  assert.equal(df.edgeVisible(edge, ALL), true);
});

test('the virtual balance node shows with the roles layer or the data flow layer', () => {
  const df = load();
  const none = { wiring: true, energy: true, balance: false, data: false };
  assert.equal(df.nodeVisible('balance', none), false);
  assert.equal(df.nodeVisible('balance', { ...none, balance: true }), true);
  assert.equal(df.nodeVisible('balance', { ...none, data: true }), true);
});

test('an executes edge shows the automation popover with its kind', () => {
  const df = load();
  const view = df.edgeView({ cat: 'automation', from: 'automation', to: 'rule:r', fromEntity: '', toEntity: '', titleKey: 'devicemap.flow.title.executes', details: { kind: 'executes', part: 'rule' }, link: { tab: 'automations', target: 'r' } }, id => id);
  assert.equal(view.eyebrowKey, 'devicemap.pop.eyebrow.automation');
  assert.equal(view.linkKey, 'devicemap.pop.link.automations');
});

test('isAutomationLink holds for rule executions and any edge touching the automation device', () => {
  const df = load();
  assert.equal(df.AUTOMATION_DEVICE_ID, 'automation');
  assert.equal(df.isAutomationLink({ from: 'netz', to: 'bat', details: { kind: 'executes' } }), true);
  assert.equal(df.isAutomationLink({ from: 'automation', to: 'rule:r' }), true);
  assert.equal(df.isAutomationLink({ from: 'netz', to: 'automation' }), true);
  assert.equal(df.isAutomationLink({ from: 'netz', to: 'bat', details: { kind: 'topic' } }), false);
  assert.equal(df.isAutomationLink({ from: 'netz', to: 'bat' }), false);
  assert.equal(df.isAutomationLink(undefined), false);
});

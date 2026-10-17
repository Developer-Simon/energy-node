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
const files = ['theme.js', 'devicemap-model.js', 'devicemap-graph.js', 'devicemap-wiring.js', 'devicemap-subdevices.js', 'devicemap-dataflow.js', 'devicemap-node-svg.js', 'devicemap-labels.js', 'energy-plant.js'];

function routes(table) {
  return async url => {
    const key = Object.keys(table).find(prefix => String(url).endsWith(prefix));
    if (!key) return { ok: false, status: 404, json: async () => ({}) };
    return { ok: true, status: 200, json: async () => JSON.parse(JSON.stringify(table[key])) };
  };
}

// isActive() reads the energy panel's "active" class and the page
// visibility, so the DOM carries the panel and jsdom pretends to be visible.
function createPlant(table) {
  const dom = new JSDOM('<!doctype html><html><body><section id="energy-panel" class="active"></section></body></html>', { runScripts: 'outside-only', pretendToBeVisual: true });
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  dom.window.fetch = routes(table);
  installI18n(dom.window);
  const context = dom.getInternalVMContext();
  for (const name of files) vm.runInContext(fs.readFileSync(path.join(jsDir, name), 'utf8'), context);
  const component = factory();
  component.$refs = {};
  component.$root = { classList: { contains: () => true } };
  return { component, window: dom.window };
}

const TABLE = {
  '/api/v1/devices': [
    { id: 'pv', name: 'PV', entities: [{ unique_id: 'pv1', unit_of_measurement: 'W', value: '500' }] },
    { id: 'wb', name: 'Wallbox', entities: [{ unique_id: 'wb1', unit_of_measurement: 'W', value: '300' }] },
  ],
  '/api/v1/device/map': { version: 2, nodes: [{ device_id: 'pv', x: 100, y: 100 }, { device_id: 'wb', x: 300, y: 100 }], edges: [], view: { layers: { wiring: true, energy: true, balance: false, data: true } } },
  '/api/v1/energy': { entities: [
    { device_id: 'pv', entity_id: 'pv1', value: 500, unit: 'W', role: { role: 'pv', source: 'override' } },
    { device_id: 'wb', entity_id: 'wb1', value: 300, unit: 'W', role: { role: 'wallbox', source: 'override' } },
  ], balance: { grid_import: 0, grid_export: 200 } },
  '/api/v1/energy/roles': { assignments: {}, groups: { uv: { label: 'UV', members: { devices: ['wb'], groups: [] } } }, categories: {} },
  '/api/v1/device/icons': [],
};

test('the plant view ignores the saved layers and shows energy and balance', async () => {
  const { component } = createPlant(TABLE);
  await component.load();
  const ids = component.elements().map(element => element.data.id);
  assert.ok(ids.includes('balance'));
  assert.ok(ids.includes('group:uv'));
  assert.ok(ids.includes('data-role:pv'), 'role edges are drawn');
  assert.ok(!ids.some(id => id.startsWith('rule:')), 'no data flow nodes');
});

test('positions fall back to computed spots without saving', async () => {
  const { component } = createPlant(TABLE);
  await component.load();
  const positions = component.positions();
  assert.ok(positions.get('balance'));
  assert.ok(positions.get('group:uv'));
  assert.equal(component.deviceMap.nodes.length, 2, 'nothing written back');
});

test('tapping a device asks the energy page to flash its rows, a group its card', async () => {
  const { component, window } = createPlant(TABLE);
  await component.load();
  const seen = [];
  window.addEventListener('energy-focus-rows', event => seen.push(['rows', event.detail.ids]));
  window.addEventListener('energy-focus-group', event => seen.push(['group', event.detail.id]));
  component.onNodeTap('pv');
  component.onNodeTap('group:uv');
  component.onNodeTap('balance');
  assert.deepEqual(JSON.parse(JSON.stringify(seen)), [['rows', ['pv1']], ['group', 'uv']]);
  assert.equal(component.focusId, 'balance');
});

test('a row click highlights the node of its device', async () => {
  const { component, window } = createPlant(TABLE);
  await component.load();
  window.dispatchEvent(new window.CustomEvent('energy-plant-focus', { detail: { entityId: 'wb1' } }));
  assert.equal(component.focusId, 'wb');
});

test('registry updates refresh only the energy snapshot, at most once per second', async () => {
  const calls = [];
  const { component, window } = createPlant(TABLE);
  window.fetch = async url => { calls.push(String(url)); return routes(TABLE)(url); };
  await component.load();
  calls.length = 0;
  component._setTimeout = () => 1; // a truthy timer id that never fires
  for (let i = 0; i < 4; i += 1) component.onRegistryUpdated();
  assert.deepEqual(calls, ['/api/v1/energy']);
});

test('without devices the view reports empty', async () => {
  const { component } = createPlant({ ...TABLE, '/api/v1/devices': [] });
  await component.load();
  assert.equal(component.empty, true);
});

test('a device without a saved position goes below the arrangement, not onto the origin', async () => {
  const devices = [...TABLE['/api/v1/devices'], { id: 'ws', name: 'Werkstatt', entities: [] }];
  const { component } = createPlant({ ...TABLE, '/api/v1/devices': devices });
  await component.load();
  assert.deepEqual({ ...component.positions().get('ws') }, { x: 100, y: 210 });
  assert.equal(component.deviceMap.nodes.length, 2, 'nothing written back');
});

test('device-map-changed and energy-roles-changed reload the whole view', async () => {
  const calls = [];
  const { component, window } = createPlant(TABLE);
  window.fetch = async url => { calls.push(String(url)); return routes(TABLE)(url); };
  await component.load();
  for (const name of ['device-map-changed', 'energy-roles-changed']) {
    calls.length = 0;
    window.dispatchEvent(new window.CustomEvent(name));
    await new Promise(resolve => setImmediate(resolve));
    assert.ok(calls.includes('/api/v1/device/map'), `${name} refetches the map`);
  }
});

// Loads the plant view with its own device list, map and energy snapshot,
// falling back to the shared TABLE for everything else.
async function plantWith({ devices, deviceMap, energy } = {}) {
  const table = { ...TABLE };
  if (devices) table['/api/v1/devices'] = devices;
  if (deviceMap) table['/api/v1/device/map'] = deviceMap;
  if (energy) table['/api/v1/energy'] = energy;
  const { component } = createPlant(table);
  await component.load();
  return component;
}

test('the plant view draws energy_node as the balance node', async () => {
  const plant = await plantWith({ devices: [
    { id: 'pv', name: 'PV', relations: [{ id: 'energy_node', kind: 'parent' }], entities: [] },
    { id: 'energy_node', name: 'Energy Node', relations: [{ id: 'pv', kind: 'child' }], entities: [] },
  ] });
  const ids = plant.elements().map(element => element.data.id);
  assert.ok(ids.includes('energy_node'));
  assert.ok(!ids.includes('balance'));
});

test('a data edge draws no flow and adds no group value', async () => {
  const plant = await plantWith({ deviceMap: {
    version: 2,
    nodes: [{ device_id: 'pv', x: 100, y: 100 }, { device_id: 'wb', x: 300, y: 100 }],
    edges: [{ id: 'relation-1', parent_id: 'group:uv', child_id: 'pv', kind: 'via_device', created_at: '' }],
    edge_props: [{ parent_id: 'group:uv', child_id: 'pv', type: 'data' }],
    view: { layers: { wiring: true, energy: true, balance: false, data: true } },
  } });
  const ids = plant.elements().map(element => element.data.id);
  assert.ok(ids.includes('flow-group:uv::wb'), 'the membership energy edge still flows');
  assert.ok(!ids.includes('flow-group:uv::pv'), 'the data edge draws no flow');
  const group = plant.labelItems().find(item => item.id === 'group:uv');
  assert.equal(group.value, 'Gruppe · Σ 300 W', 'only the membership edge (300 W) sums, the data edge (pv, 500 W) does not');
});

// A plug with two power entities, the first measured as sub:wm and a second
// sub-device (sub:gone) whose entity no longer exists.
const SUB_TABLE = {
  '/api/v1/devices': [
    { id: 'plug', name: 'Steckdose', relations: [], entities: [
      { unique_id: 'power_1', unit_of_measurement: 'W', value: '300' },
      { unique_id: 'power_2', unit_of_measurement: 'W', value: '200' },
    ] },
  ],
  '/api/v1/device/map': {
    version: 3,
    nodes: [{ device_id: 'plug', x: 100, y: 100 }, { virtual_id: 'sub:wm', x: 400, y: 50 }],
    edges: [],
    sub_devices: [
      { id: 'sub:wm', label: 'Waschmaschine', icon: 'plug', device_id: 'plug', entity_id: 'power_1' },
      { id: 'sub:gone', label: 'Trockner', icon: 'plug', device_id: 'plug', entity_id: 'power_9' },
    ],
    edge_props: [{ parent_id: 'plug', child_id: 'sub:wm', type: 'energy', channel: 1 }],
  },
  '/api/v1/energy': { entities: [
    { device_id: 'plug', entity_id: 'power_1', value: 300, unit: 'W', role: { role: 'load', source: 'override' } },
    { device_id: 'plug', entity_id: 'power_2', value: 200, unit: 'W', role: { role: 'load', source: 'override' } },
  ], balance: {} },
  '/api/v1/energy/roles': { assignments: {}, groups: {}, categories: {} },
  '/api/v1/device/icons': [],
};

test('plant view draws the sub-device at its saved position', async () => {
  const { component } = createPlant(SUB_TABLE);
  await component.load();
  const elements = component.elements();
  const byId = id => elements.find(element => element.data.id === id);

  const wm = byId('sub:wm');
  assert.deepEqual(JSON.parse(JSON.stringify(wm.position)), { x: 400, y: 50 }, 'drawn at the saved spot');
  assert.equal(wm.data.name, 'Waschmaschine');
  assert.equal(wm.classes, 'devicemap-sub');
  assert.match(decodeURIComponent(wm.data.svg), /data-border="sub"/, 'dashed ring');
  assert.doesNotMatch(decodeURIComponent(wm.data.svg), /data-health=/, 'no health dot');

  const gone = byId('sub:gone');
  assert.ok(gone.classes.includes('devicemap-sub-orphan'), 'entity is gone, so the node is an orphan');
  assert.ok(gone.position, 'an orphan without a saved spot still gets one');

  // The meter keeps the ring of its remaining entity, the sub-device edge
  // carries the sub-device's own measurement.
  assert.deepEqual(JSON.parse(JSON.stringify(component.energyByDevice().get('plug').roles)), ['load']);
  assert.equal(component.energyByDevice().get('plug').byRole.load, 200);
  assert.ok(component.wiringEdges().some(edge => edge.data.source === 'plug' && edge.data.target === 'sub:wm' && edge.data.subDevice === true));

  const labels = Object.fromEntries(component.labelItems().map(item => [item.id, item.value]));
  assert.equal(labels['sub:gone'], 'Messung fehlt');
});

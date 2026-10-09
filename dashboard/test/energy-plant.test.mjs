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
const files = ['theme.js', 'devicemap-model.js', 'devicemap-graph.js', 'devicemap-dataflow.js', 'devicemap-node-svg.js', 'devicemap-labels.js', 'energy-plant.js'];

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

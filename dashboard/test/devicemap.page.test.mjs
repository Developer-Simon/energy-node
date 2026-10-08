// Regression tests for devicemap.page.js's safety property: a plain node
// drag must only ever update x/y, and a structural relation must only ever
// be created after the explicit two-click "connect" gesture plus a
// confirmation dialog. Cytoscape itself is never instantiated here (jsdom
// has no working canvas 2D context) - tests call the Alpine component's
// event handlers directly with fake Cytoscape-shaped node/event objects,
// same approach as config.page.test.mjs uses for its DOM nodes.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { attachStores, fakeModalStore } from './helpers/notify-stores.mjs';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const themeSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'theme.js'),
  'utf8',
);
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'devicemap.page.js'),
  'utf8',
);
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');
const moduleSources = ['devicemap-model.js', 'devicemap-node-svg.js', 'devicemap-labels.js']
  .map(name => fs.readFileSync(path.join(jsDir, name), 'utf8'));

function createDevicemapPanel({ fetchImpl, confirmAnswer = true, cytoscapeImpl } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  if (cytoscapeImpl) dom.window.cytoscape = cytoscapeImpl;
  installI18n(dom.window);
  vm.runInContext(themeSource, context);
  for (const source of moduleSources) vm.runInContext(source, context);
  vm.runInContext(scriptSource, context);
  const component = factory();
  component.$refs = {}; // no canvas -> renderGraph() no-ops, matching real "panel not visible yet" state
  const stores = attachStores(component, { modal: fakeModalStore({ answer: confirmAnswer }) });
  return { component, window: dom.window, stores };
}

// A minimal stand-in for a Cytoscape instance, just enough surface for
// renderGraph()/toggleConnectMode() to drive it and for tests to observe
// autoungrabify() calls (and, optionally, the options renderGraph() called
// cytoscape() with - e.g. to inspect which layout was chosen). jsdom has no
// working canvas 2D context, so the real cytoscape() factory can't run here.
function fakeCytoscapeFactory(autoungrabifyCalls, optionsCalls, viewportCalls, panByCalls) {
  return options => {
    if (optionsCalls) optionsCalls.push(options);
    return {
      autoungrabify: value => autoungrabifyCalls.push(value),
      on: () => {},
      nodes: () => ({ removeClass: () => {} }),
      edges: () => ({ removeClass: () => {} }),
      destroy: () => {},
      style: () => {},
      zoom: () => 1,
      pan: () => ({ x: 0, y: 0 }),
      viewport: value => { if (viewportCalls) viewportCalls.push(value); },
      panBy: value => { if (panByCalls) panByCalls.push(value); },
    };
  };
}

// Stand-in for the canvas container element (x-ref="canvas"): a truthy value
// so renderGraph() doesn't early-return, plus the classList/style surface
// syncGridBackground() touches.
function fakeCanvas() {
  return { classList: { toggle: () => {} }, style: { setProperty: () => {} } };
}

function fakeNode(id, position = { x: 0, y: 0 }) {
  return {
    id: () => id,
    position: () => position,
    addClass: () => {},
  };
}

function fakeEdge(data) {
  return {
    data: () => data,
    addClass: () => {},
  };
}

test('dragging a node updates its position and marks the map unsaved, without calling the API', () => {
  const { component } = createDevicemapPanel();
  component.devices = [{ id: 'device_a', name: 'A', relations: [] }];
  component.deviceMap = { version: 1, nodes: [], edges: [] };

  component.onNodeDragFree({ target: fakeNode('device_a', { x: 12, y: 34 }) });

  assert.equal(component.unsaved, true);
  // Round-trip through JSON: objects built inside the jsdom VM context carry
  // that realm's Object.prototype and fail assert/strict's cross-realm
  // identity check despite being structurally identical (see
  // config.page.test.mjs for the same issue).
  assert.deepEqual(JSON.parse(JSON.stringify(component.deviceMap.nodes)), [{ device_id: 'device_a', x: 12, y: 34 }]);
});

test('dragging the same node twice replaces its previous position instead of duplicating it', () => {
  const { component } = createDevicemapPanel();
  component.deviceMap = { version: 1, nodes: [{ device_id: 'device_a', x: 1, y: 1 }], edges: [] };

  component.onNodeDragFree({ target: fakeNode('device_a', { x: 5, y: 6 }) });

  assert.deepEqual(JSON.parse(JSON.stringify(component.deviceMap.nodes)), [{ device_id: 'device_a', x: 5, y: 6 }]);
});

test('connect mode requires two taps and a confirmation before calling the relations API', async () => {
  let calls = 0;
  const { component } = createDevicemapPanel({
    confirmAnswer: false,
    fetchImpl: async () => { calls += 1; throw new Error('should not be called when the user declines'); },
  });
  component.devices = [
    { id: 'device_a', name: 'A', relations: [] },
    { id: 'device_b', name: 'B', relations: [] },
  ];
  component.connectMode = true;

  await component.onNodeTap({ target: fakeNode('device_a') });
  assert.equal(component.connectSourceId, 'device_a', 'first tap only selects the source node');
  assert.equal(calls, 0);

  await component.onNodeTap({ target: fakeNode('device_b') });
  assert.equal(calls, 0, 'declining the confirmation dialog must not call the API');
  assert.equal(component.connectSourceId, null, 'selection resets after the second tap regardless of outcome');
});

test('entering connect mode disables node grabbing, so a mouse click registers as a tap instead of a drag', () => {
  const autoungrabifyCalls = [];
  const { component } = createDevicemapPanel({ cytoscapeImpl: fakeCytoscapeFactory(autoungrabifyCalls) });
  component.devices = [];
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  component.$refs.canvas = fakeCanvas();

  component.renderGraph();
  assert.deepEqual(autoungrabifyCalls, [false], 'normal mode must leave nodes grabbable for free dragging');

  component.toggleConnectMode();
  assert.deepEqual(autoungrabifyCalls, [false, true], 'connect mode must disable grabbing so clicks cannot be misread as drags');

  component.toggleConnectMode();
  assert.deepEqual(autoungrabifyCalls, [false, true, false], 'leaving connect mode must restore normal dragging');
});

test('re-rendering the graph while connect mode is still active keeps grabbing disabled on the new instance', async () => {
  const autoungrabifyCalls = [];
  const { component } = createDevicemapPanel({
    cytoscapeImpl: fakeCytoscapeFactory(autoungrabifyCalls),
    confirmAnswer: true,
    fetchImpl: async () => ({
      ok: true, status: 201,
      json: async () => ({ id: 'relation-1', child_id: 'device_a', parent_id: 'device_b', kind: 'via_device', created_at: '2026-01-01T00:00:00Z' }),
    }),
  });
  component.devices = [
    { id: 'device_a', name: 'A', relations: [] },
    { id: 'device_b', name: 'B', relations: [] },
  ];
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  component.$refs.canvas = fakeCanvas();
  component.renderGraph();
  component.connectMode = true;
  autoungrabifyCalls.length = 0; // only interested in what renderGraph() does from here on

  await component.onNodeTap({ target: fakeNode('device_a') });
  await component.onNodeTap({ target: fakeNode('device_b') }); // triggers a successful connect -> renderGraph() again

  assert.deepEqual(autoungrabifyCalls, [true], 'the freshly (re)created instance must still reflect connect mode being active');
});

test('renderGraph re-applies the outgoing pan/zoom onto the freshly recreated instance instead of resetting the view', () => {
  const viewportCalls = [];
  const { component } = createDevicemapPanel({ cytoscapeImpl: fakeCytoscapeFactory([], null, viewportCalls) });
  component.devices = [];
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  component.$refs.canvas = fakeCanvas();

  component.renderGraph();
  assert.equal(viewportCalls.length, 0, 'the very first render has no prior viewport to restore');

  component.renderGraph();
  assert.deepEqual(JSON.parse(JSON.stringify(viewportCalls)), [{ zoom: 1, pan: { x: 0, y: 0 } }], 'a rebuild must carry the previous zoom/pan over onto the new instance');
});

test('the very first render pans down to clear the floating toolbar, but a rebuild never re-applies that offset', () => {
  const panByCalls = [];
  const { component } = createDevicemapPanel({ cytoscapeImpl: fakeCytoscapeFactory([], null, null, panByCalls) });
  component.devices = [];
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  component.$refs.canvas = fakeCanvas();

  component.renderGraph();
  assert.deepEqual(JSON.parse(JSON.stringify(panByCalls)), [{ x: 0, y: 72 }], 'first load must clear space under .devicemap-overlay for top-row nodes');

  component.renderGraph();
  assert.equal(panByCalls.length, 1, 'a rebuild restores the prior viewport instead - panning again would double the offset');
});

test('confirming the connect gesture posts the relation and stores the created override', async () => {
  let requestBody = null;
  const { component } = createDevicemapPanel({
    confirmAnswer: true,
    fetchImpl: async (url, options) => {
      requestBody = JSON.parse(options.body);
      return {
        ok: true,
        status: 201,
        json: async () => ({ id: 'relation-1', child_id: 'device_a', parent_id: 'device_b', kind: 'via_device', created_at: '2026-01-01T00:00:00Z' }),
      };
    },
  });
  component.devices = [
    { id: 'device_a', name: 'A', relations: [] },
    { id: 'device_b', name: 'B', relations: [] },
  ];
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  component.connectMode = true;

  await component.onNodeTap({ target: fakeNode('device_a') });
  await component.onNodeTap({ target: fakeNode('device_b') });

  assert.deepEqual(requestBody, { child_id: 'device_a', parent_id: 'device_b', kind: 'via_device' });
  assert.deepEqual(JSON.parse(JSON.stringify(component.deviceMap.edges)), [
    { id: 'relation-1', child_id: 'device_a', parent_id: 'device_b', kind: 'via_device', created_at: '2026-01-01T00:00:00Z' },
  ]);
});

test('tapping nodes outside connect mode never calls the API', async () => {
  let calls = 0;
  const { component } = createDevicemapPanel({ fetchImpl: async () => { calls += 1; return { ok: true, status: 200, json: async () => ({}) }; } });
  component.devices = [{ id: 'device_a', name: 'A', relations: [] }];
  component.connectMode = false;

  await component.onNodeTap({ target: fakeNode('device_a') });

  assert.equal(calls, 0);
  assert.equal(component.connectSourceId, null);
});

test('buildElements marks manually created relations with overrideId but leaves discovery-derived ones without it', () => {
  const { component } = createDevicemapPanel();
  component.devices = [
    { id: 'child', name: 'Child', relations: [{ id: 'parent', kind: 'parent' }], entities: [] },
    // 'parent' and 'other' both list the override-derived relation in their
    // own `relations` array too, because the registry merges RelationOverride
    // entries into every device's DeviceRelation list (registry.go
    // relationsLocked/sortedRelationsLocked) with no field marking them as
    // overrides - GET /api/v1/devices genuinely looks like this. A fixture
    // that leaves 'other' relation-less (as this test used to) can't catch a
    // dedup-order bug that only bites when the same pair shows up from both
    // device.relations and deviceMap.edges.
    { id: 'parent', name: 'Parent', relations: [{ id: 'child', kind: 'child' }, { id: 'other', kind: 'child' }], entities: [] },
    { id: 'other', name: 'Other', relations: [{ id: 'parent', kind: 'parent' }], entities: [] },
  ];
  component.deviceMap = { version: 1, nodes: [], edges: [{ id: 'relation-1', child_id: 'other', parent_id: 'parent', kind: 'via_device' }] };

  const edges = component.buildElements().filter(element => element.data.source && element.data.target);
  const discoveryEdge = edges.find(edge => edge.data.target === 'child');
  const overrideEdge = edges.find(edge => edge.data.target === 'other');

  assert.equal(discoveryEdge.data.overrideId, undefined, 'via_device-derived relations must not be deletable');
  assert.equal(overrideEdge.data.overrideId, 'relation-1', 'a relation that also appears in device.relations (as the registry always includes overrides there) must still keep its overrideId - regression test for "every connection renders as via_device and cannot be dissolved"');
});

test('buildElements gives each node its name and a role-ring image instead of a canvas label', () => {
  const { component } = createDevicemapPanel();
  component.devices = [{ id: 'pv', name: 'APsystems Dach', relations: [], entities: [{}, {}] }];
  component.energy = { entities: [{ device_id: 'pv', entity_id: 'p1', value: 2400, unit: 'W', role: { role: 'pv', source: 'override' } }] };
  const [node] = component.buildElements();
  assert.equal(node.data.name, 'APsystems Dach');
  assert.equal(node.data.label, undefined);
  assert.match(node.data.svg, /^data:image\/svg\+xml;utf8,/);
  assert.match(decodeURIComponent(node.data.svg), /data-ring="0"/);
});

test('labelItems pairs each device with its live value text', () => {
  const { component } = createDevicemapPanel();
  component.devices = [
    { id: 'pv', name: 'APsystems Dach', relations: [], entities: [] },
    { id: 'bms', name: 'BMS Bank A', relations: [], entities: [{ value: '52.8', unit_of_measurement: 'V' }] },
  ];
  component.energy = { entities: [{ device_id: 'pv', entity_id: 'p1', value: 2400, unit: 'W', role: { role: 'pv', source: 'override' } }] };
  assert.deepEqual(JSON.parse(JSON.stringify(component.labelItems())), [
    { id: 'pv', name: 'APsystems Dach', value: '2,4 kW' },
    { id: 'bms', name: 'BMS Bank A', value: '52,8 V' },
  ]);
});

test('load() keeps the map usable when the energy endpoint fails', async () => {
  const fetchImpl = async url => {
    if (url.endsWith('/api/v1/energy')) return { ok: false, status: 500, json: async () => ({ message: 'boom' }) };
    if (url.endsWith('/api/v1/devices')) return { ok: true, status: 200, json: async () => [{ id: 'a', name: 'A', relations: [], entities: [] }] };
    return { ok: true, status: 200, json: async () => ({ version: 1, nodes: [], edges: [] }) };
  };
  const { component, stores } = createDevicemapPanel({ fetchImpl });
  await component.load();
  assert.equal(component.devices.length, 1);
  assert.equal(component.energy, null);
  assert.equal(component.energyUnavailable, true);
  assert.equal(stores.toasts.items.length, 0, 'a missing energy snapshot is not a critical error');
});

test('buildElements classifies node status from availability-tracked entities only', () => {
  const { component } = createDevicemapPanel();
  component.deviceMap = { version: 1, nodes: [], edges: [] };

  component.devices = [{ id: 'a', name: 'A', relations: [], entities: [] }];
  assert.equal(component.buildElements()[0].classes, 'devicemap-status-unknown', 'no availability-tracking entities at all');

  component.devices = [{ id: 'a', name: 'A', relations: [], entities: [{ has_availability: true, available: true }, { has_availability: false }] }];
  assert.equal(component.buildElements()[0].classes, 'devicemap-status-ok', 'untracked entities must not drag the status down');

  component.devices = [{ id: 'a', name: 'A', relations: [], entities: [{ has_availability: true, available: true }, { has_availability: true, available: false }] }];
  assert.equal(component.buildElements()[0].classes, 'devicemap-status-degraded');

  component.devices = [{ id: 'a', name: 'A', relations: [], entities: [{ has_availability: true, available: false }] }];
  assert.equal(component.buildElements()[0].classes, 'devicemap-status-down');
});

test('tapping an edge selects it and exposes a human-readable label, without touching the API', () => {
  const { component } = createDevicemapPanel();
  component.devices = [
    { id: 'device_a', name: 'Kind', relations: [] },
    { id: 'device_b', name: 'Eltern', relations: [] },
  ];

  component.onEdgeTap({ target: fakeEdge({ id: 'edge-1', source: 'device_b', target: 'device_a', overrideId: 'relation-1' }) });

  assert.deepEqual(JSON.parse(JSON.stringify(component.selectedEdge)), { id: 'edge-1', source: 'device_b', target: 'device_a', overrideId: 'relation-1', membership: null });
  assert.equal(component.selectedEdgeLabel, 'Kind → Eltern');
});

test('removeSelectedRelation refuses to delete a discovery-derived (via_device) relation', async () => {
  let calls = 0;
  const { component } = createDevicemapPanel({ fetchImpl: async () => { calls += 1; return { ok: true, status: 204 }; } });
  component.onEdgeTap({ target: fakeEdge({ id: 'edge-1', source: 'parent', target: 'child' }) }); // no overrideId

  await component.removeSelectedRelation();

  assert.equal(calls, 0, 'a relation with no overrideId must never reach the API');
  assert.ok(component.selectedEdge, 'the selection is left in place so the read-only hint keeps showing');
});

test('removeSelectedRelation deletes an overridden relation only after confirmation, and drops it from the map', async () => {
  let deletedUrl = null;
  const { component } = createDevicemapPanel({
    confirmAnswer: true,
    fetchImpl: async url => { deletedUrl = url; return { ok: true, status: 204 }; },
  });
  component.devices = [
    { id: 'child', name: 'Child', relations: [] },
    { id: 'parent', name: 'Parent', relations: [] },
  ];
  component.deviceMap = { version: 1, nodes: [], edges: [{ id: 'relation-1', child_id: 'child', parent_id: 'parent', kind: 'via_device' }] };
  component.onEdgeTap({ target: fakeEdge({ id: 'edge-1', source: 'parent', target: 'child', overrideId: 'relation-1' }) });

  await component.removeSelectedRelation();

  assert.equal(deletedUrl, '/api/v1/device/map/relations/relation-1');
  assert.deepEqual(component.deviceMap.edges, []);
});

test('declining the confirmation dialog leaves the relation in place', async () => {
  let calls = 0;
  const { component } = createDevicemapPanel({
    confirmAnswer: false,
    fetchImpl: async () => { calls += 1; return { ok: true, status: 204 }; },
  });
  component.deviceMap = { version: 1, nodes: [], edges: [{ id: 'relation-1', child_id: 'child', parent_id: 'parent', kind: 'via_device' }] };
  component.onEdgeTap({ target: fakeEdge({ id: 'edge-1', source: 'parent', target: 'child', overrideId: 'relation-1' }) });

  await component.removeSelectedRelation();

  assert.equal(calls, 0);
  assert.equal(component.deviceMap.edges.length, 1);
});

test('clearEdgeSelection drops the current selection', () => {
  const { component } = createDevicemapPanel();
  component.onEdgeTap({ target: fakeEdge({ id: 'edge-1', source: 'parent', target: 'child' }) });
  assert.ok(component.selectedEdge);

  component.clearEdgeSelection();
  assert.equal(component.selectedEdge, null);
});

test('view defaults apply when the loaded device map has no view section (older revisions)', () => {
  const { component } = createDevicemapPanel();
  component.deviceMap = { version: 1, nodes: [], edges: [] };

  assert.deepEqual(JSON.parse(JSON.stringify(component.view)), { snap_to_grid: false, show_grid: false, grid_size: 40, edge_style: 'straight', width_by_power: false, layers: { wiring: true, energy: true, balance: false, data: false } });
});

test('a device without a saved position is placed below the existing arrangement instead of triggering a full re-layout', () => {
  const optionsCalls = [];
  const { component } = createDevicemapPanel({ cytoscapeImpl: fakeCytoscapeFactory([], optionsCalls) });
  component.devices = [
    { id: 'device_a', name: 'A', relations: [] },
    { id: 'device_b', name: 'B', relations: [] },
  ];
  component.deviceMap = { version: 1, nodes: [{ device_id: 'device_a', x: 100, y: 200 }], edges: [] };
  component.$refs.canvas = fakeCanvas();

  component.renderGraph();

  assert.deepEqual(JSON.parse(JSON.stringify(optionsCalls[0].layout)), { name: 'preset' }, 'existing positions must not be discarded into a full re-layout');
  assert.equal(component.placedCount, 1);
  assert.equal(component.unsaved, true);
  const placedA = component.deviceMap.nodes.find(node => node.device_id === 'device_a');
  assert.deepEqual(JSON.parse(JSON.stringify(placedA)), { device_id: 'device_a', x: 100, y: 200 }, "the existing device's position must stay untouched");
  const placedB = component.deviceMap.nodes.find(node => node.device_id === 'device_b');
  assert.ok(placedB.y > placedA.y, 'the new device must be placed below the existing arrangement');
});

test('with no saved positions at all, the graph still falls back to an automatic layout', () => {
  const optionsCalls = [];
  const { component } = createDevicemapPanel({ cytoscapeImpl: fakeCytoscapeFactory([], optionsCalls) });
  component.devices = [
    { id: 'device_a', name: 'A', relations: [] },
    { id: 'device_b', name: 'B', relations: [] },
  ];
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  component.$refs.canvas = fakeCanvas();

  component.renderGraph();

  assert.deepEqual(JSON.parse(JSON.stringify(optionsCalls[0].layout)), { name: 'breadthfirst', directed: true, padding: 30 });
  assert.equal(component.placedCount, 0, 'placeNewDevices has no existing arrangement yet to anchor new devices to');
});

test('placeNewDevices ignores saved positions belonging to devices that no longer exist when anchoring new ones', () => {
  const { component } = createDevicemapPanel();
  component.devices = [
    { id: 'device_a', name: 'A', relations: [] },
    { id: 'device_b', name: 'B', relations: [] },
  ];
  component.deviceMap = {
    version: 1,
    nodes: [
      { device_id: 'device_a', x: 0, y: 0 },
      { device_id: 'deleted_device', x: 9000, y: 9000 }, // no longer among devices
    ],
    edges: [],
  };

  const placed = component.placeNewDevices();

  assert.equal(placed, 1);
  const placedB = component.deviceMap.nodes.find(node => node.device_id === 'device_b');
  assert.ok(placedB.x < 9000 && placedB.y < 9000, "the orphaned node's far-away position must not skew the bounding box");
});

test('dragging a node snaps to the grid when snap-to-grid is enabled', () => {
  const { component } = createDevicemapPanel();
  component.deviceMap = { version: 1, nodes: [], edges: [], view: { snap_to_grid: true, show_grid: false, grid_size: 40, edge_style: 'straight' } };
  let snappedTo = null;
  const node = {
    id: () => 'device_a',
    position: value => { if (value) { snappedTo = value; return; } return { x: 53, y: 21 }; },
    addClass: () => {},
  };

  component.onNodeDragFree({ target: node });

  assert.deepEqual(JSON.parse(JSON.stringify(snappedTo)), { x: 40, y: 40 }, 'must write the snapped position back onto the cytoscape node');
  assert.deepEqual(JSON.parse(JSON.stringify(component.deviceMap.nodes)), [{ device_id: 'device_a', x: 40, y: 40 }]);
});

// Regression coverage for "dragging stops working with snap-to-grid on":
// forcing the dragged node itself onto the grid every 'drag' frame fought
// Cytoscape's own drag tracking (each frame's position is computed from the
// pointer delta since grab - overwriting it desynced that delta). onNodeDrag
// must now leave the real node's position() setter untouched and only
// create/move a separate non-interactive ghost node as the snap preview.
function fakeCytoscapeForDragTests() {
  let ghostExists = false;
  let ghostPosition = null;
  const addCalls = [];
  const positionCalls = [];
  let removeCalls = 0;
  const ghostCollection = () => ({
    empty: () => !ghostExists,
    position: value => { if (value) { ghostPosition = value; positionCalls.push(value); } return ghostPosition; },
    remove: () => { ghostExists = false; removeCalls += 1; },
  });
  const instance = {
    autoungrabify: () => {},
    on: () => {},
    nodes: () => ({ removeClass: () => {} }),
    edges: () => ({ removeClass: () => {} }),
    destroy: () => {},
    style: () => {},
    zoom: () => 1,
    pan: () => ({ x: 0, y: 0 }),
    viewport: () => {},
    panBy: () => {},
    getElementById: id => (id === SNAP_GHOST_ID_FOR_TESTS ? ghostCollection() : { empty: () => true }),
    add: opts => { ghostExists = true; ghostPosition = opts.position; addCalls.push(opts); return ghostCollection(); },
  };
  return { cytoscapeImpl: () => instance, addCalls, positionCalls, getRemoveCalls: () => removeCalls };
}
// Mirrors the private SNAP_GHOST_ID constant in devicemap.page.js - kept in
// sync manually since the module doesn't export it.
const SNAP_GHOST_ID_FOR_TESTS = '__devicemap-snap-ghost__';

test('onNodeDrag creates a snapped ghost node while dragging, without moving the real node', () => {
  const { cytoscapeImpl, addCalls, positionCalls } = fakeCytoscapeForDragTests();
  const { component } = createDevicemapPanel({ cytoscapeImpl });
  component.devices = [];
  component.deviceMap = { version: 1, nodes: [], edges: [], view: { snap_to_grid: true, show_grid: false, grid_size: 40, edge_style: 'straight' } };
  component.$refs.canvas = fakeCanvas();
  component.renderGraph();

  let realNodePosition = { x: 53, y: 21 };
  const node = {
    id: () => 'device_a',
    position: value => { if (value) { realNodePosition = value; return; } return realNodePosition; },
  };

  component.onNodeDrag({ target: node });

  assert.equal(addCalls.length, 1, 'first drag frame must create the ghost node');
  assert.deepEqual(JSON.parse(JSON.stringify(addCalls[0].position)), { x: 40, y: 40 });
  assert.equal(addCalls[0].classes, 'devicemap-ghost');
  assert.deepEqual(realNodePosition, { x: 53, y: 21 }, 'the real node must keep tracking the pointer 1:1, unmodified by snapping - this is the fix for dragging breaking with snap-to-grid on');

  realNodePosition = { x: 58, y: 22 }; // pointer moved to a new spot that still snaps to the same cell (58/40 rounds to 1)
  component.onNodeDrag({ target: node });

  assert.equal(addCalls.length, 1, 'a later frame must reuse the existing ghost instead of adding a second one');
  assert.deepEqual(JSON.parse(JSON.stringify(positionCalls.at(-1))), { x: 40, y: 40 });
});

test('onNodeDrag does nothing when snap-to-grid is disabled', () => {
  const { cytoscapeImpl, addCalls } = fakeCytoscapeForDragTests();
  const { component } = createDevicemapPanel({ cytoscapeImpl });
  component.devices = [];
  component.deviceMap = { version: 1, nodes: [], edges: [], view: { snap_to_grid: false, show_grid: false, grid_size: 40, edge_style: 'straight' } };
  component.$refs.canvas = fakeCanvas();
  component.renderGraph();

  const node = { id: () => 'device_a', position: () => ({ x: 53, y: 21 }) };
  component.onNodeDrag({ target: node });

  assert.equal(addCalls.length, 0, 'must not create a ghost when snapping is off');
});

test('onNodeDragFree removes the snap ghost once the drag ends', () => {
  const { cytoscapeImpl, addCalls, getRemoveCalls } = fakeCytoscapeForDragTests();
  const { component } = createDevicemapPanel({ cytoscapeImpl });
  component.devices = [];
  component.deviceMap = { version: 1, nodes: [], edges: [], view: { snap_to_grid: true, show_grid: false, grid_size: 40, edge_style: 'straight' } };
  component.$refs.canvas = fakeCanvas();
  component.renderGraph();

  const node = {
    id: () => 'device_a',
    position: value => { if (value) return; return { x: 53, y: 21 }; },
  };
  component.onNodeDrag({ target: node });
  assert.equal(addCalls.length, 1, 'sanity check: the ghost was created');

  component.onNodeDragFree({ target: node });

  assert.equal(getRemoveCalls(), 1, 'the ghost must be removed once the drag ends');
});

test('dragging a node keeps the raw position when snap-to-grid is disabled', () => {
  const { component } = createDevicemapPanel();
  component.deviceMap = { version: 1, nodes: [], edges: [], view: { snap_to_grid: false, show_grid: false, grid_size: 40, edge_style: 'straight' } };

  component.onNodeDragFree({ target: fakeNode('device_a', { x: 53, y: 21 }) });

  assert.deepEqual(JSON.parse(JSON.stringify(component.deviceMap.nodes)), [{ device_id: 'device_a', x: 53, y: 21 }]);
});

test('setEdgeStyle updates the style in place without recreating the cytoscape instance', () => {
  let createCalls = 0;
  const styleCalls = [];
  const cytoscapeImpl = () => {
    createCalls += 1;
    return {
      autoungrabify: () => {},
      on: () => {},
      nodes: () => ({ removeClass: () => {} }),
      edges: () => ({ removeClass: () => {} }),
      destroy: () => {},
      style: value => styleCalls.push(value),
      zoom: () => 1,
      pan: () => ({ x: 0, y: 0 }),
      panBy: () => {},
    };
  };
  const { component } = createDevicemapPanel({ cytoscapeImpl });
  component.devices = [];
  component.deviceMap = { version: 1, nodes: [], edges: [], view: { snap_to_grid: false, show_grid: false, grid_size: 40, edge_style: 'straight' } };
  component.$refs.canvas = fakeCanvas();
  component.renderGraph();
  assert.equal(createCalls, 1);

  component.setEdgeStyle('elbow');

  assert.equal(component.deviceMap.view.edge_style, 'elbow');
  assert.equal(createCalls, 1, 'must not destroy/recreate the cytoscape instance');
  assert.equal(styleCalls.length, 1, 'must push the updated style onto the existing instance');
});

test('save() includes the current view settings in the PUT body', async () => {
  let requestBody = null;
  const { component } = createDevicemapPanel({
    fetchImpl: async (url, options) => {
      requestBody = JSON.parse(options.body);
      return { ok: true, status: 200, json: async () => requestBody };
    },
  });
  component.deviceMap = { version: 1, nodes: [], edges: [], view: { snap_to_grid: true, show_grid: true, grid_size: 20, edge_style: 'curved' } };

  await component.save();

  assert.deepEqual(requestBody.view, { snap_to_grid: true, show_grid: true, grid_size: 20, edge_style: 'curved', width_by_power: false, layers: { wiring: true, energy: true, balance: false, data: false } });
});

test('discardChanges restores the last loaded/saved snapshot without a network call, and clears unsaved', async () => {
  const { component } = createDevicemapPanel({
    fetchImpl: async url => {
      if (url === '/api/v1/devices') return { ok: true, status: 200, json: async () => [] };
      if (url === '/api/v1/device/map') {
        return { ok: true, status: 200, json: async () => ({ version: 1, nodes: [{ device_id: 'device_a', x: 1, y: 1 }], edges: [] }) };
      }
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await component.load();
  component.onNodeDragFree({ target: fakeNode('device_a', { x: 99, y: 99 }) });
  assert.equal(component.unsaved, true);

  component.discardChanges();

  assert.equal(component.unsaved, false);
  assert.deepEqual(JSON.parse(JSON.stringify(component.deviceMap.nodes)), [{ device_id: 'device_a', x: 1, y: 1 }]);
});

test('discardChanges never undoes a relation edge, since those are already persisted immediately on creation', async () => {
  const { component } = createDevicemapPanel({
    confirmAnswer: true,
    fetchImpl: async () => ({
      ok: true, status: 201,
      json: async () => ({ id: 'relation-1', child_id: 'device_a', parent_id: 'device_b', kind: 'via_device', created_at: '2026-01-01T00:00:00Z' }),
    }),
  });
  component.devices = [
    { id: 'device_a', name: 'A', relations: [] },
    { id: 'device_b', name: 'B', relations: [] },
  ];
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  component.savedDeviceMap = { version: 1, nodes: [], edges: [] };
  component.connectMode = true;

  await component.onNodeTap({ target: fakeNode('device_a') });
  await component.onNodeTap({ target: fakeNode('device_b') });
  assert.equal(component.deviceMap.edges.length, 1);

  component.discardChanges();

  assert.equal(component.deviceMap.edges.length, 1, 'a relation created via the connect gesture must survive discardChanges()');
});

test('save() updates the discard snapshot, so a later discardChanges() keeps the newly saved positions', async () => {
  const { component } = createDevicemapPanel({
    fetchImpl: async (_url, options) => ({ ok: true, status: 200, json: async () => JSON.parse(options.body) }),
  });
  component.devices = [{ id: 'device_a', name: 'A', relations: [], entities: [] }];
  component.deviceMap = { version: 1, nodes: [{ device_id: 'device_a', x: 5, y: 5 }], edges: [] };
  component.savedDeviceMap = { version: 1, nodes: [], edges: [] }; // stale on purpose

  await component.save();
  component.discardChanges();

  assert.deepEqual(JSON.parse(JSON.stringify(component.deviceMap.nodes)), [{ device_id: 'device_a', x: 5, y: 5 }]);
});

test('confirmUnsavedUnload only blocks the tab close when there are unsaved position changes', () => {
  const { component } = createDevicemapPanel();
  let prevented = false;
  const event = { preventDefault: () => { prevented = true; }, returnValue: undefined };

  component.unsaved = false;
  component.confirmUnsavedUnload(event);
  assert.equal(prevented, false);

  component.unsaved = true;
  component.confirmUnsavedUnload(event);
  assert.equal(prevented, true);
  assert.equal(event.returnValue, '');
});

test('view always carries all four layers, defaulting old maps to wiring and energy', () => {
  const { component } = createDevicemapPanel();
  component.deviceMap = { version: 1, nodes: [], edges: [], view: { snap_to_grid: true } };
  assert.deepEqual(JSON.parse(JSON.stringify(component.view.layers)), { wiring: true, energy: true, balance: false, data: false });
  assert.equal(component.view.width_by_power, false);
});

test('toggleLayer flips one layer, marks the map unsaved and ignores pending layers', () => {
  const { component } = createDevicemapPanel();
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  component.toggleLayer('wiring');
  assert.equal(component.view.layers.wiring, false);
  assert.equal(component.view.layers.energy, true);
  assert.equal(component.unsaved, true);
  component.unsaved = false;
  component.toggleLayer('data');
  assert.equal(component.view.layers.data, false, 'data flow has no content before phase 5');
  assert.equal(component.unsaved, false);
  assert.equal(component.isLayerPending('balance'), true);
});

test('wiring edges become a dotted track when only the energy layer is on', () => {
  const { component } = createDevicemapPanel();
  component.deviceMap = { version: 1, nodes: [], edges: [], view: { layers: { wiring: false, energy: true, balance: false, data: false } } };
  const style = component.graphStyle();
  const wiring = style.find(rule => rule.selector === 'edge.devicemap-wiring');
  assert.equal(wiring.style['line-style'], 'dotted');
  component.deviceMap = { version: 1, nodes: [], edges: [], view: { layers: { wiring: false, energy: false, balance: false, data: false } } };
  assert.equal(component.graphStyle().find(rule => rule.selector === 'edge.devicemap-wiring').style.display, 'none');
});

test('save() sends the layers with the view', async () => {
  let body;
  const fetchImpl = async (url, options) => { body = JSON.parse(options.body); return { ok: true, status: 200, json: async () => ({}) }; };
  const { component } = createDevicemapPanel({ fetchImpl });
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  component.toggleLayer('wiring');
  await component.save();
  assert.deepEqual(body.view.layers, { wiring: false, energy: true, balance: false, data: false });
  assert.equal(body.view.width_by_power, false);
});

test('tapping a node outside connect mode focuses it and its direct neighbours', async () => {
  const { component } = createDevicemapPanel();
  component.devices = [
    { id: 'netz', name: 'Netz', relations: [] },
    { id: 'uv', name: 'UV', relations: [{ kind: 'parent', id: 'netz' }] },
    { id: 'wb', name: 'Wallbox', relations: [{ kind: 'parent', id: 'uv' }] },
    { id: 'pv', name: 'PV', relations: [{ kind: 'parent', id: 'netz' }] },
  ];
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  await component.onNodeTap({ target: fakeNode('uv') });
  assert.equal(component.focusId, 'uv');
  assert.deepEqual([...component.focusRelated].sort(), ['netz', 'uv', 'wb']);
  assert.match(component.focusSummary(), /^UV · 2 verbunden/);
  component.clearFocus();
  assert.equal(component.focusId, null);
});

test('connect mode taps never set a focus', async () => {
  const { component } = createDevicemapPanel({ confirmAnswer: false });
  component.devices = [{ id: 'a', name: 'A', relations: [] }, { id: 'b', name: 'B', relations: [] }];
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  component.connectMode = true;
  await component.onNodeTap({ target: fakeNode('a') });
  assert.equal(component.focusId, null);
});

test('registry updates refresh the energy snapshot at most once per second, only while visible', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  let energyCalls = 0;
  const fetchImpl = async url => {
    if (url.endsWith('/api/v1/energy')) energyCalls += 1;
    return { ok: true, status: 200, json: async () => ({ entities: [] }) };
  };
  const { component, window } = createDevicemapPanel({ fetchImpl });
  // The script runs in jsdom's VM context, whose setTimeout is not the one
  // node:test mocks - hand the component Node's (mocked) timer instead.
  component._setTimeout = (fn, ms) => setTimeout(fn, ms);
  component.$root = { classList: { contains: name => name === 'active' } };
  Object.defineProperty(window.document, 'visibilityState', { value: 'visible', configurable: true });
  component.onRegistryUpdated();
  component.onRegistryUpdated();
  component.onRegistryUpdated();
  await Promise.resolve();
  assert.equal(energyCalls, 1, 'burst collapses into one request');
  t.mock.timers.tick(1000);
  await Promise.resolve();
  assert.equal(energyCalls, 2, 'one trailing request after the window');
  Object.defineProperty(window.document, 'visibilityState', { value: 'hidden', configurable: true });
  t.mock.timers.tick(1000);
  component.onRegistryUpdated();
  await Promise.resolve();
  assert.equal(energyCalls, 2, 'no requests from a background tab');
  component.$root = { classList: { contains: () => false } };
  Object.defineProperty(window.document, 'visibilityState', { value: 'visible', configurable: true });
  component.onRegistryUpdated();
  await Promise.resolve();
  assert.equal(energyCalls, 2, 'no requests while another dashboard tab is active');
});

test('toggleWidthByPower flips the view flag and marks the map unsaved', () => {
  const { component } = createDevicemapPanel();
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  component.toggleWidthByPower();
  assert.equal(component.view.width_by_power, true);
  assert.equal(component.unsaved, true);
});

function flowFixture(component, batteryValue = 800) {
  component.devices = [
    { id: 'netz', name: 'Netz', relations: [] },
    { id: 'bat', name: 'Batterie', relations: [{ kind: 'parent', id: 'netz' }] },
    { id: 'bms', name: 'BMS', relations: [{ kind: 'parent', id: 'bat' }] },
  ];
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  component.energy = { entities: [{ device_id: 'bat', entity_id: 'p', value: batteryValue, unit: 'W', role: { role: 'battery', source: 'override' } }] };
}

test('flowElements draws one flow per wiring edge with data, in flow direction', () => {
  const { component } = createDevicemapPanel();
  flowFixture(component);
  const flows = JSON.parse(JSON.stringify(component.flowElements()));
  assert.equal(flows.length, 1, 'the BMS edge has no data and gets no flow');
  assert.deepEqual(flows[0].data, { id: 'flow-netz::bat', source: 'netz', target: 'bat', label: '↓ 800 W', color: 'flow-battery', width: 2.6, speed: 'mid', reverse: false });
});

test('a discharging battery flips the flow toward the parent', () => {
  const { component } = createDevicemapPanel();
  flowFixture(component, -1500);
  const [flow] = JSON.parse(JSON.stringify(component.flowElements()));
  assert.equal(flow.data.source, 'bat');
  assert.equal(flow.data.target, 'netz');
  assert.equal(flow.data.reverse, true);
  assert.equal(flow.data.label, '↑ 1,5 kW');
  assert.equal(flow.data.speed, 'fast');
});

test('flowElements is empty while the energy layer is off or the snapshot is missing', () => {
  const { component } = createDevicemapPanel();
  flowFixture(component);
  component.toggleLayer('energy');
  assert.equal(component.flowElements().length, 0);
  component.toggleLayer('energy');
  component.energy = null;
  assert.equal(component.flowElements().length, 0);
});

test('applyFlows replaces a flow whose direction changed instead of duplicating it', () => {
  const { component } = createDevicemapPanel();
  flowFixture(component, 800);
  const store = new Map();
  const fakeCy = {
    batch: fn => fn(),
    edges: selector => ({ forEach: fn => [...store.values()].forEach(fn) }),
    getElementById: id => store.get(id) || { empty: () => true },
    add: element => {
      const data = { ...element.data };
      store.set(data.id, {
        empty: () => false,
        id: () => data.id,
        data: (key, value) => { if (value !== undefined) data[key] = value; return key ? data[key] : data; },
        remove: () => store.delete(data.id),
      });
    },
    style: () => {},
  };
  component._cyForTest = fakeCy;
  component.applyFlows();
  assert.equal(store.get('flow-netz::bat').data('target'), 'bat');
  component.energy.entities[0].value = -800;
  component.applyFlows();
  assert.equal(store.size, 1);
  assert.equal(store.get('flow-netz::bat').data('target'), 'netz');
});

test('flowOffset cycles one dash pattern per speed period, moving toward the target', () => {
  const { component } = createDevicemapPanel();
  assert.equal(component.flowOffset(0, 'mid'), -0);
  assert.equal(Number(component.flowOffset(700, 'mid').toFixed(4)), -8);
  assert.equal(Number(component.flowOffset(1400, 'mid').toFixed(4)), 0, 'a full period is back at the start (toFixed drops the sign of -0)');
});

test('the animation loop only runs while it makes sense', () => {
  const frames = [];
  const { component, window } = createDevicemapPanel();
  component._requestFrame = fn => { frames.push(fn); return frames.length; };
  component._cancelFrame = () => { frames.length = 0; };
  component.deviceMap = { version: 1, nodes: [], edges: [] };
  component.$root = { classList: { contains: name => name === 'active' } };
  Object.defineProperty(window.document, 'visibilityState', { value: 'visible', configurable: true });
  window.matchMedia = () => ({ matches: false });
  assert.equal(component.shouldAnimate(), true);
  component.startFlowAnimation();
  assert.equal(frames.length, 1);
  component.toggleLayer('energy');
  assert.equal(component.shouldAnimate(), false, 'energy layer off');
  component.toggleLayer('energy');
  window.matchMedia = () => ({ matches: true });
  assert.equal(component.shouldAnimate(), false, 'reduced motion');
  window.matchMedia = () => ({ matches: false });
  Object.defineProperty(window.document, 'visibilityState', { value: 'hidden', configurable: true });
  assert.equal(component.shouldAnimate(), false, 'hidden browser tab');
  component.stopFlowAnimation();
  assert.equal(component._raf, null);
});

const panelDevices = () => ([
  {id: 'bkw', name: 'Balkonkraftwerk', relations: [], entities: [{unique_id: 'bkw_p', name: 'Leistung', unit_of_measurement: 'W', value: '600'}]},
  {id: 'wb', name: 'Wallbox', relations: [], entities: [{unique_id: 'wb_p', name: 'Ladeleistung', unit_of_measurement: 'W', value: '1100'}]},
  {id: 'energy_node', name: 'Energy Node', relations: [], entities: []},
]);

test('tapping a device focuses it and opens its panel, tapping it again keeps both', async () => {
  const {component} = createDevicemapPanel();
  component.devices = panelDevices();
  component.deviceMap = {version: 1, nodes: [], edges: []};
  await component.onNodeTap({target: fakeNode('bkw')});
  assert.equal(component.panelId, 'bkw');
  assert.equal(component.focusId, 'bkw');
  await component.onNodeTap({target: fakeNode('bkw')});
  assert.equal(component.panelId, 'bkw');
  assert.equal(component.focusId, 'bkw');
});

test('the own energy device gets a focus but no panel', async () => {
  const {component} = createDevicemapPanel();
  component.devices = panelDevices();
  component.deviceMap = {version: 1, nodes: [], edges: []};
  await component.onNodeTap({target: fakeNode('energy_node')});
  assert.equal(component.panelId, null);
  assert.equal(component.focusId, 'energy_node');
});

for (const [name, leave] of [
  ['another node', c => c.onNodeTap({target: fakeNode('wb')})],
  ['the background', c => c.onBackgroundTap()],
  ['Escape', c => c.onEscape()],
  ['the close button', c => c.requestClosePanel()],
  ['connect mode', c => c.toggleConnectMode()],
]) {
  test(`leaving a dirty panel via ${name} asks the guard and keeps the panel on "continue"`, async () => {
    const {component, stores} = createDevicemapPanel({confirmAnswer: false});
    component.devices = panelDevices();
    component.deviceMap = {version: 1, nodes: [], edges: []};
    await component.onNodeTap({target: fakeNode('bkw')});
    component.setDraft('bkw_p', 'role', 'load');
    await leave(component);
    assert.equal(stores.modal.calls.length, 1);
    assert.equal(stores.modal.calls[0].danger, true);
    assert.equal(component.panelId, 'bkw');
    assert.equal(Object.keys(component.drafts).length, 1);
  });

  test(`leaving a dirty panel via ${name} discards the drafts on "discard"`, async () => {
    const {component} = createDevicemapPanel({confirmAnswer: true});
    component.devices = panelDevices();
    component.deviceMap = {version: 1, nodes: [], edges: []};
    await component.onNodeTap({target: fakeNode('bkw')});
    component.setDraft('bkw_p', 'role', 'load');
    await leave(component);
    assert.deepEqual(Object.keys(component.drafts), []);
    assert.notEqual(component.panelId, 'bkw');
  });
}

test('Escape does nothing while the confirm modal is open', async () => {
  const {component, stores} = createDevicemapPanel();
  component.devices = panelDevices();
  component.deviceMap = {version: 1, nodes: [], edges: []};
  await component.onNodeTap({target: fakeNode('bkw')});
  stores.modal.open = true;
  await component.onEscape();
  assert.equal(component.panelId, 'bkw');
});

test('a clean panel closes without asking and beforeunload covers panel drafts', async () => {
  const {component, stores} = createDevicemapPanel();
  component.devices = panelDevices();
  component.deviceMap = {version: 1, nodes: [], edges: []};
  await component.onNodeTap({target: fakeNode('bkw')});
  await component.onBackgroundTap();
  assert.equal(stores.modal.calls.length, 0);
  assert.equal(component.panelId, null);
  await component.onNodeTap({target: fakeNode('bkw')});
  component.setDraft('bkw_p', 'role', 'load');
  let prevented = false;
  component.confirmUnsavedUnload({preventDefault: () => { prevented = true; }});
  assert.equal(prevented, true);
});

test('drafts feed the preview energy the ring and flows are drawn from', async () => {
  const {component} = createDevicemapPanel();
  component.devices = panelDevices();
  component.deviceMap = {version: 1, nodes: [], edges: []};
  component.energy = {entities: [{entity_id: 'bkw_p', device_id: 'bkw', value: 600, unit: 'W', role: {role: 'pv', scale: 1, source: 'heuristic'}}]};
  await component.onNodeTap({target: fakeNode('bkw')});
  assert.equal(component.energyByDevice().get('bkw').primaryRole, 'pv');
  component.setDraft('bkw_p', 'role', 'heat_pump');
  assert.equal(component.energyByDevice().get('bkw').primaryRole, 'heat_pump');
  assert.equal(component.energyByDevice().get('bkw').heuristic, false);
});

test('savePanel patches only the drafts with the CSRF token, refreshes energy and tells the energy page', async () => {
  const calls = [];
  const fetchImpl = async (url, options = {}) => {
    calls.push({url, options});
    if (options.method === 'PATCH') return {ok: true, status: 200, json: async () => ({assignments: {bkw_p: {role: 'pv'}}})};
    return {ok: true, status: 200, json: async () => ({entities: []})};
  };
  const {component, window, stores} = createDevicemapPanel({fetchImpl});
  let changed = 0;
  window.addEventListener('energy-roles-changed', () => { changed += 1; });
  component.devices = panelDevices();
  component.deviceMap = {version: 1, nodes: [], edges: []};
  component.csrfToken = 'tok';
  component.energy = {entities: [{entity_id: 'bkw_p', device_id: 'bkw', value: 600, unit: 'W', role: {role: 'pv', scale: 1, source: 'heuristic'}}]};
  await component.onNodeTap({target: fakeNode('bkw')});
  component.pinAssignment('bkw_p');
  await component.savePanel();
  const patch = calls.find(call => call.options.method === 'PATCH');
  assert.equal(patch.url.endsWith('/api/v1/energy/roles'), true);
  assert.equal(patch.options.headers['X-CSRF-Token'], 'tok');
  assert.deepEqual(JSON.parse(patch.options.body), {assignments: {bkw_p: {role: 'pv', scale: 1, invert: false}}});
  assert.deepEqual(Object.keys(component.drafts), []);
  assert.deepEqual(JSON.parse(JSON.stringify(component.savedAssignments)), {bkw_p: {role: 'pv'}});
  assert.ok(calls.some(call => call.url.endsWith('/api/v1/energy') && !call.options.method));
  assert.equal(changed, 1);
  assert.equal(stores.toasts.last(), 'Rollen gespeichert.');
});

test('a failed save keeps the drafts and shows the error', async () => {
  const fetchImpl = async () => ({ok: false, status: 400, json: async () => ({code: 'energy_roles_rejected', message: 'nope'})});
  const {component, stores} = createDevicemapPanel({fetchImpl});
  component.devices = panelDevices();
  component.deviceMap = {version: 1, nodes: [], edges: []};
  await component.onNodeTap({target: fakeNode('bkw')});
  component.setDraft('bkw_p', 'role', 'load');
  await component.savePanel();
  assert.equal(Object.keys(component.drafts).length, 1);
  assert.equal(stores.toasts.criticals.length, 1);
});

test('showInRoleTable asks the shell to open the energy tab with the panel entities', async () => {
  const {component, window} = createDevicemapPanel();
  let detail = null;
  window.addEventListener('dashboard-open-panel', event => { detail = event.detail; });
  component.devices = panelDevices();
  component.deviceMap = {version: 1, nodes: [], edges: []};
  await component.onNodeTap({target: fakeNode('bkw')});
  component.showInRoleTable();
  assert.deepEqual(JSON.parse(JSON.stringify(detail)), {panel: 'energy-panel', energyFocus: ['bkw_p']});
});

test('panelStatusText counts the drafts', async () => {
  const {component} = createDevicemapPanel();
  component.devices = panelDevices();
  component.deviceMap = {version: 1, nodes: [], edges: []};
  await component.onNodeTap({target: fakeNode('bkw')});
  assert.equal(component.panelStatusText(), '');
  component.setDraft('bkw_p', 'role', 'load');
  assert.equal(component.panelStatusText(), '1 Änderung nicht gespeichert');
});

const groupFixture = component => {
  component.devices = panelDevices();
  component.deviceMap = {version: 2, nodes: [{device_id: 'bkw', x: 400, y: 400}, {device_id: 'wb', x: 600, y: 420}], edges: []};
  component.savedGroups = {garage: {label: 'Garage', members: {devices: ['wb'], groups: []}}};
  component.savedCategories = {};
};

test('groups become nodes and their memberships wiring edges', () => {
  const {component} = createDevicemapPanel();
  groupFixture(component);
  const elements = component.buildElements();
  assert.ok(elements.some(element => element.data.id === 'group:garage'));
  const membership = elements.find(element => element.data.source === 'group:garage' && element.data.target === 'wb');
  assert.ok(membership);
  assert.deepEqual({...membership.data.membership}, {group: 'garage', member: 'wb'});
});

test('connecting a device under a group patches the membership and moves it out of its old group', async () => {
  const calls = [];
  const fetchImpl = async (url, options = {}) => {
    calls.push({url, options});
    if (options.method === 'PATCH') return {ok: true, status: 200, json: async () => ({assignments: {}, groups: JSON.parse(options.body).groups})};
    return {ok: true, status: 200, json: async () => ({entities: []})};
  };
  const {component, stores} = createDevicemapPanel({fetchImpl});
  groupFixture(component);
  component.savedGroups.werkstatt = {label: 'Werkstatt', members: {devices: [], groups: []}};
  component.connectMode = true;
  await component.onNodeTap({target: fakeNode('wb')});
  await component.onNodeTap({target: fakeNode('group:werkstatt')});
  const patch = JSON.parse(calls.find(call => call.options.method === 'PATCH').options.body);
  assert.deepEqual(patch.groups.garage.members.devices, []);
  assert.deepEqual(patch.groups.werkstatt.members.devices, ['wb']);
  assert.match(stores.modal.calls[0].body, /Garage/);
});

test('connecting a group under a device creates a feed relation', async () => {
  const calls = [];
  const fetchImpl = async (url, options = {}) => {
    calls.push({url, options});
    return {ok: true, status: 201, json: async () => ({id: 'r1', child_id: 'group:garage', parent_id: 'bkw', kind: 'via_device'})};
  };
  const {component} = createDevicemapPanel({fetchImpl});
  groupFixture(component);
  component.connectMode = true;
  await component.onNodeTap({target: fakeNode('group:garage')});
  await component.onNodeTap({target: fakeNode('bkw')});
  const post = calls.find(call => call.options.method === 'POST');
  assert.ok(post.url.endsWith('/api/v1/device/map/relations'));
  assert.deepEqual(JSON.parse(post.options.body), {child_id: 'group:garage', parent_id: 'bkw', kind: 'via_device'});
});

test('removing a selected membership edge patches the group', async () => {
  const calls = [];
  const fetchImpl = async (url, options = {}) => {
    calls.push({url, options});
    return {ok: true, status: 200, json: async () => ({assignments: {}, groups: {garage: {label: 'Garage', members: {devices: [], groups: []}}}})};
  };
  const {component} = createDevicemapPanel({fetchImpl});
  groupFixture(component);
  component.selectedEdge = {id: 'edge-x', source: 'group:garage', target: 'wb', overrideId: null, membership: {group: 'garage', member: 'wb'}};
  await component.removeSelectedRelation();
  const patch = JSON.parse(calls.find(call => call.options.method === 'PATCH').options.body);
  assert.deepEqual(patch.groups.garage.members.devices, []);
});

test('save writes version 2 with virtual group nodes and drops vanished groups', async () => {
  const calls = [];
  const fetchImpl = async (url, options = {}) => { calls.push({url, options}); return {ok: true, status: 200, json: async () => ({})}; };
  const {component} = createDevicemapPanel({fetchImpl});
  groupFixture(component);
  component.deviceMap.nodes.push({virtual_id: 'group:garage', x: 500, y: 290}, {virtual_id: 'group:weg', x: 1, y: 1});
  await component.save();
  const body = JSON.parse(calls.find(call => call.options.method === 'PUT').options.body);
  assert.equal(body.version, 2);
  assert.deepEqual(body.nodes.filter(node => node.virtual_id), [{virtual_id: 'group:garage', x: 500, y: 290}]);
});

test('a new group is placed above its members', () => {
  const {component} = createDevicemapPanel();
  groupFixture(component);
  component.savedGroups.garage.members.devices = ['bkw', 'wb'];
  assert.equal(component.placeNewGroups(), 1);
  const node = component.deviceMap.nodes.find(item => item.virtual_id === 'group:garage');
  assert.deepEqual({...node}, {virtual_id: 'group:garage', x: 500, y: 290});
});

test('createGroup derives an id from the label and patches it', async () => {
  const calls = [];
  const fetchImpl = async (url, options = {}) => {
    calls.push({url, options});
    return {ok: true, status: 200, json: async () => ({assignments: {}, groups: {...JSON.parse(options.body || '{}').groups}})};
  };
  const {component} = createDevicemapPanel({fetchImpl});
  groupFixture(component);
  await component.createGroup('Garage');
  const patch = JSON.parse(calls.find(call => call.options.method === 'PATCH').options.body);
  assert.deepEqual(Object.keys(patch.groups), ['garage_2']);
  assert.deepEqual(patch.groups.garage_2, {label: 'Garage', members: {devices: [], groups: []}});
});

test('tapping a group opens its panel with members, addable devices and categories', async () => {
  const {component} = createDevicemapPanel();
  groupFixture(component);
  component.savedCategories = {werkstatt: {label: 'Werkstatt', base: 'consumer', color: 'cat_1', icon: 'mdi:home'}};
  await component.onNodeTap({target: fakeNode('group:garage')});
  const view = component.panelView;
  assert.equal(view.kind, 'group');
  assert.deepEqual(JSON.parse(JSON.stringify(view.members.map(member => member.id))), ['wb']);
  assert.ok(view.addable.some(item => item.id === 'bkw'));
  assert.ok(!view.addable.some(item => item.id === 'energy_node'));
  assert.deepEqual(JSON.parse(JSON.stringify(view.roleOptions.map(option => option.value))), ['', 'custom:werkstatt']);
});

test('group name and role are drafts, guarded like device drafts and saved together', async () => {
  const calls = [];
  const fetchImpl = async (url, options = {}) => {
    calls.push({url, options});
    return {ok: true, status: 200, json: async () => ({assignments: {}, groups: JSON.parse(options.body || '{}').groups || {}})};
  };
  const {component, stores} = createDevicemapPanel({fetchImpl, confirmAnswer: false});
  groupFixture(component);
  component.savedCategories = {werkstatt: {label: 'Werkstatt', base: 'consumer', color: 'cat_1', icon: 'mdi:home'}};
  await component.onNodeTap({target: fakeNode('group:garage')});
  component.setGroupDraft('label', 'Garage Nord');
  component.setGroupDraft('role', 'custom:werkstatt');
  assert.equal(component.panelDirty(), true);
  await component.onBackgroundTap();
  assert.equal(stores.modal.calls.length, 1);
  assert.equal(component.panelId, 'group:garage');
  await component.saveGroupPanel();
  const patch = JSON.parse(calls.find(call => call.options.method === 'PATCH').options.body);
  assert.deepEqual(patch.groups.garage, {label: 'Garage Nord', members: {devices: ['wb'], groups: []}, role: 'custom:werkstatt'});
  assert.equal(component.groupDraft, null);
});

test('deleteGroup asks, patches null and closes the panel', async () => {
  const calls = [];
  const fetchImpl = async (url, options = {}) => { calls.push({url, options}); return {ok: true, status: 200, json: async () => ({assignments: {}, groups: {}})}; };
  const {component} = createDevicemapPanel({fetchImpl, confirmAnswer: true});
  groupFixture(component);
  await component.onNodeTap({target: fakeNode('group:garage')});
  await component.deleteGroup();
  const patch = JSON.parse(calls.find(call => call.options.method === 'PATCH').options.body);
  assert.deepEqual(patch, {groups: {garage: null}});
  assert.equal(component.panelId, null);
});

test('createCategory derives an id and offers the category in the device role select', async () => {
  const fetchImpl = async (url, options = {}) => ({ok: true, status: 200, json: async () => ({assignments: {}, categories: JSON.parse(options.body).categories})});
  const {component} = createDevicemapPanel({fetchImpl});
  groupFixture(component);
  component.categoryForm = {label: 'Werkstatt', base: 'consumer', color: 'cat_2', icon: 'mdi:home'};
  await component.createCategory();
  assert.equal(component.savedCategories.werkstatt.color, 'cat_2');
  const options = component.roleOptionsFor('W');
  assert.equal(options[options.length - 1].value, 'custom:werkstatt');
});

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
  for (const name of ['devicemap-model.js', 'devicemap-graph.js', 'devicemap-subdevices.js']) {
    vm.runInContext(fs.readFileSync(path.join(jsDir, name), 'utf8'), context);
  }
  return dom.window.DeviceMapSubDevices;
}
const plain = value => JSON.parse(JSON.stringify(value));

const sub = { id: 'sub:wm', label: 'Waschmaschine', icon: 'plug', device_id: 'plug_b', entity_id: 'plug_b_power' };
const deviceMap = { sub_devices: [sub] };

test('isSubId recognises the sub: prefix only', () => {
  const sd = load();
  assert.equal(sd.PREFIX, 'sub:');
  assert.equal(sd.isSubId('sub:wm'), true);
  assert.equal(sd.isSubId('plug_b'), false);
  assert.equal(sd.isSubId('group:x'), false);
});

test('list and byEntity tolerate a map without sub-devices', () => {
  const sd = load();
  assert.deepEqual(plain(sd.list(deviceMap)), [sub]);
  assert.deepEqual(plain(sd.list({})), []);
  assert.equal(sd.byEntity(deviceMap).get('plug_b_power').id, 'sub:wm');
  assert.equal(sd.byEntity({}).size, 0);
});

test('labelFor returns the label or null without sub-device', () => {
  const sd = load();
  assert.equal(sd.labelFor(deviceMap, 'plug_b_power'), 'Waschmaschine');
  assert.equal(sd.labelFor(deviceMap, 'plug_b_energy'), null);
});

test('remapSnapshot moves only anchored entities and leaves the input untouched', () => {
  const sd = load();
  const snapshot = { entities: [
    { entity_id: 'plug_b_power', device_id: 'plug_b', value: 12 },
    { entity_id: 'plug_b_energy', device_id: 'plug_b', value: 3 },
  ] };
  const before = plain(snapshot);
  const remapped = sd.remapSnapshot(snapshot, deviceMap);
  assert.equal(remapped.entities[0].device_id, 'sub:wm');
  assert.equal(remapped.entities[1].device_id, 'plug_b');
  assert.deepEqual(plain(snapshot), before);
  assert.notEqual(remapped, snapshot);
  assert.equal(sd.remapSnapshot(null, deviceMap), null);
});

test('pairs connect the parent device to the sub-device when the parent is on the map', () => {
  const sd = load();
  assert.deepEqual(plain(sd.pairs(deviceMap, new Set(['plug_b']))), [{ parent: 'plug_b', child: 'sub:wm' }]);
  assert.deepEqual(plain(sd.pairs(deviceMap, new Set(['other']))), []);
  assert.deepEqual(plain(sd.pairs({}, new Set(['plug_b']))), []);
});

test('isOrphan is true when the device or the entity is missing', () => {
  const sd = load();
  const devices = [{ id: 'plug_b', entities: [{ entity_id: 'plug_b_power', unit_of_measurement: 'W' }] }];
  assert.equal(sd.isOrphan(sub, devices), false);
  assert.equal(sd.isOrphan(sub, []), true);
  assert.equal(sd.isOrphan(sub, [{ id: 'plug_b', entities: [] }]), true);
});

test('placeNew sits right of the anchor and moves down past taken spots', () => {
  const sd = load();
  assert.deepEqual(plain(sd.placeNew({ anchor: { x: 0, y: 0 }, taken: [], snap: null })), { x: 140, y: 0 });
  assert.deepEqual(plain(sd.placeNew({ anchor: { x: 0, y: 0 }, taken: [{ x: 140, y: 0 }], snap: null })), { x: 140, y: 110 });
  assert.deepEqual(plain(sd.placeNew({ anchor: { x: 0, y: 0 }, taken: [{ x: 140, y: 0 }], snap: 40 })), { x: 160, y: 120 });
});

test('powerEntities keeps only W and kW entities', () => {
  const sd = load();
  const device = { entities: [
    { entity_id: 'a', unit_of_measurement: 'W' },
    { entity_id: 'b', unit_of_measurement: 'kW' },
    { entity_id: 'c', unit_of_measurement: '%' },
    { entity_id: 'd', unit_of_measurement: 'V' },
  ] };
  assert.deepEqual(plain(sd.powerEntities(device).map(e => e.entity_id)), ['a', 'b']);
  assert.deepEqual(plain(sd.powerEntities({})), []);
});

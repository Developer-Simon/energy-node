import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'devicemap-model.js'), 'utf8');

function loadModel() {
  const dom = new JSDOM('<!doctype html><html><head></head><body></body></html>', { runScripts: 'outside-only' });
  installI18n(dom.window);
  vm.runInContext(source, dom.getInternalVMContext());
  return dom.window.DeviceMapModel;
}
const plain = value => JSON.parse(JSON.stringify(value));
const entity = (device_id, entity_id, role, value, source = 'override', unit = 'W') => ({ device_id, entity_id, value, unit, role: { role, source } });

test('deviceEnergy groups roles per device and signs power toward the device', () => {
  const model = loadModel();
  const energy = model.deviceEnergy({ entities: [
    entity('pv', 'pv1', 'pv', 1300), entity('pv', 'pv2', 'pv', 1100),
    entity('bat', 'bat_p', 'battery', -400),
    entity('soc', 'soc_1', 'battery_soc', 64, 'override', '%'),
    entity('bkw', 'bkw_p', 'pv', 600, 'heuristic'),
  ] });
  assert.deepEqual(plain(energy.get('pv')), { roles: ['pv'], heuristic: false, power: -2400, soc: null, byRole: { pv: 2400 }, primaryRole: 'pv' });
  assert.equal(energy.get('bat').power, -400, 'discharging battery pushes power away from the device');
  assert.equal(energy.get('soc').power, null, 'battery_soc is not a power role');
  assert.equal(energy.get('soc').soc, 64);
  assert.equal(energy.get('soc').primaryRole, 'battery_soc');
  assert.equal(energy.get('bkw').heuristic, true);
});

test('deviceEnergy ignores entities without a known role and tolerates a missing snapshot', () => {
  const model = loadModel();
  assert.equal(model.deviceEnergy(null).size, 0);
  assert.equal(model.deviceEnergy({ entities: [entity('x', 'x1', '', 5), entity('y', 'y1', 'nonsense', 5)] }).size, 0);
});

test('primaryRole is the power role with the largest magnitude', () => {
  const model = loadModel();
  const energy = model.deviceEnergy({ entities: [entity('d', 'a', 'load', 100), entity('d', 'b', 'wallbox', 1100), entity('d', 'c', 'battery_soc', 50, 'override', '%')] });
  assert.equal(energy.get('d').primaryRole, 'wallbox');
  assert.deepEqual(plain(energy.get('d').roles), ['load', 'wallbox', 'battery_soc']);
});

test('nodeValueText follows the draft wording per role', () => {
  const model = loadModel();
  const one = (role, value, unit = 'W') => model.deviceEnergy({ entities: [entity('d', 'e', role, value, 'override', unit)] }).get('d');
  assert.equal(model.nodeValueText({}, one('pv', 2400)), '2,4 kW');
  assert.equal(model.nodeValueText({}, one('load', 800)), '800 W');
  assert.equal(model.nodeValueText({}, one('grid', 1000)), '1,0 kW Bezug');
  assert.equal(model.nodeValueText({}, one('grid', -300)), '300 W Einspeisung');
  assert.equal(model.nodeValueText({}, one('battery', 757)), '757 W lädt');
  assert.equal(model.nodeValueText({}, one('battery', -200)), '200 W entlädt');
  assert.equal(model.nodeValueText({}, one('battery_soc', 64, '%')), '64 %');
});

test('nodeValueText falls back to the first numeric reading, then to "keine Messwerte"', () => {
  const model = loadModel();
  const device = { entities: [{ value: 'on' }, { value: '52.8', unit_of_measurement: 'V' }] };
  assert.equal(model.nodeValueText(device, undefined), '52,8 V');
  assert.equal(model.nodeValueText({ entities: [] }, undefined), 'keine Messwerte');
});

test('deviceHealth counts only entities that publish availability', () => {
  const model = loadModel();
  assert.equal(model.deviceHealth({ entities: [{ has_availability: false }] }), 'unknown');
  assert.equal(model.deviceHealth({ entities: [{ has_availability: true, available: true }, { has_availability: false }] }), 'ok');
  assert.equal(model.deviceHealth({ entities: [{ has_availability: true, available: true }, { has_availability: true, available: false }] }), 'degraded');
  assert.equal(model.deviceHealth({ entities: [{ has_availability: true, available: false }] }), 'down');
});

test('ringSpec describes segments, dashing and icon like draft variant C', () => {
  const model = loadModel();
  const energy = model.deviceEnergy({ entities: [entity('d', 'a', 'battery', 800), entity('d', 'b', 'battery_soc', 60, 'heuristic', '%')] });
  assert.deepEqual(plain(model.ringSpec(energy.get('d'), 'ok', true)), { segments: ['flow-battery', 'flow-battery'], dashed: true, icon: 'battery', iconColor: 'flow-battery', health: 'ok' });
  assert.deepEqual(plain(model.ringSpec(undefined, 'unknown', true)), { segments: [], dashed: false, icon: 'sensor', iconColor: 'text-muted', health: 'unknown' });
  assert.equal(model.ringSpec(undefined, 'unknown', false).icon, 'box');
});

test('relatedIds returns the node with its direct parents and children', () => {
  const model = loadModel();
  const pairs = [{ parent: 'netz', child: 'uv' }, { parent: 'uv', child: 'wallbox' }, { parent: 'netz', child: 'pv' }];
  assert.deepEqual([...model.relatedIds('uv', pairs)].sort(), ['netz', 'uv', 'wallbox']);
});

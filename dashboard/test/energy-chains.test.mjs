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
  for (const name of ['devicemap-model.js', 'energy-chains.js']) {
    vm.runInContext(fs.readFileSync(path.join(jsDir, name), 'utf8'), context);
  }
  return dom.window.EnergyChains;
}
const plain = value => JSON.parse(JSON.stringify(value));

const labels = {
  'plug_balkon_power': 'Plug Balkon',
  'storage_ac_power': 'Speicher AC',
  'storage_dc_power': 'DC',
  'meter_power': 'Hausanschluss',
};
const labelOf = id => labels[id] || '';

test('insertPoint places the entity at the index and keeps the counting point', () => {
  const ec = load();
  const chain = {label: 'x', points: ['a', 'c'], counting: 'a'};
  assert.deepEqual(plain(ec.insertPoint(chain, 1, 'b')), {label: 'x', points: ['a', 'b', 'c'], counting: 'a'});
  assert.deepEqual(plain(ec.insertPoint(chain, 2, 'd')), {label: 'x', points: ['a', 'c', 'd'], counting: 'a'});
  assert.deepEqual(plain(ec.insertPoint(chain, 0, 'z')), {label: 'x', points: ['z', 'a', 'c'], counting: 'a'});
  assert.deepEqual(plain(chain), {label: 'x', points: ['a', 'c'], counting: 'a'}, 'the input stays untouched');
});

test('removePoint hands counting on and refuses to go below two points', () => {
  const ec = load();
  const chain = {label: 'x', points: ['a', 'b', 'c'], counting: 'a'};
  assert.deepEqual(plain(ec.removePoint(chain, 'a')), {label: 'x', points: ['b', 'c'], counting: 'b'});
  assert.equal(ec.removePoint({label: 'x', points: ['a', 'b'], counting: 'a'}, 'b'), null);
  assert.deepEqual(plain(ec.removePoint(chain, 'c')), {label: 'x', points: ['a', 'b'], counting: 'a'}, 'a non counting point keeps counting');
  assert.deepEqual(plain(chain), {label: 'x', points: ['a', 'b', 'c'], counting: 'a'}, 'the input stays untouched');
});

test('setCounting changes the counting point only among the points', () => {
  const ec = load();
  const chain = {label: 'x', points: ['a', 'b'], counting: 'a'};
  assert.deepEqual(plain(ec.setCounting(chain, 'b')), {label: 'x', points: ['a', 'b'], counting: 'b'});
  assert.deepEqual(plain(ec.setCounting(chain, 'elsewhere')), {label: 'x', points: ['a', 'b'], counting: 'a'});
  assert.deepEqual(plain(chain), {label: 'x', points: ['a', 'b'], counting: 'a'}, 'the input stays untouched');
});

test('chainOf returns the chain that lists the entity or null', () => {
  const ec = load();
  const chains = {
    second: {label: 'B', points: ['c', 'd'], counting: 'c'},
    first: {label: 'A', points: ['a', 'b'], counting: 'a'},
  };
  assert.deepEqual(plain(ec.chainOf(chains, 'b')), {id: 'first', chain: {label: 'A', points: ['a', 'b'], counting: 'a'}});
  assert.deepEqual(plain(ec.chainOf(chains, 'd')), {id: 'second', chain: {label: 'B', points: ['c', 'd'], counting: 'c'}});
  assert.equal(ec.chainOf(chains, 'nope'), null);
  assert.equal(ec.chainOf({}, 'a'), null);
  assert.equal(ec.chainOf(null, 'a'), null);
});

test('isReference is true only for measuring points that do not count', () => {
  const ec = load();
  const chains = {main: {label: 'A', points: ['a', 'b'], counting: 'a'}};
  assert.equal(ec.isReference(chains, 'a'), false, 'the counting point is not a reference');
  assert.equal(ec.isReference(chains, 'b'), true);
  assert.equal(ec.isReference(chains, 'other'), false);
  assert.equal(ec.isReference({}, 'b'), false);
  assert.equal(ec.isReference(null, 'b'), false);
});

test('shortPath joins the labels of the points with arrows', () => {
  const ec = load();
  const chain = {label: 'x', points: ['plug_balkon_power', 'storage_ac_power', 'storage_dc_power'], counting: 'plug_balkon_power'};
  assert.equal(ec.shortPath(chain, labelOf), 'Plug Balkon → Speicher AC → DC');
  assert.equal(ec.shortPath({label: 'x', points: ['meter_power'], counting: 'meter_power'}, labelOf), 'Hausanschluss');
  assert.equal(ec.shortPath({label: 'x', points: ['unknown_power', 'meter_power'], counting: 'unknown_power'}, labelOf), 'unknown_power → Hausanschluss', 'an empty label falls back to the entity ID');
});

test('lossText shows the loss with the efficiency, without it or as a dash', () => {
  const ec = load();
  assert.equal(ec.lossText({available: true, loss_w: 9, efficiency: 0.97}), '−9 W · 97 %');
  assert.equal(ec.lossText({available: true, loss_w: 9}), '−9 W');
  assert.equal(ec.lossText({available: true, loss_w: 1500, efficiency: 0.5}), '−1,5 kW · 50 %');
  assert.equal(ec.lossText({available: true, loss_w: 0}), '0 W');
  assert.equal(ec.lossText({available: true, loss_w: -9}), '+9 W', 'a negative loss reads as a gain');
  assert.equal(ec.lossText({available: false}), '–');
  assert.equal(ec.lossText(null), '–');
});

test('pickerItems leaves out chain members and non power rows', () => {
  const ec = load();
  const entities = [
    {id: 'plug_balkon_power', name: 'Plug Balkon', section: 'Shelly', valueText: '12 W', iconMarkup: '<i></i>', unit_of_measurement: 'W'},
    {id: 'storage_ac_power', name: 'Speicher AC', section: 'Hersteller', valueText: '', iconMarkup: '', unit_of_measurement: 'kW'},
    {id: 'meter_power', name: 'Hausanschluss', section: '', valueText: '', iconMarkup: '', unit_of_measurement: 'W'},
    {id: 'plug_balkon_energy', name: 'Energie', section: 'Shelly', valueText: '', iconMarkup: '', unit_of_measurement: 'kWh'},
    {id: 'plug_balkon_voltage', name: 'Spannung', section: 'Shelly', valueText: '', iconMarkup: '', unit_of_measurement: 'V'},
    {id: 'bare_power', name: 'Ohne Einheit', section: '', valueText: ''},
  ];
  const chains = {main: {label: 'A', points: ['plug_balkon_power', 'storage_ac_power'], counting: 'plug_balkon_power'}};
  assert.deepEqual(plain(ec.pickerItems(entities, chains)), [
    {id: 'meter_power', kind: 'device', name: 'Hausanschluss', section: '', valueText: '', iconMarkup: '', memberOf: ''},
  ]);
  assert.deepEqual(plain(ec.pickerItems(entities, {})).map(item => item.id), ['plug_balkon_power', 'storage_ac_power', 'meter_power']);
  assert.deepEqual(plain(ec.pickerItems([], {})), []);
});

test('edgeChange creates a chain when both ends are free', () => {
  const ec = load();
  const result = ec.edgeChange({chains: {}, upstream: 'up', downstream: 'down', label: 'Neue Kette'});
  assert.deepEqual(plain(result), {action: 'create', body: {label: 'Neue Kette', points: ['up', 'down'], counting: 'up'}});
});

test('edgeChange adds a free downstream point right behind an upstream point in a chain', () => {
  const ec = load();
  const chains = {main: {label: 'A', points: ['up', 'other'], counting: 'up', direction: 'reversed'}};
  const result = ec.edgeChange({chains, upstream: 'up', downstream: 'down', label: 'x'});
  assert.deepEqual(plain(result), {
    action: 'update',
    id: 'main',
    body: {label: 'A', points: ['up', 'down', 'other'], counting: 'up', direction: 'reversed'},
  });
});

test('edgeChange adds a free upstream point right in front of a downstream point in a chain', () => {
  const ec = load();
  const chains = {main: {label: 'A', points: ['other', 'down'], counting: 'other'}};
  const result = ec.edgeChange({chains, upstream: 'up', downstream: 'down', label: 'x'});
  assert.deepEqual(plain(result), {
    action: 'update',
    id: 'main',
    body: {label: 'A', points: ['other', 'up', 'down'], counting: 'other'},
  });
});

test('edgeChange does nothing when both ends are already neighbours in one chain', () => {
  const ec = load();
  const chains = {main: {label: 'A', points: ['up', 'down', 'x'], counting: 'up'}};
  assert.deepEqual(plain(ec.edgeChange({chains, upstream: 'up', downstream: 'down', label: 'x'})), {action: 'none'});
});

test('edgeChange reports a conflict for different chains or points that are not neighbours', () => {
  const ec = load();
  const chains = {
    first: {label: 'A', points: ['up', 'x'], counting: 'up'},
    second: {label: 'B', points: ['down', 'y'], counting: 'down'},
  };
  assert.deepEqual(plain(ec.edgeChange({chains, upstream: 'up', downstream: 'down', label: 'x'})), {action: 'conflict'});

  const apart = {main: {label: 'A', points: ['up', 'x', 'down'], counting: 'up'}};
  assert.deepEqual(plain(ec.edgeChange({chains: apart, upstream: 'up', downstream: 'down', label: 'x'})), {action: 'conflict'});
});

test('linkBetween finds direct neighbours in either order and ignores points further apart', () => {
  const chains = {m: {label: 'Speicher', points: ['plug', 'inverter', 'battery'], counting: 'plug'}};
  const {linkBetween} = load();
  assert.deepEqual({...linkBetween(chains, ['plug'], ['inverter'])}, {id: 'm', label: 'Speicher', upstream: 'plug', downstream: 'inverter'});
  assert.deepEqual({...linkBetween(chains, ['battery'], ['inverter'])}, {id: 'm', label: 'Speicher', upstream: 'inverter', downstream: 'battery'});
  assert.equal(linkBetween(chains, ['plug'], ['battery']), null);
  assert.equal(linkBetween(chains, ['other'], ['inverter']), null);
});

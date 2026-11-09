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
  for (const name of ['devicemap-model.js', 'energy-chains.js', 'energy-chain-suggest.js']) {
    vm.runInContext(fs.readFileSync(path.join(jsDir, name), 'utf8'), context);
  }
  return dom.window.EnergyChainSuggest;
}

const names = {
  plug_balkon_power: 'Plug Balkon',
  storage_ac_power: 'Speicher AC',
  storage_dc_power: 'Speicher DC',
};
const nameOf = id => names[id] || id;
const plain = value => JSON.parse(JSON.stringify(value));

const suggestion = overrides => ({
  a: 'plug_balkon_power',
  b: 'storage_ac_power',
  device_a: 'balkon',
  device_b: 'speicher',
  r: 0.976,
  days: 5,
  minutes: 900,
  mean_a_w: 120,
  mean_b_w: 110,
  edge: false,
  chain_id: '',
  ...overrides,
});

test('suggestion text names both points or the chain', () => {
  const model = load();
  assert.equal(
    model.suggestionText(suggestion(), nameOf, {}),
    'Plug Balkon misst vermutlich Speicher AC',
  );
  const chains = {
    k1: { label: 'Balkon', points: ['storage_ac_power', 'storage_dc_power'], counting: 'storage_ac_power', direction: 'auto' },
  };
  assert.equal(
    model.suggestionText(suggestion({ chain_id: 'k1' }), nameOf, chains),
    'Plug Balkon gehört vermutlich zur Messkette Balkon',
  );
});

test('evidence text rounds the correlation to whole percent', () => {
  const model = load();
  assert.equal(model.evidenceText(suggestion({ r: 0.976, days: 5 })), '98 % Übereinstimmung an 5 Tagen');
  assert.equal(model.evidenceText(suggestion({ r: 0.9, days: 1 })), '90 % Übereinstimmung an einem Tag');
});

test('dialog starts with A upstream and swaps', () => {
  const model = load();
  const dialog = model.dialogFor(suggestion(), nameOf);
  assert.deepEqual(plain(dialog), {
    upstream: 'plug_balkon_power',
    downstream: 'storage_ac_power',
    label: 'speicher',
    chainId: '',
  });
  const swapped = model.swap(dialog);
  assert.equal(swapped.upstream, 'storage_ac_power');
  assert.equal(swapped.downstream, 'plug_balkon_power');
  assert.equal(swapped.label, 'speicher');
  assert.equal(dialog.upstream, 'plug_balkon_power', 'swap leaves its input untouched');

  const extension = model.dialogFor(suggestion({ chain_id: 'k1' }), nameOf);
  assert.equal(extension.label, '');
  assert.equal(extension.chainId, 'k1');
});

test('change creates a chain for two free points', () => {
  const model = load();
  const dialog = model.dialogFor(suggestion(), nameOf);
  const change = model.changeFor({ ...dialog, label: 'Balkon' }, {});
  assert.equal(change.action, 'create');
  assert.deepEqual(plain(change.body), {
    label: 'Balkon',
    points: ['plug_balkon_power', 'storage_ac_power'],
    counting: 'plug_balkon_power',
  });
});

test('change extends the chain of one point', () => {
  const model = load();
  const chains = {
    k1: { label: 'Balkon', points: ['plug_balkon_power', 'storage_ac_power'], counting: 'plug_balkon_power', direction: 'auto' },
  };
  const dialog = model.dialogFor(suggestion({ a: 'storage_ac_power', b: 'storage_dc_power', chain_id: 'k1' }), nameOf);
  const change = model.changeFor(dialog, chains);
  assert.equal(change.action, 'update');
  assert.equal(change.id, 'k1');
  assert.deepEqual(plain(change.body.points), ['plug_balkon_power', 'storage_ac_power', 'storage_dc_power']);
  assert.equal(change.body.counting, 'plug_balkon_power');
});

test('change reports a conflict when both points are in chains', () => {
  const model = load();
  const chains = {
    k1: { label: 'Balkon', points: ['plug_balkon_power', 'storage_ac_power'], counting: 'plug_balkon_power', direction: 'auto' },
    k2: { label: 'Keller', points: ['storage_dc_power', 'plug_keller_power'], counting: 'storage_dc_power', direction: 'auto' },
  };
  const dialog = model.dialogFor(suggestion({ a: 'plug_balkon_power', b: 'storage_dc_power' }), nameOf);
  assert.equal(model.changeFor(dialog, chains).action, 'conflict');
});

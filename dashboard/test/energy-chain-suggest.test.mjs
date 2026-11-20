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
    suggestion: suggestion(),
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

const host = (chains = {}, extra = {}) => ({
  chains,
  chainSuggestName: nameOf,
  chainSuggestIcon: id => (id === 'plug_balkon_power' ? '<path d="plug"/>' : ''),
  chainPointValue: (chainId, id) => `live ${id}`,
  ...extra,
});
const chainK1 = { k1: { label: 'Balkon', points: ['storage_ac_power', 'storage_dc_power'], counting: 'storage_ac_power', direction: 'auto' } };

test('dialogFor keeps the suggestion and swap keeps it too', () => {
  const model = load();
  const s = suggestion();
  const dialog = model.dialogFor(s, nameOf);
  assert.deepEqual(plain(dialog.suggestion), plain(s));
  assert.deepEqual(plain(model.swap(dialog).suggestion), plain(s));
});

test('a new pair shows evidence, both points with eyebrow, mean and icon', () => {
  const model = load();
  const view = plain(model.modalView(model.dialogFor(suggestion(), nameOf), host()));
  assert.equal(view.extend, false);
  assert.equal(view.fits, true);
  assert.equal(view.title, 'Messkette übernehmen');
  assert.equal(view.percent, '98 %');
  assert.equal(view.daysText, 'an 5 Tagen');
  assert.equal(view.pairLink, 0);
  assert.deepEqual(view.nodes.map(n => [n.id, n.eyebrow, n.valueText, n.iconMarkup, n.last]), [
    ['plug_balkon_power', 'Netzseitig', 'Ø 120 W', '<path d="plug"/>', false],
    ['storage_ac_power', 'Danach', 'Ø 110 W', '', true],
  ]);
});

test('one day reads in the singular and the edge flag passes through', () => {
  const model = load();
  const view = model.modalView(model.dialogFor(suggestion({ days: 1, edge: true }), nameOf), host());
  assert.equal(view.daysText, 'an einem Tag');
  assert.equal(view.edge, true);
});

test('after a swap the means stay with their points', () => {
  const model = load();
  const view = plain(model.modalView(model.swap(model.dialogFor(suggestion(), nameOf)), host()));
  assert.deepEqual(view.nodes.map(n => [n.id, n.eyebrow, n.valueText]), [
    ['storage_ac_power', 'Netzseitig', 'Ø 110 W'],
    ['plug_balkon_power', 'Danach', 'Ø 120 W'],
  ]);
});

test('an extension lists the whole chain in edgeChange order with the new point marked', () => {
  const model = load();
  const view = plain(model.modalView(model.dialogFor(suggestion({ chain_id: 'k1' }), nameOf), host(chainK1)));
  assert.equal(view.extend, true);
  assert.equal(view.title, 'Messkette erweitern');
  assert.equal(view.chainLabel, 'Balkon');
  assert.deepEqual(view.nodes.map(n => [n.id, n.isNew, n.inPair, n.eyebrow, n.valueText]), [
    ['plug_balkon_power', true, true, 'Neu', 'Ø 120 W'],
    ['storage_ac_power', false, true, '', 'Ø 110 W'],
    ['storage_dc_power', false, false, '', 'live storage_dc_power'],
  ]);
  assert.equal(view.pairLink, 0);
});

test('a pair already adjacent in one chain fits and leaves the chain as it is', () => {
  const model = load();
  const chains = { k1: { label: 'Balkon', points: ['plug_balkon_power', 'storage_ac_power'], counting: 'plug_balkon_power', direction: 'auto' } };
  const view = plain(model.modalView(model.dialogFor(suggestion({ chain_id: 'k1' }), nameOf), host(chains)));
  assert.equal(view.fits, true);
  assert.deepEqual(view.nodes.map(n => [n.id, n.isNew]), [['plug_balkon_power', false], ['storage_ac_power', false]]);
});

test('a stale suggestion whose points sit in two chains does not fit and shows the chain unchanged', () => {
  const model = load();
  const chains = {
    ...chainK1,
    k2: { label: 'Garage', points: ['plug_balkon_power', 'x_power'], counting: 'plug_balkon_power', direction: 'auto' },
  };
  const extend = plain(model.modalView(model.dialogFor(suggestion({ chain_id: 'k1' }), nameOf), host(chains)));
  assert.equal(extend.fits, false);
  assert.deepEqual(extend.nodes.map(n => n.id), ['storage_ac_power', 'storage_dc_power']);
  assert.equal(extend.pairLink, -1);
  const fresh = plain(model.modalView(model.dialogFor(suggestion(), nameOf), host(chains)));
  assert.equal(fresh.fits, false, 'a new pair can turn stale too');
});

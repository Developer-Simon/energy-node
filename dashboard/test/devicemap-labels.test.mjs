import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'devicemap-labels.js'), 'utf8');
function setup() {
  const dom = new JSDOM('<!doctype html><html><body><div id="c"></div></body></html>', { runScripts: 'outside-only' });
  vm.runInContext(source, dom.getInternalVMContext());
  const container = dom.window.document.getElementById('c');
  return { labels: dom.window.DeviceMapLabels.create(container), container };
}
const fakeCy = (positions, zoom = 1) => ({
  zoom: () => zoom,
  getElementById: id => ({ empty: () => !positions[id], renderedPosition: () => positions[id] }),
});

test('update renders one label per item with name and value, and reuses elements', () => {
  const { labels, container } = setup();
  labels.update([{ id: 'a', name: 'APsystems Dach', value: '2,4 kW' }]);
  const first = container.querySelector('[data-label-id="a"]');
  assert.equal(first.querySelector('.devicemap-node-name').textContent, 'APsystems Dach');
  assert.equal(first.querySelector('.devicemap-node-value').textContent, '2,4 kW');
  labels.update([{ id: 'a', name: 'APsystems Dach', value: '2,5 kW' }]);
  assert.equal(container.querySelector('[data-label-id="a"]'), first, 'same element, only text changes');
  assert.equal(first.querySelector('.devicemap-node-value').textContent, '2,5 kW');
  labels.update([]);
  assert.equal(container.querySelector('[data-label-id="a"]'), null, 'labels of removed nodes disappear');
});

test('sync places the label under the node and scales it with the zoom', () => {
  const { labels, container } = setup();
  labels.update([{ id: 'a', name: 'A', value: '1 W' }, { id: 'gone', name: 'G', value: '' }]);
  labels.sync(fakeCy({ a: { x: 100, y: 50 } }, 2));
  const el = container.querySelector('[data-label-id="a"]');
  assert.equal(el.style.transform, 'translate(100px, 110px) translateX(-50%) scale(2)');
  assert.equal(el.hidden, false);
  assert.equal(container.querySelector('[data-label-id="gone"]').hidden, true);
});

test('setDimmed dims everything outside the focus set', () => {
  const { labels, container } = setup();
  labels.update([{ id: 'a', name: 'A', value: '' }, { id: 'b', name: 'B', value: '' }]);
  labels.setDimmed(new Set(['a']));
  assert.equal(container.querySelector('[data-label-id="a"]').classList.contains('is-dimmed'), false);
  assert.equal(container.querySelector('[data-label-id="b"]').classList.contains('is-dimmed'), true);
  labels.setDimmed(null);
  assert.equal(container.querySelector('[data-label-id="b"]').classList.contains('is-dimmed'), false);
});

test('setHidden fades labels with their own delay', () => {
  const { labels, container } = setup();
  labels.update([{ id: 'balance', name: 'Energiebilanz', value: '' }, { id: 'pv', name: 'PV', value: '' }]);
  labels.setHidden(new Set(['balance']), new Map([['balance', 40]]));
  const balance = container.querySelector('[data-label-id="balance"]');
  const pv = container.querySelector('[data-label-id="pv"]');
  assert.ok(balance.classList.contains('is-hidden'));
  assert.equal(balance.style.transitionDelay, '40ms');
  assert.ok(!pv.classList.contains('is-hidden'));
  labels.setHidden(new Set(), new Map());
  assert.ok(!balance.classList.contains('is-hidden'));
});

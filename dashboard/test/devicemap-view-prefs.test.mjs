import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const context = dom.getInternalVMContext();
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'devicemap-view-prefs.js'), 'utf8'), context);
  return dom.window.DeviceMapViewPrefs;
}
const plain = value => JSON.parse(JSON.stringify(value));

const base = {
  show_grid: false,
  layers: { wiring: true, energy: true, balance: false, data: false },
  wiring_types: { energy: true, data: true, info: true },
};

function memoryStorage(initial = {}) {
  const data = new Map(Object.entries(initial));
  return {
    data,
    getItem: key => (data.has(key) ? data.get(key) : null),
    setItem: (key, value) => { data.set(key, String(value)); },
    removeItem: key => { data.delete(key); },
  };
}

test('merge overrides a single layer without losing the others', () => {
  const w = load();
  const view = w.merge(base, { layers: { energy: false } });
  assert.deepEqual(plain(view.layers), { wiring: true, energy: false, balance: false, data: false });
  assert.deepEqual(plain(view.wiring_types), base.wiring_types);
  assert.equal(view.show_grid, false);
});

test('diff keeps only the deviating keys and diff of base is empty', () => {
  const w = load();
  const view = w.merge(base, { wiring_types: { info: false } });
  assert.deepEqual(plain(w.diff(base, view)), { wiring_types: { info: false } });
  assert.deepEqual(plain(w.diff(base, base)), {});
  assert.equal(w.isEmpty(w.diff(base, base)), true);
  assert.equal(w.isEmpty(w.diff(base, view)), false);
});

test('load keeps only known keys of the same type', () => {
  const w = load();
  const storage = memoryStorage({
    'devicemap-view': '{"show_grid":true,"bogus":1,"layers":{"wiring":"yes","energy":false}}',
  });
  assert.deepEqual(plain(w.load(storage, base)), { show_grid: true, layers: { energy: false } });
});

test('load returns an empty sparse view for broken JSON or a throwing storage', () => {
  const w = load();
  assert.deepEqual(plain(w.load(memoryStorage({ 'devicemap-view': 'not json' }), base)), {});
  const throwing = { getItem: () => { throw new Error('denied'); } };
  assert.deepEqual(plain(w.load(throwing, base)), {});
});

test('store with an empty sparse view removes the entry', () => {
  const w = load();
  const calls = [];
  const storage = { removeItem: key => calls.push(key), setItem: () => calls.push('set') };
  w.store(storage, {});
  assert.deepEqual(calls, ['devicemap-view']);
});

test('store writes JSON and swallows storage errors', () => {
  const w = load();
  const storage = memoryStorage();
  w.store(storage, { show_grid: true });
  assert.deepEqual(JSON.parse(storage.data.get('devicemap-view')), { show_grid: true });
  const throwing = { setItem: () => { throw new Error('quota'); } };
  assert.doesNotThrow(() => w.store(throwing, { show_grid: true }));
});

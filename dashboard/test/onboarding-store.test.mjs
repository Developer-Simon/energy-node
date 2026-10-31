// Tests for the onboarding store in onboarding.js: progress goes to the API,
// and network errors never break the bar. Run with `npm test` from dashboard/.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'onboarding.js'), 'utf8');

const PROGRESS = {
  role: 'admin',
  setup: { done: false, steps: {} },
  user: { seen: [], welcomed: false, run: null, queue: [] },
};

// Loads the store with a fake fetch. answer(url, method) returns the JSON
// body or throws to simulate a network error. The GET of the progress has
// finished before the test continues.
async function load(answer = (url) => (url === '/api/v1/auth/session' ? { csrf_token: 'tok' } : PROGRESS)) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const { window } = dom;
  const calls = [];
  window.fetch = async (url, options = {}) => {
    const method = options.method || 'GET';
    calls.push({ url, method, body: options.body ? JSON.parse(options.body) : undefined });
    const body = answer(url, method);
    return { ok: true, status: 200, json: async () => body };
  };
  const stores = {};
  window.Alpine = {
    magic() {},
    store(name, value) {
      if (value !== undefined) stores[name] = value;
      return stores[name];
    },
    data() {},
    effect() {},
  };
  installI18n(window);
  vm.runInContext(source, dom.getInternalVMContext());
  window.document.dispatchEvent(new window.Event('alpine:init'));
  await new Promise((resolve) => setImmediate(resolve));
  const store = stores.onboarding;
  // A running setup step, as the bar has it after start().
  store.run = { mode: 'setup', from: null, index: 0 };
  store.steps = [{ id: 'users', area: 'access', title: 'Konten', text: 'Text', dot: 0, dots: 1 }];
  store.index = 0;
  store.active = true;
  return { store, calls, window };
}

test('markStep done calls PUT step with {id,state} exactly once', async () => {
  const { store, calls } = await load();
  calls.length = 0;
  const ok = await store.markStep('done');
  assert.equal(ok, true);
  const puts = calls.filter((c) => c.method === 'PUT' && c.url === '/api/v1/onboarding/step');
  assert.equal(puts.length, 1);
  assert.deepEqual(puts[0].body, { id: 'users', state: 'done' });
  assert.equal(store.stateOf('users'), 'done');
});

test('a network error leaves active unchanged and does not throw', async () => {
  let down = false;
  const { store, calls } = await load((url, method) => {
    if (down && method === 'PUT') throw new TypeError('Failed to fetch');
    return url === '/api/v1/auth/session' ? { csrf_token: 'tok' } : PROGRESS;
  });
  down = true;
  calls.length = 0;
  let ok;
  await assert.doesNotReject(async () => { ok = await store.markStep('done'); });
  assert.equal(ok, false);
  assert.equal(store.active, true);
  const puts = calls.filter((c) => c.method === 'PUT');
  assert.equal(puts.length, 2, 'one retry after the first failure');
});

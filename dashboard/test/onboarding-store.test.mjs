// Tests for the onboarding store in onboarding.js: progress goes to the API,
// rights come from the session, and network errors never break the bar.
// Run with `npm test` from dashboard/.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');
const source = fs.readFileSync(path.join(scriptDir, 'onboarding.js'), 'utf8');
const modelSource = fs.readFileSync(path.join(scriptDir, 'onboarding-model.js'), 'utf8');
const stepsSource = fs.readFileSync(path.join(scriptDir, 'onboarding-steps.js'), 'utf8');

// Die Schritte des Servers (setup.items), ohne tailscale, weil der Dienst fehlt.
const ITEMS = [
  ['users', 'access', true], ['guest', 'access', true], ['companion', 'access', false],
  ['theme', 'display', true], ['title', 'display', true],
  ['statusbar', 'display', true], ['tabwidth', 'display', true], ['tiles', 'display', false],
  ['updates', 'system', true], ['metrics', 'system', true], ['config', 'devices', false], ['devices', 'devices', false],
  ['roles', 'devices', false], ['devicemap', 'devices', false],
].map(([id, area, installer]) => ({ id, area, installer, state: '' }));

const PROGRESS = {
  role: 'admin',
  setup: { done: false, steps: {}, items: ITEMS },
  user: { seen: [], welcomed: false, run: null, queue: [] },
};
const SESSION = { csrf_token: 'tok', roles: ['user_management', 'view_devices', 'view_stats'] };

// Loads the store with a fake fetch. answer(url, method) returns the JSON
// body or throws to simulate a network error. Progress and session have
// finished loading before the test continues. The state of a running setup
// step is set unless `run` is given.
async function load({ progress = PROGRESS, session = SESSION, answer, run, prepare = true } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const { window } = dom;
  const calls = [];
  window.fetch = async (url, options = {}) => {
    const method = options.method || 'GET';
    calls.push({ url, method, body: options.body ? JSON.parse(options.body) : undefined });
    const body = answer ? answer(url, method) : (url === '/api/v1/auth/session' ? session : progress);
    return { ok: true, status: 200, json: async () => body };
  };
  const stores = {};
  const bars = {};
  // Set before installI18n runs its magic hook.
  window.Alpine = {
    magic() {},
    store(name, value) {
      if (value !== undefined) stores[name] = value;
      return stores[name];
    },
    data(name, factory) { bars[name] = factory; },
    effect() {},
  };
  installI18n(window);
  vm.runInContext(modelSource, dom.getInternalVMContext());
  vm.runInContext(stepsSource, dom.getInternalVMContext());
  vm.runInContext(source, dom.getInternalVMContext());
  window.document.dispatchEvent(new window.Event('alpine:init'));
  await new Promise((resolve) => setImmediate(resolve));
  const store = stores.onboarding;
  if (prepare) {
    // A running setup step, as the bar has it after start().
    store.run = run || { mode: 'setup', from: null, index: 0 };
    store.steps = [{ id: 'users', area: 'access', title: 'Konten', text: 'Text', dot: 0, dots: 1 }];
    store.index = 0;
    store.active = true;
  }
  // The bar component, called with the store as its $store.
  const bar = () => {
    const b = bars.obBar();
    b.$store = { onboarding: store };
    return b;
  };
  return { store, calls, window, bar };
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
  const { store, calls } = await load({
    answer: (url, method) => {
      if (down && method === 'PUT') throw new TypeError('Failed to fetch');
      return url === '/api/v1/auth/session' ? SESSION : PROGRESS;
    },
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

test('can follows the session rights and nothing else', async () => {
  const { store } = await load({ progress: { ...PROGRESS, role: 'user' }, session: SESSION });
  const { can } = store.ctx();
  assert.equal(can.has('view_devices'), true);
  assert.equal(can.has('view_stats'), true);
  assert.equal(can.has('edit_users'), true, 'edit_users maps to user_management');
  assert.equal(can.has('operate_devices'), false);
});

test('steps that need a right the session lacks are dropped', async () => {
  const { store, window } = await load({
    progress: { ...PROGRESS, role: 'user' },
    session: { csrf_token: 'tok', roles: ['view_devices'] },
  });
  const step = { id: 'x', need: 'edit_users' };
  assert.equal(window.obModel.stepAllowed(step, store.ctx()), false);
  assert.equal(window.obModel.stepAllowed({ id: 'y', need: 'view_devices' }, store.ctx()), true);
});

test('can is empty when the session call fails', async () => {
  const { store } = await load({
    answer: (url) => {
      if (url === '/api/v1/auth/session') throw new TypeError('Failed to fetch');
      return PROGRESS;
    },
  });
  const { can } = store.ctx();
  assert.equal(can.has('view_devices'), false);
  assert.equal(can.has('edit_users'), false);
  assert.equal(can.has('user_management'), false);
});

test('a run that builds no steps sends run:null once', async () => {
  const { store, calls } = await load({ prepare: false });
  calls.length = 0;
  store.start('explore', 'no-such-highlight', { index: 0 });
  const runClears = calls.filter((c) => c.method === 'PUT' && c.url === '/api/v1/onboarding/me' && c.body && 'run' in c.body && c.body.run === null);
  assert.equal(runClears.length, 1);
  assert.equal(store.run, null);
});

test('setupCounts does not count a later step as done', async () => {
  const { store } = await load({ prepare: false });
  const before = store.setupCounts.done;
  store.status = { ...store.status, users: 'later' };
  assert.equal(store.setupCounts.done, before);
  store.status = { ...store.status, users: 'done' };
  assert.equal(store.setupCounts.done, before + 1);
});

test('explore later() sends no request and seen stays empty', async () => {
  const { store, calls, bar } = await load({
    prepare: false,
    progress: { ...PROGRESS, role: 'user' },
    session: SESSION,
  });
  store.start('explore', null, { index: 0 });
  store.steps = [...store.steps, { ...store.steps[0] }];
  calls.length = 0;
  bar().later();
  assert.equal(calls.length, 0, 'no request at all');
  assert.equal(Object.keys(store.seen).length, 0);
  assert.equal(store.index, 1);
  assert.equal(store.active, true);
});

test('explore later() on the last step ends the tour without a finish card', async () => {
  const { store, calls, bar } = await load({ prepare: false, progress: { ...PROGRESS, role: 'user' }, session: SESSION });
  store.start('explore', null, { index: 0 });
  store.index = store.steps.length - 1;
  calls.length = 0;
  bar().later();
  assert.equal(store.active, false);
  assert.equal(store.finished, null);
  assert.equal(Object.keys(store.seen).length, 0);
  const writes = calls.filter((c) => c.method === 'PUT');
  assert.equal(writes.length, 1, 'only the run clear');
  assert.deepEqual(writes[0].body, { run: null });
});

test('setup later() sends PUT step with state later', async () => {
  const { store, calls, bar } = await load();
  calls.length = 0;
  bar().later();
  const puts = calls.filter((c) => c.method === 'PUT' && c.url === '/api/v1/onboarding/step');
  assert.equal(puts.length, 1);
  assert.deepEqual(puts[0].body, { id: 'users', state: 'later' });
  assert.equal(store.stateOf('users'), 'later');
});

test('setup later() on a done step keeps done', async () => {
  const { store, calls } = await load({
    progress: { ...PROGRESS, setup: { done: false, steps: { users: 'done' } } },
  });
  calls.length = 0;
  store.markStep('later');
  assert.equal(calls.filter((c) => c.method === 'PUT').length, 0);
  assert.equal(store.stateOf('users'), 'done');
});

test('leaving the intro step marks setup-intro as seen and writes no setup step', async () => {
  const { store, calls, bar } = await load();
  store.steps = [{ id: 'setup-intro', intro: true, title: 'T', text: 'X', dot: 0, dots: 0 }, ...store.steps];
  store.index = 0;
  calls.length = 0;
  bar().next();
  const seen = calls.filter((c) => c.method === 'PUT' && c.url === '/api/v1/onboarding/me' && c.body?.seen_add);
  assert.deepEqual(seen.map((c) => [...c.body.seen_add]), [['setup-intro']]);
  assert.equal(calls.some((c) => c.url === '/api/v1/onboarding/step'), false);
  assert.equal(store.seen['setup-intro'], true);
});

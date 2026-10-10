// The update bar under the masthead (update-bar.js): update available,
// running, dashboard restarting, finished, failed. updateBarView() is the pure
// state function; updateBar() polls and acknowledges. Same vm + jsdom
// technique as the other page tests.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'update-bar.js'), 'utf8');

function load({ fetchImpl, storage = {} } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  installI18n(dom.window);
  const factories = {};
  // magic: i18n.js registers $t on alpine:init, which this loader dispatches.
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, magic: () => {} };
  dom.window.fetch = fetchImpl || (async () => ({ ok: true, status: 200, json: async () => ({}) }));
  for (const [k, v] of Object.entries(storage)) dom.window.localStorage.setItem(k, v);
  const timers = [];
  dom.window.setTimeout = (fn, ms) => { timers.push({ fn, ms }); return timers.length; };
  dom.window.clearTimeout = () => {};
  vm.runInContext(source, dom.getInternalVMContext());
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  return { factories, window: dom.window, timers };
}

const NOW = Date.parse('2026-10-10T12:00:00Z');
const view = (input) => load().window.updateBarView({ now: NOW, acknowledged: '', restarting: false, canInstall: false, status: null, progress: { state: 'idle' }, ...input });

test('idle without an available update is hidden', () => {
  assert.equal(view({}).kind, 'hidden');
});

test('available update with install right offers install and GitHub', () => {
  const v = view({ status: { available: true, latest: 'v0.9.1', current: 'v0.9.0', notes_url: 'https://x' }, canInstall: true });
  assert.equal(v.kind, 'available');
  assert.equal(v.canInstall, true);
  assert.equal(v.notesUrl, 'https://x');
});

test('available update without install right shows the login hint', () => {
  const v = view({ status: { available: true, latest: 'v0.9.1', current: 'v0.9.0' } });
  assert.equal(v.kind, 'available');
  assert.match(v.detail, /Logge dich als Admin ein/);
});

test('running shows step counter, title and fraction', () => {
  const v = view({ progress: { state: 'running', version: 'v0.9.1', total: 8, done: 3, step: { id: '60', title: 'Dashboard und MQTT-Brücke' } } });
  assert.equal(v.kind, 'running');
  assert.equal(v.detail, 'Schritt 4 von 8 · Dashboard und MQTT-Brücke');
  assert.equal(v.percent, 37.5);
});

test('running without a step title still counts', () => {
  const v = view({ progress: { state: 'running', version: 'v0.9.1', total: 2, done: 0, step: { id: '10', title: '' } } });
  assert.equal(v.detail, 'Schritt 1 von 2');
});

test('restarting after a failed poll while running', () => {
  const v = view({ restarting: true, lastProgress: { state: 'running', version: 'v0.9.1', total: 4, done: 2 } });
  assert.equal(v.kind, 'restarting');
  assert.match(v.detail, /startet neu/);
  assert.equal(v.percent, 50);
});

test('ok is shown until acknowledged and hides an available update for the same version', () => {
  const progress = { state: 'ok', version: 'v0.9.1', run_id: 'r1', total: 4, done: 4, finished_at: '2026-10-10T11:00:00Z' };
  const status = { available: true, latest: 'v0.9.1', current: 'v0.9.0' };
  assert.equal(view({ progress, status }).kind, 'ok');
  assert.equal(view({ progress, status, acknowledged: 'r1' }).kind, 'hidden');
});

test('ok older than seven days is not shown', () => {
  const progress = { state: 'ok', version: 'v0.9.1', run_id: 'r1', finished_at: '2026-10-01T11:00:00Z' };
  assert.equal(view({ progress }).kind, 'hidden');
});

test('ack key falls back to finished_at', () => {
  const progress = { state: 'ok', version: 'v0.9.1', finished_at: '2026-10-10T11:00:00Z' };
  assert.equal(view({ progress }).ackKey, '2026-10-10T11:00:00Z');
  assert.equal(view({ progress, acknowledged: '2026-10-10T11:00:00Z' }).kind, 'hidden');
});

test('fail names step, title and code, rejected names the code', () => {
  const fail = view({ progress: { state: 'fail', version: 'v0.9.1', run_id: 'r2', code: 'X_FAILED', step: { id: '20', title: 'MQTT-Broker' } } });
  assert.equal(fail.kind, 'fail');
  assert.equal(fail.detail, 'Schritt 20 · MQTT-Broker · X_FAILED');
  const rejected = view({ progress: { state: 'rejected', version: 'v0.9.1', run_id: 'r3', code: 'BUNDLE_SIGNATURE_INVALID' } });
  assert.equal(rejected.kind, 'fail');
  assert.match(rejected.detail, /abgelehnt/);
});

test('acknowledge stores the key and openWhatsNew navigates to the whatsnew page', async () => {
  const { factories, window } = load();
  const bar = factories.updateBar();
  bar.progress = { state: 'ok', version: 'v0.9.1', run_id: 'r1', finished_at: '2026-10-10T11:00:00Z' };
  let target = '';
  bar.navigate = (url) => { target = url; };
  bar.openWhatsNew();
  assert.equal(window.localStorage.getItem('energy-node.update-bar.acknowledged'), 'r1');
  assert.equal(target, '/?panel=settings&page=whatsnew');
});

test('a failed poll during a run switches to restarting and polls fast', async () => {
  let calls = 0;
  const { factories, timers } = load({
    fetchImpl: async (url) => {
      if (url.endsWith('/api/v1/updates/progress')) {
        calls += 1;
        if (calls === 1) return { ok: true, status: 200, json: async () => ({ state: 'running', version: 'v0.9.1', total: 4, done: 1 }) };
        throw new TypeError('network down');
      }
      return { ok: true, status: 200, json: async () => ({}) };
    },
  });
  const bar = factories.updateBar();
  await bar.poll();
  assert.equal(bar.view.kind, 'running');
  await bar.poll();
  assert.equal(bar.restarting, true);
  assert.equal(bar.view.kind, 'restarting');
  assert.equal(timers.at(-1).ms, 2000);
});

test('a 403 on the update status just means no available state', async () => {
  const { factories } = load({
    fetchImpl: async (url) => {
      if (url.endsWith('/api/v1/updates/status')) return { ok: false, status: 403, json: async () => ({ error: 'check_updates_forbidden' }) };
      if (url.endsWith('/api/v1/updates/progress')) return { ok: true, status: 200, json: async () => ({ state: 'idle' }) };
      return { ok: true, status: 200, json: async () => ({}) };
    },
  });
  const bar = factories.updateBar();
  await bar.init();
  assert.equal(bar.view.kind, 'hidden');
});

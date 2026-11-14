// Tests for openPlace() in onboarding.js: a settings sub page is loaded before
// the tour step's prepare() runs, and a failed load stops the step.
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

// Settings panel with its sidebar, and a settingsPage() stand-in whose
// ensureSubpage() the test controls.
async function setup(ensureSubpage) {
  const dom = new JSDOM(`<!doctype html><html><body>
    <section id="settings-panel"><nav class="settings-side-list"></nav></section>
    </body></html>`, { runScripts: 'outside-only', url: 'https://dashboard.local/' });
  const { window } = dom;
  const panel = { activeSettingsPage: 'general', ensureSubpage };
  const stores = {};
  window.Alpine = {
    magic() {},
    store(name, value) {
      if (value !== undefined) stores[name] = value;
      return stores[name];
    },
    data() {},
    effect() {},
    $data: () => panel,
  };
  window.obSpotlight = { visibleRect: () => ({ width: 1, height: 1 }) };
  window.fetch = async () => ({
    ok: true,
    status: 200,
    json: async () => ({
      role: 'guest',
      setup: { done: true, steps: {}, items: [] },
      user: { seen: [], welcomed: true, run: null, queue: [] },
      explore: { items: [] },
    }),
  });
  installI18n(window);
  vm.runInContext(modelSource, dom.getInternalVMContext());
  vm.runInContext(stepsSource, dom.getInternalVMContext());
  vm.runInContext(source, dom.getInternalVMContext());
  return { window, panel };
}

test('openPlace waits for the subpage before prepare', async () => {
  const order = [];
  let finish;
  const ensureSubpage = (page) => {
    order.push(`ensure:${page}`);
    return new Promise((resolve) => { finish = resolve; });
  };
  const { window, panel } = await setup(ensureSubpage);
  const done = window.obOnboarding.openPlace({
    page: 'mqtt',
    prepare: () => { order.push('prepare'); },
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(order, ['ensure:mqtt']);
  finish();
  assert.equal(await done, true);
  assert.deepEqual(order, ['ensure:mqtt', 'prepare']);
  assert.equal(panel.activeSettingsPage, 'mqtt');
});

test('openPlace reports false when the subpage fails', async () => {
  const { window } = await setup(async () => { throw new Error('load failed'); });
  let prepared = false;
  const placed = await window.obOnboarding.openPlace({
    page: 'mqtt',
    prepare: () => { prepared = true; },
  });
  assert.equal(placed, false);
  assert.equal(prepared, false);
});

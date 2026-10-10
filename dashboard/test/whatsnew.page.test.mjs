// Tests for whatsnew.page.js: the highlights of the last update. Same setup
// as versions.page.test.mjs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'whatsnew.page.js'), 'utf8');

function create(fetchImpl) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'https://dashboard.local/' });
  installI18n(dom.window);
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl;
  vm.runInContext(source, dom.getInternalVMContext());
  return { panel: factories.whatsNewPanel(), window: dom.window };
}
const json = (body, ok = true, status = 200) => ({ ok, status, json: async () => body });

const VIEW = {
  from: 'v0.8.5', to: 'v0.9.0', since_previous: true,
  components: [{ id: 'dashboard', label: 'Dashboard', from: 'v0.8.5', to: 'v0.9.0',
    releases: [{ version: 'v0.9.0', entries: [{ text: 'device map layers', pr: 103, breaking: false }] }] }],
};

test('load fills title, range and components', async () => {
  const { panel } = create(async (url) => {
    assert.equal(url, '/api/v1/changelog/highlights');
    return json(VIEW);
  });
  await panel.load();
  assert.equal(panel.title, 'Das ist neu in v0.9.0');
  assert.equal(panel.sincePrevious, true);
  assert.equal(panel.components.length, 1);
  assert.equal(panel.entryText({ text: 'x', pr: 7 }), 'x (#7)');
});

test('without a previous version the title names the installed version', async () => {
  const { panel } = create(async () => json({ ...VIEW, from: '', since_previous: false }));
  await panel.load();
  assert.equal(panel.title, 'Neuerungen der installierten Version');
});

test('a missing changelog shows the no-changelog text', async () => {
  const { panel } = create(async () => json({ error: 'no_changelog', message: 'x' }, false, 404));
  await panel.load();
  assert.ok(panel.error);
  assert.equal(panel.components.length, 0);
});

test('ensureLoaded requests only once, even after an empty answer', async () => {
  let calls = 0;
  const { panel } = create(async () => { calls += 1; return json({ to: '', components: [] }); });
  panel.ensureLoaded();
  await new Promise((resolve) => setTimeout(resolve, 0));
  panel.ensureLoaded();
  assert.equal(calls, 1);
});

test('showAll switches to the versions page in "all" mode', () => {
  const { panel, window } = create(async () => json(VIEW));
  let heard = false;
  window.addEventListener('versions-show-all', () => { heard = true; });
  panel.activeSettingsPage = 'whatsnew';
  panel.showAll();
  assert.equal(panel.activeSettingsPage, 'versions');
  assert.equal(heard, true);
});

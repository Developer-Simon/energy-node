// learning-settings.js: the usage and "lernt seit" lines, the list of new
// features and the reset action of the learning settings page. Same jsdom
// approach as device-picker.test.mjs and backup.page.test.mjs, run by
// `node --test test/learning-settings.test.mjs` from dashboard/.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';
import { attachStores, fakeModalStore, fakeToastStore } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'learning-settings.js'), 'utf8');

// The catalog keys of settings.learning.* come with the next part. Until then
// this stub carries the German wording the tests expect, with the same
// placeholders the catalog will use.
const TEXT = {
  'settings.learning.size.mb': '{value} MB',
  'settings.learning.size.kb': '{value} kB',
  'settings.learning.usage.stored': 'Belegt {size}',
  'settings.learning.usage.rate': '{size} pro Tag',
  'settings.learning.usage.written': 'Belegt {size}, schreibt {rate}',
  'settings.learning.since.one': 'Lernt seit {days} Tag aus {devices} Geräten',
  'settings.learning.since.other': 'Lernt seit {days} Tagen aus {devices} Geräten',
  'settings.learning.since_none': 'Noch nichts gelernt',
  'settings.learning.reset.confirm_title': 'Gelerntes löschen?',
  'settings.learning.reset.confirm_body': 'Alle gelernten Werte werden gelöscht.',
  'settings.learning.reset.confirm': 'Löschen',
  'settings.learning.reset.done': 'Das Gelernte wurde gelöscht.',
};
const stubT = (key, params = {}) => TEXT[key].replace(/\{(\w+)\}/g, (match, name) => String(params[name]));

function load(dom) {
  installI18n(dom.window);
  dom.window.eval(source);
  return dom.window;
}

test('usageText formats bytes in MB from 1 MB and in kB below, with the German decimal comma', () => {
  const window = load(new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' }));
  const { usageText } = window.LearningSettings;

  const text = usageText({ bytes: 412000, written_per_day: 96000 }, stubT);
  assert.ok(text.includes('0,4 MB'), text);
  assert.ok(text.includes('96 kB pro Tag'), text);

  assert.equal(usageText({ bytes: 1234567 }, stubT), 'Belegt 1,2 MB');
  assert.equal(usageText({ bytes: 18000 }, stubT), 'Belegt 18 kB');
  assert.equal(usageText(null, stubT), 'Belegt 0 kB');
});

test('sinceText counts the days since the first learning and the devices', () => {
  const window = load(new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' }));
  const { sinceText } = window.LearningSettings;
  const since = 1_700_000_000;
  const now = (since + 9 * 86400 + 3600) * 1000;

  assert.equal(sinceText({ on: true, since, devices: 14 }, stubT, now), 'Lernt seit 9 Tagen aus 14 Geräten');
  assert.equal(sinceText({ on: true, since: 0, devices: 0 }, stubT, now), 'Noch nichts gelernt');
  assert.equal(sinceText({ on: true, since, devices: 2 }, stubT, since * 1000 + 86400 * 1000), 'Lernt seit 1 Tag aus 2 Geräten');
});

test('newFeatures leaves out the features that are switched off', () => {
  const window = load(new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' }));
  const { newFeatures } = window.LearningSettings;
  const status = {
    new: ['chain_suggestions', 'chain_learning', 'auto_patterns'],
    features: {
      chain_suggestions: { on: false },
      chain_learning: { on: true },
      auto_patterns: { on: true },
    },
  };

  assert.deepEqual([...newFeatures(status)], ['chain_learning', 'auto_patterns']);
  assert.deepEqual([...newFeatures({ new: [], features: {} })], []);
  assert.deepEqual([...newFeatures(null)], []);
});

// The Alpine factory: status loading and the reset with its confirmation.
function createPage({ routes = {}, answer = true } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'https://dashboard.local/' });
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  const calls = [];
  dom.window.fetch = async (url, options = {}) => {
    const method = options.method || 'GET';
    calls.push({ url, method, headers: options.headers || {} });
    const handler = routes[`${method} ${url}`] || routes[url];
    if (!handler) throw new Error(`unexpected fetch ${method} ${url}`);
    return { ok: true, status: 200, json: async () => handler };
  };
  load(dom);
  const page = factories.learningPage();
  const toasts = fakeToastStore();
  const modal = fakeModalStore({ answer });
  attachStores(page, { toasts, modal });
  return { page, calls, toasts, modal };
}

const statusBody = {
  new: ['auto_patterns'],
  features: { chain_suggestions: { on: true, since: 0, devices: 0 }, chain_learning: { on: true, since: 0, devices: 0 }, auto_patterns: { on: true, since: 0, devices: 0 } },
  usage: { bytes: 412000, written_per_day: 96000, since: 0, devices: 0 },
};

test('load() keeps the status and the CSRF token of the session', async () => {
  const { page } = createPage({
    routes: {
      '/api/v1/learning/status': statusBody,
      '/api/v1/auth/session': { csrf_token: 'tok', roles: [] },
    },
  });
  await page.load();
  assert.equal(page.error, '');
  assert.equal(page.csrfToken, 'tok');
  assert.deepEqual([...page.newKeys], ['auto_patterns']);
});

test('reset() asks first, then posts with the CSRF header and reloads', async () => {
  const { page, calls, toasts, modal } = createPage({
    routes: {
      '/api/v1/learning/status': statusBody,
      '/api/v1/auth/session': { csrf_token: 'tok', roles: [] },
      'POST /api/v1/learning/reset': { ok: true },
    },
  });
  await page.load();
  calls.length = 0;
  assert.equal(await page.reset(), true);
  assert.equal(modal.calls.length, 1);
  assert.equal(modal.calls[0].danger, true);
  const post = calls.find(call => call.method === 'POST');
  assert.equal(post.url, '/api/v1/learning/reset');
  assert.equal(post.headers['X-CSRF-Token'], 'tok');
  assert.deepEqual(toasts.messages, ['Das Gelernte wurde gelöscht.']);
});

test('clearExcluded() empties the excluded devices without asking', () => {
  const { page, calls, modal } = createPage({ routes: {} });
  page.learning_excluded_devices = ['dev-1', 'dev-2'];
  page.clearExcluded();
  assert.deepEqual([...page.learning_excluded_devices], []);
  assert.equal(modal.calls.length, 0);
  assert.equal(calls.length, 0);
});

test('reset() sends nothing when the confirmation is declined', async () => {
  const { page, calls, modal } = createPage({ answer: false, routes: {} });
  assert.equal(await page.reset(), false);
  assert.equal(modal.calls.length, 1);
  assert.equal(calls.length, 0);
});

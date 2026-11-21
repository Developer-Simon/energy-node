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
const settingsFieldSource = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'settings-field.js'), 'utf8');

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
    calls.push({ url, method, headers: options.headers || {}, body: options.body });
    const handler = routes[`${method} ${url}`] || routes[url];
    if (!handler) throw new Error(`unexpected fetch ${method} ${url}`);
    return { ok: true, status: 200, json: async () => handler };
  };
  load(dom);
  dom.window.eval(settingsFieldSource);
  const page = factories.learningPage();
  const toasts = fakeToastStore();
  const modal = fakeModalStore({ answer });
  attachStores(page, { toasts, modal });
  return { page, calls, toasts, modal, window: dom.window };
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

// Hinweis "neu aktiv": Status mit allen drei Funktionen neu, Einrichtung fertig.
const hintStatus = {
  fresh: false,
  setup_done: true,
  new: ['chain_suggestions', 'auto_patterns'],
  acknowledged: ['chain_learning'],
  features: {
    chain_suggestions: { on: true, since: 0, devices: 0 },
    chain_learning: { on: true, since: 0, devices: 0 },
    auto_patterns: { on: true, since: 0, devices: 0 },
  },
  usage: { bytes: 412000, written_per_day: 96000, since: 0, devices: 0 },
};
const hintRoutes = (status = hintStatus, session = { csrf_token: 'tok', edit_setup: true }) => ({
  '/api/v1/learning/status': status,
  '/api/v1/auth/session': session,
  '/api/v1/settings': { learning_acknowledged: ['chain_learning'], chain_suggestions_disabled: false },
  'PUT /api/v1/settings': { ok: true },
});
const putBody = calls => JSON.parse(calls.find(call => call.method === 'PUT').body);

test('"Alle behalten" sends all new keys plus the already acknowledged ones', async () => {
  const { page, calls } = createPage({ routes: hintRoutes() });
  await page.load();
  calls.length = 0;
  await page.acknowledge(page.newKeys);
  const put = calls.find(call => call.method === 'PUT');
  assert.equal(put.headers['X-CSRF-Token'], 'tok');
  assert.deepEqual(putBody(calls).learning_acknowledged, ['chain_learning', 'chain_suggestions', 'auto_patterns']);
});

test('switching a row off acknowledges only that key', async () => {
  const { page, calls } = createPage({ routes: hintRoutes() });
  await page.load();
  calls.length = 0;
  page.switchedOff('auto_patterns', false);
  await new Promise(resolve => setImmediate(resolve));
  assert.deepEqual(putBody(calls).learning_acknowledged, ['chain_learning', 'auto_patterns']);
});

test('switching a row on does not acknowledge anything', async () => {
  const { page, calls } = createPage({ routes: hintRoutes() });
  await page.load();
  calls.length = 0;
  page.switchedOff('auto_patterns', true);
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(calls.filter(call => call.method === 'PUT').length, 0);
});

test('the hint shows only with setup done, edit_setup and features that are new', async () => {
  const done = createPage({ routes: hintRoutes() });
  await done.page.load();
  assert.equal(done.page.newHintVisible, true);

  const notSetUp = createPage({ routes: hintRoutes({ ...hintStatus, setup_done: false }) });
  await notSetUp.page.load();
  assert.equal(notSetUp.page.newHintVisible, false);

  const noEdit = createPage({ routes: hintRoutes(hintStatus, { csrf_token: 'tok', edit_setup: false }) });
  await noEdit.page.load();
  assert.equal(noEdit.page.newHintVisible, false);

  const empty = createPage({ routes: hintRoutes({ ...hintStatus, new: [] }) });
  await empty.page.load();
  assert.equal(empty.page.newHintVisible, false);
});

test('fresh picks the fresh text, an update the update text', async () => {
  const update = createPage({ routes: hintRoutes({ ...hintStatus, new: ['auto_patterns'] }) });
  await update.page.load();
  assert.equal(update.page.newHintTitle, 'Eine Lernfunktion ist neu aktiv');
  assert.ok(update.page.newHintBody.startsWith('Sie sind mit dem letzten Update dazugekommen. '), update.page.newHintBody);

  const fresh = createPage({ routes: hintRoutes({ ...hintStatus, fresh: true, new: ['auto_patterns', 'chain_suggestions'] }) });
  await fresh.page.load();
  assert.equal(fresh.page.newHintTitle, 'Lernfunktionen sind aktiv');
  assert.ok(fresh.page.newHintBody.startsWith('Die Node lernt von Anfang an aus deinen Geräten. '), fresh.page.newHintBody);
});

test('the pill marks only the new features that are still on', async () => {
  const { page } = createPage({ routes: hintRoutes() });
  await page.load();
  assert.equal(page.isNew('chain_suggestions'), true);
  assert.equal(page.isNew('chain_learning'), false);
  assert.equal(page.isNew('auto_patterns'), true);
});

// Aussentemperatur: Einzelwahl unter den Temperatur-Entitaeten.
const outdoorDevices = [
  { id: 'wetter', name: 'Wetter', entities: [
    { unique_id: 'temp-1', name: 'Außen', device_class: 'temperature' },
    { unique_id: 'hum-1', name: 'Feuchte', device_class: 'humidity' },
  ] },
  { id: 'heizung', name: 'Heizung', entities: [
    { unique_id: 'temp-2', name: 'Vorlauf', device_class: 'temperature' },
  ] },
];
const outdoorRoutes = (outdoorEntity) => ({
  '/api/v1/learning/status': statusBody,
  '/api/v1/auth/session': { csrf_token: 'tok', edit_setup: true, roles: [] },
  '/api/v1/settings': { automation_outdoor_entity: outdoorEntity, learning_acknowledged: [] },
  '/api/v1/devices': outdoorDevices,
  'PUT /api/v1/settings': { ok: true },
});

test('the outdoor picker offers only temperature entities, in single choice', async () => {
  const { page } = createPage({ routes: outdoorRoutes('') });
  await page.load();
  let captured = null;
  page.openPicker = async (options) => { captured = options; return null; };
  await page.chooseOutdoor();
  assert.equal(captured.mode, 'single');
  assert.deepEqual(captured.items.map(item => item.id), ['temp-1', 'temp-2']);
  assert.deepEqual(captured.items.map(item => item.section), ['Wetter', 'Heizung']);
  assert.deepEqual([...captured.selected], []);
});

test('a chosen outdoor entity is saved with the CSRF header and shown by device and entity name', async () => {
  const { page, calls } = createPage({ routes: outdoorRoutes('') });
  await page.load();
  assert.equal(page.outdoorLabel(), 'Keine');
  page.openPicker = async () => 'temp-1';
  calls.length = 0;
  await page.chooseOutdoor();
  await new Promise(resolve => setImmediate(resolve));
  const put = calls.find(call => call.method === 'PUT');
  assert.equal(put.headers['X-CSRF-Token'], 'tok');
  assert.equal(JSON.parse(put.body).automation_outdoor_entity, 'temp-1');
  assert.equal(page.automation_outdoor_entity, 'temp-1');
  assert.equal(page.outdoorLabel(), 'Wetter, Außen');
});

test('a saved outdoor entity that is gone from the registry shows "nicht mehr vorhanden"', async () => {
  const { page } = createPage({ routes: outdoorRoutes('gone-1') });
  await page.load();
  assert.equal(page.automation_outdoor_entity, 'gone-1');
  assert.equal(page.outdoorLabel(), 'nicht mehr vorhanden');
});

// The location hint: visible while no location is saved, gone with it. The
// state comes from status.location_set, the settings only carry the outdoor entity.
const statusWithLocation = (location_set) => ({ ...statusBody, location_set });
const settingsRoutes = (status, settings = { learning_level: 'auto' }) => ({
  '/api/v1/learning/status': status,
  '/api/v1/auth/session': { csrf_token: 'tok', roles: [] },
  '/api/v1/settings': settings,
});

test('the location hint shows when the status reports no location and goes away with one', async () => {
  const missing = createPage({ routes: settingsRoutes(statusWithLocation(false)) });
  await missing.page.load();
  assert.equal(missing.page.locationMissing, true);

  const set = createPage({ routes: settingsRoutes(statusWithLocation(true)) });
  await set.page.load();
  assert.equal(set.page.locationMissing, false);
});

test('the location hint ignores latitude and longitude in the settings', async () => {
  const { page } = createPage({ routes: settingsRoutes(statusWithLocation(true), { latitude: 52.5, longitude: 13.4 }) });
  await page.load();
  assert.equal(page.locationMissing, false);
});

test('the location hint stays hidden when the status has no location_set field', async () => {
  const { page } = createPage({ routes: settingsRoutes(statusBody) });
  await page.load();
  assert.equal(page.locationMissing, false);
});

test('the location action opens the automations panel', () => {
  const { page, window } = createPage();
  const seen = [];
  const listener = (event) => seen.push(event.detail);
  window.addEventListener('dashboard-open-panel', listener);
  page.openLocationSettings();
  assert.equal(seen.length, 1);
  assert.equal(seen[0].panel, 'automations-panel');
});

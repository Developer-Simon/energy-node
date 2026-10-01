// The masthead's update-available pill (dashboard.js: updateBadge()). It
// only ever reads the cached /api/v1/updates/status result -- the actual
// GitHub check runs server-side (internal/updatecheck), once a day in the
// background or on demand from the settings page. Same alpine:init +
// vm.runInContext technique as dashboard.overview.test.mjs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');
const source = read('dashboard.js');

function load({ fetchImpl } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, directive: () => {} };
  const intervals = [];
  dom.window.setInterval = () => intervals.push(1);
  dom.window.fetch = fetchImpl || (async () => ({ ok: true, status: 200, json: async () => ({}) }));
  Object.defineProperty(dom.window.document, 'visibilityState', { value: 'visible', writable: true });
  vm.runInContext(source, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  return { factories, window: dom.window };
}

const jsonResponse = body => ({ ok: true, status: 200, json: async () => body });

test('updateBadge init() zeigt ein verfuegbares Update aus dem zwischengespeicherten Stand', async () => {
  const { factories, window } = load({
    fetchImpl: async () => jsonResponse({ available: true, latest: '1.4.2', notes_url: 'https://example.invalid/release' }),
  });
  const component = factories.updateBadge();
  await component.init();
  assert.equal(component.available, true);
  assert.equal(component.latest, '1.4.2');
  assert.equal(component.notesUrl, 'https://example.invalid/release');
  window.close();
});

test('updateBadge init() bleibt unsichtbar, wenn noch nie geprueft wurde', async () => {
  const { factories, window } = load({
    fetchImpl: async () => jsonResponse({ checked: false }),
  });
  const component = factories.updateBadge();
  await component.init();
  assert.equal(component.available, false);
  window.close();
});

test('updateBadge init() schluckt einen Fehler ohne die Seite zu stoeren', async () => {
  const { factories, window } = load({
    fetchImpl: async () => ({ ok: false, status: 500, json: async () => ({ message: 'kaputt' }) }),
  });
  const component = factories.updateBadge();
  await assert.doesNotReject(() => component.init());
  assert.equal(component.available, false);
  window.close();
});

test('updateBadge init() fragt bei einem zweiten Aufruf nicht erneut nach (x-init + Alpine-Hook)', async () => {
  const urls = [];
  const { factories, window } = load({
    fetchImpl: async (url) => { urls.push(url); return jsonResponse({ available: true, latest: '1.4.2' }); },
  });
  const component = factories.updateBadge();
  await component.init();
  await component.init();
  assert.deepEqual(urls, ['/api/v1/updates/status', '/api/v1/auth/session'], 'einmal Status, einmal Sitzung - der zweite init() fragt nichts');
  window.close();
});

const sessionAware = (session, status = { available: true, latest: '1.4.2', notes_url: 'https://example.invalid/release' }) =>
  async (url) => {
    if (url.endsWith('/api/v1/updates/status')) return jsonResponse(status);
    if (url.endsWith('/api/v1/auth/session')) return session instanceof Error ? Promise.reject(session) : jsonResponse(session);
    throw new Error(`unexpected fetch ${url}`);
  };

test('wer Systemaktionen darf, wird von der Pille in den Aktualisieren-Bildschirm gefuehrt', async () => {
  const { factories, window } = load({ fetchImpl: sessionAware({ system_actions: true }) });
  const component = factories.updateBadge();
  await component.init();
  assert.equal(component.href, '/redeploy/');
  assert.equal(component.external, false, 'kein neuer Tab fuer eine Seite des Dashboards');
  window.close();
});

test('die Pille beachtet einen Unterpfad hinter dem Reverse-Proxy', async () => {
  const { factories, window } = load({ fetchImpl: sessionAware({ system_actions: true }) });
  window.__DASHBOARD_BASE_PATH__ = '/node';
  const component = factories.updateBadge();
  await component.init();
  assert.equal(component.href, '/node/redeploy/');
  window.close();
});

test('ein Gast behaelt den GitHub-Link in einem neuen Tab', async () => {
  const { factories, window } = load({ fetchImpl: sessionAware({ system_actions: false }) });
  const component = factories.updateBadge();
  await component.init();
  assert.equal(component.href, 'https://example.invalid/release');
  assert.equal(component.external, true);
  window.close();
});

test('faellt die Sitzungsabfrage aus, bleibt der GitHub-Link', async () => {
  const { factories, window } = load({ fetchImpl: sessionAware(new Error('offline')) });
  const component = factories.updateBadge();
  await assert.doesNotReject(() => component.init());
  assert.equal(component.href, 'https://example.invalid/release');
  window.close();
});

test('ohne verfuegbares Update fragt die Pille die Sitzung gar nicht erst ab', async () => {
  const urls = [];
  const { factories, window } = load({
    fetchImpl: async (url) => { urls.push(url); return jsonResponse({ available: false, latest: '1.4.1' }); },
  });
  const component = factories.updateBadge();
  await component.init();
  assert.deepEqual(urls, ['/api/v1/updates/status']);
  window.close();
});

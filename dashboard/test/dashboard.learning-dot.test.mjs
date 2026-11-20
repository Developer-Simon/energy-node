// Der Punkt am Reiter Einstellungen in der Huelle: sichtbar, solange neu aktive
// Lernfunktionen auf eine Quittung warten. Nur mit edit_setup und fertiger
// Einrichtung. Die Logik steckt in dashboardShell(), der Test laeuft ohne Layout,
// wie dashboard.energy-dot.test.mjs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'dashboard.js'),
  'utf8',
);

const TABS = '<button id="tab-settings" class="tab" role="tab" aria-controls="settings-panel">Einstellungen</button>';

// routes maps a URL to {status, body}; a missing route answers 404.
function createShell({ navHtml = TABS, routes = {} } = {}) {
  const dom = new JSDOM(
    `<!doctype html><html><body><div class="nav-shell"><nav id="dashboard-tabs">${navHtml}</nav></div>`
      + '<section id="settings-panel" class="panel"></section></body></html>',
    { runScripts: 'outside-only', url: 'http://localhost/' },
  );
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, directive: () => {} };
  dom.window.setInterval = () => 0;
  const requests = [];
  dom.window.fetch = async (url) => {
    requests.push(String(url));
    const route = routes[String(url)];
    if (route instanceof Error) throw route;
    const { status, body } = route || { status: 404, body: {} };
    return { ok: status < 400, status, json: async () => body, text: async () => JSON.stringify(body) };
  };
  vm.runInContext(source, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  const component = factories.dashboardShell();
  component.$nextTick = (fn) => fn();
  component.loadPanel = () => {};
  component.syncLiveTopics = () => {};
  component.fitNav = () => {};
  return { component, window: dom.window, requests };
}

const settle = () => new Promise(resolve => setImmediate(resolve));
const EDIT = { status: 200, body: { csrf_token: 'tok', edit_setup: true } };
const NO_EDIT = { status: 200, body: { csrf_token: 'tok', edit_setup: false } };
const status = (overrides = {}) => ({
  status: 200,
  body: {
    fresh: false,
    setup_done: true,
    new: ['auto_patterns'],
    acknowledged: [],
    features: { chain_suggestions: { on: true }, chain_learning: { on: true }, auto_patterns: { on: true } },
    ...overrides,
  },
});

test('eine neu aktive Funktion zeigt den Punkt am Einstellungen-Reiter', async () => {
  const { component, requests } = createShell({ routes: { '/api/v1/auth/session': EDIT, '/api/v1/learning/status': status() } });
  await component.refreshLearningNew();
  assert.deepEqual(requests, ['/api/v1/auth/session', '/api/v1/learning/status']);
  assert.equal(component.learningNewOpen, true);
});

test('ohne abgeschlossene Einrichtung bleibt der Punkt aus', async () => {
  const { component } = createShell({ routes: { '/api/v1/auth/session': EDIT, '/api/v1/learning/status': status({ setup_done: false }) } });
  await component.refreshLearningNew();
  assert.equal(component.learningNewOpen, false);
});

test('ohne edit_setup gibt es keine Statusanfrage und keinen Punkt', async () => {
  const { component, requests } = createShell({ routes: { '/api/v1/auth/session': NO_EDIT, '/api/v1/learning/status': status() } });
  await component.refreshLearningNew();
  assert.deepEqual(requests, ['/api/v1/auth/session']);
  assert.equal(component.learningNewOpen, false);
});

test('ohne neue Funktionen bleibt der Punkt aus', async () => {
  const { component } = createShell({ routes: { '/api/v1/auth/session': EDIT, '/api/v1/learning/status': status({ new: [] }) } });
  await component.refreshLearningNew();
  assert.equal(component.learningNewOpen, false);
});

test('eine abgeschaltete neue Funktion zaehlt nicht', async () => {
  const { component } = createShell({ routes: { '/api/v1/auth/session': EDIT, '/api/v1/learning/status': status({ features: { auto_patterns: { on: false } } }) } });
  await component.refreshLearningNew();
  assert.equal(component.learningNewOpen, false);
});

test('eine abgelehnte Statusanfrage (403 ohne view_automations) laesst den Punkt stehen', async () => {
  const { component } = createShell({ routes: {
    '/api/v1/auth/session': EDIT,
    '/api/v1/learning/status': { status: 403, body: { message: 'forbidden' } },
  } });
  await assert.doesNotReject(component.refreshLearningNew());
  assert.equal(component.learningNewOpen, false);
});

test('ein Netzfehler laesst den bisherigen Wert stehen', async () => {
  const routes = { '/api/v1/auth/session': EDIT };
  const { component } = createShell({ routes });
  routes['/api/v1/learning/status'] = status();
  await component.refreshLearningNew();
  assert.equal(component.learningNewOpen, true);
  routes['/api/v1/learning/status'] = new Error('network down');
  await assert.doesNotReject(component.refreshLearningNew());
  assert.equal(component.learningNewOpen, true);
});

test('ohne Einstellungen-Reiter gibt es keine Anfrage', async () => {
  const { component, requests } = createShell({ navHtml: '<button id="tab-energy" class="tab" role="tab" aria-controls="energy-panel">Energie</button>' });
  await component.refreshLearningNew();
  component.init();
  await settle();
  assert.deepEqual(requests.filter(url => url.includes('learning')), []);
  assert.equal(component.learningNewOpen, false);
});

test('das Ereignis learning-acknowledged aktualisiert den Punkt', async () => {
  const routes = { '/api/v1/auth/session': EDIT, '/api/v1/learning/status': status() };
  const { component, window } = createShell({ routes });
  component.init();
  await settle();
  assert.equal(component.learningNewOpen, true);
  routes['/api/v1/learning/status'] = status({ new: [] });
  window.dispatchEvent(new window.CustomEvent('learning-acknowledged', { detail: { ids: ['auto_patterns'] } }));
  await settle();
  assert.equal(component.learningNewOpen, false);
});

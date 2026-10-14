// PR A der SSE-Themen: die Huelle liest ihren Bedarf aus dem DOM des aktiven
// Panels und oeffnet die EventSource mit ?topics=. Sie verbindet nur neu,
// wenn sich der Bedarf wirklich aendert.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'dashboard.js'), 'utf8');

const grid = kinds => `<section id="overview-panel" class="panel active"><div id="overview-live">${
  kinds.map((kind, i) => `<div class="layout-grid-item" data-layout-item-id="i${i}" data-layout-item-kind="${kind}"></div>`).join('')
}</div></section>`;

function load(html) {
  const dom = new JSDOM(`<!doctype html><html><body>${html}</body></html>`, { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, directive: () => {} };
  dom.window.setInterval = () => 1;
  dom.window.fetch = async () => ({ ok: true, status: 200, json: async () => ({}), text: async () => '' });
  const sources = [];
  dom.window.EventSource = class {
    constructor(url) { this.url = url; this.closed = false; this.listeners = {}; sources.push(this); }
    addEventListener(type, fn) { this.listeners[type] = fn; }
    close() { this.closed = true; }
  };
  installI18n(dom.window);
  vm.runInContext(source, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  return { factories, window: dom.window, sources };
}

test('liveTopicsFor leitet die Themen aus den Kartentypen ab', () => {
  const { factories, window } = load(grid(['energy_flow', 'entity_value', 'diagnostics', 'history_view']));
  const topicsFor = factories.dashboardShell.liveTopicsFor;
  assert.equal(topicsFor(window.document.getElementById('overview-panel'), 'overview-panel'), 'diagnostics,energy,entities');
});

test('eine Geraetekachel braucht entities und structure', () => {
  const { factories, window } = load(grid(['device']));
  const topicsFor = factories.dashboardShell.liveTopicsFor;
  assert.equal(topicsFor(window.document.getElementById('overview-panel'), 'overview-panel'), 'entities,structure');
});

test('der Geraete-Tab braucht entities und structure, andere Panels nichts', () => {
  const { factories, window } = load('<section id="devices-panel"><div id="devices-live"></div></section><section id="automations-panel"></section>');
  const topicsFor = factories.dashboardShell.liveTopicsFor;
  assert.equal(topicsFor(window.document.getElementById('devices-panel'), 'devices-panel'), 'entities,structure');
  assert.equal(topicsFor(window.document.getElementById('automations-panel'), 'automations-panel'), '');
  assert.equal(topicsFor(null, 'history-panel'), '');
});

test('init oeffnet den Strom mit den Themen des aktiven Panels', () => {
  const { factories, sources } = load(grid(['energy_flow']));
  const shell = factories.dashboardShell();
  shell.init();
  assert.equal(sources.length, 1);
  assert.equal(sources[0].url, '/api/v1/events?topics=energy');
});

test('gleicher Bedarf nach einem Fragment-Tausch verbindet nicht neu', () => {
  const { factories, sources } = load(grid(['energy_flow']));
  const shell = factories.dashboardShell();
  shell.init();
  shell.syncLiveTopics();
  shell.syncLiveTopics();
  assert.equal(sources.length, 1);
  assert.equal(sources[0].closed, false);
});

test('geaenderter Bedarf schliesst den alten Strom und oeffnet einen neuen', () => {
  const { factories, window, sources } = load(grid(['energy_flow']) + '<section id="automations-panel"></section>');
  const shell = factories.dashboardShell();
  shell.init();
  shell.activePanel = 'automations-panel';
  shell.syncLiveTopics();
  assert.equal(sources.length, 2);
  assert.equal(sources[0].closed, true);
  assert.equal(sources[1].url, '/api/v1/events?topics=');
  assert.equal(shell.liveTopics, '');
});

test('ein Ereignis des neuen Stroms erreicht registry-updated', () => {
  const { factories, window, sources } = load(grid(['energy_flow']));
  const shell = factories.dashboardShell();
  shell.init();
  let received = null;
  window.addEventListener('registry-updated', event => { received = event.detail; });
  sources[0].listeners.registry({ data: '{"version":3}' });
  assert.deepEqual(JSON.parse(JSON.stringify(received)), { version: 3 });
});

test('nach einem Fehler bleibt der 30-Sekunden-Rueckfall und kein neuer Strom', () => {
  const { factories, sources } = load(grid(['energy_flow']));
  const shell = factories.dashboardShell();
  shell.init();
  sources[0].onerror();
  shell.syncLiveTopics();
  assert.equal(sources.length, 1);
  assert.notEqual(shell.liveFallback, null);
});

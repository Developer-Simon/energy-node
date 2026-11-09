// Der Punkt am Reiter Energie in der Huelle: sichtbar, solange Messketten-
// Vorschlaege offen sind. Die Logik steckt in dashboardShell(); der Test laeuft
// ohne Layout, wie dashboard.efficiency-tab.test.mjs.
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

const TABS = `
  <button id="tab-energy" class="tab" role="tab" aria-controls="energy-panel">Energie</button>
  <button id="tab-settings" class="tab" role="tab" aria-controls="settings-panel">Einstellungen</button>
`;

// respond(url) returns {status, body} or throws to simulate a network failure.
function createShell({ navHtml = TABS, respond = () => ({ status: 200, body: { suggestions: [] } }) } = {}) {
  const dom = new JSDOM(
    `<!doctype html><html><body><div class="nav-shell"><nav id="dashboard-tabs">${navHtml}</nav></div>`
      + '<section id="energy-panel" class="panel"></section></body></html>',
    { runScripts: 'outside-only', url: 'http://localhost/' },
  );
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, directive: () => {} };
  const intervals = [];
  dom.window.setInterval = (fn, ms) => { intervals.push(ms); return intervals.length; };
  const requests = [];
  dom.window.fetch = async (url) => {
    requests.push(String(url));
    const { status, body } = respond(String(url));
    return { ok: status < 400, status, json: async () => body, text: async () => JSON.stringify(body) };
  };
  vm.runInContext(source, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  const component = factories.dashboardShell();
  component.$nextTick = (fn) => fn();
  component.loadPanel = () => {};
  component.syncLiveTopics = () => {};
  component.fitNav = () => {};
  return { component, window: dom.window, requests, intervals };
}

const settle = () => new Promise(resolve => setImmediate(resolve));
const SUGGESTION = { a: 'pv_power', b: 'socket_power', r: 0.976, days: 5, minutes: 900, mean_a_w: 400, mean_b_w: 380, edge: false };

test('ein offener Vorschlag zeigt den Punkt am Energie-Reiter', async () => {
  const { component, requests } = createShell({ respond: () => ({ status: 200, body: { suggestions: [SUGGESTION] } }) });
  await component.refreshEnergySuggestions();
  assert.deepEqual(requests, ['/api/v1/energy/chain-suggestions']);
  assert.equal(component.energySuggestionsOpen, true);
});

test('keine offenen Vorschlaege lassen den Punkt aus', async () => {
  const { component } = createShell({ respond: () => ({ status: 200, body: { suggestions: [] } }) });
  await component.refreshEnergySuggestions();
  assert.equal(component.energySuggestionsOpen, false);
});

test('abgeschaltete Vorschlaege zeigen keinen Punkt, auch mit einer Liste', async () => {
  const empty = createShell({ respond: () => ({ status: 200, body: { suggestions: [], disabled: true } }) });
  await empty.component.refreshEnergySuggestions();
  assert.equal(empty.component.energySuggestionsOpen, false);
  const listed = createShell({ respond: () => ({ status: 200, body: { suggestions: [SUGGESTION], disabled: true } }) });
  await listed.component.refreshEnergySuggestions();
  assert.equal(listed.component.energySuggestionsOpen, false);
});

test('das Ereignis nach Verwerfen oder Uebernehmen loescht den Punkt, wenn nichts mehr offen ist', async () => {
  let suggestions = [SUGGESTION];
  const { component, window } = createShell({ respond: () => ({ status: 200, body: { suggestions } }) });
  component.init();
  await settle();
  assert.equal(component.energySuggestionsOpen, true);
  suggestions = [];
  window.dispatchEvent(new window.CustomEvent('energy-chain-suggestions-changed'));
  await settle();
  assert.equal(component.energySuggestionsOpen, false);
});

test('eine fehlgeschlagene Anfrage laesst den bisherigen Wert stehen', async () => {
  let fail = false;
  const { component } = createShell({ respond: () => {
    if (fail) throw new Error('network down');
    return { status: 200, body: { suggestions: [SUGGESTION] } };
  } });
  await component.refreshEnergySuggestions();
  assert.equal(component.energySuggestionsOpen, true);
  fail = true;
  await assert.doesNotReject(component.refreshEnergySuggestions());
  assert.equal(component.energySuggestionsOpen, true);
});

test('ohne Energie-Reiter gibt es keine Anfrage und kein Intervall', async () => {
  const { component, window, requests, intervals } = createShell({ navHtml: '<button id="tab-settings" class="tab" role="tab" aria-controls="settings-panel">Einstellungen</button>' });
  await component.refreshEnergySuggestions();
  component.init();
  window.dispatchEvent(new window.CustomEvent('energy-roles-changed'));
  window.dispatchEvent(new window.CustomEvent('energy-chain-suggestions-changed'));
  await settle();
  assert.deepEqual(requests, []);
  assert.deepEqual(intervals.filter(ms => ms === 5 * 60 * 1000), []);
  assert.equal(component.energySuggestionsOpen, false);
});

test('mit Energie-Reiter laeuft die Abfrage alle fuenf Minuten', () => {
  const { component, intervals } = createShell();
  component.init();
  assert.ok(intervals.includes(5 * 60 * 1000));
});

test('eine Rollenaenderung aktualisiert den Punkt', async () => {
  let suggestions = [SUGGESTION];
  const { component, window } = createShell({ respond: () => ({ status: 200, body: { suggestions } }) });
  component.init();
  await settle();
  suggestions = [];
  window.dispatchEvent(new window.CustomEvent('energy-roles-changed'));
  await settle();
  assert.equal(component.energySuggestionsOpen, false);
});

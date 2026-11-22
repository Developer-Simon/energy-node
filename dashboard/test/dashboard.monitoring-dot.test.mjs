// Der Punkt am Reiter Monitoring in der Huelle: sichtbar, solange neue Meldungen
// ohne Quittung offen sind. Die Logik steckt in dashboardShell(); der Test laeuft
// ohne Layout, wie dashboard.automation-dot.test.mjs.
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
  <button id="tab-monitoring" class="tab" role="tab" aria-controls="monitoring-panel">Monitoring</button>
  <button id="tab-settings" class="tab" role="tab" aria-controls="settings-panel">Einstellungen</button>
`;
const MONITORING = '/api/v1/monitoring';

// respond(url) returns {status, body} or throws to simulate a network failure.
function createShell({ navHtml = TABS, respond = () => ({ status: 200, body: { new_count: 0, notices: [] } }) } = {}) {
  const dom = new JSDOM(
    `<!doctype html><html><body><div class="nav-shell"><nav id="dashboard-tabs">${navHtml}</nav></div>`
      + '<section id="monitoring-panel" class="panel"></section></body></html>',
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

test('neue Meldungen zeigen den Punkt am Monitoring-Reiter', async () => {
  const { component, requests } = createShell({ respond: () => ({ status: 200, body: { new_count: 2, notices: [] } }) });
  await component.refreshMonitoring();
  assert.deepEqual(requests, [MONITORING]);
  assert.equal(component.monitoringNew, true);
});

test('keine neuen Meldungen lassen den Punkt aus', async () => {
  const { component } = createShell({ respond: () => ({ status: 200, body: { new_count: 0, notices: [] } }) });
  await component.refreshMonitoring();
  assert.equal(component.monitoringNew, false);
});

test('ein Netzfehler laesst den bisherigen Wert stehen', async () => {
  let fail = false;
  const { component } = createShell({ respond: () => {
    if (fail) throw new Error('network down');
    return { status: 200, body: { new_count: 1, notices: [] } };
  } });
  await component.refreshMonitoring();
  assert.equal(component.monitoringNew, true);
  fail = true;
  await assert.doesNotReject(component.refreshMonitoring());
  assert.equal(component.monitoringNew, true);
});

test('ohne Monitoring-Reiter gibt es keine Anfrage und kein Intervall', async () => {
  const { component, window, requests, intervals } = createShell({ navHtml: '<button id="tab-settings" class="tab" role="tab" aria-controls="settings-panel">Einstellungen</button>' });
  await component.refreshMonitoring();
  component.init();
  window.dispatchEvent(new window.CustomEvent('monitoring-changed'));
  await settle();
  assert.deepEqual(requests.filter(url => url.includes('/monitoring')), []);
  assert.deepEqual(intervals.filter(ms => ms === 5 * 60 * 1000), []);
  assert.equal(component.monitoringNew, false);
});

test('mit Monitoring-Reiter laeuft die Abfrage alle fuenf Minuten und beim Start', async () => {
  const { component, requests, intervals } = createShell();
  component.init();
  await settle();
  assert.ok(requests.includes(MONITORING));
  assert.ok(intervals.includes(5 * 60 * 1000));
});

test('das Ereignis monitoring-changed aktualisiert den Punkt', async () => {
  let newCount = 1;
  const { component, window } = createShell({ respond: () => ({ status: 200, body: { new_count: newCount, notices: [] } }) });
  component.init();
  await settle();
  assert.equal(component.monitoringNew, true);
  newCount = 0;
  window.dispatchEvent(new window.CustomEvent('monitoring-changed'));
  await settle();
  assert.equal(component.monitoringNew, false);
});

test('monitoring-open merkt den Filter und oeffnet den Reiter', async () => {
  const { component, window } = createShell();
  component.init();
  await settle();
  window.dispatchEvent(new window.CustomEvent('monitoring-open', { detail: { filter: 'energy' } }));
  await settle();
  assert.deepEqual(JSON.parse(JSON.stringify(window.__monitoringOpenRequest__)), { filter: 'energy' });
  assert.equal(component.activePanel, 'monitoring-panel');
});

// Der Reiter Effizienz in der Huelle: sichtbar nur mit Messketten, und er
// folgt den Ketten, sobald sich die Rollen aendern. Die Logik steckt in
// dashboardShell(); der Test laeuft ohne Layout, wie dashboard.nav.test.mjs.
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
  <button id="tab-efficiency" class="tab" role="tab" aria-controls="efficiency-panel">Effizienz</button>
  <button id="tab-settings" class="tab" role="tab" aria-controls="settings-panel">Einstellungen</button>
`;

function createShell({ navHtml = TABS, bodyAttrs = '', roles = null } = {}) {
  const dom = new JSDOM(
    `<!doctype html><html><body ${bodyAttrs}><div class="nav-shell"><nav id="dashboard-tabs">${navHtml}</nav></div>`
      + '<section id="energy-panel" class="panel"></section><section id="efficiency-panel" class="panel"></section></body></html>',
    { runScripts: 'outside-only', url: 'http://localhost/' },
  );
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, directive: () => {} };
  dom.window.setInterval = () => 0;
  const requests = [];
  dom.window.fetch = async (url) => {
    requests.push(String(url));
    const body = roles ?? {};
    return { ok: true, status: 200, json: async () => body, text: async () => '' };
  };
  vm.runInContext(source, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  const component = factories.dashboardShell();
  component.$nextTick = (fn) => fn();
  component.loadPanel = () => {};
  component.syncLiveTopics = () => {};
  return { component, window: dom.window, requests };
}

const settle = () => new Promise(resolve => setImmediate(resolve));

test('hasChains startet aus data-has-chains des Bodys', () => {
  assert.equal(createShell({ bodyAttrs: 'data-has-chains="true"' }).component.hasChains, true);
  assert.equal(createShell({ bodyAttrs: 'data-has-chains="false"' }).component.hasChains, false);
});

test('der Reiter folgt den Ketten nach einer Rollenaenderung', async () => {
  const { component, window, requests } = createShell({
    bodyAttrs: 'data-has-chains="false"',
    roles: { chains: { a: { label: 'Steckdose', points: ['p', 'q'], counting: 'p' } } },
  });
  let fitCalls = 0;
  component.fitNav = () => { fitCalls += 1; };
  component.init();
  window.dispatchEvent(new window.CustomEvent('energy-roles-changed'));
  await settle();
  assert.deepEqual(requests, ['/api/v1/energy/roles']);
  assert.equal(component.hasChains, true);
  assert.equal(fitCalls, 1);
});

test('die letzte Kette loeschen schliesst den offenen Reiter Effizienz', async () => {
  const { component, window } = createShell({
    bodyAttrs: 'data-has-chains="true" data-initial-panel="efficiency-panel"',
    roles: { chains: {} },
  });
  const opened = [];
  component.setActivePanel = async (panel) => { opened.push(panel); };
  component.init();
  assert.equal(component.activePanel, 'efficiency-panel');
  window.dispatchEvent(new window.CustomEvent('energy-roles-changed'));
  await settle();
  assert.equal(component.hasChains, false);
  assert.deepEqual(opened, ['energy-panel']);
});

test('ohne den Reiter loest eine Rollenaenderung keine Anfrage aus', async () => {
  const { component, window, requests } = createShell({
    navHtml: '<button id="tab-energy" class="tab" role="tab" aria-controls="energy-panel">Energie</button>',
  });
  component.init();
  window.dispatchEvent(new window.CustomEvent('energy-roles-changed'));
  await settle();
  assert.deepEqual(requests, []);
});

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

function createShell({ navHtml = TABS, bodyAttrs = '', roles = null, beforeLoad = null } = {}) {
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
  if (beforeLoad) beforeLoad(dom.window);
  vm.runInContext(source, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  // Eine neue Huelle auf demselben Fenster simuliert das Neuladen der Seite.
  const mount = () => {
    const shell = factories.dashboardShell();
    shell.$nextTick = (fn) => fn();
    shell.loadPanel = () => {};
    shell.syncLiveTopics = () => {};
    return shell;
  };
  const component = mount();
  return { component, window: dom.window, requests, mount };
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
  // Der Energie-Punkt fragt die Vorschlaege beim Start ab, hier zaehlt nur die Rollen-Anfrage.
  assert.deepEqual(requests.filter(url => url === '/api/v1/energy/roles'), ['/api/v1/energy/roles']);
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
  assert.deepEqual(requests.filter(url => url === '/api/v1/energy/roles'), []);
});

test('der Punkt am Effizienz-Reiter zeigt sich, solange er nie geoeffnet wurde', () => {
  const { component } = createShell({ bodyAttrs: 'data-has-chains="true"' });
  assert.equal(component.efficiencyTabNew, true);
});

test('der Punkt erlischt beim Betreten und bleibt nach dem Neuladen weg', () => {
  const { component, window, mount } = createShell({ bodyAttrs: 'data-has-chains="true"' });
  component.setActivePanel('efficiency-panel');
  assert.equal(component.efficiencyTabNew, false);
  assert.equal(window.localStorage.getItem('efficiency.tab_seen'), '1');
  assert.equal(mount().efficiencyTabNew, false);
});

test('ein anderer Reiter laesst den Punkt stehen', () => {
  const { component } = createShell({ bodyAttrs: 'data-has-chains="true"' });
  component.setActivePanel('energy-panel');
  assert.equal(component.efficiencyTabNew, true);
});

test('ein geoeffneter Effizienz-Reiter beim Laden raeumt den Punkt ab', () => {
  const { component, window } = createShell({
    bodyAttrs: 'data-has-chains="true" data-initial-panel="efficiency-panel"',
  });
  component.init();
  assert.equal(component.efficiencyTabNew, false);
  assert.equal(window.localStorage.getItem('efficiency.tab_seen'), '1');
});

test('ohne funktionierenden localStorage laeuft die Huelle weiter', () => {
  const broken = (win) => {
    Object.defineProperty(win, 'localStorage', {
      get() { throw new Error('denied'); },
    });
  };
  const { component } = createShell({ bodyAttrs: 'data-has-chains="true"', beforeLoad: broken });
  assert.equal(component.efficiencyTabNew, true);
  assert.doesNotThrow(() => component.setActivePanel('efficiency-panel'));
  assert.equal(component.efficiencyTabNew, false);
});

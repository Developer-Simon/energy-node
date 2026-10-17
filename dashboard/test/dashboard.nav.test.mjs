// Die Tab-Leiste wird am Handy zum Aufklappmenue. Getestet wird nur die
// Logik in dashboardShell() - ob das Menue auch *aussieht* wie eines,
// entscheidet CSS und damit die Sichtpruefung. jsdom kennt kein Layout.
//
// dashboard.js registriert seine Komponenten erst im alpine:init-Event.
// Der Test stellt ein Alpine-Doppel bereit und loest das Event aus; so
// braucht der Produktivcode keine Test-Naht.
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
  <button id="tab-overview" class="tab" role="tab" aria-controls="overview-panel">Übersicht</button>
  <button id="tab-devices" class="tab" role="tab" aria-controls="devices-panel">Geräte</button>
  <button id="tab-automations" class="tab" role="tab" aria-controls="automations-panel">Automationen</button>
`;

function createShell(navHtml = TABS) {
  const dom = new JSDOM(
    `<!doctype html><html><body><div class="nav-shell"><nav id="dashboard-tabs">${navHtml}</nav></div></body></html>`,
    { runScripts: 'outside-only', url: 'http://localhost/' },
  );
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, directive: () => {} };
  // dashboard.js startet beim Laden ein Modul-Level-setInterval (die
  // Zeitstempel-Aktualisierung) - ein echtes window.setInterval wuerde den
  // Testprozess sonst am Leben halten, gleiche Lösung wie notifications.test.mjs.
  dom.window.setInterval = () => 0;
  // devicesPanel mixes in helpers from these scripts, which load before it.
  for (const name of ['entity-values.js', 'device-tile.js', 'overview-values.js', 'device-tile-values.js', 'compact-card-values.js', 'device-structure.js']) {
    vm.runInContext(fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8'), context);
  }
  vm.runInContext(source, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  return { component: factories.dashboardShell(), window: dom.window, factories };
}

test('das Menue startet geschlossen', () => {
  const { component } = createShell();
  assert.equal(component.navOpen, false);
});

test('Umschalten oeffnet und schliesst das Menue', () => {
  const { component } = createShell();
  component.navOpen = !component.navOpen;
  assert.equal(component.navOpen, true);
  component.navOpen = !component.navOpen;
  assert.equal(component.navOpen, false);
});

test('setActivePanel schliesst das Menue und setzt den aktiven Bereich', () => {
  const { component } = createShell();
  component.navOpen = true;
  component.setActivePanel('devices-panel');
  assert.equal(component.navOpen, false);
  assert.equal(component.activePanel, 'devices-panel');
});

test('activeTabLabel liefert die Beschriftung des aktiven Tabs', () => {
  const { component } = createShell();
  assert.equal(component.activeTabLabel, 'Übersicht');
  component.setActivePanel('automations-panel');
  assert.equal(component.activeTabLabel, 'Automationen');
});

test('activeTabLabel bleibt leer, wenn kein passender Tab im DOM steht', () => {
  // Der Gast-Instanz fehlen sieben Tabs; ausserdem kann ein Fragment-Nachladen
  // die Leiste kurz ohne den gesuchten Button hinterlassen.
  const { component } = createShell('');
  assert.equal(component.activeTabLabel, '');
});

test('freigegebene Tabs heben die Breitendeckelung auf, gedeckelte setzen sie zurueck', () => {
  const { component, window } = createShell();
  window.document.body.dataset.widePanels = 'overview,layout';
  component.initWidePanels();
  assert.equal(window.document.body.style.getPropertyValue('--page-max'), 'none');

  component.setActivePanel('settings-panel');
  assert.equal(window.document.body.style.getPropertyValue('--page-max'), '1180px');

  component.setActivePanel('layout-panel');
  assert.equal(window.document.body.style.getPropertyValue('--page-max'), 'none');
});

test('eine leere Freigabeliste deckelt jeden Tab', () => {
  const { component, window } = createShell();
  window.document.body.dataset.widePanels = '';
  component.initWidePanels();
  assert.equal(window.document.body.style.getPropertyValue('--page-max'), '1180px');
});

test('layout-pages-changed nimmt nur wirklich neue Seiten in extraPages auf', () => {
  const { component, window } = createShell(`
    <button class="tab is-page" data-page-id="p1" data-page-name="Zuhause">Zuhause</button>
    <button class="tab is-page" data-page-id="p2" data-page-name="Werkstatt">Werkstatt</button>
  `);
  component.init();
  window.document.dispatchEvent(new window.CustomEvent('layout-pages-changed', {
    detail: {pages: [{id: 'p1', name: 'Zuhause'}, {id: 'p2', name: 'Werkstatt'}, {id: 'Garten', name: 'Garten'}]},
  }));
  assert.equal(component.extraPages.length, 1);
  assert.equal(component.extraPages[0].id, 'Garten');
  assert.equal(component.extraPages[0].name, 'Garten');
  assert.equal(component.activePage, 'Garten');
});

test('eine umbenannte Seite ersetzt ihren Tab, statt einen zweiten zu erzeugen', () => {
  // Eine server-seite mit neuem Namen geht in extraPages (zeigt neuen Namen)
  // und hiddenPages (versteckt alten Namen).
  const { component, window } = createShell(`
    <button class="tab is-page" data-page-id="p1" data-page-name="Zuhause">Zuhause</button>
    <button class="tab is-page" data-page-id="p2" data-page-name="Werkstatt">Werkstatt</button>
  `);
  component.init();
  window.document.dispatchEvent(new window.CustomEvent('layout-pages-changed', {
    detail: {pages: [{id: 'p1', name: 'Keller'}, {id: 'p2', name: 'Werkstatt'}], active: 'p1'},
  }));

  assert.equal(component.extraPages.length, 1, 'eine umbenannte seite in extraPages');
  assert.equal(component.extraPages[0].id, 'p1');
  assert.equal(component.extraPages[0].name, 'Keller');
  assert.deepEqual([...component.hiddenPages], ['p1'], 'alter tab-name ist versteckt');
  assert.equal(component.activePage, 'p1');
});

test('layout-pages-changed folgt der gemeldeten aktiven Seite', () => {
  const { component, window } = createShell(`
    <button class="tab is-page" data-page-id="p1" data-page-name="Zuhause">Zuhause</button>
    <button class="tab is-page" data-page-id="p2" data-page-name="Werkstatt">Werkstatt</button>
  `);
  component.init();
  window.document.dispatchEvent(new window.CustomEvent('layout-pages-changed', {
    detail: {pages: [{id: 'p2', name: 'Werkstatt'}], active: 'p2'},
  }));

  assert.deepEqual([...component.hiddenPages], ['p1'], 'die geloeschte Seite verschwindet');
  assert.equal(component.activePage, 'p2');
});

test('Umbenennen einer server-Seite haelt activePage (die ID) und gibt event {id, name}', () => {
  const { component, window } = createShell(`
    <button class="tab is-page" data-page-id="p1" data-page-name="Zuhause">Zuhause</button>
  `);
  component.init();
  component.activePage = 'p1';
  window.document.dispatchEvent(new window.CustomEvent('layout-pages-changed', {
    detail: {pages: [{id: 'p1', name: 'Keller'}], active: 'p1'},
  }));

  assert.equal(component.activePage, 'p1', 'activePage bleibt die ID');
  assert.ok(component.extraPages.some(p => p.id === 'p1' && p.name === 'Keller'));
});

test('extraPages und hiddenPages arbeiten mit IDs', () => {
  const { component, window } = createShell(`
    <button class="tab is-page" data-page-id="server1" data-page-name="Alt">Alt</button>
    <button class="tab is-page" data-page-id="server2" data-page-name="Beibehalten">Beibehalten</button>
  `);
  component.init();
  // Eine neue Seite, eine umbenannte server-seite
  window.document.dispatchEvent(new window.CustomEvent('layout-pages-changed', {
    detail: {pages: [{id: 'server1', name: 'Neu'}, {id: 'server2', name: 'Beibehalten'}, {id: 'new1', name: 'Ganz neu'}]},
  }));

  assert.ok(component.extraPages.some(p => p.id === 'server1'), 'umbenannte server-seite in extra mit ID');
  assert.ok(component.extraPages.some(p => p.id === 'new1'), 'neue seite in extra mit ID');
  assert.ok(component.hiddenPages.includes('server1'), 'alter tab versteckt mit ID');
});


test('dashboard-open-panel switches the tab and asks the energy page to focus rows', async () => {
  const { component, window } = createShell();
  component.loadPanel = () => {};
  component.init();
  const focused = [];
  window.addEventListener('energy-focus-rows', event => focused.push(event.detail.ids));
  window.dispatchEvent(new window.CustomEvent('dashboard-open-panel', { detail: { panel: 'energy-panel', energyFocus: ['a'] } }));
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(component.activePanel, 'energy-panel');
  assert.deepEqual(JSON.parse(JSON.stringify(window.__energyFocusRequest__)), ['a']);
  assert.deepEqual(focused, [['a']]);
});

test('dashboard-open-panel forwards config and automation focus requests', async () => {
  const { component, window } = createShell();
  component.loadPanel = () => {};
  component.init();
  const seen = [];
  window.addEventListener('config-focus-item', event => seen.push(['config', event.detail]));
  window.addEventListener('automation-focus-rule', event => seen.push(['automation', event.detail]));

  window.dispatchEvent(new window.CustomEvent('dashboard-open-panel', { detail: { panel: 'config-panel', configFocus: { config: 'battery_soc_devices', item: 'bank' } } }));
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(component.activePanel, 'config-panel');
  assert.deepEqual(JSON.parse(JSON.stringify(window.__configFocusRequest__)), { config: 'battery_soc_devices', item: 'bank' });
  assert.deepEqual(JSON.parse(JSON.stringify(seen)), [['config', { config: 'battery_soc_devices', item: 'bank' }]]);

  window.dispatchEvent(new window.CustomEvent('dashboard-open-panel', { detail: { panel: 'automations-panel', automationFocus: 'r 1' } }));
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(component.activePanel, 'automations-panel');
  assert.equal(window.__automationFocusRequest__, 'r 1');
  assert.deepEqual(JSON.parse(JSON.stringify(seen.at(-1))), ['automation', { id: 'r 1' }]);
});

// Modus "more": was nicht in die Zeile passt, wandert in "Weitere". jsdom kennt
// kein Layout, darum liefern die Tabs hier feste Breiten.
function createRow({ navWidth, mode = 'more' }) {
  const html = `${TABS}<span class="nav-more" hidden></span>`;
  const { component, window } = createShell(html);
  const nav = window.document.getElementById('dashboard-tabs');
  Object.defineProperty(nav, 'clientWidth', { value: navWidth });
  nav.querySelectorAll('button.tab').forEach((tab) => Object.defineProperty(tab, 'offsetWidth', { value: 100 }));
  Object.defineProperty(nav.querySelector('.nav-more'), 'offsetWidth', { value: 80 });
  component.navMode = mode;
  return { component, nav };
}
const overflowIds = (nav) => [...nav.querySelectorAll('button.tab.is-overflow')].map((tab) => tab.id);

test('fitNav haelt alle Tabs in der Zeile, wenn sie passen', () => {
  const { component, nav } = createRow({ navWidth: 400 });
  component.fitNav();
  assert.deepEqual(overflowIds(nav), []);
  assert.equal(component.overflowNav.length, 0);
  assert.equal(nav.querySelector('.nav-more').hidden, true);
});

test('fitNav schiebt die letzten Tabs in die Liste Weitere', () => {
  const { component, nav } = createRow({ navWidth: 250 });
  component.fitNav();
  assert.deepEqual(overflowIds(nav), ['tab-devices', 'tab-automations']);
  assert.deepEqual([...component.overflowNav.map((item) => item.label)], ['Geräte', 'Automationen']);
  assert.equal(nav.querySelector('.nav-more').hidden, false);
});

test('fitNav tut im Modus menu nichts und raeumt vorherige Ausblendungen auf', () => {
  const { component, nav } = createRow({ navWidth: 250 });
  component.fitNav();
  component.navMode = 'menu';
  component.fitNav();
  assert.deepEqual(overflowIds(nav), []);
  assert.equal(component.overflowNav.length, 0);
});

test('ein Klick auf einen Eintrag der Liste loest den Tab selbst aus', () => {
  const { component, nav } = createRow({ navWidth: 250 });
  component.fitNav();
  let clicked = 0;
  nav.querySelector('#tab-devices').addEventListener('click', () => { clicked += 1; });
  component.overflowNav[0].el.click();
  assert.equal(clicked, 1);
});

// Device panel stand-in for the deviceFocus tests: the real devicesPanel
// factory, with selectDevice replaced by a recorder.
function devicesPanelFor(window, factories, selected) {
  const panel = factories.devicesPanel();
  panel.$root = window.document.createElement('div');
  panel.$root.id = 'devices-panel';
  panel.$refs = {};
  panel.selectDevice = id => { selected.push(id); };
  return panel;
}

test('a deviceFocus event opens the devices tab and selects the device there', async () => {
  const { component, window, factories } = createShell();
  component.loadPanel = () => {};
  component.init();
  const selected = [];
  devicesPanelFor(window, factories, selected).init();
  window.dispatchEvent(new window.CustomEvent('dashboard-open-panel', { detail: { panel: 'devices-panel', deviceFocus: 'plug' } }));
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(component.activePanel, 'devices-panel');
  assert.deepEqual(selected, ['plug']);
});

test('a pending deviceFocus request is taken when the devices panel starts later', () => {
  const { window, factories } = createShell();
  window.__deviceFocusRequest__ = 'plug';
  const selected = [];
  devicesPanelFor(window, factories, selected).init();
  assert.deepEqual(selected, ['plug']);
  assert.equal(window.__deviceFocusRequest__, null);
});

test('the overview component ignores a deviceFocus request so no hidden dialog opens', async () => {
  const { component, window, factories } = createShell();
  component.loadPanel = () => {};
  component.init();
  const selected = [];
  const overview = devicesPanelFor(window, factories, selected);
  overview.$root.id = 'overview-panel';
  window.__deviceFocusRequest__ = 'plug';
  overview.init();
  window.dispatchEvent(new window.CustomEvent('devices-focus-device', { detail: { id: 'plug' } }));
  assert.deepEqual(selected, []);
  assert.equal(window.__deviceFocusRequest__, 'plug');
});

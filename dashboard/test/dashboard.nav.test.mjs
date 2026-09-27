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
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  // dashboard.js startet beim Laden ein Modul-Level-setInterval (die
  // Zeitstempel-Aktualisierung) - ein echtes window.setInterval wuerde den
  // Testprozess sonst am Leben halten, gleiche Lösung wie notifications.test.mjs.
  dom.window.setInterval = () => 0;
  vm.runInContext(source, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  return { component: factories.dashboardShell(), window: dom.window };
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


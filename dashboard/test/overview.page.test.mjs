// Der Modusknopf ist das einzige Stueck Editor, das mit der Uebersicht laedt.
// Er darf nichts koennen ausser: Assets anfordern, data-mode setzen, Bescheid
// sagen. Alles Weitere gehoert in layout-editor.js.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'overview.page.js'),
  'utf8',
);

function createOverview({ ready = () => Promise.resolve(), fetchImpl } = {}) {
  const dom = new JSDOM('<!doctype html><html><body><section id="overview-panel" data-editor-fragment="http://localhost/?fragment=panel&panel=layout-editor"></section></body></html>',
    { runScripts: 'outside-only', url: 'http://localhost/' });
  let factory;
  dom.window.Alpine = {
    data: (name, f) => { if (name === 'overviewShell') factory = f; },
    store: () => ({}),
    initTree: () => {},
  };
  dom.window.fetch = fetchImpl || (() => Promise.resolve({ ok: true, text: () => Promise.resolve('') }));
  vm.runInContext(source, dom.getInternalVMContext());
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  const shell = factory();
  shell.$root = dom.window.document.getElementById('overview-panel');
  shell.editorAssetsReady = ready;
  return { dom, shell, factory };
}

test('die Uebersicht startet im Ansichtsmodus', () => {
  const { shell } = createOverview();
  assert.equal(shell.mode, 'view');
});

test('Editieren fordert die Assets an und schaltet erst danach um', async () => {
  let resolveAssets;
  const { dom, shell } = createOverview({ ready: () => new Promise(r => { resolveAssets = r; }) });
  const promise = shell.enterEdit();
  assert.equal(shell.mode, 'view', 'vor dem Laden darf nicht umgeschaltet werden');
  resolveAssets();
  await promise;
  assert.equal(shell.mode, 'edit');
  assert.equal(dom.window.document.getElementById('overview-panel').dataset.mode, 'edit');
});

test('beim Umschalten wird der Editor benachrichtigt', async () => {
  const { dom, shell } = createOverview();
  const seen = [];
  dom.window.document.addEventListener('layout-editor:mount', () => seen.push('mount'));
  dom.window.document.addEventListener('layout-editor:unmount', () => seen.push('unmount'));
  await shell.enterEdit();
  shell.leaveEdit();
  assert.deepEqual(seen, ['mount', 'unmount']);
});

test('scheitert das Laden, bleibt die Ansicht stehen und meldet es', async () => {
  const { shell } = createOverview({ ready: () => Promise.reject(new Error('offline')) });
  await shell.enterEdit();
  assert.equal(shell.mode, 'view');
  assert.match(shell.error, /Editor/);
});

test('Editieren holt das Fragment, setzt es ein und initialisiert Alpine darauf', async () => {
  const calls = [];
  const { dom, shell } = createOverview({
    ready: () => Promise.resolve(),
    fetchImpl: url => { calls.push(url); return Promise.resolve({ ok: true, text: () => Promise.resolve('<aside class="layout-toolbox" id="toolbox"></aside>') }); },
  });
  dom.window.Alpine.initTree = el => calls.push(['initTree', el.id]);
  await shell.enterEdit();
  assert.ok(calls[0].includes('fragment=panel&panel=layout-editor'));
  const root = dom.window.document.getElementById('layout-editor-root');
  assert.ok(root, 'der Editor-Wurzelknoten liegt im overview-panel');
  assert.equal(root.getAttribute('x-data'), 'layoutEditor()');
  assert.ok(calls.some(c => Array.isArray(c) && c[0] === 'initTree' && c[1] === 'layout-editor-root'));
  assert.equal(dom.window.document.getElementById('overview-panel').dataset.mode, 'edit');
});

test('das Mount-Ereignis kommt erst nach initTree', async () => {
  const order = [];
  const { dom, shell } = createOverview({
    fetchImpl: () => Promise.resolve({ ok: true, text: () => Promise.resolve('<div></div>') }),
  });
  dom.window.Alpine.initTree = () => order.push('initTree');
  dom.window.document.addEventListener('layout-editor:mount', () => order.push('mount'));
  await shell.enterEdit();
  assert.deepEqual(order, ['initTree', 'mount']);
});

test('scheitert der Fragment-Fetch, bleibt die Ansicht stehen und meldet es', async () => {
  const { shell } = createOverview({ fetchImpl: () => Promise.resolve({ ok: false, status: 503, text: () => Promise.resolve('') }) });
  await shell.enterEdit();
  assert.equal(shell.mode, 'view');
  assert.match(shell.error, /Editor/);
});

test('Verlassen fragt den Waechter und entfernt das Fragment', async () => {
  const { dom, shell } = createOverview({ fetchImpl: () => Promise.resolve({ ok: true, text: () => Promise.resolve('<div></div>') }) });
  await shell.enterEdit();
  let asked = 0;
  dom.window.__layoutEditor__ = { unsaved: true, confirmLeave: () => { asked += 1; return Promise.resolve(true); } };
  await shell.leaveEdit();
  assert.equal(asked, 1);
  assert.equal(dom.window.document.getElementById('layout-editor-root'), null);
  assert.equal(shell.mode, 'view');
});

test('sagt der Waechter ab, bleibt der Editor', async () => {
  const { dom, shell } = createOverview({ fetchImpl: () => Promise.resolve({ ok: true, text: () => Promise.resolve('<div></div>') }) });
  await shell.enterEdit();
  dom.window.__layoutEditor__ = { unsaved: true, confirmLeave: () => Promise.resolve(false) };
  await shell.leaveEdit();
  assert.equal(shell.mode, 'edit');
  assert.ok(dom.window.document.getElementById('layout-editor-root'));
});

test('layout-editor:request-leave loest leaveEdit aus (Speichern & schliessen)', async () => {
  const { dom, shell } = createOverview({ fetchImpl: () => Promise.resolve({ ok: true, text: () => Promise.resolve('<div></div>') }) });
  await shell.enterEdit();
  assert.equal(shell.mode, 'edit');
  // save() hat unsaved bereits geleert - kein Waechter mehr.
  dom.window.__layoutEditor__ = { unsaved: false };
  dom.window.document.dispatchEvent(new dom.window.CustomEvent('layout-editor:request-leave'));
  await Promise.resolve();
  assert.equal(shell.mode, 'view');
  assert.equal(dom.window.document.getElementById('layout-editor-root'), null);
});

// --- Ansichtsmodus fuellt die Zeile aus -----------------------------------
// Im Editor stehen die Kacheln exakt so, wie sie eingestellt sind - mit
// Luecken. In der Ansicht soll rechts nichts leer bleiben: die groesste
// Kachel der Zeile waechst in den Rest, bei Gleichstand die letzte.

test('fillRows laesst eine volle Zeile in Ruhe', () => {
  const { factory } = createOverview();
  assert.deepEqual(factory.fillRows(['2', '2'], 4), [2, 2]);
  assert.deepEqual(factory.fillRows(['full'], 4), [4]);
});

test('fillRows dehnt bei Gleichstand die letzte Kachel', () => {
  const { factory } = createOverview();
  assert.deepEqual(factory.fillRows(['1', '1'], 4), [1, 3]);
});

test('fillRows dehnt die groesste Kachel der Zeile, auch wenn sie vorn steht', () => {
  const { factory } = createOverview();
  assert.deepEqual(factory.fillRows(['2', '1'], 4), [3, 1]);
  assert.deepEqual(factory.fillRows(['1', '2'], 4), [1, 3]);
});

test('fillRows rechnet je Zeile, nicht ueber das ganze Raster', () => {
  const { factory } = createOverview();
  // Zeile 1 ist voll und bleibt; Zeile 2 hat eine Kachel und bekommt alles.
  assert.deepEqual(factory.fillRows(['2', '2', '1'], 4), [2, 2, 4]);
});

test('fillRows deckelt Klassen oberhalb der Spaltenzahl wie die Container-Query', () => {
  const { factory } = createOverview();
  assert.deepEqual(factory.fillRows(['3', '1'], 2), [2, 2]);
});

test('die Uebersicht dehnt im Ansichtsmodus, im Bearbeitungsmodus nicht', () => {
  const { dom, shell } = createOverview();
  const panel = dom.window.document.getElementById('overview-panel');
  panel.innerHTML = '<div class="layout-grid"><div data-layout-page="Z">'
    + '<div class="layout-grid-item layout-grid-item-1"></div>'
    + '<div class="layout-grid-item layout-grid-item-1"></div>'
    + '</div></div>';
  const cards = [...panel.querySelectorAll('.layout-grid-item')];

  shell.applyRowFill(4);
  assert.equal(cards[1].style.gridColumn, 'span 3', 'die letzte Kachel fuellt den Rest');

  panel.dataset.mode = 'edit';
  shell.applyRowFill(4);
  assert.equal(cards[1].style.gridColumn, '', 'im Editor steht sie wieder auf ihrer Einstellung');
});

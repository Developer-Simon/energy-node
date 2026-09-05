// Die Editor-Abhaengigkeiten (Gridstack, Choices, layout-editor.js/css) duerfen
// NICHT mit der Uebersicht kommen - overview-panel ist als einziges Panel nicht
// lazy. Geprueft wird: nichts geladen bis zum ersten Aufruf, danach genau
// einmal, und ein Fehlschlag darf einen zweiten Versuch nicht blockieren.
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

function createShell() {
  const dom = new JSDOM(
    '<!doctype html><html><body><section id="overview-panel"'
    + ' data-editor-script="/static/js-deps/choices.min.js,/static/js/layout-editor.js"'
    + ' data-editor-css="/static/css/layout-editor.css"></section></body></html>',
    { runScripts: 'outside-only', url: 'http://localhost/' },
  );
  let shell;
  dom.window.Alpine = { data: (name, factory) => { if (name === 'dashboardShell') shell = factory(); }, store: () => ({}) };
  dom.window.setInterval = () => 0;
  vm.runInContext(source, dom.getInternalVMContext());
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  return { dom, shell };
}

test('vor dem ersten Editieren wird kein Editor-Asset geladen', () => {
  const { shell } = createShell();
  const loaded = [];
  shell.loadAsset = (src, type) => { loaded.push([src, type]); return Promise.resolve(); };
  assert.deepEqual(loaded, []);
});

test('editorAssetsReady laedt Skripte und Styles genau einmal', async () => {
  const { shell } = createShell();
  const loaded = [];
  shell.loadAsset = (src, type) => { loaded.push(type); return Promise.resolve(); };
  await shell.editorAssetsReady();
  await shell.editorAssetsReady();
  assert.deepEqual(loaded.sort(), ['script', 'style']);
});

test('nach einem Fehlschlag ist ein zweiter Versuch moeglich', async () => {
  const { shell } = createShell();
  let attempts = 0;
  shell.loadAsset = () => { attempts += 1; return attempts === 1 ? Promise.reject(new Error('offline')) : Promise.resolve(); };
  await assert.rejects(() => shell.editorAssetsReady());
  await shell.editorAssetsReady();
  assert.ok(attempts > 1, 'das verworfene Promise muss einen neuen Versuch zulassen');
});

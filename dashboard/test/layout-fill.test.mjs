// Regressionstests fuer layout-fill.js. fillRows() ist die ganze Fachlogik:
// Wunsch-Spannweiten plus Spurenzahl ergeben die tatsaechlichen
// Spannweiten. Der DOM-Teil darunter ist nur Ablesen und Zuweisen.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'layout-fill.js'),
  'utf8',
);

function load(html = '<!doctype html><html><body></body></html>') {
  const dom = new JSDOM(html, { runScripts: 'outside-only', url: 'http://localhost/' });
  vm.runInContext(scriptSource, dom.getInternalVMContext());
  return dom;
}

const fill = (spans, columns) => {
  const dom = load();
  return [...dom.window.dashboardLayoutFill.fillRows(spans, columns)];
};

test('eine volle Zeile bleibt unveraendert', () => {
  assert.deepEqual(fill([2, 2], 4), [2, 2]);
  assert.deepEqual(fill([1, 1, 1, 1], 4), [1, 1, 1, 1]);
});

test('die uebrige Spur geht an die breiteste Kachel der Zeile', () => {
  assert.deepEqual(fill([2, 2, 2], 5), [3, 2, 5]);
});

test('bei gleicher Breite bekommt die erste Kachel der Zeile den Rest', () => {
  assert.deepEqual(fill([1, 1], 3), [2, 1]);
});

test('die letzte, unvollstaendige Zeile wird ebenfalls aufgefuellt', () => {
  assert.deepEqual(fill([2, 2, 1], 4), [2, 2, 4]);
});

test('eine Spannweite groesser als die Spurenzahl wird auf sie geklemmt', () => {
  assert.deepEqual(fill([6, 1], 3), [3, 3]);
});

test('ohne bekannte Spurenzahl bleiben die Spannweiten unangetastet', () => {
  assert.deepEqual(fill([2, 3], 0), [2, 3]);
});

test('applyAll schreibt die gefuellten Spannweiten als grid-column', () => {
  const dom = load(`<!doctype html><html><body>
    <div class="layout-grid">
      <div class="layout-grid-item layout-grid-item-2"></div>
      <div class="layout-grid-item layout-grid-item-2"></div>
      <div class="layout-grid-item layout-grid-item-2"></div>
    </div>
  </body></html>`);
  // jsdom rechnet kein Grid; die Spurenzahl kommt darum aus einem Stub
  // derselben Form, die getComputedStyle im Browser liefert.
  dom.window.getComputedStyle = () => ({ gridTemplateColumns: '260px 260px 260px 260px 260px' });
  dom.window.dashboardLayoutFill.applyAll();
  const spans = [...dom.window.document.querySelectorAll('.layout-grid-item')]
    .map(item => item.style.gridColumn);
  assert.deepEqual(spans, ['span 3', 'span 2', 'span 5']);
});

test('layout-grid-item-full nimmt alle Spuren ein', () => {
  const dom = load(`<!doctype html><html><body>
    <div class="layout-grid">
      <div class="layout-grid-item layout-grid-item-full"></div>
      <div class="layout-grid-item layout-grid-item-1"></div>
    </div>
  </body></html>`);
  dom.window.getComputedStyle = () => ({ gridTemplateColumns: '260px 260px 260px' });
  dom.window.dashboardLayoutFill.applyAll();
  const spans = [...dom.window.document.querySelectorAll('.layout-grid-item')]
    .map(item => item.style.gridColumn);
  assert.deepEqual(spans, ['span 3', 'span 3']);
});

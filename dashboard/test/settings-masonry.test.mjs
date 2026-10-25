// Regressionstests fuer settings-masonry.js. rowSpan() ist die Rechnung,
// apply()/attach() nur Messen und Zuweisen; jsdom hat kein Layout, die
// Hoehen werden deshalb pro Karte vorgegeben.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'settings-masonry.js'),
  'utf8',
);

function load(html, {resizeObserver = true} = {}) {
  const dom = new JSDOM(html, { runScripts: 'outside-only', url: 'http://localhost/' });
  const view = dom.window;
  if (resizeObserver) {
    view.ResizeObserver = class { observe() {} disconnect() {} };
  }
  view.requestAnimationFrame = () => 0;
  vm.runInContext(scriptSource, dom.getInternalVMContext());
  return dom;
}

function setHeight(element, height) {
  Object.defineProperty(element, 'offsetHeight', { configurable: true, get: () => height });
}

const GRID = `<!doctype html><html><body><section id="panel">
  <div class="settings-cards" style="column-gap: 16px">
    <div class="setting-card" id="a"></div>
    <div class="setting-card" id="b" style="margin-bottom: 4px"></div>
    <div class="setting-card" id="c"></div>
  </div></section></body></html>`;

test('rowSpan rechnet Hoehe plus Abstand auf ganze 1px-Zeilen auf', () => {
  const { rowSpan } = load(GRID).window.dashboardSettingsMasonry;
  assert.equal(rowSpan(200, 16), 216);
  assert.equal(rowSpan(120.4, 16), 137);
  assert.equal(rowSpan(80, 0), 80);
});

test('rowSpan gibt fuer eine ausgeblendete Karte 0 zurueck', () => {
  const { rowSpan } = load(GRID).window.dashboardSettingsMasonry;
  assert.equal(rowSpan(0, 16), 0);
  assert.equal(rowSpan(NaN, 16), 0);
});

test('attach markiert das Raster und setzt die Spannweiten samt Aussenabstand', () => {
  const dom = load(GRID);
  const doc = dom.window.document;
  setHeight(doc.getElementById('a'), 300);
  setHeight(doc.getElementById('b'), 100);
  setHeight(doc.getElementById('c'), 0);
  dom.window.dashboardSettingsMasonry.attach(doc.getElementById('panel'));
  assert.ok(doc.querySelector('.settings-cards').classList.contains('settings-cards--masonry'));
  assert.equal(doc.getElementById('a').style.gridRowEnd, 'span 316');
  assert.equal(doc.getElementById('b').style.gridRowEnd, 'span 120');
  assert.equal(doc.getElementById('c').style.gridRowEnd, '');
});

test('ohne ResizeObserver bleibt das normale Raster stehen', () => {
  const dom = load(GRID, {resizeObserver: false});
  const doc = dom.window.document;
  setHeight(doc.getElementById('a'), 300);
  dom.window.dashboardSettingsMasonry.attach(doc);
  assert.ok(!doc.querySelector('.settings-cards').classList.contains('settings-cards--masonry'));
  assert.equal(doc.getElementById('a').style.gridRowEnd, '');
});

test('attach zaehlt die Karten in einem display-contents-Wrapper als eigene Karten', () => {
  const dom = load(`<!doctype html><html><body><section id="panel">
    <div class="settings-cards" style="column-gap: 16px">
      <div class="setting-card" id="a"></div>
      <div class="users-manage" style="display: contents">
        <div class="setting-card" id="b"></div>
      </div>
    </div></section></body></html>`);
  const doc = dom.window.document;
  setHeight(doc.getElementById('a'), 300);
  setHeight(doc.getElementById('b'), 100);
  dom.window.dashboardSettingsMasonry.attach(doc.getElementById('panel'));
  assert.equal(doc.getElementById('a').style.gridRowEnd, 'span 316');
  assert.equal(doc.getElementById('b').style.gridRowEnd, 'span 116');
});

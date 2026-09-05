// theme.js löst die Farb-Tokens aus base.css auf. In jsdom kennt
// getComputedStyle keine Custom Properties aus Stylesheets, deshalb greift
// dort immer der Mint-Rückfall - genau das prüfen die Tests, weil alle
// SVG-Generatoren im Test darauf bauen.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'theme.js'), 'utf8');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  vm.runInContext(source, dom.getInternalVMContext());
  return dom;
}

test('color() liefert den Mint-Rückfall, wenn keine Variable gesetzt ist', () => {
  const dom = load();
  assert.equal(dom.window.DashboardTheme.color('flow-pv'), '#f3c969');
  assert.equal(dom.window.DashboardTheme.color('border'), '#46515d');
  assert.equal(dom.window.DashboardTheme.color('text-muted'), '#8b949e');
});

test('color() bevorzugt einen gesetzten Inline-Wert', () => {
  const dom = load();
  dom.window.document.documentElement.style.setProperty('--flow-pv', '#e8b23a');
  assert.equal(dom.window.DashboardTheme.color('flow-pv'), '#e8b23a');
});

test('color() liefert für unbekannte Tokens den leeren String', () => {
  const dom = load();
  assert.equal(dom.window.DashboardTheme.color('gibtsnicht'), '');
});

test('colors() bildet ein Token-Objekt ab', () => {
  const dom = load();
  const result = JSON.parse(JSON.stringify(dom.window.DashboardTheme.colors({pv: 'flow-pv', rest: 'flow-rest'})));
  assert.deepEqual(result, {pv: '#f3c969', rest: '#7d8792'});
});

test('onChange() ruft den Handler beim Themewechsel und meldet wieder ab', () => {
  const dom = load();
  let calls = 0;
  const off = dom.window.DashboardTheme.onChange(() => { calls += 1; });
  dom.window.document.dispatchEvent(new dom.window.CustomEvent('dashboard-theme-changed', {detail: {theme: 'stromblau'}}));
  assert.equal(calls, 1);
  off();
  dom.window.document.dispatchEvent(new dom.window.CustomEvent('dashboard-theme-changed', {detail: {theme: 'mint'}}));
  assert.equal(calls, 1);
});

test('onChange() leert den Cache, bevor der Handler läuft', () => {
  const dom = load();
  assert.equal(dom.window.DashboardTheme.color('accent'), '#9fd');
  dom.window.document.documentElement.style.setProperty('--accent', '#5ec8ff');
  let seen = null;
  dom.window.DashboardTheme.onChange(() => { seen = dom.window.DashboardTheme.color('accent'); });
  dom.window.document.dispatchEvent(new dom.window.CustomEvent('dashboard-theme-changed', {detail: {theme: 'stromblau'}}));
  assert.equal(seen, '#5ec8ff');
});

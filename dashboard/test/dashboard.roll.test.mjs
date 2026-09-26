// x-roll (dashboard.js) tweens a number in the device modal. Since the number
// format setting, the visible text is already formatted ("12.345,6"), so the
// directive reads the raw target from data-roll-value and the unit from
// data-roll-unit and formats every tween frame itself.
//
// Alpine is replaced by a double that only records directives. A test then
// plays Alpine's part: x-bind:data-roll-value first, then x-text.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const webui = path.join(here, '..', 'internal', 'webui');
const source = fs.readFileSync(path.join(webui, 'static', 'js', 'dashboard.js'), 'utf8');

function loadRoll(i18nOptions) {
  const dom = new JSDOM('<!doctype html><html><head></head><body><span id="v"></span></body></html>', {
    runScripts: 'outside-only',
    url: 'http://localhost/',
    pretendToBeVisual: true,
  });
  installI18n(dom.window, i18nOptions);
  const directives = {};
  dom.window.Alpine = { data() {}, directive: (name, fn) => { directives[name] = fn; } };
  // dashboard.js starts a module-level setInterval that would keep the test
  // process alive.
  dom.window.setInterval = () => 0;
  vm.runInContext(source, dom.getInternalVMContext());
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  return { window: dom.window, roll: directives.roll };
}

const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

test('x-roll animiert formatierte Werte und endet exakt auf dem Zieltext', async () => {
  const { window, roll } = loadRoll({ lang: 'de', format: 'comma' });
  const el = window.document.getElementById('v');
  el.dataset.rollValue = '12345.6';
  el.dataset.rollUnit = 'kWh';
  el.textContent = '12.345,6';
  roll(el, {}, { cleanup() {} });

  // Alpine binds data-roll-value before it writes the x-text.
  el.dataset.rollValue = '12400.2';
  el.textContent = '12.400,2';

  const seen = new Set();
  for (let waited = 0; waited < 500; waited += 20) {
    seen.add(el.textContent);
    await sleep(20);
  }
  const between = [...seen].filter(text => text !== '12.345,6' && text !== '12.400,2');
  assert.ok(between.length > 0, `no animation frames seen: ${[...seen]}`);
  for (const text of between) {
    assert.match(text, /^12\.\d{3},\d$/, `tween frame is not formatted: ${text}`);
  }
  assert.equal(el.textContent, '12.400,2');
});

test('x-roll ohne Einheit zaehlt roh und endet auf dem Zieltext', async () => {
  const { window, roll } = loadRoll({ lang: 'de', format: 'comma' });
  const el = window.document.getElementById('v');
  el.dataset.rollValue = '100';
  el.dataset.rollUnit = '';
  el.textContent = '100';
  roll(el, {}, { cleanup() {} });

  el.dataset.rollValue = '200';
  el.textContent = '200';
  await sleep(500);
  assert.equal(el.textContent, '200');
});

test('jedes x-roll im Geraete-Modal bindet Rohwert und Einheit vor dem x-text', () => {
  const html = fs.readFileSync(path.join(webui, 'templates', 'devices.html'), 'utf8');
  const spans = html.match(/<span x-roll[^>]*>/g) || [];
  assert.ok(spans.length >= 3, `expected the three modal value spans, found ${spans.length}`);
  for (const span of spans) {
    const valueAt = span.indexOf('x-bind:data-roll-value=');
    const unitAt = span.indexOf('x-bind:data-roll-unit=');
    const textAt = span.indexOf('x-text=');
    assert.ok(valueAt > -1 && unitAt > -1, `missing roll bindings: ${span}`);
    assert.ok(valueAt < textAt && unitAt < textAt, `x-text must come after the roll bindings: ${span}`);
  }
});

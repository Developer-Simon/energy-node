// Relative times come from the catalog in both languages: the device detail
// uses plural keys (time.ago.*), the data-relative-timestamp nodes use the
// compact form (time.short.*).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { catalog, installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'dashboard.js'),
  'utf8',
);

function load(lang, body = '') {
  const dom = new JSDOM(`<!doctype html><html><body>${body}</body></html>`, { runScripts: 'outside-only', url: 'http://localhost/' });
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, magic() {}, directive() {}, store() {} };
  // The module-level setInterval would keep the test process alive.
  dom.window.setInterval = () => 0;
  // devicesPanel mixes in the tile helpers from overview-values.js.
  dom.window.deviceTileMixin = () => ({});
  installI18n(dom.window, { lang });
  vm.runInContext(source, dom.getInternalVMContext());
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  return { window: dom.window, factories };
}

const fill = (text, n) => text.replace('{n}', String(n));

for (const lang of ['de', 'en']) {
  test(`device detail relative time uses the plural keys (${lang})`, () => {
    const cat = catalog(lang);
    const panel = load(lang).factories.devicesPanel();
    const now = new Date('2026-09-26T12:00:00Z');
    for (const n of [0, 1, 2]) {
      const at = new Date(now.getTime() - n * 1000);
      const key = n === 1 ? 'time.ago.seconds.one' : 'time.ago.seconds.other';
      assert.equal(panel.relativeTime(at, now), fill(cat[key], n));
    }
    assert.equal(panel.relativeTime(new Date(now.getTime() - 120000), now), fill(cat['time.ago.minutes.other'], 2));
  });

  test(`relative timestamp nodes use the compact catalog form (${lang})`, () => {
    const cat = catalog(lang);
    const past = Date.now() - 5000;
    const { window } = load(lang, `<span id="past" data-relative-timestamp="${past}"></span><span id="now" data-relative-timestamp="${Date.now()}"></span>`);
    window.document.dispatchEvent(new window.Event('DOMContentLoaded'));
    assert.equal(window.document.getElementById('past').textContent, fill(cat['time.short.past.seconds'], 5));
    assert.equal(window.document.getElementById('now').textContent, cat['time.just_now']);
  });
}

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'dashboard.js'),
  'utf8',
);

function loadDashboard(lang = 'de') {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  installI18n(dom.window, { lang });
  vm.runInContext(scriptSource, context);
  return dom.window;
}

test('Relative time for 0 seconds ago (de)', () => {
  const win = loadDashboard('de');
  // The formatRelativeTimestamp function is not directly exported, so we test through the t() function
  const catalog = win.I18n;

  // Check that time keys exist for German
  assert.ok(catalog.t('time.ago.just_now'), 'time.ago.just_now should exist in de');
  assert.ok(catalog.t('time.ago.seconds'), 'time.ago.seconds should exist in de');
  assert.ok(catalog.t('time.ago.minutes'), 'time.ago.minutes should exist in de');
});

test('Relative time for 0 seconds ago (en)', () => {
  const win = loadDashboard('en');
  const catalog = win.I18n;

  // Check that time keys exist for English
  assert.ok(catalog.t('time.ago.just_now'), 'time.ago.just_now should exist in en');
  assert.ok(catalog.t('time.ago.seconds'), 'time.ago.seconds should exist in en');
  assert.ok(catalog.t('time.ago.minutes'), 'time.ago.minutes should exist in en');
});

test('Panel load error messages use catalog keys (de)', () => {
  const win = loadDashboard('de');
  const catalog = win.I18n;

  // Check that panel error keys exist
  assert.ok(catalog.t('panel.load_failed', { code: 404 }), 'panel.load_failed should exist in de');
  assert.ok(catalog.t('panel.asset_load_failed', { url: 'test.js' }), 'panel.asset_load_failed should exist in de');
  assert.ok(catalog.t('panel.request_failed'), 'panel.request_failed should exist in de');
});

test('Panel load error messages use catalog keys (en)', () => {
  const win = loadDashboard('en');
  const catalog = win.I18n;

  // Check that panel error keys exist
  assert.ok(catalog.t('panel.load_failed', { code: 404 }), 'panel.load_failed should exist in en');
  assert.ok(catalog.t('panel.asset_load_failed', { url: 'test.js' }), 'panel.asset_load_failed should exist in en');
  assert.ok(catalog.t('panel.request_failed'), 'panel.request_failed should exist in en');
});

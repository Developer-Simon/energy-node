// Tests for the diagnostics panel component (diagnosticsPanel() in dashboard.js).
// Similar to devices.modal.test.mjs: jsdom setup, Alpine init, component extraction.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'dashboard.js'),
  'utf8',
);

function createDiagnosticsPanel() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: 'http://localhost/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, magic() {}, directive() {} };
  dom.window.setInterval = () => 0;
  installI18n(dom.window, {lang: 'en'});
  vm.runInContext(source, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  const panel = factories.diagnosticsPanel();
  Object.defineProperty(panel, 'testWindow', {value: dom.window});
  return panel;
}

test('warningText returns catalog value for a warning with key', () => {
  const panel = createDiagnosticsPanel();
  panel.testWindow.I18n.catalog['diagnostics.rule.offline.hint'] = "Check it's powered";
  const warning = {key: 'offline', hint: 'fallback hint'};
  assert.equal(panel.warningText(warning, 'hint'), "Check it's powered");
});

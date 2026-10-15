// Auf schmalen Fenstern setzt dashboard.js für den offenen Hilfe-Tooltip den
// Versatz nach links und die Breite, damit er unter dem Knopf die
// Fensterbreite mit 20 px Rand nutzt.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'dashboard.js'), 'utf8');

function load(narrow) {
  const dom = new JSDOM('<!doctype html><html><body><h3>Titel <span class="field-help"><button class="field-help-trigger" type="button"></button><span class="field-help-text" role="tooltip">Hilfe</span></span></h3></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  dom.window.Alpine = { data() {}, magic() {}, directive() {}, store() {} };
  dom.window.setInterval = () => 0;
  dom.window.deviceTileMixin = () => ({});
  dom.window.matchMedia = query => ({ matches: narrow && query === '(max-width: 700px)' });
  Object.defineProperty(dom.window.document.documentElement, 'clientWidth', { value: 400 });
  installI18n(dom.window);
  vm.runInContext(source, dom.getInternalVMContext());
  const help = dom.window.document.querySelector('.field-help');
  help.getBoundingClientRect = () => ({ left: 150, right: 172, top: 300, bottom: 322, width: 22, height: 22 });
  return { window: dom.window, help };
}

test('ein fokussierter Hilfe-Knopf setzt auf schmalen Fenstern Versatz und Breite', () => {
  const { window, help } = load(true);
  help.querySelector('.field-help-trigger').dispatchEvent(new window.FocusEvent('focusin', { bubbles: true }));
  assert.equal(help.style.getPropertyValue('--help-tip-left'), '-130px');
  assert.equal(help.style.getPropertyValue('--help-tip-width'), '360px');
  window.close();
});

test('auf breiten Fenstern bleibt der Tooltip unverändert', () => {
  const { window, help } = load(false);
  help.querySelector('.field-help-trigger').dispatchEvent(new window.FocusEvent('focusin', { bubbles: true }));
  assert.equal(help.style.getPropertyValue('--help-tip-left'), '');
  window.close();
});

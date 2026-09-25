// Regression tests for i18n.js: placeholder filling, plural forms, the
// "missing key shows the key" rule, and the language switcher writing the
// `lang` cookie under the reverse-proxy base path. Run with `npm test`.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'i18n.js'),
  'utf8',
);

function load({ boot, body = '' } = {}) {
  const dom = new JSDOM(
    `<!doctype html><html lang="de" data-base-path="/node"><body>${body}</body></html>`,
    { runScripts: 'outside-only', url: 'https://dashboard.local/node/' },
  );
  if (boot) dom.window.__I18N__ = boot;
  vm.runInContext(source, dom.getInternalVMContext());
  return dom.window;
}

const catalog = {
  greet: 'Hallo {name}',
  'file.one': '{n} Datei',
  'file.other': '{n} Dateien',
  empty: '',
};

test('t fills placeholders and keeps unknown ones visible', () => {
  const { I18n } = load({ boot: { lang: 'de', catalog } });
  assert.equal(I18n.t('greet', { name: 'Sam' }), 'Hallo Sam');
  assert.equal(I18n.t('greet'), 'Hallo {name}');
  assert.equal(I18n.t('greet', { other: 1 }), 'Hallo {name}');
});

test('a missing or empty key is shown as the key', () => {
  const { I18n } = load({ boot: { lang: 'de', catalog } });
  assert.equal(I18n.t('nope.nothing'), 'nope.nothing');
  assert.equal(I18n.t('empty'), 'empty');
});

test('tn picks .one for exactly 1 and .other otherwise', () => {
  const { I18n } = load({ boot: { lang: 'de', catalog } });
  assert.equal(I18n.tn('file', 1), '1 Datei');
  assert.equal(I18n.tn('file', 0), '0 Dateien');
  assert.equal(I18n.tn('file', 5), '5 Dateien');
});

test('lang comes from the catalog script, else from <html lang>', () => {
  assert.equal(load({ boot: { lang: 'en', catalog } }).I18n.lang, 'en');
  const bare = load();
  assert.equal(bare.I18n.lang, 'de');
  assert.equal(bare.I18n.t('greet'), 'greet');
});

test('setLanguage writes the cookie under the base path and reloads', () => {
  const window = load({ boot: { lang: 'de', catalog } });
  let reloads = 0;
  window.I18n.reload = () => { reloads += 1; };
  window.I18n.setLanguage('en');
  assert.match(window.document.cookie, /(^|; )lang=en(;|$)/);
  assert.equal(reloads, 1);
});

const pill = '<div class="lang-pill" role="radiogroup">'
  + '<label><input type="radio" name="lang" value="de" data-lang-select checked>DE</label>'
  + '<label><input type="radio" name="lang" value="en" data-lang-select>EN</label></div>';

const settle = (ms) => new Promise((resolve) => { setTimeout(resolve, ms); });

test('choosing a language in the pill switches once the thumb has settled', async () => {
  const window = load({ boot: { lang: 'de', catalog }, body: pill });
  let reloads = 0;
  window.I18n.reload = () => { reloads += 1; };
  window.I18n.settleMs = 20;
  const english = window.document.querySelector('[data-lang-select][value="en"]');
  english.checked = true;
  english.dispatchEvent(new window.Event('change', { bubbles: true }));
  assert.equal(reloads, 0, 'the reload waits for the thumb');
  await settle(40);
  assert.match(window.document.cookie, /lang=en/);
  assert.equal(reloads, 1);
});

test('with reduced motion the switch does not wait', async () => {
  const window = load({ boot: { lang: 'de', catalog }, body: pill });
  window.matchMedia = () => ({ matches: true });
  let reloads = 0;
  window.I18n.reload = () => { reloads += 1; };
  window.I18n.settleMs = 10000;
  const english = window.document.querySelector('[data-lang-select][value="en"]');
  english.checked = true;
  english.dispatchEvent(new window.Event('change', { bubbles: true }));
  await settle(5);
  assert.equal(reloads, 1);
});

test('the settings page can hide and show the pill without a reload', () => {
  const window = load({ boot: { lang: 'de', catalog }, body: pill });
  const element = window.document.querySelector('.lang-pill');
  window.document.dispatchEvent(new window.CustomEvent('language-switch-setting-changed', { detail: { visible: false } }));
  assert.equal(element.hidden, true);
  window.document.dispatchEvent(new window.CustomEvent('language-switch-setting-changed', { detail: { visible: true } }));
  assert.equal(element.hidden, false);
});

test('other selects do not touch the language', async () => {
  const window = load({ boot: { lang: 'de', catalog }, body: '<select id="other"><option value="x">x</option></select>' });
  let reloads = 0;
  window.I18n.reload = () => { reloads += 1; };
  window.document.getElementById('other').dispatchEvent(new window.Event('change', { bubbles: true }));
  await settle(window.I18n.settleMs + 20);
  assert.equal(reloads, 0);
});

test('alpine:init registers the $t and $tn magics', () => {
  const window = load({ boot: { lang: 'de', catalog } });
  const magics = {};
  window.Alpine = { magic: (name, factory) => { magics[name] = factory; } };
  window.document.dispatchEvent(new window.Event('alpine:init'));
  assert.equal(magics.t()('greet', { name: 'Sam' }), 'Hallo Sam');
  assert.equal(magics.tn()('file', 2), '2 Dateien');
});

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadScripts } from './helpers/load.mjs';

const load = (html) => loadScripts(['i18n.js'], { html });

test('t() liefert den Text aus dem Katalog', () => {
  const { window } = load();
  window.I18n.catalog = { 'app.title.install': 'Energy Node einrichten' };
  assert.equal(window.I18n.t('app.title.install'), 'Energy Node einrichten');
});

test('ein fehlender Schluessel wird als Schluessel gezeigt', () => {
  const { window } = load();
  window.I18n.catalog = {};
  assert.equal(window.I18n.t('precheck.heading'), 'precheck.heading');
});

test('Platzhalter werden gefuellt, fehlende bleiben stehen', () => {
  const { window } = load();
  window.I18n.catalog = { 'run.progress': 'Schritt {current} von {total}: {name}' };
  assert.equal(window.I18n.t('run.progress', { current: 4, total: 7, name: 'Tailscale' }), 'Schritt 4 von 7: Tailscale');
  assert.equal(window.I18n.t('run.progress', { current: 4 }), 'Schritt 4 von {total}: {name}');
});

test('tn() waehlt one bei genau 1 und other sonst', () => {
  const { window } = load();
  window.I18n.catalog = {
    'precheck.hint.warn.one': '{n} Hinweis, blockiert nicht',
    'precheck.hint.warn.other': '{n} Hinweise, blockieren nicht',
  };
  assert.equal(window.I18n.tn('precheck.hint.warn', 1), '1 Hinweis, blockiert nicht');
  assert.equal(window.I18n.tn('precheck.hint.warn', 3), '3 Hinweise, blockieren nicht');
  assert.equal(window.I18n.tn('precheck.hint.warn', 0), '0 Hinweise, blockieren nicht');
});

test('number() schreibt aus, wo der Katalog es kann, und faellt sonst auf Ziffern', () => {
  const { window } = load();
  window.I18n.catalog = { 'number.7': 'Sieben', 'number.3': 'drei' };
  assert.equal(window.I18n.number(7), 'Sieben');
  assert.equal(window.I18n.number(3), 'drei');
  assert.equal(window.I18n.number(42), '42');
});

test('apply() setzt data-i18n-Texte und -Attribute', () => {
  const { window } = load(`<!doctype html><html><body>
    <h1 data-i18n="app.title.install">alt</h1>
    <input data-i18n-placeholder="field.address">
    <button data-i18n-title="action.connect">x</button>
  </body></html>`);
  window.I18n.catalog = { 'app.title.install': 'Titel', 'field.address': 'Adresse', 'action.connect': 'Verbinden' };
  window.I18n.apply(window.document.body);
  assert.equal(window.document.querySelector('h1').textContent, 'Titel');
  assert.equal(window.document.querySelector('input').getAttribute('placeholder'), 'Adresse');
  assert.equal(window.document.querySelector('button').getAttribute('title'), 'Verbinden');
});

test('load() holt den Katalog, merkt die Sprache und setzt lang am Dokument', async () => {
  const { window } = load();
  const calls = [];
  window.fetch = async (url) => {
    calls.push(url);
    return { ok: true, json: async () => ({ 'app.title.install': 'Titel' }) };
  };
  await window.I18n.load('', 'tok', 'en');
  assert.equal(window.I18n.lang, 'en');
  assert.ok(calls[0].includes('/api/catalog/en'), calls[0]);
  assert.equal(window.localStorage.getItem('energy-node-installer.lang'), 'en');
  assert.equal(window.document.documentElement.getAttribute('lang'), 'en');
});

test('die gemerkte Sprache gewinnt gegen die Vorauswahl des Servers', () => {
  const { window } = load();
  window.localStorage.setItem('energy-node-installer.lang', 'en');
  assert.equal(window.I18n.preferred('de'), 'en');
  window.localStorage.removeItem('energy-node-installer.lang');
  assert.equal(window.I18n.preferred('de'), 'de');
});

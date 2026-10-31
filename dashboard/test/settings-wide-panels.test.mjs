// Der Schalter "Alle Tabs in voller Breite" in der Karte Tab-Breite. Geprueft
// wird das echte Template: x-on:change und x-bind:checked aus settings.html
// laufen hier gegen die Komponente, so wie Alpine sie auswertet. Ein Test, der
// die Logik nur nachbaut, faende einen Fehler im Markup nie.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { attachStores } from './helpers/notify-stores.mjs';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const webui = path.join(here, '..', 'internal', 'webui');
const scriptSource = fs.readFileSync(path.join(webui, 'static', 'js', 'settings.page.js'), 'utf8');
const templateSource = fs.readFileSync(path.join(webui, 'templates', 'settings.html'), 'utf8');

const ALL_TABS = ['overview', 'devices', 'history', 'config', 'energy', 'layout', 'devicemap', 'diagnostics', 'settings', 'automations'];

class FakeChoices {
  constructor() { this.currentValues = []; }
  removeActiveItems() { this.currentValues = []; }
  setChoiceByValue(values) { this.currentValues = [...this.currentValues, ...values]; }
  getValue(asValue) { return asValue ? [...this.currentValues] : this.currentValues.map((value) => ({ value })); }
}

function createPanel() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: 'https://dashboard.local/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  installI18n(dom.window);
  vm.runInContext(scriptSource, context);
  const component = factories.settingsPanel();
  attachStores(component);
  component.widePanelsChoices = new FakeChoices();
  return { component, context, dom };
}

// Die Attribute des Schalters aus dem Template. Der Wert von x-on:change
// enthaelt "=>", deshalb wird nicht ueber die schliessende Klammer des Tags
// gesucht, sondern bis zum </label> des Fragments.
function switchAttribute(name) {
  const start = templateSource.indexOf('id="wide-panels-all"');
  assert.notEqual(start, -1, 'Schalter #wide-panels-all fehlt im Template');
  const block = templateSource.slice(start, templateSource.indexOf('</label>', start));
  const match = block.match(new RegExp(`${name}="([^"]*)"`));
  assert.ok(match, `Attribut ${name} fehlt am Schalter`);
  return match[1];
}

// Fuehrt einen Alpine-Ausdruck gegen die Komponente aus. Lesen und Schreiben
// der Komponentenfelder laufen ueber einen Proxy, damit "widePanels = ..."
// im Handler die Komponente aendert.
function scopeRun(context, component, code, event) {
  const scope = new Proxy({}, {
    has: (_, key) => key === '$event' || key in component,
    get: (_, key) => (key === '$event' ? event : component[key]),
    set: (_, key, value) => { component[key] = value; return true; },
  });
  const fn = vm.runInContext(`(function (scope) { with (scope) { ${code} } })`, context);
  return fn(scope);
}

function toggle(panel, checked) {
  scopeRun(panel.context, panel.component, switchAttribute('x-on:change'), { target: { checked } });
}

function isChecked(panel) {
  return scopeRun(panel.context, panel.component, `return (${switchAttribute('x-bind:checked')})`, undefined);
}

// Komponentenarrays stammen aus dem vm-Kontext und sind nicht deepEqual-
// vergleichbar mit Arrays dieser Realm.
const plain = (value) => JSON.parse(JSON.stringify(value));

test('Umlegen an setzt alle zehn Tabs in Reihenfolge und schreibt sie in die Auswahl', () => {
  const panel = createPanel();
  toggle(panel, true);
  assert.deepEqual(plain(panel.component.widePanels), ALL_TABS);
  assert.deepEqual(plain(panel.component.widePanelsChoices.currentValues), ALL_TABS);
  assert.equal(isChecked(panel), true);
  panel.dom.window.close();
});

test('Umlegen aus leert die Auswahl', () => {
  const panel = createPanel();
  toggle(panel, true);
  toggle(panel, false);
  assert.deepEqual(plain(panel.component.widePanels), []);
  assert.deepEqual(plain(panel.component.widePanelsChoices.currentValues), []);
  assert.equal(isChecked(panel), false);
  panel.dom.window.close();
});

test('ein einzeln abgewaehlter Tab schaltet den Schalter aus', () => {
  const panel = createPanel();
  toggle(panel, true);
  panel.component.widePanels = ALL_TABS.filter((key) => key !== 'layout');
  assert.equal(isChecked(panel), false);
  panel.component.widePanels = [...ALL_TABS];
  assert.equal(isChecked(panel), true);
  panel.dom.window.close();
});

test('Speichern schreibt nach dem Umlegen dieselbe Liste wie die Mehrfachauswahl', () => {
  const switched = createPanel();
  toggle(switched, true);

  const multiselect = createPanel();
  multiselect.component.widePanelsChoices.setChoiceByValue(ALL_TABS);
  multiselect.component.widePanels = multiselect.component.syncFromChoices(
    multiselect.component.widePanelsChoices,
    multiselect.component.widePanelOptions,
  );

  assert.deepEqual(plain(switched.component.payload().wide_panels), plain(multiselect.component.payload().wide_panels));
  assert.deepEqual(plain(switched.component.payload().wide_panels), ALL_TABS);
  switched.dom.window.close();
  multiselect.dom.window.close();
});

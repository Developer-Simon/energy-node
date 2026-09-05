// Task 10: der Waechter vor ungespeicherten Aenderungen. confirmLeave() oeffnet
// das Themen-<dialog> #layout-guard-modal statt window.confirm und loest auf
// den geklickten Knopf auf. jsdom 25 kennt die <dialog>-API nicht
// (showModal/close sind undefined, siehe notify.test.mjs) - layout-editor.js
// faellt darum auf das open-Attribut zurueck, was fuer diese Tests reicht.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { attachStores } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'layout-editor.js'),
  'utf8',
);

// Minimaler Ersatz fuer eine GridStack-Instanz - genug Oberflaeche, damit
// init()/mount() nicht stolpern. Kopie aus layout-editor.test.mjs.
function fakeGridStackClass(initCalls) {
  return class FakeGridStack {
    static init(options, el) {
      const instance = {
        options, el, nodes: [], listeners: [], destroyed: false,
        columnCount: options.column,
        compactCalls: [],
        load(items) { this.nodes = items.map(item => ({...item})); },
        addWidget(item) {
          const node = {...item};
          this.nodes.push(node);
          this.listeners.forEach(cb => cb());
          return {gridstackNode: node};
        },
        removeWidget(target) {
          const node = target?.gridstackNode;
          this.nodes = this.nodes.filter(n => n !== node);
          this.listeners.forEach(cb => cb());
        },
        save(includeContent) {
          return this.nodes.map(({content, ...rest}) => (includeContent ? {...rest, content} : {...rest}));
        },
        on(_events, cb) { this.listeners.push(cb); },
        destroy() { this.destroyed = true; },
        column(count) { this.columnCount = count; },
        getColumn() { return this.columnCount; },
        getGridItems() { return this.nodes.map(node => ({gridstackNode: node})); },
        update(target, changes) {
          const node = target?.gridstackNode || target;
          Object.assign(node, changes);
          return this;
        },
        compact(layout) { this.compactCalls.push(layout); },
      };
      initCalls.push(instance);
      return instance;
    }
  };
}

// Der Standard-Body traegt das Waechter-<dialog>, damit createEditor() ohne
// Argument funktioniert. Ein .layout-grid ist mit dabei, damit mount() (falls
// ein Test es ausloest) nicht ins Leere greift.
const DEFAULT_BODY = '<div class="layout-grid"></div>'
  + '<dialog id="layout-guard-modal"><button data-guard-cancel></button><button data-guard-discard></button></dialog>';

// Wie createEditor() in layout-editor.test.mjs: bodyHTML kommt in den
// JSDOM-Body, init() bindet die Listener und legt window.__layoutEditor__ an,
// global.document wird auf dasselbe document gesetzt, weil confirmLeave() es
// blank anspricht.
function createEditor(bodyHTML = DEFAULT_BODY) {
  const dom = new JSDOM(`<!doctype html><html><body>${bodyHTML}</body></html>`, { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  dom.window.fetch = async () => { throw new Error('fetch should not be called'); };
  const initCalls = [];
  dom.window.GridStack = fakeGridStackClass(initCalls);
  vm.runInContext(scriptSource, context);
  global.document = dom.window.document;
  const editor = factory();
  editor.$nextTick = fn => fn();
  const stores = attachStores(editor);
  editor.init();
  return { dom, editor, window: dom.window, document: dom.window.document, initCalls, factory, stores };
}

test('ohne Aenderung fragt niemand', async () => {
  const { editor } = createEditor();
  editor.unsaved = false;
  assert.equal(await editor.confirmLeave('tab'), true);
});

test('mit Aenderung oeffnet das Themen-Modal, nicht window.confirm', async () => {
  const { dom, editor } = createEditor();
  dom.window.confirm = () => { throw new Error('window.confirm ist verboten'); };
  editor.unsaved = true;
  const pending = editor.confirmLeave('tab');
  assert.ok(dom.window.document.getElementById('layout-guard-modal').open);
  dom.window.document.querySelector('[data-guard-discard]').click();
  assert.equal(await pending, true);
});

test('Abbrechen haelt den Editor fest', async () => {
  const { dom, editor } = createEditor();
  editor.unsaved = true;
  const pending = editor.confirmLeave('mode');
  dom.window.document.querySelector('[data-guard-cancel]').click();
  assert.equal(await pending, false);
});

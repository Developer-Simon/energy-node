// devicesPanel (dashboard.js) laedt device-modal.js erst beim ersten Oeffnen
// eines Geraets. Der Test spielt den Browser: das angehaengte <script> wird
// per vm im selben Kontext ausgefuehrt und loest danach onload aus.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const js = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');

function lazyPanel({ fail = false } = {}) {
  const dom = new JSDOM('<!doctype html><html><head></head><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, magic() {}, directive() {} };
  dom.window.setInterval = () => 0;
  dom.window.deviceTileMixin = () => ({});
  installI18n(dom.window);
  vm.runInContext(js('dashboard.js'), context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  const loaded = [];
  const append = dom.window.document.head.append.bind(dom.window.document.head);
  dom.window.document.head.append = node => {
    append(node);
    loaded.push(node.src);
    queueMicrotask(() => {
      if (fail) { node.onerror(); return; }
      vm.runInContext(js('device-modal.js'), context);
      node.onload();
    });
  };
  const panel = factories.devicesPanel();
  const template = dom.window.document.createElement('template');
  template.dataset.script = '/static/js/device-modal.js?v=1';
  panel.$refs = { deviceModalTemplate: template };
  panel.$nextTick = () => Promise.resolve();
  const toasts = [];
  panel.$store = { toasts: { push: (message, level) => toasts.push(level) } };
  panel.testWindow = dom.window;
  return { panel, loaded, toasts };
}

test('selectDevice laedt device-modal.js einmal und oeffnet dann das Geraet', async () => {
  const { panel, loaded } = lazyPanel();
  assert.equal(panel.openDevice, undefined, 'vor dem ersten Oeffnen fehlt das Modal');
  const opened = [];
  // Das Mixin ueberschreibt nichts, was schon da ist - der Recorder kommt
  // also nach dem Laden ueber attachDeviceModal.
  panel.testWindow.fetch = async () => ({ ok: true, status: 200, json: async () => ({ id: 'a', entities: [] }) });
  await panel.selectDevice('a');
  assert.deepEqual(loaded, ['http://localhost/static/js/device-modal.js?v=1']);
  assert.equal(panel.deviceModalReady, true);
  assert.equal(panel.selectedDeviceId, 'a');
  panel.openDevice = id => { opened.push(id); };
  await panel.selectDevice('b');
  assert.equal(loaded.length, 1, 'kein zweites Laden');
  assert.deepEqual(opened, ['b']);
});

test('ein fehlgeschlagenes Laden meldet sich und laesst einen neuen Versuch zu', async () => {
  const { panel, loaded, toasts } = lazyPanel({ fail: true });
  await panel.selectDevice('a');
  assert.deepEqual(toasts, ['critical']);
  assert.equal(panel.deviceModalReady, false);
  assert.equal(panel._deviceModalLoad, null);
  await panel.selectDevice('a');
  assert.equal(loaded.length, 2);
});

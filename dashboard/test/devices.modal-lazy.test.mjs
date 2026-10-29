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

// Template-Waechter fuer die Favoritenreihe, Sterne, Icon-Popover und das
// Mehr-Menue (devices.html). Sie lesen das Template als Text, weil das Modal
// erst im Browser entsteht.
const template = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'devices.html'), 'utf8');
const defineBlock = name => {
  const start = template.indexOf(`{{define "${name}"}}`);
  assert.notEqual(start, -1, `{{define "${name}"}} fehlt`);
  return template.slice(start, template.indexOf('{{end}}', start));
};
const openTag = (source, pattern) => {
  const match = source.match(pattern);
  assert.ok(match, `Tag passend zu ${pattern} fehlt`);
  return match[0];
};

test('die Favoritenreihe device-modal-hero steht im Modal', () => {
  assert.match(template, /class="device-modal-hero"/);
  assert.match(template, /x-effect="favoriteLimitHit/);
});

test('jede Kachel-Vorlage hat einen Stern nur mit canEditAppearance', () => {
  for (const name of ['device-modal-stat-tile', 'device-modal-control-tile']) {
    const star = openTag(defineBlock(name), /<button[^>]*device-modal-star[^>]*>/);
    assert.match(star, /x-show="canEditAppearance"/, `${name}: Stern ohne x-show`);
    assert.match(star, /aria-pressed/, `${name}: Stern ohne aria-pressed`);
    assert.match(star, /toggleFavorite\(entity\)/, `${name}: Stern ohne toggleFavorite`);
  }
});

test('die alte Favoritenauswahl und das Anheft-Feld sind aus dem Template verschwunden', () => {
  assert.doesNotMatch(template, /favoritesSelect/);
  assert.doesNotMatch(template, /pin_favorites/);
  assert.doesNotMatch(template, /device-modal-pinned/);
});

test('das Mehr-Menue ist ein role="menu" mit genau einer Aktion je Befehl', () => {
  const menu = openTag(template, /<div class="device-modal-menu"[^>]*>/);
  assert.match(menu, /role="menu"/);
  assert.equal((template.match(/role="menuitem"/g) || []).length, 3);
  assert.equal((template.match(/reloadDevice\(\)/g) || []).length, 1, 'reloadDevice nur einmal');
  assert.equal((template.match(/ignoreDevice\(selectedDeviceId\)/g) || []).length, 1, 'ignoreDevice nur einmal');
  assert.equal((template.match(/openDiscoveryDelete\(selectedDeviceId\)/g) || []).length, 1, 'Discovery-Loeschen nur einmal');
  assert.doesNotMatch(template, /device-modal-manage-row/);
});

test('das Icon im Modal-Kopf oeffnet ein Popover mit dem Icon-Picker auf saveIcon', () => {
  const icon = openTag(template, /<button[^>]*device-modal-topbar-icon[^>]*>/);
  assert.match(icon, /aria-haspopup="dialog"/);
  assert.match(icon, /aria-expanded/);
  assert.match(icon, /canEditAppearance/);
  assert.match(template, /class="device-modal-iconpop"/);
  assert.match(template, /dict "Current" "draftIconName" "Default" "defaultIconName" "Pick" "saveIcon"/);
});

// Analyse-Tab (Aufgabe 12): keine Tabellen mehr, je Entitaet ein Akkordeon.
const managementPanel = () => {
  const start = template.indexOf('id="device-modal-panel-management"');
  assert.notEqual(start, -1, 'Management-Panel fehlt');
  return template.slice(start, template.indexOf('device-modal-underline', start));
};

test('das Management-Panel hat keine <table> mehr', () => {
  assert.doesNotMatch(managementPanel(), /<table/);
  assert.doesNotMatch(managementPanel(), /table-scroll/);
});

test('der Analyse-Tab ist nur mit canViewAnalysis sichtbar und heisst devices.analysis.title', () => {
  const pair = openTag(template, /<div class="device-modal-pair" data-tab="management"[^>]*>/);
  assert.match(pair, /x-show="canViewAnalysis"/);
  assert.match(template, /\{\{t "devices\.analysis\.title"\}\}/);
  assert.doesNotMatch(template, /devices\.manage_analyze\.title/);
});

test('das Template liest kein recentMessages mehr', () => {
  assert.doesNotMatch(template, /recentMessages/);
});

test('die Aktivitaet im Geraete-Abschnitt hat keine isAccordion-Bedingung', () => {
  const info = defineBlock('device-modal-info-body');
  assert.doesNotMatch(info, /isAccordion/);
  assert.match(info, /activityLog/);
});

test('jede Entitaet im Analyse-Tab ist ein device-modal-entity mit aria-expanded', () => {
  const panel = managementPanel();
  assert.match(panel, /class="device-modal-entity/);
  const header = openTag(panel, /<button[^>]*device-modal-entity-head[^>]*>/);
  assert.match(header, /aria-expanded/);
  assert.match(header, /toggleAnalysisEntity\(/);
});

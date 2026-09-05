// Beide Stores werden ohne Alpine geprüft: notify.js legt sie unter
// window.__notifyStores ab, so wie notifications.js es mit
// window.__automationNotifications macht. jsdom implementiert <dialog>
// nicht (showModal/close sind undefined), deshalb enthält notify.js
// bewusst keinen DOM-Zugriff - der Klebe-Code dafür ist eine x-effect-
// Zeile in base.html.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'notify.js'),
  'utf8',
);

// setTimeout/clearTimeout werden ersetzt, damit Tests sehen koennen, ob ein
// Timer gestellt bzw. geloescht wurde, ohne acht Sekunden zu warten.
// timers[id - 1] ist der gestellte Timer; .cleared markiert clearTimeout.
function loadNotify() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  const timers = [];
  dom.window.setTimeout = (fn, ms) => { timers.push({ fn, ms, cleared: false }); return timers.length; };
  dom.window.clearTimeout = (id) => { if (timers[id - 1]) timers[id - 1].cleared = true; };
  vm.runInContext(scriptSource, context);
  const { toasts, modal } = dom.window.__notifyStores;
  return { toasts, modal, timers, window: dom.window };
}

test('push() haengt einen Eintrag an und liefert dessen id', () => {
  const { toasts } = loadNotify();
  const id = toasts.push('Layout gespeichert.');
  assert.equal(toasts.items.length, 1);
  assert.equal(toasts.items[0].id, id);
  assert.equal(toasts.items[0].message, 'Layout gespeichert.');
  assert.equal(toasts.items[0].severity, 'info');
  assert.equal(toasts.items[0].count, 1);
});

test('der sechste push() verdraengt den aeltesten, items bleibt bei 5', () => {
  const { toasts } = loadNotify();
  ['a', 'b', 'c', 'd', 'e', 'f'].forEach((message) => toasts.push(message, 'warning'));
  assert.equal(toasts.items.length, 5);
  assert.equal(toasts.items[0].message, 'b');
  assert.equal(toasts.items[4].message, 'f');
});

test('der Timer des verdraengten Toasts wird geloescht', () => {
  const { toasts, timers } = loadNotify();
  const first = toasts.push('a');
  ['b', 'c', 'd', 'e', 'f'].forEach((message) => toasts.push(message));
  assert.equal(timers[first - 1].cleared, true);
});

test('gleicher Text und gleicher Schweregrad zaehlen hoch statt anzuhaengen - an gleicher Position', () => {
  const { toasts } = loadNotify();
  const id = toasts.push('Anfrage fehlgeschlagen', 'critical');
  toasts.push('etwas anderes', 'critical');
  const again = toasts.push('Anfrage fehlgeschlagen', 'critical');
  assert.equal(again, id, 'die Zusammenfassung liefert die id des bestehenden Toasts');
  assert.equal(toasts.items.length, 2);
  assert.equal(toasts.items[0].message, 'Anfrage fehlgeschlagen', 'der Eintrag bleibt an seiner Position');
  assert.equal(toasts.items[0].count, 2);
});

test('gleicher Text bei anderem Schweregrad erzeugt einen zweiten Eintrag', () => {
  const { toasts } = loadNotify();
  toasts.push('Achtung', 'info');
  toasts.push('Achtung', 'warning');
  assert.equal(toasts.items.length, 2);
});

test('info startet einen 8-Sekunden-Timer, warning und critical nicht', () => {
  const info = loadNotify();
  info.toasts.push('gespeichert');
  assert.equal(info.timers.length, 1);
  assert.equal(info.timers[0].ms, 8000);

  const warning = loadNotify();
  warning.toasts.push('achtung', 'warning');
  assert.equal(warning.timers.length, 0);

  const critical = loadNotify();
  critical.toasts.push('kaputt', 'critical');
  assert.equal(critical.timers.length, 0);
});

test('eine zusammengefasste info-Meldung startet den Timer neu', () => {
  const { toasts, timers } = loadNotify();
  const id = toasts.push('gespeichert');
  toasts.push('gespeichert');
  assert.equal(timers.length, 2, 'ein zweiter Timer wurde gestellt');
  assert.equal(timers[0].cleared, true, 'der erste Timer wurde geloescht');
  assert.equal(toasts.items[0].id, id);
});

test('dismiss() entfernt den Eintrag und loescht dessen Timer', () => {
  const { toasts, timers } = loadNotify();
  const id = toasts.push('gespeichert');
  toasts.dismiss(id);
  assert.equal(toasts.items.length, 0);
  assert.equal(timers[id - 1].cleared, true);
});

test('der abgelaufene Timer entfernt den Toast', () => {
  const { toasts, timers } = loadNotify();
  toasts.push('gespeichert');
  timers[0].fn();
  assert.equal(toasts.items.length, 0);
});

test('ein unbekannter Schweregrad landet als info', () => {
  const { toasts } = loadNotify();
  toasts.push('was auch immer', 'katastrophal');
  assert.equal(toasts.items[0].severity, 'info');
});

test('leere Meldungen werden ignoriert und liefern null', () => {
  const { toasts } = loadNotify();
  assert.equal(toasts.push(''), null);
  assert.equal(toasts.push('   '), null);
  assert.equal(toasts.push(undefined), null);
  assert.equal(toasts.items.length, 0);
});

test('clear() leert die Liste und loescht alle Timer', () => {
  const { toasts, timers } = loadNotify();
  toasts.push('a');
  toasts.push('b');
  toasts.clear();
  assert.equal(toasts.items.length, 0);
  assert.equal(timers.every((timer) => timer.cleared), true);
});

test('alpine:init registriert beide Stores', () => {
  const { window, toasts, modal } = loadNotify();
  const registered = {};
  window.Alpine = { store: (name, value) => { registered[name] = value; } };
  window.document.dispatchEvent(new window.Event('alpine:init'));
  assert.equal(registered.toasts, toasts);
  assert.equal(registered.modal, modal);
});

test('confirm() setzt open und uebernimmt die Vorgabewerte', () => {
  const { modal } = loadNotify();
  modal.confirm({ title: 'Geraet wirklich ignorieren?' });
  assert.equal(modal.open, true);
  assert.equal(modal.title, 'Geraet wirklich ignorieren?');
  assert.equal(modal.body, '');
  assert.equal(modal.confirmLabel, 'Bestätigen');
  assert.equal(modal.cancelLabel, 'Abbrechen');
  assert.equal(modal.danger, false);
});

test('confirm() uebernimmt uebergebene Labels, body und danger', () => {
  const { modal } = loadNotify();
  modal.confirm({
    title: 'Geraet wirklich ignorieren?',
    body: 'Das Geraet verschwindet aus der Uebersicht.',
    confirmLabel: 'Ignorieren',
    cancelLabel: 'Doch nicht',
    danger: true,
  });
  assert.equal(modal.body, 'Das Geraet verschwindet aus der Uebersicht.');
  assert.equal(modal.confirmLabel, 'Ignorieren');
  assert.equal(modal.cancelLabel, 'Doch nicht');
  assert.equal(modal.danger, true);
});

test('resolve(true) loest das Promise mit true auf und setzt open zurueck', async () => {
  const { modal } = loadNotify();
  const pending = modal.confirm({ title: 'Sicher?' });
  modal.resolve(true);
  assert.equal(await pending, true);
  assert.equal(modal.open, false);
});

test('resolve(false) loest mit false auf', async () => {
  const { modal } = loadNotify();
  const pending = modal.confirm({ title: 'Sicher?' });
  modal.resolve(false);
  assert.equal(await pending, false);
});

test('ein zweiter resolve() zum selben Vorgang tut nichts', async () => {
  const { modal } = loadNotify();
  const pending = modal.confirm({ title: 'Sicher?' });
  modal.resolve(true);
  modal.resolve(false);
  assert.equal(await pending, true, 'der zweite resolve darf das Ergebnis nicht umbiegen');
});

test('confirm() bei bereits offenem Modal loest sofort mit false auf', async () => {
  const { modal, window } = loadNotify();
  const warnings = [];
  window.console.warn = (message) => warnings.push(message);
  modal.confirm({ title: 'erstens' });
  const second = await modal.confirm({ title: 'zweitens' });
  assert.equal(second, false);
  assert.equal(warnings.length, 1, 'der Bug wird nicht verschwiegen');
  assert.equal(modal.title, 'erstens', 'das offene Modal wird nicht ueberschrieben');
});

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
import { installI18n } from './helpers/i18n.mjs';

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
  installI18n(dom.window);
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

test('jede Schwere laeuft doppelt so langsam ab wie die vorige: 8 s, 16 s, 32 s', () => {
  const expected = { info: 8000, warning: 16000, critical: 32000 };
  Object.entries(expected).forEach(([severity, ms]) => {
    const { toasts, timers } = loadNotify();
    toasts.push('meldung', severity);
    assert.equal(timers.length, 1, `${severity} stellt einen Timer`);
    assert.equal(timers[0].ms, ms, `${severity} laeuft ${ms} ms`);
    assert.equal(toasts.items[0].durationMs, ms);
  });
});

test('eine zusammengefasste Meldung startet den Ablauf neu und zaehlt run hoch', () => {
  const { toasts, timers } = loadNotify();
  const id = toasts.push('Anfrage fehlgeschlagen', 'critical');
  toasts.push('Anfrage fehlgeschlagen', 'critical');
  assert.equal(timers.length, 2, 'ein zweiter Timer wurde gestellt');
  assert.equal(timers[0].cleared, true, 'der erste Timer wurde geloescht');
  assert.equal(timers[1].ms, 32000);
  assert.equal(toasts.items[0].id, id);
  assert.equal(toasts.items[0].run, 1);
});

test('dismiss() markiert den Eintrag als leaving und loescht den Ablauf-Timer', () => {
  const { toasts, timers } = loadNotify();
  const id = toasts.push('gespeichert');
  toasts.dismiss(id);
  assert.equal(toasts.items.length, 1, 'der Eintrag bleibt fuer die Austrittsanimation stehen');
  assert.equal(toasts.items[0].state, 'leaving');
  assert.equal(timers[0].cleared, true);
});

test('dismiss(id, "swipe") markiert den Eintrag als swiped', () => {
  const { toasts } = loadNotify();
  const id = toasts.push('gespeichert');
  toasts.dismiss(id, 'swipe');
  assert.equal(toasts.items[0].state, 'swiped');
});

test('nach der Austrittsanimation faellt der Eintrag aus der Liste', () => {
  const { toasts, timers } = loadNotify();
  const id = toasts.push('gespeichert');
  toasts.dismiss(id);
  assert.equal(timers[1].ms, 520);
  timers[1].fn();
  assert.equal(toasts.items.length, 0);
});

test('ein zweites dismiss() desselben Toasts stellt keinen zweiten Austritts-Timer', () => {
  const { toasts, timers } = loadNotify();
  const id = toasts.push('gespeichert');
  toasts.dismiss(id);
  toasts.dismiss(id, 'swipe');
  assert.equal(timers.length, 2);
  assert.equal(toasts.items[0].state, 'leaving');
});

test('eine Meldung, die gerade geht, wird nicht hochgezaehlt, sondern neu angelegt', () => {
  const { toasts } = loadNotify();
  const id = toasts.push('gespeichert');
  toasts.dismiss(id);
  const again = toasts.push('gespeichert');
  assert.notEqual(again, id);
  assert.equal(toasts.items.length, 2);
  assert.equal(toasts.items[0].count, 1);
});

test('hold() haelt den Ablauf an und release() setzt mit der Restzeit fort', () => {
  const { toasts, timers, window } = loadNotify();
  const realNow = window.Date.now;
  let now = 1_000_000;
  window.Date.now = () => now;
  try {
    const id = toasts.push('achtung', 'warning');
    now += 5000;
    toasts.hold(id, 'hover');
    assert.equal(timers[0].cleared, true);
    assert.equal(toasts.items[0].paused, true);
    assert.equal(toasts.items[0].remainingMs, 11000);
    now += 60000;
    toasts.release(id, 'hover');
    assert.equal(toasts.items[0].paused, false);
    assert.equal(timers.length, 2);
    assert.equal(timers[1].ms, 11000);
  } finally {
    window.Date.now = realNow;
  }
});

test('mit zwei Gruenden laeuft der Ablauf erst nach dem letzten release() weiter', () => {
  const { toasts, timers } = loadNotify();
  const id = toasts.push('gespeichert');
  toasts.hold(id, 'hover');
  toasts.hold(id, 'focus');
  toasts.release(id, 'hover');
  assert.equal(toasts.items[0].paused, true);
  assert.equal(timers.length, 1);
  toasts.release(id, 'focus');
  assert.equal(toasts.items[0].paused, false);
  assert.equal(timers.length, 2);
});

test('nach dem Pausieren bleibt mindestens 1,5 s Restzeit', () => {
  const { toasts, timers, window } = loadNotify();
  const realNow = window.Date.now;
  let now = 1_000_000;
  window.Date.now = () => now;
  try {
    const id = toasts.push('gespeichert');
    now += 7900;
    toasts.hold(id, 'hover');
    toasts.release(id, 'hover');
    assert.equal(timers[1].ms, 1500);
  } finally {
    window.Date.now = realNow;
  }
});

test('eine Wiederholung unter der Maus startet den Ablauf erst nach dem Verlassen', () => {
  const { toasts, timers } = loadNotify();
  const id = toasts.push('gespeichert');
  toasts.hold(id, 'hover');
  toasts.push('gespeichert');
  assert.equal(timers.length, 1, 'kein neuer Timer, solange gehalten wird');
  assert.equal(toasts.items[0].paused, true);
  toasts.release(id, 'hover');
  assert.equal(timers.length, 2);
  assert.equal(timers[1].ms, 8000);
});

test('der abgelaufene Timer blendet den Toast aus und entfernt ihn danach', () => {
  const { toasts, timers } = loadNotify();
  toasts.push('gespeichert');
  timers[0].fn();
  assert.equal(toasts.items[0].state, 'leaving');
  timers[1].fn();
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

test('push() mit Aktion fuehrt run genau einmal aus, ein zweites runAction tut nichts', () => {
  const { toasts } = loadNotify();
  let runs = 0;
  const id = toasts.push('Regel angelegt', 'info', { action: { label: 'Rückgängig', run: () => { runs += 1; } } });
  toasts.runAction(id);
  toasts.runAction(id);
  assert.equal(runs, 1);
});

test('runAction() schliesst den Toast mit der Aktion', () => {
  const { toasts, timers } = loadNotify();
  const id = toasts.push('Regel angelegt', 'info', { action: { label: 'Rückgängig', run: () => {} } });
  toasts.runAction(id);
  assert.equal(toasts.items[0].state, 'leaving');
  assert.equal(timers[0].cleared, true);
});

test('runAction() bei unbekannter id oder ohne Aktion tut nichts und wirft nicht', () => {
  const { toasts } = loadNotify();
  const plain = toasts.push('gespeichert');
  assert.doesNotThrow(() => toasts.runAction(999));
  assert.doesNotThrow(() => toasts.runAction(plain));
  assert.equal(toasts.items[0].state, 'open');
});

test('ein Toast mit Aktion entdoppelt nicht, auch bei gleichem Text und Schweregrad', () => {
  const { toasts } = loadNotify();
  const action = { label: 'Rückgängig', run: () => {} };
  const first = toasts.push('Regel angelegt', 'info', { action });
  const second = toasts.push('Regel angelegt', 'info', { action });
  assert.notEqual(second, first);
  assert.equal(toasts.items.length, 2);
  assert.equal(toasts.items[0].count, 1);
  assert.equal(toasts.items[1].count, 1);
});

test('ein Toast ohne Aktion fasst keinen Toast mit Aktion zusammen und umgekehrt', () => {
  const { toasts } = loadNotify();
  toasts.push('Regel angelegt', 'info', { action: { label: 'Rückgängig', run: () => {} } });
  toasts.push('Regel angelegt');
  assert.equal(toasts.items.length, 2);
  assert.equal(toasts.items[0].count, 1);
});

test('der Toast mit Aktion laeuft in der Dauer seiner Schwere ab', () => {
  const { toasts, timers } = loadNotify();
  toasts.push('Regel angelegt', 'warning', { action: { label: 'Rückgängig', run: () => {} } });
  assert.equal(timers[0].ms, 16000);
  assert.equal(toasts.items[0].durationMs, 16000);
});

test('eine Aktion ohne run-Funktion wird ignoriert, der Toast bleibt ein normaler Toast', () => {
  const { toasts } = loadNotify();
  const id = toasts.push('gespeichert', 'info', { action: { label: 'Kaputt' } });
  assert.equal(toasts.items[0].action, null);
  assert.equal(toasts.items[0].id, id);
  toasts.push('gespeichert');
  assert.equal(toasts.items.length, 1, 'ohne Aktion wird weiterhin zusammengefasst');
});

test('ohne Label fällt die Aktion auf common.undo zurueck', () => {
  const { toasts } = loadNotify();
  toasts.push('gespeichert', 'info', { action: { run: () => {} } });
  assert.equal(toasts.items[0].action.label, 'Rückgängig');
});

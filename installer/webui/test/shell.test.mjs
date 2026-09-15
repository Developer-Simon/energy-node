import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadScripts } from './helpers/load.mjs';

const BOOT = {
  host: 'installer', entry_points: ['install', 'redeploy', 'diagnose'], needs_connection: true,
  bundle_version: 'v1.4.2', bundle_arch: 'armv6', language: 'de', language_fixed: false, languages: ['de', 'en'],
};
const DASHBOARD = {
  host: 'dashboard', entry_points: ['redeploy', 'diagnose'], needs_connection: false,
  bundle_version: 'v1.4.2', bundle_arch: 'armv6', language: 'de', language_fixed: true, languages: ['de', 'en'],
};

async function createShell({ bootstrap = BOOT, catalog = {}, stored = null, bootstrapError = null, hello } = {}) {
  const { window } = loadScripts(['i18n.js', 'api.js', 'app.js']);
  if (stored) {
    window.localStorage.setItem('energy-node-installer.lang', stored);
  }
  const factories = {};
  window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  if (hello !== undefined) {
    window.Events = { hello: async () => hello };
  }
  const requests = [];
  window.fetch = async (url) => {
    requests.push(String(url));
    if (String(url).startsWith('/api/bootstrap')) {
      if (bootstrapError) {
        return { ok: false, status: 401, json: async () => ({ error: bootstrapError, detail: '' }) };
      }
      return { ok: true, status: 200, json: async () => bootstrap };
    }
    if (String(url).startsWith('/api/catalog/')) {
      return { ok: true, status: 200, json: async () => catalog };
    }
    throw new Error('unexpected request ' + url);
  };
  window.document.dispatchEvent(new window.Event('alpine:init'));
  const shell = factories.installerShell();
  await shell.init();
  return { shell, window, requests };
}

// shell.stepperParts is built inside the jsdom vm context, so its arrays and
// objects come from a different realm than this file's array/object
// literals; assert.deepEqual (strict) rejects that as "not reference-equal"
// even when structurally identical. JSON.parse(JSON.stringify(...)) - called
// here, in this realm - normalizes the value before comparing (same pattern
// as dashboard/test/energy-board.page.test.mjs).
const plain = (value) => JSON.parse(JSON.stringify(value));
const classes = (shell) => plain(shell.stepperParts.map((part) => part.cls));

test('mit Verbindungsbildschirm startet die Shell auf Verbindung beim ersten Einstiegspunkt', async () => {
  const { shell, window } = await createShell();
  assert.equal(shell.ready, true);
  assert.equal(shell.screen, 'connect');
  assert.equal(shell.entry, 'install');
  assert.equal(shell.connected, false);
  assert.equal(window.Installer.shell, shell);
});

test('ein Wirt ohne Verbindungsbildschirm startet beim ersten Einstiegspunkt', async () => {
  const { shell } = await createShell({ bootstrap: DASHBOARD });
  assert.equal(shell.screen, 'preview');
  assert.equal(shell.entry, 'redeploy');
  assert.equal(shell.connected, true);
});

test('die gemerkte Sprache gewinnt, ausser der Wirt legt sie fest', async () => {
  const free = await createShell({ stored: 'en' });
  assert.equal(free.shell.lang, 'en');
  assert.ok(free.requests.some((url) => url.startsWith('/api/catalog/en')));

  const fixed = await createShell({ bootstrap: DASHBOARD, stored: 'en' });
  assert.equal(fixed.shell.lang, 'de');
});

test('der Titel folgt dem Einstiegspunkt', async () => {
  const { shell } = await createShell({ catalog: { 'app.title.install': 'Energy Node einrichten', 'app.title.diagnose': 'Diagnose' } });
  assert.equal(shell.title, 'Energy Node einrichten');
  shell.entry = 'diagnose';
  assert.equal(shell.title, 'Diagnose');
});

test('der Stepper der Erstinstallation hat fuenf Stationen und markiert den Weg', async () => {
  const { shell } = await createShell({ catalog: { 'stepper.precheck': 'Vorprüfung' } });
  shell.screen = 'precheck';
  assert.deepEqual(classes(shell), [
    'st-item done', 'st-conn filled', 'st-item active', 'st-conn', 'st-item', 'st-conn', 'st-item', 'st-conn', 'st-item',
  ]);
  const items = shell.stepperParts.filter((part) => part.item);
  assert.deepEqual(plain(items.map((item) => item.n)), [1, 2, 3, 4, 5]);
  assert.equal(items[1].label, 'Vorprüfung');
  assert.equal(items[0].done, true);
});

test('ohne Verbindungsbildschirm rueckt der Stepper auf', async () => {
  const { shell } = await createShell({ bootstrap: DASHBOARD });
  const items = shell.stepperParts.filter((part) => part.item);
  assert.deepEqual(plain(items.map((item) => item.key)), ['preview', 'run', 'result']);
  assert.equal(items[0].cls, 'st-item active');
});

test('die Konfiguration im Nur-Dienste-Modus steht im Stepper auf der Vorschau', async () => {
  const { shell } = await createShell();
  shell.entry = 'redeploy';
  shell.shared.servicesOnly = true;
  shell.screen = 'configure';
  const active = shell.stepperParts.find((part) => part.cls === 'st-item active');
  assert.equal(active.key, 'preview');
});

test('die Diagnose hat keinen Stepper', async () => {
  const { shell } = await createShell();
  shell.entry = 'diagnose';
  shell.screen = 'diagnose';
  assert.deepEqual(plain(shell.stepperParts), []);
});

test('der Einstiegs-Umschalter steht nur auf Bildschirmen, die nichts veraendern', async () => {
  const { shell } = await createShell();
  for (const [screen, visible] of [['connect', true], ['precheck', true], ['preview', true], ['diagnose', true], ['configure', false], ['run', false], ['result', false]]) {
    shell.screen = screen;
    assert.equal(shell.showEntrySwitch, visible, screen);
  }
  shell.screen = 'precheck';
  shell.mutating = true;
  assert.equal(shell.showEntrySwitch, false, 'waehrend eines Laufs nie');
});

test('der Sprachumschalter steht nur auf der Verbindung und nie bei fester Sprache', async () => {
  const { shell } = await createShell();
  assert.equal(shell.showLanguageSwitch, true);
  shell.screen = 'precheck';
  assert.equal(shell.showLanguageSwitch, false);

  const fixed = await createShell({ bootstrap: Object.assign({}, BOOT, { language_fixed: true }) });
  assert.equal(fixed.shell.showLanguageSwitch, false);
});

test('switchEntry fuehrt zum ersten Bildschirm und wird waehrend eines Laufs ignoriert', async () => {
  const { shell } = await createShell();
  shell.switchEntry('diagnose');
  assert.equal(shell.entry, 'diagnose');
  assert.equal(shell.screen, 'connect', 'ohne Verbindung geht jeder Einstieg ueber die Verbindung');

  shell.connected = true;
  shell.switchEntry('redeploy');
  assert.equal(shell.screen, 'preview');

  shell.mutating = true;
  shell.switchEntry('install');
  assert.equal(shell.entry, 'redeploy');
});

test('go und back setzen die Richtung und raeumen Fehler und Kopfzeilen-Zusaetze', async () => {
  const { shell } = await createShell();
  shell.error = { code: 'X', detail: '' };
  shell.bar.sub = 'Paket 1.4.2 · armv6';
  shell.go('precheck');
  assert.equal(shell.dir, 'forward');
  assert.equal(shell.error, null);
  assert.equal(shell.bar.sub, '');
  shell.back('connect');
  assert.equal(shell.dir, 'back');
  assert.equal(shell.screen, 'connect');
});

test('ein ApiError wird ueber error.<CODE> uebersetzt, unbekannt ueber error.unknown', async () => {
  const { shell, window } = await createShell({
    catalog: { 'error.NOT_CONNECTED': 'Keine Verbindung.', 'error.unknown': 'Unbekannter Fehler: {code}' },
  });
  shell.fail(new window.ApiError('NOT_CONNECTED', '', 409));
  assert.equal(shell.errorText, 'Keine Verbindung.');
  shell.fail(new window.ApiError('WHAT_IS_THIS', 'detail', 500));
  assert.equal(shell.errorText, 'Unbekannter Fehler: WHAT_IS_THIS');
  assert.equal(shell.error.detail, 'detail');
});

test('startRun sperrt, finishRun gibt frei und fuehrt ins Ergebnis', async () => {
  const { shell } = await createShell();
  shell.startRun({ run_id: 'run-1', seq: 4 }, { mode: 'install' });
  assert.equal(shell.mutating, true);
  assert.equal(shell.screen, 'run');
  assert.deepEqual(plain(shell.shared.run), { runId: 'run-1', mode: 'install', only: '', resumed: false });

  shell.finishRun({ ok: true });
  assert.equal(shell.mutating, false);
  assert.equal(shell.shared.run, null);
  assert.deepEqual(plain(shell.shared.lastRun), { ok: true });
  assert.equal(shell.screen, 'result');
});

test('openDiagnose wechselt den Einstieg oder bleibt in ihm', async () => {
  const { shell } = await createShell();
  shell.connected = true;
  shell.openDiagnose();
  assert.equal(shell.entry, 'diagnose');
  assert.equal(shell.screen, 'diagnose');
  shell.screen = 'result';
  shell.openDiagnose();
  assert.equal(shell.screen, 'diagnose');
});

test('der Chip zeigt user@host erst nach der Verbindung und nie in der Diagnose', async () => {
  const { shell } = await createShell();
  assert.equal(shell.chip, '');
  shell.afterConnect({ host: 'energy-node.local', user: 'pi' });
  assert.equal(shell.screen, 'precheck');
  assert.equal(shell.chip, 'pi@energy-node.local');
  shell.screen = 'diagnose';
  assert.equal(shell.chip, '');
});

test('switchLanguage laedt den Katalog der neuen Sprache', async () => {
  const { shell, requests } = await createShell();
  await shell.switchLanguage('en');
  assert.equal(shell.lang, 'en');
  assert.ok(requests.some((url) => url.startsWith('/api/catalog/en')));
});

test('ein gescheiterter Bootstrap zeigt den Fehler statt einer leeren Seite', async () => {
  const { shell } = await createShell({ bootstrapError: 'TOKEN_INVALID' });
  assert.equal(shell.ready, true);
  assert.equal(shell.error.code, 'TOKEN_INVALID');
});

test('ein laufender Lauf wird beim Laden erkannt und fuehrt in die Ausfuehrung', async () => {
  const { shell } = await createShell({ hello: { seq: 40, running: true, run_id: 'run-7', at: 1 } });
  assert.equal(shell.screen, 'run');
  assert.equal(shell.mutating, true);
  assert.equal(shell.connected, true);
  assert.deepEqual(plain(shell.shared.run), { runId: 'run-7', mode: '', only: '', resumed: true });
});

test('ohne laufenden Lauf ist alles bis hello.seq Vergangenheit', async () => {
  const { shell } = await createShell({ hello: { seq: 40, running: false, run_id: 'run-7', at: 1 } });
  assert.equal(shell.screen, 'connect');
  assert.equal(shell.shared.run, null);
});

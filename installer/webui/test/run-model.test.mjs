import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadScripts } from './helpers/load.mjs';
import { realCatalog } from './helpers/mount.mjs';
import { MANIFEST, SELECTION } from './helpers/fixtures.mjs';

const plain = (value) => JSON.parse(JSON.stringify(value));
const T0 = new Date(2026, 8, 13, 14, 18, 3).getTime();

function setup() {
  const { window } = loadScripts(['i18n.js', 'format.js', 'services.js', 'run-model.js']);
  window.I18n.catalog = realCatalog('de');
  const shell = { t: (key, params) => window.I18n.t(key, params) };
  const groups = window.Services.runGroups(MANIFEST, SELECTION, shell);
  return { M: window.RunModel, groups, t: shell.t };
}

// play: spielt [type, data] mit at-Versatz in Sekunden ab T0 ein.
function play(M, model, events) {
  for (const [type, data, seconds] of events) {
    M.apply(model, type, Object.assign({ at: T0 + seconds * 1000 }, data));
  }
}

const DRAFT_RUN = [
  ['run-started', { run_id: 'run-1', mode: 'install', only: '' }, 0],
  ['step', { id: '10', state: 'begin' }, 0], ['step', { id: '10', state: 'ok' }, 102],
  ['step', { id: '20', state: 'begin' }, 102], ['log', { step_id: '20', line: 'mosquitto_passwd -b energynode ***' }, 103],
  ['step', { id: '20', state: 'ok' }, 123],
  ['step', { id: '30', state: 'begin' }, 123], ['step', { id: '30', state: 'ok' }, 127],
  ['step', { id: '40', state: 'begin' }, 229],
  ['log', { step_id: '40', line: 'tailscaled.service aktiviert und gestartet' }, 250],
  ['log', { step_id: '40', line: 'Anmeldung noetig. Diese Adresse im Browser oeffnen: https://login.tailscale.com/a/4f2c8ab19de3' }, 252],
];

test('Ereignisse vor dem eigenen run-started und fremder Laeufe zaehlen nicht', () => {
  const { M } = setup();
  const model = M.create('run-2');
  assert.equal(M.apply(model, 'step', { id: '10', state: 'begin', at: 1 }), false);
  assert.equal(M.apply(model, 'run-started', { run_id: 'run-1', at: 1 }), false);
  assert.equal(M.apply(model, 'run-started', { run_id: 'run-2', mode: 'redeploy', at: 2 }), true);
  assert.equal(M.apply(model, 'run-finished', { run_id: 'run-1', ok: true, at: 3 }), false);
  assert.equal(model.finished, false);
  assert.equal(model.mode, 'redeploy');
});

test('der Lauf der Vorlage: Schritt 4 von 7, Dauern aus at, Anmeldeadresse erkannt', () => {
  const { M, groups } = setup();
  const model = M.create('run-1');
  play(M, model, DRAFT_RUN);

  assert.deepEqual(plain(M.current(model, groups)), { number: 4, total: 7, label: 'Tailscale' });
  assert.deepEqual(plain(groups.map((g) => M.groupState(model, g))), ['ok', 'ok', 'ok', 'run', 'wait', 'wait', 'wait']);
  assert.equal(M.groupDuration(model, groups[0], T0 + 252000), 102000);
  assert.equal(M.groupDuration(model, groups[3], T0 + 252000), 23000);
  assert.equal(M.elapsed(model, T0 + 252000), 252000);
  assert.equal(M.progress(model, groups), 50);
  assert.equal(model.loginUrl, 'https://login.tailscale.com/a/4f2c8ab19de3');
  assert.equal(model.loginStep, '40');
  assert.equal(model.steps['40'].lastLine.startsWith('Anmeldung noetig'), true);
});

test('Marker stehen im Protokoll als ##STEP <id> <state>', () => {
  const { M } = setup();
  const model = M.create('run-1');
  play(M, model, DRAFT_RUN);
  const markers = model.log.filter((entry) => entry.marker).map((entry) => entry.text);
  assert.deepEqual(plain(markers.slice(0, 3)), ['##STEP 10 begin', '##STEP 10 ok', '##STEP 20 begin']);
  assert.ok(model.log.every((entry, i) => i === 0 || entry.key > model.log[i - 1].key));
});

test('ein Skip traegt seinen Grund, login ausstehend wird gemerkt', () => {
  const { M, t } = setup();
  const model = M.create('run-1');
  play(M, model, DRAFT_RUN.concat([['step', { id: '40', state: 'skip', detail: 'login ausstehend' }, 253]]));
  assert.equal(model.loginPending, true);
  assert.equal(M.skipText('login ausstehend', t), 'wartet auf die Anmeldung');
  assert.equal(M.skipText('nicht ausgewaehlt', t), 'nicht ausgewählt');
  assert.equal(M.skipText('ein neuer Grund', t), 'ein neuer Grund');
});

test('ein Gruppe aus lauter Skips ist skip, eine mit einem Fehler fail', () => {
  const { M, groups } = setup();
  const model = M.create('run-1');
  play(M, model, [
    ['run-started', { run_id: 'run-1' }, 0],
    ['step', { id: '10', state: 'skip', detail: 'bereits erledigt' }, 1],
    ['step', { id: '20', state: 'begin' }, 1], ['step', { id: '20', state: 'fail', detail: 'MOSQUITTO_CONFIG_INVALID' }, 2],
  ]);
  assert.equal(M.groupState(model, groups[0]), 'skip');
  assert.equal(M.groupState(model, groups[1]), 'fail');
});

test('faultText: fault vor error vor unbekannt', () => {
  const { M, t } = setup();
  const catalog = realCatalog('de');
  assert.equal(M.faultText('UFW_MISSING', t), catalog['fault.UFW_MISSING.message']);
  assert.equal(M.faultText('NOT_CONNECTED', t), catalog['error.NOT_CONNECTED']);
  assert.equal(M.faultText('SOMETHING_NEW', t), 'Unbekannter Fehlercode SOMETHING_NEW.');
});

test('segments trennt die Maske des Geheimnis-Filters heraus', () => {
  const { M } = setup();
  assert.deepEqual(plain(M.segments('mosquitto_passwd -b energynode ***')), [
    { secret: false, text: 'mosquitto_passwd -b energynode ' }, { secret: true, text: '' },
  ]);
  assert.deepEqual(plain(M.segments('ohne')), [{ secret: false, text: 'ohne' }]);
});

test('run-finished beendet den Lauf; outcome traegt die letzten Zeilen des Fehlschritts', () => {
  const { M, groups } = setup();
  const model = M.create('run-1');
  play(M, model, [
    ['run-started', { run_id: 'run-1', mode: 'install' }, 0],
    ['step', { id: '50', state: 'begin' }, 1],
    ...Array.from({ length: 10 }, (_, i) => ['log', { step_id: '50', line: `pip ${i}` }, 2]),
    ['step', { id: '50', state: 'fail', detail: 'PIP_EXTERNALLY_MANAGED' }, 3],
    ['run-finished', { run_id: 'run-1', ok: false, code: 'PIP_EXTERNALLY_MANAGED', step_id: '50' }, 492],
  ]);
  assert.equal(M.apply(model, 'log', { step_id: '50', line: 'danach', at: T0 }), false, 'nach dem Ende zaehlt nichts mehr');
  const outcome = M.outcome(model, groups);
  assert.equal(outcome.ok, false);
  assert.equal(outcome.code, 'PIP_EXTERNALLY_MANAGED');
  assert.equal(outcome.stepId, '50');
  assert.equal(outcome.finishedAt - outcome.startedAt, 492000);
  assert.deepEqual(plain(outcome.lastLines), ['pip 2', 'pip 3', 'pip 4', 'pip 5', 'pip 6', 'pip 7', 'pip 8', 'pip 9']);
  assert.ok(outcome.logText.startsWith('14:18:04  ##STEP 50 begin\n'), outcome.logText.slice(0, 40));
  assert.equal(M.elapsed(model, T0 + 999999), 492000, 'nach dem Ende steht die Uhr');
});

test('das Protokoll haelt hoechstens 2000 Eintraege', () => {
  const { M } = setup();
  const model = M.create('run-1');
  M.apply(model, 'run-started', { run_id: 'run-1', at: 1 });
  for (let i = 0; i < 2100; i++) {
    M.apply(model, 'log', { step_id: '10', line: `z${i}`, at: 2 });
  }
  assert.equal(model.log.length, 2000);
  assert.equal(model.log[0].text, 'z100');
});

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountScreen, realCatalog } from './helpers/mount.mjs';
import { installEventSource } from './helpers/eventsource.mjs';
import { MANIFEST, SELECTION } from './helpers/fixtures.mjs';

const plain = (value) => JSON.parse(JSON.stringify(value));
const T0 = new Date(2026, 8, 13, 14, 18, 3).getTime();

async function mount(options = {}) {
  const mounted = mountScreen('screen-run.js', 'screenRun', Object.assign({
    scripts: ['services.js', 'events.js', 'run-model.js', 'download.js'],
    catalog: realCatalog('de'),
    responses: { 'GET /api/manifest': MANIFEST, 'GET /api/selection': SELECTION, 'POST /api/cancel': { cancelled: true } },
  }, options, {
    shell: Object.assign({
      screen: 'run', connected: true, mutating: true,
      shared: { run: { runId: 'run-1', mode: 'install', only: '', resumed: false } },
    }, options.shell),
  }));
  const sources = installEventSource(mounted.window);
  mounted.window.setInterval = () => 1;
  mounted.window.clearInterval = () => {};
  await mounted.screen.init();
  return Object.assign(mounted, { sources, source: sources[0] });
}

function emitAll(source, events) {
  let seq = 0;
  for (const [type, data, seconds] of events) {
    source.emit(type, ++seq, Object.assign({ at: T0 + seconds * 1000 }, data));
  }
}

const UNTIL_TAILSCALE = [
  ['run-started', { run_id: 'run-0' }, -600], ['step', { id: '10', state: 'ok' }, -590],
  ['run-started', { run_id: 'run-1', mode: 'install', only: '' }, 0],
  ['step', { id: '10', state: 'begin' }, 0], ['step', { id: '10', state: 'ok' }, 102],
  ['step', { id: '20', state: 'begin' }, 102], ['log', { step_id: '20', line: 'mosquitto_passwd -b energynode ***' }, 103],
  ['step', { id: '20', state: 'ok' }, 123],
  ['step', { id: '30', state: 'begin' }, 123], ['step', { id: '30', state: 'ok' }, 127],
  ['step', { id: '40', state: 'begin' }, 229],
  ['log', { step_id: '40', line: 'To authenticate, visit: https://login.tailscale.com/a/4f2c8ab19de3' }, 250],
];

test('init oeffnet den Strom ab seq 0 und schreibt Paket und Architektur in die Kopfleiste', async () => {
  const { source, shell } = await mount();
  assert.equal(source.url, '/api/events?token=tok&since=0');
  assert.equal(shell.bar.sub, 'Paket 1.4.2 · armv6');
  assert.equal(shell.shared.manifest, MANIFEST);
});

test('der Lauf der Vorlage: Stationen, Fortschritt, Dauer aus at statt Ankunftszeit', async () => {
  const { screen, source } = await mount();
  emitAll(source, UNTIL_TAILSCALE);
  source.emit('hello', 99, { seq: 99, running: true, run_id: 'run-1', at: T0 + 252000 });

  assert.equal(screen.progressText, 'Schritt 4 von 7: Tailscale');
  assert.equal(screen.elapsedText, '04:12 vergangen');
  const steps = screen.parts.filter((part) => part.type === 'stp');
  assert.deepEqual(plain(steps.map((part) => [part.title, part.cls, part.time])), [
    ['Systempakete', 'stp', '1:42'], ['MQTT-Broker', 'stp', '0:21'], ['Firewall', 'stp', '0:04'],
    ['Tailscale', 'stp now', '0:23'], ['Python-Pakete', 'stp wait', ''], ['Dienste und Dashboard', 'stp wait', ''],
    ['HTTPS über Caddy', 'stp wait', ''],
  ]);
  assert.equal(steps[3].detail, 'To authenticate, visit: https://login.tailscale.com/a/4f2c8ab19de3');
  assert.equal(steps[5].detail, 'Aus der Auswahl: 7 von 8 Diensten');
});

test('die Anmeldeleiste steht hinter ihrer Station, die Dienste als Unterpunkte', async () => {
  const { screen, source, opened } = await mount();
  emitAll(source, UNTIL_TAILSCALE);
  const keys = screen.parts.map((part) => part.type === 'stp' ? part.title : part.type);
  assert.deepEqual(plain(keys), ['Systempakete', 'MQTT-Broker', 'Firewall', 'Tailscale', 'call', 'Python-Pakete', 'Dienste und Dashboard', 'subs', 'HTTPS über Caddy']);
  screen.openLogin();
  assert.deepEqual(plain(opened), ['https://login.tailscale.com/a/4f2c8ab19de3']);
  const subs = screen.parts.find((part) => part.type === 'subs').subs;
  assert.equal(subs.find((sub) => sub.name === 'Batterie-SoC').on, false);
});

test('das Protokoll zeigt Uhrzeit, Marker und maskierte Geheimnisse', async () => {
  const { screen, source } = await mount();
  emitAll(source, UNTIL_TAILSCALE);
  const entries = screen.logEntries;
  assert.equal(entries[0].clock, '14:18:03');
  assert.equal(entries[0].marker, true);
  assert.equal(entries[0].text, '##STEP 10 begin');
  const secret = entries.find((entry) => entry.text.startsWith('mosquitto_passwd'));
  assert.deepEqual(plain(secret.segments.map((s) => s.secret)), [false, true]);
});

test('run-finished schliesst den Strom und fuehrt mit dem Ausgang ins Ergebnis', async () => {
  const { source, shell } = await mount();
  emitAll(source, UNTIL_TAILSCALE.concat([
    ['step', { id: '40', state: 'ok' }, 260],
    ['run-finished', { run_id: 'run-1', ok: true }, 492],
  ]));
  assert.equal(source.closed, true);
  assert.equal(shell.screen, 'result');
  assert.equal(shell.mutating, false);
  assert.equal(shell.shared.lastRun.ok, true);
  assert.equal(shell.shared.lastRun.finishedAt - shell.shared.lastRun.startedAt, 492000);
  assert.equal(shell.shared.lastRun.groups.length, 7);
});

test('Abbrechen schickt cancel genau einmal; der Lauf endet ueber den Strom', async () => {
  const { screen, calls, source, shell } = await mount();
  emitAll(source, UNTIL_TAILSCALE);
  await screen.cancel();
  await screen.cancel();
  assert.equal(calls.filter((call) => call.key === 'POST /api/cancel').length, 1);
  source.emit('run-finished', 200, { run_id: 'run-1', ok: false, code: 'RUN_CANCELLED', at: T0 + 300000 });
  assert.equal(shell.shared.lastRun.code, 'RUN_CANCELLED');
});

test('ein wiederaufgenommener Lauf uebernimmt Modus und Einstieg aus run-started', async () => {
  const { source, shell } = await mount({ shell: { entry: 'install', shared: { run: { runId: 'run-9', mode: '', only: '', resumed: true } } } });
  source.emit('run-started', 1, { run_id: 'run-9', mode: 'repair', only: '83', at: T0 });
  assert.equal(shell.shared.run.mode, 'repair');
  assert.equal(shell.entry, 'diagnose');
});

test('eine Reparatur zeigt genau eine Station mit dem Namen des Dienstes', async () => {
  const { screen, source } = await mount({ shell: { entry: 'diagnose', shared: { run: { runId: 'run-3', mode: 'repair', only: '83', resumed: false } } } });
  source.emit('run-started', 1, { run_id: 'run-3', mode: 'repair', only: '83', at: T0 });
  assert.deepEqual(plain(screen.parts.map((part) => part.title)), ['Shelly']);
  assert.equal(screen.progressText, 'Schritt 1 von 1: Shelly');
});

test('Speichern legt das Protokoll als Textdatei ab', async () => {
  const { screen, source, window } = await mount();
  emitAll(source, UNTIL_TAILSCALE);
  const saved = [];
  window.Download.text = (name, content) => saved.push({ name, content });
  screen.save();
  assert.match(saved[0].name, /^energy-node-protokoll-\d{8}-\d{6}\.txt$/);
  assert.ok(saved[0].content.includes('14:18:03  ##STEP 10 begin'));
  assert.ok(saved[0].content.includes('mosquitto_passwd -b energynode ***'));
});

test('destroy schliesst den Strom', async () => {
  const { screen, source } = await mount();
  screen.destroy();
  assert.equal(source.closed, true);
});

test('nach einem Neustart des Wirts baut der Lauf sich unter der ID aus hello neu auf', async () => {
  const { source, sources, shell, screen } = await mount();
  source.emit('hello', 0, { seq: 0, running: true, run_id: 'run-1', bus: 'a', at: T0 });
  emitAll(source, UNTIL_TAILSCALE);
  source.emit('hello', 3, { seq: 3, running: true, run_id: 'resumed-1', bus: 'b', at: T0 });
  assert.equal(shell.shared.run.runId, 'resumed-1');
  const again = sources[sources.length - 1];
  again.emit('run-started', 1, { run_id: 'resumed-1', mode: 'redeploy', only: '', at: T0 });
  again.emit('step', 2, { id: '60', state: 'ok', at: T0 + 1000 });
  again.emit('run-finished', 3, { run_id: 'resumed-1', ok: true, at: T0 + 2000 });
  assert.equal(screen.model.finished, true);
  assert.equal(shell.screen, 'result');
  assert.equal(shell.shared.lastRun.ok, true);
});

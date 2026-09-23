import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadScripts } from './helpers/load.mjs';
import { installEventSource, manualTimers } from './helpers/eventsource.mjs';

const plain = (value) => JSON.parse(JSON.stringify(value));

function setup() {
  const { window } = loadScripts(['api.js', 'events.js']);
  window.Api.configure({ basePath: '', token: 'tok' });
  const sources = installEventSource(window);
  return { window, sources, timers: manualTimers() };
}

test('open verbindet mit Token und since', () => {
  const { window, sources, timers } = setup();
  window.Events.open({ since: 0, onEvent: () => {}, setTimeout: timers.setTimeout });
  assert.equal(sources[0].url, '/api/events?token=tok&since=0');
});

test('Ereignisse kommen mit geparsten Daten und seq, doppelte werden verworfen', () => {
  const { window, sources, timers } = setup();
  const seen = [];
  const stream = window.Events.open({ since: 0, onEvent: (type, data, seq) => seen.push([type, seq, data]), setTimeout: timers.setTimeout });
  sources[0].emit('hello', 9, { seq: 9, running: true, run_id: 'run-1', at: 5 });
  sources[0].emit('step', 3, { id: '10', state: 'begin', at: 1 });
  sources[0].emit('step', 3, { id: '10', state: 'begin', at: 1 });
  sources[0].emit('log', 2, { step_id: '10', line: 'alt', at: 1 });
  sources[0].emit('log', 4, { step_id: '10', line: 'neu', at: 2 });
  assert.deepEqual(seen.map(([type, seq]) => [type, seq]), [['hello', 9], ['step', 3], ['log', 4]]);
  assert.equal(seen[2][2].line, 'neu');
  assert.equal(stream.lastSeq, 4, 'hello rueckt lastSeq nicht vor');
});

test('nach einem Fehler verbindet der Klient ab dem letzten seq neu, mit wachsender Pause', () => {
  const { window, sources, timers } = setup();
  const states = [];
  window.Events.open({ since: 0, onEvent: () => {}, onState: (s) => states.push(s), setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout });
  sources[0].emit('step', 7, { id: '10', state: 'ok', at: 1 });
  sources[0].fail();
  assert.equal(sources[0].closed, true);
  assert.equal(timers.pending[0].delay, 500);
  timers.run();
  assert.equal(sources[1].url, '/api/events?token=tok&since=7');

  sources[1].fail();
  assert.equal(timers.pending[0].delay, 1000);
  timers.run();
  sources[2].open();
  sources[2].fail();
  assert.equal(timers.pending[0].delay, 500, 'ein gelungenes Oeffnen setzt die Pause zurueck');
  assert.deepEqual(states.slice(0, 2), ['reconnecting', 'reconnecting']);
});

test('close beendet auch ein geplantes Wiederverbinden', () => {
  const { window, sources, timers } = setup();
  const stream = window.Events.open({ since: 0, onEvent: () => {}, setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout });
  sources[0].fail();
  stream.close();
  timers.run();
  assert.equal(sources.length, 1);
});

test('hello liefert den Stand des Busses und schliesst gleich wieder', async () => {
  const { window, sources, timers } = setup();
  const pending = window.Events.hello({ setTimeout: timers.setTimeout });
  assert.ok(sources[0].url.includes(`since=${Number.MAX_SAFE_INTEGER}`), sources[0].url);
  sources[0].emit('hello', 12, { seq: 12, running: true, run_id: 'run-7', at: 1 });
  assert.deepEqual(plain(await pending), { seq: 12, running: true, run_id: 'run-7', at: 1 });
  assert.equal(sources[0].closed, true);
});

test('hello liefert null bei Fehler oder Zeitablauf', async () => {
  const failing = setup();
  const a = failing.window.Events.hello({ setTimeout: failing.timers.setTimeout });
  failing.sources[0].fail();
  assert.equal(await a, null);

  const silent = setup();
  const b = silent.window.Events.hello({ setTimeout: silent.timers.setTimeout });
  silent.timers.run();
  assert.equal(await b, null);
});

test('ein neuer Bus (Wirt neu gestartet) setzt lastSeq zurueck und holt ab 0 nach', () => {
  const { window, sources, timers } = setup();
  const restarts = [];
  const seen = [];
  const stream = window.Events.open({
    since: 0, onEvent: (type, data, seq) => seen.push([type, seq]), onRestart: (hello) => restarts.push(hello.run_id),
    setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout,
  });
  sources[0].emit('hello', 0, { seq: 0, running: true, run_id: 'run-1', bus: 'a', at: 1 });
  sources[0].emit('step', 40, { id: '60', state: 'begin', at: 1 });
  sources[0].fail();
  timers.run();
  assert.equal(sources[1].url, '/api/events?token=tok&since=40');

  sources[1].emit('hello', 12, { seq: 12, running: true, run_id: 'run-1', bus: 'b', at: 2 });
  assert.equal(sources[1].closed, true, 'die Verbindung ab dem alten seq wird verworfen');
  assert.deepEqual(restarts, ['run-1']);
  assert.equal(sources[2].url, '/api/events?token=tok&since=0');
  sources[2].emit('hello', 12, { seq: 12, running: true, run_id: 'run-1', bus: 'b', at: 2 });
  sources[2].emit('run-started', 1, { run_id: 'run-1', at: 1 });
  assert.deepEqual(seen.slice(-1), [['run-started', 1]]);
  assert.equal(stream.lastSeq, 1);
  assert.deepEqual(restarts, ['run-1'], 'derselbe Bus loest keinen zweiten Neustart aus');
});

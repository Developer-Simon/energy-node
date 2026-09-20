import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountScreen, realCatalog } from './helpers/mount.mjs';

function mount(options = {}) {
  const mounted = mountScreen('screen-prepare.js', 'screenPrepare', Object.assign({
    catalog: realCatalog('de'),
    scripts: ['events.js'],
    responses: { 'POST /api/run': { run_id: 'run-1', seq: 3 } },
  }, options));
  // Events.open needs an EventSource; hand the screen a controllable one.
  const sources = [];
  mounted.window.EventSource = class {
    constructor(url) { this.url = url; this.listeners = {}; sources.push(this); }
    addEventListener(type, fn) { this.listeners[type] = fn; }
    close() { this.closed = true; }
    emit(type, data, id) { this.listeners[type]({ data: JSON.stringify(data), lastEventId: String(id) }); }
  };
  mounted.sources = sources;
  return mounted;
}

const BOOTSTRAP_SIGNED = { bundle_version: 'v1.4.2', bundle_arch: 'armv6', package: { resolved: { kind: 'github', signed: true } } };
const BOOTSTRAP_UNSIGNED = { bundle_version: 'v1.4.2', bundle_arch: 'armv6', package: { resolved: { kind: 'file', signed: false } } };

test('prepare starts the prepare run and collects the package log', async () => {
  const { screen, calls, sources } = mount();
  await screen.init();
  assert.deepEqual(calls[0], { key: 'POST /api/run', body: { mode: 'prepare' } });
  sources[0].emit('run-started', { run_id: 'run-1', mode: 'prepare' }, 4);
  sources[0].emit('log', { step_id: 'package', line: 'Lade energy-node-v1-armv6.tar.gz' }, 5);
  sources[0].emit('log', { step_id: 'other', line: 'not ours' }, 6);
  assert.deepEqual(JSON.parse(JSON.stringify(screen.lines)), ['Lade energy-node-v1-armv6.tar.gz']);
});

test('events of an earlier run are ignored until our run-started', async () => {
  const { screen, sources } = mount();
  await screen.init();
  sources[0].emit('log', { step_id: 'package', line: 'stale' }, 1);
  sources[0].emit('run-started', { run_id: 'run-0', mode: 'install' }, 2);
  sources[0].emit('log', { step_id: 'package', line: 'still stale' }, 3);
  assert.equal(screen.lines.length, 0);
});

test('a signed package advances straight to the entry screen and refreshes the caches', async () => {
  const { screen, shell, sources } = mount({ responses: { 'POST /api/run': { run_id: 'run-1', seq: 3 }, 'GET /api/bootstrap': BOOTSTRAP_SIGNED } });
  shell.shared.manifest = { stale: true };
  shell.shared.selection = { stale: true };
  await screen.init();
  sources[0].emit('run-started', { run_id: 'run-1', mode: 'prepare' }, 4);
  sources[0].emit('run-finished', { run_id: 'run-1', ok: true }, 5);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(shell.screen, 'precheck');
  assert.equal(shell.shared.manifest, null);
  assert.equal(shell.shared.selection, null);
  assert.equal(shell.bootstrap.bundle_version, 'v1.4.2');
});

test('an unsigned package waits for the operator', async () => {
  const { screen, shell, sources } = mount({ responses: { 'POST /api/run': { run_id: 'run-1', seq: 3 }, 'GET /api/bootstrap': BOOTSTRAP_UNSIGNED } });
  await screen.init();
  sources[0].emit('run-started', { run_id: 'run-1', mode: 'prepare' }, 4);
  sources[0].emit('run-finished', { run_id: 'run-1', ok: true }, 5);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(screen.state, 'done');
  assert.equal(screen.unsigned, true);
  assert.equal(shell.screen, 'connect', 'no automatic advance for an unsigned package');
  screen.next();
  assert.equal(shell.screen, 'precheck');
});

test('a failed prepare shows the code and goes back to the connect screen', async () => {
  const { screen, shell, sources } = mount();
  await screen.init();
  sources[0].emit('run-started', { run_id: 'run-1', mode: 'prepare' }, 4);
  sources[0].emit('run-finished', { run_id: 'run-1', ok: false, code: 'GITHUB_NO_RELEASE' }, 5);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(screen.state, 'failed');
  assert.equal(shell.error.code, 'GITHUB_NO_RELEASE');
  screen.back();
  assert.equal(shell.screen, 'connect');
});

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
  assert.deepEqual(calls[0], { key: 'POST /api/run', body: { mode: 'prepare', force_full_transfer: false } });
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

test('a translated log event with key renders the text from the German catalog', async () => {
  const { screen, sources } = mount();
  await screen.init();
  sources[0].emit('run-started', { run_id: 'run-1', mode: 'prepare' }, 4);
  sources[0].emit('log', { step_id: 'package', key: 'package.log.arch_detected', args: { machine: 'armv6l', arch: 'armv6' } }, 5);
  assert.deepEqual(JSON.parse(JSON.stringify(screen.lines)), ['Gerät meldet armv6l, Paket für armv6']);
});

test('a translated log event with key renders the text from the English catalog', async () => {
  const { screen, sources } = mount({ catalog: realCatalog('en') });
  await screen.init();
  sources[0].emit('run-started', { run_id: 'run-1', mode: 'prepare' }, 4);
  sources[0].emit('log', { step_id: 'package', key: 'package.log.arch_detected', args: { machine: 'armv6l', arch: 'armv6' } }, 5);
  assert.deepEqual(JSON.parse(JSON.stringify(screen.lines)), ['Device reports armv6l, package for armv6']);
});

test('a raw log event without a key still renders the line as-is', async () => {
  const { screen, sources } = mount();
  await screen.init();
  sources[0].emit('run-started', { run_id: 'run-1', mode: 'prepare' }, 4);
  sources[0].emit('log', { step_id: 'package', line: 'raw output text' }, 5);
  assert.deepEqual(JSON.parse(JSON.stringify(screen.lines)), ['raw output text']);
});

test('auf dem Dashboard fuehrt Zurueck zum Dashboard statt zur Verbindung', async () => {
  const { screen, shell } = mount({ shell: { bootstrap: { host: 'dashboard', auto_prepare: true, needs_connection: false } } });
  await screen.init();
  screen.back();
  assert.equal(shell.backToDashboardCalled, 1);
});

test('nach einem Fehler geht es mit einem schon vorhandenen Paket weiter', async () => {
  const { screen, shell, sources } = mount({
    shell: { bootstrap: { host: 'dashboard', auto_prepare: true, needs_connection: false, bundle_version: 'v1.4.2' } },
  });
  await screen.init();
  sources[0].emit('run-started', { run_id: 'run-1', mode: 'prepare' }, 4);
  sources[0].emit('run-finished', { run_id: 'run-1', ok: false, code: 'GITHUB_UNREACHABLE' }, 5);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(screen.state, 'failed');
  assert.equal(screen.canUseExisting, true);
  let advanced = 0;
  shell.afterPrepare = () => { advanced += 1; };
  screen.useExisting();
  assert.equal(advanced, 1);
});

test('ohne vorhandenes Paket wird "Weiter mit vorhandenem Paket" nicht angeboten', async () => {
  const { screen, sources } = mount({
    shell: { bootstrap: { host: 'dashboard', auto_prepare: true, needs_connection: false, bundle_version: '' } },
  });
  await screen.init();
  sources[0].emit('run-started', { run_id: 'run-1', mode: 'prepare' }, 4);
  sources[0].emit('run-finished', { run_id: 'run-1', ok: false, code: 'GITHUB_NO_RELEASE' }, 5);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(screen.canUseExisting, false);
});

test('finish({ok:false, code:PACKAGE_VERIFY_FAILED_DELTA, detail:x}) sets state failed, canForceFull true, does NOT call shell.fail, pushes detail into lines', async () => {
  const { screen, shell } = mount();
  await screen.init();
  let shellFailCalled = false;
  const originalFail = shell.fail;
  shell.fail = () => { shellFailCalled = true; };
  screen.finish({ ok: false, code: 'PACKAGE_VERIFY_FAILED_DELTA', detail: 'delta transfer verification failed' });
  assert.equal(screen.state, 'failed');
  assert.equal(screen.canForceFull, true);
  assert.equal(shellFailCalled, false);
  assert.ok(screen.lines.includes('delta transfer verification failed'));
  shell.fail = originalFail;
});

test('finish({ok:false, code:PACKAGE_STAGE_FAILED}) calls shell.fail and canForceFull is false', async () => {
  const { screen, shell } = mount();
  await screen.init();
  let shellFailCalled = false;
  const originalFail = shell.fail;
  shell.fail = () => { shellFailCalled = true; };
  screen.finish({ ok: false, code: 'PACKAGE_STAGE_FAILED', detail: 'stage failed' });
  assert.equal(screen.state, 'failed');
  assert.equal(screen.canForceFull, false);
  assert.equal(shellFailCalled, true);
  shell.fail = originalFail;
});

test('forceFull() posts /api/run with body {mode:prepare, force_full_transfer:true} and resets state/lines/lastErrorCode', async () => {
  const { screen, calls } = mount();
  screen.state = 'failed';
  screen.lines = ['old line'];
  screen.lastErrorCode = 'PACKAGE_VERIFY_FAILED_DELTA';
  await screen.forceFull();
  assert.deepEqual(calls[0], { key: 'POST /api/run', body: { mode: 'prepare', force_full_transfer: true } });
  assert.equal(screen.state, 'working');
  assert.deepEqual(JSON.parse(JSON.stringify(screen.lines)), []);
  assert.equal(screen.lastErrorCode, null);
});

test('init() posts {mode:prepare, force_full_transfer:false} by default', async () => {
  const { screen, calls } = mount();
  await screen.init();
  assert.deepEqual(calls[0], { key: 'POST /api/run', body: { mode: 'prepare', force_full_transfer: false } });
});

test('init() posts {mode:prepare, force_full_transfer:true} when shell.shared.forceFullTransfer is true', async () => {
  const { screen, shell, calls } = mount();
  shell.shared.forceFullTransfer = true;
  await screen.init();
  assert.deepEqual(calls[0], { key: 'POST /api/run', body: { mode: 'prepare', force_full_transfer: true } });
});

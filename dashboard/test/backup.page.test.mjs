// Tests for backup.page.js: the data mapping from GET /api/v1/backup/status,
// the settings PUT that keeps unchanged fields, the export POST and its job
// polling, inline errors, delete and download. Same jsdom approach as
// tailscale.page.test.mjs, run by `npm test` from dashboard/.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { installI18n, catalog } from './helpers/i18n.mjs';
import { attachStores, fakeModalStore } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'backup.page.js'),
  'utf8',
);
const de = catalog('de');

const reply = (status, body) => ({ status, body });

function statusBody(overrides = {}) {
  return {
    settings: {
      before_update: true,
      schedule: true,
      every: 'week',
      keep: 5,
      system_fallback: true,
      secrets: false,
      content: { settings: true, devices: true, users: true, secrets: false, stats: true, history: true, battery: false },
    },
    archives: [
      {
        id: '20261106-030000-auto',
        trigger: 'auto',
        version: '0.11.7',
        place: 'system',
        size: 2411520,
        created_at: '2026-11-06T03:00:00Z',
        areas: ['settings', 'devices', 'users', 'stats', 'history'],
        secrets: false,
        locked: false,
      },
    ],
    job: { id: '', kind: '', phases: [], current: 0, error: '', result: '', running: false },
    last_skipped: { at: null, reason: '' },
    place: 'system',
    areas: [
      { key: 'settings', size: 1024 },
      { key: 'devices', size: 0 },
      { key: 'users', size: 0 },
      { key: 'secrets', size: 0 },
      { key: 'stats', size: 2202009 },
      { key: 'history', size: 184320 },
      { key: 'battery', size: 512 },
    ],
    ...overrides,
  };
}

function session(roles = ['backup_export', 'backup_import', 'backup_secrets']) {
  return reply(200, { csrf_token: 'tok', roles });
}

// routes: "GET /path" or "/path" -> reply, or a function returning one.
// Every call is recorded in calls for the assertions.
function router(routes, calls = []) {
  const fetchImpl = async (url, options = {}) => {
    const method = options.method || 'GET';
    const body = typeof options.body === 'string' ? JSON.parse(options.body) : options.body;
    calls.push({ url, method, headers: options.headers || {}, body });
    const handler = routes[`${method} ${url}`] || routes[url];
    if (!handler) throw new Error(`unexpected fetch ${method} ${url}`);
    const result = typeof handler === 'function' ? handler() : handler;
    return { ok: result.status < 400, status: result.status, json: async () => result.body };
  };
  return { fetchImpl, calls };
}

function createBackupPage({ fetchImpl, url } = {}) {
  const dom = new JSDOM(
    '<!doctype html><html><body><div class="bs-flow-stage"></div><div class="bs-flow"></div></body></html>',
    { runScripts: 'outside-only', url: url || 'https://dashboard.local/' },
  );
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  // Reduced motion: go() switches steps without animating, jsdom has no animate().
  dom.window.matchMedia = () => ({ matches: true });
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  installI18n(dom.window);
  vm.runInContext(scriptSource, context);
  const page = factories.backupPage();
  const stores = attachStores(page, { modal: fakeModalStore({ answer: true }) });
  page.$nextTick = (fn) => fn();
  return { page, stores, window: dom.window };
}

test('init() maps status, auto settings, data areas and the secrets right', async () => {
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(['backup_export', 'backup_import']),
    '/api/v1/backup/status': reply(200, statusBody()),
  });
  const { page } = createBackupPage({ fetchImpl });
  await page.init();

  assert.equal(page.csrfToken, 'tok');
  assert.equal(page.canSecrets, false, 'backup_secrets is missing in this session');
  assert.equal(page.archives.length, 1);
  const [archive] = page.archives;
  assert.equal(archive.id, '20261106-030000-auto');
  assert.equal(archive.trigger, de['backup.trigger.auto']);
  assert.equal(archive.place, de['backup.place.system']);
  assert.equal(archive.version, '0.11.7');
  assert.ok(archive.size.endsWith('MB'), `size ${archive.size}`);
  assert.ok(archive.when.length > 0 && !archive.when.includes('T03'), `when ${archive.when}`);
  assert.deepEqual([...archive.tags], [de['backup.area.stats'], de['backup.area.history']]);
  assert.deepEqual([...page.dataAreas.map((area) => area.key)], ['stats', 'history', 'battery']);
  assert.equal(page.dataAreas[2].label, de['backup.area.battery']);
  assert.deepEqual(JSON.parse(JSON.stringify(page.auto.content)), { stats: true, history: true, battery: false });
  assert.equal(page.auto.every, 'week');
  assert.equal(page.autoDirty(), false);
});

test('saveAuto() sends the full settings object so unchanged fields survive', async () => {
  const calls = [];
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    'PUT /api/v1/backup/settings': reply(200, statusBody().settings),
    '/api/v1/backup/status': reply(200, statusBody()),
  }, calls);
  const { page, stores } = createBackupPage({ fetchImpl });
  await page.init();

  page.auto.keep = 7;
  page.auto.content.battery = true;
  page.auto.secrets = true;
  assert.equal(page.autoDirty(), true);
  await page.saveAuto();

  const put = calls.find((call) => call.method === 'PUT');
  assert.equal(put.url, '/api/v1/backup/settings');
  assert.equal(put.headers['X-CSRF-Token'], 'tok');
  assert.equal(put.body.keep, 7);
  assert.equal(put.body.system_fallback, true, 'a field the page does not edit is sent back');
  assert.equal(put.body.secrets, true);
  assert.equal(put.body.content.battery, true);
  assert.equal(put.body.content.settings, true, 'config areas keep their stored value');
  assert.equal(put.body.content.secrets, true, 'the secrets area follows the automatic secrets switch');
  assert.equal(page.autoDirty(), false);
  assert.equal(stores.toasts.last(), de['common.saved']);
});

test('the secrets switches stay off without backup_secrets', async () => {
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(['backup_export']),
    '/api/v1/backup/status': reply(200, statusBody()),
  });
  const { page } = createBackupPage({ fetchImpl });
  await page.init();
  assert.equal(page.canSecrets, false);
});

test('an export to the node posts the chosen areas, password and target store, then refreshes the list', async () => {
  const calls = [];
  let polls = 0;
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    'POST /api/v1/backup/archives': reply(202, { job: 'job-1' }),
    '/api/v1/backup/job': () => {
      polls += 1;
      return reply(200, { id: 'job-1', kind: 'export', running: false, current: 1, error: '', result: '20261109-101010-manual', phases: [{ key: 'write', done: true }, { key: 'finish', done: true }] });
    },
    '/api/v1/backup/status': () => reply(200, statusBody({
      archives: [
        { id: '20261109-101010-manual', trigger: 'manual', version: '0.11.7', place: 'system', size: 1024, created_at: '2026-11-09T10:10:10Z', areas: ['settings', 'stats'], secrets: false, locked: true },
        ...statusBody().archives,
      ],
    })),
  }, calls);
  const { page } = createBackupPage({ fetchImpl });
  await page.init();

  page.openExport();
  assert.equal(page.exp.secrets, false);
  assert.equal(page.exp.target, 'download');
  page.exp.target = 'node';
  page.exp.protect = true;
  page.exp.pw = 'geheim123';
  page.exp.pw2 = 'geheim123';
  page.flowPrimary();
  await new Promise((resolve) => setImmediate(resolve));
  const post = calls.find((call) => call.method === 'POST');
  assert.equal(post.url, '/api/v1/backup/archives');
  assert.equal(post.body.target, 'store');
  assert.equal(post.body.password, 'geheim123');
  assert.equal(post.body.secrets, false);
  assert.equal(post.body.areas.stats, true);
  assert.equal(post.body.areas.battery, false);
  assert.equal(post.body.areas.secrets, false);

  await page.pollJob();
  page.stopPolling();
  assert.ok(polls >= 1);
  assert.equal(page.step, 'done');
  assert.equal(page.doneTitle(), de['backup.done.export.title']);
  assert.equal(page.archives[0].id, '20261109-101010-manual');
  assert.equal(page.archives[0].fresh, true);
});

test('a download export hands the result id to the download on the done step', async () => {
  const calls = [];
  const downloads = [];
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    'POST /api/v1/backup/archives': reply(202, { job: 'job-2' }),
    '/api/v1/backup/job': reply(200, { id: 'job-2', kind: 'export', running: false, current: 1, error: '', result: '20261109-111111-manual', phases: [{ key: 'write', done: true }, { key: 'finish', done: true }] }),
  }, calls);
  const { page } = createBackupPage({ fetchImpl });
  await page.init();
  page.download = (id) => downloads.push(id);

  page.openExport();
  page.flowPrimary();
  await new Promise((resolve) => setImmediate(resolve));
  const post = calls.find((call) => call.method === 'POST');
  assert.equal(post.body.target, 'download');
  await page.pollJob();
  page.stopPolling();
  assert.equal(page.step, 'done');
  assert.equal(page.primaryLabel(), de['backup.archives.download']);
  page.flowPrimary();
  assert.deepEqual(downloads, ['20261109-111111-manual']);
  assert.equal(page.flow, null);
});

test('a busy server shows its catalogue text inline and stays in the export step', async () => {
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    'POST /api/v1/backup/archives': reply(409, { code: 'backup_busy', message: 'Ein Backup läuft bereits' }),
  });
  const { page } = createBackupPage({ fetchImpl });
  await page.init();
  page.openExport();
  page.flowPrimary();
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(page.flowError, de['error.backup_busy']);
  assert.equal(page.step, 'export');
  assert.equal(page.tasks.length, 0);
});

test('a failed job returns to the export step with the reason', async () => {
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    'POST /api/v1/backup/archives': reply(202, { job: 'job-3' }),
    '/api/v1/backup/job': reply(200, { id: 'job-3', kind: 'export', running: false, current: 0, error: 'Platz voll', result: '', phases: [{ key: 'write', done: false }, { key: 'finish', done: false }] }),
  });
  const { page } = createBackupPage({ fetchImpl });
  await page.init();
  page.openExport();
  page.flowPrimary();
  await new Promise((resolve) => setImmediate(resolve));
  await page.pollJob();
  page.stopPolling();
  assert.equal(page.step, 'export');
  assert.ok(page.flowError.includes('Platz voll'), page.flowError);
});

test('removeArchive() confirms, then deletes and reloads the list', async () => {
  const calls = [];
  let statusReads = 0;
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    'DELETE /api/v1/backup/archives/20261106-030000-auto': reply(200, { id: '20261106-030000-auto', status: 'deleted' }),
    '/api/v1/backup/status': () => (statusReads++ === 0 ? reply(200, statusBody()) : reply(200, statusBody({ archives: [] }))),
  }, calls);
  const { page, stores } = createBackupPage({ fetchImpl });
  await page.init();
  await page.removeArchive(page.archives[0]);
  assert.equal(stores.modal.calls.length, 1);
  assert.equal(stores.modal.calls[0].danger, true);
  const del = calls.find((call) => call.method === 'DELETE');
  assert.equal(del.headers['X-CSRF-Token'], 'tok');
  assert.equal(page.archives.length, 0);
  assert.equal(page.heroTone(), 'warn');
  assert.equal(page.heroTitle(), de['backup.hero.none.title']);
});

test('a skipped automatic backup without a location warns in the hero', async () => {
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    '/api/v1/backup/status': reply(200, statusBody({ last_skipped: { at: '2026-11-08T03:00:00Z', reason: 'no_location' } })),
  });
  const { page } = createBackupPage({ fetchImpl });
  await page.init();
  assert.equal(page.heroTone(), 'warn');
  assert.equal(page.heroNote(), de['backup.hero.skipped.no_location']);
});

test('downloadArchive() points the download route at the archive', async () => {
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    '/api/v1/backup/status': reply(200, statusBody()),
  });
  const { page, window } = createBackupPage({ fetchImpl, url: 'https://dashboard.local/node/' });
  await page.init();
  window.__DASHBOARD_BASE_PATH__ = '/node';
  const clicked = [];
  window.HTMLAnchorElement.prototype.click = function click() { clicked.push({ href: this.getAttribute('href'), download: this.hasAttribute('download') }); };
  page.downloadArchive(page.archives[0]);
  assert.deepEqual(clicked, [{ href: '/node/api/v1/backup/archives/20261106-030000-auto', download: true }]);
});

// ---- Einspielen -----------------------------------------------------------

const settle = () => new Promise((resolve) => setImmediate(resolve));

function previewAnswer(environment = [], differentDevice = false) {
  const area = (key, available, size = 0, changes = []) => ({ area: key, available, count: 0, size, changes });
  return {
    manifest: { node_version: '0.11.7', hostname: 'energy-node', trigger: 'manual', created_at: '2026-11-06T03:00:00Z', areas: [], secrets: false },
    areas: [
      area('settings', true, 1200, [{ kind: 'mod', label: 'Seite', name: 'Übersicht' }]),
      area('devices', true),
      area('users', true),
      area('secrets', false),
      area('stats', true, 2202009),
      area('history', true, 184320),
      area('battery', false),
    ],
    environment,
    different_device: differentDevice,
  };
}

test('a corrupt archive keeps the pick step and shows its text', async () => {
  const { fetchImpl, calls } = router({
    '/api/v1/auth/session': session(),
    'POST /api/v1/backup/import': reply(422, { code: 'backup_corrupt', message: 'Das Backup-Archiv ist beschädigt' }),
  });
  const { page } = createBackupPage({ fetchImpl });
  await page.init();
  page.openImport();
  await page.readFile({ name: 'archiv.tar.gz' });

  const post = calls.find((call) => call.method === 'POST');
  assert.equal(post.headers['Content-Type'], 'application/octet-stream');
  assert.equal(page.step, 'pick');
  assert.equal(page.checking, false);
  assert.equal(page.importError.title, de['backup.import.corrupt.title']);
  assert.equal(page.importError.text, de['backup.import.corrupt.text']);
});

test('a wrong password keeps the unlock step and marks the field', async () => {
  const { fetchImpl, calls } = router({
    '/api/v1/auth/session': session(),
    'POST /api/v1/backup/import': reply(409, { code: 'password_required', message: 'verschlüsselt' }),
    'POST /api/v1/backup/import/unlock': reply(422, { code: 'wrong_password', message: 'falsch' }),
  });
  const { page } = createBackupPage({ fetchImpl });
  await page.init();
  await page.openImport({ id: '20261102-214000-manual', locked: true });
  assert.equal(page.step, 'unlock');

  page.unlockPw = 'falsch123';
  page.flowPrimary();
  await settle();
  assert.equal(page.step, 'unlock');
  assert.equal(page.unlockFailed, true);
  const unlock = calls.find((call) => call.url === '/api/v1/backup/import/unlock');
  assert.deepEqual(unlock.body, { password: 'falsch123' });
  assert.equal(unlock.headers['X-CSRF-Token'], 'tok');
});

test('the environment step is skipped without rows and shown with rows', async () => {
  const calls = [];
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    'POST /api/v1/backup/import': reply(200, previewAnswer([], true)),
    'POST /api/v1/backup/import/apply': reply(202, { job: 'job-7' }),
  }, calls);
  const { page } = createBackupPage({ fetchImpl });
  await page.init();
  page.openImport();
  await page.readFile({ name: 'archiv.tar.gz' });
  assert.deepEqual([...page.stepList()], ['pick', 'preview']);
  assert.equal(page.primaryLabel(), de['backup.button.apply']);
  page.flowPrimary();
  await settle();
  page.stopPolling();
  const apply = calls.find((call) => call.url === '/api/v1/backup/import/apply');
  assert.deepEqual(apply.body, { areas: page.previewAreas.reduce((acc, area) => ({ ...acc, [area.key]: area.on }), {}), use_archive: {} });
  assert.equal(page.jobId, 'job-7');
  assert.equal(page.step, 'run');

  const withRows = createBackupPage({ fetchImpl: router({
    '/api/v1/auth/session': session(),
    'POST /api/v1/backup/import': reply(200, previewAnswer([
      { key: 'broker', here: 'localhost:1883', archive: '192.168.1.20:1883' },
      { key: 'bridge', off: 'bridge' },
    ], true)),
  }).fetchImpl });
  await withRows.page.init();
  withRows.page.openImport();
  await withRows.page.readFile({ name: 'archiv.tar.gz' });
  assert.deepEqual([...withRows.page.stepList()], ['pick', 'preview', 'env']);
  assert.equal(withRows.page.primaryLabel(), de['backup.button.next']);
  assert.equal(withRows.page.envRows[0].label, de['backup.env.row.broker']);
  assert.equal(withRows.page.envRows[0].use, 'here');
  assert.equal(withRows.page.envRows[1].off, de['backup.env.off.bridge']);
  withRows.page.flowPrimary();
  assert.equal(withRows.page.step, 'env');
});

test('the password step is skipped for an unprotected archive and shown for a locked one', async () => {
  const plain = createBackupPage({ fetchImpl: router({
    '/api/v1/auth/session': session(),
    'POST /api/v1/backup/import': reply(200, previewAnswer()),
  }).fetchImpl });
  await plain.page.init();
  plain.page.openImport();
  await plain.page.readFile({ name: 'archiv.tar.gz' });
  assert.deepEqual([...plain.page.stepList()], ['pick', 'preview']);
  assert.equal(plain.page.step, 'preview');

  const locked = createBackupPage({ fetchImpl: router({
    '/api/v1/auth/session': session(),
    'POST /api/v1/backup/import': reply(409, { code: 'password_required', message: 'verschlüsselt' }),
  }).fetchImpl });
  await locked.page.init();
  locked.page.openImport();
  await locked.page.readFile({ name: 'archiv.tar.gz' });
  assert.equal(locked.page.step, 'unlock');
  assert.deepEqual([...locked.page.stepList()], ['pick', 'unlock', 'preview']);
});

test('a failed apply shows its code and offers the rollback to the safety archive', async () => {
  const calls = [];
  const safety = '20261109-120000-pre_import';
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    '/api/v1/backup/status': reply(200, statusBody({
      archives: [{ id: safety, trigger: 'pre_import', version: '0.11.7', place: 'system', size: 1024, created_at: '2026-11-09T12:00:00Z', areas: ['settings'], secrets: false, locked: false }, ...statusBody().archives],
    })),
    'POST /api/v1/backup/import': reply(200, previewAnswer()),
  }, calls);
  const { page } = createBackupPage({ fetchImpl });
  await page.init();
  page.flow = 'import';
  page.doneKind = 'import';
  await page.finishImport({ ok: false, code: 'interrupted', finished_at: '2026-11-09T12:01:00Z' });

  assert.equal(page.step, 'done');
  assert.equal(page.doneTitle(), de['backup.done.import.failed']);
  assert.equal(page.doneNote(), de['backup.import.code.interrupted']);
  assert.equal(page.rollbackId, safety);

  await page.rollbackImport();
  const post = calls.find((call) => call.method === 'POST' && call.url === '/api/v1/backup/import');
  assert.deepEqual(post.body, { archive: safety });
  assert.equal(page.step, 'preview');
});

test('an apply job that fails before the restart goes back to the pick step with the error', async () => {
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    'POST /api/v1/backup/import': reply(200, previewAnswer()),
    'POST /api/v1/backup/import/apply': reply(202, { job: 'job-5' }),
    '/api/v1/backup/job': reply(200, { id: 'job-5', kind: 'import', running: false, current: 0, error: 'Platz voll', result: '', phases: [{ key: 'safety', done: false }, { key: 'stage', done: false }, { key: 'stop', done: false }, { key: 'restart', done: false }] }),
  });
  const { page } = createBackupPage({ fetchImpl });
  await page.init();
  await page.openImport({ id: '20261102-214000-manual', locked: false });
  assert.equal(page.step, 'preview');
  page.flowPrimary();
  await settle();
  await page.pollJob();
  page.stopPolling();

  assert.equal(page.step, 'pick');
  assert.equal(page.importError.title, de['backup.import.failed']);
  assert.equal(page.importError.text, 'Platz voll');
  assert.equal(page.previewAreas.length, 0);
  assert.equal(page.checking, false);
});

test('an apply answered with backup_no_import goes back to the pick step with its text', async () => {
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    'POST /api/v1/backup/import': reply(200, previewAnswer()),
    'POST /api/v1/backup/import/apply': reply(409, { code: 'backup_no_import', message: 'kein Backup' }),
  });
  const { page } = createBackupPage({ fetchImpl });
  await page.init();
  await page.openImport({ id: '20261102-214000-manual', locked: false });
  page.flowPrimary();
  await settle();
  page.stopPolling();

  assert.equal(page.step, 'pick');
  assert.equal(page.importError.text, de['error.backup_no_import']);
  assert.equal(page.previewAreas.length, 0);
});

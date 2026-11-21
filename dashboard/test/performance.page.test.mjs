// Tests for performance.page.js: the budget head (tone, title, note) from
// GET /api/v1/performance, the measurement groups, the bar parts and the
// 30 s poll that only runs while the page is visible. Same jsdom approach as
// backup.page.test.mjs, run by `npm test` from dashboard/.
import { afterEach, test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { installI18n, catalog } from './helpers/i18n.mjs';
import { attachStores } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'performance.page.js'),
  'utf8',
);
const coreSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'performance-core.js'),
  'utf8',
);
const de = catalog('de');

// The shelly row of the fixture. Its cost_pct makes base plus rows equal the
// fixture's today_pct, so the selection total and the measured share agree.
const SHELLY_ROW = { id: 'shelly', current: 20, steps: [2, 3, 5, 10, 15, 20, 30, 60], floor: 2, round_s: 0.4, cost_pct: 24.4, right: 'system_actions', editable: true, follows: '', source: true, balance: true };

const reply = (status, body) => ({ status, body });

// The answer of GET /api/v1/performance, the example of the API brief. The
// units are extended to a full page: polling rows and the base load, whose
// CPU parts add up to base_pct.
function performanceBody(overrides = {}) {
  return {
    measured_s: 7800,
    min_measure_s: 21600,
    cores: 4,
    target_pct: 70,
    default_target_pct: 70,
    deduction: { pct: 0, reasons: [] },
    base_pct: 7.7,
    today_pct: 32.1,
    over_since_s: null,
    clients: { max: 3, seen: true },
    device: { temp_c: 54, throttled: false, ram_free_pct: 61, swap_pages_s: 0 },
    external: { name: 'Wärmepumpe', every_s: 15 },
    rows: [
      SHELLY_ROW,
    ],
    units: [
      { id: 'shelly', cpu_pct: 5, ram_bytes: 23068672, round_s: 0.4, msg_per_min: 126, fixed: false },
      { id: 'trucki', cpu_pct: 2, ram_bytes: 15728640, round_s: 1.2, msg_per_min: 12, fixed: false },
      { id: 'tuya_mqtt', cpu_pct: 0.5, ram_bytes: 0, round_s: 2.4, msg_per_min: 16, fixed: false },
      { id: 'energy-node-dashboard.service', cpu_pct: 14, ram_bytes: 0, round_s: 0, msg_per_min: 0, fixed: true },
      { id: 'mosquitto.service', cpu_pct: 3.1, ram_bytes: 5242880, round_s: 0, msg_per_min: 0, fixed: true },
      { id: 'tailscaled.service', cpu_pct: 2.2, ram_bytes: 25165824, round_s: 0, msg_per_min: 0, fixed: true },
      { id: 'automation.service', cpu_pct: 0.9, ram_bytes: 14680064, round_s: 0, msg_per_min: 0, fixed: true },
      { id: 'caddy.service', cpu_pct: 0.6, ram_bytes: 15728640, round_s: 0, msg_per_min: 0, fixed: true },
    ],
    change: null,
    ...overrides,
  };
}

// Overrides that set today_pct and move the shelly row so the selection total
// equals it, for the tests that read the budget head.
const today = (todayPct) => ({
  today_pct: todayPct,
  rows: [{ ...SHELLY_ROW, cost_pct: Math.round((todayPct - 7.7) * 10) / 10 }],
});

// routes: "METHOD /path" or "/path" -> reply, or a function returning one.
// Every call is recorded in calls for the assertions.
function router(routes, calls = []) {
  const fetchImpl = async (url, options = {}) => {
    const method = options.method || 'GET';
    calls.push({ url, method, body: options.body, headers: options.headers });
    const handler = routes[`${method} ${url}`] || routes[url];
    if (!handler) throw new Error(`unexpected fetch ${method} ${url}`);
    const result = typeof handler === 'function' ? handler() : handler;
    return { ok: result.status < 400, status: result.status, json: async () => result.body };
  };
  return { fetchImpl, calls };
}

// Pages created by a test. The poll timer is a fake here, so nothing keeps
// node waiting, but every page is destroyed and its window closed anyway.
const created = [];
afterEach(() => {
  while (created.length) {
    const { page, window } = created.pop();
    page.destroy();
    window.close();
  }
});

function createPerformancePage({ fetchImpl, page: settingsPage = 'performance' } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: 'https://dashboard.local/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  const visibility = { state: 'visible' };
  const timers = [];
  const cleared = [];
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.matchMedia = () => ({ matches: true });
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  dom.window.setInterval = (fn, ms) => { timers.push({ fn, ms }); return timers.length; };
  dom.window.clearInterval = (id) => { cleared.push(id); };
  Object.defineProperty(dom.window.document, 'visibilityState', { configurable: true, get: () => visibility.state });
  installI18n(dom.window);
  vm.runInContext(coreSource, context);
  vm.runInContext(scriptSource, context);
  const page = factories.pfPage();
  const stores = attachStores(page);
  // The settings shell owns activeSettingsPage, the tests switch it by hand.
  page.activeSettingsPage = settingsPage;
  page.$watch = (key, fn) => { page.watchers = { ...page.watchers, [key]: fn }; };
  const made = { page, stores, window: dom.window, timers, cleared, visibility };
  created.push(made);
  return made;
}

// A page whose data is the fixture, with the fields a test changes applied.
async function loadedPage(overrides = {}, pageOptions = {}) {
  const { fetchImpl } = router({ '/api/v1/performance': reply(200, performanceBody(overrides)) });
  const made = createPerformancePage({ fetchImpl, ...pageOptions });
  await made.page.load();
  return made;
}

test('init() loads the page at once and polls every 30 s while it is visible', async () => {
  const calls = [];
  const { fetchImpl } = router({ '/api/v1/performance': reply(200, performanceBody()) }, calls);
  const made = createPerformancePage({ fetchImpl });
  await made.page.init();

  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/v1/performance');
  assert.equal(made.page.data.measured_s, 7800);
  assert.equal(made.timers.length, 1);
  assert.equal(made.timers[0].ms, 30000);

  made.visibility.state = 'hidden';
  await made.timers[0].fn();
  assert.equal(calls.length, 1, 'a hidden page does not poll');

  made.visibility.state = 'visible';
  made.page.activeSettingsPage = 'general';
  await made.timers[0].fn();
  assert.equal(calls.length, 1, 'another settings page does not poll');

  made.page.activeSettingsPage = 'performance';
  await made.timers[0].fn();
  assert.equal(calls.length, 2, 'a visible performance page polls');
});

test('destroy() clears the poll timer', async () => {
  const { fetchImpl } = router({ '/api/v1/performance': reply(200, performanceBody()) });
  const made = createPerformancePage({ fetchImpl });
  await made.page.init();
  made.page.destroy();
  assert.deepEqual(made.cleared, [1]);
  assert.equal(made.page.timer, 0);
});

test('load() keeps the last data and raises a critical toast on failure', async () => {
  let fail = false;
  const { fetchImpl } = router({
    '/api/v1/performance': () => (fail ? reply(500, { message: 'boom' }) : reply(200, performanceBody())),
  });
  const made = createPerformancePage({ fetchImpl });
  await made.page.load();
  assert.equal(made.page.data.today_pct, 32.1);

  fail = true;
  await made.page.load();
  assert.equal(made.page.data.today_pct, 32.1, 'the old values stay on screen');
  assert.equal(made.stores.toasts.items.length, 1);
  assert.equal(made.stores.toasts.items[0].severity, 'critical');
});

test('load() raises one toast per failure streak, a successful load ends it', async () => {
  let fail = true;
  const { fetchImpl } = router({
    '/api/v1/performance': () => (fail ? reply(500, { message: 'boom' }) : reply(200, performanceBody())),
  });
  const made = createPerformancePage({ fetchImpl });
  await made.page.load();
  await made.page.load();
  await made.page.load();
  assert.equal(made.stores.toasts.items.length, 1, 'the 30 s poll does not repeat the toast');

  fail = false;
  await made.page.load();
  assert.equal(made.page.failing, false);

  fail = true;
  await made.page.load();
  assert.equal(made.stores.toasts.items.length, 2, 'a new streak raises a new toast');
});

test('data starts with the device values as null, so the page shows dashes', () => {
  const { page } = createPerformancePage();
  assert.equal(page.data.device.temp_c, null);
  assert.equal(page.data.device.ram_free_pct, null);
});

test('headTone() is info while measuring, then bad, warn or ok by the free share', async () => {
  const { page } = await loadedPage();
  assert.equal(page.headTone(), 'info', 'measured 7800 s of 21600 s');

  const finished = await loadedPage({ measured_s: 21600 });
  assert.equal(finished.page.headTone(), 'ok', '37.9 % free');

  const tight = await loadedPage({ measured_s: 21600, ...today(68) });
  assert.equal(tight.page.headTone(), 'warn', '2 % free is under 3 %');

  const over = await loadedPage({ measured_s: 21600, ...today(71) });
  assert.equal(over.page.headTone(), 'bad', 'above the target');
});

test('headTone() takes the deduction out of the available share', async () => {
  const deducted = await loadedPage({
    measured_s: 21600,
    ...today(65),
    deduction: { pct: 10, reasons: ['throttled'] },
  });
  assert.equal(deducted.page.available(), 60);
  assert.equal(deducted.page.headTone(), 'bad', 'above 70 - 10');

  const close = await loadedPage({
    measured_s: 21600,
    ...today(58),
    deduction: { pct: 10, reasons: ['throttled'] },
  });
  assert.equal(close.page.headTone(), 'warn', '2 % free of 60');
});

test('headTitle() names the used and the free or over share', async () => {
  const { page } = await loadedPage({ measured_s: 21600 });
  assert.equal(page.headTitle(), '32,1 % genutzt, 37,9 % frei');

  const over = await loadedPage({ measured_s: 21600, ...today(72) });
  assert.equal(over.page.headTitle(), '72,0 % genutzt, 2,0 % über dem Budget');
});

test('headNote() names the remaining measuring time', async () => {
  const { page } = await loadedPage();
  const note = page.headNote();
  assert.ok(note.includes('noch 3 h 50 min'), note);
  assert.ok(note.includes('6 Stunden'), note);
});

test('headNote() names the minutes over the budget, otherwise the default note', async () => {
  const over = await loadedPage({ measured_s: 21600, over_since_s: 2520 });
  assert.equal(over.page.headNote(), 'Seit 42 Minuten über dem Budget. Geschätzt aus den letzten 6 Stunden, Spitzenwert ohne Ausreißer.');

  const plain = await loadedPage({ measured_s: 21600 });
  assert.equal(plain.page.headNote(), 'Geschätzt aus den letzten 6 Stunden, Spitzenwert ohne Ausreißer.');
});

test('measureGroups() returns polls and base, each sorted by CPU', async () => {
  const { page } = await loadedPage();
  const groups = page.measureGroups();
  assert.equal(groups.map((group) => group.id).join(','), 'polls,base');
  assert.equal(groups[0].label, de['performance.js.group.polls']);
  assert.equal(groups[1].label, de['performance.js.group.base']);

  assert.equal(groups[0].rows.map((row) => row.id).join(','), 'shelly,trucki,tuya_mqtt');
  assert.equal(groups[1].rows.map((row) => row.id).join(','), 'energy-node-dashboard.service,mosquitto.service,tailscaled.service,automation.service,caddy.service');
});

test('measureGroups() sums each group and shares the largest unit of the page', async () => {
  const { page } = await loadedPage();
  const [polls, base] = page.measureGroups();

  assert.ok(Math.abs(polls.sum - 7.5) < 1e-9, `polls sum ${polls.sum}`);
  assert.ok(Math.abs(base.sum - 20.8) < 1e-9, `base sum ${base.sum}`);
  assert.equal(polls.sumText, '7,5 %');

  const byId = Object.fromEntries([...polls.rows, ...base.rows].map((row) => [row.id, row]));
  assert.equal(byId['energy-node-dashboard.service'].share, 1, 'the largest unit fills the bar');
  assert.equal(byId.shelly.share, 0.3571, '5 of 14');
  assert.equal(byId['mosquitto.service'].share, 0.2214, '3.1 of 14');
});

test('measureGroups() formats RAM, round and message rate', async () => {
  const { page } = await loadedPage();
  const [polls, base] = page.measureGroups();
  const shelly = polls.rows.find((row) => row.id === 'shelly');
  assert.equal(shelly.ram, '22 MB RAM');
  assert.equal(shelly.round, 'Runde 0,4 s');
  assert.equal(shelly.msg, '126 Nachrichten/min');
  assert.equal(shelly.cpu, '5,0 %');

  const tuya = polls.rows.find((row) => row.id === 'tuya_mqtt');
  assert.equal(tuya.ram, '', 'a unit without a RAM sample shows no RAM text');
  assert.equal(tuya.round, 'Runde 2,4 s');

  const mosquitto = base.rows.find((row) => row.id === 'mosquitto.service');
  assert.equal(mosquitto.ram, '5 MB RAM');
  assert.equal(mosquitto.msg, '');
  assert.equal(base.rows.find((row) => row.id === 'energy-node-dashboard.service').ram, '', 'fixed units without RAM show nothing');
});

test('measureGroups() names the units by their unit name, dropping .service', async () => {
  const units = [
    { id: 'energy-node-dashboard.service', cpu_pct: 3, ram_bytes: 0, round_s: 0, msg_per_min: 0, fixed: true },
    { id: 'automation.service', cpu_pct: 2, ram_bytes: 0, round_s: 0, msg_per_min: 0, fixed: true },
    { id: 'mosquitto.service', cpu_pct: 1.5, ram_bytes: 0, round_s: 0, msg_per_min: 0, fixed: true },
    { id: 'tailscaled.service', cpu_pct: 1.4, ram_bytes: 0, round_s: 0, msg_per_min: 0, fixed: true },
    { id: 'caddy.service', cpu_pct: 1.3, ram_bytes: 0, round_s: 0, msg_per_min: 0, fixed: true },
    { id: 'other', cpu_pct: 1.2, ram_bytes: 0, round_s: 0, msg_per_min: 0, fixed: true },
    { id: 'shelly', cpu_pct: 2, ram_bytes: 0, round_s: 0, msg_per_min: 0, fixed: false },
    { id: 'trucki', cpu_pct: 1.5, ram_bytes: 0, round_s: 0, msg_per_min: 0, fixed: false },
    { id: 'apsystems_ez1', cpu_pct: 1.1, ram_bytes: 0, round_s: 0, msg_per_min: 0, fixed: false },
    { id: 'battery_soc', cpu_pct: 1, ram_bytes: 0, round_s: 0, msg_per_min: 0, fixed: false },
    { id: 'mystery.service', cpu_pct: 0.5, ram_bytes: 0, round_s: 0, msg_per_min: 0, fixed: true },
  ];
  const { page } = await loadedPage({ units });
  const [polls, base] = page.measureGroups();
  const names = (group) => group.rows.map((row) => row.name).join('|');

  assert.equal(names(base), [
    'Dashboard', 'Automation', 'Mosquitto', 'Tailscale', 'Caddy', 'Übriges', 'mystery.service',
  ].join('|'));
  assert.equal(names(polls), 'Shelly|Trucki|APsystems EZ1|Battery SoC');
  assert.equal(de['performance.js.unit.tailscaled'], 'Tailscale');
});

test('meterParts() splits the bar into base load and polling, clamped to 0 to 100', async () => {
  const { page } = await loadedPage();
  const [fixed, now] = page.meterParts();
  assert.equal(fixed.cls, 'pf-m-fixed');
  assert.equal(fixed.w, 7.7);
  assert.equal(now.cls, 'pf-m-now');
  assert.ok(Math.abs(now.w - 24.4) < 1e-9, `polling part ${now.w}`);

  const full = await loadedPage({ ...today(140) });
  assert.equal(full.page.meterParts()[1].w, 100);
});

// Makes the page's texts show their key and parameters, so a test can read both.
function keyStub(made) {
  made.window.I18n.t = (key, params) => (params ? `${key} ${JSON.stringify(params)}` : key);
}

const TICK_ROW = { id: 'tick', current: 10, steps: [5, 10, 15, 20, 30, 60], floor: 5, round_s: 0.1, cost_pct: 0, right: 'automations', editable: true, follows: 'half', source: false, balance: false };

test('customProblem() flags a value under the hard floor as bad and does not take it over', async () => {
  const made = await loadedPage();
  keyStub(made);
  made.page.customInput('shelly', '1');
  const problem = made.page.customProblem('shelly');
  assert.equal(problem.tone, 'bad');
  assert.ok(problem.text.startsWith('performance.js.custom.below_floor'), problem.text);
  assert.equal(made.page.sel.shelly, 20, 'the pick stays');
});

test('customProblem() warns when the selection is over the budget, with the amount', async () => {
  const made = await loadedPage({ measured_s: 21600 });
  keyStub(made);
  made.page.customInput('shelly', '2');
  const problem = made.page.customProblem('shelly');
  assert.equal(problem.tone, 'warn');
  assert.ok(problem.text.startsWith('performance.js.custom.over_budget'), problem.text);
  assert.ok(problem.text.includes('145,1 %'), problem.text);
});

test('customProblem() marks a value under a follow row soft floor as info, not bad', async () => {
  const made = await loadedPage({ rows: [SHELLY_ROW, TICK_ROW] });
  keyStub(made);
  // The external meter every 15 s gives half 7.5 s, so the soft floor is 10 s.
  made.page.customInput('tick', '3');
  assert.equal(made.page.sel.tick, 3, 'a follow row takes any value from 1 s');
  const problem = made.page.customProblem('tick');
  assert.equal(problem.tone, 'info');
  assert.ok(problem.text.startsWith('performance.js.custom.below_source'), problem.text);
});

test('customProblem() also hints at a stored value under the soft floor without typed input', async () => {
  const made = await loadedPage({ rows: [SHELLY_ROW, TICK_ROW] });
  keyStub(made);
  assert.equal(made.page.custom.tick, undefined);
  // The tick row runs every 10 s in the fixture, the soft floor is 10 s, so lower the stored value.
  made.page.sel.tick = 5;
  const problem = made.page.customProblem('tick');
  assert.ok(problem && problem.tone === 'info', JSON.stringify(problem));
  assert.ok(problem.text.startsWith('performance.js.custom.below_source'), problem.text);
  made.page.sel.tick = 30;
  assert.equal(made.page.customProblem('tick'), null);
});

test('requestApply() while measuring asks first and only then sends the request', async () => {
  const calls = [];
  const { fetchImpl } = router({
    '/api/v1/performance': reply(200, performanceBody()),
    'POST /api/v1/performance/apply': reply(200, {}),
    '/api/v1/auth/session': reply(200, { csrf_token: 'tok' }),
  }, calls);
  const made = createPerformancePage({ fetchImpl });
  await made.page.load();
  made.page.pick('shelly', 10);
  calls.length = 0;

  await made.page.requestApply();
  assert.equal(made.page.confirming, true);
  assert.equal(calls.filter((call) => call.method === 'POST').length, 0, 'no request before the confirmation');

  await made.page.requestApply();
  assert.equal(calls.filter((call) => call.method === 'POST').length, 1);
  assert.equal(made.page.confirming, false);
});

test('apply() posts only the changed rows, and the target only when it changed', async () => {
  const calls = [];
  const { fetchImpl } = router({
    '/api/v1/performance': reply(200, performanceBody({ measured_s: 21600, rows: [SHELLY_ROW, TICK_ROW] })),
    'POST /api/v1/performance/apply': reply(200, {}),
    '/api/v1/auth/session': reply(200, { csrf_token: 'tok' }),
  }, calls);
  const made = createPerformancePage({ fetchImpl });
  await made.page.load();

  made.page.pick('shelly', 10);
  await made.page.apply();
  const posts = calls.filter((call) => call.method === 'POST');
  assert.equal(posts.length, 1);
  assert.equal(posts[0].headers['X-CSRF-Token'], 'tok');
  assert.equal(posts[0].headers['Content-Type'], 'application/json');
  assert.deepEqual(JSON.parse(posts[0].body), { rates: { shelly: 10 } });

  made.page.setTarget(75);
  await made.page.apply();
  const again = calls.filter((call) => call.method === 'POST');
  assert.equal(again.length, 2);
  assert.deepEqual(JSON.parse(again[1].body), { rates: { shelly: 10 }, target_pct: 75 });
});

test('a row without the right is locked: pick() changes nothing, lockText names the right', async () => {
  const locked = { ...SHELLY_ROW, editable: false };
  const made = await loadedPage({ measured_s: 21600, rows: [locked] });
  keyStub(made);
  made.page.pick('shelly', 10);
  assert.equal(made.page.sel.shelly, 20);
  assert.ok(made.page.lockText(locked).includes('performance.js.right.system_actions'), made.page.lockText(locked));
});

test('even() does nothing while measuring', async () => {
  const made = await loadedPage({ rows: [SHELLY_ROW, TICK_ROW] });
  made.page.pick('shelly', 10);
  const before = { ...made.page.sel };
  made.page.even();
  assert.deepEqual({ ...made.page.sel }, before);
});

test('load() keeps an unsaved selection and an unsaved limit when a new poll arrives', async () => {
  let body = performanceBody();
  const { fetchImpl } = router({ '/api/v1/performance': () => reply(200, body) });
  const made = createPerformancePage({ fetchImpl });
  await made.page.load();

  made.page.pick('shelly', 10);
  made.page.setTarget(75);
  body = performanceBody({ measured_s: 8000 });
  await made.page.load();

  assert.equal(made.page.sel.shelly, 10, 'the unsaved pick stays');
  assert.equal(made.page.targetDraft, 75, 'the unsaved limit stays');
  assert.equal(made.page.cur.shelly, 20, 'the saved value follows the server');
  assert.equal(made.page.savedTarget, 70);
});

test('notes() names the period and, while measuring, when the suggestions start', async () => {
  const made = await loadedPage();
  keyStub(made);
  const texts = made.page.notes('shelly').map((note) => note.text.split(' ')[0]);
  assert.ok(texts.includes('performance.js.note.period'), texts.join(' | '));
  assert.ok(texts.includes('performance.js.note.proposal_in'), texts.join(' | '));
});

// The change of the API: shelly went from 20 s to 5 s, dash from 3 s to 2 s.
// The check started five minutes ago, so 10 minutes are left.
const CHANGE = {
  at: new Date(Date.now() - 5 * 60000).toISOString(),
  before: { shelly: 20, dash: 3 },
  after: { shelly: 5, dash: 2 },
  expected_pct: 24.4,
  available_pct: 70,
  state: 'running',
  restarted: false,
};

// A page with the given change and a fake rollback endpoint.
async function checkPage(change, rollback) {
  const calls = [];
  const routes = {
    '/api/v1/performance': reply(200, performanceBody({ measured_s: 21600, change })),
    'POST /api/v1/performance/rollback': rollback || reply(200, { change: null, left_unchanged: [] }),
    '/api/v1/auth/session': reply(200, { csrf_token: 'tok' }),
  };
  const { fetchImpl } = router(routes, calls);
  const made = createPerformancePage({ fetchImpl });
  await made.page.load();
  return { ...made, calls };
}

const rollbackCalls = (calls) => calls.filter((call) => call.url === '/api/v1/performance/rollback');

test('checkState() follows the server state, a local conflict overrides it until undone', async () => {
  for (const state of ['running', 'ok', 'bad', 'undone']) {
    const { page } = await checkPage({ ...CHANGE, state });
    assert.equal(page.checkState(), state);
  }
  const { page } = await checkPage({ ...CHANGE, state: 'bad' });
  page.conflict = { shelly: 10 };
  assert.equal(page.checkState(), 'conflict');

  const undone = await checkPage({ ...CHANGE, state: 'undone' });
  undone.page.conflict = { shelly: 10 };
  assert.equal(undone.page.checkState(), 'undone', 'an undone change stays undone');
});

test('checkTone() and checkTitle() give the tone and title of each state', async () => {
  const running = await checkPage(CHANGE);
  assert.equal(running.page.checkTone(), 'info');
  assert.equal(running.page.checkTitle(), 'Wirkung wird geprüft, noch 10 Minuten');

  const ok = await checkPage({ ...CHANGE, state: 'ok', measured_pct: 24 });
  assert.equal(ok.page.checkTone(), 'ok');
  assert.equal(ok.page.checkTitle(), 'Wirkung wie erwartet');

  const bad = await checkPage({ ...CHANGE, state: 'bad', measured_pct: 42 });
  assert.equal(bad.page.checkTone(), 'bad');
  assert.equal(bad.page.checkTitle(), 'Grenze nach der Übernahme überschritten');

  const undone = await checkPage({ ...CHANGE, state: 'undone' });
  assert.equal(undone.page.checkTone(), 'ok');
  assert.equal(undone.page.checkTitle(), 'Zurückgenommen');

  const conflict = await checkPage({ ...CHANGE, state: 'bad', measured_pct: 42 });
  conflict.page.conflict = { shelly: 10 };
  assert.equal(conflict.page.checkTone(), 'warn');
  assert.equal(conflict.page.checkTitle(), 'Ein Wert wurde inzwischen anders gesetzt');
});

test('checkNote() adds the restart sentence while a restarted check runs', async () => {
  const restarted = await checkPage({ ...CHANGE, restarted: true });
  assert.ok(restarted.page.checkNote().endsWith(de['performance.js.check.note.restarted']), restarted.page.checkNote());

  const plain = await checkPage(CHANGE);
  assert.ok(!plain.page.checkNote().includes(de['performance.js.check.note.restarted']));
});

test('a bad check puts the measurement in the head instead of the estimate', async () => {
  const { page } = await checkPage({ ...CHANGE, state: 'bad', measured_pct: 42 });
  assert.equal(page.headTone(), 'bad');
  assert.ok(page.headTitle().startsWith('42,0 % gemessen'), page.headTitle());
  assert.ok(page.headNote().includes('Geschätzt waren 24,4 %'), page.headNote());
});

test('tryUndo() on a 409 rollback_conflict switches to the conflict state and sends only_unchanged false', async () => {
  const conflict = reply(409, {
    code: 'rollback_conflict',
    message: 'Einige Zeilen wurden seit der Übernahme geändert',
    conflicts: { shelly: 10 },
  });
  const { page, calls } = await checkPage({ ...CHANGE, state: 'bad', measured_pct: 42 }, conflict);
  await page.tryUndo();

  assert.equal(page.checkState(), 'conflict');
  assert.deepEqual({ ...page.conflict }, { shelly: 10 }, 'the conflicts come from the 409 body');
  const [call] = rollbackCalls(calls);
  assert.equal(call.method, 'POST');
  assert.deepEqual(JSON.parse(call.body), { only_unchanged: false });
  assert.equal(call.headers['X-CSRF-Token'], 'tok');
});

test('tryUndo() on success raises a toast and reloads the page', async () => {
  const { page, calls, stores } = await checkPage({ ...CHANGE, state: 'bad', measured_pct: 42 });
  await page.tryUndo();
  assert.equal(stores.toasts.items.length, 1);
  assert.equal(stores.toasts.items[0].severity, 'info');
  assert.equal(calls.filter((call) => call.url === '/api/v1/performance').length, 2, 'load() runs again');
});

test('tryUndo() on another 409 shows its message and keeps the state', async () => {
  const none = reply(409, { code: 'no_change_to_undo', message: 'Es gibt keine Änderung zum Zurücknehmen' });
  const { page, stores } = await checkPage({ ...CHANGE, state: 'bad', measured_pct: 42 }, none);
  await page.tryUndo();
  assert.deepEqual({ ...page.conflict }, {});
  assert.equal(stores.toasts.items.length, 1);
  assert.equal(stores.toasts.items[0].severity, 'critical');
});

test('undoRest() sends only_unchanged true, clears the conflict and reloads', async () => {
  const { page, calls } = await checkPage({ ...CHANGE, state: 'bad', measured_pct: 42 });
  page.conflict = { shelly: 10 };
  await page.undoRest();
  const [call] = rollbackCalls(calls);
  assert.deepEqual(JSON.parse(call.body), { only_unchanged: true });
  assert.deepEqual({ ...page.conflict }, {});
  assert.equal(calls.filter((c) => c.url === '/api/v1/performance').length, 2, 'load() runs again');
});

test('keepAll() clears the conflict display and leaves the change as the server has it', async () => {
  const { page, calls } = await checkPage({ ...CHANGE, state: 'bad', measured_pct: 42 });
  page.conflict = { shelly: 10 };
  page.keepAll();
  assert.deepEqual({ ...page.conflict }, {});
  assert.equal(page.checkState(), 'bad');
  assert.equal(rollbackCalls(calls).length, 0, 'keeping sends nothing');
});

test('changeList() lists each changed row and marks the conflicting one with its current value', async () => {
  const { page } = await checkPage({ ...CHANGE, state: 'bad', measured_pct: 42 });
  page.conflict = { shelly: 10 };
  const list = page.changeList();
  assert.equal(list.map((row) => row.id).sort().join(","), "dash,shelly");
  const shelly = list.find((row) => row.id === 'shelly');
  assert.ok(shelly.now !== null && shelly.now.includes('10'), shelly.now);
  assert.equal(list.find((row) => row.id === 'dash').now, null);
});

// ---- Aufschluesselung (Task 20) -------------------------------------------

// The breakdown of GET /api/v1/performance: dashboard parts and automation
// groups. The automation group "tick" has four entries beyond the six shown.
const BREAKDOWN = {
  dashboard: {
    parts: [
      { id: 'push', pct: 6.3, clients: 3 },
      { id: 'http', pct: 1.7 },
      { id: 'mqtt', pct: 2.8 },
      { id: 'node', pct: 0.7 },
      { id: 'background', pct: 0.8 },
      { id: 'gc', pct: 1.7 },
    ],
  },
  automation: {
    groups: [
      {
        row: 'tick',
        pct: 0.6,
        items: [{ id: 'nacht', name: 'Nachtladen', kind: 'scenario', pct: 0.3 }],
        more: 4,
        more_pct: 0.2,
      },
      {
        row: 'regulator',
        pct: 0.3,
        items: [{ id: 'boiler', name: 'Überschuss in den Boiler', kind: 'scenario', pct: 0.3 }],
        more: 0,
        more_pct: 0,
      },
    ],
  },
};

test('breakdownOf() returns the parts for the dashboard, the groups for automation, null otherwise', async () => {
  const { page } = await loadedPage({ breakdown: BREAKDOWN });
  const dashboard = page.breakdownOf('energy-node-dashboard.service');
  assert.equal(dashboard.kind, 'dashboard');
  assert.deepEqual(dashboard.parts.map((part) => part.id), ['push', 'http', 'mqtt', 'node', 'background', 'gc']);
  const automation = page.breakdownOf('automation.service');
  assert.equal(automation.kind, 'automation');
  assert.deepEqual(automation.groups.map((group) => group.row), ['tick', 'regulator']);
  assert.equal(page.breakdownOf('mosquitto.service'), null);
  assert.equal(page.breakdownOf('shelly'), null);
});

test('breakdownOf() is null for every unit without breakdown data', async () => {
  const { page } = await loadedPage({ breakdown: { dashboard: null, automation: null } });
  assert.equal(page.breakdownOf('energy-node-dashboard.service'), null);
  assert.equal(page.breakdownOf('automation.service'), null);
  const bare = await loadedPage();
  assert.equal(bare.page.breakdownOf('energy-node-dashboard.service'), null);
  assert.equal(bare.page.breakdownOf('automation.service'), null);
});

test('toggleBreakdown() opens and closes one unit and keeps the state across load()', async () => {
  const calls = [];
  const { fetchImpl } = router({ '/api/v1/performance': () => reply(200, performanceBody({ breakdown: BREAKDOWN })) }, calls);
  const made = createPerformancePage({ fetchImpl });
  const { page } = made;
  await page.load();
  assert.equal(page.isOpen('energy-node-dashboard.service'), false);
  page.toggleBreakdown('energy-node-dashboard.service');
  assert.equal(page.isOpen('energy-node-dashboard.service'), true);
  assert.equal(page.isOpen('automation.service'), false, 'other units stay closed');
  await page.load();
  assert.equal(page.isOpen('energy-node-dashboard.service'), true, 'a new answer keeps it open');
  page.toggleBreakdown('energy-node-dashboard.service');
  assert.equal(page.isOpen('energy-node-dashboard.service'), false);
});

test('"weitere N" shows for a group with more than six entries, with its share', async () => {
  const { page } = await loadedPage({ breakdown: BREAKDOWN });
  const [tick, regulator] = page.breakdownOf('automation.service').groups;
  assert.equal(tick.more, 4);
  assert.equal(tick.morePctText, '0,2 %');
  assert.equal(page.moreText(tick.more), de['performance.js.breakdown.more'].replace('{count}', '4'));
  assert.equal(regulator.more, 0, 'no more line without entries beyond six');
});

test('the push part names the number of browsers', async () => {
  const { page } = await loadedPage({ breakdown: BREAKDOWN });
  const push = page.breakdownOf('energy-node-dashboard.service').parts[0];
  assert.equal(push.clients, 3);
  assert.equal(page.clientsText(push.clients), 'bei bis zu 3 Browsern');
  assert.equal(page.clientsText(1), 'bei einem Browser');
});

// ---- Learning row ----------------------------------------------------------

const LEARNING = { setting: 'auto', level: 'eco', share: 0.02, target: 0.05, per_run_ms: 840, top: [{ detector: 'switch_time', ms: 420 }] };

test('the learning row reads the learning block; without it the row is absent', async () => {
  const made = await loadedPage({ learning: LEARNING });
  assert.equal(made.page.learningSetting(), 'auto');
  assert.equal(made.page.learningSummary(), de['performance.js.learning.summary']
    .replace('{duration}', '0,8 s').replace('{share}', '2,0 %'));
  const none = await loadedPage({ learning: null });
  assert.equal(none.page.learning(), null);
  assert.equal(none.page.learningSummary(), '');
  assert.equal(none.page.learningTop().length, 0);
});

test('the selector writes learning_level, keeps the rest of the document and sends the CSRF token', async () => {
  const calls = [];
  const settingsDoc = { latitude: 52.5, longitude: 13.4, learning_level: 'auto', dashboard_title: 'Haus' };
  const { fetchImpl } = router({
    '/api/v1/performance': reply(200, performanceBody({ learning: LEARNING })),
    '/api/v1/auth/session': reply(200, { csrf_token: 'tok', edit_setup: true }),
    '/api/v1/settings': reply(200, settingsDoc),
    'PUT /api/v1/settings': reply(200, {}),
  }, calls);
  const made = createPerformancePage({ fetchImpl });
  await made.page.load();
  await made.page.pickLearning('full');
  const put = calls.find((call) => call.method === 'PUT');
  assert.ok(put, 'a PUT is sent');
  assert.equal(put.headers['X-CSRF-Token'], 'tok');
  assert.deepEqual(JSON.parse(put.body), { ...settingsDoc, learning_level: 'full' });
});

test('pickLearning() writes nothing without the setup right', async () => {
  const calls = [];
  const { fetchImpl } = router({
    '/api/v1/performance': reply(200, performanceBody({ learning: LEARNING })),
    '/api/v1/auth/session': reply(200, { csrf_token: 'tok', edit_setup: false }),
  }, calls);
  const made = createPerformancePage({ fetchImpl });
  await made.page.load();
  await made.page.pickLearning('full');
  assert.equal(calls.filter((call) => call.method === 'PUT').length, 0);
  assert.equal(made.stores.toasts.items.at(-1).severity, 'critical');
});

test('the suggest row exists only when the API gives a suggestion, and its button sets that level', async () => {
  const without = await loadedPage({ learning: LEARNING });
  assert.equal(without.page.learningSuggest(), '');

  const withSuggest = await loadedPage({ learning: { ...LEARNING, suggest: 'balanced' } });
  assert.equal(withSuggest.page.learningSuggest(), 'balanced');
  assert.equal(withSuggest.page.learningSuggestText(), de['performance.js.learning.suggest'].replace('{level}', de['performance.js.learning.level.balanced']));
});

test('a level above the measurement gets the note with the measured load, a level within it does not', async () => {
  // eco at 2 % with budget 2 s: balanced (10 s) projects to 10 %, target 10 %, so no note.
  const within = await loadedPage({ learning: { ...LEARNING, setting: 'balanced', share: 0.01 } });
  assert.equal(within.page.learningOverNote(), '');

  // full (60 s) projects 2 % * 30 = 60 %, far above the 10 % target.
  const above = await loadedPage({ learning: { ...LEARNING, setting: 'full' } });
  const note = above.page.learningOverNote();
  assert.ok(note.includes('2,0 %'), note);
  assert.ok(note.includes('60,0 %'), note);
  assert.ok(note.includes('10,0 %'), note);

  // The choice stays possible: the page still reports the chosen setting.
  assert.equal(above.page.learningSetting(), 'full');

  // A level at or below the measured one gives no note.
  const same = await loadedPage({ learning: { ...LEARNING, setting: 'eco' } });
  assert.equal(same.page.learningOverNote(), '');
  const auto = await loadedPage({ learning: LEARNING });
  assert.equal(auto.page.learningOverNote(), '');
});

test('learningDetector() names a known detector and falls back to its id', () => {
  const made = createPerformancePage();
  assert.equal(made.page.learningDetector('switch_time'), de['performance.js.learning.detector.switch_time']);
  assert.equal(made.page.learningDetector('brand_new'), 'brand_new');
});

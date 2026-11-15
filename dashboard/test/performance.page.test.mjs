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
const de = catalog('de');

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
      { id: 'shelly', current: 20, steps: [2, 3, 5, 10, 15, 20, 30, 60], floor: 2, round_s: 0.4, cost_pct: 5, right: 'system_actions', editable: true, follows: '', source: true, balance: true },
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

// routes: "METHOD /path" or "/path" -> reply, or a function returning one.
// Every call is recorded in calls for the assertions.
function router(routes, calls = []) {
  const fetchImpl = async (url, options = {}) => {
    const method = options.method || 'GET';
    calls.push({ url, method });
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

  const tight = await loadedPage({ measured_s: 21600, today_pct: 68 });
  assert.equal(tight.page.headTone(), 'warn', '2 % free is under 3 %');

  const over = await loadedPage({ measured_s: 21600, today_pct: 71 });
  assert.equal(over.page.headTone(), 'bad', 'above the target');
});

test('headTone() takes the deduction out of the available share', async () => {
  const deducted = await loadedPage({
    measured_s: 21600,
    today_pct: 65,
    deduction: { pct: 10, reasons: ['throttled'] },
  });
  assert.equal(deducted.page.available(), 60);
  assert.equal(deducted.page.headTone(), 'bad', 'above 70 - 10');

  const close = await loadedPage({
    measured_s: 21600,
    today_pct: 58,
    deduction: { pct: 10, reasons: ['throttled'] },
  });
  assert.equal(close.page.headTone(), 'warn', '2 % free of 60');
});

test('headTitle() names the used and the free or over share', async () => {
  const { page } = await loadedPage({ measured_s: 21600 });
  assert.equal(page.headTitle(), '32,1 % genutzt, 37,9 % frei');

  const over = await loadedPage({ measured_s: 21600, today_pct: 72 });
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

  const full = await loadedPage({ today_pct: 140 });
  assert.equal(full.page.meterParts()[1].w, 100);
});

// Tests for servicelogs.panel.js: the role gate, the request shapes of the
// system log endpoints, cursor-based polling with its buffer cap, and the
// stop on a failed poll. Same approach as tailscale.page.test.mjs (jsdom,
// no browser). Run with `npm test` from dashboard/.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { catalog, installI18n } from './helpers/i18n.mjs';
import { attachStores } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'servicelogs.panel.js'),
  'utf8',
);

function createServiceLogsPanel({ fetchImpl } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: 'https://dashboard.local/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  const timers = { started: [], cleared: [] };
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  dom.window.setInterval = (fn, ms) => {
    timers.started.push({ fn, ms });
    return timers.started.length;
  };
  dom.window.clearInterval = (id) => { timers.cleared.push(id); };
  installI18n(dom.window, { lang: 'de' });
  vm.runInContext(scriptSource, context);
  const component = factories.serviceLogsPanel();
  attachStores(component);
  return { component, window: dom.window, timers };
}

const json = (body, ok = true, status = ok ? 200 : 503) => ({ ok, status, json: async () => body });

const UNITS = {
  units: ['dashboard', 'caddy'],
  since_minutes: [15, 60],
  lines: [200, 500],
  default_unit: 'dashboard',
  default_since_minutes: 60,
  default_lines: 200,
};

const entry = (message, priority = 6) => ({
  time: '2026-10-21T10:00:00Z',
  priority,
  unit: 'dashboard',
  message,
  truncated: false,
});

// Returns a fetch double that answers the session, units and log URLs and
// records every URL it was asked for.
function routedFetch({ session = { csrf_token: 'tok', system_actions: true }, logs = [], units = UNITS } = {}) {
  const calls = [];
  let logIndex = 0;
  const fetchImpl = async (url) => {
    calls.push(url);
    if (url === '/api/v1/auth/session') return json(session);
    if (url === '/api/v1/system/logs/units') return json(units);
    if (url.startsWith('/api/v1/system/logs?')) {
      const next = logs[Math.min(logIndex, logs.length - 1)];
      logIndex += 1;
      return next;
    }
    throw new Error(`unexpected fetch ${url}`);
  };
  return { fetchImpl, calls };
}

const paramsOf = (url) => new URL(url, 'https://dashboard.local').searchParams;

test('init hides the section without the system_actions role', async () => {
  const { fetchImpl, calls } = routedFetch({ session: { csrf_token: 'tok', system_actions: false } });
  const { component } = createServiceLogsPanel({ fetchImpl });
  await component.init();
  assert.equal(component.canView, false);
  assert.deepEqual(calls, ['/api/v1/auth/session']);
});

test('init loads options and the default unit', async () => {
  const { fetchImpl, calls } = routedFetch({
    logs: [json({ entries: [], cursor: '', limited: false })],
  });
  const { component } = createServiceLogsPanel({ fetchImpl });
  await component.init();
  assert.equal(component.canView, true);
  assert.equal(component.options.default_unit, 'dashboard');
  assert.deepEqual(calls, [
    '/api/v1/auth/session',
    '/api/v1/system/logs/units',
    '/api/v1/system/logs?unit=dashboard&since=60&lines=200',
  ]);
});

test('load sends the search term and replaces entries', async () => {
  const { fetchImpl, calls } = routedFetch({
    logs: [json({ entries: [entry('tls handshake'), entry('tls retry')], cursor: 's=2', limited: false })],
  });
  const { component } = createServiceLogsPanel({ fetchImpl });
  component.query = 'tls';
  component.entries = [entry('old')];
  await component.load();
  const params = paramsOf(calls[0]);
  assert.equal(params.get('q'), 'tls');
  assert.equal(params.has('cursor'), false);
  assert.equal(component.entries.length, 2);
  assert.equal(component.entries[0].message, 'tls handshake');
  assert.equal(component.cursor, 's=2');
});

test('poll appends entries and sends the cursor', async () => {
  const { fetchImpl, calls } = routedFetch({
    logs: [
      json({ entries: [entry('first')], cursor: 's=1', limited: false }),
      json({ entries: [entry('second'), entry('third')], cursor: 's=3', limited: false }),
    ],
  });
  const { component } = createServiceLogsPanel({ fetchImpl });
  await component.load();
  await component.poll();
  assert.ok(calls[1].includes('cursor=s%3D1'), `poll URL was ${calls[1]}`);
  assert.equal(component.entries.length, 3);
  assert.equal(component.cursor, 's=3');
});

test('poll keeps the cursor when nothing new arrives', async () => {
  const { fetchImpl } = routedFetch({
    logs: [
      json({ entries: [entry('first')], cursor: 's=1', limited: false }),
      json({ entries: [], cursor: 's=1', limited: false }),
    ],
  });
  const { component } = createServiceLogsPanel({ fetchImpl });
  await component.load();
  await component.poll();
  assert.equal(component.cursor, 's=1');
  assert.equal(component.entries.length, 1);
});

test('poll caps the buffer at 2000 entries and drops the oldest', async () => {
  const existing = Array.from({ length: 1999 }, (_, index) => entry(`old ${index}`));
  const fresh = Array.from({ length: 5 }, (_, index) => entry(`new ${index}`));
  const { fetchImpl } = routedFetch({
    logs: [
      json({ entries: [], cursor: 's=1', limited: false }),
      json({ entries: fresh, cursor: 's=9', limited: false }),
    ],
  });
  const { component } = createServiceLogsPanel({ fetchImpl });
  await component.load();
  component.entries = existing;
  await component.poll();
  assert.equal(component.entries.length, 2000);
  assert.equal(component.entries[0].message, 'old 4');
  assert.equal(component.entries[1999].message, 'new 4');
});

test('a failed poll stops auto refresh and shows the error', async () => {
  const { fetchImpl } = routedFetch({
    logs: [
      json({ entries: [entry('first')], cursor: 's=1', limited: false }),
      json({ code: 'journal_not_permitted' }, false, 503),
    ],
  });
  const { component } = createServiceLogsPanel({ fetchImpl });
  await component.load();
  component.setAutoRefresh(true);
  assert.ok(component.pollTimer, 'auto refresh should have started');
  await component.poll();
  assert.equal(component.autoRefresh, false);
  assert.equal(component.pollTimer, null);
  assert.equal(component.error, catalog('de')['error.journal_not_permitted']);
});

test('changing the unit resets the cursor', async () => {
  const { fetchImpl, calls } = routedFetch({
    logs: [
      json({ entries: [entry('first')], cursor: 's=1', limited: false }),
      json({ entries: [], cursor: '', limited: false }),
    ],
  });
  const { component } = createServiceLogsPanel({ fetchImpl });
  await component.load();
  component.unit = 'caddy';
  await component.load();
  const params = paramsOf(calls[calls.length - 1]);
  assert.equal(params.get('unit'), 'caddy');
  assert.equal(params.has('cursor'), false);
});

test('an empty answer shows the empty state', async () => {
  const { fetchImpl } = routedFetch({
    logs: [json({ entries: [], cursor: '', limited: false })],
  });
  const { component } = createServiceLogsPanel({ fetchImpl });
  await component.load();
  assert.equal(component.loaded, true);
  assert.equal(component.entries.length, 0);
});

test('priorityClass maps journald levels', () => {
  const { component } = createServiceLogsPanel();
  const cases = [
    [0, 'log-error'], [1, 'log-error'], [2, 'log-error'], [3, 'log-error'],
    [4, 'log-warning'], [5, 'log-info'], [6, 'log-info'], [7, 'log-debug'],
  ];
  for (const [priority, expected] of cases) {
    assert.equal(component.priorityClass(entry('x', priority)), expected, `priority ${priority}`);
  }
});

test('poll is skipped while the panel is not visible', async () => {
  let calls = 0;
  const { component } = createServiceLogsPanel({
    fetchImpl: async () => { calls += 1; throw new Error('should not be called'); },
  });
  component.$el = { offsetParent: null };
  component.cursor = 's=1';
  await component.poll();
  assert.equal(calls, 0);
});

test('poll is skipped while the tab is hidden', async () => {
  let calls = 0;
  const { component, window } = createServiceLogsPanel({
    fetchImpl: async () => { calls += 1; throw new Error('should not be called'); },
  });
  Object.defineProperty(window.document, 'visibilityState', { value: 'hidden', configurable: true });
  component.cursor = 's=1';
  await component.poll();
  assert.equal(calls, 0);
});

test('load ends on the newest line like a terminal', async () => {
  const { fetchImpl } = routedFetch({
    logs: [json({ entries: [entry('first'), entry('second')], cursor: 's=2', limited: false })],
  });
  const { component } = createServiceLogsPanel({ fetchImpl });
  const list = { scrollTop: 0, scrollHeight: 640 };
  component.$refs = { list };
  component.$nextTick = (fn) => fn();
  await component.load();
  assert.equal(list.scrollTop, 640);
});

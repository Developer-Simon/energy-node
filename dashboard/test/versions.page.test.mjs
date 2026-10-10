// Tests for versions.page.js: the versions settings page. Run with `npm test`
// from dashboard/ (Node's test runner plus jsdom), same approach as
// tailscale.page.test.mjs.
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
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'versions.page.js'),
  'utf8',
);
const de = catalog('de');

function createVersionsPanel({ fetchImpl } = {}) {
  const dom = new JSDOM('<!doctype html><html><head></head><body></body></html>', {
    runScripts: 'outside-only',
    url: 'https://dashboard.local/',
  });
  installI18n(dom.window);
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  vm.runInContext(scriptSource, context);
  return { component: factories.versionsPanel(), window: dom.window };
}

// Objects from the jsdom context have other prototypes than the test's own;
// turn them into plain literals before deepEqual.
const plain = (value) => JSON.parse(JSON.stringify(value));

const jsonResponse = (body, ok = true) => ({ ok, status: ok ? 200 : 404, json: async () => body });

const SNAPSHOT = {
  bundle: { version: 'v0.7.5', built_at: '2026-09-21T10:00:00+02:00', arch: 'armv6' },
  running: { dashboard: 'v0.7.5-dev' },
  has_changelog: true,
  components: [
    { id: 'service:shelly', label: 'Shelly', kind: 'service', version: 'v0.4.0', installed: false },
    { id: 'dashboard', label: 'Dashboard', kind: 'app', version: 'v0.7.5', installed: true },
    { id: 'energy_node_common', label: 'energy_node_common', kind: 'library', version: 'v0.4.5', installed: true },
    { id: 'service:trucki', label: 'Trucki', kind: 'service', version: 'v0.4.0', installed: true },
  ],
};

async function loaded(overrides = {}) {
  const panel = createVersionsPanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/versions') return jsonResponse({ ...SNAPSHOT, ...overrides });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await panel.component.load();
  return panel;
}

test('load() fills bundle, running version and components', async () => {
  const { component } = await loaded();
  assert.equal(component.bundle.version, 'v0.7.5');
  assert.equal(component.running.dashboard, 'v0.7.5-dev');
  assert.equal(component.components.length, 4);
  assert.equal(component.hasChangelog, true);
  assert.equal(component.loading, false);
});

test('groups follow the fixed kind order, keep the order inside a kind and take titles from the catalog', async () => {
  const { component } = await loaded();
  assert.deepEqual(
    plain(component.groups.map((g) => [g.title, g.components.map((c) => c.id)])),
    [
      [de['settings.versions.kind.app'], ['dashboard']],
      [de['settings.versions.kind.service'], ['service:shelly', 'service:trucki']],
      [de['settings.versions.kind.library'], ['energy_node_common']],
    ],
  );
});

test('an unknown or missing kind is listed under "other" instead of vanishing', async () => {
  const { component } = await loaded({
    components: [
      ...SNAPSHOT.components,
      { id: 'x', label: 'X', kind: 'gadget', version: 'v1.0.0', installed: true },
      { id: 'y', label: 'Y', kind: '', version: 'v1.0.0', installed: true },
    ],
  });
  const last = component.groups[component.groups.length - 1];
  assert.equal(last.title, de['settings.versions.kind_other']);
  assert.deepEqual(plain(last.components.map((c) => c.id)), ['x', 'y']);
});

test('dashboardDiffers ignores a -dev suffix but notices a real version difference', async () => {
  const same = await loaded();
  assert.equal(same.component.dashboardDiffers, false, 'v0.7.5-dev vs v0.7.5 is the same version');
  const newer = await loaded({ running: { dashboard: 'v0.7.6' } });
  assert.equal(newer.component.dashboardDiffers, true);
  const unknown = await loaded({ running: { dashboard: 'dev' } });
  assert.equal(unknown.component.dashboardDiffers, false, 'a non-release build cannot be compared');
});

test('builtAt is formatted through I18n, not shown raw', async () => {
  const { component } = await loaded();
  assert.notEqual(component.builtAt, '');
  assert.notEqual(component.builtAt, SNAPSHOT.bundle.built_at);
});

test('a node the installer never touched shows no bundle and no components', async () => {
  const { component } = await loaded({ bundle: null, components: [], has_changelog: false });
  assert.equal(component.bundle, null);
  assert.deepEqual(plain(component.groups), []);
  assert.equal(component.builtAt, '');
});

test('load() turns a failed request into the catalog text of its error code', async () => {
  const { component } = createVersionsPanel({
    fetchImpl: async () => jsonResponse({ code: 'versions_unreadable', message: 'server text' }, false),
  });
  await component.load();
  assert.equal(component.error, de['error.versions_unreadable']);
  assert.equal(component.loading, false);
});

test('toggle() fetches a component changelog once and caches it', async () => {
  const calls = [];
  const panel = createVersionsPanel({
    fetchImpl: async (url) => {
      calls.push(url);
      if (url === '/api/v1/versions') return jsonResponse(SNAPSHOT);
      if (url === '/api/v1/changelog?component=service%3Ashelly') {
        return jsonResponse({ components: [{ id: 'service:shelly', releases: [{ version: 'v0.4.0', date: '2026-09-15', groups: [] }] }] });
      }
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await panel.component.load();
  await panel.component.toggle('service:shelly');
  assert.equal(panel.component.open['service:shelly'], true);
  assert.equal(panel.component.releases['service:shelly'][0].version, 'v0.4.0');
  await panel.component.toggle('service:shelly'); // close
  await panel.component.toggle('service:shelly'); // open again
  assert.equal(calls.filter((url) => url.startsWith('/api/v1/changelog')).length, 1, 'the second open must not refetch');
});

test('a failing changelog request is reported per component', async () => {
  const panel = createVersionsPanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/versions') return jsonResponse(SNAPSHOT);
      return jsonResponse({ code: 'no_changelog', message: 'server text' }, false);
    },
  });
  await panel.component.load();
  await panel.component.toggle('dashboard');
  assert.equal(panel.component.releaseErrors.dashboard, de['error.no_changelog']);
  assert.equal(panel.component.releases.dashboard, undefined, 'a failed load may be retried on the next open');
});

test('entryText prefixes the scope when there is one', async () => {
  const { component } = createVersionsPanel();
  assert.equal(component.entryText({ text: 'fix it', scope: 'installer' }), 'installer: fix it');
  assert.equal(component.entryText({ text: 'fix it' }), 'fix it');
});

test('releaseDate formats a plain date through I18n and keeps an empty one empty', async () => {
  const { component } = createVersionsPanel();
  const shown = component.releaseDate('2026-09-21');
  assert.notEqual(shown, '2026-09-21');
  assert.match(shown, /2026/);
  assert.equal(component.releaseDate(''), '');
});

test('groupTitle translates known change types and keeps the label of unknown ones', async () => {
  const { component } = createVersionsPanel();
  assert.equal(component.groupTitle({ type: 'feat', label: 'Features' }), de['settings.versions.change_type.feat']);
  assert.equal(component.groupTitle({ type: 'wibble', label: 'Wibble' }), 'Wibble');
  assert.equal(component.groupTitle({ label: 'Features' }), 'Features');
  assert.equal(component.groupTitle({ type: 'other', label: 'Other' }), de['settings.versions.change_type_other']);
});

test('load() also reads the cached update status and the system_actions flag', async () => {
  const panel = createVersionsPanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/versions') return jsonResponse(SNAPSHOT);
      if (url === '/api/v1/auth/session') return jsonResponse({ system_actions: true });
      if (url === '/api/v1/updates/status') return jsonResponse({ available: true, latest: '1.4.2', notes_url: 'https://example.invalid' });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await panel.component.load();
  assert.equal(panel.component.updateStatus.available, true);
  assert.equal(panel.component.updateStatus.latest, '1.4.2');
  assert.equal(panel.component.canSystemActions, true);
});

test('an update status that was never checked counts as no result', async () => {
  const panel = createVersionsPanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/versions') return jsonResponse(SNAPSHOT);
      if (url === '/api/v1/auth/session') return jsonResponse({});
      if (url === '/api/v1/updates/status') return jsonResponse({ checked: false });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await panel.component.load();
  assert.equal(panel.component.updateStatus, null);
  assert.equal(panel.component.canSystemActions, false);
});

test('checkForUpdates() stores the answer and ignores a second click while running', async () => {
  let calls = 0;
  const panel = createVersionsPanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/updates/check') {
        calls += 1;
        return jsonResponse({ available: false, latest: '1.4.1' });
      }
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  attachStores(panel.component);
  panel.component.checkingForUpdates = true;
  await panel.component.checkForUpdates();
  assert.equal(calls, 0, 'a running check must not start a second one');
  panel.component.checkingForUpdates = false;
  await panel.component.checkForUpdates();
  assert.equal(calls, 1);
  assert.equal(panel.component.updateStatus.available, false);
  assert.equal(panel.component.checkingForUpdates, false);
});

test('checkForUpdates() reports a failure through the toast store', async () => {
  const panel = createVersionsPanel({
    fetchImpl: async () => jsonResponse({ code: 'updates_check_failed', message: 'server text' }, false),
  });
  const stores = attachStores(panel.component);
  await panel.component.checkForUpdates();
  assert.equal(stores.toasts.items.length, 1);
  assert.equal(stores.toasts.items[0].message, de['error.updates_check_failed']);
});

// Highlights / Alles: changelog.json flags every entry. Highlights is the default.
const FLAGGED_RELEASES = [
  { version: 'v0.8.0', date: '2026-10-01', groups: [
    { type: 'feat', label: 'Features', entries: [
      { text: 'show what is new before an update', breaking: false, pr: 82, highlight: true },
      { text: 'add the switch to the screen', breaking: false, highlight: false },
    ] },
    { type: 'test', label: 'Tests', entries: [{ text: 'cover the switch', breaking: false, highlight: false }] },
  ] },
  { version: 'v0.7.9', date: '2026-09-30', groups: [
    { type: 'refactor', label: 'Refactors', entries: [{ text: 'move a helper', breaking: false, pr: 80, highlight: false }] },
  ] },
];
const shownTexts = (component, id) => plain(component.shownReleases(id).map((r) => [r.version, r.groups.flatMap((g) => g.entries.map((e) => e.text))]));

test('shownReleases keeps the highlights by default and everything on "all"', async () => {
  const { component } = await loaded();
  component.releases = { dashboard: FLAGGED_RELEASES };
  assert.equal(component.mode, 'highlights');
  assert.deepEqual(shownTexts(component, 'dashboard'), [['v0.8.0', ['show what is new before an update']]]);
  assert.equal(component.noHighlights('dashboard'), false);
  component.mode = 'all';
  assert.deepEqual(shownTexts(component, 'dashboard'), [
    ['v0.8.0', ['show what is new before an update', 'add the switch to the screen', 'cover the switch']],
    ['v0.7.9', ['move a helper']],
  ]);
});

test('a component with details only says so in the highlights view', async () => {
  const { component } = await loaded();
  component.releases = { 'service:trucki': [FLAGGED_RELEASES[1]] };
  assert.deepEqual(shownTexts(component, 'service:trucki'), []);
  assert.equal(component.noHighlights('service:trucki'), true);
  component.mode = 'all';
  assert.equal(component.noHighlights('service:trucki'), false);
});

test('a changelog without flags is shown in full in either view', async () => {
  const { component } = await loaded();
  const unflagged = [{ version: 'v0.4.0', date: '2026-09-15', groups: [{ type: 'test', label: 'Tests', entries: [{ text: 'old', breaking: false }] }] }];
  component.releases = { 'service:shelly': unflagged };
  assert.deepEqual(shownTexts(component, 'service:shelly'), [['v0.4.0', ['old']]]);
  assert.equal(component.noHighlights('service:shelly'), false);
});

test('setUpdateCheck() changes only update_check_disabled and tells the settings form', async () => {
  const puts = [];
  const { component, window } = createVersionsPanel({
    fetchImpl: async (url, options) => {
      if (url === '/api/v1/settings' && options && options.method === 'PUT') {
        puts.push(JSON.parse(options.body));
        return jsonResponse({});
      }
      if (url === '/api/v1/settings') return jsonResponse({ theme: 'mint', update_check_disabled: false });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  attachStores(component);
  const heard = [];
  window.document.addEventListener('update-check-setting-changed', (event) => heard.push(event.detail.disabled));
  await component.setUpdateCheck(false);
  assert.deepEqual(plain(puts), [{ theme: 'mint', update_check_disabled: true }]);
  assert.equal(component.updateCheckDisabled, true);
  assert.equal(component.savingUpdateCheck, false);
  assert.deepEqual(heard, [true]);
});

test('setUpdateCheck() restores the switch and toasts when saving fails', async () => {
  const { component } = createVersionsPanel({
    fetchImpl: async (url, options) => {
      if (options && options.method === 'PUT') return jsonResponse({ code: 'settings_rejected', message: 'server text' }, false);
      return jsonResponse({ update_check_disabled: false });
    },
  });
  attachStores(component);
  await component.setUpdateCheck(false);
  assert.equal(component.updateCheckDisabled, false);
  assert.equal(component.savingUpdateCheck, false);
  assert.deepEqual(component.$store.toasts.criticals, [de['error.settings_rejected']]);
});

// Regression tests for settings.page.js: the Choices.js wiring for the
// wide-panels/status-bar-items multi-selects. initChoices()/setChoicesSelection()
// bridge Alpine's widePanels/statusBarItems arrays to the Choices.js API (which
// only reads the underlying <select> once, at construction time), so both need
// direct coverage independent of the vendored library itself.
// Run with `npm test` from dashboard/, same approach as mqtt.page.test.mjs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { attachStores } from './helpers/notify-stores.mjs';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'settings.page.js'),
  'utf8',
);

class FakeChoices {
  constructor(element, options) {
    this.element = element;
    this.options = options;
    this.removeActiveItemsCalls = 0;
    this.setChoiceByValueCalls = [];
    this.currentValues = [];
    FakeChoices.instances.push(this);
  }
  removeActiveItems() { this.removeActiveItemsCalls += 1; this.currentValues = []; }
  setChoiceByValue(values) {
    this.setChoiceByValueCalls.push(values);
    this.currentValues = [...this.currentValues, ...(Array.isArray(values) ? values : [values])];
  }
  // Real Choices.getValue(true) returns the selected values array; used by
  // syncFromChoices() instead of trusting the "change" event payload (see
  // settings.page.js for why - Alpine's x-model reads CustomEvent.detail
  // directly for select[multiple], which is not what Choices puts there).
  getValue(asValue) { return asValue ? [...this.currentValues] : this.currentValues.map((value) => ({ value })); }
}

function createSettingsPanel({ fetchImpl, withChoices = true } = {}) {
  FakeChoices.instances = [];
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: 'https://dashboard.local/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  if (withChoices) dom.window.Choices = FakeChoices;
  installI18n(dom.window);
  vm.runInContext(scriptSource, context);
  const component = factories.settingsPanel();
  const stores = attachStores(component);
  component.$refs = {
    widePanelsSelect: dom.window.document.createElement('select'),
    statusBarItemsSelect: dom.window.document.createElement('select'),
  };
  return { component, window: dom.window, stores };
}

const jsonResponse = (body, ok = true) => ({ ok, status: ok ? 200 : 400, json: async () => body });

const settingsResponse = (overrides = {}) => ({
  health_score_threshold: 180, sweep_interval_seconds: 300,
  show_runtime_status: true, device_view_mode: 'compact', theme: 'mint',
  show_config_entities_on_tile: false, show_diagnostic_entities_on_tile: false,
  live_update_interval_seconds: 3, wide_panels: [], status_bar_items: [],
  number_format: 'auto', number_grouping: 'match',
  ...overrides,
});

const stubbedLoadFetch = (overrides) => async (url) => {
  if (url === '/api/v1/settings') return jsonResponse(settingsResponse(overrides));
  if (url === '/api/v1/auth/session') return jsonResponse({});
  if (url === '/api/v1/health/storage') return jsonResponse({ available: false });
  throw new Error(`unexpected fetch ${url}`);
};

// component runs inside a jsdom vm context, so arrays it builds (spreads,
// [...x]) belong to that context's own Array - structurally identical to an
// outer-realm array but not assert.deepEqual-comparable to one. Same fix as
// energy.page.test.mjs uses for the same reason.
const plain = (value) => JSON.parse(JSON.stringify(value));

test('toggleWidePanel/toggleStatusBarItem were removed with the checkbox-list markup', () => {
  const { component } = createSettingsPanel();
  assert.equal(component.toggleWidePanel, undefined);
  assert.equal(component.toggleStatusBarItem, undefined);
});

// Regression test for the actual bug reported after shipping: with x-model on
// a Choices-controlled <select multiple>, Alpine reads the "change" event's
// CustomEvent.detail directly as the new model value. Choices dispatches that
// event with detail={value: <single item>}, so widePanels/statusBarItems
// silently turned into a plain object - "not iterable" as soon as payload()
// tried to spread it. syncFromChoices() must ignore the event entirely and
// ask the Choices instance for its current full selection instead.
test('syncFromChoices() reads the full current selection from Choices, not an event payload', () => {
  const { component } = createSettingsPanel();
  component.initChoices();
  const instance = component.widePanelsChoices;

  instance.removeActiveItems();
  instance.setChoiceByValue(['overview', 'layout']);
  assert.deepEqual(
    plain(component.syncFromChoices(instance, component.widePanelOptions)),
    ['overview', 'layout'],
  );
});

test('syncFromChoices() preserves widePanelOptions order regardless of selection order', () => {
  const { component } = createSettingsPanel();
  component.initChoices();
  const instance = component.widePanelsChoices;

  instance.removeActiveItems();
  instance.setChoiceByValue(['layout', 'overview']);
  assert.deepEqual(
    plain(component.syncFromChoices(instance, component.widePanelOptions)),
    ['overview', 'layout'],
  );
});

test('initChoices() creates exactly one Choices instance per select, guarded against re-init', () => {
  const { component } = createSettingsPanel();
  component.initChoices();
  component.initChoices();
  assert.equal(FakeChoices.instances.length, 2);
  assert.equal(component.widePanelsChoices, FakeChoices.instances[0]);
  assert.equal(component.statusBarItemsChoices, FakeChoices.instances[1]);
});

test('initChoices() is a no-op when Choices.js has not loaded yet', () => {
  const { component } = createSettingsPanel({ withChoices: false });
  component.initChoices();
  assert.equal(component.widePanelsChoices, null);
  assert.equal(component.statusBarItemsChoices, null);
});

test('setChoicesSelection() clears then re-selects, and skips setChoiceByValue for an empty selection', () => {
  const { component } = createSettingsPanel();
  const instance = new FakeChoices({}, {});

  component.setChoicesSelection(instance, ['energy', 'layout']);
  assert.equal(instance.removeActiveItemsCalls, 1);
  assert.deepEqual(instance.setChoiceByValueCalls, [['energy', 'layout']]);

  component.setChoicesSelection(instance, []);
  assert.equal(instance.removeActiveItemsCalls, 2);
  assert.deepEqual(instance.setChoiceByValueCalls, [['energy', 'layout']]);

  assert.doesNotThrow(() => component.setChoicesSelection(null, ['x']));
});

test('load() pushes the server selection into both Choices instances', async () => {
  const { component } = createSettingsPanel({
    fetchImpl: stubbedLoadFetch({ wide_panels: ['overview', 'energy'], status_bar_items: ['mqtt'] }),
  });
  // initChoices() runs once at panel init (empty widePanels/statusBarItems at
  // that point), so it already contributes one removeActiveItems() call each
  // before load() ever runs - same as the real x-init="$nextTick(() =>
  // initChoices()); load()" wiring in settings.html.
  component.initChoices();
  await component.load();

  assert.deepEqual(plain(component.widePanels), ['overview', 'energy']);
  assert.deepEqual(plain(component.statusBarItems), ['mqtt']);
  assert.equal(component.widePanelsChoices.removeActiveItemsCalls, 2);
  assert.deepEqual(plain(component.widePanelsChoices.setChoiceByValueCalls), [['overview', 'energy']]);
  assert.equal(component.statusBarItemsChoices.removeActiveItemsCalls, 2);
  assert.deepEqual(plain(component.statusBarItemsChoices.setChoiceByValueCalls), [['mqtt']]);
});

test('load() called again (e.g. restoring a settings revision) replaces the prior selection instead of adding to it', async () => {
  const { component, window } = createSettingsPanel({
    fetchImpl: stubbedLoadFetch({ wide_panels: ['overview'], status_bar_items: [] }),
  });
  component.initChoices();
  await component.load();
  assert.deepEqual(plain(component.widePanelsChoices.setChoiceByValueCalls), [['overview']]);
  assert.deepEqual(component.statusBarItemsChoices.setChoiceByValueCalls, []);

  window.fetch = stubbedLoadFetch({ wide_panels: ['layout', 'devicemap'], status_bar_items: ['cache'] });
  await component.load();

  assert.equal(component.widePanelsChoices.removeActiveItemsCalls, 3);
  assert.deepEqual(plain(component.widePanelsChoices.setChoiceByValueCalls), [['overview'], ['layout', 'devicemap']]);
  assert.equal(component.statusBarItemsChoices.removeActiveItemsCalls, 3);
  assert.deepEqual(plain(component.statusBarItemsChoices.setChoiceByValueCalls), [['cache']]);
});

test('payload() reports widePanels/statusBarItems unchanged', () => {
  const { component } = createSettingsPanel();
  component.widePanels = ['config', 'energy'];
  component.statusBarItems = ['storage', 'uptime'];
  const payload = component.payload();
  assert.deepEqual(plain(payload.wide_panels), ['config', 'energy']);
  assert.deepEqual(plain(payload.status_bar_items), ['storage', 'uptime']);
});

test('load() and payload() carry the number format', async () => {
  const { component, window } = createSettingsPanel();
  window.fetch = stubbedLoadFetch({ number_format: 'comma', number_grouping: 'thin' });
  await component.load();
  assert.equal(component.numberFormat, 'comma');
  assert.equal(component.numberGrouping, 'thin');
  const payload = component.payload();
  assert.equal(payload.number_format, 'comma');
  assert.equal(payload.number_grouping, 'thin');
});

test('load() falls back to auto/match for unknown values', async () => {
  const { component, window } = createSettingsPanel();
  window.fetch = stubbedLoadFetch({ number_format: 'xx', number_grouping: undefined });
  await component.load();
  assert.equal(component.numberFormat, 'auto');
  assert.equal(component.numberGrouping, 'match');
});

test('save() reloads the page only when the number format changed', async () => {
  const { component, window } = createSettingsPanel();
  window.fetch = stubbedLoadFetch({});
  await component.load();
  let reloads = 0;
  component.reloadPage = () => { reloads += 1; };
  window.fetch = async () => jsonResponse({});
  await component.save();
  assert.equal(reloads, 0);
  component.numberFormat = 'point';
  await component.save();
  assert.equal(reloads, 1);
});

test('save() reloads only once when language and number format change together', async () => {
  const { component, window } = createSettingsPanel();
  window.fetch = stubbedLoadFetch({});
  await component.load();
  let reloads = 0;
  let languageSwitches = 0;
  const originalI18n = window.I18n;
  window.I18n = {
    lang: 'de',
    setLanguage: () => { languageSwitches += 1; },
    t: (key, params) => originalI18n.t(key, params),
    formatNumber: originalI18n.formatNumber,
  };
  component.reloadPage = () => { reloads += 1; };
  window.fetch = async () => jsonResponse({});
  component.uiLanguage = 'en';
  component.numberFormat = 'point';
  await component.save();
  assert.equal(languageSwitches, 1);
  assert.equal(reloads, 0);
});

test('storageText() formats numeric params before translation', async () => {
  const { component, window } = createSettingsPanel();
  // Mock I18n with German formatting (comma decimal, period thousands)
  window.I18n = {
    lang: 'de',
    formatNumber: (n) => n.toString().replace('.', ','),
    t: (key, params) => {
      if (key === 'storage_health.remaining.years' && params) {
        return `ca. ${params.min} bis ${params.max} Jahre`;
      }
      return key;
    },
  };
  
  const result = component.storageText('storage_health.remaining.years', { min: 1.5, max: 2 });
  assert.match(result, /1,5.*2/); // German format: 1,5 and 2
});

// settingsPage() with a given query string, for the ?page= deep link.
function createSettingsPage(search) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: `https://dashboard.local/${search}`,
  });
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = async () => { throw new Error('fetch should not be called'); };
  installI18n(dom.window);
  vm.runInContext(scriptSource, dom.getInternalVMContext());
  const page = factories.settingsPage();
  page.$nextTick = () => {};
  return page;
}

test('settingsPage opens the sub page named in ?page=', () => {
  const page = createSettingsPage('?panel=settings&page=versions');
  page.init();
  assert.equal(page.activeSettingsPage, 'versions');
});

test('settingsPage ignores an unknown ?page=', () => {
  const page = createSettingsPage('?page=nope');
  page.init();
  assert.equal(page.activeSettingsPage, 'general');
});

// The sidebar is the one source of the sub pages: the narrow picker and its
// sheet read the same entries from it, so they only ever offer the installed
// services with their translated texts.
function createSidebar() {
  const tab = (page, icon, title, desc) => `<button role="tab" aria-controls="settings-${page}">
      <svg><use href="#${icon}"></use></svg><span class="settings-nav-title">${title}</span><span class="settings-nav-desc">${desc}</span></button>`;
  const dom = new JSDOM(`<!doctype html><html><body><div id="list">
    ${tab('general', 'ico-gear', 'General', 'Operation and storage')}
    ${tab('display', 'ico-layers', 'Appearance', 'Display, language and tiles')}
    ${tab('mqtt', 'ico-mqtt', 'MQTT', 'Broker and bridge')}
    ${tab('system', 'ico-info', 'System', 'Access and restart')}
    </div><button id="picker"></button></body></html>`, { runScripts: 'outside-only', url: 'https://dashboard.local/' });
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  vm.runInContext(scriptSource, dom.getInternalVMContext());
  const component = factories.settingsPage();
  const doc = dom.window.document;
  component.$refs = { subnav: doc.getElementById('list'), picker: doc.getElementById('picker') };
  component.$nextTick = (fn) => fn();
  component.collectPages();
  return { component, doc };
}

test('collectPages() reads page, icon, title and description from the sidebar', () => {
  const { component } = createSidebar();
  assert.deepEqual(plain(component.pages[2]), { page: 'mqtt', icon: 'ico-mqtt', label: 'MQTT', desc: 'Broker and bridge' });
  assert.deepEqual(plain(component.pages.map((page) => page.page)), ['general', 'display', 'mqtt', 'system']);
});

test('currentPage() follows the active sub page, unknown pages fall back to the first', () => {
  const { component } = createSidebar();
  component.activeSettingsPage = 'mqtt';
  assert.equal(component.currentIndex(), 2);
  assert.equal(component.currentPage().label, 'MQTT');
  component.activeSettingsPage = 'tailscale';
  assert.equal(component.currentPage().page, 'general');
});

test('a sideways swipe on the picker steps to the neighbouring page and stops at the ends', () => {
  const { component } = createSidebar();
  component.activeSettingsPage = 'display';
  component.pickerSwipeStart({ clientX: 200 });
  component.pickerSwipeEnd({ clientX: 120 });
  assert.equal(component.activeSettingsPage, 'mqtt');
  component.pickerSwipeStart({ clientX: 100 });
  component.pickerSwipeEnd({ clientX: 180 });
  assert.equal(component.activeSettingsPage, 'display');
  component.activeSettingsPage = 'system';
  component.pickerSwipeStart({ clientX: 200 });
  component.pickerSwipeEnd({ clientX: 100 });
  assert.equal(component.activeSettingsPage, 'system');
});

test('the click that ends a swipe does not open the sheet, a plain tap does', () => {
  const { component } = createSidebar();
  component.pickerSwipeStart({ clientX: 200 });
  component.pickerSwipeEnd({ clientX: 120 });
  component.openSheet();
  assert.equal(component.sheetOpen, false);
  component.pickerSwipeStart({ clientX: 200 });
  component.pickerSwipeEnd({ clientX: 195 });
  component.openSheet();
  assert.equal(component.sheetOpen, true);
});

test('choosing a page in the sheet switches to it, closes the sheet and returns focus to the picker', () => {
  const { component, doc } = createSidebar();
  component.openSheet();
  component.choosePage('system');
  assert.equal(component.activeSettingsPage, 'system');
  assert.equal(component.sheetOpen, false);
  assert.equal(doc.activeElement, doc.getElementById('picker'));
});

test('pulling the grab handle down far enough closes the sheet, a short pull does not', () => {
  const { component } = createSidebar();
  const handle = { setPointerCapture() {} };
  component.openSheet();
  component.sheetDragStart({ clientY: 400, pointerId: 1, currentTarget: handle });
  component.sheetDragEnd({ clientY: 430 });
  assert.equal(component.sheetOpen, true);
  component.sheetDragStart({ clientY: 400, pointerId: 1, currentTarget: handle });
  component.sheetDragEnd({ clientY: 500 });
  assert.equal(component.sheetOpen, false);
});

// The health threshold is measured in seconds. The stepper, the form field
// bounds and valid() must all agree with the server schema (minimum 10,
// maximum 3600) so the form can never produce a value that saving rejects.
test('health threshold is in seconds: default 180, stepper bounds 10 to 3600', () => {
  const { component } = createSettingsPanel();
  assert.equal(component.healthScoreThreshold, 180);

  component.healthScoreThreshold = 10;
  component.stepField('healthScoreThreshold', -1);
  assert.equal(component.healthScoreThreshold, 10);

  component.healthScoreThreshold = 3600;
  component.stepField('healthScoreThreshold', 1);
  assert.equal(component.healthScoreThreshold, 3600);
});

test('valid() rejects a health threshold below 10 seconds or above 3600', () => {
  const { component } = createSettingsPanel();
  component.healthScoreThreshold = 9;
  assert.equal(component.valid, false);
  component.healthScoreThreshold = 10;
  assert.equal(component.valid, true);
  component.healthScoreThreshold = 3600;
  assert.equal(component.valid, true);
  component.healthScoreThreshold = 3601;
  assert.equal(component.valid, false);
});

// The Konto card in Allgemein: own password, HTTP password and the notice for
// a session that signed in over HTTP. Guests (and later HA accounts) get no card.
const accountFetch = (session, calls = []) => async (url, options) => {
  if (url === '/api/v1/settings') return jsonResponse(settingsResponse());
  if (url === '/api/v1/auth/session') return jsonResponse(session);
  if (url === '/api/v1/health/storage') return jsonResponse({ available: false });
  if (url.startsWith('/api/v1/users/me/')) {
    calls.push({ url, options });
    return jsonResponse({ status: 'ok' });
  }
  throw new Error(`unexpected fetch ${url}`);
};

const signedInSession = (overrides = {}) => ({
  username: 'anna', guest: false, restricted: false, http_login: false,
  csrf_token: 'csrf-1', ...overrides,
});

test('the Konto card is hidden for guest sessions', async () => {
  const { component } = createSettingsPanel({
    fetchImpl: accountFetch(signedInSession({ username: 'guest', guest: true })),
  });
  await component.load();
  assert.equal(component.accountVisible, false);
});

test('the Konto card is shown for a signed in account', async () => {
  const { component } = createSettingsPanel({ fetchImpl: accountFetch(signedInSession()) });
  await component.load();
  assert.equal(component.accountVisible, true);
  assert.equal(component.accountUsername, 'anna');
});

test('a session over HTTP (restricted: true) sets the notice flag', async () => {
  const { component } = createSettingsPanel({
    fetchImpl: accountFetch(signedInSession({ restricted: true })),
  });
  await component.load();
  assert.equal(component.accountRestricted, true);
});

test('a session over HTTPS has no restricted notice', async () => {
  const { component } = createSettingsPanel({ fetchImpl: accountFetch(signedInSession()) });
  await component.load();
  assert.equal(component.accountRestricted, false);
});

test('the HTTP password block follows http_login from the session', async () => {
  const withLogin = createSettingsPanel({ fetchImpl: accountFetch(signedInSession({ http_login: true })) });
  await withLogin.component.load();
  assert.equal(withLogin.component.accountHTTPLogin, true);

  const without = createSettingsPanel({ fetchImpl: accountFetch(signedInSession()) });
  await without.component.load();
  assert.equal(without.component.accountHTTPLogin, false);
});

test('changing the own password sends old_password and password to me/password', async () => {
  const calls = [];
  const { component, stores } = createSettingsPanel({ fetchImpl: accountFetch(signedInSession(), calls) });
  await component.load();
  component.passwordForm = { old: 'altes-passwort', next: 'neues-passwort-1', repeat: 'neues-passwort-1' };

  const ok = await component.changeOwnPassword();

  assert.equal(ok, true);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/v1/users/me/password');
  assert.equal(calls[0].options.method, 'PUT');
  assert.equal(calls[0].options.headers['X-CSRF-Token'], 'csrf-1');
  assert.deepEqual(JSON.parse(calls[0].options.body), { old_password: 'altes-passwort', password: 'neues-passwort-1' });
  assert.equal(component.passwordForm.next, '');
  assert.equal(stores.toasts.items.length, 1);
  assert.equal(stores.toasts.items[0].severity, 'info');
});

test('a repeat that differs from the new password makes no request and shows an inline error', async () => {
  const calls = [];
  const { component } = createSettingsPanel({ fetchImpl: accountFetch(signedInSession(), calls) });
  await component.load();
  component.passwordForm = { old: 'altes-passwort', next: 'neues-passwort-1', repeat: 'neues-passwort-2' };

  const ok = await component.changeOwnPassword();

  assert.equal(ok, false);
  assert.equal(calls.length, 0);
  assert.notEqual(component.passwordError, '');
});

test('a new password under 10 characters makes no request', async () => {
  const calls = [];
  const { component } = createSettingsPanel({ fetchImpl: accountFetch(signedInSession(), calls) });
  await component.load();
  component.passwordForm = { old: 'altes-passwort', next: 'kurz', repeat: 'kurz' };

  assert.equal(await component.changeOwnPassword(), false);
  assert.equal(calls.length, 0);
  assert.notEqual(component.passwordError, '');
});

test('an empty HTTP password removes it via me/http-password with an empty body', async () => {
  const calls = [];
  const { component } = createSettingsPanel({
    fetchImpl: accountFetch(signedInSession({ http_login: true }), calls),
  });
  await component.load();
  component.httpPasswordDraft = '';

  await component.saveHTTPPassword();

  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/v1/users/me/http-password');
  assert.equal(calls[0].options.method, 'PUT');
  assert.deepEqual(JSON.parse(calls[0].options.body), { password: '' });
});

test('the own password check follows password_min_length from the session', async () => {
  const calls = [];
  const { component } = createSettingsPanel({
    fetchImpl: accountFetch(signedInSession({ password_min_length: 12 }), calls),
  });
  await component.load();
  assert.equal(component.passwordMin, 12);

  component.passwordForm = { old: 'altes-passwort', next: 'a'.repeat(11), repeat: 'a'.repeat(11) };
  assert.equal(await component.changeOwnPassword(), false);
  assert.equal(calls.length, 0, 'eleven characters make no request for a minimum of 12');
  assert.notEqual(component.passwordError, '');

  component.passwordForm = { old: 'altes-passwort', next: 'ä'.repeat(12), repeat: 'ä'.repeat(12) };
  assert.equal(await component.changeOwnPassword(), true);
  assert.equal(calls.length, 1);
});

test('the own password keeps the minimum of 10 until the session says otherwise', async () => {
  const { component } = createSettingsPanel({ fetchImpl: accountFetch(signedInSession()) });
  assert.equal(component.passwordMin, 10);
  await component.load();
  assert.equal(component.passwordMin, 10);
  assert.equal(component.httpPasswordValid, true);
  component.httpPasswordDraft = 'a'.repeat(9);
  assert.equal(component.httpPasswordValid, false, 'nine characters are below the minimum');
});

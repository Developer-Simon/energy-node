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
  health_score_threshold: 3, sweep_interval_seconds: 300, show_discovery_tooltips: true,
  show_runtime_status: true, device_view_mode: 'compact', theme: 'mint',
  show_config_entities_on_tile: false, show_diagnostic_entities_on_tile: false,
  live_update_interval_seconds: 3, wide_panels: [], status_bar_items: [],
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

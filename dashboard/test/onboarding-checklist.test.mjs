// Tests for the checklist page (onboarding-checklist.js) on the real onboarding
// store: row states, the counter, other-device highlights without a button, and
// the deep link that picks the mode. Run with `node --test` from dashboard/.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = (name) => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');
const sources = ['onboarding-steps.js', 'onboarding-model.js', 'onboarding.js', 'onboarding-checklist.js'].map((name) => [name, read(name)]);

// Schritte des Servers mit tailscale (installiert), in Katalogreihenfolge.
const SERVER_ITEMS = [
  ['users', 'access', true], ['guest', 'access', true], ['tailscale', 'access', false],
  ['companion', 'access', false], ['theme', 'display', true],
  ['title', 'display', true], ['statusbar', 'display', true], ['tabwidth', 'display', true],
  ['tiles', 'display', false], ['updates', 'system', true], ['metrics', 'system', true],
  ['config', 'devices', false], ['devices', 'devices', false], ['roles', 'devices', false],
  ['devicemap', 'devices', false],
].map(([id, area, installer]) => ({ id, area, installer, state: '' }));

// Loads the page scripts into a jsdom window and returns the store plus a
// checklist component bound to it. role decides admin or guest, width the device.
async function load({ url = 'http://localhost/', role = 'admin', width = 1024, explore = null } = {}) {
  const dom = new JSDOM('<!doctype html><html><body><ul id="root"></ul></body></html>', { runScripts: 'outside-only', url, pretendToBeVisual: true });
  const { window } = dom;
  Object.defineProperty(window, 'innerWidth', { value: width, configurable: true });
  // Die Schritte des Servers (setup.items), wie steps.go sie in dieser Reihenfolge liefert.
  const progress = { role, setup: { done: false, steps: {}, items: SERVER_ITEMS }, user: { seen: [], welcomed: false, run: null, queue: [] } };
  const calls = [];
  window.fetch = async (requestUrl, options = {}) => {
    calls.push({ url: requestUrl, method: options.method || 'GET', body: options.body ? JSON.parse(options.body) : undefined });
    const body = requestUrl === '/api/v1/auth/session' ? { csrf_token: 'tok' } : progress;
    return { ok: true, status: 200, json: async () => body };
  };
  const stores = {};
  const factories = {};
  window.Alpine = {
    magic() {},
    store(name, value) {
      if (value !== undefined) stores[name] = value;
      return stores[name];
    },
    data(name, fn) { factories[name] = fn; },
    effect() {},
  };
  // Stands in for the spotlight, which is not loaded here: every element counts as visible.
  window.obSpotlight = { visibleRect: () => ({ top: 0, left: 0, width: 1, height: 1 }) };
  installI18n(window);
  const context = dom.getInternalVMContext();
  for (const [, source] of sources) vm.runInContext(source, context);
  if (explore) window.obSteps.explore = explore;
  window.document.dispatchEvent(new window.Event('alpine:init'));
  await new Promise((resolve) => setImmediate(resolve));
  const store = stores.onboarding;
  window.obOnboardingStore = store;
  const component = factories.obChecklist();
  component.$store = { onboarding: store };
  component.$root = window.document.getElementById('root');
  return { window, store, component, calls, t: (key, params) => window.I18n.t(key, params) };
}

const setupItem = (window, id) => [...window.obOnboardingStore.setupItems, ...window.obSteps.installerOnly].find((item) => item.id === id);

test('setup rows show the state, its sub line and the action', async () => {
  const { window, store, component: cl, t } = await load();
  store.status = { users: 'done', guest: 'installer', config: 'later' };

  const users = setupItem(window, 'users');
  assert.equal(cl.stateOf(users), 'done');
  assert.equal(cl.subOf(users), t('onboarding.checklist.sub_done'));
  assert.equal(cl.actionOf(users), t('onboarding.checklist.action_change'));

  const guest = setupItem(window, 'guest');
  assert.equal(cl.subOf(guest), t('onboarding.checklist.sub_installer'));
  assert.equal(cl.actionOf(guest), t('onboarding.checklist.action_change'));

  const config = setupItem(window, 'config');
  assert.equal(cl.subOf(config), t('onboarding.checklist.sub_later'));
  assert.equal(cl.actionOf(config), t('onboarding.checklist.action_later'));

  // Offen und im Installer abgefragt: die Zeile sagt, dass es dort auch steht.
  const theme = setupItem(window, 'theme');
  assert.equal(cl.stateOf(theme), 'open');
  assert.equal(cl.subOf(theme), t('onboarding.checklist.sub_installer_also'));
  assert.equal(cl.actionOf(theme), t('onboarding.checklist.action_show'));

  // Offen und nur in der Leiste: die Zeile sagt, dass es nur hier steht.
  assert.equal(cl.subOf(setupItem(window, 'devices')), t('onboarding.checklist.sub_here_only'));
});

test('installer-only items have no action and their title is translated', async () => {
  const { window, component: cl, t } = await load();
  const admin = setupItem(window, 'admin');
  assert.equal(cl.actionOf(admin), '');
  assert.equal(cl.titleOf(admin), t(admin.title));
  assert.notEqual(cl.titleOf(admin), admin.title);
  assert.equal(cl.subOf(admin), t(admin.sub));
});

test('the counter counts done and installer rows out of all items', async () => {
  const { store, component: cl, t } = await load();
  store.status = { users: 'done', guest: 'installer', title: 'done', theme: 'installer', config: 'later' };
  const { total, done } = cl.counts;
  assert.equal(done, 4);
  assert.equal(t('onboarding.checklist.count', { done, total }), `4 von ${total}`);
  // 15 Einrichtungsschritte plus 3 nur im Installer, Dienst tailscale ist vorausgewaehlt.
  assert.equal(total, 18);
});

test('the setup summary says how many items are still open', async () => {
  const { store, component: cl, t } = await load();
  store.setupDone = true;
  store.status = {};
  assert.equal(cl.summaryNote, t('onboarding.checklist.note_open_many', { n: cl.openCount }));
  store.status = Object.fromEntries(store.setupItems.map((item) => [item.id, 'done']));
  assert.equal(cl.summaryNote, t('onboarding.checklist.note_done'));
});

test('the setup mode is offered to admins only', async () => {
  const admin = await load({ role: 'admin' });
  admin.store.checklistMode = 'setup';
  assert.equal(admin.component.mode, 'setup');

  const guest = await load({ role: 'guest' });
  guest.store.checklistMode = 'setup';
  assert.equal(guest.component.mode, 'explore');
});

test('a highlight that only works on the other device has no button and names the device', async () => {
  const phoneOnly = [{
    version: '0.12.0', current: true,
    items: [{
      id: 'hp', title: 'onboarding.explore.h184.title', text: 'onboarding.explore.h184.text', detail: 'onboarding.explore.h184.detail',
      steps: [{ panel: 'settings-panel', page: 'users', device: 'phone', target: '#x' }],
    }],
  }];
  const desktop = await load({ width: 1024, explore: phoneOnly });
  const item = phoneOnly[0].items[0];
  assert.equal(desktop.component.newsState(item), 'other-device');
  assert.equal(desktop.component.newsAction(item), '');
  assert.equal(desktop.component.newsSub(item), desktop.t('onboarding.checklist.only_phone'));
  assert.equal(desktop.component.newsSub(item), 'Nur am Handy');
  desktop.component.newsAct(item);
  assert.equal(desktop.component.isReading(item), false);

  const desktopOnly = [{
    version: '0.12.0', current: true,
    items: [{ id: 'dk', title: 'onboarding.explore.h184.title', text: 'onboarding.explore.h184.text', detail: 'onboarding.explore.h184.detail', steps: [{ panel: 'x', device: 'desktop', target: '#x' }] }],
  }];
  const phone = await load({ width: 500, explore: desktopOnly });
  const other = desktopOnly[0].items[0];
  assert.equal(phone.component.newsAction(other), '');
  assert.equal(phone.component.newsSub(other), 'Nur am Desktop');
});

test('a highlight that fits this device keeps its button', async () => {
  const fits = [{
    version: '0.12.0', current: true,
    items: [{ id: 'ok', title: 'onboarding.explore.h184.title', text: 'onboarding.explore.h184.text', detail: 'onboarding.explore.h184.detail', steps: [{ panel: 'x', target: '#x' }] }],
  }];
  const { component: cl, t } = await load({ explore: fits });
  const item = fits[0].items[0];
  assert.equal(cl.newsState(item), 'new');
  assert.equal(cl.newsAction(item), t('onboarding.checklist.action_show'));
});

test('a highlight without a tour is read in place and marked seen', async () => {
  const { window, store, component: cl, calls } = await load();
  const item = window.obSteps.explore[0].items.find((i) => i.id === 'h181');
  assert.ok(item, 'h181 is in the example catalog');
  assert.equal(cl.newsState(item), 'text');
  cl.newsAct(item);
  assert.equal(cl.isReading(item), true);
  assert.equal(store.seen.h181, true);
  await new Promise((resolve) => setImmediate(resolve));
  const put = calls.find((c) => c.method === 'PUT' && c.url === '/api/v1/onboarding/me');
  assert.deepEqual(put?.body, { seen_add: ['h181'] });
});

test('the deep link picks the explore mode and scrolls to and highlights exactly that item', async () => {
  const { window, store, component: cl } = await load({ url: 'http://localhost/?panel=settings&page=checklist&mode=explore&item=h181' });
  const root = window.document.getElementById('root');
  root.innerHTML = '<li data-item="h181"></li><li data-item="h184"></li>';
  const target = root.querySelector('[data-item="h181"]');
  const other = root.querySelector('[data-item="h184"]');
  const scrolled = [];
  const animated = [];
  for (const el of [target, other]) {
    el.scrollIntoView = (options) => scrolled.push({ el, options });
    el.animate = (keyframes, options) => { animated.push({ el, keyframes, options }); return {}; };
  }
  cl.init();
  assert.equal(store.checklistMode, 'explore');
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(scrolled.length, 1, 'scrollIntoView is called once');
  assert.equal(scrolled[0].el, target, 'scrollIntoView targets h181');
  // Die Optionen kommen aus dem jsdom-Fenster, daher in dieses Realm kopieren.
  assert.deepEqual({ ...scrolled[0].options }, { block: 'center', behavior: 'smooth' });
  assert.equal(animated.length, 1, 'animate is called once');
  assert.equal(animated[0].el, target, 'animate highlights h181');
  assert.equal(animated[0].options.duration, 1600);
});

test('the deep link picks the setup mode for an admin', async () => {
  const { store, component: cl } = await load({ url: 'http://localhost/?panel=settings&page=checklist&mode=setup' });
  store.checklistMode = 'explore';
  cl.init();
  assert.equal(store.checklistMode, 'setup');
  assert.equal(cl.mode, 'setup');
});

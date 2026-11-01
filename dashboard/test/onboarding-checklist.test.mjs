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
import { catalog, installI18n } from './helpers/i18n.mjs';

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

// Highlights aus /api/v1/onboarding (explore) wie der Server sie liefert:
// h181 ohne Tour, h184 mit einem Schritt ohne Rechte.
const SAMPLE_EXPLORE = {
  current_version: '0.12.0',
  baseline_seen: false,
  items: [
    { id: 'h184', version: '0.12.0', pr: 184, text: 'Guest switch', details: null, tour: { steps: [{ panel: 'settings-panel', page: 'users', target: 'guest', device: '', need: '', title: { en: 'Guest' }, text: { en: 'Here' } }] } },
    { id: 'h181', version: '0.12.0', pr: 181, text: 'Text only', details: null, tour: null },
  ],
};

// Loads the page scripts into a jsdom window and returns the store plus a
// checklist component bound to it. role decides admin or guest, width the device.
// explore ist der Server-Block explore (Standard: SAMPLE_EXPLORE).
async function load({ url = 'http://localhost/', role = 'admin', width = 1024, explore = SAMPLE_EXPLORE, setupDone = false, older } = {}) {
  const dom = new JSDOM('<!doctype html><html><body><ul id="root"></ul></body></html>', { runScripts: 'outside-only', url, pretendToBeVisual: true });
  const { window } = dom;
  // Die Wahl der Filter-Schaltflaeche steht in der Sitzung, sie gilt schon beim Start.
  if (older !== undefined) window.sessionStorage.setItem('ob-explore-older', older);
  Object.defineProperty(window, 'innerWidth', { value: width, configurable: true });
  // Die Schritte des Servers (setup.items), wie steps.go sie in dieser Reihenfolge liefert.
  const progress = { role, setup: { done: setupDone, steps: {}, items: SERVER_ITEMS }, user: { seen: [], welcomed: false, run: null, queue: [] }, explore };
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
  const phoneOnly = {
    current_version: '0.12.0',
    items: [{ id: 'hp', version: '0.12.0', text: 'Phone', tour: { steps: [{ panel: 'settings-panel', page: 'users', device: 'phone', target: 'x', title: {}, text: {} }] } }],
  };
  const desktop = await load({ width: 1024, explore: phoneOnly });
  const [item] = desktop.store.exploreVersions[0].items;
  assert.equal(desktop.component.newsState(item), 'other-device');
  assert.equal(desktop.component.newsAction(item), '');
  assert.equal(desktop.component.newsSub(item), desktop.t('onboarding.checklist.only_phone'));
  assert.equal(desktop.component.newsSub(item), 'Nur am Handy');
  desktop.component.newsAct(item);
  assert.equal(desktop.component.isReading(item), false);

  const desktopOnly = {
    current_version: '0.12.0',
    items: [{ id: 'dk', version: '0.12.0', text: 'Desktop', tour: { steps: [{ panel: 'x', device: 'desktop', target: 'x', title: {}, text: {} }] } }],
  };
  const phone = await load({ width: 500, explore: desktopOnly });
  const [other] = phone.store.exploreVersions[0].items;
  assert.equal(phone.component.newsAction(other), '');
  assert.equal(phone.component.newsSub(other), 'Nur am Desktop');
});

test('a highlight that fits this device keeps its button', async () => {
  const fits = {
    current_version: '0.12.0',
    items: [{ id: 'ok', version: '0.12.0', text: 'Fits', tour: { steps: [{ panel: 'x', target: 'x', device: '', title: {}, text: {} }] } }],
  };
  const { store, component: cl, t } = await load({ explore: fits });
  const [item] = store.exploreVersions[0].items;
  assert.equal(cl.newsState(item), 'new');
  assert.equal(cl.newsAction(item), t('onboarding.checklist.action_show'));
});

test('a highlight without a tour is read in place and marked seen', async () => {
  const { store, component: cl, calls } = await load();
  const item = store.exploreVersions[0].items.find((i) => i.id === 'h181');
  assert.ok(item, 'h181 is in the server highlights');
  assert.equal(cl.newsState(item), 'text');
  cl.newsAct(item);
  assert.equal(cl.isReading(item), true);
  assert.equal(store.seen.h181, true);
  await new Promise((resolve) => setImmediate(resolve));
  const put = calls.find((c) => c.method === 'PUT' && c.url === '/api/v1/onboarding/me');
  assert.deepEqual(put?.body, { seen_add: ['h181'] });
});

test('the deep link picks the explore mode and scrolls to and marks exactly that item', async () => {
  const { window, store, component: cl } = await load({ url: 'http://localhost/?panel=settings&page=checklist&mode=explore&item=h181' });
  const root = window.document.getElementById('root');
  root.innerHTML = '<li data-item="h181"></li><li data-item="h184"></li>';
  const target = root.querySelector('[data-item="h181"]');
  const other = root.querySelector('[data-item="h184"]');
  const scrolled = [];
  for (const el of [target, other]) {
    el.scrollIntoView = (options) => scrolled.push({ el, options });
  }
  cl.init();
  assert.equal(store.checklistMode, 'explore');
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(scrolled.length, 1, 'scrollIntoView is called once');
  assert.equal(scrolled[0].el, target, 'scrollIntoView targets h181');
  // Die Optionen kommen aus dem jsdom-Fenster, daher in dieses Realm kopieren.
  assert.deepEqual({ ...scrolled[0].options }, { block: 'center', behavior: 'smooth' });
  assert.equal(target.classList.contains('is-target'), true, 'h181 is marked');
  assert.equal(other.classList.contains('is-target'), false, 'h184 is not marked');
});

test('each version card links to its release notes on the versions page', async () => {
  const { component: cl } = await load();
  const version = { version: 'v0.12.0', current: true, items: [] };
  assert.equal(cl.releaseHref(version), '/?panel=settings&page=versions&release=v0.12.0');
});

test('the checklist template shows the shared highlight details and the release notes link', () => {
  const template = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'checklist.html'), 'utf8');
  assert.match(template, /\{\{template "highlightDetails"\}\}/);
  assert.match(template, /x-bind:href="releaseHref\(version\)"/);
  assert.match(template, /\{\{t "onboarding\.checklist\.release_notes"\}\}/);
});

test('the deep link picks the setup mode for an admin', async () => {
  const { store, component: cl } = await load({ url: 'http://localhost/?panel=settings&page=checklist&mode=setup' });
  store.checklistMode = 'explore';
  cl.init();
  assert.equal(store.checklistMode, 'setup');
  assert.equal(cl.mode, 'setup');
});

const TEXT_ONLY_EXPLORE = {
  current_version: '0.12.0',
  baseline_seen: false,
  items: [{ id: 'h181', version: '0.12.0', pr: 181, text: 'Text only', details: null, tour: null }],
};

test('the explore card starts the tour while highlights are unseen', async () => {
  const { store, component: cl, t } = await load();
  assert.equal(cl.exploreUnseen, 1);
  assert.equal(cl.exploreHasTour, true);
  assert.equal(cl.exploreNote, t('onboarding.checklist.explore_note_unseen', { n: 1 }));
  cl.exploreStart();
  assert.equal(store.run.mode, 'explore');
  assert.equal(store.run.all, false);
  assert.equal(store.steps.length, 1);
});

test('the explore card repeats every tour once all is seen', async () => {
  const { store, component: cl, t } = await load();
  store.seen = { h184: true };
  assert.equal(cl.exploreUnseen, 0);
  assert.equal(cl.exploreNote, t('onboarding.checklist.explore_note_all_seen'));
  cl.exploreStart();
  assert.equal(store.run.all, true);
  assert.equal(store.steps.length, 1);
  assert.equal(store.steps[0].id, 'h184');
});

test('the explore card is disabled without any tour for this device', async () => {
  const { component: cl, t } = await load({ explore: TEXT_ONLY_EXPLORE });
  assert.equal(cl.exploreHasTour, false);
  assert.equal(cl.exploreUnseen, 0);
  assert.equal(cl.exploreNote, t('onboarding.checklist.explore_note_none'));
});

test('the setup repeat with all runs every allowed step', async () => {
  const { window, store, component: cl } = await load();
  store.status = Object.fromEntries(store.setupItems.map((item) => [item.id, 'done']));
  cl.run('setup', cl.firstId, { all: true });
  const allowed = store.setupItems.filter((item) => window.obModel.stepAllowed(item, store.ctx())).length;
  assert.ok(allowed > 1);
  assert.equal(store.steps.length, allowed);
});

test('a mode in the address wins over the default of a finished setup', async () => {
  const { store } = await load({ url: 'http://localhost/?panel=settings&page=checklist&mode=setup', setupDone: true });
  assert.equal(store.checklistMode, 'setup');
});

test('a finished setup opens the checklist on explore', async () => {
  const { store } = await load({ setupDone: true });
  assert.equal(store.checklistMode, 'explore');
});

// Eine Version mit einer Tour, die gesehen oder ungesehen ist, dazu ein Text.
const STALE_EXPLORE = {
  current_version: '0.12.0',
  items: [{ id: 'st', version: '0.12.0', minor: '0.12', text: 'Stale', tour: { stale: true, steps: [{ panel: 'settings-panel', target: 'gone', device: '', need: '', title: {}, text: {} }] } }],
};

test('a stale tour shows a disabled tour button with the hint, and the text stays readable', async () => {
  const { store, component: cl, calls, t } = await load({ explore: STALE_EXPLORE });
  const [item] = store.exploreVersions[0].items;
  assert.equal(cl.newsState(item), 'stale');
  assert.equal(cl.isStale(item), true);
  assert.equal(cl.newsSub(item), t('onboarding.checklist.news_stale'));
  assert.equal(cl.newsAction(item), t('onboarding.checklist.action_show'));
  cl.newsAct(item);
  assert.equal(store.run, null, 'a stale tour is not started');
  cl.toggleReading(item);
  assert.equal(cl.isReading(item), true);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(calls.some((c) => c.method === 'PUT'), false, 'reading a stale tour does not mark it seen');
});

test('a stale tour is no tour for the explore card', async () => {
  const { component: cl, t } = await load({ explore: STALE_EXPLORE });
  assert.equal(cl.exploreHasTour, false);
  assert.equal(cl.exploreUnseen, 0);
  assert.equal(cl.exploreNote, t('onboarding.checklist.explore_note_none'));
});

const TWO_MINORS = {
  current_version: '0.12.2',
  items: [
    { id: 'n1', version: '0.12.2', minor: '0.12', text: 'Now', tour: null },
    { id: 'o1', version: '0.11.0', minor: '0.11', text: 'Seen older', tour: { steps: [{ panel: 'settings-panel', target: 'guest', device: '', need: '', title: {}, text: {} }] } },
    { id: 'o2', version: '0.10.0', minor: '0.10', text: 'Unseen older', tour: { steps: [{ panel: 'settings-panel', target: 'guest', device: '', need: '', title: {}, text: {} }] } },
  ],
};

test('older versions are hidden unless they still have an unseen tour, the toggle shows the rest', async () => {
  const { store, component: cl, t, window } = await load({ explore: TWO_MINORS });
  store.seen = { o1: true };
  assert.deepEqual([...cl.shownVersions.map((v) => v.version)], ['0.12', '0.10']);
  assert.equal(cl.olderHidden, 1);
  assert.equal(cl.olderToggleShown, true);
  assert.equal(cl.olderLabel, t('onboarding.checklist.older_show', { n: 1 }));
  cl.toggleOlder();
  assert.equal(cl.olderOpen, true);
  assert.deepEqual([...cl.shownVersions.map((v) => v.version)], ['0.12', '0.11', '0.10']);
  assert.equal(cl.olderLabel, t('onboarding.checklist.older_hide'));
  assert.equal(window.sessionStorage.getItem('ob-explore-older'), '1');
  cl.toggleOlder();
  assert.equal(window.sessionStorage.getItem('ob-explore-older'), '0');
});

test('the choice of the older filter is read from the session at the start', async () => {
  const { component: cl } = await load({ explore: TWO_MINORS, older: '1' });
  assert.equal(cl.olderOpen, true);
  assert.equal(cl.shownVersions.length, 3);
});

test('the older filter is hidden without older cards', async () => {
  const onlyCurrent = { current_version: '0.12.0', items: [{ id: 'n1', version: '0.12.0', minor: '0.12', text: 'Now', tour: null }] };
  const { component: cl } = await load({ explore: onlyCurrent });
  assert.equal(cl.olderToggleShown, false);
  assert.deepEqual([...cl.shownVersions.map((v) => v.version)], ['0.12']);
});

test('a card is labelled by its minor and links to its newest release', async () => {
  const { component: cl, t } = await load();
  assert.equal(cl.versionLabel({ version: '0.12' }), t('onboarding.checklist.version', { version: '0.12' }));
  assert.equal(cl.versionLabel({ version: '0.12.0' }), t('onboarding.checklist.version', { version: '0.12' }));
  assert.equal(cl.versionLabel({ version: '1.0' }), t('onboarding.checklist.version', { version: '1.0' }));
  assert.equal(cl.releaseHref({ version: '0.12', release: '0.12.2' }), '/?panel=settings&page=versions&release=0.12.2');
});

test('the explore part filters older cards and greys out stale tour buttons in the template', () => {
  const template = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'checklist.html'), 'utf8');
  assert.match(template, /x-for="version in shownVersions"/);
  assert.match(template, /x-on:click="toggleOlder\(\)"/);
  assert.match(template, /x-bind:aria-expanded="olderOpen \? 'true' : 'false'"/);
  assert.match(template, /x-show="olderToggleShown"/);
  assert.match(template, /x-bind:disabled="isStale\(item\)"/);
  assert.match(template, /'is-disabled': isStale\(item\)/);
});

test('the filter and the stale hint have texts in both languages', () => {
  for (const key of ['onboarding.checklist.older_show', 'onboarding.checklist.older_hide', 'onboarding.checklist.news_stale']) {
    assert.ok(catalog('de')[key], `de ${key}`);
    assert.ok(catalog('en')[key], `en ${key}`);
  }
  assert.match(catalog('de')['onboarding.checklist.older_show'], /\{n\}/);
  assert.match(catalog('en')['onboarding.checklist.older_show'], /\{n\}/);
  assert.equal(catalog('de')['onboarding.checklist.news_stale'], 'Diese Tour passt nicht mehr zur aktuellen Oberfläche.');
  assert.equal(catalog('en')['onboarding.checklist.news_stale'], 'This tour no longer matches the current interface.');
});

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
async function load({ url = 'http://localhost/', role = 'admin', width = 1024, explore = SAMPLE_EXPLORE, setupDone = false, older, archive } = {}) {
  const dom = new JSDOM('<!doctype html><html><body><ul id="root"></ul></body></html>', { runScripts: 'outside-only', url, pretendToBeVisual: true });
  const { window } = dom;
  // Die Wahl der Filter-Schaltflaeche steht in der Sitzung, sie gilt schon beim Start.
  if (older !== undefined) window.sessionStorage.setItem('ob-explore-older', older);
  if (archive !== undefined) window.sessionStorage.setItem('ob-explore-archive', archive);
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

test('a highlight with only one sentence is marked as read, the row has no chevron and no expand', async () => {
  const { store, component: cl, calls, t } = await load();
  const item = store.exploreVersions[0].items.find((i) => i.id === 'h181');
  assert.ok(item, 'h181 is in the server highlights');
  assert.equal(cl.newsState(item), 'text');
  assert.equal(cl.newsAction(item), t('onboarding.checklist.action_mark_read'));
  assert.equal(cl.newsSub(item), 'Text only', 'the one sentence is the row subtitle');
  assert.equal(cl.isMarkOnly(item), true);
  cl.newsAct(item);
  assert.equal(cl.isReading(item), false, 'clicking does not expand the sentence');
  assert.equal(store.seen.h181, true);
  assert.equal(store.run, null, 'nothing is started');
  await new Promise((resolve) => setImmediate(resolve));
  const put = calls.find((c) => c.method === 'PUT' && c.url === '/api/v1/onboarding/me');
  assert.deepEqual(put?.body, { seen_add: ['h181'] });
});

test('a seen highlight with only one sentence has no action button and no expand', async () => {
  const { store, component: cl, t } = await load();
  const item = store.exploreVersions[0].items.find((i) => i.id === 'h181');
  store.seen = { h181: true };
  assert.equal(cl.newsState(item), 'seen');
  assert.equal(cl.newsAction(item), '', 'no action button');
  assert.equal(cl.isMarkOnly(item), true);
  assert.equal(cl.newsSub(item), 'Text only');
  assert.notEqual(cl.newsSub(item), t('onboarding.checklist.news_seen'));
});

test('a seen highlight without a tour but with details offers Lesen and only toggles the reading', async () => {
  const explore = {
    current_version: '0.12.0',
    items: [{ id: 'dt', version: '0.12.0', text: 'With details', details: { doc: '' }, tour: null }],
  };
  const { store, component: cl, calls, t } = await load({ explore });
  const item = store.exploreVersions[0].items[0];
  store.seen = { dt: true };
  assert.equal(cl.newsState(item), 'seen');
  assert.equal(cl.newsAction(item), t('onboarding.checklist.action_read'), 'Lesen, not Nochmal');
  assert.equal(cl.isMarkOnly(item), false);
  assert.equal(cl.newsSub(item), t(item.tour === false ? 'onboarding.checklist.news_no_tour' : 'onboarding.checklist.news_no_tour_rights'), 'the subtitle stays as before');
  cl.newsAct(item);
  assert.equal(cl.isReading(item), true, 'the details open');
  assert.equal(store.run, null, 'no tour is started');
  cl.newsAct(item);
  assert.equal(cl.isReading(item), false, 'the details close again');
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(calls.some((c) => c.method === 'PUT'), false, 'a seen highlight is not saved again');
});

test('the checklist row hides the chevron and the expand state for a highlight with only one sentence', () => {
  const template = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'checklist.html'), 'utf8');
  assert.match(template, /x-bind:aria-expanded="isStale\(item\) \|\| isMarkOnly\(item\) \? null : \(isReading\(item\) \? 'true' : 'false'\)"/);
  assert.match(template, /<svg viewBox="0 0 24 24" aria-hidden="true" x-show="!isStale\(item\) && !isMarkOnly\(item\)"/);
  assert.match(template, /<template x-if="item\.details">\s*<div class="ob-cl-detail" x-show="isReading\(item\)" x-collapse>/);
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
  assert.match(template, /\{\{template "highlightDetailsBody"\}\}/);
  assert.match(template, /x-bind:href="releaseHref\(version\)"/);
  assert.match(template, /\{\{t "onboarding\.checklist\.release_notes"\}\}/);
});

test('the checklist reads a highlight directly: body only, no details wrapper, doc link shown', () => {
  const template = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'checklist.html'), 'utf8');
  const start = template.indexOf('class="ob-cl-detail"');
  const end = template.indexOf('</li>', start);
  assert.ok(start > 0 && end > start, 'the ob-cl-detail block exists');
  const detail = template.slice(start, end);
  assert.match(detail, /<div class="whatsnew-body" x-data="\{ entry: item, showDoc: true, cropped: true, loaded: false \}" x-effect="if \(isReading\(item\)\) loaded = true">\{\{template "highlightDetailsBody"\}\}<\/div>/);
  assert.doesNotMatch(detail, /<summary/);
  assert.doesNotMatch(detail, /<details/);
  assert.doesNotMatch(detail, /\{\{template "highlightDetails"\}\}/);
  const partial = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'highlight-details.html'), 'utf8');
  assert.match(partial, /<a class="whatsnew-link" x-bind:href="entry\.details\.doc" target="_blank" rel="noopener">\{\{t "settings\.whatsnew\.doc_link"\}\}<\/a>/);
});

test('the checklist detail has no placeholder doc link, the real doc link comes from the shared body', () => {
  const template = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'checklist.html'), 'utf8');
  const start = template.indexOf('class="ob-cl-detail"');
  const end = template.indexOf('</li>', start);
  const detail = template.slice(start, end);
  assert.doesNotMatch(detail, /x-on:click\.prevent/);
  assert.doesNotMatch(detail, /href="#"/);
  assert.equal(template.includes('onboarding.checklist.docs'), false);
});

test('the shared body shows the doc link only when showDoc is set, the versions page keeps the details wrapper', () => {
  const partial = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'highlight-details.html'), 'utf8');
  const body = partial.slice(partial.indexOf('{{define "highlightDetailsBody"}}'), partial.indexOf('{{define "highlightDetails"}}'));
  assert.match(partial, /\{\{define "highlightDetailsBody"\}\}/);
  assert.match(body, /<template x-if="entry\.details\.doc && showDoc">/);
  assert.match(body, /<p class="whatsnew-text" x-text="window\.highlightDetails\.detailText\(entry\)"><\/p>/);
  assert.match(body, /<span class="whatsnew-shot"/);
  assert.match(partial, /x-data="\{ loaded: false, cropped: true, showDoc: true \}"/);
  assert.match(partial, /<summary class="whatsnew-summary">/);
  assert.match(partial, /<div class="whatsnew-body">\{\{template "highlightDetailsBody"\}\}<\/div>/);
});

test('the German doc link hint says the guide is English only', () => {
  assert.equal(catalog('de')['settings.whatsnew.doc_link'], 'In der Anleitung ansehen (en)');
  assert.equal(catalog('en')['settings.whatsnew.doc_link'], 'Open the guide');
});

test('the explore part has no all-versions row, each card keeps its release notes chip', () => {
  const template = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'checklist.html'), 'utf8');
  assert.doesNotMatch(template, /all=1/);
  assert.doesNotMatch(template, /onboarding\.checklist\.all_versions/);
  assert.match(template, /<a class="ob-crosslink ob-crosslink--chip" x-bind:href="releaseHref\(version\)">/);
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

// Eine veraltete Tour mit Ersatz: h20 zeigt die neue Stelle, der Schritt 1 (zweiter) ist der Start.
const REPLACED_EXPLORE = {
  current_version: '0.12.0',
  items: [
    { id: 'old', version: '0.12.0', minor: '0.12', text: 'Old', tour: { stale: true, replacement: { id: 'h20', index: 1 }, steps: [{ panel: 'settings-panel', target: 'gone', device: '', need: '', title: {}, text: {} }] } },
    { id: 'h20', version: '0.12.0', minor: '0.12', text: 'New place', tour: { steps: [
      { panel: 'settings-panel', page: 'users', target: 'guest', device: '', need: '', title: { en: 'First' }, text: { en: 'One' } },
      { panel: 'settings-panel', page: 'users', target: 'tailscale', device: '', need: '', title: { en: 'Second' }, text: { en: 'Two' } },
    ] } },
  ],
};

test('a stale highlight with a replacement offers to show the new place', async () => {
  const { store, component: cl, t } = await load({ explore: REPLACED_EXPLORE });
  const item = store.exploreVersions[0].items.find((i) => i.id === 'old');
  assert.equal(cl.newsState(item), 'stale');
  assert.equal(cl.isStale(item), true);
  assert.equal(cl.newsAction(item), 'Neue Stelle zeigen');
  assert.equal(cl.newsAction(item), t('onboarding.checklist.action_replacement'));
  assert.equal(cl.newsSub(item), t('onboarding.checklist.news_replaced'));
  assert.equal(cl.isReading(item), false);
  assert.ok(cl.replacementStart(item), 'the replacement is walkable on this device');
});

test('the replacement starts the replacing highlight at its step', async () => {
  const { store, component: cl } = await load({ explore: REPLACED_EXPLORE });
  const item = store.exploreVersions[0].items.find((i) => i.id === 'old');
  const started = [];
  store.start = (...args) => started.push(args);
  cl.newsAct(item);
  // Das Optionsobjekt kommt aus dem VM-Realm, daher kopieren wie in den anderen Tests.
  assert.equal(started.length, 1);
  assert.deepEqual([started[0][0], started[0][1], { ...started[0][2] }], ['explore', 'h20', { index: 1 }]);
  assert.equal(cl.isReading(item), false, 'a replacement start does not open the text');
});

test('a replacement step for another device leaves the stale hint', async () => {
  const desktopOnly = {
    current_version: '0.12.0',
    items: [
      { id: 'old', version: '0.12.0', minor: '0.12', text: 'Old', tour: { stale: true, replacement: { id: 'hd', index: 0 }, steps: [{ panel: 'settings-panel', target: 'gone', device: '', need: '', title: {}, text: {} }] } },
      { id: 'hd', version: '0.12.0', minor: '0.12', text: 'Desktop', tour: { steps: [{ panel: 'settings-panel', page: 'users', target: 'guest', device: 'desktop', need: '', title: {}, text: {} }] } },
    ],
  };
  const { store, component: cl, t, calls } = await load({ width: 500, explore: desktopOnly });
  const item = store.exploreVersions[0].items.find((i) => i.id === 'old');
  assert.equal(cl.replacementStart(item), null);
  assert.equal(cl.isStale(item) && !cl.replacementStart(item), true, 'the button stays grey');
  assert.equal(cl.newsAction(item), t('onboarding.checklist.action_show'));
  assert.equal(cl.newsSub(item), t('onboarding.checklist.news_stale'));
  cl.newsAct(item);
  assert.equal(store.run, null, 'nothing is started');
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(calls.some((c) => c.method === 'PUT'), false);
});

const TWO_MINORS = {
  current_version: '0.12.2',
  items: [
    { id: 'n1', version: '0.12.2', minor: '0.12', text: 'Now', tour: null },
    { id: 'o1', version: '0.11.0', minor: '0.11', text: 'Seen older', tour: { steps: [{ panel: 'settings-panel', target: 'guest', device: '', need: '', title: {}, text: {} }] } },
    { id: 'o2', version: '0.10.0', minor: '0.10', text: 'Unseen older', tour: { steps: [{ panel: 'settings-panel', target: 'guest', device: '', need: '', title: {}, text: {} }] } },
  ],
};

test('the two newest minors stay visible, older minors wait behind the toggle even with an unseen tour', async () => {
  const { store, component: cl, t, window } = await load({ explore: TWO_MINORS });
  store.seen = {};
  assert.deepEqual([...cl.shownVersions.map((v) => v.version)], ['0.12.2', '0.11.0']);
  assert.equal(cl.olderHidden, 1);
  assert.equal(cl.olderToggleShown, true);
  assert.equal(cl.olderLabel, t('onboarding.checklist.older_show', { n: 1 }));
  cl.toggleOlder();
  assert.equal(cl.olderOpen, true);
  assert.deepEqual([...cl.shownVersions.map((v) => v.version)], ['0.12.2', '0.11.0', '0.10.0']);
  assert.equal(cl.olderLabel, t('onboarding.checklist.older_hide'));
  assert.equal(window.sessionStorage.getItem('ob-explore-older'), '1');
  cl.toggleOlder();
  assert.equal(window.sessionStorage.getItem('ob-explore-older'), '0');
});

test('every release of the two newest minors stays visible, only older minors are hidden', async () => {
  const sameMinor = {
    current_version: '0.10.33',
    items: [
      { id: 'a', version: '0.10.33', minor: '0.10', text: 'A', tour: null },
      { id: 'b', version: '0.10.8', minor: '0.10', text: 'B', tour: null },
      { id: 'c', version: '0.10.0', minor: '0.10', text: 'C', tour: null },
      { id: 'd', version: '0.9.4', minor: '0.9', text: 'D', tour: null },
      { id: 'e', version: '0.8.1', minor: '0.8', text: 'E', tour: null },
    ],
  };
  const { component: cl } = await load({ explore: sameMinor });
  assert.deepEqual([...cl.shownVersions.map((v) => v.version)], ['0.10.33', '0.10.8', '0.10.0', '0.9.4']);
  assert.equal(cl.olderHidden, 1);
  assert.equal(cl.cardShown(cl.versions[4]), false);
  cl.toggleOlder();
  assert.equal(cl.cardShown(cl.versions[4]), true);
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
  assert.deepEqual([...cl.shownVersions.map((v) => v.version)], ['0.12.0']);
});

test('a card is labelled by its release, the v prefix goes, a .0 stays, and it links to that release', async () => {
  const { component: cl, t } = await load();
  assert.equal(cl.versionLabel({ version: 'v0.10.33' }), t('onboarding.checklist.version', { version: '0.10.33' }));
  assert.equal(cl.versionLabel({ version: '0.12.0' }), t('onboarding.checklist.version', { version: '0.12.0' }));
  assert.equal(cl.versionLabel({ version: '0.12.0' }), 'Version 0.12.0');
  assert.equal(cl.releaseHref({ version: 'v0.10.33', release: 'v0.10.33' }), '/?panel=settings&page=versions&release=v0.10.33');
});

test('a component section gets a subheadline with its label and version, the dashboard section none', async () => {
  const { component: cl } = await load();
  assert.equal(cl.sectionHead({ id: 'bootstrap', label: 'Bootstrap', version: 'v0.2.6' }), 'Bootstrap 0.2.6');
  assert.equal(cl.sectionHead({ id: 'service:automation', label: 'Automation', version: '' }), 'Automation');
});

test('the explore part keeps every card in the DOM and shows it with x-show, stale tour buttons stay greyed out', () => {
  const template = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'checklist.html'), 'utf8');
  assert.match(template, /x-for="version in versions"/);
  assert.match(template, /x-show="cardShown\(version\)"/);
  assert.match(template, /x-on:click="toggleOlder\(\)"/);
  assert.match(template, /x-bind:aria-expanded="olderOpen \? 'true' : 'false'"/);
  assert.match(template, /x-show="olderToggleShown"/);
  assert.match(template, /x-bind:disabled="isStale\(item\) && !replacementStart\(item\)"/);
  assert.match(template, /'is-disabled': isStale\(item\) && !replacementStart\(item\)/);
});

test('the card has one list per component section and the subheadline is shown for every section but the dashboard', () => {
  const template = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'checklist.html'), 'utf8');
  assert.match(template, /<template x-for="section in version\.sections" x-bind:key="section\.id">/);
  assert.match(template, /<p class="ob-cl-subhead" x-show="section\.id !== 'dashboard'" x-text="sectionHead\(section\)"><\/p>/);
  assert.doesNotMatch(template, /x-for="item in version\.items"/);
  assert.doesNotMatch(template, /Version 0\.2/);
});

test('the older cards enter and leave with the ob-cl-card classes, staggered and capped at five', async () => {
  const template = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'checklist.html'), 'utf8');
  assert.match(template, /x-transition:enter="ob-cl-card-enter" x-transition:enter-start="ob-cl-card-enter-start" x-transition:enter-end="ob-cl-card-enter-end"/);
  assert.match(template, /x-transition:leave="ob-cl-card-leave" x-transition:leave-start="ob-cl-card-leave-start" x-transition:leave-end="ob-cl-card-leave-end"/);
  assert.match(template, /x-bind:style="\{ '--ob-delay': enterDelay\(version\) \}"/);
  // Gestaffelt nach der Reihe der ausgeblendeten Karten, hoechstens fuenf mal 40 ms.
  const many = {
    current_version: '0.12.0',
    items: [{ id: 'n', version: '0.12.0', minor: '0.12', text: 'N', tour: null }, ...[1, 2, 3, 4, 5, 6, 7, 8].map((i) => ({ id: `o${i}`, version: `0.${10 - i}.0`, minor: `0.${10 - i}`, text: 'O', tour: null }))],
  };
  const { component: many7 } = await load({ explore: many });
  const hidden = many7.versions.filter((v) => !many7.shownByDefault(v));
  assert.equal(hidden.length, 7);
  assert.equal(many7.enterDelay(many7.versions[0]), '0ms', 'a card shown anyway has no delay');
  assert.equal(many7.enterDelay(hidden[0]), '0ms');
  assert.equal(many7.enterDelay(hidden[1]), '40ms');
  assert.equal(many7.enterDelay(hidden[5]), '200ms');
  assert.equal(many7.enterDelay(hidden[6]), '200ms', 'capped at five cards');
});

test('the explore motion is transform and opacity only, reduced motion keeps the fade', () => {
  const css = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'css', 'onboarding.css'), 'utf8');
  assert.match(css, /\.ob-cl-card-enter\s*\{\s*transition: opacity 200ms cubic-bezier\(0\.23, 1, 0\.32, 1\) var\(--ob-delay, 0ms\), transform 200ms cubic-bezier\(0\.23, 1, 0\.32, 1\) var\(--ob-delay, 0ms\);/);
  assert.match(css, /\.ob-cl-card-enter-start\s*\{\s*opacity: 0;\s*transform: translateY\(8px\);\s*\}/);
  assert.match(css, /\.ob-cl-card-leave\s*\{\s*transition: opacity 150ms ease-out, transform 150ms ease-out;\s*\}/);
  assert.match(css, /\.ob-cl-card-leave-end\s*\{\s*opacity: 0;\s*transform: translateY\(8px\);\s*\}/);
  assert.doesNotMatch(css, /transition:\s*all\b/);
  const reduced = css.slice(css.indexOf('@media (prefers-reduced-motion: reduce) {', css.indexOf('.ob-cl-card-enter')));
  assert.match(reduced, /\.ob-cl-card-enter-start, \.ob-cl-card-leave-end \{ transform: none; \}/);
});

test('the toggle presses to 0.97 and hovers only with a fine pointer, its chevron turns without moving the layout', () => {
  const css = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'css', 'onboarding.css'), 'utf8');
  const template = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'checklist.html'), 'utf8');
  assert.match(css, /\.ob-cl-older:active\s*\{\s*transform: scale\(\.97\);\s*\}/);
  assert.match(css, /@media \(hover: hover\) and \(pointer: fine\)\s*\{[^}]*\.ob-cl-older:hover/);
  assert.match(css, /\.ob-cl-older-chev\.is-open\s*\{\s*transform: rotate\(90deg\);\s*\}/);
  assert.match(template, /<svg class="ob-cl-older-chev" x-bind:class="\{ 'is-open': olderOpen \}" viewBox="0 0 24 24" aria-hidden="true"><use href="#ico-chevron"><\/use><\/svg>/);
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

const WITH_ARCHIVE = {
  current_version: '0.12.2',
  items: [
    { id: 'n1', version: '0.12.2', minor: '0.12', text: 'Now', tour: null },
    { id: 'o1', version: '0.11.0', minor: '0.11', text: 'Older', tour: { steps: [{ panel: 'settings-panel', target: 'guest', device: '', need: '', title: {}, text: {} }] } },
    { id: 'a1', version: '0.9.0', minor: '0.9', archive: true, text: 'Archived', tour: { steps: [{ panel: 'settings-panel', target: 'guest', device: '', need: '', title: {}, text: {} }] } },
  ],
};

test('archived versions stay hidden even with an unseen tour, their own toggle shows them', async () => {
  const { store, component: cl, t, window } = await load({ explore: WITH_ARCHIVE });
  store.seen = {};
  assert.deepEqual([...cl.shownVersions.map((v) => v.version)], ['0.12.2', '0.11.0']);
  assert.equal(cl.olderHidden, 0);
  assert.equal(cl.archiveCount, 1);
  assert.equal(cl.archiveToggleShown, true);
  assert.equal(cl.archiveLabel, t('onboarding.checklist.archive_show', { n: 1 }));
  cl.toggleArchive();
  assert.deepEqual([...cl.shownVersions.map((v) => v.version)], ['0.12.2', '0.11.0', '0.9.0']);
  assert.equal(cl.archiveLabel, t('onboarding.checklist.archive_hide'));
  assert.equal(window.sessionStorage.getItem('ob-explore-archive'), '1');
  cl.toggleArchive();
  assert.equal(window.sessionStorage.getItem('ob-explore-archive'), '0');
});

test('the archive toggle only appears once the older versions are open, and closing them closes the archive too', async () => {
  const tiers = {
    current_version: '0.12.0',
    items: [
      { id: 'n', version: '0.12.0', minor: '0.12', text: 'N', tour: null },
      { id: 'p', version: '0.11.0', minor: '0.11', text: 'P', tour: null },
      { id: 'o', version: '0.10.0', minor: '0.10', text: 'O', tour: null },
      { id: 'a', version: '0.9.0', minor: '0.9', archive: true, text: 'A', tour: null },
    ],
  };
  const { component: cl, window } = await load({ explore: tiers });
  const shown = () => [...cl.shownVersions.map((v) => v.version)];
  assert.equal(cl.olderToggleShown, true);
  assert.equal(cl.archiveToggleShown, false);
  cl.toggleOlder();
  assert.equal(cl.archiveToggleShown, true);
  assert.deepEqual(shown(), ['0.12.0', '0.11.0', '0.10.0']);
  cl.toggleArchive();
  assert.deepEqual(shown(), ['0.12.0', '0.11.0', '0.10.0', '0.9.0']);
  cl.toggleOlder();
  assert.equal(cl.archiveOpen, false);
  assert.equal(window.sessionStorage.getItem('ob-explore-archive'), '0');
  assert.deepEqual(shown(), ['0.12.0', '0.11.0']);
});

test('the archive choice is read from the session at the start', async () => {
  const { component: cl } = await load({ explore: WITH_ARCHIVE, archive: '1' });
  assert.equal(cl.archiveOpen, true);
  assert.equal(cl.cardShown(cl.versions[2]), true);
});

test('an archived card is labelled as an earlier version, the toggle is hidden without archive', async () => {
  const { component: cl, t } = await load({ explore: WITH_ARCHIVE });
  assert.equal(cl.versionEyebrow(cl.versions[2]), t('onboarding.checklist.archive'));
  const { component: plain } = await load({ explore: TWO_MINORS });
  assert.equal(plain.archiveToggleShown, false);
});

test('the checklist template has the archive toggle next to the older toggle', () => {
  const html = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'checklist.html'), 'utf8');
  assert.match(html, /x-show="archiveToggleShown"[^>]*x-on:click="toggleArchive\(\)"/);
});

const STEP = { panel: 'settings-panel', target: 'guest', device: '', need: '', title: { en: 'T' }, text: { en: 'X' } };
const CATCH_UP = {
  current_version: '0.12.2',
  items: [
    { id: 'n1', version: '0.12.2', minor: '0.12', text: 'Now', tour: { steps: [STEP] } },
    { id: 'o1', version: '0.11.0', minor: '0.11', text: 'Text only', tour: null },
    { id: 'o2', version: '0.11.0', minor: '0.11', text: 'First tour', tour: { steps: [STEP] } },
    { id: 'o3', version: '0.11.0', minor: '0.11', text: 'Second tour', tour: { steps: [STEP, STEP] } },
    { id: 'p1', version: '0.10.0', minor: '0.10', text: 'Only text', tour: null },
  ],
};

test('older cards with a tour offer to catch up on it, the current card and text-only cards do not', async () => {
  const { component: cl } = await load({ explore: CATCH_UP });
  const byVersion = Object.fromEntries(cl.versions.map((v) => [v.version, v]));
  assert.equal(cl.catchUpShown(byVersion['0.12.2']), false);
  assert.equal(cl.catchUpShown(byVersion['0.11.0']), true);
  assert.equal(cl.catchUpShown(byVersion['0.10.0']), false);
});

test('catching up runs every tour of that release from its first tour, seen or not', async () => {
  const { store, component: cl } = await load({ explore: CATCH_UP });
  store.seen = { o2: true, o3: true };
  cl.catchUp(cl.versions.find((v) => v.version === '0.11.0'));
  assert.equal(store.run.mode, 'explore');
  assert.equal(store.run.from, 'o2');
  assert.equal(store.run.all, true);
  assert.deepEqual([...store.steps.map((s) => s.id)], ['o2', 'o3', 'o3']);
});

test('the catch-up button turns into a repeat once every tour of that release is seen', async () => {
  const { store, component: cl } = await load({ explore: CATCH_UP });
  const older = cl.versions.find((v) => v.version === '0.11.0');
  store.seen = { o2: true };
  assert.equal(cl.catchUpDone(older), false);
  store.seen = { o2: true, o3: true };
  assert.equal(cl.catchUpDone(older), true);
});

test('the catch-up state ignores items without a tour and stale tours', async () => {
  const explore = {
    current_version: '0.12.2',
    items: [
      { id: 'n1', version: '0.12.2', minor: '0.12', text: 'Now', tour: { steps: [STEP] } },
      { id: 'o1', version: '0.11.0', minor: '0.11', text: 'Text only', tour: null },
      { id: 'o2', version: '0.11.0', minor: '0.11', text: 'Seen tour', tour: { steps: [STEP] } },
      { id: 'o3', version: '0.11.0', minor: '0.11', text: 'Stale tour', tour: { steps: [STEP], stale: true } },
    ],
  };
  const { store, component: cl } = await load({ explore });
  const older = cl.versions.find((v) => v.version === '0.11.0');
  store.seen = { o2: true };
  assert.equal(cl.catchUpDone(older), true);
});

test('the checklist template puts the catch-up button next to the release notes chip', () => {
  const html = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'checklist.html'), 'utf8');
  assert.match(html, /<div class="ob-cl-card-links">\s*<button type="button" class="ob-crosslink ob-crosslink--chip ob-crosslink--tour" x-bind:class="\{ 'is-primary': !catchUpDone\(version\) \}" data-tour="checklist-catch-up" x-show="catchUpShown\(version\)" x-on:click="catchUp\(version\)">/);
  assert.match(html, /catch_up/);
});

test('the toggles for older and earlier versions sit in their own card below the version cards', () => {
  const html = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'checklist.html'), 'utf8');
  const summary = html.slice(html.indexOf('ob-cl-summary" data-tour="checklist-explore"'), html.indexOf('x-for="version in versions"'));
  assert.doesNotMatch(summary, /toggleOlder|toggleArchive/);
  const more = html.slice(html.indexOf('class="setting-card ob-cl-more" data-tour="checklist-more"'));
  assert.ok(html.indexOf('class="setting-card ob-cl-more" data-tour="checklist-more"') > html.indexOf('x-for="version in versions"'));
  assert.match(more, /x-show="olderToggleShown \|\| archiveToggleShown"/);
  assert.match(more, /x-on:click="toggleArchive\(\)"[\s\S]*x-on:click="toggleOlder\(\)"/);
});

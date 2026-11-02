// Tests for the highlight part of the versions page (versions.page.js and
// versions.html): state and buttons per highlight, the checklist link, the
// release deep link and the catalog keys. Run with `npm test` from dashboard/.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { catalog, installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = (...parts) => fs.readFileSync(path.join(here, '..', 'internal', 'webui', ...parts), 'utf8');
const versionsSource = read('static', 'js', 'versions.page.js');
const modelSource = read('static', 'js', 'onboarding-model.js');
const template = read('templates', 'versions.html');
const settingsTemplate = read('templates', 'settings.html');
const de = catalog('de');
const en = catalog('en');

// Explore items as the onboarding store builds them (exploreVersions).
const EXPLORE = [{
  version: '0.12.0',
  current: true,
  items: [
    { id: 'h184', pr: 184, title: 'Guest', text: 'Guest', tour: undefined, steps: [{ panel: 'settings-panel', page: 'users', target: '[data-tour="guest"]', device: '', need: '', title: 'Guest', text: 'Here', literal: true }] },
    { id: 'h181', pr: 181, title: 'Text', text: 'Text', tour: false, steps: [] },
    { id: 'h190', pr: 190, title: 'Phone', text: 'Phone', tour: undefined, steps: [{ panel: 'settings-panel', page: 'users', target: '[data-tour="guest"]', device: 'phone', need: '', title: 'Phone', text: 'Here', literal: true }] },
    { id: 'h191', pr: 191, title: 'Desktop', text: 'Desktop', tour: undefined, steps: [{ panel: 'settings-panel', page: 'users', target: '[data-tour="guest"]', device: 'desktop', need: '', title: 'Desktop', text: 'Here', literal: true }] },
    { id: 'h192', pr: 192, title: 'Stale', text: 'Stale', tour: undefined, stale: true, steps: [{ panel: 'settings-panel', page: 'users', target: '[data-tour="gone"]', device: '', need: '', title: 'Stale', text: 'Here', literal: true }] },
  ],
}];

const SNAPSHOT = {
  bundle: { version: 'v0.12.0', built_at: '2026-10-30T10:00:00+01:00', arch: 'armv6' },
  running: { dashboard: 'v0.12.0' },
  has_changelog: true,
  components: [
    { id: 'dashboard', label: 'Dashboard', kind: 'app', version: 'v0.12.0', installed: true },
  ],
};

const jsonResponse = (body, ok = true) => ({ ok, status: ok ? 200 : 404, json: async () => body });

function createPanel({ url = 'https://dashboard.local/', device = 'desktop', seen = {}, fetchImpl } = {}) {
  const dom = new JSDOM('<!doctype html><html><head></head><body></body></html>', {
    runScripts: 'outside-only',
    url,
    pretendToBeVisual: true,
  });
  Object.defineProperty(dom.window, 'innerWidth', { value: device === 'phone' ? 400 : 1024, configurable: true });
  installI18n(dom.window);
  const context = dom.getInternalVMContext();
  vm.runInContext(modelSource, context);
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  vm.runInContext(versionsSource, context);

  const store = {
    seen: { ...seen },
    queue: ['setup'],
    exploreVersions: EXPLORE,
    started: [],
    ctx() {
      return { role: 'admin', can: new Set(), device: dom.window.innerWidth <= 759 ? 'phone' : 'desktop', installed: [], t: (key, params) => dom.window.I18n.t(key, params) };
    },
    start(mode, from) {
      this.started.push([mode, from, [...this.queue]]);
      return true;
    },
  };
  const component = factories.versionsPanel();
  component.$store = { onboarding: store, toasts: { push() {} } };
  return { component, store, window: dom.window };
}

test('highlightState is the exploreState of the highlight with the same id, empty without one', () => {
  const { component, store } = createPanel();
  assert.equal(component.highlightState({ id: 'h184' }), 'new');
  assert.equal(component.highlightState({ id: 'h181' }), 'text', 'a highlight without a tour is text only');
  assert.equal(component.highlightState({ id: 'h190' }), 'other-device', 'a phone-only step on the desktop');
  assert.equal(component.highlightState({ id: 'h999' }), '', 'no item, no state');
  assert.equal(component.highlightState({ title: 'no id' }), '');
  store.seen = { h184: true };
  assert.equal(component.highlightState({ id: 'h184' }), 'seen', 'the seen map of the store decides between new and seen');
});

test('highlightLabel names the state, the other-device label follows the device', () => {
  const { component } = createPanel();
  assert.equal(component.highlightLabel({ id: 'h184' }), de['onboarding.checklist.news_new']);
  assert.equal(component.highlightLabel({ id: 'h181' }), de['settings.versions.highlight.text']);
  assert.equal(component.highlightLabel({ id: 'h190' }), de['onboarding.checklist.only_phone'], 'on the desktop the highlight is for the phone');
  assert.equal(component.highlightLabel({ id: 'h999' }), '');
  const seen = createPanel({ seen: { h184: true } });
  assert.equal(seen.component.highlightLabel({ id: 'h184' }), de['onboarding.checklist.news_seen'], 'the checklist wording');
  const phone = createPanel({ device: 'phone' });
  assert.equal(phone.component.highlightLabel({ id: 'h191' }), de['onboarding.checklist.only_desktop'], 'on the phone the highlight is for the desktop');
  assert.equal(phone.component.highlightLabel({ id: 'h190' }), de['onboarding.checklist.news_new'], 'a phone highlight is new on the phone');
});

test('highlightButtons: new shows the tour and the checklist, seen repeats, text and other-device only the checklist', () => {
  const { component, store } = createPanel();
  assert.deepEqual({ ...component.highlightButtons({ id: 'h184' }) }, { run: true, runText: de['onboarding.checklist.action_show'], checklist: true });
  store.seen = { h184: true };
  assert.deepEqual({ ...component.highlightButtons({ id: 'h184' }) }, { run: true, runText: de['onboarding.checklist.action_again'], checklist: true });
  assert.deepEqual({ ...component.highlightButtons({ id: 'h181' }) }, { run: false, runText: '', checklist: true });
  assert.deepEqual({ ...component.highlightButtons({ id: 'h190' }) }, { run: false, runText: '', checklist: true });
  assert.deepEqual({ ...component.highlightButtons({ id: 'h999' }) }, { run: false, runText: '', checklist: false });
});

test('the highlight button starts the explore tour of that highlight after an empty queue', () => {
  const { component, store } = createPanel();
  component.showHighlight({ id: 'h184' });
  // The queue is an array of the jsdom realm, so compare plain copies.
  assert.deepEqual(JSON.parse(JSON.stringify(store.started)), [['explore', 'h184', []]]);
  assert.deepEqual(JSON.parse(JSON.stringify(store.queue)), []);
});

test('checklistHref opens the checklist in explore mode, on the highlight when given', () => {
  const { component } = createPanel();
  assert.equal(component.checklistHref(), '/?panel=settings&page=checklist&mode=explore');
  assert.equal(component.checklistHref({ id: 'h184' }), '/?panel=settings&page=checklist&mode=explore&item=h184');
});

test('a changelog entry without id, the shape the API really sends, gets the id h<PR>', () => {
  const { component, store } = createPanel();
  const real = { pr: 184, text: 'Guest', highlight: true };
  assert.equal(component.highlightId(real), 'h184');
  assert.equal(component.highlightId({ id: 'h181', pr: 181 }), 'h181', 'an explicit id wins');
  assert.equal(component.highlightId({ text: 'no pr' }), '', 'no id and no PR, no id');
  assert.equal(component.highlightState(real), 'new');
  assert.equal(component.highlightLabel(real), de['onboarding.checklist.news_new']);
  assert.deepEqual({ ...component.highlightButtons(real) }, { run: true, runText: de['onboarding.checklist.action_show'], checklist: true });
  assert.equal(component.checklistHref(real), '/?panel=settings&page=checklist&mode=explore&item=h184');
  component.showHighlight(real);
  assert.deepEqual(JSON.parse(JSON.stringify(store.started)), [['explore', 'h184', []]]);
});

test('?all=1 starts the all view, anything else and a ?release= link start in the highlights view', () => {
  const plain = createPanel();
  plain.component.init();
  assert.equal(plain.component.mode, 'highlights');
  const all = createPanel({ url: 'https://dashboard.local/?panel=settings&page=versions&all=1' });
  all.component.init();
  assert.equal(all.component.mode, 'all');
  const release = createPanel({ url: 'https://dashboard.local/?panel=settings&page=versions&release=v0.12.0' });
  release.component.init();
  assert.equal(release.component.mode, 'highlights');
});

test('a release deep link opens the dashboard changelog and loads its releases', async () => {
  const calls = [];
  const panel = createPanel({
    url: 'https://dashboard.local/?panel=settings&page=versions&release=v0.12.0',
    fetchImpl: async (url) => {
      calls.push(url);
      if (url === '/api/v1/versions') return jsonResponse(SNAPSHOT);
      if (url === '/api/v1/changelog?component=dashboard&bundles=5') {
        return jsonResponse({ components: [{ id: 'dashboard', releases: [{ version: 'v0.12.0', date: '2026-10-30', groups: [] }] }] });
      }
      return jsonResponse({}, false);
    },
  });
  panel.component.init();
  await panel.component.load();
  assert.equal(panel.component.open.dashboard, true);
  assert.equal(panel.component.releases.dashboard[0].version, 'v0.12.0');
  assert.equal(panel.component.mode, 'highlights');
  assert.ok(calls.includes('/api/v1/changelog?component=dashboard&bundles=5'));
});

test('without ?release= the page loads no changelog on its own', async () => {
  const calls = [];
  const panel = createPanel({
    fetchImpl: async (url) => {
      calls.push(url);
      if (url === '/api/v1/versions') return jsonResponse(SNAPSHOT);
      return jsonResponse({}, false);
    },
  });
  panel.component.init();
  await panel.component.load();
  assert.deepEqual({ ...panel.component.open }, {});
  assert.equal(calls.some((url) => url.startsWith('/api/v1/changelog')), false);
});

test('the release list marks each highlight and each release for the deep link', () => {
  assert.match(template, /x-bind:data-highlight="highlightId\(entry\) \|\| null"/);
  assert.match(template, /x-bind:data-release="release\.version"/);
});

test('an entry with details renders the shared highlight details, the plain text stays without them', () => {
  assert.match(template, /<template x-if="entry\.details">\s*\{\{template "highlightDetails"\}\}\s*<\/template>/);
  assert.match(template, /<span x-show="!entry\.details">/);
});

test('the toolbar links to the checklist explore mode instead of the what\'s new dialog', () => {
  assert.equal(template.includes('update-whatsnew-open'), false);
  assert.equal(template.includes('settings.versions.whatsnew'), false);
  assert.match(template, /\{\{t "settings\.versions\.explore_link"\}\}/);
  assert.match(template, /x-bind:href="checklistHref\(\)"/);
});

test('the versions page opens with one prominent row to the checklist explore mode', () => {
  assert.match(template, /<a class="ob-crosslink" x-bind:href="checklistHref\(\)">\s*<svg[^>]*><use href="#ico-check"><\/use><\/svg>\s*<span class="ob-crosslink-text">\s*<strong class="ob-crosslink-title">\{\{t "settings\.versions\.explore_link"\}\}<span class="ob-marker" aria-hidden="true" x-show="\$store\.onboarding\.unseenCount"><\/span><\/strong>\s*<span class="ob-crosslink-sub">\{\{t "settings\.versions\.explore_sub"\}\}<\/span>/);
  assert.equal(template.includes('class="icon-button labeled accent" x-bind:href="checklistHref()"'), false);
  assert.ok(template.indexOf('ob-crosslink') < template.indexOf('class="setting-card"'), 'the row sits above the first card');
});

test('the versions tab of the settings nav shows the unseen marker', () => {
  assert.match(settingsTemplate, /\{\{t "settings\.nav\.versions"\}\}<span class="ob-marker" aria-hidden="true" x-show="\$store\.onboarding\.unseenCount"><\/span>/);
});

test('the catalogs drop the what\'s new key and carry the new texts in both languages', () => {
  const keys = ['settings.versions.explore_link', 'settings.versions.highlight.checklist', 'settings.versions.highlight.text'];
  for (const key of keys) {
    assert.ok(de[key], `de ${key}`);
    assert.ok(en[key], `en ${key}`);
  }
  assert.equal(de['settings.versions.explore_link'], 'Neu erkunden');
  assert.equal(en['settings.versions.explore_link'], "Explore what's new");
  assert.equal('settings.versions.whatsnew' in de, false);
  assert.equal('settings.versions.whatsnew' in en, false);
  assert.equal('settings.versions.highlight.seen' in de, false, 'the seen state uses onboarding.checklist.news_seen');
  assert.equal('settings.versions.highlight.seen' in en, false);
  assert.equal('update_bar.modal.close' in de, false);
  assert.equal('settings.whatsnew.title' in de, false);
  assert.equal(de['onboarding.checklist.release_notes'], 'Release-Notes');
  assert.equal(en['onboarding.checklist.release_notes'], 'Release notes');
  // Die Zeile "Alle Versionen" steht nicht mehr in Neu erkunden, ihre Texte sind weg.
  for (const catalog of [de, en]) {
    assert.equal('onboarding.checklist.all_versions' in catalog, false);
    assert.equal('onboarding.checklist.all_versions_sub' in catalog, false);
  }
  assert.equal(de['settings.versions.explore_sub'], 'Neuerungen mit Tour Schritt für Schritt ansehen.');
  assert.equal(en['settings.versions.explore_sub'], 'Walk through what is new, step by step.');
});

test('the crosslink motion is transitions only on named properties, never all, and it respects reduced motion', () => {
  const css = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'css', 'onboarding.css'), 'utf8');
  assert.doesNotMatch(css, /transition:\s*all\b/);
  assert.match(css, /\.ob-crosslink\s*\{[^}]*transition:\s*transform 160ms cubic-bezier\(0\.23, 1, 0\.32, 1\), background-color 160ms cubic-bezier\(0\.23, 1, 0\.32, 1\), border-color 160ms cubic-bezier\(0\.23, 1, 0\.32, 1\);/);
  assert.match(css, /@media \(prefers-reduced-motion: reduce\)\s*\{[^}]*\.ob-crosslink/);
});

test('the version card head wraps onto clean lines: the chip and title never break, the eyebrow drops below', () => {
  const css = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'css', 'onboarding.css'), 'utf8');
  assert.match(css, /\.ob-cl-summary-head\s*\{[^}]*flex-wrap: wrap;[^}]*row-gap: \.35rem;/);
  assert.match(css, /\.ob-cl-summary-head \.setting-card-title\s*\{\s*white-space: nowrap;\s*\}/);
  assert.match(css, /\.ob-cl-summary-head \.ob-crosslink--chip\s*\{\s*white-space: nowrap;\s*margin-left: auto;\s*\}/);
});

test('a stale highlight shows the hint and a disabled tour button, the checklist link stays', () => {
  const { component } = createPanel();
  assert.equal(component.highlightState({ id: 'h192' }), 'stale');
  assert.equal(component.highlightLabel({ id: 'h192' }), de['onboarding.checklist.news_stale']);
  assert.deepEqual({ ...component.highlightButtons({ id: 'h192' }) }, { run: true, runText: de['onboarding.checklist.action_show'], checklist: true });
  assert.match(template, /x-bind:disabled="highlightState\(entry\) === 'stale'"/);
});

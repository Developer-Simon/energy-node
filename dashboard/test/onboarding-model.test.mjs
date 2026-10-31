// Entscheidungskern der Ersteinrichtung und Neu erkunden. Reine Funktionen,
// die Datei laeuft in einem eigenen JSDOM-Kontext wie onboarding-motion.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'onboarding-model.js'),
  'utf8',
);

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  vm.runInContext(source, dom.getInternalVMContext());
  return dom.window.obModel;
}

const AREAS = [
  { id: 'access', label: 'Zugang & Benutzer' },
  { id: 'display', label: 'Darstellung' },
  { id: 'system', label: 'System' },
  { id: 'devices', label: 'Geräte & Energierollen' },
];

// ctx.t liest den echten Katalog und fuellt die {name}-Platzhalter.
const catalog = JSON.parse(fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'catalogs', 'de.json'),
  'utf8',
));
const t = (key, params = {}) => catalog[key].replace(/\{(\w+)\}/g, (_, name) => String(params[name]));

const ctx = (overrides = {}) => ({
  role: 'admin',
  can: new Set(['view_devices', 'view_stats', 'edit_users']),
  device: 'desktop',
  installed: ['tailscale'],
  t,
  ...overrides,
});

test('deviceOf boundary 759/760', () => {
  const m = load();
  assert.equal(m.deviceOf(759), 'phone');
  assert.equal(m.deviceOf(760), 'desktop');
});

test('phone-only step hidden on desktop', () => {
  const m = load();
  const step = { id: 's', device: 'phone', title: 'T', text: '', target: '', check: 'leave' };
  assert.equal(m.stepAllowed(step, ctx({ device: 'desktop' })), false);
  assert.equal(m.stepAllowed(step, ctx({ device: 'phone' })), true);
});

test('rights filter drops step without need', () => {
  const m = load();
  const step = { id: 's', need: 'edit_users' };
  assert.equal(m.stepAllowed(step, ctx({ can: new Set(['view_devices']) })), false);
  assert.equal(m.stepAllowed(step, ctx({ can: new Set(['edit_users']) })), true);
});

test('service filter drops step for missing service', () => {
  const m = load();
  const step = { id: 's', needs: 'tailscale' };
  assert.equal(m.stepAllowed(step, ctx({ installed: [] })), false);
  assert.equal(m.stepAllowed(step, ctx({ installed: ['tailscale'] })), true);
});

test('highlight only for other device is not counted unseen', () => {
  const m = load();
  const explore = [{
    version: '0.12.0', current: true,
    items: [{ id: 'h1', steps: [{ id: 'h1a', device: 'phone' }] }],
  }];
  assert.equal(m.unseenCount(explore, {}, ctx({ device: 'desktop' })), 0);
  assert.equal(m.unseenCount(explore, {}, ctx({ device: 'phone' })), 1);
});

test('exploreState other-device vs text', () => {
  const m = load();
  const phoneOnly = { id: 'a', steps: [{ id: 'a1', device: 'phone' }] };
  assert.equal(m.exploreState(phoneOnly, {}, ctx({ device: 'desktop' })), 'other-device');
  assert.equal(m.exploreState({ id: 'b', tour: false }, {}, ctx()), 'text');
  const rightsOnly = { id: 'c', steps: [{ id: 'c1', need: 'edit_users' }] };
  assert.equal(m.exploreState(rightsOnly, {}, ctx({ can: new Set() })), 'text');
  const desk = { id: 'd', steps: [{ id: 'd1' }] };
  assert.equal(m.exploreState(desk, {}, ctx()), 'new');
  assert.equal(m.exploreState(desk, { d: true }, ctx()), 'seen');
});

test('nextAfterResize skips other-device steps', () => {
  const m = load();
  const steps = [{ id: 'p', device: 'phone' }, { id: 'd', device: 'desktop' }, { id: 'any' }];
  assert.equal(m.nextAfterResize(steps, 0, 'desktop'), 1);
});

test('nextAfterResize returns -1 when nothing left', () => {
  const m = load();
  const steps = [{ id: 'd', device: 'desktop' }, { id: 'p', device: 'phone' }];
  assert.equal(m.nextAfterResize(steps, 1, 'desktop'), -1);
});

test('welcome replaces explore in queue', () => {
  const m = load();
  // [...x] holt die Ergebnisse aus dem VM-Kontext in ein Array dieses Realms.
  assert.deepEqual([...m.queueAfter(['explore', 'welcome'], 'setup')], ['welcome']);
  assert.deepEqual([...m.queueAfter(['explore'], 'setup')], ['explore']);
  assert.deepEqual([...m.queueAfter(['welcome', 'setup'], 'setup')], ['welcome']);
  // Ohne welcome bleibt explore, die Reihenfolge ist setup vor explore.
  assert.deepEqual([...m.queueAfter(['explore', 'setup'], 'welcome')], ['setup', 'explore']);
});

test('setup step with unknown area is skipped', () => {
  const m = load();
  const setup = [
    { id: 'known', area: 'display', title: 'A', text: '', target: '', check: 'change' },
    { id: 'orphan', area: 'nowhere', title: 'B', text: '', target: '', check: 'change' },
  ];
  const steps = m.buildSteps({ mode: 'setup', setup, areas: AREAS, seen: {}, status: {}, ctx: ctx() });
  assert.deepEqual([...steps.map((s) => s.id)], ['setup-intro', 'known']);
  assert.equal(steps[1].dots, 1);
});

test('setup starts with the intro step for an admin who has not seen it', () => {
  const m = load();
  const setup = [{ id: 'theme', area: 'display', title: 'B', text: '', target: '[data-tour="theme"]', check: 'change' }];
  const steps = m.buildSteps({ mode: 'setup', from: null, setup, areas: AREAS, seen: {}, status: {}, ctx: ctx() });
  assert.deepEqual([...steps.map((s) => s.id)], ['setup-intro', 'theme']);
  const [intro] = steps;
  assert.equal(intro.intro, true);
  assert.equal(intro.target, null);
  assert.equal(intro.panel, null);
  assert.equal(intro.dots, 0);
  assert.equal(intro.title, 'onboarding.setup.intro.title');
  assert.equal(intro.text, 'onboarding.setup.intro.text');
  assert.equal(intro.eyebrow, 'Einrichtung · Willkommen');
});

test('the intro step is absent once it is seen', () => {
  const m = load();
  const setup = [{ id: 'theme', area: 'display', title: 'B', text: '', target: '', check: 'change' }];
  const steps = m.buildSteps({ mode: 'setup', from: null, setup, areas: AREAS, seen: { 'setup-intro': true }, status: {}, ctx: ctx() });
  assert.deepEqual([...steps.map((s) => s.id)], ['theme']);
});

test('the intro step is absent when the run starts at a step', () => {
  const m = load();
  const setup = [
    { id: 'theme', area: 'display', title: 'B', text: '', target: '', check: 'change' },
    { id: 'title', area: 'display', title: 'C', text: '', target: '', check: 'change' },
  ];
  const steps = m.buildSteps({ mode: 'setup', from: 'title', setup, areas: AREAS, seen: {}, status: {}, ctx: ctx() });
  assert.deepEqual([...steps.map((s) => s.id)], ['title']);
});

test('the intro step is absent for a user and without real steps', () => {
  const m = load();
  const setup = [{ id: 'theme', area: 'display', title: 'B', text: '', target: '', check: 'change' }];
  assert.deepEqual([...m.buildSteps({ mode: 'setup', from: null, setup, areas: AREAS, seen: {}, status: {}, ctx: ctx({ role: 'user' }) })], []);
  assert.deepEqual([...m.buildSteps({ mode: 'setup', from: null, setup: [], areas: AREAS, seen: {}, status: {}, ctx: ctx() })], []);
});

test('eyebrow strings match the draft', () => {
  const m = load();
  const setup = [
    { id: 'tailscale', area: 'access', title: 'A', text: '', target: '', check: 'leave', needs: 'tailscale' },
    { id: 'theme', area: 'display', title: 'B', text: '', target: '', check: 'change' },
    { id: 'devices', area: 'devices', title: 'C', text: '', target: '', check: 'change' },
    { id: 'roles', area: 'devices', title: 'D', text: '', target: '', check: 'change' },
  ];
  const setupSteps = m.buildSteps({ mode: 'setup', from: null, setup, areas: AREAS, seen: {}, status: {}, ctx: ctx() });
  const theme = setupSteps.find((s) => s.id === 'theme');
  assert.equal(theme.eyebrow, 'Einrichtung 2 von 4 · Darstellung');
  assert.equal(theme.dot, 0);
  assert.equal(theme.dots, 1);

  const change = m.buildSteps({ mode: 'change', from: 'theme', setup, areas: AREAS, seen: {}, status: {}, ctx: ctx() });
  assert.equal(change.length, 1);
  assert.equal(change[0].eyebrow, 'Einrichtung · Ändern');
  assert.equal(change[0].dots, 1);

  const explore = [{
    version: '0.12.0', current: true,
    items: [{ id: 'h1', title: 'H', text: '', steps: [
      { id: 'h1a', title: 'H', text: '', target: '', check: 'leave' },
      { id: 'h1b', title: 'H', text: '', target: '', check: 'leave' },
    ] }],
  }];
  const exploreSteps = m.buildSteps({ mode: 'explore', from: null, explore, seen: {}, status: {}, ctx: ctx() });
  assert.equal(exploreSteps[0].eyebrow, 'Neu in 0.12 · 1 von 2');
  assert.equal(exploreSteps[1].eyebrow, 'Neu in 0.12 · 2 von 2');

  const welcome = [1, 2, 3, 4].map((i) => ({ id: `w${i}`, title: 'W', text: '', target: '', check: 'leave' }));
  const welcomeSteps = m.buildSteps({ mode: 'welcome', from: null, welcome, seen: {}, status: {}, ctx: ctx() });
  assert.equal(welcomeSteps[0].eyebrow, 'Willkommen · 1 von 4');

  assert.equal(m.buildSteps({ mode: 'setup', setup, areas: AREAS, seen: {}, status: {}, ctx: ctx({ role: 'user' }) }).length, 0);
});

// Geraetekontext des Automations-Assistenten: Rollen je Geraet, Stellglieder,
// Messwerte, Schalter, Verfuegbarkeit und Netzbilanz. Das Modul hat kein eigenes
// DOM, daher laedt der Test es in einen jsdom-Kontext mit echtem i18n.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const i18n = installI18n(dom.window);
  const context = dom.getInternalVMContext();
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'automation-wizard.js'), 'utf8'), context);
  return { wizard: dom.window.__automationWizard, t: (key, params) => i18n.t(key, params) };
}

// Objekte aus dem VM-Kontext haben andere Prototypen, daher vor dem Vergleich
// einmal durch JSON laufen lassen.
const plain = (value) => JSON.parse(JSON.stringify(value));

const devices = [
  {
    id: 'pv1', name: 'Wechselrichter Dach',
    entities: [
      { unique_id: 'pv1_limit', name: 'Leistungsgrenze', command_topic: 'werkstatt/pv1/limit', min_value: 50, max_value: 800, step: 10, unit_of_measurement: 'W', has_value: true, value: '500' },
      { unique_id: 'pv1_power', name: 'Leistung', state_topic: 'werkstatt/pv1/power', template_supported: true, unit_of_measurement: 'W', has_value: true, value: '742' },
    ],
  },
  {
    id: 'pv2', name: 'Wechselrichter Garage',
    entities: [
      { unique_id: 'pv2_power', name: 'Leistung', state_topic: 'werkstatt/pv2/power', template_supported: true, unit_of_measurement: 'W', has_value: true, value: '120' },
    ],
  },
  {
    id: 'wb', name: 'Wallbox',
    entities: [
      { unique_id: 'wb_limit', name: 'Ladeleistung', command_topic: 'werkstatt/wb/limit', min_value: 1400, max_value: 11000, step: 100, unit_of_measurement: 'W', has_value: true, value: '5000' },
      { unique_id: 'wb_switch', name: 'Laden', command_topic: 'werkstatt/wb/switch', payload_on: 'ON', payload_off: 'OFF', has_value: true, value: 'ON' },
      { unique_id: 'wb_plug', name: 'Stecker', state_topic: 'werkstatt/wb/plug', template_supported: true, device_class: 'plug', has_value: true, value: 'ON' },
      { unique_id: 'wb_power', name: 'Ladeleistung Ist', state_topic: 'werkstatt/wb/power', template_supported: true, unit_of_measurement: 'W', has_value: true, value: '1400' },
    ],
  },
  {
    id: 'plugs', name: 'Steckdosen',
    entities: [
      { unique_id: 'sw_a', name: 'Steckdose A', command_topic: 'werkstatt/plugs/a', payload_on: 'ON', payload_off: 'OFF', has_value: true, value: 'OFF' },
      { unique_id: 'sw_b', name: 'Steckdose B', command_topic: 'werkstatt/plugs/b', payload_on: 'ON', payload_off: 'OFF', has_value: true, value: 'ON' },
      { unique_id: 'sw_c', name: 'Nur Ein', command_topic: 'werkstatt/plugs/c', payload_on: 'ON', has_value: true, value: 'ON' },
    ],
  },
];

const snapshot = {
  values: { pv: 862, wallbox: 1400, battery_soc: 0.64 },
  entities: [
    { device_id: 'pv1', entity_id: 'pv1_power', role: { role: 'pv' } },
    { device_id: 'pv2', entity_id: 'pv2_power', role: { role: 'pv' } },
    { device_id: 'wb', entity_id: 'wb_power', role: { role: 'wallbox' } },
  ],
};

const document = {
  rules: [{ id: 'rule-1', name: 'Nachtladung' }],
  scenarios: [
    { id: 'scen-1', name: 'Regler Dach', while: { regulator: { output: { topic: 'werkstatt/pv1/limit' } } } },
  ],
  settings: { latitude: 52.5, longitude: 13.4 },
};

const build = (wizard, overrides = {}) => wizard.buildContext({ devices, snapshot, document, ...overrides });

test('inverters list every pv device and disable the one without a limit', () => {
  const { wizard, t } = load();
  const ctx = plain(build(wizard));
  assert.deepEqual(ctx.inverters.map((item) => item.deviceName), ['Wechselrichter Dach', 'Wechselrichter Garage']);
  assert.equal(ctx.inverters[0].id, 'pv1_limit');
  assert.equal(ctx.inverters[0].disabled, t('automations.assistant.device.already_regulated', { name: 'Regler Dach' }));
  assert.equal(ctx.inverters[1].disabled, t('automations.assistant.device.no_limit'));
  assert.ok(ctx.inverters[1].disabled.length > 0);
});

test('an inverter remembers its measured pv entity', () => {
  const { wizard } = load();
  const ctx = plain(build(wizard, { document: { rules: [], scenarios: [], settings: {} } }));
  const [dach, garage] = ctx.inverters;
  assert.equal(dach.measured.unique_id, 'pv1_power');
  assert.equal(dach.live, '742 W');
  assert.equal(garage.measured.unique_id, 'pv2_power');
  assert.equal(garage.live, '120 W');
  assert.equal(garage.disabled, load().t('automations.assistant.device.no_limit'));
});

test('an already regulated output is disabled with the scenario name', () => {
  const { wizard, t } = load();
  const ctx = plain(build(wizard));
  assert.equal(ctx.inverters[0].disabled, t('automations.assistant.device.already_regulated', { name: 'Regler Dach' }));
  assert.ok(ctx.inverters[0].disabled.includes('Regler Dach'));
  assert.deepEqual(ctx.regulatedTopics, { 'werkstatt/pv1/limit': 'Regler Dach' });
});

test('wallbox entries carry the charging switch and the plug sensor', () => {
  const { wizard } = load();
  const ctx = plain(build(wizard));
  assert.equal(ctx.wallboxes.length, 1);
  const [box] = ctx.wallboxes;
  assert.equal(box.deviceName, 'Wallbox');
  assert.equal(box.id, 'wb_limit');
  assert.equal(box.switchEntity.unique_id, 'wb_switch');
  assert.equal(box.plugEntity.unique_id, 'wb_plug');
  assert.equal(box.measured.unique_id, 'wb_power');
  assert.equal(box.live, '1400 W');
  assert.equal(box.disabled, '');
});

test('switches need payload_on and payload_off', () => {
  const { wizard } = load();
  const ctx = plain(build(wizard));
  assert.deepEqual(ctx.switches.map((item) => item.id), ['wb_switch', 'sw_a', 'sw_b']);
  assert.equal(ctx.switches[1].deviceName, 'Steckdosen');
  assert.equal(ctx.switches[1].live, 'OFF');
});

test('socAvailable follows the snapshot values', () => {
  const { wizard } = load();
  assert.equal(build(wizard).socAvailable, true);
  const without = build(wizard, { snapshot: { values: { pv: 10 }, entities: [] } });
  assert.equal(without.socAvailable, false);
});

test('hasLocation needs both coordinates and usedIds lists rules and scenarios', () => {
  const { wizard } = load();
  const ctx = plain(build(wizard));
  assert.equal(ctx.hasLocation, true);
  assert.deepEqual(ctx.usedIds, ['rule-1', 'scen-1']);
  const noCoords = plain(build(wizard, { document: { rules: [], scenarios: [], settings: { latitude: 52.5 } } }));
  assert.equal(noCoords.hasLocation, false);
});

test('gridNet matches the energy model for split and signed grid roles', () => {
  const { wizard } = load();
  assert.equal(wizard.gridNet({ values: { grid_import: 300, grid_export: 0 } }), 300);
  assert.equal(wizard.gridNet({ values: { grid: -470 } }), -470);
  assert.equal(wizard.gridNet({ values: { pv: 800 } }), null);
});

test('devicesFor returns the list for each filter', () => {
  const { wizard } = load();
  const ctx = build(wizard);
  assert.equal(wizard.devicesFor('inverter', ctx), ctx.inverters);
  assert.equal(wizard.devicesFor('wallbox', ctx), ctx.wallboxes);
  assert.equal(wizard.devicesFor('switch', ctx), ctx.switches);
});

// Kleine Vorlagen mit fertigen Texten, damit die Suche ohne Katalogeintraege
// der Vorlagen laeuft (die Vorlagen selbst kommen in Task 4).
const zeroFeed = {
  id: 'zero_feed', category: 'feed_in', title: 'Nulleinspeisung',
  blurb: 'Speist nur so viel ein, wie das Haus braucht.', keywords: 'einspeisung null limit drossel',
  requires: { devices: 'inverter' },
  steps: [
    { id: 'device', type: 'device', filter: 'inverter' },
    { id: 'period', type: 'tiles', default: 'sun', options: [
      { id: 'sun', unavailable: (ctx) => (ctx.hasLocation ? '' : 'automations.assistant.unavailable.location') },
      { id: 'always' },
      { id: 'fixed' },
    ] },
    { id: 'window', type: 'time', when: (answers) => answers.period === 'fixed', default: { from: '08:00', to: '18:00', days: [0, 1, 2, 3, 4, 5, 6] } },
    { id: 'extras', type: 'multi', options: [
      { id: 'reset', default: true },
      { id: 'notify' },
      { id: 'deadline', reveal: { type: 'time', default: '07:00' } },
    ] },
  ],
};

const chargeTemplate = {
  id: 'surplus_wallbox', category: 'charging', title: 'Überschussladen',
  blurb: 'Das Auto lädt mit Sonnenstrom.', keywords: 'wallbox auto laden',
  requires: { devices: 'wallbox' },
  steps: [
    { id: 'device', type: 'device', filter: { role: 'wallbox' } },
    { id: 'start', type: 'presets', default: 1400, options: [{ id: 1400 }, { id: 2000 }] },
  ],
};

const timeTemplate = {
  id: 'time_switch', category: 'time', title: 'Zeitfenster schalten',
  blurb: 'Ein Gerät läuft zu festen Zeiten.', keywords: 'zeit uhr schalten',
  requires: { devices: 'switch' },
  steps: [
    { id: 'device', type: 'device', filter: 'switch' },
    { id: 'window', type: 'time', default: { from: '11:00', to: '15:00', days: [0, 1, 2, 3, 4, 5, 6] } },
  ],
};

const batteryTemplate = {
  id: 'battery_warn', category: 'warning', title: 'Akku-Warnung',
  blurb: 'Mitteilung bei niedrigem Ladestand.', keywords: 'akku batterie soc',
  requires: { soc: true },
  steps: [
    { id: 'below', type: 'presets', default: 20, options: [{ id: 10 }, { id: 20 }] },
  ],
};

test('visibleSteps drops a step whose when is false and always ends with summary', () => {
  const { wizard } = load();
  const ids = plain(wizard.visibleSteps(zeroFeed, { period: 'always' })).map((step) => step.id);
  assert.deepEqual(ids, ['device', 'period', 'extras', 'summary']);
  const fixed = plain(wizard.visibleSteps(zeroFeed, { period: 'fixed' })).map((step) => step.id);
  assert.deepEqual(fixed, ['device', 'period', 'window', 'extras', 'summary']);
});

test('changing an earlier answer recounts the steps at once', () => {
  const { wizard } = load();
  const before = wizard.visibleSteps(zeroFeed, { period: 'fixed' });
  const after = wizard.visibleSteps(zeroFeed, { period: 'always' });
  assert.equal(after.length, before.length - 1);
  assert.equal(after.some((step) => step.id === 'window'), false);
});

test('defaultAnswers skips an unavailable default option', () => {
  const { wizard } = load();
  const noLocation = wizard.buildContext({ devices, snapshot, document: { rules: [], scenarios: [], settings: {} } });
  assert.equal(plain(wizard.defaultAnswers(zeroFeed, noLocation)).period, 'always');
  const withLocation = wizard.buildContext({ devices, snapshot, document });
  assert.equal(plain(wizard.defaultAnswers(zeroFeed, withLocation)).period, 'sun');
});

test('defaultAnswers picks the first usable device and copies time and multi answers', () => {
  const { wizard } = load();
  const ctx = wizard.buildContext({ devices, snapshot, document });
  const answers = plain(wizard.defaultAnswers(zeroFeed, ctx));
  assert.equal(answers.device, '');
  assert.equal(plain(wizard.defaultAnswers(chargeTemplate, ctx)).device, 'wb_limit');
  assert.deepEqual(answers.extras, { reset: true, notify: false, deadline: false, deadline_value: '07:00' });
  const timeAnswers = plain(wizard.defaultAnswers(timeTemplate, ctx));
  assert.deepEqual(timeAnswers.window, { from: '11:00', to: '15:00', days: [0, 1, 2, 3, 4, 5, 6] });
  assert.equal(timeAnswers.device, 'wb_switch');
});

test('templateAvailability names the missing device and hints at the found ones', () => {
  const { wizard, t } = load();
  const ctx = wizard.buildContext({ devices, snapshot, document });
  assert.deepEqual(plain(wizard.templateAvailability(zeroFeed, ctx)), {
    available: false,
    reason: t('automations.assistant.unavailable.inverter'),
    hint: '',
  });
  assert.deepEqual(plain(wizard.templateAvailability(chargeTemplate, ctx)), {
    available: true,
    reason: '',
    hint: t('automations.assistant.hint.device_one', { name: 'Wallbox' }),
  });
  assert.equal(wizard.templateAvailability(timeTemplate, ctx).hint, t('automations.assistant.hint.device_many', { n: 3 }));
  assert.equal(wizard.templateAvailability(batteryTemplate, ctx).available, true);
  const noSoc = wizard.buildContext({ devices, snapshot: { values: {}, entities: [] }, document });
  assert.equal(wizard.templateAvailability(batteryTemplate, noSoc).reason, t('automations.assistant.unavailable.soc'));
});

test('searchTemplates splits fitting and others and finds keywords', () => {
  const { wizard } = load();
  const ctx = wizard.buildContext({ devices, snapshot, document });
  const templates = [zeroFeed, chargeTemplate, timeTemplate];
  const all = plain(wizard.searchTemplates(templates, { query: '', category: 'all' }, ctx));
  assert.deepEqual(all.fitting.map((item) => item.id), ['surplus_wallbox', 'time_switch']);
  assert.deepEqual(all.others.map((item) => item.id), ['zero_feed']);
  const keyword = plain(wizard.searchTemplates(templates, { query: 'auto', category: 'all' }, ctx));
  assert.deepEqual(keyword.fitting.map((item) => item.id), ['surplus_wallbox']);
  assert.deepEqual(keyword.others, []);
  const umlaut = plain(wizard.searchTemplates(templates, { query: 'Überschuss', category: 'all' }, ctx));
  assert.deepEqual(umlaut.fitting.map((item) => item.id), ['surplus_wallbox']);
  const unavailable = plain(wizard.searchTemplates(templates, { query: 'drossel', category: 'all' }, ctx));
  assert.deepEqual(unavailable.fitting, []);
  assert.deepEqual(unavailable.others.map((item) => item.id), ['zero_feed']);
  const byCategory = plain(wizard.searchTemplates(templates, { query: '', category: 'charging' }, ctx));
  assert.deepEqual(byCategory.fitting.map((item) => item.id), ['surplus_wallbox']);
  assert.deepEqual(byCategory.others, []);
});

test('CATEGORIES lists the five template categories', () => {
  const { wizard } = load();
  assert.deepEqual(plain(wizard.CATEGORIES), ['feed_in', 'charging', 'switching', 'warning', 'time']);
});

test('uniqueId appends a counter', () => {
  const { wizard } = load();
  assert.equal(wizard.uniqueId('zero_feed', []), 'zero_feed');
  assert.equal(wizard.uniqueId('zero_feed', ['rule-1']), 'zero_feed');
  assert.equal(wizard.uniqueId('zero_feed', ['zero_feed']), 'zero_feed_2');
  assert.equal(wizard.uniqueId('zero_feed', ['zero_feed', 'zero_feed_2']), 'zero_feed_3');
});

test('stepComplete checks device, number, presets and time', () => {
  const { wizard } = load();
  assert.equal(wizard.stepComplete({ type: 'device' }, ''), false);
  assert.equal(wizard.stepComplete({ type: 'device' }, 'pv1_limit'), true);
  assert.equal(wizard.stepComplete({ type: 'number', min: 10, max: 90 }, 5), false);
  assert.equal(wizard.stepComplete({ type: 'number', min: 10, max: 90 }, 40), true);
  assert.equal(wizard.stepComplete({ type: 'presets' }, 0), false);
  assert.equal(wizard.stepComplete({ type: 'presets' }, 1400), true);
  assert.equal(wizard.stepComplete({ type: 'time' }, { from: '08:00', to: '08:00', days: [] }), false);
  assert.equal(wizard.stepComplete({ type: 'time' }, { from: '25:00', to: '08:00', days: [] }), false);
  assert.equal(wizard.stepComplete({ type: 'time' }, { from: '08:00', to: '18:00', days: [] }), true);
  assert.equal(wizard.stepComplete({ type: 'tiles' }, 'sun'), true);
});

test('firstReached finds the first minute with enough feed-in', () => {
  const { wizard } = load();
  const rows = [
    { ts: 1000, min: 0, avg: 120 },
    { ts: 2000, min: -40, avg: -30 },
    { ts: 3000, min: -260, avg: -200 },
    { ts: 4000, min: -300, avg: -250 },
  ];
  assert.equal(wizard.firstReached(rows, 100), 3000);
  assert.equal(wizard.firstReached(rows, 250), 4000);
  assert.equal(wizard.firstReached(rows, 500), null);
  assert.equal(wizard.firstReached([], 10), null);
});

test('lowest reads the minimum', () => {
  const { wizard } = load();
  assert.equal(wizard.lowest([{ min: 5 }, { min: -3 }, { min: 12 }]), -3);
  assert.equal(wizard.lowest([]), null);
  assert.equal(wizard.lowest(undefined), null);
});

test('presetEffect returns nothing without history rows', async () => {
  const { wizard } = load();
  const ctx = wizard.buildContext({ devices, snapshot: { values: {} }, document: {} });
  const calls = [];
  const empty = { readRange: async (...args) => { calls.push(args); return []; } };
  const failing = { readRange: async () => { throw new Error('closed'); } };
  assert.equal(await wizard.presetEffect({ id: 'start', effect: 'starts' }, 1400, ctx, empty), '');
  assert.equal(await wizard.presetEffect({ id: 'soc', effect: 'soc' }, 40, ctx, empty), '');
  assert.equal(await wizard.presetEffect({ id: 'start', effect: 'starts' }, 1400, ctx, failing), '');
  assert.equal(await wizard.presetEffect({ id: 'soc', effect: 'soc' }, 40, ctx, failing), '');
  assert.equal(await wizard.presetEffect({ id: 'start', effect: 'none' }, 1400, ctx, empty), '');
  assert.equal(calls[0][0], '1m');
  assert.equal(calls[0][1], 'role:grid');
  assert.equal(calls[1][1], 'role:battery_soc');
  assert.ok(calls[0][3] - calls[0][2] > 23 * 3600 * 1000);
});

test('presetEffect describes the band, the first feed-in minute and the lowest charge level', async () => {
  const { wizard, t } = load();
  const ctx = wizard.buildContext({ devices, snapshot: { values: {} }, document: {} });
  const band = await wizard.presetEffect({ id: 'target', effect: 'band' }, 30, ctx, null);
  assert.equal(band, '');
  const rows = [{ ts: 1000, min: -10, avg: -20 }, { ts: 60000, min: -1500, avg: -1400 }];
  const store = { readRange: async () => rows };
  const first = new Date(60000);
  const pad = (n) => String(n).padStart(2, '0');
  const time = `${pad(first.getHours())}:${pad(first.getMinutes())}`;
  assert.equal(await wizard.presetEffect({ id: 'start', effect: 'starts' }, 1400, ctx, store),
    t('automations.assistant.effect.reached_at', { time }));
  assert.equal(await wizard.presetEffect({ id: 'start', effect: 'starts' }, 5000, ctx, store),
    t('automations.assistant.effect.not_reached'));
  const socStore = { readRange: async () => [{ min: 35 }, { min: 22 }] };
  assert.equal(await wizard.presetEffect({ id: 'soc', effect: 'soc' }, 40, ctx, socStore),
    t('automations.assistant.effect.lowest_soc', { value: '22' }));
});

test('regulatorStep matches the python cases', () => {
  const { wizard } = load();
  const reg = (overrides = {}) => ({
    output: { min: 50, max: 800, step: 10, decimals: 0 },
    direction: 'producer', target_w: 30, deadband_w: 20, gain: 0.8,
    ...overrides,
  });
  const step = (regulator, input) => plain(wizard.regulatorStep(regulator, input));
  // producer: Einspeisung senkt die Grenze
  assert.deepEqual(step(reg(), { net: -470, base: 800, lastValue: 800 }),
    { value: 400, payload: '400', reason: null, error: -500 });
  // consumer: Einspeisung hebt den Sollwert
  const consumer = reg({ direction: 'consumer', target_w: 0, output: { min: 0, max: 3700, step: 100, decimals: 0 } });
  assert.equal(step(consumer, { net: -1500, base: 0, lastValue: null }).payload, '1200');
  // Totband: nichts senden
  assert.deepEqual(step(reg(), { net: 45, base: 500, lastValue: 500 }),
    { value: null, payload: null, reason: 'deadband', error: 15 });
  // Abrunden auf step, nicht runden
  assert.equal(step(reg(), { net: -100, base: 800, lastValue: null }).payload, '690');
  // Grenzen
  assert.equal(step(reg(), { net: -5000, base: 800, lastValue: null }).payload, '50');
  assert.equal(step(reg(), { net: 5000, base: 800, lastValue: null }).payload, '800');
  // max_change_w begrenzt den Schritt gegen den letzten Wert
  assert.equal(step(reg({ max_change_w: 100 }), { net: -470, base: 800, lastValue: 800 }).payload, '700');
  // unveraendert: nichts erneut senden
  assert.deepEqual(step(reg(), { net: 60, base: 780, lastValue: 800 }),
    { value: null, payload: null, reason: 'unchanged', error: 30 });
});

test('regulatorStep uses gain 0.8 and deadband 20 when they are missing', () => {
  const { wizard } = load();
  const bare = { output: { min: 50, max: 800, step: 10, decimals: 0 }, direction: 'producer', target_w: 30 };
  assert.equal(wizard.regulatorStep(bare, { net: -470, base: 800, lastValue: 800 }).payload, '400');
  assert.equal(wizard.regulatorStep(bare, { net: 45, base: 500, lastValue: null }).reason, 'deadband');
});

test('regulatorPreview describes the next limit', () => {
  const { wizard } = load();
  const scenario = {
    while: {
      mode: 'regulate',
      regulator: {
        output: { topic: 'werkstatt/pv1/limit', min: 50, max: 800, step: 10, decimals: 0 },
        direction: 'producer', target_w: 30, deadband_w: 20, gain: 0.8,
        base: { source: 'balance', field: 'pv' },
        fallback: { payload: '800' },
      },
    },
  };
  const ctx = wizard.buildContext({ devices, snapshot: { values: { grid: -470, pv: 800 } }, document: {} });
  assert.equal(wizard.regulatorPreview(scenario, ctx), 'Die Grenze sinkt von 800 W auf 400 W.');
  const holdCtx = wizard.buildContext({ devices, snapshot: { values: { grid: 45, pv: 800 } }, document: {} });
  assert.equal(wizard.regulatorPreview(scenario, holdCtx), 'Der Regler lässt die Grenze unverändert.');
  const noNet = wizard.buildContext({ devices, snapshot: { values: { pv: 800 } }, document: {} });
  assert.equal(wizard.regulatorPreview(scenario, noNet), '');
  const rule = { while: { mode: 'hold' } };
  assert.equal(wizard.regulatorPreview(rule, ctx), '');
});

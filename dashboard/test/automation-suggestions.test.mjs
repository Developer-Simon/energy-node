// Tests for the learned-suggestions band: pure selection and texts, no DOM. The catalogs
// must carry every text the band can show, in German and English.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const webui = path.join(here, '..', 'internal', 'webui');
const jsDir = path.join(webui, 'static', 'js');
const autolearnDir = path.join(here, '..', 'internal', 'autolearn');
const suggestionsSource = fs.readFileSync(path.join(jsDir, 'automation-suggestions.js'), 'utf8');
const evidenceSource = fs.readFileSync(path.join(jsDir, 'automation-evidence.js'), 'utf8');
const catalogs = {
  de: JSON.parse(fs.readFileSync(path.join(webui, 'catalogs', 'de.json'), 'utf8')),
  en: JSON.parse(fs.readFileSync(path.join(webui, 'catalogs', 'en.json'), 'utf8')),
};

// Summary codes the new detectors emit (11a scope). switch_time gets its direction suffix at runtime.
const SUMMARY_CODES = [
  'switch_time.on', 'switch_time.off', 'runtime.forgotten', 'coupling.follow',
  'check.flapping', 'check.no_effect', 'check.overridden', 'check.offline_window', 'temperature.limit',
];
const LEGEND_SHAPES = ['day', 'events', 'scale'];
const FACT_CODES = ['window_s', 'direction_share', 'spread_min', 'hit_rate', 'overnight', 'cause', 'limit_s', 'feature', 'min_runtime', 'per_hour_max', 'season_months', 'knee'];
const CAUSE_VALUES = ['offline_window', 'clustered', 'scattered'];
const FIX_CODES = ['min_runtime', 'repeat', 'open_rule', 'add_condition', 'shift_time', 'move_before'];
const BUTTONS = ['open_assistant', 'later', 'never', 'undo'];
const BAND_KEYS = ['title', 'count_one', 'count_many', 'learning', 'more', 'less'];
const GROUPS = ['habit', 'check', 'reliability', 'energy'];

// Every catalog key the band needs, for one language.
function requiredKeys() {
  const keys = [];
  SUMMARY_CODES.forEach((code) => keys.push(`automations.learn.${code}.title`, `automations.learn.${code}.line`));
  // Die Regelmarke zeigt den Befund mit .text, deshalb braucht jede Zuverlässigkeitsprüfung auch diesen Text.
  SUMMARY_CODES.filter((code) => code.startsWith('check.')).forEach((code) => keys.push(`automations.learn.${code}.text`));
  LEGEND_SHAPES.forEach((shape) => keys.push(`automations.learn.legend.${shape}`));
  FACT_CODES.forEach((code) => keys.push(`automations.learn.fact.${code}`));
  CAUSE_VALUES.forEach((value) => keys.push(`automations.learn.fact.cause.${value}`));
  FIX_CODES.forEach((code) => keys.push(`automations.learn.fix.${code}`));
  BUTTONS.forEach((name) => keys.push(`automations.learn.button.${name}`));
  BAND_KEYS.forEach((name) => keys.push(`automations.learn.band.${name}`));
  GROUPS.forEach((name) => keys.push(`automations.learn.group.${name}`));
  keys.push('automations.learn.hint.dismissed', 'automations.learn.lane.ist', 'automations.learn.lane.soll');
  keys.push('automations.learn.season.heating', 'automations.learn.unit.kwh_week');
  return keys;
}

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  vm.runInContext(suggestionsSource, dom.getInternalVMContext());
  vm.runInContext(evidenceSource, dom.getInternalVMContext());
  return { api: dom.window.AutomationSuggestions, evidence: dom.window.AutomationEvidence, document: dom.window.document };
}

// Translation stub: returns the key and remembers the calls so params can be checked.
function recorder() {
  const calls = [];
  const t = (key, params) => {
    calls.push({ key, params });
    return key;
  };
  return { t, calls };
}

function suggestion(code, params, shape = 'day') {
  return {
    id: code + ':x',
    group: 'habit',
    device: 'Werkstatt',
    summary: { code, params },
    evidence: { shape, from: 0, to: 1440, ist: [], soll: [] },
    facts: [],
    apply: [],
  };
}

test('arrange with 6 suggestions and max 3 shows 3 and counts 3 more', () => {
  const { api } = load();
  const list = [1, 2, 3, 4, 5, 6].map((n) => ({ id: 'id' + n }));
  const { visible, rest } = api.arrange(list, 3);
  assert.deepEqual(visible.map((s) => s.id), ['id1', 'id2', 'id3']);
  assert.equal(rest, 3);
});

test('arrange keeps the list order and reports no rest when the list fits', () => {
  const { api } = load();
  const list = [{ id: 'a' }, { id: 'b' }];
  const { visible, rest } = api.arrange(list, 3);
  assert.deepEqual(visible, list);
  assert.equal(rest, 0);
});

test('arrange with max 0 or a missing list shows nothing', () => {
  const { api } = load();
  const zero = api.arrange([{ id: 'a' }, { id: 'b' }], 0);
  assert.equal(zero.visible.length, 0);
  assert.equal(zero.rest, 2);
  const none = api.arrange(undefined, 3);
  assert.equal(none.visible.length, 0);
  assert.equal(none.rest, 0);
});

test('title and line use the summary code as catalog key and pass the params', () => {
  const { api } = load();
  const { t, calls } = recorder();
  api.title(suggestion('switch_time.off', { device: 'Leiste', time: '22:30', hits: 12, days: 14 }), t);
  api.line(suggestion('switch_time.off', { device: 'Leiste', time: '22:30', hits: 12, days: 14 }), t);
  assert.deepEqual(calls.map((c) => c.key), ['automations.learn.switch_time.off.title', 'automations.learn.switch_time.off.line']);
  assert.equal(calls[0].params.time, '22:30');
  assert.equal(calls[1].params.hits, 12);
});

test('offline window minutes become clock times and the no effect share becomes percent', () => {
  const { api } = load();
  const { t, calls } = recorder();
  api.title(suggestion('check.offline_window', { rule: 'Nacht', device: 'NAS', window_from: 1350, window_to: 360 }), t);
  assert.equal(calls[0].params.window_from, '22:30');
  assert.equal(calls[0].params.window_to, '06:00');
  api.line(suggestion('check.no_effect', { rule: 'R', device: 'D', failures: 9, share: 0.38 }), t);
  assert.equal(calls[1].params.share, 38);
});

test('legend follows the evidence shape and is empty for an unknown shape', () => {
  const { api } = load();
  const { t, calls } = recorder();
  api.legend(suggestion('switch_time.on', {}, 'day'), t);
  api.legend(suggestion('switch_time.on', {}, 'curve'), t);
  assert.equal(calls[0].key, 'automations.learn.legend.day');
  assert.equal(calls.length, 1, 'unknown shape asks for no text');
});

test('every band summary code has title and line in both languages', () => {
  const missing = [];
  ['de', 'en'].forEach((lang) => {
    requiredKeys().forEach((key) => {
      const value = catalogs[lang][key];
      if (typeof value !== 'string' || value.trim() === '') missing.push(`${lang}: ${key}`);
    });
  });
  assert.deepEqual(missing, []);
});

test('band texts have no semicolons and no joining dashes', () => {
  const bad = [];
  ['de', 'en'].forEach((lang) => {
    requiredKeys().forEach((key) => {
      const value = catalogs[lang][key];
      if (/;|–|—| - /.test(value)) bad.push(`${lang}: ${key}`);
    });
  });
  assert.deepEqual(bad, []);
});

test('the summary codes of the backend are the ones the band covers', () => {
  // Scans the Text{Code: "..."} literals of the detectors (not tests). A dotted prefix such as
  // "switch_time." gets its direction suffix, which the detector takes from on and off.
  const found = new Set();
  fs.readdirSync(autolearnDir)
    .filter((name) => name.endsWith('.go') && !name.endsWith('_test.go'))
    .forEach((name) => {
      const src = fs.readFileSync(path.join(autolearnDir, name), 'utf8');
      for (const match of src.matchAll(/Text\{Code: "([a-z_.]+)"/g)) {
        const code = match[1];
        if (code.endsWith('.')) {
          found.add(code + 'on');
          found.add(code + 'off');
        } else if (/^(switch_time|runtime|coupling|check|temperature)\./.test(code)) {
          found.add(code);
        }
      }
    });
  SUMMARY_CODES.forEach((code) => assert.ok(found.has(code), `backend emits ${code}`));
});

test('the Go fact and fix codes the band shows have catalog keys in both languages', () => {
  const keys = [];
  FACT_CODES.forEach((code) => keys.push(`automations.learn.fact.${code}`));
  FIX_CODES.forEach((code) => keys.push(`automations.learn.fix.${code}`));
  ['de', 'en'].forEach((lang) => {
    keys.forEach((key) => assert.ok(catalogs[lang][key], `${lang}: ${key}`));
  });
});

test('a day suggestion draws its marks on both edges of the axis through the band', () => {
  const { api, evidence, document } = load();
  const { t } = recorder();
  const s = suggestion('switch_time.off', { device: 'Leiste', time: '22:30', hits: 12, days: 14 }, 'day');
  s.evidence = { shape: 'day', from: 0, to: 1440, ist: [1435, 0], soll: [0], ist_spans: [], soll_spans: [] };
  assert.equal(api.title(s, t), 'automations.learn.switch_time.off.title');
  const host = document.createElement('div');
  host.innerHTML = evidence.render(s.evidence, t, { full: true });
  const bars = Array.from(host.querySelectorAll('path.auto-learn-bar')).map((b) => Number(b.getAttribute('data-x')));
  assert.equal(bars.length, 2, 'one bar for minute 0 and one for minute 1435');
  assert.ok(bars.every((x) => x >= 0 && x <= 100), 'no bar outside the axis');
  assert.ok(bars.some((x) => x < 1) && bars.some((x) => x > 99), 'bars sit on both edges');
  const xs = Array.from(host.querySelectorAll('line.auto-learn-mark-soll')).map((m) => Number(m.getAttribute('x1')));
  assert.equal(xs.length, 1);
  assert.ok(xs.every((x) => x >= 0 && x <= 100), 'no mark outside the axis');
  assert.ok(xs.some((x) => x === 0));
});

// Band tests: the Alpine data object with a fake fetch. The factory comes from a fake
// Alpine.data, the way automations.page.js registers its own component.
function finding(id, extra = {}) {
  return {
    id,
    group: 'habit',
    device: 'Werkstatt',
    confidence: 'high',
    summary: { code: 'switch_time.off', params: { device: 'Leiste', time: '22:30', hits: 12, days: 14 } },
    evidence: { shape: 'day', from: 0, to: 1440, ist: [1435], soll: [0], ist_spans: [], soll_spans: [] },
    facts: [{ code: 'hit_rate', value: 0.8 }],
    apply: [{ code: 'open_rule', rule_id: 'r1' }],
    ...extra,
  };
}

// Objects made inside the vm realm fail strict deep equality, so compare plain copies.
const plain = (value) => JSON.parse(JSON.stringify(value));

function withTemplate(s) {
  return { ...s, apply: [{ code: 'follow_switch', template: 'follow_switch', prefill: { source: 'a' } }] };
}

function loadBand(routes) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const calls = [];
  let factory = null;
  dom.window.Alpine = { data: (name, fn) => { if (name === 'autoLearnBand') factory = fn; } };
  dom.window.fetch = async (url, options = {}) => {
    const method = options.method || 'GET';
    calls.push({ url, method, headers: options.headers || {} });
    const body = routes(url, method);
    return { ok: true, status: 200, json: async () => body };
  };
  const context = dom.getInternalVMContext();
  vm.runInContext(suggestionsSource, context);
  vm.runInContext(evidenceSource, context);
  assert.equal(typeof factory, 'function', 'autoLearnBand is registered with Alpine');
  const band = factory();
  return { band, calls, window: dom.window, close: () => dom.window.close() };
}

const SUGGESTIONS_PATH = '/api/v1/automations/suggestions';
const listRoutes = (body) => (url, method) => (method === 'GET' ? body : { ok: true });

test('the band loads once and reads the suggestions and the learning counts', async () => {
  const a = finding('a:1');
  const b = finding('b:2');
  const { band, calls, close } = loadBand(listRoutes({ enabled: true, suggestions: [a, b], learning: { since: 1, devices: 14, days: 4 } }));
  await band.load();
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url.endsWith(SUGGESTIONS_PATH), true);
  assert.equal(band.shown, true);
  assert.equal(band.count, 2);
  assert.equal(band.days, 4);
  assert.equal(band.devices, 14);
  close();
});

test('the band is absent when learning is off, and also when the load fails', async () => {
  const off = loadBand(listRoutes({ enabled: false, suggestions: [], learning: { since: 1, devices: 2, days: 1 } }));
  await off.band.load();
  assert.equal(off.band.shown, false);
  off.close();

  const failing = loadBand(() => { throw new Error('offline'); });
  await failing.band.load();
  assert.equal(failing.band.shown, false);
  failing.close();
});

test('without suggestions the band shows the learning text with days and devices', async () => {
  const { band, close } = loadBand(listRoutes({ enabled: true, suggestions: [], learning: { since: 1, devices: 14, days: 4 } }));
  await band.load();
  const { t, calls } = recorder();
  assert.equal(band.count, 0);
  band.learningText(t);
  assert.equal(calls[0].key, 'automations.learn.band.learning');
  assert.deepEqual(plain(calls[0].params), { days: 4, devices: 14 });
  close();
});

test('collapsed the band shows three tiles and counts the rest, expanded it shows all', async () => {
  const list = [finding('1'), finding('2'), finding('3'), finding('4'), finding('5'), finding('6')];
  const { band, close } = loadBand(listRoutes({ enabled: true, suggestions: list, learning: { since: 1, devices: 1, days: 1 } }));
  await band.load();
  assert.equal(band.visible.length, 3);
  assert.equal(band.rest, 3);
  band.expandAll();
  assert.equal(band.visible.length, 6);
  assert.equal(band.rest, 0);
  close();
});

test('dismiss removes the suggestion, shows the hint and posts the dismiss route', async () => {
  const a = finding('a:1');
  const b = finding('b:2');
  const { band, calls, window, close } = loadBand(listRoutes({ enabled: true, suggestions: [a, b], learning: { since: 1, devices: 1, days: 1 } }));
  let changed = 0;
  window.addEventListener('automation-findings-changed', () => { changed += 1; });
  await band.load();
  await band.dismiss(band.items[0]);
  const post = calls[calls.length - 1];
  assert.equal(post.method, 'POST');
  assert.equal(post.url, `${SUGGESTIONS_PATH}/${encodeURIComponent('a:1')}/dismiss`);
  assert.equal(post.headers['X-CSRF-Token'], '');
  assert.equal(band.count, 1);
  assert.notEqual(band.dropped, null, 'the hint is shown');
  assert.equal(band.dropped.item.id, 'a:1');
  assert.equal(changed, 1);
  close();
});

test('later posts the later route and shows the same hint', async () => {
  const { band, calls, close } = loadBand(listRoutes({ enabled: true, suggestions: [finding('a:1')], learning: { since: 1, devices: 1, days: 1 } }));
  await band.load();
  band.csrf = 'token-1';
  await band.later(band.items[0]);
  const post = calls[calls.length - 1];
  assert.equal(post.url.endsWith('/later'), true);
  assert.equal(post.headers['X-CSRF-Token'], 'token-1');
  assert.equal(band.count, 0);
  assert.notEqual(band.dropped, null);
  close();
});

test('undo posts the undo route and puts the suggestion back at its place', async () => {
  const a = finding('a:1');
  const b = finding('b:2');
  const c = finding('c:3');
  const { band, calls, window, close } = loadBand(listRoutes({ enabled: true, suggestions: [a, b, c], learning: { since: 1, devices: 1, days: 1 } }));
  let changed = 0;
  window.addEventListener('automation-findings-changed', () => { changed += 1; });
  await band.load();
  await band.dismiss(band.items[1]);
  await band.undo();
  const post = calls[calls.length - 1];
  assert.equal(post.url, `${SUGGESTIONS_PATH}/${encodeURIComponent('b:2')}/undo`);
  assert.deepEqual(band.items.map((s) => s.id), ['a:1', 'b:2', 'c:3']);
  assert.equal(band.dropped, null);
  assert.equal(changed, 2);
  close();
});

test('the assistant button needs a template fix and the editing permission', async () => {
  const unplanned = finding('p:1');
  const templ = withTemplate(finding('t:1'));
  const { band, close } = loadBand(listRoutes({ enabled: true, suggestions: [unplanned, templ], learning: { since: 1, devices: 1, days: 1 } }));
  await band.load();
  band.canEdit = true;
  assert.deepEqual(plain(band.actionsOf(band.items[0])), { assistant: false, apply: false, later: true, dismiss: true });
  assert.deepEqual(plain(band.actionsOf(band.items[1])), { assistant: true, apply: false, later: true, dismiss: true });
  assert.equal(band.fixOf(band.items[1]).template, 'follow_switch');
  close();
});

function withOpsFix(s) {
  const fix = { code: 'add_condition', rule_id: 'r1', ops: [{ path: ['conditions', 0], value: { type: 'entity_value' } }] };
  return { ...s, apply: [fix] };
}

test('a fix with ops and no template gets the apply button with the fix label', async () => {
  const limit = withOpsFix(finding('l:1'));
  const { band, close } = loadBand(listRoutes({ enabled: true, suggestions: [limit], learning: { since: 1, devices: 1, days: 1 } }));
  await band.load();
  band.canEdit = true;
  assert.deepEqual(plain(band.actionsOf(band.items[0])), { assistant: false, apply: true, later: true, dismiss: true });
  assert.equal(band.opFixOf(band.items[0]).code, 'add_condition');
  assert.equal(band.applyLabelOf(band.items[0], catalogT('de')), catalogs.de['automations.learn.fix.add_condition']);
  band.canEdit = false;
  assert.equal(band.actionsOf(band.items[0]).apply, false);
  close();
});

test('without the automations permission there are no buttons at all', async () => {
  const templ = withTemplate(finding('t:1'));
  const { band, close } = loadBand(listRoutes({ enabled: true, suggestions: [templ], learning: { since: 1, devices: 1, days: 1 } }));
  await band.load();
  band.canEdit = false;
  assert.deepEqual(plain(band.actionsOf(band.items[0])), { assistant: false, apply: false, later: false, dismiss: false });
  close();
});

test('the band texts and the evidence come from the shared helpers', async () => {
  const s = finding('a:1');
  const { band, close } = loadBand(listRoutes({ enabled: true, suggestions: [s], learning: { since: 1, devices: 1, days: 1 } }));
  await band.load();
  const { t, calls } = recorder();
  assert.equal(band.titleOf(s, t), 'automations.learn.switch_time.off.title');
  assert.equal(band.groupLabel(s, t), 'automations.learn.group.habit');
  assert.equal(band.confidenceOf(s, t).tone, 'ok');
  assert.equal(band.confidenceOf({ ...s, confidence: 'medium' }, t).tone, 'wait');
  assert.match(band.evidenceOf(s, t), /auto-learn-shape-day/);
  assert.equal(calls[0].key, 'automations.learn.switch_time.off.title');
  close();
});

test('expand all shows the less button, and the same method collapses again', async () => {
  const list = [finding('1'), finding('2'), finding('3'), finding('4'), finding('5')];
  const { band, close } = loadBand(listRoutes({ enabled: true, suggestions: list, learning: { since: 1, devices: 1, days: 1 } }));
  await band.load();
  assert.equal(band.moreShown, true, 'the more button shows while there is a rest');
  band.open = '5';
  band.expandAll();
  assert.equal(band.all, true);
  assert.equal(band.visible.length, 5);
  assert.equal(band.rest, 0);
  assert.equal(band.moreShown, true, 'the less button stays reachable once expanded');
  band.expandAll();
  assert.equal(band.all, false);
  assert.equal(band.visible.length, 3);
  assert.equal(band.open, '', 'a card outside the collapsed tiles closes');
  assert.equal(band.moreShown, true);
  close();
});

test('the more button hides when everything fits and the band is collapsed', async () => {
  const { band, close } = loadBand(listRoutes({ enabled: true, suggestions: [finding('1'), finding('2')], learning: { since: 1, devices: 1, days: 1 } }));
  await band.load();
  assert.equal(band.moreShown, false);
  close();
});

// Catalog-backed translation: looks the text up and fills the {placeholders}, so formatted values can be checked.
function catalogT(lang = 'de') {
  return (key, params = {}) => String(catalogs[lang][key] ?? key)
    .replace(/\{(\w+)\}/g, (_, name) => (name in params ? String(params[name]) : ''));
}

test('the legend texts exist for day, events and scale and are empty for the other shapes', () => {
  const { api } = load();
  const t = catalogT('de');
  assert.equal(api.legend(suggestion('switch_time.on', {}, 'day'), t), catalogs.de['automations.learn.legend.day']);
  assert.equal(api.legend(suggestion('switch_time.on', {}, 'events'), t), catalogs.de['automations.learn.legend.events']);
  assert.equal(api.legend(suggestion('switch_time.on', {}, 'scale'), t), catalogs.de['automations.learn.legend.scale']);
  assert.equal(api.legend(suggestion('switch_time.on', {}, 'curve'), t), '');
  assert.equal(api.legend({ summary: { code: 'x' }, evidence: {} }, t), '');
});

test('toggle closes the tooltip, and closeTip closes it for Escape and for a click outside', async () => {
  const { band, close } = loadBand(listRoutes({ enabled: true, suggestions: [finding('a:1')], learning: { since: 1, devices: 1, days: 1 } }));
  await band.load();
  band.tipOpen = true;
  band.toggle('a:1');
  assert.equal(band.tipOpen, false, 'a new card starts with the tooltip closed');
  band.toggle('a:1');
  band.tipOpen = true;
  band.closeTip();
  assert.equal(band.tipOpen, false);
  close();
});

test('toggle points the card caret at the middle of the chosen tile', async () => {
  const { band, close } = loadBand(listRoutes({ enabled: true, suggestions: [finding('a:1')], learning: { since: 1, devices: 1, days: 1 } }));
  await band.load();
  const row = { getBoundingClientRect: () => ({ left: 40, width: 900 }) };
  const tile = { parentElement: row, getBoundingClientRect: () => ({ left: 350, width: 300 }) };
  band.toggle('a:1', tile);
  assert.equal(band.caret, '460px');
  band.toggle('a:1', tile);
  assert.equal(band.open, '');
  assert.equal(band.caret, '460px', 'closing keeps the caret, so the card leaves where it came from');
  close();
});

// Fact values as the card shows them, for one language.
function factText(code, value, lang = 'de') {
  const { band, close } = loadBand(listRoutes({ enabled: true, suggestions: [], learning: { since: 1, devices: 1, days: 1 } }));
  const s = { ...finding('f:1'), facts: [{ code, value }] };
  const [fact] = band.factsOf(s, catalogT(lang));
  close();
  return fact.value;
}

test('spread in minutes is rounded to whole minutes with the unit', () => {
  assert.equal(factText('spread_min', 7.8333), '8 min');
});

test('seconds stay seconds below two minutes and switch to minutes from 120 s', () => {
  assert.equal(factText('window_s', 150), '2.5 min');
  assert.equal(factText('window_s', 90), '90 s');
  assert.equal(factText('limit_s', 120), '2 min');
  assert.equal(factText('limit_s', 59.6), '60 s');
});

test('minimum runtime is in seconds and per hour counts are whole numbers', () => {
  assert.equal(factText('min_runtime', 45), '45 s');
  assert.equal(factText('per_hour_max', 3), '3 pro Stunde');
  assert.equal(factText('per_hour_max', 3, 'en'), '3 per hour');
});

test('hit rate and direction share stay percent, season months stay as they are', () => {
  assert.equal(factText('hit_rate', 0.8), '80 %');
  assert.equal(factText('direction_share', 0.375), '38 %');
  assert.equal(factText('season_months', [1, 2]), '1,2');
});

test('any other numeric fact is rounded to at most one decimal', () => {
  assert.equal(factText('unknown_code', 3.14159), '3.1');
  assert.equal(factText('unknown_code', 4), '4');
});

test('the heating validity reads the knee and is empty without one', () => {
  const { band, close } = loadBand(listRoutes({ enabled: false, suggestions: [] }));
  const t = catalogT('de');
  const withKnee = { season: { from_week: 40, to_week: 16, heating_below_c: 15.04 } };
  assert.equal(band.heatingOf(withKnee, t), 'Heizzeit unter 15 °C');
  assert.equal(band.heatingOf({ season: { from_week: 40, to_week: 16 } }, t), '');
  assert.equal(band.heatingOf({}, t), '');
  close();
});

test('a heating only season shows no week range, a dated season shows its weeks', () => {
  const { band, close } = loadBand(listRoutes({ enabled: false, suggestions: [] }));
  const t = catalogT('de');
  assert.equal(band.seasonOf({ season: { heating_below_c: 15 } }, t), '');
  assert.equal(band.seasonOf({}, t), catalogs.de['automations.learn.season.all']);
  assert.equal(band.seasonOf({ season: { from_week: 40, to_week: 16 } }, t), 'Gilt von KW 40 bis KW 16');
  close();
});

test('a finding with only the heating limit shows the chip and no week range', () => {
  const { band, close } = loadBand(listRoutes({ enabled: false, suggestions: [] }));
  const t = catalogT('de');
  const finding = { season: { from_week: 0, to_week: 0, heating_below_c: 15.04 } };
  assert.equal(band.seasonOf(finding, t), '');
  assert.equal(band.heatingOf(finding, t), 'Heizzeit unter 15 °C');
  close();
});

test('the temperature limit title and line show the rule, device, knee and weekly kWh', () => {
  const { api } = load();
  const t = catalogT('de');
  const s = suggestion('temperature.limit', { rule: 'Pumpe Nacht', device: 'Wärmepumpe', knee: 15, kwh_warm: 1.2345 }, 'scale');
  assert.equal(api.title(s, t), 'Pumpe Nacht nur bei Außentemperatur unter 15 °C');
  assert.equal(api.line(s, t), 'In warmen Wochen verbraucht Wärmepumpe 1.2 kWh pro Woche. Mit der Bedingung schaltet die Regel nur bei Kälte.');
  assert.equal(api.legend(s, t), catalogs.de['automations.learn.legend.scale']);
});

test('a scale evidence with the weekly unit shows the catalog unit on the axis', () => {
  const { evidence } = load();
  const t = catalogT('de');
  const html = evidence.render({ shape: 'scale', ist_value: 12, soll_value: 0, unit: 'kwh_week' }, t);
  assert.ok(html.includes('kWh/Woche'), 'the unit text is shown');
  assert.ok(!html.includes('kwh_week'), 'the raw unit code is not shown');
});

test('the temperature limit and the knee fact have catalog texts in both languages', () => {
  ['de', 'en'].forEach((lang) => {
    ['automations.learn.temperature.limit.title', 'automations.learn.temperature.limit.line', 'automations.learn.fact.knee', 'automations.learn.group.energy'].forEach((key) => {
      assert.ok(catalogs[lang][key], `${lang}: ${key}`);
    });
  });
});

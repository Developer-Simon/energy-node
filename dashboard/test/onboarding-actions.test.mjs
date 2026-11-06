// Aktions-Engine der Tourschritte in onboarding-actions.js. Die Tests laufen
// gegen ein jsdom-Dokument, das Spotlight ist ein Stub: sichtbar ist alles,
// außer Elemente mit data-hidden.
// Run with `npm test` from dashboard/.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'onboarding-actions.js'),
  'utf8',
);

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  dom.window.obSpotlight = { visibleRect: (el) => (el.hasAttribute('data-hidden') ? null : {}) };
  vm.runInContext(source, dom.getInternalVMContext());
  return { document: dom.window.document, a: dom.window.obActions, win: dom.window };
}

// Ein Lauf wie next() ihn anlegt, hier von Hand.
const runOf = (entries) => ({ entries, records: [], scope: null, takenOver: false, pending: null });
// Die Suche findet sofort oder gar nicht, Wartezeiten braucht der Test nicht.
const waitFor = async (find) => find() || null;

// Ein Schalter, der aria-expanded umschaltet wie ein Akkordeon.
function toggle(el, clicks) {
  el.addEventListener('click', () => {
    clicks.push(el.id);
    el.setAttribute('aria-expanded', el.getAttribute('aria-expanded') === 'true' ? 'false' : 'true');
  });
}

// Eine Reiterleiste, bei der ein Klick den Reiter wählt und die anderen abwählt.
function tabs(tablist, ids) {
  const select = (tab) => {
    for (const t of tablist.querySelectorAll('[role="tab"]')) t.setAttribute('aria-selected', String(t === tab));
  };
  for (const id of ids) {
    tablist.querySelector(`#${id}`).addEventListener('click', (e) => select(e.currentTarget));
  }
}

test('stateOf reads tab, expanded, pressed and details', () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <div role="tablist">
      <button id="t1" role="tab" aria-selected="true"></button>
      <button id="t2" role="tab" aria-selected="false"></button>
    </div>
    <button id="e" aria-expanded="false"></button>
    <button id="p" aria-pressed="true"></button>
    <details id="d" open></details>
    <details id="c"></details>
    <button id="none"></button>`;
  const get = (id) => document.getElementById(id);
  assert.equal(a.stateOf(get('t1')), 'open');
  assert.equal(a.stateOf(get('t2')), 'closed');
  assert.equal(a.stateOf(get('e')), 'closed');
  assert.equal(a.stateOf(get('p')), 'open');
  assert.equal(a.stateOf(get('d')), 'open');
  assert.equal(a.stateOf(get('c')), 'closed');
  assert.equal(a.stateOf(get('none')), null);
});

test('find prefers the scope and falls back to the document', () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <button id="outer" data-tour="x"></button>
    <section id="scope"><button id="inner" data-tour="x"></button></section>
    <section id="empty"></section>
    <section id="hidden-scope"><button id="gone" data-tour="x" data-hidden></button></section>`;
  const get = (id) => document.getElementById(id);
  assert.equal(a.find('x', get('scope')), get('inner'));
  assert.equal(a.find('x', get('empty')), get('outer'));
  assert.equal(a.find('x', get('hidden-scope')), get('outer'), 'a hidden match in the scope is skipped');
  assert.equal(a.find('x', null), get('outer'));
  assert.equal(a.find('missing', null), null);
});

test('sharedPrefix counts equal leading entries', () => {
  const { a } = load();
  assert.equal(a.sharedPrefix(['a', '@b', 'c'], ['a', '@b', 'd']), 2);
  assert.equal(a.sharedPrefix([], ['a']), 0);
  assert.equal(a.sharedPrefix(['a'], ['a', 'b']), 1);
});

test('apply clicks a closed toggle and revert clicks it again', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<button id="x" data-tour="x" aria-expanded="false"></button>';
  const x = document.getElementById('x');
  const clicks = [];
  toggle(x, clicks);
  let shown = 0;
  const run = runOf(['x']);
  const work = a.apply(run, { alive: () => true, show: async () => { shown += 1; }, waitFor });
  assert.ok(run.pending, 'pending is set while the entries run');
  await work;
  assert.equal(run.pending, null, 'pending is cleared afterwards');
  assert.deepEqual(clicks, ['x']);
  assert.equal(shown, 1);
  assert.equal(x.getAttribute('aria-expanded'), 'true');
  assert.equal(run.records.length, 1);
  run.records[0].revert();
  assert.deepEqual(clicks, ['x', 'x']);
  assert.equal(x.getAttribute('aria-expanded'), 'false');
});

test('an element that is already open is not clicked and not reverted', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<button id="x" data-tour="x" aria-expanded="true"></button>';
  const x = document.getElementById('x');
  const clicks = [];
  toggle(x, clicks);
  let shown = 0;
  const run = runOf(['x']);
  await a.apply(run, { alive: () => true, show: async () => { shown += 1; }, waitFor });
  assert.deepEqual(clicks, []);
  assert.equal(shown, 0);
  assert.equal(run.records.length, 1);
  assert.equal(run.records[0].revert, null);
  assert.equal(x.getAttribute('aria-expanded'), 'true');
});

test('a tab is reverted by clicking the tab that was selected before', async () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <div role="tablist" id="list">
      <button id="first" role="tab" aria-selected="true"></button>
      <button id="second" role="tab" data-tour="x" aria-selected="false"></button>
    </div>`;
  const get = (id) => document.getElementById(id);
  tabs(get('list'), ['first', 'second']);
  const run = runOf(['x']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  assert.equal(get('second').getAttribute('aria-selected'), 'true');
  assert.equal(get('first').getAttribute('aria-selected'), 'false');
  run.records[0].revert();
  assert.equal(get('first').getAttribute('aria-selected'), 'true');
  assert.equal(get('second').getAttribute('aria-selected'), 'false');
});

test('revert leaves an element alone whose state changed since', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<button id="x" data-tour="x" aria-expanded="false"></button>';
  const x = document.getElementById('x');
  const clicks = [];
  toggle(x, clicks);
  const run = runOf(['x']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  assert.deepEqual(clicks, ['x']);
  // Der Nutzer klappt selbst wieder zu.
  x.setAttribute('aria-expanded', 'false');
  run.records[0].revert();
  assert.deepEqual(clicks, ['x'], 'no second click');
  assert.equal(x.getAttribute('aria-expanded'), 'false');
});

test('a named action sets the scope for the following entries', async () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <button id="outer" data-tour="x"></button>
    <section id="panel"><button id="inner" data-tour="x"></button></section>`;
  const get = (id) => document.getElementById(id);
  const panel = get('panel');
  a.action('open-panel', { run: async () => ({ el: panel, scope: panel, revert() {} }) });
  const clicks = [];
  for (const id of ['outer', 'inner']) get(id).addEventListener('click', () => clicks.push(id));
  const run = runOf(['@open-panel', 'x']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  assert.deepEqual(clicks, ['inner']);
  assert.equal(run.scope, panel);
  assert.equal(run.records.length, 2);
});

test('apply stops when alive turns false', async () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <button id="a" data-tour="a" aria-pressed="false"></button>
    <button id="b" data-tour="b" aria-pressed="false"></button>`;
  const clicks = [];
  for (const id of ['a', 'b']) document.getElementById(id).addEventListener('click', () => clicks.push(id));
  const run = runOf(['a', 'b']);
  await a.apply(run, { alive: () => clicks.length < 1, show: async () => {}, waitFor });
  assert.deepEqual(clicks, ['a']);
  assert.equal(run.records.length, 1);
});

test('apply stops at an element that is missing', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<button id="b" data-tour="b" aria-pressed="false"></button>';
  const clicks = [];
  document.getElementById('b').addEventListener('click', () => clicks.push('b'));
  const run = runOf(['missing', 'b']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  assert.deepEqual(clicks, []);
  assert.equal(run.records.length, 0);
});

test('next keeps the shared prefix open', async () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <button id="t" data-tour="t" aria-expanded="false"></button>
    <button id="x" data-tour="x" aria-expanded="false"></button>
    <button id="y" data-tour="y" aria-expanded="false"></button>`;
  const get = (id) => document.getElementById(id);
  const clicks = [];
  for (const id of ['t', 'x', 'y']) toggle(get(id), clicks);
  const prev = runOf(['t', 'x']);
  await a.apply(prev, { alive: () => true, show: async () => {}, waitFor });
  const run = await a.next(prev, ['t', 'y']);
  assert.deepEqual(clicks, ['t', 'x', 'x'], 'only x is reverted');
  assert.equal(get('t').getAttribute('aria-expanded'), 'true');
  assert.equal(get('x').getAttribute('aria-expanded'), 'false');
  assert.deepEqual(run.entries, ['t', 'y']);
  assert.equal(run.records.length, 1);
  assert.equal(run.records[0], prev.records[0]);
});

test('next reverts in reverse order', async () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <button id="a" data-tour="a" aria-expanded="false"></button>
    <button id="b" data-tour="b" aria-expanded="false"></button>
    <button id="c" data-tour="c" aria-expanded="false"></button>`;
  const clicks = [];
  for (const id of ['a', 'b', 'c']) toggle(document.getElementById(id), clicks);
  const prev = runOf(['a', 'b', 'c']);
  await a.apply(prev, { alive: () => true, show: async () => {}, waitFor });
  const run = await a.next(prev, []);
  assert.deepEqual(clicks, ['a', 'b', 'c', 'c', 'b', 'a']);
  assert.equal(run.records.length, 0);
});


test('next waits for a running apply before it reverts', async () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <button id="a" data-tour="a" aria-expanded="false"></button>
    <button id="b" data-tour="b" aria-expanded="false"></button>`;
  const clicks = [];
  for (const id of ['a', 'b']) toggle(document.getElementById(id), clicks);
  let release;
  const gate = new Promise((resolve) => { release = resolve; });
  const show = async (el) => { if (el.id === 'b') await gate; };
  const prev = runOf(['a', 'b']);
  const work = a.apply(prev, { alive: () => true, show, waitFor });
  let resolved = false;
  const nextP = a.next(prev, []).then((run) => { resolved = true; return run; });
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(resolved, false, 'next has not resolved while apply waits');
  assert.deepEqual(clicks, ['a'], 'nothing is reverted while apply waits');
  release();
  const run = await nextP;
  await work;
  assert.equal(resolved, true);
  assert.deepEqual(clicks, ['a', 'b', 'b', 'a'], 'all entries reverted in reverse order');
  assert.equal(run.records.length, 0);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.deepEqual(clicks, ['a', 'b', 'b', 'a'], 'no click after next resolved');
});


test('a trusted click outside the dock marks the run as taken over', () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <div class="ob-dock"><button id="d"></button></div>
    <button id="o"></button>`;
  // Der Ignore-Filter behandelt das Ereignis als vertrauenswuerdig: nur das Dock zaehlt.
  const run = runOf(['a']);
  const stop = a.watchTakeover(run, (e) => e.target.closest('.ob-dock'));
  document.getElementById('o').click();
  stop();
  assert.equal(run.takenOver, true);

  // Ohne Filter zaehlt ein nicht vertrauenswuerdiger Klick nicht.
  const untrusted = runOf(['a']);
  const stopDefault = a.watchTakeover(untrusted);
  document.getElementById('o').click();
  stopDefault();
  assert.equal(untrusted.takenOver, false, 'untrusted click with the default ignore does not count');
});

test('next after a takeover reverts nothing and keeps nothing', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<button id="x" data-tour="x" aria-expanded="false"></button>';
  const x = document.getElementById('x');
  const clicks = [];
  toggle(x, clicks);
  const prev = runOf(['x']);
  await a.apply(prev, { alive: () => true, show: async () => {}, waitFor });
  prev.takenOver = true;
  const run = await a.next(prev, ['x']);
  assert.deepEqual(clicks, ['x'], 'no revert click');
  assert.equal(x.getAttribute('aria-expanded'), 'true');
  assert.equal(run.records.length, 0);
  assert.equal(run.takenOver, false);
});

test('clicks inside the dock and synthetic clicks do not count', () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <div class="ob-dock"><button id="d"></button><input id="i"></div>
    <button id="o"></button>`;
  const run = runOf(['a']);
  const stop = a.watchTakeover(run);
  document.getElementById('d').click();
  document.getElementById('i').dispatchEvent(new document.defaultView.Event('input', { bubbles: true }));
  document.getElementById('o').click();
  document.getElementById('o').dispatchEvent(new document.defaultView.Event('change', { bubbles: true }));
  stop();
  assert.equal(run.takenOver, false);
});

// Die Seite der Regeln als Stub: Alpine.$data liefert sie, die Methoden
// protokollieren, was der Lauf aufruft. Regeln stehen als li im DOM.
function rulesPage(document, rules) {
  const page = {
    document: { rules },
    expanded: {},
    editing: {},
    newIds: {},
    undo: 'stale',
    started: [],
    discarded: [],
    startEditing(rule) {
      this.started.push(rule.id);
      this.editing[rule.id] = true;
      this.expanded[rule.id] = true;
    },
    discardEditing(rule) {
      this.discarded.push(rule.id);
      this.editing[rule.id] = false;
    },
    stopEditing(rule) {
      this.editing[rule.id] = false;
    },
    insertEntry(kind, entry) {
      this.document.rules.push(entry);
      this.expanded[entry.id] = true;
      this.editing[entry.id] = true;
      this.newIds[entry.id] = true;
      ruleItem(document, entry.id);
    },
  };
  document.defaultView.Alpine = { $data: () => page };
  document.defaultView.I18n = { t: (key) => (key === 'onboarding.example_rule' ? 'Beispielregel' : key) };
  return page;
}

function ruleItem(document, id) {
  const li = document.createElement('li');
  li.setAttribute('data-rule-id', id);
  li.innerHTML = '<div class="automation-rule-head"></div>';
  document.getElementById('rules').append(li);
  return li;
}

const windowRule = (id) => ({ id, conditions: [{ type: 'time_window', start: '08:00', end: '18:00', weekdays: [] }], actions: [] });

test('time-rule opens the first rule with a time window for editing', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  const first = { id: 'a', conditions: [{ type: 'state' }], actions: [] };
  const second = windowRule('b');
  ruleItem(document, 'a');
  const li = ruleItem(document, 'b');
  const page = rulesPage(document, [first, second]);
  const run = runOf(['@time-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  assert.deepEqual(page.started, ['b']);
  assert.equal(run.scope, li, 'the scope is the li of that rule');
  assert.equal(run.records[0].el, li.querySelector('.automation-rule-head'));
  assert.equal(page.document.rules.length, 2, 'no example rule is added');
});

test('time-rule creates a disabled example rule when no rule has a time window', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  const page = rulesPage(document, [{ id: 'a', conditions: [{ type: 'state' }], actions: [] }]);
  page.undo = 'stale';
  const run = runOf(['@time-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  const created = page.document.rules.find((r) => r.id.startsWith('regel_'));
  assert.ok(created, 'an example rule was inserted');
  assert.equal(created.name, 'Beispielregel');
  assert.equal(created.enabled, false);
  assert.equal(page.undo, null);
  assert.equal(run.records.length, 1);
  assert.equal(run.scope, document.querySelector(`li[data-rule-id="${created.id}"]`));
});

test('time-rule revert removes the example rule', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  const page = rulesPage(document, [{ id: 'a', conditions: [], actions: [] }]);
  const run = runOf(['@time-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  const id = run.records[0].scope.getAttribute('data-rule-id');
  run.records[0].revert();
  assert.deepEqual(page.document.rules.map((r) => r.id), ['a']);
  assert.equal(Object.hasOwn(page.expanded, id), false);
  assert.equal(Object.hasOwn(page.editing, id), false);
  assert.equal(Object.hasOwn(page.newIds, id), false);
});

test('time-rule revert discards editing and collapses only what it opened', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  ruleItem(document, 'b');
  const page = rulesPage(document, [windowRule('b')]);
  page.expanded.b = true;
  page.editing.b = false;
  const run = runOf(['@time-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  assert.equal(page.editing.b, true);
  run.records[0].revert();
  assert.deepEqual(page.discarded, ['b']);
  assert.equal(page.editing.b, false);
  assert.equal(page.expanded.b, true, 'a rule that was open stays open');
});

test('time-rule keeps a changed rule', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  ruleItem(document, 'b');
  const page = rulesPage(document, [windowRule('b')]);
  const run = runOf(['@time-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  page.document.rules[0].repeat_seconds = 60;
  run.records[0].revert();
  assert.equal(page.document.rules.length, 1);
  assert.equal(page.document.rules[0].repeat_seconds, 60);
  assert.deepEqual(page.discarded, []);
});

test('sun-rule opens the first sun window rule and skips a time window', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  const timeOnly = windowRule('a');
  const sun = { id: 'b', conditions: [{ type: 'sun_window', from: 'sunrise', to: 'sunset' }], actions: [] };
  ruleItem(document, 'a');
  const li = ruleItem(document, 'b');
  const page = rulesPage(document, [timeOnly, sun]);
  const run = runOf(['@sun-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  assert.deepEqual(page.started, ['b']);
  assert.equal(run.scope, li);
  assert.equal(page.document.rules.length, 2, 'no example rule is added');
});

test('sun-rule creates a disabled example rule with a sun window when none matches', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  const page = rulesPage(document, [windowRule('a')]);
  const run = runOf(['@sun-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  const created = page.document.rules.find((r) => r.id.startsWith('regel_'));
  assert.ok(created, 'an example rule was inserted');
  assert.equal(created.enabled, false);
  assert.deepEqual(JSON.parse(JSON.stringify(created.conditions)), [{ type: 'sun_window', from: 'sunrise', from_offset_min: 0, to: 'sunset', to_offset_min: 0, weekdays: [] }]);
  assert.equal(run.records.length, 1);
});

test('balance-rule opens the first balance threshold rule', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  const balance = { id: 'c', conditions: [{ type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: 100 }], actions: [] };
  const other = { id: 'd', conditions: [{ type: 'time_window' }], actions: [] };
  ruleItem(document, 'c');
  ruleItem(document, 'd');
  const page = rulesPage(document, [balance, other]);
  const run = runOf(['@balance-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  assert.deepEqual(page.started, ['c']);
  assert.equal(page.document.rules.length, 2, 'no example rule is added');
});

test('balance-rule creates a disabled example rule with a threshold when none matches', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  const page = rulesPage(document, [windowRule('a')]);
  const run = runOf(['@balance-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  const created = page.document.rules.find((r) => r.id.startsWith('regel_'));
  assert.ok(created, 'an example rule was inserted');
  assert.equal(created.enabled, false);
  assert.deepEqual(JSON.parse(JSON.stringify(created.conditions)), [{ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 800, hysteresis: 0, hold_seconds: 300 }]);
  assert.equal(run.records.length, 1);
});

test('open-rule expands the first rule without the editor and revert collapses it', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  const first = windowRule('a');
  const second = { ...windowRule('b'), history_enabled: true };
  const li = ruleItem(document, 'a');
  ruleItem(document, 'b');
  const page = rulesPage(document, [first, second]);
  const run = runOf(['@open-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  assert.deepEqual(page.started, [], 'startEditing is not called');
  assert.equal(page.expanded.a, true);
  assert.equal(page.expanded.b, undefined, 'only the first rule opens');
  assert.equal(page.editing.a, undefined, 'the editor stays off');
  assert.equal(run.scope, li);
  assert.equal(run.records[0].el, li.querySelector('.automation-rule-head'));
  assert.equal(page.document.rules.length, 2, 'no example rule is added');
  run.records[0].revert();
  assert.equal(page.expanded.a, false, 'revert collapses the rule it opened');
  assert.deepEqual(page.discarded, [], 'revert never discards editing');
  assert.deepEqual(page.started, []);
});

test('history-rule prefers a rule with entries and prepares its list', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  ruleItem(document, 'a');
  ruleItem(document, 'b');
  const li = ruleItem(document, 'c');
  const page = rulesPage(document, [windowRule('a'), { ...windowRule('b'), history_enabled: true }, { ...windowRule('c'), history_enabled: true }]);
  page.liveHistory = { c: [{ at: 1 }] };
  page.historyRendered = {};
  const run = runOf(['@history-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  assert.equal(page.expanded.c, true);
  assert.equal(page.expanded.a, undefined);
  assert.equal(page.historyRendered.c, true);
  assert.equal(run.scope, li);
});

test('history-rule falls back to a recording rule and then to the example rule', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  ruleItem(document, 'b');
  const recording = rulesPage(document, [windowRule('a'), { ...windowRule('b'), history_enabled: true }]);
  await a.apply(runOf(['@history-rule']), { alive: () => true, show: async () => {}, waitFor });
  assert.equal(recording.expanded.b, true);
  const none = rulesPage(document, [windowRule('a')]);
  await a.apply(runOf(['@history-rule']), { alive: () => true, show: async () => {}, waitFor: async () => null });
  assert.equal(none.document.rules.length, 1, 'a failed example rule is taken back');
});

test('open-rule keeps a rule expanded that was open before', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  ruleItem(document, 'b');
  const page = rulesPage(document, [{ ...windowRule('b'), history_enabled: true }]);
  page.expanded.b = true;
  const run = runOf(['@open-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  run.records[0].revert();
  assert.equal(page.expanded.b, true);
  assert.deepEqual(page.discarded, []);
});

test('open-rule creates an expanded example rule with the editor off when there is no rule', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  const page = rulesPage(document, []);
  page.undo = 'stale';
  const run = runOf(['@open-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  const created = page.document.rules.find((r) => r.id.startsWith('regel_'));
  assert.ok(created, 'an example rule was inserted');
  assert.equal(created.enabled, false);
  assert.equal(page.editing[created.id], false, 'the editor is off');
  assert.equal(page.expanded[created.id], true);
  assert.equal(page.undo, null);
  assert.deepEqual(page.started, []);
  assert.equal(run.records.length, 1);
  assert.equal(run.scope, document.querySelector(`li[data-rule-id="${created.id}"]`));
});

test('open-rule revert removes the example rule it created', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  const page = rulesPage(document, []);
  const run = runOf(['@open-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  const id = run.records[0].scope.getAttribute('data-rule-id');
  run.records[0].revert();
  assert.deepEqual(page.document.rules.map((r) => r.id), []);
  assert.equal(Object.hasOwn(page.expanded, id), false);
  assert.equal(Object.hasOwn(page.editing, id), false);
  assert.equal(Object.hasOwn(page.newIds, id), false);
});

test('open-rule keeps a changed rule on revert', async () => {
  const { document, a } = load();
  document.body.innerHTML = '<div id="automations-panel"><ul id="rules"></ul></div>';
  ruleItem(document, 'b');
  const page = rulesPage(document, [{ ...windowRule('b'), history_enabled: true }]);
  const run = runOf(['@open-rule']);
  await a.apply(run, { alive: () => true, show: async () => {}, waitFor });
  page.document.rules[0].repeat_seconds = 60;
  run.records[0].revert();
  assert.equal(page.document.rules.length, 1);
  assert.equal(page.document.rules[0].repeat_seconds, 60);
  assert.equal(page.expanded.b, true, 'a changed rule is left alone, so it stays open');
  assert.deepEqual(page.discarded, []);
});

// Die Konfigurationsseite als Stub: Alpine.$data liefert sie. loadConfig fuellt
// das Formular, wenn der Batteriedienst gewaehlt ist, und zaehlt die Aufrufe.
function configPage(document, { configs = ['grid', 'battery_soc_devices'], formFills = true } = {}) {
  const page = {
    configs: configs.map((name) => ({ name })),
    selectedName: 'grid',
    loads: 0,
    async loadConfig() {
      this.loads += 1;
      const form = document.getElementById('schema-form');
      form.replaceChildren();
      if (formFills && this.selectedName === 'battery_soc_devices') form.append(document.createElement('input'));
    },
  };
  document.defaultView.Alpine = { $data: () => page };
  return page;
}

test('battery-soc sets the battery service on the page and reverts the old one', async () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <section id="config-panel"></section>
    <select id="config-name"><option value="grid">Netz</option><option value="battery_soc_devices">Batterie</option></select>
    <div id="schema-form"></div>`;
  const select = document.getElementById('config-name');
  const form = document.getElementById('schema-form');
  const page = configPage(document);
  const rec = await a.named['battery-soc'].run({ waitFor });
  assert.equal(rec.el, select);
  assert.equal(rec.scope, null);
  assert.equal(page.selectedName, 'battery_soc_devices');
  assert.equal(page.loads, 1);
  assert.equal(form.children.length, 1);
  rec.revert();
  assert.equal(page.selectedName, 'grid');
  assert.equal(page.loads, 2);
  assert.equal(form.children.length, 0);
});

test('battery-soc leaves the page alone when the user chose another service since', async () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <section id="config-panel"></section>
    <select id="config-name"><option value="grid">Netz</option></select>
    <div id="schema-form"></div>`;
  const page = configPage(document);
  const rec = await a.named['battery-soc'].run({ waitFor });
  page.selectedName = 'solar';
  rec.revert();
  assert.equal(page.selectedName, 'solar');
  assert.equal(page.loads, 1, 'only the run loaded the form');
});

test('battery-soc returns null without the battery service, the panel or the select', async () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <section id="config-panel"></section>
    <select id="config-name"><option value="grid">Netz</option></select>
    <div id="schema-form"></div>`;
  configPage(document, { configs: ['grid'] });
  assert.equal(await a.named['battery-soc'].run({ waitFor }), null);
  document.body.innerHTML = '<div id="schema-form"></div>';
  assert.equal(await a.named['battery-soc'].run({ waitFor }), null);
  document.body.innerHTML = `
    <select id="config-name"><option value="grid">Netz</option></select>
    <div id="schema-form"></div>`;
  assert.equal(await a.named['battery-soc'].run({ waitFor }), null);
});

test('battery-soc gives up and restores the old service when the form does not appear', async () => {
  const { document, a } = load();
  document.body.innerHTML = `
    <section id="config-panel"></section>
    <select id="config-name"><option value="grid">Netz</option><option value="battery_soc_devices">Batterie</option></select>
    <div id="schema-form"></div>`;
  const page = configPage(document, { formFills: false });
  const rec = await a.named['battery-soc'].run({ waitFor });
  assert.equal(rec, null);
  assert.equal(page.selectedName, 'grid');
  assert.equal(page.loads, 2, 'the revert reloads the old service');
});

// Das Geraetedetail als Stub: der Dialog oeffnet sich beim Klick auf den Ausloeser,
// closeDeviceDetail schliesst ihn und zaehlt die Aufrufe.
function devicePage(document, { opens = true } = {}) {
  const panel = {
    closed: 0,
    closeDeviceDetail() {
      this.closed += 1;
      document.getElementById('device-modal').removeAttribute('open');
    },
  };
  document.defaultView.Alpine = { $data: () => panel };
  document.body.innerHTML = `
    <button class="device-detail-trigger" id="hidden" data-hidden></button>
    <button class="device-detail-trigger" id="trig">Geraet</button>
    <dialog id="device-modal"></dialog>`;
  if (opens) {
    document.getElementById('trig').addEventListener('click', () => {
      document.getElementById('device-modal').setAttribute('open', '');
    });
  }
  return panel;
}

test('device-modal shows the first visible trigger slowly, opens the dialog and closes it on revert', async () => {
  const { document, a } = load();
  const panel = devicePage(document);
  const shows = [];
  const show = async (el, opts) => { shows.push([el.id, opts]); };
  const rec = await a.named['device-modal'].run({ waitFor, show });
  const dialog = document.getElementById('device-modal');
  assert.deepEqual(shows.map(([id]) => id), ['trig']);
  assert.equal(shows[0][1].slow, true);
  assert.equal(rec.el, dialog);
  assert.equal(rec.scope, dialog);
  assert.equal(dialog.hasAttribute('open'), true);
  rec.revert();
  assert.equal(panel.closed, 1);
  assert.equal(dialog.hasAttribute('open'), false);
});

test('device-modal revert does nothing when the dialog was closed by the user', async () => {
  const { document, a } = load();
  const panel = devicePage(document);
  const rec = await a.named['device-modal'].run({ waitFor, show: async () => {} });
  document.getElementById('device-modal').removeAttribute('open');
  rec.revert();
  assert.equal(panel.closed, 0);
});

test('device-modal returns null without a visible trigger', async () => {
  const { document, a } = load();
  const panel = devicePage(document);
  document.getElementById('trig').setAttribute('data-hidden', '');
  const shows = [];
  const rec = await a.named['device-modal'].run({ waitFor, show: async (el) => { shows.push(el.id); } });
  assert.equal(rec, null);
  assert.deepEqual(shows, []);
  assert.equal(panel.closed, 0);
});

test('device-modal returns null when the dialog never opens', async () => {
  const { document, a } = load();
  const panel = devicePage(document, { opens: false });
  const rec = await a.named['device-modal'].run({ waitFor, show: async () => {} });
  assert.equal(rec, null);
  assert.equal(panel.closed, 0, 'nothing open, nothing to close');
});

test('device-modal inside apply shows the dialog once more after it opened', async () => {
  const { document, a } = load();
  devicePage(document);
  const shows = [];
  const run = runOf(['@device-modal']);
  await a.apply(run, { alive: () => true, show: async (el) => { shows.push(el.id); }, waitFor });
  assert.deepEqual(shows, ['trig', 'device-modal']);
  assert.equal(run.scope, document.getElementById('device-modal'));
});

// Ein Toast-Store, der die Aufrufe mitschreibt. push gibt die gegebene id zurueck.
function toastStore(calls, pushId = 7) {
  return {
    push: (...args) => { calls.push(['push', ...args]); return pushId; },
    hold: (...args) => { calls.push(['hold', ...args]); },
    dismiss: (...args) => { calls.push(['dismiss', ...args]); },
  };
}

test('demo-toast pushes a held toast and returns its element', async () => {
  const { document, a, win } = load();
  const calls = [];
  const store = toastStore(calls);
  win.Alpine = { store: (name) => (name === 'toasts' ? store : null) };
  win.I18n = { t: (key) => key };
  document.body.innerHTML = '<div class="notification-toast" id="t"></div>';
  const rec = await a.named['demo-toast'].run({ waitFor, show: async () => {} });
  assert.deepEqual(calls.slice(0, 2), [['push', 'onboarding.demo_toast', 'info'], ['hold', 7, 'tour']]);
  assert.equal(rec.el, document.getElementById('t'));
  assert.equal(rec.scope, null);
  rec.revert();
  assert.deepEqual(calls[calls.length - 1], ['dismiss', 7]);
});

test('demo-toast returns null and does not hold when push gives no id', async () => {
  const { a, win } = load();
  const calls = [];
  win.Alpine = { store: () => toastStore(calls, null) };
  win.I18n = { t: (key) => key };
  const rec = await a.named['demo-toast'].run({ waitFor, show: async () => {} });
  assert.equal(rec, null);
  assert.deepEqual(calls.map((c) => c[0]), ['push']);
});

test('demo-toast dismisses the toast when its element never appears', async () => {
  const { a, win } = load();
  const calls = [];
  win.Alpine = { store: () => toastStore(calls) };
  win.I18n = { t: (key) => key };
  const rec = await a.named['demo-toast'].run({ waitFor: async () => null, show: async () => {} });
  assert.equal(rec, null);
  assert.deepEqual(calls[calls.length - 1], ['dismiss', 7]);
});

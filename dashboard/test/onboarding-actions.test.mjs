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
  return { document: dom.window.document, a: dom.window.obActions };
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

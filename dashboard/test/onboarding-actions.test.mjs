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

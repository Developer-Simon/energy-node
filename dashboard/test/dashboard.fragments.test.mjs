// Der gemeinsame Fragment-Lader (window.dashboardFragments) holt Panels und
// Settings-Unterseiten. Geprueft wird: Tausch (outer/inner), Ankunftsmarkierung,
// einmaliges Laden von Skripten, Fehlerdarstellung, verworfene Antworten und
// dass loadPanel weiterhin ueber den Lader laeuft.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'dashboard.js'),
  'utf8',
);

// Asset-Tags laden in JSDOM nie. Sobald ein Tag an den Kopf gehaengt wird,
// feuert onload asynchron - so verhaelt sich ein sofort geladenes Skript.
function createPage(bodyHtml) {
  const dom = new JSDOM(
    `<!doctype html><html><head></head><body>${bodyHtml}</body></html>`,
    { runScripts: 'outside-only', url: 'http://localhost/' },
  );
  const { window } = dom;
  const head = window.document.head;
  const append = head.append.bind(head);
  head.append = (...nodes) => {
    append(...nodes);
    for (const node of nodes) window.setTimeout(() => node.onload?.(), 0);
  };
  const factories = {};
  window.Alpine = { data: (name, fn) => { factories[name] = fn; }, directive: () => {} };
  window.setInterval = () => 0;
  vm.runInContext(source, dom.getInternalVMContext());
  window.document.dispatchEvent(new window.Event('alpine:init'));
  return { window, factories, loader: window.dashboardFragments };
}

function stubFetch(window, { ok = true, status = 200, body = '' } = {}) {
  window.fetch = async () => ({ ok, status, text: async () => body });
}

test('outer swap replaces the placeholder and marks arrival', async () => {
  const { window, loader } = createPage(
    '<section id="x" aria-busy="true" data-panel-src="/f"><div class="sk-layer"></div><p data-panel-loading>Lade</p></section>',
  );
  stubFetch(window, { body: '<section id="x"><p>ok</p></section>' });
  const container = window.document.getElementById('x');
  await loader.load(container, { src: '/f' });
  const x = window.document.getElementById('x');
  assert.equal(x.querySelector('p').textContent, 'ok');
  assert.ok(x.classList.contains('is-arriving'));
  assert.equal(x.hasAttribute('aria-busy'), false);
});

test('inner swap keeps the container', async () => {
  const { window, loader } = createPage('<section id="y"><div class="sk-layer"></div></section>');
  stubFetch(window, { body: '<p>ok</p>' });
  const container = window.document.getElementById('y');
  const result = await loader.load(container, { src: '/f', swap: 'inner' });
  assert.equal(result, container);
  assert.equal(container.innerHTML, '<p>ok</p>');
  assert.ok(container.classList.contains('is-arriving'));
});

test('each script url loads once across calls', async () => {
  const { window, loader } = createPage('<section id="z"></section>');
  stubFetch(window, { body: '<p>ok</p>' });
  const container = window.document.getElementById('z');
  const options = { src: '/f', script: '/a.js,/b.js', swap: 'inner' };
  await loader.load(container, options);
  await loader.load(container, options);
  const srcs = [...window.document.head.querySelectorAll('script')].map(script => script.getAttribute('src'));
  assert.deepEqual(srcs, ['/a.js', '/b.js']);
});

test('http error shows message and drops skeleton', async () => {
  const { window, loader } = createPage(
    '<section id="e" data-panel-src="/f"><div class="sk-layer"></div><p data-panel-loading>Lade</p></section>',
  );
  stubFetch(window, { ok: false, status: 500 });
  const container = window.document.getElementById('e');
  await assert.rejects(() => loader.load(container, { src: '/f' }));
  assert.equal(window.document.querySelector('#e [data-panel-loading]').textContent, 'panel.load_failed');
  assert.equal(window.document.querySelector('#e .sk-layer'), null);
});

test('stale result is not inserted', async () => {
  const { window, loader } = createPage('<section id="s"><p>before</p></section>');
  stubFetch(window, { body: '<section id="s"><p>late</p></section>' });
  const container = window.document.getElementById('s');
  const result = await loader.load(container, { src: '/f', isCurrent: () => false });
  assert.equal(result, null);
  assert.equal(window.document.getElementById('s'), container);
  assert.equal(container.querySelector('p').textContent, 'before');
});

test('loadPanel still delegates with outer swap', async () => {
  const { window, factories } = createPage(
    '<section id="history-panel" data-panel-src="/history" data-panel-css="/c.css" data-panel-script="/s.js"></section>',
  );
  const calls = [];
  window.dashboardFragments.load = (container, opts) => {
    calls.push({ container, opts });
    return Promise.resolve(null);
  };
  const shell = factories.dashboardShell();
  await shell.loadPanel('history-panel');
  assert.equal(calls.length, 1);
  assert.equal(calls[0].container.id, 'history-panel');
  assert.equal(calls[0].opts.src, '/history');
  assert.equal(calls[0].opts.css, '/c.css');
  assert.equal(calls[0].opts.script, '/s.js');
  assert.equal(calls[0].opts.swap, 'outer');
});

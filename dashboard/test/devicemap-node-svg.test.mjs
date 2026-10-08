import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'devicemap-node-svg.js'), 'utf8');
function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  vm.runInContext(source, dom.getInternalVMContext());
  return { svg: dom.window.DeviceMapNodeSvg, dom };
}
const colorOf = token => `#${token}`;

test('one solid ring segment per role, icon and health dot', () => {
  const { svg, dom } = load();
  const markup = svg.markup({ segments: ['flow-pv', 'flow-battery'], dashed: false, icon: 'pv', iconColor: 'flow-pv', health: 'degraded' }, colorOf);
  const doc = new dom.window.DOMParser().parseFromString(markup, 'image/svg+xml');
  const rings = [...doc.querySelectorAll('circle[data-ring]')];
  assert.equal(rings.length, 2);
  assert.equal(rings[0].getAttribute('stroke'), '#flow-pv');
  assert.equal(rings[1].getAttribute('stroke'), '#flow-battery');
  assert.ok(!rings[0].getAttribute('stroke-dasharray').startsWith('4 3'));
  assert.equal(doc.querySelector('circle[data-health]').getAttribute('fill'), '#warn');
  assert.equal(doc.querySelector('[data-icon]').getAttribute('data-icon'), 'pv');
});

test('heuristic roles are dashed, no roles draw the grey dotted ring', () => {
  const { svg, dom } = load();
  const dashed = new dom.window.DOMParser().parseFromString(svg.markup({ segments: ['flow-pv'], dashed: true, icon: 'pv', iconColor: 'flow-pv', health: 'ok' }, colorOf), 'image/svg+xml');
  assert.equal(dashed.querySelector('circle[data-ring]').getAttribute('stroke-dasharray'), '4 3');
  const empty = new dom.window.DOMParser().parseFromString(svg.markup({ segments: [], dashed: false, icon: 'box', iconColor: 'text-muted', health: 'unknown' }, colorOf), 'image/svg+xml');
  const ring = empty.querySelector('circle[data-ring]');
  assert.equal(ring.getAttribute('stroke'), '#border');
  assert.equal(ring.getAttribute('stroke-dasharray'), '2 4');
  assert.equal(empty.querySelector('circle[data-health]').getAttribute('fill'), '#text-faint');
});

test('dataUri is an encoded svg image', () => {
  const { svg } = load();
  const uri = svg.dataUri({ segments: [], dashed: false, icon: 'box', iconColor: 'text-muted', health: 'ok' }, colorOf);
  assert.match(uri, /^data:image\/svg\+xml;utf8,%3Csvg/);
});

test('a category icon is drawn from the catalogue markup with the device icon stroke', () => {
  const { svg } = load();
  const markup = svg.markup({segments: ['flow-cat-1'], dashed: false, icon: 'cat:mdi:home', iconMarkup: '<path d="M6 11 12 4 18 11"/>', iconColor: 'flow-cat-1', health: 'ok'}, token => `#${token}`);
  assert.match(markup, /<path d="M6 11 12 4 18 11"\/>/);
  assert.match(markup, /stroke-width="1\.6"/);
  assert.match(markup, /#flow-cat-1/);
});

test('a group node is a dashed circle with a sigma', () => {
  const { svg } = load();
  const markup = svg.groupMarkup(token => `#${token}`);
  assert.match(markup, /stroke-dasharray/);
  assert.match(markup, /Σ/);
  assert.match(markup, /#flow-rest/);
  assert.match(svg.groupDataUri(token => `#${token}`), /^data:image\/svg\+xml;utf8,/);
});

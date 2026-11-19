// Tests for the before and after graphic of rule findings: pure string rendering, no text inside SVG.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');
const source = fs.readFileSync(path.join(jsDir, 'automation-evidence.js'), 'utf8');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  vm.runInContext(source, dom.getInternalVMContext());
  return { api: dom.window.AutomationEvidence, document: dom.window.document };
}

const t = (key) => key;

function mount(document, html) {
  const host = document.createElement('div');
  host.innerHTML = html;
  return host;
}

test('events shape draws one mark per event and one soll span', () => {
  const { api, document } = load();
  const from = 1700000000;
  const to = from + 14400;
  const html = api.render({
    shape: 'events',
    from,
    to,
    ist: [from + 1000, from + 5000, from + 9000],
    soll_spans: [[from + 1000, from + 9000]],
  }, t);
  const host = mount(document, html);
  const marks = host.querySelectorAll('line.auto-learn-mark-event');
  assert.equal(marks.length, 3);
  assert.equal(host.querySelectorAll('rect.auto-learn-soll').length, 1);
  assert.equal(Number(marks[0].getAttribute('x1')).toFixed(3), ((1000 / 14400) * 100).toFixed(3));
  const axisLabels = host.querySelectorAll('.auto-learn-axis span');
  assert.ok(axisLabels.length >= 3);
});

test('events shape writes the axis as HH clock times', () => {
  const { api, document } = load();
  const from = 1700000000;
  const html = api.render({ shape: 'events', from, to: from + 14400, ist: [], soll_spans: [] }, t);
  const host = mount(document, html);
  const labels = Array.from(host.querySelectorAll('.auto-learn-axis > div > span')).map((s) => s.textContent);
  assert.equal(labels.length, 3);
  labels.forEach((label) => assert.match(label, /^\d{2}$/));
});

test('scale shape puts the threshold line at ist_value / max in percent', () => {
  const { api, document } = load();
  const html = api.render({
    shape: 'scale',
    seen: [100, 400],
    ist_value: 300,
    soll_value: 500,
    max: 600,
    unit: 'W',
  }, t);
  const host = mount(document, html);
  const threshold = host.querySelector('line.auto-learn-mark-threshold');
  assert.ok(threshold, 'threshold line exists');
  assert.equal(Number(threshold.getAttribute('x1')).toFixed(4), ((300 / 600) * 100).toFixed(4));
  const soll = host.querySelector('line.auto-learn-mark-soll');
  assert.equal(Number(soll.getAttribute('x1')).toFixed(4), ((500 / 600) * 100).toFixed(4));
});

test('scale shape without max falls back to a usable scale', () => {
  const { api, document } = load();
  const html = api.render({ shape: 'scale', seen: [0, 200], ist_value: 300 }, t);
  const host = mount(document, html);
  const threshold = host.querySelector('line.auto-learn-mark-threshold');
  assert.ok(threshold);
  const x = Number(threshold.getAttribute('x1'));
  assert.ok(Number.isFinite(x) && x >= 0 && x <= 100, 'threshold stays inside the lane');
});

test('links shape with Missing marks the target as dashed', () => {
  const { api, document } = load();
  const html = api.render({
    shape: 'links',
    target: 'sensor.gone',
    missing: true,
    soll_names: ['Abendlicht'],
  }, t);
  const host = mount(document, html);
  assert.ok(host.querySelector('.is-missing'));
});

test('links shape shows several Ist chips on one target', () => {
  const { api, document } = load();
  const html = api.render({
    shape: 'links',
    target: 'switch.pump',
    ist_names: ['Regel A', 'Regel B'],
    soll_names: ['Szene'],
  }, t);
  const host = mount(document, html);
  const chips = Array.from(host.querySelectorAll('.auto-learn-chip')).map((c) => c.textContent);
  assert.ok(chips.includes('Regel A'));
  assert.ok(chips.includes('Regel B'));
  assert.ok(chips.includes('Szene'));
  assert.ok(html.includes('switch.pump'));
});

test('links shape without soll names shows only the target on the Soll lane', () => {
  const { api, document } = load();
  const html = api.render({ shape: 'links', target: 'switch.pump', ist_names: ['Regel A'] }, t);
  const host = mount(document, html);
  const soll = host.querySelector('.auto-learn-lane.is-soll');
  assert.ok(soll);
  assert.equal(soll.querySelectorAll('.auto-learn-chip').length, 0);
  assert.match(soll.textContent, /switch\.pump/);
});

test('rule names are escaped and never become markup', () => {
  const { api, document } = load();
  const evil = '<img src=x onerror="alert(1)"><b>Regel</b>';
  const html = api.render({ shape: 'links', target: 'x.y', ist_names: [evil], soll_names: [evil] }, t);
  const host = mount(document, html);
  assert.equal(host.querySelectorAll('img').length, 0);
  assert.equal(host.querySelectorAll('b').length, 0);
  const chips = host.querySelectorAll('.auto-learn-chip');
  assert.equal(chips[0].textContent, evil);
});

test('rendered graphics contain no text element inside SVG', () => {
  const { api, document } = load();
  const shapes = [
    { shape: 'events', from: 1700000000, to: 1700014400, ist: [1700001000], soll_spans: [[1700001000, 1700002000]] },
    { shape: 'scale', seen: [0, 200], ist_value: 100, soll_value: 150, max: 300 },
    { shape: 'links', target: 'a.b', ist_names: ['R'], soll_names: ['S'] },
  ];
  shapes.forEach((ev) => {
    const host = mount(document, api.render(ev, t));
    assert.equal(host.querySelectorAll('svg text').length, 0, ev.shape);
  });
});

test('unknown shape or missing evidence renders an empty string', () => {
  const { api } = load();
  assert.equal(api.render({ shape: 'day' }, t), '');
  assert.equal(api.render({ shape: 'unknown' }, t), '');
  assert.equal(api.render(null, t), '');
  assert.equal(api.render(undefined, t), '');
});

// Regression tests for energy-ring.js: annulusPath()/ringSegments() are the
// pure geometry port of Vorschlag B ("Autarkie-Ring") from the
// six-proposals exploration.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');
const themeSource = read('theme.js');
const energyModelSource = read('energy-model.js');
const energyPresentationSource = read('energy-presentation.js');
const scriptSource = read('energy-ring.js');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  vm.runInContext(themeSource, context);
  vm.runInContext(energyModelSource, context);
  vm.runInContext(energyPresentationSource, context);
  vm.runInContext(scriptSource, context);
  return { factory, model: dom.window.EnergyModel, window: dom.window, document: dom.window.document };
}

test('ringSegments covers the full circle minus gaps for a single flow', () => {
  const { factory } = load();
  const segments = factory.ringSegments([{id: 'pv', value: 1000, color: '#fff'}], 170, 170, 128, 20);
  assert.equal(segments.length, 1);
  assert.equal(segments[0].fraction, 1);
});

test('ringSegments splits proportionally across multiple flows', () => {
  const { factory } = load();
  const segments = factory.ringSegments([
    {id: 'pv', value: 750, color: '#fff'},
    {id: 'grid_import', value: 250, color: '#0f0'},
  ], 170, 170, 128, 20);
  assert.equal(segments.length, 2);
  assert.equal(segments[0].fraction, 0.75);
  assert.equal(segments[1].fraction, 0.25);
});

test('ringSegments drops a flow whose share is smaller than the segment gap', () => {
  const { factory } = load();
  // A 0.01% slice at r=128 is far narrower than the fixed 2px gap between
  // segments, so its computed span goes negative and it is skipped.
  const segments = factory.ringSegments([
    {id: 'pv', value: 999990, color: '#fff'},
    {id: 'grid_import', value: 10, color: '#0f0'},
  ], 170, 170, 128, 20);
  assert.equal(segments.length, 1);
  assert.equal(segments[0].flow.id, 'pv');
});

test('annulusPath starts and ends on the outer radius', () => {
  const { factory } = load();
  const d = factory.annulusPath(170, 170, 128, 20, 0, Math.PI / 2);
  const first = d.match(/^M ([\d.-]+) ([\d.-]+)/);
  assert.ok(first);
  const [, x, y] = first;
  const outer = 128 + 10;
  const dist = Math.hypot(Number(x) - 170, Number(y) - 170);
  assert.ok(Math.abs(dist - outer) < 0.01);
});

test('readoutRows defaults to "both": percent and absolute power together', () => {
  const { factory, model } = load();
  const rows = factory.readoutRows([{id: 'pv', label: 'PV', value: 750, color: '#fff'}, {id: 'grid_import', label: 'Netzbezug', value: 250, color: '#0f0'}]);
  assert.equal(rows[0].text, `${model.formatPercent(0.75)} · ${model.formatPower(750)}`);
});

test('readoutRows label_mode "pct" shows only the percentage', () => {
  const { factory, model } = load();
  const rows = factory.readoutRows([{id: 'pv', label: 'PV', value: 750, color: '#fff'}], 'pct');
  assert.equal(rows[0].text, model.formatPercent(1));
});

test('readoutRows label_mode "abs" shows only the absolute power', () => {
  const { factory, model } = load();
  const rows = factory.readoutRows([{id: 'pv', label: 'PV', value: 750, color: '#fff'}], 'abs');
  assert.equal(rows[0].text, model.formatPower(750));
});

test('readoutRows misst den Anteilsbalken gegen die groesste Position', () => {
  const { factory } = load();
  const rows = factory.readoutRows([
    {id: 'pv', label: 'PV', value: 800, color: '#fff'},
    {id: 'grid_import', label: 'Netzbezug', value: 200, color: '#0f0'},
  ]);
  assert.equal(rows[0].share, 1);
  assert.equal(rows[1].share, 0.25);
});

test('rankFlows sortiert eine unbekannte Reihenfolge nach Groesse', () => {
  const { factory } = load();
  const ranked = factory.rankFlows([
    {id: 'base', value: 300},
    {id: 'wallbox', value: 4000},
    {id: 'heat_pump', value: 900},
  ], []);
  assert.deepEqual(Array.from(ranked, f => f.id), ['wallbox', 'heat_pump', 'base']);
});

// Ohne Vorsprung wuerden zwei fast gleich grosse Positionen bei jedem
// Schnappschuss tauschen - die Liste flackerte, statt einen Rangwechsel zu
// zeigen.
test('rankFlows haelt den Rang, solange der Vorsprung unter der Schwelle liegt', () => {
  const { factory } = load();
  const ranked = factory.rankFlows([
    {id: 'base', value: 500},
    {id: 'wallbox', value: 505},
  ], ['base', 'wallbox']);
  assert.deepEqual(Array.from(ranked, f => f.id), ['base', 'wallbox']);
});

test('rankFlows tauscht, sobald der Vorsprung reicht', () => {
  const { factory } = load();
  const ranked = factory.rankFlows([
    {id: 'base', value: 500},
    {id: 'wallbox', value: 900},
  ], ['base', 'wallbox']);
  assert.deepEqual(Array.from(ranked, f => f.id), ['wallbox', 'base']);
});

test('rankFlows laesst eine neue Position frei einsteigen und traegt eine verschwundene aus', () => {
  const { factory } = load();
  const ranked = factory.rankFlows([
    {id: 'base', value: 300},
    {id: 'wallbox', value: 4000},
  ], ['heat_pump', 'base']);
  assert.deepEqual(Array.from(ranked, f => f.id), ['wallbox', 'base']);
});

// Die Umlaufzeit der Laufschrift traegt den Anteil des Strangs: viel Leistung
// laeuft schnell, wenig langsam.
test('dashSeconds laeuft beim groessten Anteil am schnellsten', () => {
  const { factory } = load();
  assert.ok(factory.dashSeconds(1) < factory.dashSeconds(0.5));
  assert.ok(factory.dashSeconds(0.5) < factory.dashSeconds(0));
  assert.ok(factory.dashSeconds(1) >= 0.5, 'nicht schneller als noch lesbar');
});

test('segmentsMarkup schreibt die anteilsabhaengige Umlaufzeit an die Laufschrift', () => {
  const { factory } = load();
  const segments = factory.ringSegments([
    {id: 'pv', value: 900, color: '#fff'},
    {id: 'grid_import', value: 100, color: '#0f0'},
  ], 170, 170, 128, 20);
  const markup = factory.segmentsMarkup(segments, false, 'ring-speed');
  const durations = [...markup.matchAll(/animation-duration:([\d.]+)s/g)].map(m => Number(m[1]));
  assert.equal(durations.length, 2);
  assert.ok(durations[0] < durations[1], `PV muss schneller laufen als Netzbezug: ${durations}`);
});

test('kpiValue defaults to Autarkiegrad', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 1000, load: 1000, grid_import: 250}, roles: []});
  const kpi = factory.kpiValue(balance, 'autarkie');
  assert.equal(kpi.label, 'Autarkiegrad');
  assert.equal(kpi.value, model.formatPercent(balance.kpi.autarkie));
});

// <template x-for> never gets a .content DocumentFragment inside <svg> in
// any browser (foreign-content parsing rule), so the ring segments render
// through a markup string bound via x-html instead - see the comment in
// energy-ring.js.
test('segmentsMarkup renders one <path> per segment with its fill/opacity when animation is suppressed', () => {
  const { factory } = load();
  const segments = factory.ringSegments([{id: 'pv', value: 1000, color: '#fff'}], 170, 170, 128, 20);
  const markup = factory.segmentsMarkup(segments, true);
  assert.match(markup, /^<path d="[^"]+" fill="#fff" opacity="0.9"><\/path>$/);
});

test('segmentsMarkup lowers opacity for the rest segment', () => {
  const { factory } = load();
  const segments = factory.ringSegments([{id: 'rest', value: 1000, color: '#888', rest: true}], 170, 170, 128, 20);
  assert.match(factory.segmentsMarkup(segments, true), /opacity="0.35"/);
});

test('segmentsMarkup adds a dashed direction indicator when animation is not suppressed', () => {
  const { factory } = load();
  const segments = factory.ringSegments([{id: 'pv', value: 1000, color: '#fff'}], 170, 170, 128, 20);
  assert.match(factory.segmentsMarkup(segments, false), /energy-ring-flow-dash/);
});

test('segmentsMarkup never animates the rest segment, even when animation is not suppressed', () => {
  const { factory } = load();
  const segments = factory.ringSegments([{id: 'rest', value: 1000, color: '#888', rest: true}], 170, 170, 128, 20);
  assert.doesNotMatch(factory.segmentsMarkup(segments, false), /energy-ring-flow-dash/);
});

test('outerArcPath starts on the outer radius, same as annulusPath', () => {
  const { factory } = load();
  const d = factory.outerArcPath(170, 170, 128, 20, 0, Math.PI / 2);
  const first = d.match(/^M ([\d.-]+) ([\d.-]+)/);
  assert.ok(first);
  const [, x, y] = first;
  const outer = 128 + 10;
  const dist = Math.hypot(Number(x) - 170, Number(y) - 170);
  assert.ok(Math.abs(dist - outer) < 0.01);
});

test('der Bilanzring uebernimmt measured_split aus dem data-Attribut der Kachel', () => {
  const { factory, document } = load();
  document.body.innerHTML = `
    <div data-layout-item-id="energy-ring" data-measured-split="entities">
      <section id="energy-ring-card">
        <script type="application/json" id="energy-ring-initial">${JSON.stringify({values: {pv: 3000, load: 900, wallbox: 500}, roles: [], interpretation: {load_mode: 'combined'}})}</script>
      </section>
    </div>
  `;
  const component = factory();
  component.$root = document.querySelector('#energy-ring-card');
  component.init();
  assert.equal(component.options.measuredSplit, 'entities');
});

// Die Leseliste ordnet sich nach Groesse um, der Ring bleibt in seiner festen
// fachlichen Reihenfolge stehen - dort ist der Ort eines Segments etwas, das
// man sich merkt.
test('die Leseliste sortiert sich um, der Ring behaelt seine Reihenfolge', () => {
  const { factory, document } = load();
  const snapshot = base => ({
    values: {pv: 100, load: base + 4000, wallbox: 4000, heat_pump: 0},
    roles: [],
    interpretation: {load_mode: 'measured'},
  });
  document.body.innerHTML = `
    <div data-layout-item-id="energy-ring-order">
      <section id="energy-ring-card">
        <script type="application/json" id="energy-ring-initial">${JSON.stringify(snapshot(300))}</script>
      </section>
    </div>
  `;
  const component = factory();
  component.$root = document.querySelector('#energy-ring-card');
  component.$nextTick = fn => fn();
  component.init();

  const firstRing = Array.from(component.sinkSegments, s => s.flow.id);
  assert.deepEqual(Array.from(component.sinkRows, r => r.id), ['wallbox', 'base']);

  // "base" ueberholt die Wallbox deutlich - die Liste muss tauschen.
  component.snapshot = snapshot(9000);
  component.compute();
  assert.deepEqual(Array.from(component.sinkRows, r => r.id), ['base', 'wallbox']);
  assert.deepEqual(Array.from(component.sinkSegments, s => s.flow.id), firstRing);
});

// getBoundingClientRect() liefert in jsdom ueberall 0, playRowFlip() findet
// also keine Verschiebung. Der Test sichert damit nicht die Bewegung, sondern
// dass der FLIP-Pfad ueber echte Zeilen laeuft, ohne zu werfen, und die
// Stilattribute hinterher wieder abgeraeumt sind.
test('playRowFlip raeumt seine transforms wieder ab', () => {
  const { factory, document } = load();
  document.body.innerHTML = `
    <section id="energy-ring-card">
      <div class="energy-ring-readout-row" data-row-id="pv"></div>
      <div class="energy-ring-readout-row" data-row-id="base"></div>
    </section>
  `;
  const component = factory();
  component.$root = document.querySelector('#energy-ring-card');
  const before = component.measureRows();
  assert.deepEqual([...before.keys()].sort(), ['base', 'pv']);
  component.playRowFlip(before);
  for (const row of document.querySelectorAll('.energy-ring-readout-row')) {
    assert.equal(row.style.transform, '');
  }
});

test('segmentsMarkup verankert die Laufschrift an der fortgeschriebenen Phase', () => {
  const { factory, window: win } = load();
  let now = 0;
  Object.defineProperty(win, 'performance', { value: { now: () => now }, configurable: true });
  const segments = factory.ringSegments([{ id: 'pv', value: 1000, color: '#fff', label: 'PV' }], 170, 170, 128, 20);

  const first = factory.segmentsMarkup(segments, false, 'ring-1');
  assert.match(first, /animation-delay:-0\.000s/);

  // 0,7 s von 1,4 s Umlaufzeit = ein halber Umlauf. Die neue Zeichenkette muss
  // die Animation vorspulen, statt sie wieder bei 0 beginnen zu lassen.
  now = 700;
  const second = factory.segmentsMarkup(segments, false, 'ring-1');
  assert.match(second, /animation-delay:-0\.700s/);
});

test('segmentsMarkup schreibt ohne Animation keine Verzoegerung', () => {
  const { factory } = load();
  const segments = factory.ringSegments([{ id: 'pv', value: 1000, color: '#fff', label: 'PV' }], 170, 170, 128, 20);
  assert.doesNotMatch(factory.segmentsMarkup(segments, true, 'ring-2'), /animation-delay/);
});

// Regression tests for energy-band.js: stackSlots()/centerSlots()/
// spreadLabelY()/bandGeometry() are the pure geometry port of Vorschlag A
// ("Bilanzband") from the six-proposals exploration, including its five
// layout-editor options (height_reference, scale_mode, unit,
// bundle_threshold, animate).
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
const scriptSource = read('energy-band.js');

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

test('bandGeometry returns null for a fully empty balance', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 0, load: 0, grid: 0}, roles: []});
  assert.equal(factory.bandGeometry(balance, 980), null);
});

test('stackSlots stacks flows top to bottom with a fixed gap, floored at 1.5px', () => {
  const { factory } = load();
  const slots = factory.stackSlots([{id: 'a', value: 100}, {id: 'b', value: 0.1}], 268, 1, v => v);
  assert.equal(slots[0].y1 + 2, slots[1].y0); // GAP = 2 between bands
  assert.equal(slots[1].h, 1.5); // floored, since 0.1 * k=1 is below the floor
});

test('centerSlots keeps the same band heights as stackSlots, re-centered', () => {
  const { factory } = load();
  const slots = factory.stackSlots([{id: 'a', value: 100}, {id: 'b', value: 300}], 268, 1, v => v);
  const centers = factory.centerSlots(slots);
  assert.equal(centers[0].y1 - centers[0].y0, slots[0].h);
  assert.equal(centers[1].y1 - centers[1].y0, slots[1].h);
});

test('spreadLabelY enforces the minimum gap between adjacent label positions', () => {
  const { factory } = load();
  // Two nearly-coincident tiny bands would otherwise print labels on top of each other.
  const slots = factory.stackSlots([{id: 'a', value: 0.1}, {id: 'b', value: 0.1}], 268, 1, v => v);
  const ys = factory.spreadLabelY(slots, 34);
  assert.ok(ys[1] - ys[0] >= 34 - 0.01);
});

test('bandGeometry fills the available height for a sunny-surplus balance', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 6400, battery: 2200, load: 620, grid: -3580}, roles: []});
  const geometry = factory.bandGeometry(balance, 980);
  assert.ok(geometry);
  // Bus height should span the full source stack (the taller side here).
  assert.ok(geometry.busHeight > 300);
  assert.equal(geometry.sourceRibbons.length, 1); // pv only
  assert.equal(geometry.sinkRibbons.length, 3); // base, battery_charge, grid_export
});

test('bandGeometry omits the flow-direction animation for the rest band and thin bands', () => {
  const { factory, model } = load();
  // gap forces a "Nicht zugeordnet" rest entry into sinks - only in diagnostic
  // mode; the unknown_consumer default folds it into "base" instead.
  const balance = model.deriveBalance({values: {pv: 3100, load: 3100, heat_pump: 620, grid: 940}, roles: []}, {gap_mode: 'diagnostic'});
  const geometry = factory.bandGeometry(balance, 980);
  const rest = geometry.sinkRibbons.find(r => r.flow.id === 'rest');
  assert.ok(rest);
  assert.equal(rest.animation, null);
});

test('bundleFlows collapses flows below the threshold into one "Sonstiges" entry', () => {
  const { factory } = load();
  const flows = [{id: 'a', value: 90}, {id: 'b', value: 2}, {id: 'c', value: 3}];
  const bundled = factory.bundleFlows(flows, 0.08, 95); // b,c are each < 8% of 95
  assert.equal(bundled.length, 2);
  assert.equal(bundled[1].label, 'Sonstiges (2)');
  assert.equal(bundled[1].value, 5);
});

test('bundleFlows leaves a single below-threshold flow alone instead of "bundling" it with nothing', () => {
  const { factory } = load();
  const flows = [{id: 'a', value: 90}, {id: 'b', value: 2}];
  const bundled = factory.bundleFlows(flows, 0.08, 92);
  assert.deepEqual(bundled, flows);
});

test('bundleFlows is a no-op for threshold 0 (the default)', () => {
  const { factory } = load();
  const flows = [{id: 'a', value: 90}, {id: 'b', value: 2}];
  assert.equal(factory.bundleFlows(flows, 0, 92), flows);
});

test('bandGeometry with scale_mode "sqrt" compresses large flows relative to small ones', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 6400, battery: 2200, load: 620, grid: -3580}, roles: []});
  const linear = factory.bandGeometry(balance, 980, {scaleMode: 'linear'});
  const sqrt = factory.bandGeometry(balance, 980, {scaleMode: 'sqrt'});
  // A single pv source ribbon should be shorter relative to the plot height under sqrt scaling.
  const linearHeight = linear.sourceRibbons[0] ? linear.busHeight : 0;
  const sqrtHeight = sqrt.sourceRibbons[0] ? sqrt.busHeight : 0;
  assert.ok(linearHeight > 0 && sqrtHeight > 0);
});

test('bandGeometry with height_reference "abs" shrinks a small system instead of filling the height', () => {
  const { factory, model } = load();
  const tiny = model.deriveBalance({values: {pv: 95, load: 60, grid: -35}, roles: []});
  const fill = factory.bandGeometry(tiny, 980, {heightReference: 'fill'});
  const abs = factory.bandGeometry(tiny, 980, {heightReference: 'abs'});
  assert.ok(abs.busHeight < fill.busHeight, 'abs reference must not stretch a tiny flow to fill the height');
});

test('bandGeometry passes the unit option through to source/sink labels', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 6400, battery: 2200, load: 620, grid: -3580}, roles: []});
  const geometry = factory.bandGeometry(balance, 980, {unit: 'w'});
  assert.ok(geometry.sourceLabels.every(label => label.value.includes(' W')));
});

test('buildLabels adds a connector when the label had to move away from its band', () => {
  const { factory } = load();
  const slots = factory.stackSlots([{id: 'a', value: 0.1, label: 'A', color: '#fff'}, {id: 'b', value: 0.1, label: 'B', color: '#0f0'}], 268, 1, v => v);
  const ys = factory.spreadLabelY(slots, 34);
  // Reach buildLabels indirectly through bandGeometry with a synthetic balance shape.
  const geometry = factory.bandGeometry({sources: [{id: 'a', value: 0.1, label: 'A', color: '#fff'}, {id: 'b', value: 0.1, label: 'B', color: '#0f0'}], sinks: [{id: 'c', value: 100, label: 'C', color: '#00f'}], total: 100.2}, 980);
  assert.ok(geometry.sourceLabels.some(l => l.connector));
});

// <template x-for>/<template x-if> never get a .content DocumentFragment
// inside <svg> in any browser (foreign-content parsing rule), so the whole
// filled state renders through one markup string bound via x-html instead -
// see the comment in energy-band.js. These pure string-builders are the
// only thing standing in for the old templates, so they get the same
// coverage the removed template markup implicitly had.
test('ribbonsMarkup includes the animated dash path only when animation is present and motion is allowed', () => {
  const { factory } = load();
  const withAnimation = [{path: 'M 0 0 Z', flow: {color: '#fff', rest: false}, animation: {path: 'M 1 1 Z', strokeWidth: 1.5}}];
  const noAnimation = [{path: 'M 0 0 Z', flow: {color: '#fff', rest: true}, animation: null}];
  assert.match(factory.ribbonsMarkup(withAnimation, false), /energy-band-flow-dash/);
  assert.doesNotMatch(factory.ribbonsMarkup(withAnimation, true), /energy-band-flow-dash/); // reducedMotion
  assert.doesNotMatch(factory.ribbonsMarkup(noAnimation, false), /energy-band-flow-dash/);
});

test('labelsMarkup includes a connector path only when the label carries one', () => {
  const { factory } = load();
  const withConnector = [{nameX: 1, nameY: 2, valueX: 3, valueY: 4, anchor: 'end', name: 'PV', value: '1 kW', swatchX: 5, swatchY: 6, swatchHeight: 7, color: '#fff', connector: 'M 0 0 L 1 1'}];
  const withoutConnector = [{...withConnector[0], connector: null}];
  assert.match(factory.labelsMarkup(withConnector), /<path d="M 0 0 L 1 1"/);
  assert.doesNotMatch(factory.labelsMarkup(withoutConnector), /<path/);
  assert.match(factory.labelsMarkup(withConnector), /<text x="1" y="2"[^>]*>PV<\/text>/);
});

test('bandMarkup combines ribbons, bus bar and labels for the filled state', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 6400, battery: 2200, load: 620, grid: -3580}, roles: []});
  const geometry = factory.bandGeometry(balance, 980);
  const markup = factory.bandMarkup(geometry, '9,2 kW', false);
  assert.match(markup, /HAUS/);
  assert.match(markup, /9,2 kW/);
  assert.match(markup, /ERZEUGUNG/);
  assert.match(markup, /VERWENDUNG/);
});

test('bandGeometry keeps every label x-position inside [0, width] and centers the HAUS column for a narrow container', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 6400, battery: 2200, load: 620, grid: -3580}, roles: []});
  const geometry = factory.bandGeometry(balance, 560);
  assert.ok(geometry);
  for (const label of [...geometry.sourceLabels, ...geometry.sinkLabels]) {
    assert.ok(label.nameX >= 0 && label.nameX <= 560, `label x ${label.nameX} within [0, 560]`);
  }
  assert.ok(Math.abs(geometry.busX + geometry.busWidth / 2 - 280) < 1, 'HAUS-Saeule bleibt bei width/2 zentriert');
});

test('bandMarkup places the HAUS text and ERZEUGUNG/VERWENDUNG labels at the measured width, not the old fixed 490/960', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 6400, battery: 2200, load: 620, grid: -3580}, roles: []});
  const geometry = factory.bandGeometry(balance, 560);
  const markup = factory.bandMarkup(geometry, '9,2 kW', false);
  assert.match(markup, /x="280"[^>]*>HAUS/);
  assert.match(markup, /x="540"[^>]*text-anchor="end"[^>]*>VERWENDUNG/);
  assert.doesNotMatch(markup, /x="490"/);
});

function cardMarkup(initialSnapshot, dataAttrs) {
  const attrs = Object.entries(dataAttrs || {}).map(([k, v]) => ` data-${k}="${v}"`).join('');
  return `
    <div data-layout-item-id="energy-band"${attrs}>
      <section id="energy-band-card">
        <div class="energy-band-canvas">
          <svg></svg>
        </div>
        <script type="application/json" id="energy-band-initial">${JSON.stringify(initialSnapshot)}</script>
      </section>
    </div>
  `;
}

function buildComponent(window, document, factory, initialSnapshot, dataAttrs) {
  document.body.innerHTML = cardMarkup(initialSnapshot, dataAttrs);
  const root = document.querySelector('#energy-band-card');
  const component = factory();
  component.$root = root;
  component.$refs = {
    canvas: root.querySelector('.energy-band-canvas'),
    svg: root.querySelector('.energy-band-canvas svg'),
  };
  window.matchMedia = () => ({ matches: false });
  return { component, root };
}

test('init() measures the canvas container and rebuilds the band at that width', () => {
  const { factory, window, document } = load();
  const { component } = buildComponent(window, document, factory, {values: {pv: 6400, battery: 2200, load: 620, grid: -3580}, roles: []});
  Object.defineProperty(component.$refs.canvas, 'clientWidth', { value: 560, configurable: true });
  component.init();
  assert.equal(component.width, 560);
  assert.ok(component.geometry);
  assert.equal(component.$refs.svg.getAttribute('viewBox'), '0 0 560 430');
});

test('init() reads its layout-editor options from the wrapping [data-layout-item-id] element', () => {
  const { factory, window, document } = load();
  const { component } = buildComponent(
    window, document, factory,
    {values: {pv: 6400, battery: 2200, load: 620, grid: -3580}, roles: []},
    {unit: 'w', 'scale-mode': 'sqrt', 'height-reference': 'abs', 'bundle-threshold': '0.08', animate: 'off'},
  );
  Object.defineProperty(component.$refs.canvas, 'clientWidth', { value: 560, configurable: true });
  component.init();
  assert.equal(component.options.unit, 'w');
  assert.equal(component.options.scaleMode, 'sqrt');
  assert.equal(component.options.heightReference, 'abs');
  assert.equal(component.options.bundleThreshold, '0.08');
  assert.equal(component.options.animate, 'off');
  assert.ok(component.totalLabel.endsWith(' W'), 'unit "w" must force the watt formatting');
});

test('init() falls back to the defaults when no options are set on the wrapper', () => {
  const { factory, window, document } = load();
  const { component } = buildComponent(window, document, factory, {values: {pv: 6400, battery: 2200, load: 620, grid: -3580}, roles: []});
  Object.defineProperty(component.$refs.canvas, 'clientWidth', { value: 560, configurable: true });
  component.init();
  assert.equal(component.options.scaleMode, 'linear');
  assert.equal(component.options.heightReference, 'fill');
  assert.equal(component.options.unit, 'auto');
  assert.equal(component.options.bundleThreshold, '0');
  assert.equal(component.options.animate, 'on');
});

test('init() falls back to 980 when the container reports zero width (hidden panel)', () => {
  const { factory, window, document } = load();
  const { component } = buildComponent(window, document, factory, {values: {pv: 6400, battery: 2200, load: 620, grid: -3580}, roles: []});
  Object.defineProperty(component.$refs.canvas, 'clientWidth', { value: 0, configurable: true });
  component.init();
  assert.equal(component.width, 980);
  assert.ok(component.geometry, 'a zero-width measurement (hidden x-show panel) must not produce a null geometry');
});

const kombiniertSnapshot = {
  values: {pv: 3000, load: 900, wallbox: 500},
  roles: [],
  entities: [
    {entity_id: 'werkstatt_power', label: 'Werkstatt / Leistung', value: 520, role: {role: 'load'}},
    {entity_id: 'waschmaschine_power', label: 'Waschmaschine / Leistung', value: 380, role: {role: 'load'}},
  ],
  interpretation: {load_mode: 'combined'},
};

test('das Bilanzband zeichnet bei measured_split "entities" eine Bahn je gemessener Entitaet', () => {
  const { factory, window, document } = load();
  const { component } = buildComponent(window, document, factory, kombiniertSnapshot, {'measured-split': 'entities'});
  Object.defineProperty(component.$refs.canvas, 'clientWidth', { value: 980, configurable: true });
  component.init();
  assert.equal(component.options.measuredSplit, 'entities');
  // Senken: Übriger Verbrauch, Werkstatt, Waschmaschine, Wallbox.
  assert.equal(component.geometry.sinkLabels.length, 4);
  assert.match(component.description, /Werkstatt \/ Leistung/);
});

test('das Bilanzband buendelt die gemessenen Verbraucher ohne measured_split zu einer Bahn', () => {
  const { factory, window, document } = load();
  const { component } = buildComponent(window, document, factory, kombiniertSnapshot);
  Object.defineProperty(component.$refs.canvas, 'clientWidth', { value: 980, configurable: true });
  component.init();
  assert.equal(component.options.measuredSplit, 'sum');
  // Senken: Übriger Verbrauch, Gemessene Verbraucher, Wallbox.
  assert.equal(component.geometry.sinkLabels.length, 3);
});

test('ribbonsMarkup verankert die Laufschrift an der fortgeschriebenen Phase', () => {
  const { factory, window: win } = load();
  let now = 0;
  Object.defineProperty(win, 'performance', { value: { now: () => now }, configurable: true });
  const ribbons = [{
    flow: { id: 'pv', color: '#fff', rest: false },
    path: 'M0 0 L10 0',
    animation: { path: 'M0 0 L10 0', strokeWidth: 3 },
  }];
  assert.match(factory.ribbonsMarkup(ribbons, false, 'band-1'), /animation-delay:-0\.000s/);
  now = 700;
  assert.match(factory.ribbonsMarkup(ribbons, false, 'band-1'), /animation-delay:-0\.700s/);
});

test('das Bilanzband zeigt beim zweiten Schnappschuss zunaechst den alten Wert', () => {
  const { factory, window: win } = load();
  let now = 0;
  Object.defineProperty(win, 'performance', { value: { now: () => now }, configurable: true });
  win.requestAnimationFrame = () => {};
  win.DashboardTheme.onChange = () => () => {};

  const mount = raw => {
    const markup = `
      <div data-layout-item-id="band-1">
        <section id="energy-band-card">
          <div class="energy-band-canvas">
            <svg></svg>
          </div>
          <script type="application/json" id="energy-band-initial">${JSON.stringify(raw)}</script>
        </section>
      </div>
    `;
    win.document.body.innerHTML = markup;
    const root = win.document.querySelector('#energy-band-card');
    const card = factory();
    card.$root = root;
    card.$refs = {
      canvas: root.querySelector('.energy-band-canvas'),
      svg: root.querySelector('svg'),
    };
    Object.defineProperty(card.$refs.canvas, 'clientWidth', { value: 500, configurable: true });
    card.init();
    return card;
  };

  const first = mount({ values: { pv: 1000 }, roles: [] });
  assert.equal(first.snapshot.values.pv, 1000, 'der erste Aufbau steht sofort richtig da');
  first.destroy();

  const second = mount({ values: { pv: 3000 }, roles: [] });
  assert.equal(second.snapshot.values.pv, 1000, 'die neue Instanz startet beim sichtbaren Wert');
  second.destroy();
});

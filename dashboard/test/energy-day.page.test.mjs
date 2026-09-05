// Regression tests for energy-day.js: dayPoints()/pointSeries()/
// xPositions()/stackAreas()/dayGeometry() are the pure port of Vorschlag D
// ("Tagesband") from the six-proposals exploration, driven by real
// role:<role> IndexedDB history samples instead of the prototype's
// simulated day profile - the x-axis spans whatever history actually
// exists instead of a fixed hour window, so this card has no layout-editor
// equivalent of the prototype's "Zeitfenster" selector (see
// knowhow/dashboard/energiegrafiken-konfiguration-backlog.md). Its two
// remaining options, display_mode and show_now, are covered below.
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
const scriptSource = read('energy-day.js');

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

function rolesSamplesAt(timestamp, values) {
  return Object.entries(values).map(([role, value]) => ({entity_id: `role:${role}`, timestamp, value}));
}

test('groupRoleSamples groups role: samples back into one point per poll timestamp', () => {
  const { model } = load();
  const samples = [
    ...rolesSamplesAt('2026-08-07T11:00:00Z', {pv: 100, battery: 0, grid: -100, load: 0, wallbox: 0, heat_pump: 0}),
    ...rolesSamplesAt('2026-08-07T11:00:10Z', {pv: 150, battery: 0, grid: -150, load: 0, wallbox: 0, heat_pump: 0}),
    {entity_id: 'sensor.something', timestamp: '2026-08-07T11:00:10Z', value: 42},
  ];
  const points = model.groupRoleSamples(samples);
  assert.equal(points.length, 2);
  assert.equal(points[0].pv, 100);
  assert.equal(points[1].pv, 150);
});

test('dayGeometry returns null with fewer than two grouped points', () => {
  const { factory } = load();
  const samples = rolesSamplesAt('2026-08-07T11:00:00Z', {pv: 100, battery: 0, grid: -100, load: 0, wallbox: 0, heat_pump: 0});
  assert.equal(factory.dayGeometry(samples, undefined, 980), null);
});

test('pointSeries closes the balance at every historical point', () => {
  const { factory, model } = load();
  const points = model.groupRoleSamples([
    ...rolesSamplesAt('2026-08-07T11:00:00Z', {pv: 6400, battery: 2200, grid: -3580, load: 620, wallbox: 0, heat_pump: 0}),
  ]);
  const series = factory.pointSeries(points);
  assert.equal(series[0].supply.pv, 6400);
  assert.equal(series[0].demand.battery_charge, 2200);
  assert.equal(series[0].demand.grid_export, 3580);
});

// unknown_consumer is the default gap_mode (see DEFAULT_INTERPRETATION in
// energy-model.js): a positive gap is folded into "base" by
// deriveBalanceCore() itself, so composeBalance() (used by every other
// card) never adds a separate "rest" entry for it - only diagnostic mode's
// unresolved gap does that. pointSeries() must follow the same rule instead
// of reading raw balance.gap unconditionally, or the folded-in amount gets
// drawn a second time as "Nicht zugeordnet" on top of "Übriger Verbrauch".
test('pointSeries does not draw a gap twice as both "base" and "rest" under the default unknown_consumer gap mode', () => {
  const { factory, model } = load();
  const points = model.groupRoleSamples([
    ...rolesSamplesAt('2026-08-07T11:00:00Z', {pv: 1000, battery: 0, grid: 0, load: 200, wallbox: 0, heat_pump: 0}),
  ]);
  const series = factory.pointSeries(points);
  assert.equal(series[0].demand.base, 1000, 'the 800W gap is folded into base by deriveBalanceCore');
  assert.equal(series[0].demand.rest, 0, 'must not also appear as "Nicht zugeordnet" once already folded into base');
});

test('xPositions spreads points evenly between the first and last timestamp', () => {
  const { factory } = load();
  const series = [
    {timestamp: '2026-08-07T11:00:00Z'},
    {timestamp: '2026-08-07T11:05:00Z'},
    {timestamp: '2026-08-07T11:10:00Z'},
  ];
  const xs = factory.xPositions(series, 980);
  assert.equal(xs[0], 58); // PAD_L
  assert.equal(xs[2], 962); // PAD_L + PLOT_W bei Breite 980
  assert.ok(Math.abs(xs[1] - (xs[0] + xs[2]) / 2) < 0.01);
});

test('stackAreas skips a key that never crosses the visibility floor', () => {
  const { factory } = load();
  const series = [
    {supply: {pv: 100, battery_discharge: 0, grid_import: 0, rest: 0}},
    {supply: {pv: 150, battery_discharge: 0, grid_import: 0, rest: 0}},
  ];
  const areas = factory.stackAreas(series, [58, 962], 'supply', ['pv', 'battery_discharge', 'grid_import', 'rest'], -1, 1, 0);
  assert.deepEqual(JSON.parse(JSON.stringify(areas.map(a => a.id))), ['pv']);
});

test('dayGeometry builds a mirrored area chart for a real two-point series', () => {
  const { factory } = load();
  const samples = [
    ...rolesSamplesAt('2026-08-07T11:00:00Z', {pv: 6400, battery: 2200, grid: -3580, load: 620, wallbox: 0, heat_pump: 0}),
    ...rolesSamplesAt('2026-08-07T11:10:00Z', {pv: 0, battery: -760, grid: 450, load: 760, wallbox: 0, heat_pump: 0}),
  ];
  const geometry = factory.dayGeometry(samples, undefined, 980);
  assert.ok(geometry);
  assert.equal(geometry.nowX, 962);
  assert.ok(geometry.supplyAreas.length > 0);
  assert.ok(geometry.demandAreas.length > 0);
});

// <template x-for> inside <svg> never gets a .content DocumentFragment in
// any browser (foreign-content parsing rule), so the repeated grid lines and
// stacked areas render through markup strings bound via x-html instead -
// see the comment in energy-day.js. These pure string-builders are the only
// thing standing in for the old templates, so they get the same coverage.
test('gridLinesMarkup renders one <g> per line with its stroke/label', () => {
  const { factory } = load();
  const markup = factory.gridLinesMarkup([{y: 100, label: '1,2', strong: true}, {y: 140, label: '0,6', strong: false}], 980);
  assert.match(markup, /<line x1="58" y1="100" x2="962" y2="100" stroke="#46515d" stroke-width="1.2">/);
  assert.match(markup, /<text x="50" y="104"[^>]*>1,2<\/text>/);
  assert.match(markup, /stroke="#333333" stroke-width="1"/);
});

test('areasMarkup renders one <path> per area with its d/fill/opacity', () => {
  const { factory } = load();
  const markup = factory.areasMarkup([{path: 'M 0 0 Z', color: '#fff', opacity: 0.72}]);
  assert.equal(markup, '<path d="M 0 0 Z" fill="#fff" opacity="0.72"></path>');
});

test('dayGeometry exposes markup strings alongside the geometry arrays', () => {
  const { factory } = load();
  const samples = [
    ...rolesSamplesAt('2026-08-07T11:00:00Z', {pv: 6400, battery: 2200, grid: -3580, load: 620, wallbox: 0, heat_pump: 0}),
    ...rolesSamplesAt('2026-08-07T11:10:00Z', {pv: 0, battery: -760, grid: 450, load: 760, wallbox: 0, heat_pump: 0}),
  ];
  const geometry = factory.dayGeometry(samples, undefined, 980);
  assert.equal(geometry.gridLinesMarkup, factory.gridLinesMarkup(geometry.gridLines, 980));
  assert.equal(geometry.supplyAreasMarkup, factory.areasMarkup(geometry.supplyAreas));
  assert.equal(geometry.demandAreasMarkup, factory.areasMarkup(geometry.demandAreas));
});

test('xPositions scales the plot width down for a narrow container', () => {
  const { factory } = load();
  const series = [
    {timestamp: '2026-08-07T11:00:00Z'},
    {timestamp: '2026-08-07T11:10:00Z'},
  ];
  const xs = factory.xPositions(series, 480);
  assert.equal(xs[0], 58); // PAD_L bleibt absolut
  assert.equal(xs[1], 480 - 18); // width - PAD_R
});

test('gridLinesMarkup ends the gridline at width - PAD_R, not at the old fixed 962', () => {
  const { factory } = load();
  const markup = factory.gridLinesMarkup([{y: 100, label: '1,2', strong: true}], 480);
  assert.match(markup, /x2="462"/); // 480 - PAD_R(18)
  assert.doesNotMatch(markup, /x2="962"/);
});

test('dayGeometry display_mode "supply" draws only the supply areas, growing from the bottom edge', () => {
  const { factory } = load();
  const samples = [
    ...rolesSamplesAt('2026-08-07T11:00:00Z', {pv: 6400, battery: 2200, grid: -3580, load: 620, wallbox: 0, heat_pump: 0}),
    ...rolesSamplesAt('2026-08-07T11:10:00Z', {pv: 0, battery: -760, grid: 450, load: 760, wallbox: 0, heat_pump: 0}),
  ];
  const geometry = factory.dayGeometry(samples, undefined, 980, {displayMode: 'supply', showNow: 'on'});
  assert.ok(geometry.supplyAreas.length > 0);
  assert.equal(geometry.demandAreas.length, 0);
  assert.equal(geometry.demandAreasMarkup, '');
  assert.ok(geometry.nowSupplyLabel.startsWith('Deckung'));
  assert.equal(geometry.nowDemandLabel, '');
});

test('dayGeometry display_mode "demand" draws only the demand areas, growing from the bottom edge', () => {
  const { factory } = load();
  const samples = [
    ...rolesSamplesAt('2026-08-07T11:00:00Z', {pv: 6400, battery: 2200, grid: -3580, load: 620, wallbox: 0, heat_pump: 0}),
    ...rolesSamplesAt('2026-08-07T11:10:00Z', {pv: 0, battery: -760, grid: 450, load: 760, wallbox: 0, heat_pump: 0}),
  ];
  const geometry = factory.dayGeometry(samples, undefined, 980, {displayMode: 'demand', showNow: 'on'});
  assert.equal(geometry.supplyAreas.length, 0);
  assert.ok(geometry.demandAreas.length > 0);
  assert.equal(geometry.supplyAreasMarkup, '');
  assert.ok(geometry.nowDemandLabel.startsWith('Verwendung'));
  assert.equal(geometry.nowSupplyLabel, '');
});

test('dayGeometry show_now "off" clears both now-labels without affecting the areas', () => {
  const { factory } = load();
  const samples = [
    ...rolesSamplesAt('2026-08-07T11:00:00Z', {pv: 6400, battery: 2200, grid: -3580, load: 620, wallbox: 0, heat_pump: 0}),
    ...rolesSamplesAt('2026-08-07T11:10:00Z', {pv: 0, battery: -760, grid: 450, load: 760, wallbox: 0, heat_pump: 0}),
  ];
  const geometry = factory.dayGeometry(samples, undefined, 980, {displayMode: 'mirror', showNow: 'off'});
  assert.equal(geometry.nowSupplyLabel, '');
  assert.equal(geometry.nowDemandLabel, '');
  assert.ok(geometry.supplyAreas.length > 0 && geometry.demandAreas.length > 0);
});

test('dayGeometry mirrored gridlines appear on both sides of the centerline, single-direction modes only above the baseline', () => {
  const { factory } = load();
  const samples = [
    ...rolesSamplesAt('2026-08-07T11:00:00Z', {pv: 6400, battery: 2200, grid: -3580, load: 620, wallbox: 0, heat_pump: 0}),
    ...rolesSamplesAt('2026-08-07T11:10:00Z', {pv: 0, battery: -760, grid: 450, load: 760, wallbox: 0, heat_pump: 0}),
  ];
  const mirrored = factory.dayGeometry(samples, undefined, 980, {displayMode: 'mirror'});
  const supplyOnly = factory.dayGeometry(samples, undefined, 980, {displayMode: 'supply'});
  assert.ok(mirrored.gridLines.some(l => l.y > 190)); // below the centerline (CY ~= 176)
  assert.ok(supplyOnly.gridLines.every(l => l.y <= 330.01)); // BOTTOM = H - PAD_B = 330, the single growth direction never crosses it
});

test('dayGeometry keeps every x-position inside [0, width] for a narrow container', () => {
  const { factory } = load();
  const samples = [
    ...rolesSamplesAt('2026-08-07T11:00:00Z', {pv: 6400, battery: 2200, grid: -3580, load: 620, wallbox: 0, heat_pump: 0}),
    ...rolesSamplesAt('2026-08-07T11:10:00Z', {pv: 0, battery: -760, grid: 450, load: 760, wallbox: 0, heat_pump: 0}),
  ];
  const geometry = factory.dayGeometry(samples, undefined, 480);
  assert.ok(geometry);
  assert.equal(geometry.nowX, 480 - 18);
});

function cardMarkup(initialSnapshot, dataAttrs) {
  const attrs = Object.entries(dataAttrs || {}).map(([k, v]) => ` data-${k}="${v}"`).join('');
  return `
    <div data-layout-item-id="energy-day"${attrs}>
      <section id="energy-day-card">
        <div class="energy-day-canvas">
          <svg></svg>
        </div>
        <script type="application/json" id="energy-day-initial">${JSON.stringify(initialSnapshot)}</script>
      </section>
    </div>
  `;
}

function buildComponent(window, document, factory, samples, dataAttrs) {
  document.body.innerHTML = cardMarkup({}, dataAttrs);
  const root = document.querySelector('#energy-day-card');
  const component = factory();
  component.$root = root;
  component.$refs = {
    canvas: root.querySelector('.energy-day-canvas'),
    svg: root.querySelector('.energy-day-canvas svg'),
  };
  window.dashboardHistorizer = { readSamples: async () => samples };
  return { component, root };
}

function twoPointSamples() {
  return [
    ...rolesSamplesAt('2026-08-07T11:00:00Z', {pv: 6400, battery: 2200, grid: -3580, load: 620, wallbox: 0, heat_pump: 0}),
    ...rolesSamplesAt('2026-08-07T11:10:00Z', {pv: 0, battery: -760, grid: 450, load: 760, wallbox: 0, heat_pump: 0}),
  ];
}

test('init() measures the canvas container and builds geometry at that width', async () => {
  const { factory, window, document } = load();
  const { component } = buildComponent(window, document, factory, twoPointSamples());
  Object.defineProperty(component.$refs.canvas, 'clientWidth', { value: 500, configurable: true });
  await component.init();
  assert.equal(component.width, 500);
  assert.ok(component.geometry);
  assert.equal(component.$refs.svg.getAttribute('viewBox'), '0 0 500 360');
});

test('init() reads display_mode/show_now from the wrapper and applies them to the built geometry', async () => {
  const { factory, window, document } = load();
  const { component } = buildComponent(window, document, factory, twoPointSamples(), {'display-mode': 'demand', 'show-now': 'off'});
  Object.defineProperty(component.$refs.canvas, 'clientWidth', { value: 500, configurable: true });
  await component.init();
  assert.equal(component.options.displayMode, 'demand');
  assert.equal(component.options.showNow, 'off');
  assert.equal(component.geometry.supplyAreas.length, 0);
  assert.equal(component.geometry.nowDemandLabel, '');
});

test('init() falls back to 980 when the container reports zero width (hidden panel)', async () => {
  const { factory, window, document } = load();
  const { component } = buildComponent(window, document, factory, twoPointSamples());
  Object.defineProperty(component.$refs.canvas, 'clientWidth', { value: 0, configurable: true });
  await component.init();
  assert.equal(component.width, 980);
  assert.ok(component.geometry, 'a zero-width measurement (hidden x-show panel) must not produce a null/NaN geometry');
  assert.ok(Number.isFinite(component.geometry.nowX));
});

// dashboard.js's refreshLiveFragment() swaps this whole card's DOM via htmx
// outerHTML on every "registry" SSE tick, destroying and re-creating this
// Alpine component - see the cachedSamples comment in energy-day.js. A
// second instance built from the same module (same `load()`, since the
// cache lives at module scope) must render synchronously from the first
// instance's samples instead of sitting in the loading/no-geometry state
// with the default viewBox until its own IndexedDB read resolves.
test('init() seeds a re-created instance from the previous instance\'s samples before its own read resolves', async () => {
  const { factory, window, document } = load();

  const first = buildComponent(window, document, factory, twoPointSamples());
  Object.defineProperty(first.component.$refs.canvas, 'clientWidth', { value: 500, configurable: true });
  await first.component.init();
  assert.ok(first.component.geometry);

  let resolveSamples;
  document.body.innerHTML = cardMarkup({}, {});
  const root = document.querySelector('#energy-day-card');
  const second = factory();
  second.$root = root;
  second.$refs = {
    canvas: root.querySelector('.energy-day-canvas'),
    svg: root.querySelector('.energy-day-canvas svg'),
  };
  Object.defineProperty(second.$refs.canvas, 'clientWidth', { value: 500, configurable: true });
  window.dashboardHistorizer = { readSamples: () => new Promise(resolve => { resolveSamples = resolve; }) };

  const initPromise = second.init();
  assert.equal(second.loading, false, 'a re-created instance must not sit in the loading state while its own read is in flight');
  assert.ok(second.geometry, 'a re-created instance must have geometry seeded from the previous instance');
  assert.equal(second.$refs.svg.getAttribute('viewBox'), '0 0 500 360', 'the seeded viewBox must use the measured width, not the 980 default');

  resolveSamples(twoPointSamples());
  await initPromise;
  assert.ok(second.geometry);
});

test('das Tagesband fuehrt den gemessenen Verbrauch als eigene Verwendungsflaeche', () => {
  const { factory } = load();
  const points = [
    {timestamp: '2026-08-21T10:00:00Z', pv: 3000, load: 900, wallbox: 500},
    {timestamp: '2026-08-21T10:01:00Z', pv: 3000, load: 900, wallbox: 500},
  ];
  const series = factory.pointSeries(points, {load_mode: 'combined'});
  assert.equal(series[0].demand.load_measured, 900);
  assert.equal(series[0].demand.base, 1600);
});

test('das Tagesband zeigt beim zweiten Schnappschuss zunaechst den alten Wert', async () => {
  const { factory, window: win } = load();
  let now = 0;
  Object.defineProperty(win, 'performance', { value: { now: () => now }, configurable: true });
  win.requestAnimationFrame = () => {};
  win.DashboardTheme.onChange = () => () => {};

  const mount = async raw => {
    const markup = `
      <div data-layout-item-id="day-1">
        <section id="energy-day-card">
          <div class="energy-day-canvas">
            <svg></svg>
          </div>
          <script type="application/json" id="energy-day-initial">${JSON.stringify(raw)}</script>
        </section>
      </div>
    `;
    win.document.body.innerHTML = markup;
    const root = win.document.querySelector('#energy-day-card');
    const card = factory();
    card.$root = root;
    card.$refs = {
      canvas: root.querySelector('.energy-day-canvas'),
      svg: root.querySelector('svg'),
    };
    Object.defineProperty(card.$refs.canvas, 'clientWidth', { value: 500, configurable: true });
    win.dashboardHistorizer = { readSamples: async () => [] };
    await card.init();
    return card;
  };

  const first = await mount({ values: { pv: 1000 }, roles: [] });
  assert.equal(first.liveSnapshot.values.pv, 1000, 'der erste Aufbau steht sofort richtig da');
  first.destroy();

  const second = await mount({ values: { pv: 3000 }, roles: [] });
  assert.equal(second.liveSnapshot.values.pv, 1000, 'die neue Instanz startet beim sichtbaren Wert');
  second.destroy();
});

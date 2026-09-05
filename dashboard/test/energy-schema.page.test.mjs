// Regression tests for energy-schema.js: branchValues()/widthFor()/
// branchGeometry()/branchEntities() are the pure port of Vorschlag E
// ("Anlagenschema") from the six-proposals exploration - leistungs-
// proportional line width by default. Since the "fließende Skalierung" spec
// (2026-08-22) the x-geometry is a function of the measured container width
// (schemaGeometry()), and the busbar carries the sources' mix with a bar per
// consuming branch (allocate() in energy-model.js). Its layout-editor
// options (stroke_mode, entity_labels, hide_inactive, display_size, animate)
// are covered below.
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
const scriptSource = read('energy-schema.js');

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

test('BRANCHES declares five fixed branches covering pv/battery/wallbox/heat_pump/base', () => {
  const { factory } = load();
  assert.deepEqual(JSON.parse(JSON.stringify(factory.BRANCHES.map(b => b.id))).sort(), ['base', 'battery', 'heat_pump', 'pv', 'wallbox'].sort());
});

test('branchValues reports "in" for pv and "out" for a charging battery', () => {
  const { factory, model } = load();
  const snapshot = {values: {pv: 6400, battery: 2200, load: 620, grid: -3580}, roles: []};
  const balance = model.deriveBalance(snapshot);
  const values = factory.branchValues(snapshot, balance);
  assert.equal(values.pv.dir, 'in');
  assert.equal(values.battery.dir, 'out');
  assert.equal(values.battery.value, 2200);
});

test('branchValues reports "in" for a discharging battery', () => {
  const { factory, model } = load();
  const snapshot = {values: {pv: 0, battery: -760, load: 760, grid: 450}, roles: []};
  const balance = model.deriveBalance(snapshot);
  const values = factory.branchValues(snapshot, balance);
  assert.equal(values.battery.dir, 'in');
  assert.equal(values.battery.value, 760);
});

test('branchValues nets simultaneous charge and discharge into one battery balance instead of dropping one side', () => {
  const { factory, model } = load();
  const snapshot = {values: {pv: 0, battery_charge: 300, battery_discharge: 900, load: 600}, roles: []};
  const balance = model.deriveBalance(snapshot);
  const values = factory.branchValues(snapshot, balance);
  assert.equal(values.battery.value, 600);
  assert.equal(values.battery.dir, 'in');
});

test('widthFor grows monotonically with value and clamps at the peak', () => {
  const { factory } = load();
  const small = factory.widthFor(100, 1000);
  const large = factory.widthFor(1000, 1000);
  const overPeak = factory.widthFor(5000, 1000);
  assert.ok(small < large);
  assert.equal(large, overPeak);
});

test('widthFor stroke_mode "const" is a fixed 2.4px regardless of value', () => {
  const { factory } = load();
  assert.equal(factory.widthFor(100, 1000, 'const'), 2.4);
  assert.equal(factory.widthFor(1000, 1000, 'const'), 2.4);
});

test('widthFor stroke_mode "power" (default, incl. omitted) stays proportional', () => {
  const { factory } = load();
  assert.ok(factory.widthFor(100, 1000, 'power') < factory.widthFor(1000, 1000, 'power'));
  assert.ok(factory.widthFor(100, 1000) < factory.widthFor(1000, 1000));
});

test('branchEntities returns the source entity IDs for a plain role', () => {
  const { factory } = load();
  const snapshot = {sources: {pv: ['sensor.pv_1', 'sensor.pv_2']}};
  assert.deepEqual(JSON.parse(JSON.stringify(factory.branchEntities(snapshot, 'pv'))), ['sensor.pv_1', 'sensor.pv_2']);
});

test('branchEntities combines battery/battery_charge/battery_discharge sources for the "battery" branch', () => {
  const { factory } = load();
  const snapshot = {sources: {battery: ['sensor.batt'], battery_charge: ['sensor.batt_charge']}};
  assert.deepEqual(JSON.parse(JSON.stringify(factory.branchEntities(snapshot, 'battery'))), ['sensor.batt', 'sensor.batt_charge']);
});

test('branchEntities returns null for "base" - a derived quantity with no single source entity', () => {
  const { factory } = load();
  assert.equal(factory.branchEntities({sources: {load: ['sensor.load']}}, 'base'), null);
});

// --- schemaGeometry(): Abschnitt 1+2 der Skalierungs-Spec -----------------

test('schemaGeometry centers the busbar and keeps the branch line at least 26px, even on a narrow card', () => {
  const { factory } = load();
  for (const W of [280, 400, 700, 980, 1400]) {
    const geom = factory.schemaGeometry(W, 1);
    assert.equal(geom.busX, Math.round(W / 2));
    const half = Math.min(geom.busX, W - geom.busX) - geom.busWidth / 2 - geom.margin;
    const lineLen = half - geom.boxWidth;
    assert.ok(lineLen >= 26 - 0.01, `line length ${lineLen} at W=${W}`);
  }
});

test('schemaGeometry: the box gives way before the 26px line minimum, not the other way round', () => {
  const { factory } = load();
  const wide = factory.schemaGeometry(980, 1);
  const narrow = factory.schemaGeometry(320, 1);
  assert.ok(narrow.boxWidth < wide.boxWidth, 'a narrow card must shrink the box, not the line');
});

test('schemaGeometry: display_size factors are monotonic in box width and font size (XS <= S <= M <= L <= XL)', () => {
  const { factory } = load();
  const W = 1400; // gross genug, dass keine Groessenstufe an den Clamps haengt
  const stages = ['xs', 's', 'm', 'l', 'xl'].map(key => factory.schemaGeometry(W, factory.SIZE_FACTORS[key]));
  for (let i = 1; i < stages.length; i++) {
    assert.ok(stages[i].boxWidth >= stages[i - 1].boxWidth, `boxWidth stage ${i}`);
    assert.ok(stages[i].fontSize >= stages[i - 1].fontSize, `fontSize stage ${i}`);
  }
});

// --- branchGeometry() -------------------------------------------------

test('branchGeometry places left branches so their line runs box-edge to bus-edge', () => {
  const { factory } = load();
  const geom = factory.schemaGeometry(980, 1);
  const branch = {id: 'pv', side: 'left', y: 122, label: 'PV-Wechselrichter'};
  const geometry = factory.branchGeometry(branch, {value: 6400, dir: 'in'}, 6400, false, 'power', geom);
  assert.equal(geometry.x1, geom.busX - geom.busWidth / 2); // ends at the busbar edge
  assert.equal(geometry.x0, geom.margin + geom.boxWidth); // starts at the right edge of the left-side box
  assert.equal(geometry.active, true);
  assert.equal(geometry.left, true);
});

test('branchGeometry places right branches so their line runs bus-edge to box-edge', () => {
  const { factory } = load();
  const geom = factory.schemaGeometry(980, 1);
  const branch = {id: 'wallbox', side: 'right', y: 122, label: 'Wallbox'};
  const geometry = factory.branchGeometry(branch, {value: 2000, dir: 'out'}, 2000, false, 'power', geom);
  assert.equal(geometry.x0, geom.busX + geom.busWidth / 2);
  assert.equal(geometry.x1, geom.W - geom.margin - geom.boxWidth);
  assert.equal(geometry.left, false);
});

test('branchGeometry marks an idle branch inactive with the fixed idle stroke width', () => {
  const { factory } = load();
  const geom = factory.schemaGeometry(980, 1);
  const branch = {id: 'wallbox', side: 'right', y: 122, label: 'Wallbox'};
  const geometry = factory.branchGeometry(branch, {value: 0, dir: 'out'}, 1000, false, 'power', geom);
  assert.equal(geometry.active, false);
  assert.equal(geometry.strokeWidth, 1.2);
});

// --- arrowSegment(): Abschnitt 3 (Pfeil laeuft hinter dem Balken los) -----

// arrowSegment()/branchBarSegments() run inside the JSDOM vm context, so
// their return values have that realm's Object/Array prototype -
// round-trip through JSON before deepEqual, same workaround used elsewhere
// in this file (see batterySocLabel's test) for cross-realm objects.
const plain = value => JSON.parse(JSON.stringify(value));

test('arrowSegment covers the whole line for a producer (no bar to dodge)', () => {
  const { factory } = load();
  const bg = {x0: 230, x1: 483, left: true, dir: 'in'};
  assert.deepEqual(plain(factory.arrowSegment(bg, 0)), {from: 230, to: 483});
});

test('arrowSegment excludes the bar for a left-side consumer (battery charging)', () => {
  const { factory } = load();
  const bg = {x0: 40, x1: 483, left: true, dir: 'out'};
  assert.deepEqual(plain(factory.arrowSegment(bg, 30)), {from: 453, to: 40});
});

test('arrowSegment excludes the bar for a right-side consumer', () => {
  const { factory } = load();
  const bg = {x0: 497, x1: 750, left: false, dir: 'out'};
  assert.deepEqual(plain(factory.arrowSegment(bg, 30)), {from: 527, to: 750});
});

// --- branchBarSegments(): Abschnitt 4 (Verbraucherbalken) -----------------

test('branchBarSegments stacks segments adjacent to the busbar, extending outward toward the box', () => {
  const { factory } = load();
  const bg = {x0: 40, x1: 483, y: 122, left: true};
  const mix = [{label: 'PV', color: '#pv', v: 600}, {label: 'Netzbezug', color: '#grid', v: 300}];
  const segments = factory.branchBarSegments(bg, 30, mix, 900, 10);
  assert.equal(segments.length, 2);
  // erstes Segment sitzt direkt an der Schiene (x1), zweites weiter Richtung Kasten.
  assert.equal(segments[0].x + segments[0].w, bg.x1);
  assert.ok(segments[1].x < segments[0].x);
  const totalWidth = segments.reduce((sum, s) => sum + s.w, 0);
  assert.ok(Math.abs(totalWidth - 30) < 0.01);
});

test('branchBarSegments returns nothing without a mix', () => {
  const { factory } = load();
  const bg = {x0: 40, x1: 483, y: 122, left: true};
  assert.deepEqual(plain(factory.branchBarSegments(bg, 30, null, 900, 10)), []);
  assert.deepEqual(plain(factory.branchBarSegments(bg, 30, [], 900, 10)), []);
});

// --- branchesMarkup(): reine Markup-Zusammensetzung -----------------------

function baseBranch(overrides) {
  return {
    id: 'pv', label: 'PV-Wechselrichter', y: 122, boxX: 40, boxWidth: 190, boxHeight: 46,
    fontSize: 13, busX: 490, x0: 230, x1: 483, active: true, stale: false,
    strokeWidth: 5, labelX: 265, color: '#fff', valueLabel: '6,4 kW',
    socLabel: '', socStale: false, entityLabel: '',
    barSegments: [], arrowFrom: 260, arrowTo: 483, animateDuration: 1.8,
    ...overrides,
  };
}

test('branchesMarkup draws a static arrow polygon for an active branch when animation is suppressed, none for an idle one', () => {
  const { factory } = load();
  const active = baseBranch();
  const idle = baseBranch({active: false});
  assert.match(factory.branchesMarkup([active], true), /<polygon points="[^"]+" fill="#fff">/);
  assert.doesNotMatch(factory.branchesMarkup([idle], true), /<polygon/);
  assert.match(factory.branchesMarkup([active], true), /PV-Wechselrichter/);
});

test('branchesMarkup animates the arrow on two nested <g> when the free segment is >= 10px and animation is not suppressed', () => {
  const { factory } = load();
  const branch = baseBranch({arrowFrom: 260, arrowTo: 483});
  const markup = factory.branchesMarkup([branch], false);
  assert.match(markup, /class="energy-schema-flow-anim"/);
  assert.match(markup, /--dx:223px/);
  // aeusseres g traegt das statische transform, inneres die Animationsklasse -
  // eine CSS-Animation auf demselben Element wuerde das transform ueberschreiben.
  assert.match(markup, /<g transform="translate\(260 122\)"><g class="energy-schema-flow-anim"/);
});

test('branchesMarkup falls back to a static, centered arrow when the free segment is under 10px, even with animation on', () => {
  const { factory } = load();
  const branch = baseBranch({arrowFrom: 480, arrowTo: 483});
  const markup = factory.branchesMarkup([branch], false);
  assert.doesNotMatch(markup, /energy-schema-flow-anim/);
  assert.match(markup, /<polygon/);
});

test('branchesMarkup renders a second line for a branch with a battery state-of-charge label, and omits it otherwise', () => {
  const { factory } = load();
  const battery = baseBranch({id: 'battery', y: 246, socLabel: '62 %'});
  const pv = baseBranch({socLabel: ''});
  assert.match(factory.branchesMarkup([battery], true), /62 %/);
  assert.doesNotMatch(factory.branchesMarkup([pv], true), /<text[^>]*>62 %/);
});

test('branchesMarkup renders the entity line only when entityLabel is set', () => {
  const { factory } = load();
  const withEntity = baseBranch({entityLabel: 'sensor.pv_1'});
  const withoutEntity = baseBranch({entityLabel: ''});
  assert.match(factory.branchesMarkup([withEntity], true), /sensor\.pv_1/);
  assert.doesNotMatch(factory.branchesMarkup([withoutEntity], true), /sensor\.pv_1/);
});

test('branchesMarkup draws one rect per bar segment', () => {
  const { factory } = load();
  const branch = baseBranch({barSegments: [{x: 470, y: 118, w: 10, h: 8, color: '#111'}, {x: 460, y: 118, w: 5, h: 8, color: '#222'}]});
  const markup = factory.branchesMarkup([branch], true);
  assert.match(markup, /<rect x="470" y="118" width="10" height="8" fill="#111">/);
  assert.match(markup, /<rect x="460" y="118" width="5" height="8" fill="#222">/);
});

test('batterySocLabel reads the battery_soc role, formats it as a percentage and reports its staleness', () => {
  const { factory } = load();
  // batterySocLabel() runs inside the JSDOM vm context, so its return value has
  // that realm's Object prototype - round-trip through JSON before deepEqual,
  // same workaround energyflow.page.test.mjs uses for cross-realm objects.
  const plain = result => JSON.parse(JSON.stringify(result));
  assert.deepEqual(plain(factory.batterySocLabel({values: {battery_soc: 62.4}, roles: []})), {label: '62 %', stale: false});
  assert.deepEqual(
    plain(factory.batterySocLabel({values: {battery_soc: 8}, roles: [{role: 'battery_soc', freshness: 'stale'}]})),
    {label: '8 %', stale: true},
  );
  // no battery_soc key at all (no entity carries capacity_kwh) - no label.
  assert.deepEqual(plain(factory.batterySocLabel({values: {}, roles: []})), {label: '', stale: false});
});

test('das Anlagenschema bekommt im Kombiniert-Modus einen vierten Zweig rechts', () => {
  const { factory } = load();
  const ohne = factory.branchesFor(false).filter(b => b.side === 'right').map(b => b.id);
  const mit = factory.branchesFor(true).filter(b => b.side === 'right').map(b => b.id);
  assert.equal(ohne.length, 3, 'without measured: should have 3 branches');
  assert.equal(ohne[0], 'wallbox');
  assert.equal(ohne[1], 'heat_pump');
  assert.equal(ohne[2], 'base');
  assert.equal(mit.length, 4, 'with measured: should have 4 branches');
  assert.equal(mit[0], 'wallbox');
  assert.equal(mit[1], 'heat_pump');
  assert.equal(mit[2], 'load_measured');
  assert.equal(mit[3], 'base');
  // Kein Kasten darf unter die Sammelschiene rutschen (BUS_BOT 312, Kastenhoehe 46).
  for (const branch of factory.branchesFor(true)) assert.ok(branch.y + 23 <= 312, branch.id);
});

// --- Der ganze Alpine-Baustein: init()/recompute() ------------------------

function cardMarkup(initialSnapshot, dataAttrs) {
  const attrs = Object.entries(dataAttrs || {}).map(([k, v]) => ` data-${k}="${v}"`).join('');
  return `
    <div data-layout-item-id="energy-schema"${attrs}>
      <section id="energy-schema-card">
        <div class="energy-schema-canvas">
          <svg></svg>
        </div>
        <script type="application/json" id="energy-schema-initial">${JSON.stringify(initialSnapshot)}</script>
      </section>
    </div>
  `;
}

function buildComponent(window, document, factory, initialSnapshot, dataAttrs) {
  document.body.innerHTML = cardMarkup(initialSnapshot, dataAttrs);
  const root = document.querySelector('#energy-schema-card');
  const component = factory();
  component.$root = root;
  component.$refs = {
    canvas: root.querySelector('.energy-schema-canvas'),
    svg: root.querySelector('.energy-schema-canvas svg'),
  };
  window.matchMedia = () => ({ matches: false });
  return component;
}

test('init() measures the canvas container and rebuilds the geometry at that width', () => {
  const { factory, window, document } = load();
  const component = buildComponent(window, document, factory, {values: {pv: 6400, battery: 0, load: 0, grid: -6400, wallbox: 0, heat_pump: 0}, roles: [], sources: {}});
  Object.defineProperty(component.$refs.canvas, 'clientWidth', { value: 560, configurable: true });
  component.init();
  assert.equal(component.width, 560);
  assert.equal(component.geom.busX, 280);
  assert.equal(component.$refs.svg.getAttribute('viewBox'), '0 0 560 384');
});

test('init() hide_inactive "on" drops idle branches from the rendered set', () => {
  const { factory, window, document } = load();
  // pv active, everything else idle (0 W) - with hide_inactive on, only pv should remain.
  const snapshot = {values: {pv: 6400, battery: 0, load: 0, grid: -6400, wallbox: 0, heat_pump: 0}, roles: [], sources: {}};
  const component = buildComponent(window, document, factory, snapshot, {'hide-inactive': 'on'});
  component.init();
  assert.deepEqual(JSON.parse(JSON.stringify(component.branches.map(b => b.id))), ['pv']);
});

test('init() entity_labels "entity" populates entityLabel from Snapshot.Sources', () => {
  const { factory, window, document } = load();
  const snapshot = {values: {pv: 6400, battery: 0, load: 0, grid: -6400, wallbox: 0, heat_pump: 0}, roles: [], sources: {pv: ['sensor.pv_1']}};
  const component = buildComponent(window, document, factory, snapshot, {'entity-labels': 'entity'});
  component.init();
  const pv = component.branches.find(b => b.id === 'pv');
  const base = component.branches.find(b => b.id === 'base');
  assert.equal(pv.entityLabel, 'sensor.pv_1');
  assert.equal(base.entityLabel, 'abgeleitet aus Hausverbrauch');
});

test('init() entity_labels "power" (default) leaves entityLabel empty on every branch, including "base"', () => {
  const { factory, window, document } = load();
  const snapshot = {values: {pv: 6400, battery: 0, load: 0, grid: -6400, wallbox: 0, heat_pump: 0}, roles: [], sources: {pv: ['sensor.pv_1']}};
  const component = buildComponent(window, document, factory, snapshot, {});
  component.init();
  assert.ok(component.branches.every(b => b.entityLabel === ''), 'entity_labels power must not show the "abgeleitet" fallback either');
});

test('init() animate defaults to "off" for energy_schema (unlike energy_band/energy_ring)', () => {
  const { factory, window, document } = load();
  const snapshot = {values: {pv: 6400, battery: 0, load: 0, grid: -6400, wallbox: 0, heat_pump: 0}, roles: [], sources: {}};
  const component = buildComponent(window, document, factory, snapshot, {});
  component.init();
  assert.equal(component.options.animate, 'off');
  // ohne Animation ist die Beschriftung ein statisches Polygon, keine animierte Gruppe.
  assert.doesNotMatch(component.branchesMarkup, /energy-schema-flow-anim/);
});

test('init() gives a consuming branch a bar sourced from allocate() - PV covers the whole load here', () => {
  const { factory, window, document } = load();
  const snapshot = {values: {pv: 3000, battery: 0, load: 3000, grid: 0, wallbox: 0, heat_pump: 0}, roles: [], sources: {}};
  const component = buildComponent(window, document, factory, snapshot, {});
  component.init();
  const base = component.branches.find(b => b.id === 'base');
  assert.ok(base.barSegments.length > 0, 'consuming branch should get a bar');
  const totalWidth = base.barSegments.reduce((sum, s) => sum + s.w, 0);
  assert.ok(totalWidth > 0, 'bar segments should have non-zero width');
  // Erzeuger bekommen keinen Balken.
  const pv = component.branches.find(b => b.id === 'pv');
  assert.deepEqual(plain(pv.barSegments), []);
});

test('init() gibt der Einspeisung einen senkrechten Balken, der den Netzanschluss-Kasten nicht beruehrt', () => {
  const { factory, window, document } = load();
  // PV-Ueberschuss: alles geht ins Netz.
  const snapshot = {values: {pv: 4000, battery: 0, load: 0, grid: -4000, wallbox: 0, heat_pump: 0}, roles: [], sources: {}};
  const component = buildComponent(window, document, factory, snapshot, {});
  Object.defineProperty(component.$refs.canvas, 'clientWidth', { value: 980, configurable: true });
  component.init();
  assert.notEqual(component.exportBarMarkup, '');
  const ys = [...component.exportBarMarkup.matchAll(/y="([0-9.]+)" width/g)].map(m => Number(m[1]));
  const heights = [...component.exportBarMarkup.matchAll(/height="([0-9.]+)"/g)].map(m => Number(m[1]));
  // Kein Rect darf ueber y=59 (Netzanschluss-Kastenunterkante bei der
  // Standardgroesse) hinausragen - die "-12"-Regel in exportBarMarkup()
  // haelt einen Sicherheitsabstand.
  for (let i = 0; i < ys.length; i++) assert.ok(ys[i] >= 59, `export bar rect top ${ys[i]} touches the Netzanschluss box`);
});

test('arrowMarkup verankert den Pfeil an seiner eigenen Umlaufzeit', () => {
  const { factory, window: win } = load();
  let now = 0;
  Object.defineProperty(win, 'performance', { value: { now: () => now }, configurable: true });
  const branch = { id: 'pv', active: true, arrowFrom: 0, arrowTo: 200, animateDuration: 2, color: '#fff', y: 122 };
  assert.match(factory.arrowMarkup(branch, false, 'schema-1'), /animation-delay:-0\.000s/);
  now = 1000;   // eine Sekunde von zwei = ein halber Umlauf
  assert.match(factory.arrowMarkup(branch, false, 'schema-1'), /animation-delay:-1\.000s/);
});

test('das Anlagenschema zeigt beim zweiten Schnappschuss zunaechst den alten Wert', () => {
  const { factory, window: win } = load();
  let now = 0;
  Object.defineProperty(win, 'performance', { value: { now: () => now }, configurable: true });
  win.requestAnimationFrame = () => {};
  win.DashboardTheme.onChange = () => () => {};

  const mount = raw => {
    const markup = `
      <div data-layout-item-id="schema-1">
        <section id="energy-schema-card">
          <div class="energy-schema-canvas">
            <svg></svg>
          </div>
          <script type="application/json" id="energy-schema-initial">${JSON.stringify(raw)}</script>
        </section>
      </div>
    `;
    win.document.body.innerHTML = markup;
    const root = win.document.querySelector('#energy-schema-card');
    const card = factory();
    card.$root = root;
    card.$refs = {
      canvas: root.querySelector('.energy-schema-canvas'),
      svg: root.querySelector('svg'),
    };
    Object.defineProperty(card.$refs.canvas, 'clientWidth', { value: 500, configurable: true });
    card.init();
    return card;
  };

  const first = mount({ values: { pv: 1000 }, roles: [], sources: {} });
  assert.equal(first.snapshot.values.pv, 1000, 'der erste Aufbau steht sofort richtig da');
  first.destroy();

  const second = mount({ values: { pv: 3000 }, roles: [], sources: {} });
  assert.equal(second.snapshot.values.pv, 1000, 'die neue Instanz startet beim sichtbaren Wert');
  second.destroy();
});

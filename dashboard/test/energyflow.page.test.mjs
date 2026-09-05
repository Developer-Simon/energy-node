// Regression tests for energy-flow.js: geometry()/strokeWidth()/flowDuration()/
// nodeText() are pure ports of the old server-side layout, and the Alpine
// component draws the graphic from the snapshot embedded by the server
// (#energy-flow-initial) on every mount.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'energy-flow.js'),
  'utf8',
);

// Seit dem Umbau auf eine Linie je Verbindung: vier <path data-flow>, nicht
// mehr acht. Richtung (Bezug/Einspeisung, Laden/Entladen) traegt is-reversed.
const FLOW_IDS = ['pv', 'grid', 'battery', 'load'];
const NODE_IDS = ['pv', 'grid', 'home', 'battery', 'load'];

function loadEnergyFlow() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  vm.runInContext(fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'energy-presentation.js'), 'utf8'), context);
  vm.runInContext(scriptSource, context);
  return { factory, window: dom.window, document: dom.window.document };
}

// Jeder Knoten traegt inzwischen ein animiertes Icon im <output> (siehe
// overview.html "energy-flow-card"): der SVG mit .energy-flow-node-icon plus
// ein separater Textknoten .energy-flow-node-text fuer die Zahl. Der Inhalt
// der Icons ist hier egal - render() findet sie ueber die gemeinsame Klasse,
// step() haengt die Zustandsklasse ans SVG-Element.
const nodeOutputMarkup = id => {
  // Lasten-Knoten: eine Liste von Verbraucher-Zeilen statt eines festen Icons
  // (energy-flow.js renderLoadList). Server rendert eine Vorgabe-Zeile.
  if (id === 'load') {
    return '<output><ul class="energy-flow-load-list"><li class="energy-flow-load-row"><svg class="energy-flow-node-icon energy-flow-load-icon" data-load-icon="generic"></svg><span class="energy-flow-node-text"></span></li></ul></output>';
  }
  const iconClass = id === 'battery'
    ? 'energy-flow-node-icon energy-flow-battery-icon'
    : `energy-flow-node-icon energy-flow-${id}-icon`;
  const inner = id === 'battery' ? '<rect class="energy-flow-battery-fill"></rect>' : '';
  if (id === 'battery') {
    // Der Fuellstand steht in einer eigenen Zeile neben dem Leistungswert
    // (overview.html .energy-flow-battery-vals), step() zieht sie nach.
    return `<output><svg class="${iconClass}">${inner}</svg><span class="energy-flow-battery-vals"><span class="energy-flow-battery-text energy-flow-node-text"></span><span class="energy-flow-battery-soc" hidden></span></span></output>`;
  }
  return `<output><svg class="${iconClass}">${inner}</svg><span class="energy-flow-node-text"></span></output>`;
};

function cardMarkup(initialSnapshot) {
  return `
    <section id="energy-flow-card">
      <div class="energy-flow-map">
        <svg class="energy-flow-lines"></svg>
        ${NODE_IDS.map(id => `<article data-node="${id}">${nodeOutputMarkup(id)}</article>`).join('')}
      </div>
      <script type="application/json" id="energy-flow-initial">${JSON.stringify(initialSnapshot)}</script>
    </section>
  `;
}

function buildComponent(window, document, factory, initialSnapshot) {
  document.body.innerHTML = cardMarkup(initialSnapshot);
  const root = document.querySelector('#energy-flow-card');
  const component = factory();
  component.$root = root;
  component.$refs = {
    map: root.querySelector('.energy-flow-map'),
    svg: root.querySelector('.energy-flow-map svg'),
  };
  component.$nextTick = fn => fn();
  Object.defineProperty(component.$refs.map, 'clientWidth', { value: 800, configurable: true });
  Object.defineProperty(component.$refs.map, 'clientHeight', { value: 400, configurable: true });
  window.matchMedia = () => ({ matches: false });
  return { component, root };
}

function snapshotWith(values, roles = []) {
  return { at: '2026-01-01T10:00:00Z', values, roles, quality: [] };
}

// scaleMode und die Referenz-Attribute kommen beide vom naechsten
// [data-layout-item-id]-Vorfahren (pro Kachel, siehe layout.page.js's
// FlowScale/SpeedReferenceMode/-Watts), nicht von root selbst.
function wrapInLayoutItem(document, root, flowScale, speedReferenceMode, speedReferenceWatts) {
  const wrapper = document.createElement('div');
  wrapper.dataset.layoutItemId = 'item-1';
  wrapper.dataset.flowScale = flowScale;
  if (speedReferenceMode !== undefined) wrapper.dataset.speedReferenceMode = speedReferenceMode;
  if (speedReferenceWatts !== undefined) wrapper.dataset.speedReferenceWatts = speedReferenceWatts;
  root.replaceWith(wrapper);
  wrapper.appendChild(root);
  return wrapper;
}

test('geometry() places all five nodes and four connection paths inside the container, and swaps the axis with the aspect ratio', () => {
  const { factory } = loadEnergyFlow();
  for (const [width, height] of [[800, 400], [400, 600]]) {
    const { nodes, paths } = factory.geometry(width, height);
    assert.equal(nodes.length, 5);
    for (const node of nodes) {
      assert.ok(node.x >= 0 && node.x <= width, `${node.id}.x within container at ${width}x${height}`);
      assert.ok(node.y >= 0 && node.y <= height, `${node.id}.y within container at ${width}x${height}`);
      assert.ok(Number.isFinite(node.x) && Number.isFinite(node.y));
    }
    for (const id of FLOW_IDS) {
      assert.match(paths[id], /^M -?\d+(\.\d+)? -?\d+(\.\d+)? Q /, `${id} path is a quadratic bezier at ${width}x${height}`);
    }
    const at = id => nodes.find(n => n.id === id);
    if (width >= height) {
      // Querformat: PV/Speicher links, Netz/Verbrauch rechts.
      assert.ok(at('pv').x < width / 2 && at('battery').x < width / 2, 'PV und Speicher links');
      assert.ok(at('grid').x > width / 2 && at('load').x > width / 2, 'Netz und Verbrauch rechts');
    } else {
      // Hochformat: PV/Netz oben, Speicher/Verbrauch unten.
      assert.ok(at('pv').y < height / 2 && at('grid').y < height / 2, 'PV und Netz oben');
      assert.ok(at('battery').y > height / 2 && at('load').y > height / 2, 'Speicher und Verbrauch unten');
    }
  }
});

// Nur im sehr breiten Querformat: die Kruemmung kippt nach aussen, sonst
// schneiden die einwaerts gebogenen Kurven einander vor dem Gebaeude.
const controlPointY = pathStr => {
  const m = pathStr.match(/^M [\d.-]+ ([\d.-]+) Q [\d.-]+ ([\d.-]+) /);
  return { start: Number(m[1]), ctrl: Number(m[2]) };
};

test('geometry() biegt die Linien erst ab einem sehr breiten Seitenverhaeltnis nach aussen', () => {
  const { factory } = loadEnergyFlow();
  // Massvoll breit (2:1): PV-Kurve biegt einwaerts, der Kontrollpunkt liegt
  // zwischen PV und Gebaeude, also unterhalb des PV-Knotens.
  const normal = controlPointY(factory.geometry(800, 400).paths.pv);
  assert.ok(normal.ctrl > normal.start, 'normales Querformat: PV biegt zur Mitte');
  // Sehr breit (4:1): dieselbe Linie biegt nach aussen, der Kontrollpunkt
  // liegt oberhalb des PV-Knotens.
  const wide = controlPointY(factory.geometry(1600, 400).paths.pv);
  assert.ok(wide.ctrl < wide.start, 'sehr breites Querformat: PV biegt nach aussen');
  // Hochformat bleibt einwaerts, egal wie schmal.
  const tall = controlPointY(factory.geometry(300, 900).paths.pv);
  assert.ok(Number.isFinite(tall.ctrl), 'Hochformat liefert weiter einen gueltigen Pfad');
});

test('strokeWidth() scales monotonically with power and clamps to [2, 14]', () => {
  const { factory } = loadEnergyFlow();
  const reference = 1000;
  const zero = factory.strokeWidth(0, reference);
  const half = factory.strokeWidth(500, reference);
  const full = factory.strokeWidth(1000, reference);
  const overshoot = factory.strokeWidth(5000, reference);
  assert.equal(zero, 2);
  assert.ok(half > zero && half < full, 'strictly increases with power');
  assert.equal(full, 14);
  assert.equal(overshoot, 14, 'clamps at the reference power');
});

test('flowDuration() reproduces the deleted Go test values at the old fixed 3000W reference', () => {
  const { factory } = loadEnergyFlow();
  const reference = 3000;
  assert.equal(factory.flowDuration(0, reference).toFixed(2), '2.20');
  assert.equal(factory.flowDuration(1500, reference).toFixed(2), '1.28');
  assert.equal(factory.flowDuration(-5000, reference).toFixed(2), '0.35');
});

test('nodeText() prioritizes directional roles and reports staleness of the winning role', () => {
  const { factory } = loadEnergyFlow();
  // nodeText() runs inside the JSDOM vm context, so its return value has that
  // realm's Object prototype - round-trip through JSON before deepEqual,
  // same workaround devicemap.page.test.mjs uses for cross-realm objects.
  const plain = result => JSON.parse(JSON.stringify(result));
  assert.deepEqual(plain(factory.nodeText('pv', snapshotWith({ pv: 420 }))), { text: '420 W', stale: false, dir: 'producing' });
  assert.deepEqual(plain(factory.nodeText('pv', snapshotWith({}))), { text: '--', stale: false, dir: 'idle' });
  assert.deepEqual(
    plain(factory.nodeText('grid', snapshotWith({ grid_import: 1500 }, [{ role: 'grid_import', freshness: 'stale' }]))),
    { text: '1500 W', stale: true, dir: 'importing', full: 'Bezug 1500 W' },
  );
  assert.deepEqual(plain(factory.nodeText('grid', snapshotWith({ grid_export: 300 }))), { text: '300 W', stale: false, dir: 'exporting', full: 'Einspeisung 300 W' });
  assert.deepEqual(plain(factory.nodeText('load', snapshotWith({ load: 900 }))), { text: 'Hausverbrauch', stale: false, dir: 'drawing', icon: 'generic' });
  assert.deepEqual(plain(factory.nodeText('load', snapshotWith({ wallbox: 1100, load: 900 }))), { text: '1100 W', stale: false, dir: 'drawing', icon: 'wallbox', full: 'Wallbox 1100 W' });
});

// Seit das Wort "Laden"/"Entladen" durch das animierte Icon ersetzt wird
// (energy-flow-battery-icon), traegt nodeText("battery") die Richtung als
// eigenes `dir`-Feld statt im Text - `text` bleibt reine Zahl(en), `full` ist
// der frueher sichtbare Satz und wandert als aria-label ans <output>.
test('nodeText() returns the battery state of charge as its own `soc` field, not appended to the power text', () => {
  const { factory } = loadEnergyFlow();
  const plain = result => JSON.parse(JSON.stringify(result));
  assert.deepEqual(
    plain(factory.nodeText('battery', snapshotWith({ battery_discharge: 500, battery_soc: 62 }))),
    { text: '500 W', stale: false, dir: 'discharging', soc: '62 %', full: 'Entladen 500 W · Füllstand 62 %' },
  );
  assert.deepEqual(
    plain(factory.nodeText('battery', snapshotWith({ battery_charge: 500, battery_soc: 8 }))),
    { text: '500 W', stale: false, dir: 'charging', soc: '8 %', full: 'Laden 500 W · Füllstand 8 %' },
  );
  // No power role at all, but a state of charge is known - text stays "--",
  // soc carries the value, full names it without a charge/discharge word.
  assert.deepEqual(
    plain(factory.nodeText('battery', snapshotWith({ battery_soc: 45 }))),
    { text: '--', stale: false, dir: 'idle', soc: '45 %', full: 'Füllstand 45 %' },
  );
  // No battery_soc role configured (e.g. no capacity_kwh assigned anywhere) - soc is empty.
  assert.deepEqual(
    plain(factory.nodeText('battery', snapshotWith({ battery_discharge: 500 }))),
    { text: '500 W', stale: false, dir: 'discharging', soc: '', full: 'Entladen 500 W' },
  );
  // A stale battery_soc marks the node stale too, even if the power role itself is fresh.
  assert.deepEqual(
    plain(factory.nodeText('battery', snapshotWith({ battery_discharge: 500, battery_soc: 62 }, [{ role: 'battery_soc', freshness: 'stale' }]))),
    { text: '500 W', stale: true, dir: 'discharging', soc: '62 %', full: 'Entladen 500 W · Füllstand 62 %' },
  );
  // Keine Batterie zugeordnet: dir bleibt "idle", full == text == "--".
  assert.deepEqual(
    plain(factory.nodeText('battery', snapshotWith({}))),
    { text: '--', stale: false, dir: 'idle', soc: '', full: '--' },
  );
});

test('nodeText("battery") picks the actually active role by power, not just by which role is assigned', () => {
  const { factory } = loadEnergyFlow();
  const plain = result => JSON.parse(JSON.stringify(result));
  // Both battery_charge and battery_discharge are assigned (two separate sensors),
  // but only charging is actually happening - the idle discharge sensor still
  // reports a value (0), so hasValue('battery_discharge') is always true and must
  // not win over the role that is actually carrying power.
  assert.deepEqual(
    plain(factory.nodeText('battery', snapshotWith({ battery_charge: 500, battery_discharge: 0 }))),
    { text: '500 W', stale: false, dir: 'charging', soc: '', full: 'Laden 500 W' },
  );
  assert.deepEqual(
    plain(factory.nodeText('battery', snapshotWith({ battery_charge: 0, battery_discharge: 500 }))),
    { text: '500 W', stale: false, dir: 'discharging', soc: '', full: 'Entladen 500 W' },
  );
});

test('nodeText("battery") nets simultaneous charge and discharge instead of reporting the discharge side alone', () => {
  const { factory } = loadEnergyFlow();
  const plain = result => JSON.parse(JSON.stringify(result));
  assert.deepEqual(
    plain(factory.nodeText('battery', snapshotWith({ battery_charge: 800, battery_discharge: 300 }))),
    { text: '500 W', stale: false, dir: 'charging', soc: '', full: 'Laden 500 W' },
  );
  assert.deepEqual(
    plain(factory.nodeText('battery', snapshotWith({ battery_charge: 300, battery_discharge: 800 }))),
    { text: '500 W', stale: false, dir: 'discharging', soc: '', full: 'Entladen 500 W' },
  );
});

test('simultaneous charge and discharge roles animate one netted line; its direction follows the sign of the net', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ battery_charge: 800, battery_discharge: 300 }));
  component.init();
  const battery = root.querySelector('[data-flow="battery"]');
  assert.match(battery.getAttribute('class'), /is-active/);
  assert.match(battery.getAttribute('class'), /is-reversed/, 'Netto +500 W = Laden = home->battery');
});

test('a net that flips sign flips the line direction, not a second arrow', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ battery_charge: 300, battery_discharge: 800 }));
  component.init();
  const battery = root.querySelector('[data-flow="battery"]');
  assert.match(battery.getAttribute('class'), /is-active/);
  assert.doesNotMatch(battery.getAttribute('class'), /is-reversed/, 'Netto -500 W = Entladen = battery->home');
});

test('nodeText("home") zeigt den gerechneten Hausverbrauch, wenn keine Rolle "load" zugeordnet ist', () => {
  const { factory } = loadEnergyFlow();
  const plain = result => JSON.parse(JSON.stringify(result));
  const snapshot = {
    ...snapshotWith({ pv: 640, grid: 511, battery: -130 }),
    balance: { load_total: 1021, load_source: 'calculated' },
  };
  assert.deepEqual(plain(factory.nodeText('home', snapshot)), { text: '1021 W', stale: false, dir: 'live' });
});

test('nodeText("home") bevorzugt die Bilanz auch dann, wenn eine Rolle "load" existiert', () => {
  const { factory } = loadEnergyFlow();
  // gap_mode "unknown_consumer" rechnet eine Luecke in den Hausverbrauch ein;
  // die Karte muss die Bilanzzahl zeigen, nicht den rohen Messwert darunter.
  const plain = result => JSON.parse(JSON.stringify(result));
  const snapshot = {
    ...snapshotWith({ load: 900 }),
    balance: { load_total: 1150, load_source: 'measured' },
  };
  assert.deepEqual(plain(factory.nodeText('home', snapshot)), { text: '1150 W', stale: false, dir: 'live' });
});

test('nodeText("home") bleibt leer, wenn load_mode "measured" ohne zugeordnete Rolle gewaehlt ist', () => {
  const { factory } = loadEnergyFlow();
  const plain = result => JSON.parse(JSON.stringify(result));
  const snapshot = { ...snapshotWith({ pv: 640 }), balance: { load_total: 0, load_source: 'missing' } };
  assert.deepEqual(plain(factory.nodeText('home', snapshot)), { text: '--', stale: false, dir: 'idle' });
});

test('nodeText("home") meldet Veraltung der beitragenden Rollen, wenn der Wert gerechnet ist', () => {
  const { factory } = loadEnergyFlow();
  const snapshot = {
    ...snapshotWith({ pv: 640, grid: 511 }, [{ role: 'pv', freshness: 'stale' }]),
    balance: { load_total: 1151, load_source: 'calculated' },
  };
  assert.equal(factory.nodeText('home', snapshot).stale, true);
});

test('eine vorzeichenbehaftete grid-Rolle animiert den Netzbezug statt neutral zu bleiben', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ grid: 511 }));
  component.init();
  const grid = root.querySelector('[data-flow="grid"]');
  assert.match(grid.getAttribute('class'), /is-active/);
  assert.doesNotMatch(grid.getAttribute('class'), /is-neutral/);
  assert.doesNotMatch(grid.getAttribute('class'), /is-reversed/, 'Bezug laeuft grid->home');
});

test('eine negative grid-Rolle animiert die Einspeisung', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ grid: -1400 }));
  component.init();
  const grid = root.querySelector('[data-flow="grid"]');
  assert.match(grid.getAttribute('class'), /is-active/);
  assert.match(grid.getAttribute('class'), /is-reversed/, 'Einspeisung laeuft home->grid');
});

test('eine grid-Rolle bei genau 0 W zeichnet die neutrale Verbindung', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ grid: 0 }));
  component.init();
  const grid = root.querySelector('[data-flow="grid"]');
  assert.match(grid.getAttribute('class'), /is-neutral/);
  assert.doesNotMatch(grid.getAttribute('class'), /is-active/);
});

test('nodeText("grid") benennt die Richtung auch bei kombinierter grid-Rolle', () => {
  const { factory } = loadEnergyFlow();
  const plain = result => JSON.parse(JSON.stringify(result));
  assert.deepEqual(plain(factory.nodeText('grid', snapshotWith({ grid: 511 }))), { text: '511 W', stale: false, dir: 'importing', full: 'Bezug 511 W' });
  assert.deepEqual(plain(factory.nodeText('grid', snapshotWith({ grid: -1400 }))), { text: '1400 W', stale: false, dir: 'exporting', full: 'Einspeisung 1400 W' });
  assert.deepEqual(plain(factory.nodeText('grid', snapshotWith({ grid: 0 }))), { text: '0 W', stale: false, dir: 'idle' });
});

test('der Lastfluss traegt den gesamten Hausverbrauch, nicht den groessten Einzelverbraucher', () => {
  const { factory, window, document } = loadEnergyFlow();
  const plain = result => JSON.parse(JSON.stringify(result));
  const snapshot = {
    ...snapshotWith({ wallbox: 45 }),
    balance: { load_total: 1021, load_source: 'calculated' },
  };
  const { component, root } = buildComponent(window, document, factory, snapshot);
  component.init();
  const path = root.querySelector('[data-flow="load"]');
  assert.match(path.getAttribute('class'), /is-active/);
});

test('init() reads the embedded snapshot and draws the graphic immediately', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component } = buildComponent(window, document, factory, snapshotWith({ pv: 420 }));
  component.init();
  assert.deepEqual(JSON.parse(JSON.stringify(component.snapshot)), snapshotWith({ pv: 420 }));
  assert.ok(component.$refs.svg.children.length > 0, 'graphic is drawn on init');
});

test('render() draws in "width" scale mode by default: stroke-width varies with power, duration stays unset', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component } = buildComponent(window, document, factory, snapshotWith({ pv: 3000, load: 500 }));
  component.init();
  const svg = component.$refs.svg;
  const pv = svg.querySelector('[data-flow="pv"]');
  const load = svg.querySelector('[data-flow="load"]');
  assert.ok(pv.classList.contains('is-active'));
  assert.notEqual(pv.style.getPropertyValue('stroke-width'), '');
  assert.equal(pv.style.getPropertyValue('--energy-flow-duration'), '');
  assert.notEqual(
    pv.style.getPropertyValue('stroke-width'),
    load.style.getPropertyValue('stroke-width'),
    'the larger PV flow gets a thicker stroke than the smaller load flow',
  );
});

// Vorher ungetestet: render() im Modus "speed" lief nur ueber
// flowDuration()'s reine Funktion, nie ueber den echten DOM-Pfad (scaleMode
// und Referenz beide aus dem [data-layout-item-id]-Vorfahren) - siehe
// energiegrafiken.md "Bekannte Einschraenkungen". Die Regression, die
// diese Tests abdecken: eine feste 3000W-Referenz liess kleine Anlagen (z.B.
// Balkonkraftwerk-Groessenordnung, alle Werte weit unter 3000W) mit fast
// ununterscheidbaren Dauern animieren.
test('render() in "speed" scale mode defaults to a relative reference, so two small flows still get clearly different durations', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ pv: 200, load: 800 }));
  wrapInLayoutItem(document, root, 'speed');
  component.init();
  const svg = component.$refs.svg;
  const pv = svg.querySelector('[data-flow="pv"]');
  const load = svg.querySelector('[data-flow="load"]');
  assert.equal(pv.style.getPropertyValue('stroke-width'), '', 'speed mode leaves stroke-width unset');
  // reference = max(1000W floor, groesster aktiver Fluss 800W) = 1000W
  assert.equal(pv.style.getPropertyValue('--energy-flow-duration'), '1.83s');
  assert.equal(load.style.getPropertyValue('--energy-flow-duration'), '0.72s');
});

test('render() in "speed" scale mode raises the relative reference above its floor when a flow exceeds it', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ pv: 200, load: 2000 }));
  wrapInLayoutItem(document, root, 'speed', undefined, '500');
  component.init();
  const svg = component.$refs.svg;
  const pv = svg.querySelector('[data-flow="pv"]');
  const load = svg.querySelector('[data-flow="load"]');
  // reference = max(500W floor, groesster aktiver Fluss 2000W) = 2000W
  assert.equal(pv.style.getPropertyValue('--energy-flow-duration'), '2.02s');
  assert.equal(load.style.getPropertyValue('--energy-flow-duration'), '0.35s', 'clamped to the minimum duration at the reference power');
});

test('render() in "speed" scale mode uses a fixed reference when configured, regardless of the active flows', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ pv: 1500 }));
  wrapInLayoutItem(document, root, 'speed', 'fixed', '3000');
  component.init();
  const svg = component.$refs.svg;
  const pv = svg.querySelector('[data-flow="pv"]');
  assert.equal(pv.style.getPropertyValue('--energy-flow-duration'), '1.28s');
});

test('render() positions the five nodes and updates their text from the snapshot', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component } = buildComponent(window, document, factory, snapshotWith({ pv: 420 }));
  component.init();
  const pvArticle = component.$refs.map.querySelector('[data-node="pv"]');
  assert.equal(pvArticle.querySelector('output').textContent, '420 W');
  assert.notEqual(pvArticle.style.left, '');
  assert.notEqual(pvArticle.style.top, '');
});

test('a snapshot without any role values activates no flow in the live graphic', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component } = buildComponent(window, document, factory, snapshotWith({}));
  component.init();
  const svg = component.$refs.svg;
  for (const id of FLOW_IDS) {
    const path = svg.querySelector(`[data-flow="${id}"]`);
    assert.equal(path.classList.contains('is-active'), false, `${id} inactive`);
    assert.equal(path.classList.contains('is-neutral'), false, `${id} not neutral`);
  }
});

test('generic battery < 0 zeichnet die Batterielinie als Entladung: aktiv, in Vorwaertsrichtung', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component } = buildComponent(window, document, factory, snapshotWith({ battery: -150 })); // negativ = Entladung, per Snapshot.BatteryDischargePower()
  component.init();
  const battery = component.$refs.svg.querySelector('[data-flow="battery"]');
  assert.ok(battery.classList.contains('is-active'));
  assert.equal(battery.classList.contains('is-reversed'), false, 'Entladen = battery->home');
  assert.equal(battery.classList.contains('is-neutral'), false);
});

test('generic battery > 0 zeichnet die Batterielinie als Ladung (umgekehrt), und eine 0-W-Wallbox aktiviert den Verbrauch nicht', () => {
  const { factory, window, document } = loadEnergyFlow();
  // positiv = Laden, per Snapshot.BatteryChargePower(); wallbox: 0 und sonst
  // keine last-artige Rolle - die Verbrauchslinie muss inaktiv bleiben.
  const { component } = buildComponent(window, document, factory, snapshotWith({ battery: 150, wallbox: 0 }));
  component.init();
  const svg = component.$refs.svg;
  const battery = svg.querySelector('[data-flow="battery"]');
  const load = svg.querySelector('[data-flow="load"]');
  assert.ok(battery.classList.contains('is-active'));
  assert.ok(battery.classList.contains('is-reversed'), 'Laden = home->battery');
  assert.equal(load.classList.contains('is-active'), false);
});

test('die Flusskarte liest den geteilten Schnappschuss, wenn kein eigener da ist', () => {
  const { factory, window, document } = loadEnergyFlow();
  document.body.innerHTML = `
    <section id="energy-flow-card">
      <div class="energy-flow-map"><svg class="energy-flow-lines"></svg></div>
    </section>
    <script type="application/json" id="energy-snapshot-initial">${JSON.stringify(snapshotWith({ pv: 42 }))}</script>
  `;
  const root = document.querySelector('#energy-flow-card');
  const component = factory();
  component.$root = root;
  component.$refs = { map: root.querySelector('.energy-flow-map'), svg: root.querySelector('svg') };
  component.$nextTick = fn => fn();
  Object.defineProperty(component.$refs.map, 'clientWidth', { value: 800, configurable: true });
  Object.defineProperty(component.$refs.map, 'clientHeight', { value: 400, configurable: true });
  window.matchMedia = () => ({ matches: false });
  component.init();
  assert.equal(component.snapshot.values.pv, 42);
});

test('der Lastknoten nennt im Kombiniert-Modus den gemessenen Teilverbrauch', () => {
  const { factory } = loadEnergyFlow();
  const snapshot = {
    values: {pv: 3000, load: 900, wallbox: 500},
    balance: {load_total: 3000, load_source: 'combined', load_measured: 900},
  };
  assert.equal(factory.nodeText('load', snapshot).text, '900 W');
  assert.match(factory.nodeText('load', snapshot).full, /Gemessen 900 W/);
});

// --- Kontinuitaet ueber den htmx-Tausch ---------------------------------
//
// dashboard.js's refreshLiveFragment() ersetzt #overview-live per outerHTML,
// diese Karte wird also bei jedem Live-Update komplett neu montiert. Die
// Tests unten bilden genau das ab: zweimal buildComponent() auf dasselbe
// Dokument, das zweite Markup verdraengt das erste. Was dabei sichtbar
// zusammenhaengend bleiben muss, steht in energy-flow.js's Modulblock
// "Darstellungszustand, der den htmx-Tausch ueberlebt".

// performance.now() ist die Uhr, an der Federn und Laufschrift-Phase haengen.
// Ohne Stellschraube darauf haengen die Erwartungen unten an der realen
// Laufzeit zwischen zwei init()-Aufrufen.
function stubClock(window) {
  let current = 0;
  Object.defineProperty(window.performance, 'now', { value: () => current, configurable: true });
  return { advance: ms => { current += ms; } };
}

const outputText = (root, node) => root.querySelector(`[data-node="${node}"] output`).textContent;
const opacityOf = (root, flow) => Number(root.querySelector(`[data-flow="${flow}"]`).style.getPropertyValue('--energy-flow-opacity'));

// Laesst die Modulschleife von Hand laufen: requestAnimationFrame gibt es in
// JSDOM nicht, step() ist aber dieselbe Funktion, die sie aufrufen wuerde.
function settle(component, frames = 600) {
  for (let frame = 1; frame <= frames; frame += 1) {
    if (!component.step(1 / 60, frame)) return frame;
  }
  return null;
}

test('nach dem Tausch zeichnet die Karte zuerst den zuletzt gezeigten Wert, nicht den neuen', () => {
  const { factory, window, document } = loadEnergyFlow();
  const first = buildComponent(window, document, factory, snapshotWith({ pv: 400 }));
  first.component.init();
  assert.equal(outputText(first.root, 'pv'), '400 W');

  const second = buildComponent(window, document, factory, snapshotWith({ pv: 1600 }));
  second.component.init();
  assert.equal(outputText(second.root, 'pv'), '400 W', 'startet beim Darstellungswert der Vorgaengerin, nicht beim Ziel');
});

test('die Federn laufen nach dem Tausch zum neuen Wert und rasten dort ein', () => {
  const { factory, window, document } = loadEnergyFlow();
  buildComponent(window, document, factory, snapshotWith({ pv: 400 })).component.init();
  const second = buildComponent(window, document, factory, snapshotWith({ pv: 1600 }));
  second.component.init();

  second.component.step(1 / 60, 1);
  const intermediate = Number.parseInt(outputText(second.root, 'pv'), 10);
  assert.ok(intermediate > 400 && intermediate < 1600, `Zwischenwert ${intermediate} liegt zwischen altem und neuem Wert`);

  assert.ok(settle(second.component), 'schwingt ein, statt endlos zu laufen');
  assert.equal(outputText(second.root, 'pv'), '1600 W', 'landet exakt auf dem Ziel');
});

test('der allererste Mount rastet ein, statt die ganze Seite aus der Null hochlaufen zu lassen', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ pv: 1600 }));
  component.init();
  assert.equal(outputText(root, 'pv'), '1600 W');
});

test('die Laufschrift laeuft nach dem Neuaufbau weiter, statt auf Phase 0 zurueckzuspringen', () => {
  const { factory, window, document } = loadEnergyFlow();
  const clock = stubClock(window);
  const first = buildComponent(window, document, factory, snapshotWith({ pv: 400 }));
  first.component.init();
  // Rueckfalldauer aus base.css (`var(--energy-flow-duration, 1.4s)`): der
  // erste Mount beginnt zwangslaeufig am Anfang des Umlaufs.
  assert.equal(first.root.querySelector('[data-flow="pv"]').style.animationDelay, '0.000s');

  clock.advance(700); // ein halber Umlauf
  const second = buildComponent(window, document, factory, snapshotWith({ pv: 400 }));
  second.component.init();
  assert.equal(
    second.root.querySelector('[data-flow="pv"]').style.animationDelay,
    '-0.700s',
    'negative Verzoegerung spult die frische CSS-Animation auf die halbe Phase vor',
  );
});

test('ein Richtungswechsel der Batterie laeuft als Bewegung durch die Null, statt die Laufrichtung hart umzuschalten', () => {
  const { factory, window, document } = loadEnergyFlow();
  buildComponent(window, document, factory, snapshotWith({ battery: 800 })).component.init();
  const second = buildComponent(window, document, factory, snapshotWith({ battery: -800 }));
  second.component.init();

  const battery = second.root.querySelector('[data-flow="battery"]');
  // Erstes Bild nach dem Tausch: der dargestellte Wert steht noch bei ~800 W,
  // die eine Batterielinie zeigt also weiter die Laderichtung der Vorgaengerin.
  assert.match(battery.getAttribute('class'), /is-active/);
  assert.match(battery.getAttribute('class'), /is-reversed/, 'startet in der Laderichtung der Vorgaengerin');

  // Auf dem Weg zu -800 W federt der dargestellte Wert stetig durch die Null,
  // statt von +800 auf -800 zu springen: der Betrag kommt unterwegs nahe an
  // Null, und die Laufrichtung dreht dabei um. Ein harter Tausch haette
  // keinen dieser Zwischenwerte.
  let minAbs = Infinity;
  let flippedWhileSmall = false;
  for (let frame = 1; frame <= 600; frame += 1) {
    const moving = second.component.step(1 / 60, frame);
    const abs = Math.abs(Number.parseInt(outputText(second.root, 'battery'), 10));
    minAbs = Math.min(minAbs, abs);
    if (abs < 200 && !/is-reversed/.test(battery.getAttribute('class'))) flippedWhileSmall = true;
    if (!moving) break;
  }
  assert.ok(minAbs < 60, `der Betrag kommt beim Nulldurchgang nahe an Null (min ${minAbs})`);
  assert.ok(flippedWhileSmall, 'die Laufrichtung dreht waehrend des Nulldurchgangs, nicht als Sprung am Ende');
  assert.match(battery.getAttribute('class'), /is-active/);
  assert.doesNotMatch(battery.getAttribute('class'), /is-reversed/, 'endet in der Entladerichtung');
  assert.equal(outputText(second.root, 'battery'), '800 W');
});

test('das Batterie-Icon zeigt Lade-/Entladerichtung als Klasse, das Wort steht nur noch im aria-label', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ battery_charge: 500 }));
  window.matchMedia = () => ({ matches: true }); // reduzierte Bewegung: keine Feder abwarten muessen
  component.init();

  const icon = root.querySelector('.energy-flow-battery-icon');
  const output = root.querySelector('[data-node="battery"] output');
  assert.match(icon.getAttribute('class'), /is-charging/);
  assert.doesNotMatch(icon.getAttribute('class'), /is-discharging/);
  assert.equal(output.getAttribute('aria-label'), 'Laden 500 W');

  window.EnergyPresentation.publish(snapshotWith({ battery_discharge: 300 }));

  assert.doesNotMatch(icon.getAttribute('class'), /is-charging/);
  assert.match(icon.getAttribute('class'), /is-discharging/);
  assert.equal(output.getAttribute('aria-label'), 'Entladen 300 W');
});

test('jeder Knoten traegt seinen Zustand als Icon-Klasse: is-producing/-importing/-exporting/-live/-drawing', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, {
    ...snapshotWith({ pv: 800, grid_import: 400, wallbox: 1500 }),
    balance: { load_total: 2000, load_source: 'calculated' },
  });
  window.matchMedia = () => ({ matches: true }); // reduzierte Bewegung: Federn stehen sofort auf dem Ziel
  component.init();

  const cls = id => root.querySelector(`[data-node="${id}"] .energy-flow-node-icon`).getAttribute('class');
  assert.match(cls('pv'), /is-producing/);
  assert.match(cls('grid'), /is-importing/);
  assert.doesNotMatch(cls('grid'), /is-exporting/);
  assert.match(cls('home'), /is-live/);
  assert.match(cls('load'), /is-drawing/);
});

test('das Lasten-Icon folgt dem Verbraucher mit der hoechsten Leistung und tauscht das SVG', () => {
  const { factory } = loadEnergyFlow();
  // Text und icon-Feld folgen beide dem groesseren der beiden Verbraucher.
  const wallboxWins = factory.nodeText('load', snapshotWith({ wallbox: 1800, heat_pump: 600 }));
  assert.equal(wallboxWins.icon, 'wallbox');
  assert.equal(wallboxWins.text, '1800 W');
  assert.match(wallboxWins.full, /Wallbox 1800 W/);
  const pumpWins = factory.nodeText('load', snapshotWith({ wallbox: 300, heat_pump: 900 }));
  assert.equal(pumpWins.icon, 'heatpump');
  assert.equal(pumpWins.text, '900 W');
  assert.match(pumpWins.full, /Wärmepumpe 900 W/);
});

test('render() setzt das passende Lasten-SVG ins <output> und wechselt es bei Verbraucherwechsel', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ wallbox: 1500, heat_pump: 200 }));
  window.matchMedia = () => ({ matches: true });
  component.init();
  const icon = root.querySelector('[data-node="load"] .energy-flow-node-icon');
  assert.equal(icon.dataset.loadIcon, 'wallbox');
  assert.ok(icon.querySelector('.efn-car'), 'Wallbox-Grafik ist eingesetzt');

  window.EnergyPresentation.publish(snapshotWith({ wallbox: 100, heat_pump: 1400 }));
  assert.equal(icon.dataset.loadIcon, 'heatpump');
  assert.ok(icon.querySelector('.efn-rotor'), 'Waermepumpen-Grafik ist eingesetzt');
  assert.match(icon.getAttribute('class'), /is-drawing/);
});

test('loadConsumers() listet die benannten Verbraucher plus eine Sammelzeile fuer den Rest', () => {
  const { factory } = loadEnergyFlow();
  const snapshot = {
    ...snapshotWith({ wallbox: 1500, heat_pump: 400 }),
    balance: { load_total: 2400, load_source: 'calculated' },
  };
  const rows = JSON.parse(JSON.stringify(factory.loadConsumers(snapshot, false)))
    .map(r => [r.label, r.icon, r.power, r.dir]);
  assert.deepEqual(rows, [
    ['Wallbox', 'wallbox', 1500, 'drawing'],
    ['Übriger Verbrauch', 'generic', 500, 'drawing'], // 2400 - 1500 - 400
    ['Wärmepumpe', 'heatpump', 400, 'drawing'],
  ]);
});

test('loadConsumers(hideInactive) wirft Verbraucher unter der Schwelle raus', () => {
  const { factory } = loadEnergyFlow();
  const snapshot = {
    ...snapshotWith({ wallbox: 0, heat_pump: 900 }),
    balance: { load_total: 900, load_source: 'calculated' },
  };
  const labels = (hide) => JSON.parse(JSON.stringify(factory.loadConsumers(snapshot, hide))).map(r => r.label);
  assert.deepEqual(labels(false), ['Wärmepumpe', 'Wallbox']);
  assert.deepEqual(labels(true), ['Wärmepumpe']);
});

test('die Lasten-Kachel rendert eine <li>-Zeile je Verbraucher', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, {
    ...snapshotWith({ wallbox: 1500, heat_pump: 900 }),
    balance: { load_total: 2600, load_source: 'calculated' },
  });
  window.matchMedia = () => ({ matches: true });
  component.init();
  const rows = root.querySelectorAll('[data-node="load"] .energy-flow-load-row');
  assert.equal(rows.length, 3, 'Wallbox + Wärmepumpe + Sammelzeile');
  assert.equal(rows[0].querySelector('.energy-flow-node-text').textContent, '1500 W');
  assert.match(rows[0].getAttribute('aria-label'), /Wallbox 1500 W/);
});

test('das Batterie-Icon friert im Ruhezustand auf den Fuellstand ein, auf 5 Stufen gerundet', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ battery: 0, battery_soc: 63 }));
  window.matchMedia = () => ({ matches: true });
  component.init();
  const icon = root.querySelector('.energy-flow-battery-icon');
  // 63 % -> round(63/20)/5 = 3/5 = 0.60
  assert.equal(icon.style.getPropertyValue('--energy-battery-soc'), '0.60');
  assert.doesNotMatch(icon.getAttribute('class') || '', /is-charging|is-discharging/);
});

test('der Batterie-Fuellstand steht in seiner eigenen Zeile und verschwindet ohne battery_soc', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ battery_discharge: 500, battery_soc: 63 }));
  window.matchMedia = () => ({ matches: true });
  component.init();
  const socEl = root.querySelector('[data-node="battery"] .energy-flow-battery-soc');
  const textEl = root.querySelector('[data-node="battery"] .energy-flow-battery-text');
  assert.equal(textEl.textContent, '500 W', 'der Leistungswert bleibt eine reine Zahl');
  assert.equal(socEl.textContent, '63 %');
  assert.equal(socEl.hidden, false);

  window.EnergyPresentation.publish(snapshotWith({ battery_discharge: 500 }));
  assert.equal(socEl.textContent, '');
  assert.equal(socEl.hidden, true);
});

test('das Netz-Icon wechselt is-importing/is-exporting mit der Flussrichtung', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ grid_import: 900 }));
  window.matchMedia = () => ({ matches: true }); // reduzierte Bewegung: keine Feder abwarten muessen
  component.init();
  const icon = root.querySelector('[data-node="grid"] .energy-flow-node-icon');
  assert.match(icon.getAttribute('class'), /is-importing/);

  window.EnergyPresentation.publish(snapshotWith({ grid_export: 900 }));
  assert.doesNotMatch(icon.getAttribute('class'), /is-importing/);
  assert.match(icon.getAttribute('class'), /is-exporting/);
});

test('das Haus-Icon laeuft bei gemessen/berechnet/kombiniert und steht nur bei "nicht zugeordnet" still', () => {
  const { factory } = loadEnergyFlow();
  for (const source of ['measured', 'calculated', 'combined']) {
    const snapshot = { ...snapshotWith({ pv: 640 }), balance: { load_total: 1000, load_source: source } };
    assert.equal(factory.nodeText('home', snapshot).dir, 'live', `dir bei load_source=${source}`);
  }
  const missing = { ...snapshotWith({ pv: 640 }), balance: { load_total: 0, load_source: 'missing' } };
  assert.equal(factory.nodeText('home', missing).dir, 'idle');
});

test('nodeText().dir meldet Ruhe, wenn der gezeigte Wert null ist', () => {
  const { factory } = loadEnergyFlow();
  assert.equal(factory.nodeText('pv', snapshotWith({ pv: 0 })).dir, 'idle');
  assert.equal(factory.nodeText('grid', snapshotWith({ grid_import: 0 })).dir, 'idle');
  assert.equal(factory.nodeText('load', snapshotWith({ wallbox: 0 })).dir, 'idle');
  assert.equal(factory.nodeText('load', snapshotWith({ wallbox: 1200 })).dir, 'drawing');
});

test('bei reduzierter Bewegung steht der neue Messwert sofort richtig da', () => {
  const { factory, window, document } = loadEnergyFlow();
  buildComponent(window, document, factory, snapshotWith({ pv: 400 })).component.init();
  const second = buildComponent(window, document, factory, snapshotWith({ pv: 1600 }));
  window.matchMedia = query => ({ matches: query.includes('reduced-motion') });
  second.component.init();
  assert.equal(outputText(second.root, 'pv'), '1600 W', 'kein Einschwingen, der Wert ist die Information');
  assert.equal(second.root.querySelector('[data-flow="pv"]').style.animationDelay, '', 'und keine Laufschrift, die verankert werden muesste');
});

test('eine Rolle, die erst spaeter auftaucht, steigt aus der Null hoch statt aufzublitzen', () => {
  const { factory, window, document } = loadEnergyFlow();
  buildComponent(window, document, factory, snapshotWith({ pv: 400 })).component.init();
  const second = buildComponent(window, document, factory, snapshotWith({ pv: 400, wallbox: 2000 }));
  second.component.init();
  assert.equal(outputText(second.root, 'load'), '0 W');
  assert.ok(settle(second.component));
  assert.equal(outputText(second.root, 'load'), '2000 W');
});

test('eine verschwundene Rolle laesst keine Feder zurueck, die den naechsten Wert verzerrt', () => {
  const { factory, window, document } = loadEnergyFlow();
  buildComponent(window, document, factory, snapshotWith({ pv: 400, wallbox: 2000 })).component.init();
  buildComponent(window, document, factory, snapshotWith({ pv: 400 })).component.init();
  // wallbox ist weg und muss beim Wiederauftauchen wie eine neue Rolle
  // anfangen - nicht bei den 2000 W von vorhin.
  const third = buildComponent(window, document, factory, snapshotWith({ pv: 400, wallbox: 50 }));
  third.component.init();
  assert.equal(outputText(third.root, 'load'), '0 W');
});

// Regression: seit dashboard.js den vollen #overview-live-Tausch bei reinen
// Energie-Layouts ueberspringt (liveGridPushCovers()), ist
// EnergyPresentation.publish() der einzige Weg, wie diese Karte je wieder
// einen frischen Schnappschuss sieht - init() liest ihn nur einmal beim
// ersten Mount aus dem eingebetteten Script.
test('die Karte bekommt neue Werte ueber EnergyPresentation.publish(), nicht nur beim Mount', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ pv: 400 }));
  // Reduzierte Bewegung: der gepushte Wert steht sofort da, ohne dass der
  // Test die Feder erst manuell einschwingen lassen muss.
  window.matchMedia = () => ({ matches: true });
  component.init();
  assert.equal(outputText(root, 'pv'), '400 W');

  window.EnergyPresentation.publish(snapshotWith({ pv: 900 }));

  assert.equal(outputText(root, 'pv'), '900 W', 'liest den gepushten Schnappschuss, nicht nur den beim Mount eingebetteten');
});

// Nach dem Abriss (destroy(), z.B. bei einem verbleibenden vollen Tausch fuer
// eine andere Kachel im selben Raster) darf die alte, nicht mehr im Dokument
// stehende Karte nicht weiter auf publish() reagieren.
test('destroy() meldet die Karte von EnergyPresentation.publish() ab', () => {
  const { factory, window, document } = loadEnergyFlow();
  const { component, root } = buildComponent(window, document, factory, snapshotWith({ pv: 400 }));
  window.matchMedia = () => ({ matches: true });
  component.init();
  component.destroy();
  root.remove();

  window.EnergyPresentation.publish(snapshotWith({ pv: 900 }));

  assert.equal(outputText(root, 'pv'), '400 W', 'eine abgebaute Karte darf nicht mehr schreiben');
});

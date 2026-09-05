// Regression tests for energy-status.js: statusLead()/beamGeometry()/
// statusTiles() are pure ports of Vorschlag F ("Statuskarte") from the
// six-proposals exploration. Two of its four prototype options
// (Überschuss-/Netzbezug-Schwelle) moved to the Energie-Einstellungen
// (Interpretation.SurplusThresholdW/ImportThresholdW) instead of the layout
// editor - see knowhow/dashboard/energiegrafiken-konfiguration-backlog.md -
// so these pure functions take thresholds as a plain argument rather than
// reading a snapshot; beam_span and show_advice are covered at the
// component level further down.
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
const scriptSource = read('energy-status.js');

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

const DEFAULT_THRESHOLDS = { surplus: 800, import: 1500 };

test('statusLead reports a critical gap ahead of any surplus/import advice (diagnostic mode)', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 3100, load: 3100, heat_pump: 620, grid: 940}, roles: []}, {gap_mode: 'diagnostic'});
  const lead = factory.statusLead(balance, 0, DEFAULT_THRESHOLDS, true);
  assert.equal(lead.state, 'kritisch');
  assert.match(lead.title, /Bilanzlücke/);
});

test('statusLead does not report a gap once unknown_consumer mode has absorbed it', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 3100, load: 3100, heat_pump: 620, grid: 940}, roles: []});
  const lead = factory.statusLead(balance, 0, DEFAULT_THRESHOLDS, true);
  assert.notEqual(lead.state, 'kritisch');
});

test('statusLead reports stale roles when the balance closes but data is old', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 0, load: 0, grid: 0}, roles: []});
  const lead = factory.statusLead(balance, 2, DEFAULT_THRESHOLDS, true);
  assert.equal(lead.state, 'kritisch');
  assert.match(lead.title, /2 Rollen veraltet/);
});

test('statusLead reports surplus above the 800 W threshold as "gut"', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 6400, load: 620, battery: 2200, grid: -3580}, roles: []});
  const lead = factory.statusLead(balance, 0, DEFAULT_THRESHOLDS, true);
  assert.equal(lead.state, 'gut');
  assert.match(lead.title, /Überschuss/);
});

test('statusLead reports import above the 1500 W threshold as "Hinweis"', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 0, load: 1800, grid: 1800}, roles: []});
  const lead = factory.statusLead(balance, 0, DEFAULT_THRESHOLDS, true);
  assert.equal(lead.state, 'Hinweis');
  assert.match(lead.title, /aus dem Netz/);
});

test('statusLead falls back to "Ausgeglichen" below both thresholds', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 500, load: 600, grid: 100}, roles: []});
  const lead = factory.statusLead(balance, 0, DEFAULT_THRESHOLDS, true);
  assert.equal(lead.state, 'gut');
  assert.equal(lead.title, 'Ausgeglichen');
});

test('statusLead uses the passed-in thresholds instead of a fixed 800/1500 W', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 250, load: 100, grid: -150}, roles: []});
  const defaultLead = factory.statusLead(balance, 0, DEFAULT_THRESHOLDS, true);
  const lowerLead = factory.statusLead(balance, 0, { surplus: 100, import: 1500 }, true);
  assert.equal(defaultLead.title, 'Ausgeglichen'); // 150 W export stays below the 800 W default threshold
  assert.match(lowerLead.title, /Überschuss/);
});

test('statusLead show_advice=false swaps the actionable sentence for a bare state note', () => {
  const { factory, model } = load();
  const surplusBalance = model.deriveBalance({values: {pv: 6400, load: 620, battery: 2200, grid: -3580}, roles: []});
  const withAdvice = factory.statusLead(surplusBalance, 0, DEFAULT_THRESHOLDS, true);
  const withoutAdvice = factory.statusLead(surplusBalance, 0, DEFAULT_THRESHOLDS, false);
  assert.match(withAdvice.sub, /Guter Zeitpunkt/);
  assert.doesNotMatch(withoutAdvice.sub, /Guter Zeitpunkt/);
  assert.equal(withoutAdvice.title, withAdvice.title); // the state itself is unaffected

  const importBalance = model.deriveBalance({values: {pv: 0, load: 1800, grid: 1800}, roles: []});
  const importWithoutAdvice = factory.statusLead(importBalance, 0, DEFAULT_THRESHOLDS, false);
  assert.match(factory.statusLead(importBalance, 0, DEFAULT_THRESHOLDS, true).sub, /Verschiebbare Verbraucher/);
  assert.doesNotMatch(importWithoutAdvice.sub, /Verschiebbare Verbraucher/);
});

test('beamGeometry clamps to the track ends beyond the configured span', () => {
  const { factory, model } = load();
  const overImport = model.deriveBalance({values: {pv: 0, load: 20000, grid: 20000}, roles: []});
  const geometry = factory.beamGeometry(overImport, 6000);
  assert.equal(geometry.isImport, true);
  assert.equal(geometry.x, geometry.mid + (980 / 2 - 20));
});

test('beamGeometry reports "ausgeglichen" for a near-zero balance', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 500, load: 500}, roles: []});
  const geometry = factory.beamGeometry(balance, 6000);
  assert.equal(geometry.label, 'ausgeglichen');
});

test('statusTiles marks the battery tile stale from an isStale role', () => {
  const { factory, model } = load();
  const snapshot = {values: {pv: 0, load: 0, battery: 400}, roles: [{role: 'battery', freshness: 'stale'}]};
  const balance = model.deriveBalance(snapshot);
  const tiles = factory.statusTiles(balance, snapshot, 0, DEFAULT_THRESHOLDS);
  const battery = tiles.find(tile => tile.label === 'Batterie');
  assert.equal(battery.state, 'veraltet');
});

test('statusTiles nets simultaneous charge and discharge into one battery balance instead of dropping one side', () => {
  const { factory, model } = load();
  const snapshot = {values: {pv: 0, load: 0, battery_charge: 200, battery_discharge: 650}, roles: []};
  const balance = model.deriveBalance(snapshot);
  const tiles = factory.statusTiles(balance, snapshot, 0, DEFAULT_THRESHOLDS);
  const battery = tiles.find(tile => tile.label === 'Batterie');
  assert.equal(battery.value, model.formatPower(450));
  assert.match(battery.note, /entl/);
});

test('statusTiles reports "vollständig" data quality with no gap and no stale roles', () => {
  const { factory, model } = load();
  const snapshot = {values: {pv: 500, load: 500}, roles: []};
  const balance = model.deriveBalance(snapshot);
  const tiles = factory.statusTiles(balance, snapshot, 0, DEFAULT_THRESHOLDS);
  const quality = tiles.find(tile => tile.label === 'Datenqualität');
  assert.equal(quality.state, 'gut');
  assert.equal(quality.value, 'vollständig');
});

function cardMarkup(initialSnapshot, dataAttrs) {
  const attrs = Object.entries(dataAttrs || {}).map(([k, v]) => ` data-${k}="${v}"`).join('');
  return `
    <div data-layout-item-id="energy-status"${attrs}>
      <section id="energy-status-card">
        <svg><desc></desc></svg>
        <script type="application/json" id="energy-status-initial">${JSON.stringify(initialSnapshot)}</script>
      </section>
    </div>
  `;
}

function buildComponent(document, factory, initialSnapshot, dataAttrs) {
  document.body.innerHTML = cardMarkup(initialSnapshot, dataAttrs);
  const root = document.querySelector('#energy-status-card');
  const component = factory();
  component.$root = root;
  component.$refs = { beam: root.querySelector('svg') };
  return component;
}

test('init() reads beam_span/show_advice from the wrapper and applies them', () => {
  const { factory, document } = load();
  const snapshot = {values: {pv: 6400, load: 620, battery: 2200, grid: -3580}, roles: []};
  const component = buildComponent(document, factory, snapshot, {'beam-span': '3000', 'show-advice': 'off'});
  component.init();
  assert.equal(component.options.beamSpan, '3000');
  assert.equal(component.options.showAdvice, 'off');
  assert.doesNotMatch(component.lead.sub, /Guter Zeitpunkt/);
});

test('init() falls back to the defaults (6000, on) when no options are set on the wrapper', () => {
  const { factory, document } = load();
  const snapshot = {values: {pv: 6400, load: 620, battery: 2200, grid: -3580}, roles: []};
  const component = buildComponent(document, factory, snapshot, {});
  component.init();
  assert.equal(component.options.beamSpan, '6000');
  assert.equal(component.options.showAdvice, 'on');
});

test('the "thresholds" getter reads surplus_threshold_w/import_threshold_w from snapshot.interpretation', () => {
  const { factory, document } = load();
  const snapshot = {
    values: {pv: 900, load: 100, grid: -800}, roles: [],
    interpretation: {surplus_threshold_w: 100, import_threshold_w: 2000},
  };
  const component = buildComponent(document, factory, snapshot, {});
  component.init();
  // 800 W export clears the configured 100 W surplus threshold, so this must
  // read as a surplus - not "Ausgeglichen", which the 800 W built-in default would give.
  assert.match(component.lead.title, /Überschuss/);
});

test('the "thresholds" getter falls back to 800/1500 when snapshot.interpretation is absent', () => {
  const { factory, document } = load();
  const snapshot = {values: {pv: 250, load: 100, grid: -150}, roles: []};
  const component = buildComponent(document, factory, snapshot, {});
  component.init();
  assert.equal(component.lead.title, 'Ausgeglichen');
});

test('beamGeometry rechnet gegen die uebergebene Breite statt gegen feste 980', () => {
  const { factory, model } = load();
  // Netzbezug 3000 W bei einem Skalenende von 6000 W: der Zeiger steht auf
  // der halben Strecke zwischen Mitte und rechtem Anschlag.
  const balance = model.deriveBalance({values: {pv: 0, grid: 3000}, roles: []});
  const wide = factory.beamGeometry(balance, 6000, 980);
  const narrow = factory.beamGeometry(balance, 6000, 480);
  assert.equal(wide.mid, 490);
  assert.equal(wide.x, 490 + 0.5 * (490 - 20));   // 725
  assert.equal(narrow.mid, 240);
  assert.equal(narrow.x, 240 + 0.5 * (240 - 20)); // 350
});

test('beamGeometry behaelt 980 als Vorgabe', () => {
  const { factory, model } = load();
  const balance = model.deriveBalance({values: {pv: 0, grid: 0}, roles: []});
  assert.equal(factory.beamGeometry(balance, 6000).mid, 490);
});

test('renderBeam setzt die viewBox auf die gemessene Breite', () => {
  const { factory, window, document } = load();
  document.body.innerHTML = `
    <section id="energy-status-card">
      <div class="energy-status-beam">
        <svg><title></title><desc></desc></svg>
      </div>
      <script type="application/json" id="energy-status-initial">${JSON.stringify({values: {pv: 640, grid: 511}, roles: [], interpretation: {}})}</script>
    </section>`;
  const root = document.querySelector('#energy-status-card');
  const component = factory();
  component.$root = root;
  component.$refs = {
    beam: root.querySelector('svg'),
    beamWrap: root.querySelector('.energy-status-beam'),
  };
  Object.defineProperty(component.$refs.beamWrap, 'clientWidth', { value: 512, configurable: true });
  window.DashboardTheme.onChange = () => () => {};
  component.init();
  assert.equal(component.$refs.beam.getAttribute('viewBox'), '0 0 512 52');
});

test('die Statuskarte zeigt beim zweiten Schnappschuss zunaechst den alten Wert', () => {
  const { factory, window: win } = load();
  let now = 0;
  win.performance = { now: () => now };
  win.requestAnimationFrame = () => {};
  win.DashboardTheme.onChange = () => () => {};

  const mount = raw => {
    win.document.body.innerHTML = '<div data-layout-item-id="status-1"><div data-root></div></div>';
    const card = factory();
    card.$root = win.document.querySelector('[data-root]');
    card.$refs = {};
    card.snapshotOverride = raw;
    card.init();
    return card;
  };

  const first = mount({ values: { grid: 1000 }, roles: [] });
  assert.equal(first.snapshot.values.grid, 1000, 'der erste Aufbau steht sofort richtig da');
  first.destroy();

  const second = mount({ values: { grid: 3000 }, roles: [] });
  assert.equal(second.snapshot.values.grid, 1000, 'die neue Instanz startet beim sichtbaren Wert');
  second.destroy();
});

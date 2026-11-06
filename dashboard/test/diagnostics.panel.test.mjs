// Tests for the diagnostics panel component (diagnosticsPanel() in dashboard.js).
// Similar to devices.modal.test.mjs: jsdom setup, Alpine init, component extraction.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'dashboard.js'),
  'utf8',
);

function createDiagnosticsPanel() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: 'http://localhost/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, magic() {}, directive() {} };
  dom.window.setInterval = () => 0;
  installI18n(dom.window, {lang: 'en'});
  vm.runInContext(source, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  const panel = factories.diagnosticsPanel();
  Object.defineProperty(panel, 'testWindow', {value: dom.window});
  return panel;
}

test('warningText returns catalog value for a warning with key', () => {
  const panel = createDiagnosticsPanel();
  panel.testWindow.I18n.catalog['diagnostics.rule.offline.hint'] = "Check it's powered";
  const warning = {key: 'offline', hint: 'fallback hint'};
  assert.equal(panel.warningText(warning, 'hint'), "Check it's powered");
});

const sampleWarnings = () => [
  {rule_id: 'NoStateUpdate', key: 'no_state_update', severity: 'warning', device_id: 'wr', entity_id: 'wr_power'},
  {rule_id: 'NoStateUpdate', key: 'no_state_update', severity: 'warning', device_id: 'wr', entity_id: 'wr_soc'},
  {rule_id: 'NoStateUpdate', key: 'no_state_update', severity: 'warning', device_id: 'bms', entity_id: 'bms_voltage'},
  {rule_id: 'WrongUnit', key: 'missing_unit', severity: 'warning', device_id: 'bms', entity_id: 'bms_voltage'},
  {rule_id: 'Offline', key: 'offline', severity: 'critical', device_id: 'bms', entity_id: 'bms_voltage'},
];

test('warningGroups puts one group per cause, most urgent first', () => {
  const panel = createDiagnosticsPanel();
  panel.warnings = sampleWarnings();
  const groups = panel.warningGroups;
  assert.deepEqual([...groups.map(group => group.id)], ['offline', 'no_state_update', 'missing_unit']);
  assert.equal(groups[0].severity, 'critical');
  assert.equal(groups[0].title, 'Device offline');
  const stale = groups[1];
  assert.equal(stale.count, 3);
  assert.equal(stale.hint, panel.warningText({key: 'no_state_update'}, 'hint'));
  assert.deepEqual([...stale.devices].map(entry => [entry.deviceID, [...entry.entities]]), [
    ['bms', ['bms_voltage']],
    ['wr', ['wr_power', 'wr_soc']],
  ]);
});

test('ruleTitle falls back to the raw rule_id for rules without a catalog title', () => {
  const panel = createDiagnosticsPanel();
  assert.equal(panel.ruleTitle({rule_id: 'BrandNewRule', key: 'brand_new'}), 'BrandNewRule');
});

test('severity counts follow the device filter but ignore the severity filter', () => {
  const panel = createDiagnosticsPanel();
  panel.warnings = sampleWarnings();
  panel.severity = 'critical';
  assert.equal(panel.severityCount('all'), 5);
  assert.equal(panel.severityCount('warning'), 4);
  assert.equal(panel.filteredWarnings.length, 1);
  panel.device = 'wr';
  assert.equal(panel.severityCount('all'), 2);
  assert.equal(panel.severityCount('critical'), 0);
});

test('selectDevice toggles the device filter', () => {
  const panel = createDiagnosticsPanel();
  panel.selectDevice('bms');
  assert.equal(panel.device, 'bms');
  panel.selectDevice('bms');
  assert.equal(panel.device, 'all');
});

test('headline summarises the counts and the affected devices', () => {
  const panel = createDiagnosticsPanel();
  assert.equal(panel.headline, 'Checking devices ...');
  panel.loaded = true;
  assert.equal(panel.headline, 'No issues found.');
  assert.equal(panel.headlineTone, 'ok');
  panel.warnings = sampleWarnings();
  assert.equal(panel.headline, '1 critical, 4 warnings across 2 devices');
  assert.equal(panel.headlineTone, 'bad');
});

test('sortedHealthScores puts the worst device first', () => {
  const panel = createDiagnosticsPanel();
  panel.healthScores = [
    {device_id: 'b', status: 'healthy', score: 100},
    {device_id: 'c', status: 'degraded', score: 60},
    {device_id: 'a', status: 'critical', score: 0},
    {device_id: 'd', status: 'degraded', score: 40},
  ];
  assert.deepEqual([...panel.sortedHealthScores.map(item => item.device_id)], ['a', 'd', 'c', 'b']);
});

test('groups open by default when critical and keep a toggled state', () => {
  const panel = createDiagnosticsPanel();
  panel.warnings = sampleWarnings();
  const [critical, warning] = panel.warningGroups;
  assert.equal(panel.isGroupOpen(critical), true);
  assert.equal(panel.isGroupOpen(warning), false);
  panel.rememberGroup(warning, true);
  panel.rememberGroup(critical, false);
  panel.severity = 'warning';
  assert.equal(panel.isGroupOpen(warning), true);
  assert.equal(panel.isGroupOpen(critical), false);
});

test('a single group opens by default', () => {
  const panel = createDiagnosticsPanel();
  panel.warnings = sampleWarnings().filter(item => item.key === 'missing_unit');
  assert.equal(panel.isGroupOpen(panel.warningGroups[0]), true);
});

test('the refresh arrow keeps spinning until its turn ends after loading', () => {
  const panel = createDiagnosticsPanel();
  panel.loading = true;
  panel.spinning = true;
  panel.finishSpin();
  assert.equal(panel.spinning, true, 'a turn that ends during loading starts the next one');
  panel.loading = false;
  assert.equal(panel.spinning, true, 'the end of loading alone does not stop the arrow');
  panel.finishSpin();
  assert.equal(panel.spinning, false);
});

test('a device card that changes place glides there and replays its value', async () => {
  const panel = createDiagnosticsPanel();
  const {window} = {window: panel.testWindow};
  const document = window.document;
  document.body.innerHTML = `<div id="grid">
    <button class="diag-device" id="moved"><span class="diag-ring" style="--score: 30"></span><span class="diagnostics-health-status"></span></button>
    <button class="diag-device" id="still"><span class="diag-ring" style="--score: 60"></span><span class="diagnostics-health-status"></span></button>
  </div>`;
  const grid = document.getElementById('grid');
  Object.defineProperty(grid, 'offsetParent', {value: document.body});
  const rect = (left, top) => ({left, top});
  grid.getBoundingClientRect = () => rect(0, 0);
  const lefts = {moved: 0, still: 200};
  const animations = [];
  for (const card of grid.querySelectorAll('.diag-device')) {
    card.getBoundingClientRect = () => rect(lefts[card.id], 0);
    card.animate = frames => { animations.push({el: card, frames}); return {cancel() {}}; };
    const ring = card.querySelector('.diag-ring');
    ring.animate = frames => { animations.push({el: ring, frames}); return {cancel() {}}; };
  }
  window.fetch = async () => ({ok: true, json: async () => []});
  const ticks = [];
  panel.$refs = {healthGrid: grid};
  panel.$nextTick = fn => ticks.push(fn);
  await panel.load();

  lefts.moved = 200;
  grid.querySelector('#moved .diag-ring').style.setProperty('--score', '95');
  ticks.forEach(fn => fn());

  const moved = document.getElementById('moved');
  const glide = animations.find(item => item.el === moved);
  assert.equal(glide.frames[0].transform, 'translate(-200px, 0px)');
  assert.equal(glide.frames[1].transform, 'none');
  assert.ok(!animations.some(item => item.el.id === 'still'), 'a card that stays does not move');
  const ring = animations.find(item => item.el === moved.querySelector('.diag-ring'));
  assert.deepEqual([ring.frames[0]['--score'], ring.frames[1]['--score']], ['30', '95']);
});

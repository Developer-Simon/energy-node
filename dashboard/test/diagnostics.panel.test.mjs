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

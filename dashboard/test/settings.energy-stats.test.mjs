// Block "Energiestatistik" unter Einstellungen > Verlaeufe: Moduswahl und
// Statuszeilen aus der Antwort von /api/v1/energy/stats.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';
import { attachStores } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');

function panel() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = {data: (name, fn) => { factories[name] = fn; }};
  installI18n(dom.window);
  vm.runInContext(read('history-rollup.js'), context);
  vm.runInContext(read('settings.page.js'), context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  const component = factories.settingsPanel();
  attachStores(component);
  return {dom, component};
}

test('payload traegt den Modus, Standard ist always', () => {
  const {dom, component} = panel();
  assert.equal(component.payload().energy_stats_recording, 'always');
  component.energyStatsRecording = 'pause_with_companion';
  assert.equal(component.payload().energy_stats_recording, 'pause_with_companion');
  dom.window.close();
});

test('Statuszeilen bei laufendem Nachladen', () => {
  const {dom, component} = panel();
  component.energyStats = {
    status: {state: 'paused', mode: 'pause_with_companion'},
    companion: {connected: true, current: true, missing_days: 120, backfill_from: '2021-03-14T00:00:00Z'},
  };
  assert.deepEqual(JSON.parse(JSON.stringify(component.energyStatsLines)), [
    'Eigene Aufzeichnung pausiert, Home Assistant liefert',
    'Companion verbunden',
    'Nachladen aus Home Assistant bis 14.03.2021, noch 120 Tage',
  ]);
  dom.window.close();
});

test('Statuszeilen ohne Companion und mit Warnungen', () => {
  const {dom, component} = panel();
  component.energyStats = {
    status: {state: 'recording', mode: 'always', write_error: 'no space left on device', corrupt_months: ['2026-09']},
  };
  assert.deepEqual(JSON.parse(JSON.stringify(component.energyStatsLines)), [
    'Eigene Aufzeichnung aktiv',
    'Companion nicht verbunden',
    'Speichern fehlgeschlagen: no space left on device',
    'Beschädigte Monatsdatei beiseitegelegt: 2026-09',
  ]);
  dom.window.close();
});

test('Nachladen vollstaendig, wenn verbunden und nichts fehlt', () => {
  const {dom, component} = panel();
  component.energyStats = {status: {state: 'recording', mode: 'always'}, companion: {connected: true, current: false, missing_days: 0}};
  assert.equal(component.energyStatsLines.at(-1), 'Nachladen aus Home Assistant vollständig');
  dom.window.close();
});

test('loadEnergyStatsStatus fragt den laufenden Tag ab und meldet Fehler als Zeile', async () => {
  const {dom, component} = panel();
  const urls = [];
  dom.window.fetch = async url => { urls.push(url); return {ok: false, status: 501, json: async () => ({code: 'energy_stats_unavailable', message: 'x'})}; };
  await component.loadEnergyStatsStatus();
  assert.deepEqual(urls, ['/api/v1/energy/stats?period=day']);
  assert.equal(component.energyStats, null);
  assert.ok(component.energyStatsError);
  dom.window.close();
});

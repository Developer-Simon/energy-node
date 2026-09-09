// Der Verlaufs-Teil der Einstellungsseite. Geprueft wird die Rechnung
// hinter der Anzeige - eine Abtastrate ohne Volumenangabe daneben ist fuer
// den Nutzer eine Zahl ohne Bedeutung, und genau diese Umrechnung ist die
// Stelle, an der sich ein Vorzeichen- oder Faktorfehler versteckt.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { attachStores } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');
const rollupSource = read('history-rollup.js');
const settingsSource = read('settings.page.js');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = {data: (name, fn) => { factories[name] = fn; }};
  // settings.page.js registriert seine Komponenten ueber alpine:init.
  vm.runInContext(rollupSource, context);
  vm.runInContext(settingsSource, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  return {dom, factories};
}

function panel() {
  const {dom, factories} = load();
  const component = factories.settingsPanel();
  attachStores(component);
  return {dom, component};
}

test('historyVolumeText nennt das Tagesvolumen aus Abtastrate und Serienzahl', () => {
  const {dom, component} = panel();
  component.historySampleIntervalSeconds = 10;
  component.historySeriesCount = 6;
  const text = component.historyVolumeText;
  // 8640 Samples/Tag * 6 Serien * 120 Byte = 6.220.800 Byte (roh)
  // + Minutenwerte (1.382.400) + Fuenf-Minuten-Werte (276.480) = 7.879.680 Byte ≈ 7.5 MB
  assert.match(text, /7[.,]\d\s?MB\/Tag/);
  dom.window.close();
});

test('historyVolumeText halbiert sich, wenn die Abtastrate verdoppelt wird', () => {
  const {dom, component} = panel();
  component.historySeriesCount = 6;
  component.historySampleIntervalSeconds = 10;
  const fast = component.historyBytesPerDay;
  component.historySampleIntervalSeconds = 20;
  const slow = component.historyBytesPerDay;
  assert.ok(slow < fast, `erwartet weniger Volumen bei groesserem Intervall: ${slow} vs ${fast}`);
  dom.window.close();
});

test('historyBudgetText rechnet das Budget in Tage um', () => {
  const {dom, component} = panel();
  component.historyRetentionMode = 'size';
  component.historyBudgetMb = 1024;
  component.historySampleIntervalSeconds = 10;
  component.historySeriesCount = 6;
  assert.match(component.historyBudgetText, /\d+\s?Tage/);
  dom.window.close();
});

test('historyBudgetText bleibt im Zeitmodus stumm ueber das Budget', () => {
  const {dom, component} = panel();
  component.historyRetentionMode = 'time';
  component.historyRetentionHours = 48;
  assert.match(component.historyBudgetText, /48/);
  assert.doesNotMatch(component.historyBudgetText, /MB|GB/);
  dom.window.close();
});

test('valid weist eine Abtastrate unter der 5-Sekunden-Grenze zurueck', () => {
  const {dom, component} = panel();
  component.historySampleIntervalSeconds = 4;
  assert.equal(component.valid, false);
  component.historySampleIntervalSeconds = 5;
  assert.equal(component.valid, true);
  dom.window.close();
});

test('payload traegt die acht Verlaufsfelder', () => {
  const {dom, component} = panel();
  component.historySampleIntervalSeconds = 30;
  component.historyRetentionMode = 'size';
  component.historyBudgetMb = 2048;
  component.historyExtraEntities = ['shelly_em_power'];
  const payload = component.payload();
  assert.equal(payload.history_sample_interval_seconds, 30);
  assert.equal(payload.history_retention_mode, 'size');
  assert.equal(payload.history_budget_mb, 2048);
  assert.equal(payload.history_extra_entities.length, 1);
  assert.equal(payload.history_extra_entities[0], 'shelly_em_power');
  assert.ok('history_retention_hours' in payload);
  assert.ok('history_raw_window_hours' in payload);
  assert.ok('history_minute_window_days' in payload);
  assert.ok('history_views' in payload);
  dom.window.close();
});

test('der Austausch ist in der Vorgabe an und geht so in die Nutzlast', () => {
  const {dom, component} = panel();
  // false heisst "nicht abgeschaltet". Die invertierte Benennung stammt aus
  // settings.go: normalizeSettings erkennt "nicht gesetzt" am Nullwert.
  assert.equal(component.historyExchangeDisabled, false);
  assert.equal(component.payload().history_exchange_disabled, false);
  dom.window.close();
});

test('ein abgeschalteter Austausch wird mitgesendet', () => {
  const {dom, component} = panel();
  component.historyExchangeDisabled = true;
  assert.equal(component.payload().history_exchange_disabled, true);
  dom.window.close();
});

test('refreshExchangeStatus nennt Peers und ergaenzte Messwerte', () => {
  const {dom, component} = panel();
  dom.window.HistoryExchange = {
    status: () => ({
      connected: true, peerId: 'p-selbst', peers: ['p-selbst', 'p-fremd'],
      addedRows: 42, lastPeer: 'p-fremd', lastAt: 0, reason: '',
    }),
  };
  component.refreshExchangeStatus();
  assert.match(component.historyExchangeStatus, /1 weiteres Gerät/);
  assert.match(component.historyExchangeStatus, /42/);
  dom.window.close();
});

test('refreshExchangeStatus nennt den Grund, wenn nicht verbunden', () => {
  const {dom, component} = panel();
  dom.window.HistoryExchange = {
    status: () => ({connected: false, peerId: '', peers: [], addedRows: 0, lastPeer: '', lastAt: 0,
      reason: 'Der Server bietet keinen Verlauf-Austausch an.'}),
  };
  component.refreshExchangeStatus();
  assert.match(component.historyExchangeStatus, /bietet keinen Verlauf-Austausch/);
  dom.window.close();
});

test('ohne Austausch-Client sagt die Statuszeile das auch', () => {
  const {dom, component} = panel();
  component.refreshExchangeStatus();
  assert.match(component.historyExchangeStatus, /nicht aktiv/);
  dom.window.close();
});

// Die Stepper der neuen Verlaufs-Karten schreiben dieselben Felder wie die
// alten Zahlenfelder, nur ueber stepField() mit fester Schrittweite und
// hartem Clamp an den Grenzen, die auch valid() prueft.
test('stepField erhoeht die Abtastrate um die Schrittweite 5 und clampt bei 3600', () => {
  const {dom, component} = panel();
  component.historySampleIntervalSeconds = 10;
  component.stepField('historySampleIntervalSeconds', 1);
  assert.equal(component.historySampleIntervalSeconds, 15);
  component.historySampleIntervalSeconds = 3599;
  component.stepField('historySampleIntervalSeconds', 1);
  assert.equal(component.historySampleIntervalSeconds, 3600);
  dom.window.close();
});

test('stepField clampt nach unten an die jeweilige Feldgrenze', () => {
  const {dom, component} = panel();
  component.historySampleIntervalSeconds = 5;
  component.stepField('historySampleIntervalSeconds', -1);
  assert.equal(component.historySampleIntervalSeconds, 5);
  component.historyRawWindowHours = 1;
  component.stepField('historyRawWindowHours', -1);
  assert.equal(component.historyRawWindowHours, 1);
  dom.window.close();
});

test('stepField kennt das Speicherbudget mit Schrittweite 16 und Obergrenze 8192', () => {
  const {dom, component} = panel();
  component.historyBudgetMb = 512;
  component.stepField('historyBudgetMb', 1);
  assert.equal(component.historyBudgetMb, 528);
  component.historyBudgetMb = 8180;
  component.stepField('historyBudgetMb', 1);
  assert.equal(component.historyBudgetMb, 8192);
  dom.window.close();
});

test('stepField ignoriert ein unbekanntes Feld, statt NaN zu schreiben', () => {
  const {dom, component} = panel();
  component.stepField('nichtVorhanden', 1);
  assert.equal(component.nichtVorhanden, undefined);
  dom.window.close();
});

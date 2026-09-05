// Regressionstests fuer history-recorder.js. roleSamples() ist die reine
// Funktion, die aus einem /api/v1/energy-Schnappschuss die signierten
// "role:<rolle>"-Serien macht - die Standard-Aufzeichnung der Historie und
// die Datenquelle der Sparklines in energy-board.js sowie der Tagesleiste in
// energy-day.js. Die Speicherschicht ist in history-store.test.mjs geprueft.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { IDBFactory as FDBFactory, IDBKeyRange as FDBKeyRange } from 'fake-indexeddb';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');
const rollupSource = read('history-rollup.js');
const storeSource = read('history-store.js');
const energyModelSource = read('energy-model.js');
const recorderSource = read('history-recorder.js');

// start() wird beim Laden aufgerufen und wuerde einen echten Intervall-Timer
// setzen, der den Testprozess am Leben haelt - setInterval wird deshalb
// stillgelegt, fetch abgelehnt, damit collect() im eigenen catch endet.
function load({withEnergyModel = true} = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
  dom.window.fetch = () => Promise.reject(new Error('kein Netz im Test'));
  dom.window.setInterval = () => 0;
  dom.window.indexedDB = new FDBFactory();
  dom.window.IDBKeyRange = FDBKeyRange;
  vm.runInContext(rollupSource, context);
  vm.runInContext(storeSource, context);
  if (withEnergyModel) vm.runInContext(energyModelSource, context);
  vm.runInContext(recorderSource, context);
  return dom;
}

test('roleSamples macht aus einem Schnappschuss eine signierte Serie je zugeordneter Rolle', () => {
  const dom = load();
  const samples = dom.window.dashboardHistorizer.roleSamples({
    at: '2026-08-07T12:00:00Z',
    values: {pv: 6400, battery: 2200, load: 620, grid_export: 3580, wallbox: 0, heat_pump: 900},
    roles: [],
  });
  const bySeries = Object.fromEntries(samples.map(sample => [sample.series, sample]));
  assert.equal(samples.length, 6);
  assert.equal(bySeries['role:pv'].v, 6400);
  assert.equal(bySeries['role:battery'].v, 2200); // Laden = positiv
  assert.equal(bySeries['role:grid'].v, -3580); // Einspeisen = negativ
  assert.equal(bySeries['role:load'].v, 620);
  assert.equal(bySeries['role:heat_pump'].v, 900);
  const expectedTs = Date.parse('2026-08-07T12:00:00Z');
  for (const sample of samples) {
    assert.equal(sample.ts, expectedTs);
    assert.equal(typeof sample.ts, 'number');
    assert.equal(sample.u, 'W');
  }
  dom.window.close();
});

test('roleSamples liefert eine leere Liste ohne EnergyModel', () => {
  const dom = load({withEnergyModel: false});
  const samples = dom.window.dashboardHistorizer.roleSamples({at: '2026-08-07T12:00:00Z', values: {}, roles: []});
  assert.deepEqual(JSON.parse(JSON.stringify(samples)), []);
  dom.window.close();
});

test('roleSamples laesst eine Rolle ohne zugeordnete Entitaet weg statt eine 0 zu schreiben', () => {
  const dom = load();
  const samples = dom.window.dashboardHistorizer.roleSamples({
    at: '2026-08-17T15:00:00Z',
    values: {pv: 640, grid: 511, battery: -130},
    roles: [],
  });
  assert.equal(samples.length, 3);
  const series = samples.map(sample => sample.series).sort();
  assert.equal(series[0], 'role:battery');
  assert.equal(series[1], 'role:grid');
  assert.equal(series[2], 'role:pv');
  dom.window.close();
});

test('roleSamples schreibt eine echte 0 weiterhin', () => {
  const dom = load();
  const samples = dom.window.dashboardHistorizer.roleSamples({
    at: '2026-08-17T15:00:00Z', values: {pv: 0, load: 0}, roles: [],
  });
  const bySeries = Object.fromEntries(samples.map(sample => [sample.series, sample.v]));
  assert.equal(bySeries['role:pv'], 0);
  assert.equal(bySeries['role:load'], 0);
  dom.window.close();
});

test('roleSamples leitet die Batterie- und Netzrolle aus den getrennten Rollen ab', () => {
  const dom = load();
  const samples = dom.window.dashboardHistorizer.roleSamples({
    at: '2026-08-17T15:00:00Z', values: {battery_charge: 300, grid_export: 1400}, roles: [],
  });
  const bySeries = Object.fromEntries(samples.map(sample => [sample.series, sample.v]));
  assert.equal(bySeries['role:battery'], 300);
  assert.equal(bySeries['role:grid'], -1400);
  dom.window.close();
});

test('roleSamples nimmt die Batterie-Fuellstandrolle als role:battery_soc mit Einheit % auf', () => {
  const dom = load();
  const samples = dom.window.dashboardHistorizer.roleSamples({
    at: '2026-09-02T12:00:00Z',
    values: {pv: 6400, battery: 2200, battery_soc: 47},
    roles: [],
  });
  const bySeries = Object.fromEntries(samples.map(sample => [sample.series, sample]));
  assert.equal(bySeries['role:battery_soc'].v, 47);
  assert.equal(bySeries['role:battery_soc'].u, '%');
  // Die Leistungsrollen behalten ihre Einheit - nur der Fuellstand ist %.
  assert.equal(bySeries['role:pv'].u, 'W');
  assert.equal(bySeries['role:battery'].u, 'W');
  dom.window.close();
});

test('roleSamples laesst role:battery_soc weg, wenn keine Fuellstand-Entitaet zugeordnet ist', () => {
  const dom = load();
  const samples = dom.window.dashboardHistorizer.roleSamples({
    at: '2026-09-02T12:00:00Z', values: {pv: 6400, battery: 2200}, roles: [],
  });
  assert.ok(!samples.some(sample => sample.series === 'role:battery_soc'));
  dom.window.close();
});

test('roleSamples schreibt einen echten Fuellstand von 0 %', () => {
  const dom = load();
  const samples = dom.window.dashboardHistorizer.roleSamples({
    at: '2026-09-02T12:00:00Z', values: {battery_soc: 0}, roles: [],
  });
  const bySeries = Object.fromEntries(samples.map(sample => [sample.series, sample]));
  assert.equal(bySeries['role:battery_soc'].v, 0);
  assert.equal(bySeries['role:battery_soc'].u, '%');
  dom.window.close();
});

test('readSamples liefert die Altform, die energy-board.js und energy-day.js lesen', async () => {
  const dom = load();
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts: Date.now() - 60000, v: 640, u: 'W'},
  ]);
  const samples = await dom.window.dashboardHistorizer.readSamples();
  assert.equal(samples.length, 1);
  assert.equal(samples[0].entity_id, 'role:pv');
  assert.equal(samples[0].value, 640);
  assert.equal(samples[0].unit, 'W');
  assert.equal(samples[0].source, 'browser');
  assert.ok(!Number.isNaN(Date.parse(samples[0].timestamp)), 'timestamp muss parsebar sein');
  dom.window.close();
});

test('readSamples deckelt das Fenster, statt die gesamte Historie zu laden', async () => {
  const dom = load();
  const now = Date.now();
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts: now - 40 * 24 * 3600 * 1000, v: 1, u: 'W'},
    {series: 'role:pv', ts: now - 60000, v: 2, u: 'W'},
  ]);
  const samples = await dom.window.dashboardHistorizer.readSamples();
  assert.equal(samples.length, 1, 'der 40 Tage alte Satz darf im 24h-Standardfenster nicht auftauchen');
  assert.equal(samples[0].value, 2);
  dom.window.close();
});

test('ohne Web-Locks-API zeichnet der Tab auf, statt gar nicht aufzuzeichnen', async () => {
  const dom = load();
  // jsdom kennt navigator.locks nicht - genau der Rueckfallpfad.
  // Der async start() muss abgearbeitet werden, deshalb eine Tick warten.
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(dom.window.navigator.locks, undefined);
  assert.equal(dom.window.dashboardHistorizer.isLeader(), true);
  dom.window.close();
});

test('ein volles Kontingent pausiert die Aufzeichnung mit Begruendung statt still zu scheitern', async () => {
  const dom = load();
  // start() laeuft beim Laden automatisch los und liest in becomeLeader()
  // ueber zwei echte IndexedDB-Zugriffe (persist(), meta()) den gespeicherten
  // Pause-Zustand, bevor der eigene Bootstrap-Collect beginnt. Ohne diese
  // Wartezeit ueberschreibt dieser Lauf das paused=true von unten mit einem
  // veralteten "false", weil er erst danach zu Ende laeuft.
  await new Promise(resolve => setTimeout(resolve, 20));
  const historizer = dom.window.dashboardHistorizer;
  const events = [];
  dom.window.addEventListener('dashboard-history-paused', event => events.push(event.detail));

  // Der Store meldet ein volles Kontingent - genau das, was ein Browser an
  // seiner Quota-Grenze wirft.
  dom.window.HistoryStore.writeRaw = () => {
    const error = new Error('Quota überschritten');
    error.name = 'QuotaExceededError';
    return Promise.reject(error);
  };
  dom.window.fetch = () => Promise.resolve({
    ok: true,
    json: () => Promise.resolve({at: new Date().toISOString(), values: {pv: 100}, roles: []}),
  });

  await historizer.collectOnce();

  assert.equal(historizer.status().paused, true);
  assert.equal(events.length, 1);
  assert.match(events[0].reason, /Speicher/);
  dom.window.close();
});

test('eine pausierte Aufzeichnung schreibt nicht weiter', async () => {
  const dom = load();
  // Siehe Kommentar oben: der automatische Bootstrap-Lauf muss abgeschlossen
  // sein, bevor wir den Zustand manuell manipulieren.
  await new Promise(resolve => setTimeout(resolve, 20));
  const historizer = dom.window.dashboardHistorizer;
  let writes = 0;
  dom.window.HistoryStore.writeRaw = () => {
    writes += 1;
    const error = new Error('Quota überschritten');
    error.name = 'QuotaExceededError';
    return Promise.reject(error);
  };
  dom.window.fetch = () => Promise.resolve({
    ok: true,
    json: () => Promise.resolve({at: new Date().toISOString(), values: {pv: 100}, roles: []}),
  });
  await historizer.collectOnce();
  await historizer.collectOnce();
  assert.equal(writes, 1, 'nach dem Pausieren darf kein zweiter Schreibversuch folgen');
  dom.window.close();
});

test('resume nimmt die Aufzeichnung wieder auf', async () => {
  const dom = load();
  // Siehe Kommentar oben: der automatische Bootstrap-Lauf muss abgeschlossen
  // sein, bevor wir den Zustand manuell manipulieren.
  await new Promise(resolve => setTimeout(resolve, 20));
  const historizer = dom.window.dashboardHistorizer;
  dom.window.HistoryStore.writeRaw = () => {
    const error = new Error('Quota überschritten');
    error.name = 'QuotaExceededError';
    return Promise.reject(error);
  };
  dom.window.fetch = () => Promise.resolve({
    ok: true,
    json: () => Promise.resolve({at: new Date().toISOString(), values: {pv: 100}, roles: []}),
  });
  await historizer.collectOnce();
  assert.equal(historizer.status().paused, true);
  // resume() versucht sofort wieder zu schreiben (siehe Implementierung) -
  // ist der Grund fuer die Pause noch da, muss resume() erneut pausieren.
  // Fuer "resume nimmt wieder auf" muss das zugrundeliegende Problem also
  // vorher behoben sein, genau wie ein Nutzer erst das Budget anpasst.
  dom.window.HistoryStore.writeRaw = () => Promise.resolve();
  await historizer.resume();
  assert.equal(historizer.status().paused, false);
  dom.window.close();
});

test('ein gewoehnlicher Netzfehler pausiert die Aufzeichnung nicht', async () => {
  const dom = load();
  const historizer = dom.window.dashboardHistorizer;
  dom.window.fetch = () => Promise.reject(new Error('kein Netz'));
  await historizer.collectOnce();
  assert.equal(historizer.status().paused, false);
  dom.window.close();
});

// Ein Double fuer den Austausch-Client: der Recorder soll ihn beim
// Fuehrungsantritt starten und nach der Verdichtung fuettern - was der
// Client dann tut, ist in history-exchange.test.mjs geprueft.
function fakeExchange() {
  return {
    started: 0,
    stopped: 0,
    pushed: [],
    refreshed: 0,
    async start() { this.started += 1; return true; },
    stop() { this.stopped += 1; },
    async pushBuffer(rows) { this.pushed.push(rows); },
    async refreshOffer() { this.refreshed += 1; },
  };
}

// Wartet den automatischen Fuehrungsantritt ab und haengt danach das Double
// ein. Wuerde man es vorher setzen, zaehlte der Start aus becomeLeader()
// mit und die Erwartungen waeren von der Promise-Reihenfolge abhaengig.
async function leaderWithExchange() {
  const dom = load();
  await new Promise(resolve => setTimeout(resolve, 10));
  const exchange = fakeExchange();
  dom.window.HistoryExchange = exchange;
  return {dom, exchange};
}

test('der fuehrende Tab startet den Austausch', async () => {
  const {dom, exchange} = await leaderWithExchange();
  assert.equal(dom.window.dashboardHistorizer.isLeader(), true);
  await dom.window.dashboardHistorizer.startExchange();
  assert.equal(exchange.started, 1);
  dom.window.close();
});

test('ein abgeschalteter Austausch wird nicht gestartet', async () => {
  const {dom, exchange} = await leaderWithExchange();
  dom.window.document.dispatchEvent(new dom.window.CustomEvent('history-settings-changed', {
    detail: {exchangeDisabled: true},
  }));
  await new Promise(resolve => setTimeout(resolve, 10));
  await dom.window.dashboardHistorizer.startExchange();
  assert.equal(exchange.started, 0);
  assert.equal(dom.window.dashboardHistorizer.config().exchangeDisabled, true);
  dom.window.close();
});

test('das Abschalten waehrend des Betriebs beendet den Austausch', async () => {
  const {dom, exchange} = await leaderWithExchange();
  await dom.window.dashboardHistorizer.startExchange();
  dom.window.document.dispatchEvent(new dom.window.CustomEvent('history-settings-changed', {
    detail: {exchangeDisabled: true},
  }));
  await new Promise(resolve => setTimeout(resolve, 10));
  assert.equal(exchange.stopped, 1);
  dom.window.close();
});

test('frisch verdichtete Minutenwerte gehen in den Server-Puffer', async () => {
  const {dom, exchange} = await leaderWithExchange();
  const rows = [{series: 'role:pv', ts: 60000, min: 1, max: 1, avg: 1, n: 60, u: 'W'}];
  await dom.window.dashboardHistorizer.afterMaintenance({minuteRows: rows});
  assert.deepEqual(exchange.pushed, [rows]);
  assert.equal(exchange.refreshed, 1);
  dom.window.close();
});

test('ein Fehler des Austauschs haelt die Aufzeichnung nicht an', async () => {
  const {dom, exchange} = await leaderWithExchange();
  exchange.pushBuffer = async () => { throw new Error('Server weg'); };
  const seen = [];
  dom.window.addEventListener('dashboard-history-error', event => seen.push(event.detail.message));
  await dom.window.dashboardHistorizer.afterMaintenance({
    minuteRows: [{series: 'role:pv', ts: 60000, min: 1, max: 1, avg: 1, n: 60, u: 'W'}],
  });
  assert.equal(seen.length, 1);
  assert.match(seen[0], /Verlauf-Austausch/);
  dom.window.close();
});

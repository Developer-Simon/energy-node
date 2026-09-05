// dashboard/test/history-rollup.test.mjs
// Reine Rechenfunktionen der Verlaufs-Verdichtung. Kein DOM, kein IndexedDB -
// history-rollup.js haengt an nichts, deshalb reicht ein nacktes vm-Context.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'history-rollup.js'),
  'utf8',
);

function load() {
  const context = vm.createContext({ window: {} });
  vm.runInContext(source, context);
  return context.window.HistoryRollup;
}

test('normalize macht aus einem Rohsatz einen Verdichtungssatz mit n=1', () => {
  const rollup = load();
  const rows = rollup.normalize([{series: 'role:pv', ts: 1000, v: 640, u: 'W'}]);
  assert.deepEqual(JSON.parse(JSON.stringify(rows)), [{series: 'role:pv', ts: 1000, min: 640, max: 640, avg: 640, n: 1, u: 'W'}]);
});

test('normalize laesst einen bereits verdichteten Satz unveraendert', () => {
  const rollup = load();
  const row = {series: 'role:pv', ts: 1000, min: 1, max: 9, avg: 5, n: 4, u: 'W'};
  assert.deepEqual(rollup.normalize([row]), [row]);
});

test('bucket fasst Rohwerte je Minute und Serie zusammen', () => {
  const rollup = load();
  const rows = rollup.bucket(rollup.normalize([
    {series: 'role:pv', ts: 60000, v: 100, u: 'W'},
    {series: 'role:pv', ts: 90000, v: 300, u: 'W'},
    {series: 'role:pv', ts: 120000, v: 50, u: 'W'},
    {series: 'role:grid', ts: 61000, v: -20, u: 'W'},
  ]), rollup.MINUTE_MS);
  const pv = rows.filter(row => row.series === 'role:pv');
  assert.equal(pv.length, 2);
  assert.deepEqual(JSON.parse(JSON.stringify(pv[0])), {series: 'role:pv', ts: 60000, min: 100, max: 300, avg: 200, n: 2, u: 'W'});
  assert.deepEqual(JSON.parse(JSON.stringify(pv[1])), {series: 'role:pv', ts: 120000, min: 50, max: 50, avg: 50, n: 1, u: 'W'});
  assert.equal(rows.filter(row => row.series === 'role:grid').length, 1);
});

test('bucket gewichtet den Mittelwert beim Verdichten einer Verdichtung mit n', () => {
  const rollup = load();
  // Ein Bucket mit 9 Messwerten a 100, einer mit 1 Messwert a 1000.
  // Ungewichtet waere das Ergebnis 550, richtig ist (900 + 1000) / 10 = 190.
  const rows = rollup.bucket([
    {series: 'role:pv', ts: 0, min: 100, max: 100, avg: 100, n: 9, u: 'W'},
    {series: 'role:pv', ts: 60000, min: 1000, max: 1000, avg: 1000, n: 1, u: 'W'},
  ], rollup.FIVE_MINUTE_MS);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].avg, 190);
  assert.equal(rows[0].min, 100);
  assert.equal(rows[0].max, 1000);
  assert.equal(rows[0].n, 10);
  assert.equal(rows[0].ts, 0);
});

test('selectTier waehlt die Stufe nach der angefragten Spanne', () => {
  const rollup = load();
  const windows = {rawWindowMs: 24 * 3600 * 1000, minuteWindowMs: 7 * 24 * 3600 * 1000};
  assert.equal(rollup.selectTier(3600 * 1000, windows), 'raw');
  assert.equal(rollup.selectTier(24 * 3600 * 1000, windows), 'raw');
  assert.equal(rollup.selectTier(3 * 24 * 3600 * 1000, windows), '1m');
  assert.equal(rollup.selectTier(30 * 24 * 3600 * 1000, windows), '5m');
});

test('thin laesst kleine Reihen unveraendert und deckelt grosse', () => {
  const rollup = load();
  const small = rollup.normalize([{series: 'a', ts: 0, v: 1, u: 'W'}, {series: 'a', ts: 1000, v: 2, u: 'W'}]);
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.thin(small, 10))), JSON.parse(JSON.stringify(small)));

  const many = [];
  for (let index = 0; index < 5000; index += 1) many.push({series: 'a', ts: index * 1000, v: index, u: 'W'});
  const thinned = rollup.thin(rollup.normalize(many), 100);
  assert.ok(thinned.length <= 100, `erwartet <= 100, war ${thinned.length}`);
  assert.ok(thinned.length > 0);
  // Extremwerte duerfen durch die Verdichtung nicht verschwinden.
  assert.equal(Math.min(...thinned.map(row => row.min)), 0);
  assert.equal(Math.max(...thinned.map(row => row.max)), 4999);
});

test('estimateBytesPerDay rechnet Abtastrate und Serienzahl in Tagesvolumen um', () => {
  const rollup = load();
  const estimate = rollup.estimateBytesPerDay({
    intervalSeconds: 10, seriesCount: 6, rawBytes: 120, rollupBytes: 160,
  });
  // 86400 / 10 = 8640 Samples je Serie und Tag.
  assert.equal(estimate.raw, 8640 * 6 * 120);
  assert.equal(estimate.minute, 1440 * 6 * 160);
  assert.equal(estimate.fiveMinute, 288 * 6 * 160);
});

test('detectGaps meldet keine Luecke bei gleichmaessigem Abstand', () => {
  const rollup = load();
  const points = [0, 10000, 20000, 30000, 40000].map(ts => ({ts}));
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.detectGaps(points))), []);
});

test('detectGaps toleriert leichten Jitter um den ueblichen Abstand', () => {
  const rollup = load();
  // ueblicher Abstand 10s, einmal 12s statt 10s - kein Aufzeichnungsloch.
  const points = [0, 10000, 22000, 32000, 42000].map(ts => ({ts}));
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.detectGaps(points))), []);
});

test('detectGaps findet eine Luecke, die deutlich groesser als der uebliche Abstand ist', () => {
  const rollup = load();
  // ueblicher Abstand 10s, eine Luecke von 5 Minuten.
  const points = [0, 10000, 20000, 320000, 330000, 340000].map(ts => ({ts}));
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.detectGaps(points))), [{from: 20000, to: 320000}]);
});

test('detectGaps findet mehrere Luecken in derselben Reihe', () => {
  const rollup = load();
  const points = [0, 10000, 500000, 510000, 900000, 910000].map(ts => ({ts}));
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.detectGaps(points))), [
    {from: 10000, to: 500000},
    {from: 510000, to: 900000},
  ]);
});

test('detectGaps ignoriert 60s-Abstand durch Tab-Throttling bei kurzem Aufzeichnungsintervall', () => {
  const rollup = load();
  // ueblicher Abstand 10s, einmal 60s durch Browser-Throttling im
  // reduzierten Tab - ohne Mindestschwelle waere die Multiplikator-Schwelle
  // nur 50s und wuerde das faelschlich als Luecke melden.
  const points = [0, 10000, 20000, 80000, 90000, 100000].map(ts => ({ts}));
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.detectGaps(points))), []);
});

test('detectGaps meldet trotz Mindestschwelle eine echte Luecke ueber der Multiplikator-Schwelle', () => {
  const rollup = load();
  // ueblicher Abstand 10s, eine Luecke von 3 Minuten - deutlich ueber der
  // Mindestschwelle von 90s.
  const points = [0, 10000, 20000, 200000, 210000, 220000].map(ts => ({ts}));
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.detectGaps(points))), [{from: 20000, to: 200000}]);
});

test('detectGaps liefert bei weniger als zwei Punkten keine Luecken', () => {
  const rollup = load();
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.detectGaps([]))), []);
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.detectGaps([{ts: 0}]))), []);
});

test('withGapBreaks laesst Reihen ohne Luecken unveraendert', () => {
  const rollup = load();
  const pairs = [[0, 1], [10000, 2], [20000, 3]];
  assert.deepEqual(rollup.withGapBreaks(pairs, []), pairs);
});

test('withGapBreaks fuegt nach einer Luecke einen Null-Punkt ein, der die Linie unterbricht', () => {
  const rollup = load();
  const pairs = [[0, 1], [20000, 2], [320000, 3], [330000, 4]];
  const gaps = [{from: 20000, to: 320000}];
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.withGapBreaks(pairs, gaps))), [
    [0, 1],
    [20000, 2],
    [20001, null],
    [320000, 3],
    [330000, 4],
  ]);
});

test('recordingGaps meldet eine Luecke vor dem ersten Punkt', () => {
  const rollup = load();
  // Der haeufigste Fall aus der Praxis und genau der, den detectGaps() nicht
  // sehen kann: der Browser war die erste Haelfte des Zeitraums zu.
  const rows = [300000, 310000, 320000].map(ts => ({series: 'role:pv', ts}));
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.recordingGaps(rows, 0, 320000))), [{from: 0, to: 300000}]);
});

test('recordingGaps meldet eine Luecke nach dem letzten Punkt', () => {
  const rollup = load();
  const rows = [0, 10000, 20000].map(ts => ({series: 'role:pv', ts}));
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.recordingGaps(rows, 0, 320000))), [{from: 20000, to: 320000}]);
});

test('recordingGaps laesst knapp anliegende Raender in Ruhe', () => {
  const rollup = load();
  // Weniger als die Mindestschwelle von 90s Abstand zum Fensterrand ist
  // normaler Takt, keine Luecke.
  const rows = [1000, 11000, 21000].map(ts => ({series: 'role:pv', ts}));
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.recordingGaps(rows, 0, 22000))), []);
});

test('recordingGaps macht aus einem Fenster ohne jeden Punkt eine einzige Luecke', () => {
  const rollup = load();
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.recordingGaps([], 0, 320000))), [{from: 0, to: 320000}]);
});

test('recordingGaps findet innere Luecken weiter wie detectGaps', () => {
  const rollup = load();
  const rows = [0, 10000, 20000, 320000, 330000, 340000].map(ts => ({series: 'role:pv', ts}));
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.recordingGaps(rows, 0, 340000))), [{from: 20000, to: 320000}]);
});

test('uncoveredGaps meldet nichts, wenn der Austausch die Luecke dicht fuellt', () => {
  const rollup = load();
  const gaps = [{from: 0, to: 300000}];
  const fill = [60000, 120000, 180000, 240000].map(ts => ({ts}));
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.uncoveredGaps(gaps, fill))), []);
});

test('uncoveredGaps laesst links und rechts eines isolierten Punktes je eine Rest-Luecke', () => {
  const rollup = load();
  // Der Bug aus der Praxis: ein einzelner Austausch-Punkt mitten in einer
  // Luecke von 20 Minuten macht daraus keine durchgehende Aufzeichnung.
  const gaps = [{from: 0, to: 1200000}];
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.uncoveredGaps(gaps, [{ts: 600000}]))), [
    {from: 0, to: 600000},
    {from: 600000, to: 1200000},
  ]);
});

test('uncoveredGaps gibt unberuehrte Luecken unveraendert zurueck', () => {
  const rollup = load();
  const gaps = [{from: 0, to: 300000}, {from: 900000, to: 1200000}];
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.uncoveredGaps(gaps, []))), gaps);
});

test('fillRawGaps laesst eine luecklos aufgezeichnete Serie unveraendert', () => {
  const rollup = load();
  const raw = rollup.normalize([
    {series: 'role:pv', ts: 0, v: 1, u: 'W'},
    {series: 'role:pv', ts: 10000, v: 2, u: 'W'},
    {series: 'role:pv', ts: 20000, v: 3, u: 'W'},
  ]);
  const rollupRows = [{series: 'role:pv', ts: 15000, min: 99, max: 99, avg: 99, n: 6, u: 'W'}];
  const {rows, gaps} = rollup.fillRawGaps(raw, rollupRows, 0, 20000);
  assert.equal(rows.length, 3, 'ohne Luecke darf kein Rollup-Satz einsickern');
  assert.deepEqual(JSON.parse(JSON.stringify(gaps.get('role:pv'))), []);
});

test('fillRawGaps fuellt eine echte Luecke mit dem Rollup-Satz', () => {
  const rollup = load();
  const raw = rollup.normalize([
    {series: 'role:pv', ts: 0, v: 1, u: 'W'},
    {series: 'role:pv', ts: 10000, v: 2, u: 'W'},
    {series: 'role:pv', ts: 20000, v: 3, u: 'W'},
    {series: 'role:pv', ts: 320000, v: 4, u: 'W'},
    {series: 'role:pv', ts: 330000, v: 5, u: 'W'},
    {series: 'role:pv', ts: 340000, v: 6, u: 'W'},
  ]);
  const rollupRows = [{series: 'role:pv', ts: 100000, min: 50, max: 50, avg: 50, n: 6, u: 'W'}];
  const {rows} = rollup.fillRawGaps(raw, rollupRows, 0, 340000);
  assert.equal(rows.length, 7);
  const filled = rows.find(row => row.ts === 100000);
  assert.ok(filled, 'der Luecken-Satz fehlt');
  assert.equal(filled.avg, 50);
});

test('fillRawGaps fuellt auch die Luecke VOR dem ersten Rohpunkt', () => {
  const rollup = load();
  // Genau der Fall, an dem die erste Fassung scheiterte: zwischen zwei
  // vorhandenen Punkten gibt es keine Luecke, aber davor fehlen Stunden.
  const raw = rollup.normalize([
    {series: 'role:pv', ts: 600000, v: 1, u: 'W'},
    {series: 'role:pv', ts: 610000, v: 2, u: 'W'},
    {series: 'role:pv', ts: 620000, v: 3, u: 'W'},
  ]);
  const rollupRows = [60000, 120000, 180000].map(ts => ({series: 'role:pv', ts, min: 5, max: 5, avg: 5, n: 6, u: 'W'}));
  const {rows} = rollup.fillRawGaps(raw, rollupRows, 0, 620000);
  assert.equal(rows.length, 6, 'alle drei Minutenmittel liegen in der Luecke vor dem ersten Rohpunkt');
});

test('fillRawGaps fuellt auch die Luecke NACH dem letzten Rohpunkt', () => {
  const rollup = load();
  const raw = rollup.normalize([
    {series: 'role:pv', ts: 0, v: 1, u: 'W'},
    {series: 'role:pv', ts: 10000, v: 2, u: 'W'},
    {series: 'role:pv', ts: 20000, v: 3, u: 'W'},
  ]);
  const rollupRows = [300000, 360000].map(ts => ({series: 'role:pv', ts, min: 5, max: 5, avg: 5, n: 6, u: 'W'}));
  const {rows} = rollup.fillRawGaps(raw, rollupRows, 0, 400000);
  assert.equal(rows.length, 5);
});

test('fillRawGaps ignoriert Rollup-Saetze ausserhalb jeder Luecke', () => {
  const rollup = load();
  const raw = rollup.normalize([
    {series: 'role:pv', ts: 0, v: 1, u: 'W'},
    {series: 'role:pv', ts: 10000, v: 2, u: 'W'},
    {series: 'role:pv', ts: 20000, v: 3, u: 'W'},
    {series: 'role:pv', ts: 320000, v: 4, u: 'W'},
  ]);
  // Dieser Rollup-Satz liegt in einem Bereich, den raw bereits abdeckt - er
  // darf nicht auftauchen, sonst zeichnete das Chart eine grobe Kopie ueber
  // die eigene Messung.
  const rollupRows = [{series: 'role:pv', ts: 5000, min: 99, max: 99, avg: 99, n: 6, u: 'W'}];
  const {rows} = rollup.fillRawGaps(raw, rollupRows, 0, 320000);
  assert.equal(rows.length, 4);
});

test('fillRawGaps nimmt eine Serie ohne eigene Rohdaten komplett aus der 1m-Stufe', () => {
  const rollup = load();
  const rollupRows = [
    {series: 'role:heat_pump', ts: 60000, min: 10, max: 10, avg: 10, n: 6, u: 'W'},
    {series: 'role:heat_pump', ts: 120000, min: 20, max: 20, avg: 20, n: 6, u: 'W'},
  ];
  const {rows} = rollup.fillRawGaps([], rollupRows, 0, 180000);
  assert.equal(rows.length, 2);
});

test('fillRawGaps haelt mehrere Serien unabhaengig auseinander', () => {
  const rollup = load();
  const raw = rollup.normalize([
    {series: 'role:pv', ts: 0, v: 1, u: 'W'},
    {series: 'role:pv', ts: 10000, v: 2, u: 'W'},
    {series: 'role:pv', ts: 20000, v: 3, u: 'W'},
    {series: 'role:pv', ts: 320000, v: 4, u: 'W'},
    {series: 'role:pv', ts: 330000, v: 5, u: 'W'},
    {series: 'role:pv', ts: 340000, v: 6, u: 'W'},
  ]);
  const rollupRows = [
    {series: 'role:pv', ts: 100000, min: 5, max: 5, avg: 5, n: 6, u: 'W'},
    {series: 'role:grid', ts: 60000, min: 7, max: 7, avg: 7, n: 6, u: 'W'},
  ];
  const {rows} = rollup.fillRawGaps(raw, rollupRows, 0, 340000);
  assert.equal(rows.filter(row => row.series === 'role:grid').length, 1);
  assert.equal(rows.filter(row => row.series === 'role:pv').length, 7);
});

test('fillRawGaps meldet die Rest-Luecke um einen isolierten Austausch-Punkt', () => {
  const rollup = load();
  const raw = rollup.normalize([
    {series: 'role:pv', ts: 0, v: 1, u: 'W'},
    {series: 'role:pv', ts: 10000, v: 2, u: 'W'},
    {series: 'role:pv', ts: 20000, v: 3, u: 'W'},
    {series: 'role:pv', ts: 1220000, v: 4, u: 'W'},
    {series: 'role:pv', ts: 1230000, v: 5, u: 'W'},
  ]);
  const rollupRows = [{series: 'role:pv', ts: 600000, min: 5, max: 5, avg: 5, n: 6, u: 'W'}];
  const {gaps} = rollup.fillRawGaps(raw, rollupRows, 0, 1230000);
  assert.deepEqual(JSON.parse(JSON.stringify(gaps.get('role:pv'))), [
    {from: 20000, to: 600000},
    {from: 600000, to: 1220000},
  ]);
});

test('gapsBySeries liefert je Serie eigene Luecken inklusive der Fensterraender', () => {
  const rollup = load();
  const rows = [
    ...[0, 60000, 120000].map(ts => ({series: 'role:pv', ts})),
    ...[600000, 660000, 720000].map(ts => ({series: 'role:grid', ts})),
  ];
  const gaps = rollup.gapsBySeries(rows, 0, 720000);
  assert.deepEqual(JSON.parse(JSON.stringify(gaps.get('role:pv'))), [{from: 120000, to: 720000}]);
  assert.deepEqual(JSON.parse(JSON.stringify(gaps.get('role:grid'))), [{from: 0, to: 600000}]);
});

test('withGapBreaks bricht die Linie auch, wenn der Luecken-Rand keinen Punkt trifft', () => {
  const rollup = load();
  // So sieht es nach thin() aus: die Punkte liegen auf Bucket-Grenzen, die
  // Luecke wurde auf den ungeduennten Saetzen bestimmt. Die vorige Fassung
  // verglich exakt auf gap.from und setzte hier gar keinen Abbruch.
  const pairs = [[0, 1], [15000, 2], [315000, 3], [330000, 4]];
  const gaps = [{from: 20000, to: 320000}];
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.withGapBreaks(pairs, gaps))), [
    [0, 1],
    [15000, 2],
    [15001, null],
    [315000, 3],
    [330000, 4],
  ]);
});

test('withGapBreaks behandelt mehrere Luecken in derselben Reihe', () => {
  const rollup = load();
  const pairs = [[0, 1], [10000, 2], [500000, 3], [510000, 4], [900000, 5]];
  const gaps = [{from: 10000, to: 500000}, {from: 510000, to: 900000}];
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.withGapBreaks(pairs, gaps))), [
    [0, 1],
    [10000, 2],
    [10001, null],
    [500000, 3],
    [510000, 4],
    [510001, null],
    [900000, 5],
  ]);
});

test('gapStubs zieht den letzten bekannten Wert an beiden Luecken-Raendern flach in die Luecke hinein', () => {
  const rollup = load();
  const pairs = [[0, 10], [1000, 12], [100000, 20], [101000, 22]];
  const gaps = [{from: 1000, to: 100000}];
  // Jeder Stummel liegt zwischen from und to und ist beidseitig null-umzaeunt.
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.gapStubs(pairs, gaps, 5000))), [
    [999, null],
    [1000, 12],
    [6000, 12],
    [6001, null],
    [94999, null],
    [95000, 20],
    [100000, 20],
    [100001, null],
  ]);
});

test('gapStubs deckelt den Stummel auf die halbe Luecke, damit sich die Raender nie beruehren', () => {
  const rollup = load();
  const pairs = [[0, 10], [1000, 12], [5000, 20], [6000, 22]];
  const gaps = [{from: 1000, to: 5000}];
  // reachMs 9000 waere laenger als die halbe Luecke (2000).
  const stubs = rollup.gapStubs(pairs, gaps, 9000);
  assert.deepEqual(JSON.parse(JSON.stringify(stubs)), [
    [999, null],
    [1000, 12],
    [3000, 12],
    [3001, null],
    [3001, null],
    [3001, 20],
    [5000, 20],
    [5001, null],
  ]);
  // Streng aufsteigend in x - kein Ruecksprung, der quer ueber den Chart zoege.
  for (let i = 1; i < stubs.length; i += 1) assert.ok(stubs[i][0] >= stubs[i - 1][0]);
});

test('gapStubs laesst nur den Rand aus, der keinen echten Nachbarpunkt hat', () => {
  const rollup = load();
  const pairs = [[100000, 20], [101000, 22]];
  const gaps = [{from: 0, to: 100000}];
  // Kein Punkt vor der Luecke - nur der rechte Stummel entsteht.
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.gapStubs(pairs, gaps, 5000))), [
    [94999, null],
    [95000, 20],
    [100000, 20],
    [100001, null],
  ]);
});

test('gapStubs verkettet keine Stummel quer ueber den Chart, wenn Luecken doppelt oder ueberlappend ankommen', () => {
  const rollup = load();
  const pairs = [[0, 10], [10000, 50]];
  const g = {from: 100, to: 900};
  // Wie bei der abgeleiteten Serie: dieselbe Luecke mehrfach, dazu eine
  // ueberlappende. Ohne Merge/Null-Zaun zog das eine Linie von x=900
  // zurueck auf x=100.
  const stubs = rollup.gapStubs(pairs, [g, g, {from: 300, to: 900}], 200);
  // Nach dem Merge bleibt eine Luecke; die Ausgabe ist streng aufsteigend.
  for (let i = 1; i < stubs.length; i += 1) {
    assert.ok(stubs[i][0] >= stubs[i - 1][0], `x-Ruecksprung ${JSON.stringify(stubs[i - 1])} -> ${JSON.stringify(stubs[i])}`);
  }
});

test('mergeIntervals fasst doppelte und ueberlappende Intervalle zu disjunkten zusammen', () => {
  const rollup = load();
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.mergeIntervals([
    {from: 300, to: 900}, {from: 100, to: 500}, {from: 100, to: 500}, {from: 1000, to: 1200},
  ]))), [
    {from: 100, to: 900},
    {from: 1000, to: 1200},
  ]);
});

test('mergeIntervals laesst beruehrende Intervalle getrennt - dazwischen liegt ein echter Punkt', () => {
  const rollup = load();
  assert.deepEqual(JSON.parse(JSON.stringify(rollup.mergeIntervals([
    {from: 100, to: 500}, {from: 500, to: 900},
  ]))), [
    {from: 100, to: 500},
    {from: 500, to: 900},
  ]);
});

test('gapStubs bleibt leer ohne Luecken, ohne Punkte oder ohne Reichweite', () => {
  const rollup = load();
  const pairs = [[0, 10], [100000, 20]];
  assert.equal(rollup.gapStubs(pairs, [], 5000).length, 0);
  assert.equal(rollup.gapStubs([], [{from: 1, to: 2}], 5000).length, 0);
  assert.equal(rollup.gapStubs(pairs, [{from: 1, to: 2}], 0).length, 0);
});

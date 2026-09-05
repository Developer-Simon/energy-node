// Regressionstests fuer den hostunabhaengigen Kern der Batteriekarte. Der
// Kern bekommt ein flaches BatteryInput und kennt weder Schnappschuss noch
// hass - deshalb braucht kein Test hier eine der beiden Welten.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const CORE = path.join(here, '..', 'internal', 'webui', 'static', 'js', 'battery-card-core.js');

export function loadCore(extraGlobals = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  for (const [key, value] of Object.entries(extraGlobals)) dom.window[key] = value;
  const context = dom.getInternalVMContext();
  vm.runInContext(fs.readFileSync(CORE, 'utf8'), context);
  return { core: dom.window.BatteryCardCore, window: dom.window, document: dom.window.document };
}

// 12,8 kWh nutzbar, 62 % Ladestand, 1,24 kW Entladeleistung, Reserve 15 %.
export const evening = { soc: 62, capacity: 12.8, watts: -1240, reserve: 15, nowTs: 1_757_000_000_000 };

test('der Kern greift auf kein fremdes Global zu', () => {
  // Kommentare raus: der Kern *erklaert* die Regel in seinem Kopf und wuerde
  // sonst an seiner eigenen Begruendung scheitern.
  const code = fs.readFileSync(CORE, 'utf8')
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/^\s*\/\/.*$/gm, '');
  for (const forbidden of ['EnergyModel', 'HistoryStore', 'HistoryRollup', 'Alpine', 'hass']) {
    assert.doesNotMatch(code, new RegExp(`\\b${forbidden}\\b`), `Kern greift auf ${forbidden} zu`);
  }
});

test('batteryState rechnet Vorrat und abrufbare Energie gegen die Reserve', () => {
  const { core } = loadCore();
  const state = core.batteryState(core.normalizeInput(evening));
  assert.equal(state.mode, 'discharge');
  assert.ok(Math.abs(state.stored - 7.936) < 0.001, `stored = ${state.stored}`);
  assert.ok(Math.abs(state.reserveKWh - 1.92) < 0.001, `reserveKWh = ${state.reserveKWh}`);
  assert.ok(Math.abs(state.usable - 6.016) < 0.001, `usable = ${state.usable}`);
});

test('runtime zielt beim Entladen auf die Reserve', () => {
  const { core } = loadCore();
  const run = core.runtime(core.batteryState(core.normalizeInput(evening)));
  assert.equal(run.kind, 'reserve');
  assert.equal(run.bound, 15);
  assert.ok(Math.abs(run.hours - 4.8516) < 0.001, `hours = ${run.hours}`);
});

// Der Kern der Anforderung: abgeschaltete Reserve rechnet bis 0 %.
test('runtime rechnet ohne Reserve bis auf 0 Prozent', () => {
  const { core } = loadCore();
  const state = core.batteryState(core.normalizeInput({ ...evening, reserve: 0 }));
  const run = core.runtime(state);
  assert.equal(state.reserveKWh, 0);
  assert.ok(Math.abs(state.usable - 7.936) < 0.001, `usable = ${state.usable}`);
  assert.equal(run.kind, 'empty');
  assert.equal(run.bound, 0);
  assert.ok(Math.abs(run.hours - 6.4) < 0.001, `hours = ${run.hours}`);
});

test('runtime liefert unterhalb der Reserve null Stunden statt einer negativen Zahl', () => {
  const { core } = loadCore();
  const run = core.runtime(core.batteryState(core.normalizeInput({ ...evening, soc: 8 })));
  assert.equal(run.hours, 0);
});

test('runtime zielt beim Laden auf 100 Prozent', () => {
  const { core } = loadCore();
  const run = core.runtime(core.batteryState(core.normalizeInput({ soc: 38, capacity: 12.8, watts: 3450, reserve: 15 })));
  assert.equal(run.kind, 'full');
  assert.equal(run.bound, 100);
  assert.ok(Math.abs(run.hours - 2.2998) < 0.001, `hours = ${run.hours}`);
});

test('runtime erfindet ohne Fluss und ohne Kapazität keine Restlaufzeit', () => {
  const { core } = loadCore();
  assert.equal(core.runtime(core.batteryState(core.normalizeInput({ soc: 62, capacity: 12.8, watts: 0 }))).hours, null);
  assert.equal(core.runtime(core.batteryState(core.normalizeInput({ soc: 62, capacity: 0, watts: -1240 }))).hours, null);
});

// Home Assistant bringt mit sensor.*_time_to_empty eine eigene Zahl mit, die
// die Entladekurve kennt. Wo sie da ist, gewinnt sie gegen die lineare
// Schaetzung - die Grenze und ihre Benennung bleiben die des Kerns.
test('runtime übernimmt eine mitgelieferte Restlaufzeit', () => {
  const { core } = loadCore();
  const run = core.runtime(core.batteryState(core.normalizeInput(evening)), 3.5);
  assert.equal(run.hours, 3.5);
  assert.equal(run.kind, 'reserve');
});

// Die mitgelieferte Zahl ist ihrer Natur nach eine Entlade-Schaetzung
// (sensor.*_time_to_empty). Beim Laden gilt sie nicht - sonst stuende eine
// Zeit-bis-leer unter der Beschriftung "bis voll".
test('runtime ignoriert die mitgelieferte Restlaufzeit beim Laden', () => {
  const { core } = loadCore();
  const state = core.batteryState(core.normalizeInput({ soc: 38, capacity: 12.8, watts: 3450, reserve: 15 }));
  const run = core.runtime(state, 3.5);
  assert.equal(run.kind, 'full');
  assert.ok(Math.abs(run.hours - 2.2998) < 0.001, `hours = ${run.hours}`);
});

test('toneOf warnt unter drei Stunden, schlägt unter einer an und meldet veraltete Werte', () => {
  const { core } = loadCore();
  const at = (soc, watts, stale = false) => {
    const state = core.batteryState(core.normalizeInput({ ...evening, soc, watts, stale }));
    return core.toneOf(state, core.runtime(state));
  };
  assert.equal(at(62, -1240), 'ok');
  assert.equal(at(30, -1240), 'warn');
  assert.equal(at(18, -2100), 'bad');
  assert.equal(at(62, -1240, true), 'stale');
});

test('forecast flacht an der angesteuerten Grenze ab', () => {
  const { core } = loadCore();
  const state = core.batteryState(core.normalizeInput(evening));
  const points = core.forecast(state, core.runtime(state));
  assert.equal(points.length, 13);
  assert.equal(points[0].v, 62);
  assert.ok(points[12].v >= 15, 'Fortschreibung faellt unter die Reserve');
  assert.ok(points[12].v < 16, 'Fortschreibung bleibt oberhalb der Reserve stehen');
});

test('normalizeInput klemmt Bereiche und füllt Vorgaben', () => {
  const { core } = loadCore();
  const input = core.normalizeInput({ soc: 140, capacity: -3, reserve: 250 });
  assert.equal(input.soc, 100);
  assert.equal(input.capacity, 0);
  assert.equal(input.reserve, 100);
  assert.deepEqual(JSON.parse(JSON.stringify(input.history)), []);
  assert.equal(typeof input.nowTs, 'number');
  assert.equal(core.normalizeInput({}).soc, null);
});

test('formatRuntime liest sich wie eine Uhrzeit und wechselt unter einer Stunde auf Minuten', () => {
  const { core } = loadCore();
  assert.equal(core.formatRuntime(4.8516), '4:51 h');
  assert.equal(core.formatRuntime(0.183), '11 min');
  assert.equal(core.formatRuntime(null), null);
});

const MINUTE = 60 * 1000;
const NOW = evening.nowTs;
const SIX_HOURS = 6 * 60 * MINUTE;

// Gleichmaessige Reihe, ts aufsteigend, letzter Punkt bei endTs.
const series = (count, stepMs, endTs = NOW) =>
  Array.from({ length: count }, (_, index) => ({ ts: endTs - (count - 1 - index) * stepMs, v: 60 - index }));

test('socSegments hält eine lückenlose Reihe in einem Segment', () => {
  const { core } = loadCore();
  const result = core.socSegments(series(13, 30 * MINUTE), NOW - SIX_HOURS, NOW);
  assert.equal(result.segments.length, 1);
  assert.equal(result.segments[0].length, 13);
  assert.equal(result.covered.to, NOW);
});

// Der eigentliche Fall: der Wirt war zwischendurch zu. Ueber die Luecke wird
// nicht gezeichnet.
test('socSegments bricht die Linie an einer Aufzeichnungslücke auf', () => {
  const { core } = loadCore();
  const points = [...series(6, 10 * MINUTE, NOW - 150 * MINUTE), ...series(6, 10 * MINUTE, NOW)];
  const result = core.socSegments(points, NOW - SIX_HOURS, NOW);
  assert.equal(result.segments.length, 2);
  assert.equal(result.segments[0].length, 6);
  assert.equal(result.segments[1].length, 6);
});

test('socSegments verwirft Punkte außerhalb des Fensters und unbrauchbare Werte', () => {
  const { core } = loadCore();
  const points = [
    { ts: NOW - 12 * 60 * MINUTE, v: 90 },
    ...series(4, 30 * MINUTE),
    { ts: NOW - 10 * MINUTE, v: null },
  ];
  assert.equal(core.socSegments(points, NOW - SIX_HOURS, NOW).points.length, 4);
});

test('historyMode meldet "none", wenn gar kein Verlauf da ist', () => {
  const { core } = loadCore();
  const mode = core.historyMode(core.socSegments([], NOW - SIX_HOURS, NOW), NOW, SIX_HOURS);
  assert.equal(mode.mode, 'none');
  assert.equal(mode.spanMs, 0);
});

// Ein gerade erst geoeffnetes Dashboard hat ein paar Minuten - zu wenig fuer
// eine Zeitachse. Das zaehlt wie kein Verlauf.
test('historyMode wertet weniger als 15 Minuten wie keinen Verlauf', () => {
  const { core } = loadCore();
  const mode = core.historyMode(core.socSegments(series(5, 2 * MINUTE), NOW - SIX_HOURS, NOW), NOW, SIX_HOURS);
  assert.equal(mode.mode, 'none');
});

test('historyMode meldet einen Teilverlauf mit seiner echten Spanne', () => {
  const { core } = loadCore();
  const mode = core.historyMode(core.socSegments(series(5, 30 * MINUTE), NOW - SIX_HOURS, NOW), NOW, SIX_HOURS);
  assert.equal(mode.mode, 'partial');
  assert.equal(mode.spanMs, 2 * 60 * MINUTE);
  assert.equal(mode.fromTs, NOW - 2 * 60 * MINUTE);
});

test('historyMode meldet ein volles Fenster', () => {
  const { core } = loadCore();
  const mode = core.historyMode(core.socSegments(series(13, 30 * MINUTE), NOW - SIX_HOURS, NOW), NOW, SIX_HOURS);
  assert.equal(mode.mode, 'full');
  assert.equal(mode.spanMs, SIX_HOURS);
});

test('readHistory normalisiert v und avg und sortiert nach Zeit', async () => {
  const { core } = loadCore();
  const reader = async () => [{ ts: NOW, avg: 62 }, { ts: NOW - MINUTE, v: 61 }, { ts: NOW - 2 * MINUTE, v: 'kaputt' }];
  assert.deepEqual(JSON.parse(JSON.stringify(await core.readHistory(reader, NOW - SIX_HOURS, NOW))), [
    { ts: NOW - MINUTE, v: 61 }, { ts: NOW, v: 62 },
  ]);
});

// Ein Recorder, der nicht antwortet, darf die Karte nicht mitreissen - sie
// hat dann eben keinen Verlauf, und dafuer gibt es schon einen Fall.
test('readHistory liefert bei einem Fehler eine leere Reihe statt zu werfen', async () => {
  const { core } = loadCore();
  assert.deepEqual(JSON.parse(JSON.stringify(await core.readHistory(async () => { throw new Error('offline'); }, 0, 1))), []);
  assert.deepEqual(JSON.parse(JSON.stringify(await core.readHistory(null, 0, 1))), []);
});

function geometryFor(points, reserve = 15) {
  const { core } = loadCore();
  const state = core.batteryState(core.normalizeInput({ ...evening, reserve }));
  const run = core.runtime(state);
  const windowed = core.socSegments(points, NOW - SIX_HOURS, NOW);
  const mode = core.historyMode(windowed, NOW, SIX_HOURS);
  return { core, geometry: core.chartGeometry(state, run, windowed, mode), mode };
}

test('chartGeometry teilt die Fläche mittig, wenn der Verlauf voll ist', () => {
  const { geometry } = geometryFor(series(13, 30 * MINUTE));
  assert.equal(geometry.historyPaths.length, 1);
  assert.ok(Math.abs(geometry.nowX - 160) < 0.5, `nowX = ${geometry.nowX}`);
  assert.equal(geometry.hint, null);
  assert.equal(geometry.ticks.left, '−6 h');
  assert.equal(geometry.ticks.center, 'jetzt');
});

// Der Fall aus der Anforderung: der Wirt war sechs Stunden zu.
test('chartGeometry gibt der Fortschreibung ohne Verlauf die volle Breite', () => {
  const { geometry, mode } = geometryFor([]);
  assert.equal(mode.mode, 'none');
  assert.equal(geometry.historyPaths.length, 0);
  assert.ok(Math.abs(geometry.nowX - 10) < 0.5, `nowX = ${geometry.nowX}`);
  assert.match(geometry.hint, /Kein Verlauf/i);
  assert.equal(geometry.ticks.left, 'jetzt');
  assert.equal(geometry.ticks.center, '');
  assert.equal(geometry.ticks.right, '+6 h');
  assert.ok(geometry.forecastPath.length > 0, 'Fortschreibung fehlt');
});

test('chartGeometry nennt bei Teilverlauf die echte Spanne', () => {
  const { geometry } = geometryFor(series(5, 30 * MINUTE));
  assert.equal(geometry.ticks.left, '−2 h');
  assert.ok(geometry.nowX > 10 && geometry.nowX < 160, `nowX = ${geometry.nowX}`);
  assert.match(geometry.hint, /2 h aufgezeichnet/i);
});

test('chartGeometry bricht die Linie an einer Lücke in zwei Pfade', () => {
  const { geometry } = geometryFor([...series(6, 10 * MINUTE, NOW - 150 * MINUTE), ...series(6, 10 * MINUTE, NOW)]);
  assert.equal(geometry.historyPaths.length, 2);
});

test('chartGeometry zeigt das Reserve-Band und die Kreuzung', () => {
  const { geometry } = geometryFor(series(13, 30 * MINUTE), 15);
  assert.equal(geometry.showBand, true);
  assert.ok(geometry.bandHeight > 0);
  assert.ok(geometry.crossing, 'keine Kreuzung berechnet');
  assert.match(geometry.crossing.label, /Reserve/);
});

// Reserve aus: kein Band, und die Grenze heisst "leer".
test('chartGeometry lässt das Band weg, wenn die Reserve aus ist', () => {
  const { geometry } = geometryFor(series(13, 30 * MINUTE), 0);
  assert.equal(geometry.showBand, false);
  assert.equal(geometry.bandHeight, 0);
  assert.match(geometry.crossing.label, /leer/i);
});

test('chartGeometry meldet keine Kreuzung außerhalb des Fensters', () => {
  const { core } = loadCore();
  const state = core.batteryState(core.normalizeInput({ soc: 95, capacity: 12.8, watts: -200, reserve: 15 }));
  const windowed = core.socSegments(series(13, 30 * MINUTE), NOW - SIX_HOURS, NOW);
  const geometry = core.chartGeometry(state, core.runtime(state), windowed, core.historyMode(windowed, NOW, SIX_HOURS));
  assert.equal(geometry.crossing, null);
});

test('chartGeometry hält jede gezeichnete Koordinate in der viewBox', () => {
  const { core, geometry } = geometryFor(series(13, 30 * MINUTE));
  const coords = [...geometry.historyPaths, geometry.forecastPath]
    .join(' ').split(/\s+/).filter(Boolean).map(pair => pair.split(',').map(Number));
  for (const [x, y] of coords) {
    assert.ok(x >= core.VIEW.x0 - 0.6 && x <= core.VIEW.x1 + 0.6, `x = ${x}`);
    assert.ok(y >= core.VIEW.y0 - 0.6 && y <= core.VIEW.y1 + 0.6, `y = ${y}`);
  }
});

// --- Einstellbare Zeitfenster ------------------------------------------------

test('chartGeometry beschriftet die Fortschreibung mit der übergebenen Stundenzahl', () => {
  const { core } = loadCore();
  const state = core.batteryState(core.normalizeInput(evening));
  const windowed = core.socSegments(series(13, 30 * MINUTE), NOW - SIX_HOURS, NOW);
  const mode = core.historyMode(windowed, NOW, SIX_HOURS);
  const geometry = core.chartGeometry(state, core.runtime(state), windowed, mode, { forecastHours: 12 });
  assert.equal(geometry.ticks.right, '+12 h');
});

test('chartGeometry zeichnet in die übergebene Breite statt in die feste viewBox', () => {
  const { core } = loadCore();
  const state = core.batteryState(core.normalizeInput(evening));
  const windowed = core.socSegments(series(13, 30 * MINUTE), NOW - SIX_HOURS, NOW);
  const mode = core.historyMode(windowed, NOW, SIX_HOURS);

  const narrow = core.chartGeometry(state, core.runtime(state), windowed, mode);
  assert.equal(narrow.view.x1, core.VIEW.x1);

  const wide = core.chartGeometry(state, core.runtime(state), windowed, mode, { width: 900 });
  assert.equal(wide.view.x1, 900 - core.VIEW.x0);
  const xs = [...wide.historyPaths, wide.forecastPath]
    .join(' ').split(/\s+/).filter(Boolean).map(pair => Number(pair.split(',')[0]));
  for (const x of xs) assert.ok(x <= wide.view.x1 + 0.6, `x = ${x}`);
});

test('viewFrom lässt die Projektion dem Verlaufsfenster folgen und erlaubt eine eigene', () => {
  const { core } = loadCore();
  const paired = core.viewFrom({ ...evening, historyHours: 3 });
  assert.equal(paired.chartInputs.forecastHours, 3);
  assert.equal(paired.chart.ticks.right, '+3 h');

  const split = core.viewFrom({ ...evening, historyHours: 12, forecastHours: 6 });
  assert.equal(split.chartInputs.forecastHours, 6);
  assert.equal(split.chart.ticks.right, '+6 h');
});

test('viewFrom liest den Verlauf über das eingestellte Fenster', () => {
  const { core } = loadCore();
  // Punkte, die nur in ein 12-h-Fenster fallen (älter als 6 h).
  const older = series(6, 30 * MINUTE, NOW - 7 * 60 * MINUTE);
  assert.equal(core.viewFrom({ ...evening, history: older }).chart.historyPaths.length, 0);
  assert.equal(core.viewFrom({ ...evening, historyHours: 12, history: older }).chart.historyPaths.length, 1);
});

test('viewFrom nennt Deckung, Reserve und Kapazität', () => {
  const { core } = loadCore();
  const view = core.viewFrom(evening);
  assert.equal(view.tone, 'ok');
  assert.equal(view.socLabel, '62 %');
  assert.match(view.stateLabel, /Entlädt/);
  assert.equal(view.rows.length, 3);
  assert.equal(view.rows[0].key, 'coverage');
  assert.match(view.rows[0].value, /4:51/);
  assert.equal(view.rows[1].key, 'reserve');
  assert.match(view.rows[1].value, /15\s*%/);
  assert.match(view.rows[1].value, /1,9\s*kWh/);
  assert.equal(view.rows[2].key, 'capacity');
});

// Abgeschaltete Reserve: dieselbe Zeilenzahl, andere Zeile. Keine Leere und
// keine "0 % Reserve", die es nicht gibt.
test('viewFrom ersetzt die Reserve-Zeile durch den Vorrat, wenn die Reserve aus ist', () => {
  const { core } = loadCore();
  const view = core.viewFrom({ ...evening, reserve: 0 });
  assert.equal(view.rows.length, 3);
  assert.equal(view.rows[1].key, 'stock');
  assert.match(view.rows[1].value, /7,9\s*kWh/);
  assert.match(view.rows[1].note, /keine Reserve/i);
  assert.equal(view.gauge.showReserve, false);
});

test('viewFrom sagt es, wenn keine Speicher-Rolle zugeordnet ist', () => {
  const { core } = loadCore();
  const view = core.viewFrom({ capacity: 12.8 });
  assert.equal(view.socLabel, '—');
  assert.match(view.rows[0].value, /keine/i);
});

test('mountColumn baut das Gerüst genau einmal und schreibt danach nur Werte', () => {
  const { core, document } = loadCore();
  const root = document.createElement('div');
  const card = core.mountColumn(root);
  card.update(core.viewFrom(evening));
  const fill = root.querySelector('.battery-column-fill');
  const marker = root.querySelector('.battery-column-pct');
  assert.ok(fill && marker);
  assert.match(fill.getAttribute('style'), /scaleY\(0\.62\)/);
  assert.equal(marker.textContent, '62 %');

  card.update(core.viewFrom({ ...evening, soc: 30 }));
  // Dieselben Knoten, nur andere Werte - sonst faellt jede Transition aus.
  assert.equal(root.querySelector('.battery-column-fill'), fill);
  assert.equal(root.querySelector('.battery-column-pct'), marker);
  assert.match(fill.getAttribute('style'), /scaleY\(0\.3\)/);
  assert.equal(root.querySelectorAll('.battery-row').length, 3);
});

test('mountTrajectory zeichnet Ring, Verlauf und Fortschreibung', () => {
  const { core, document } = loadCore();
  const root = document.createElement('div');
  const card = core.mountTrajectory(root);
  card.update(core.viewFrom({ ...evening, history: series(13, 30 * MINUTE) }));
  assert.equal(root.querySelectorAll('polyline.battery-hist').length, 1);
  assert.ok(root.querySelector('polyline.battery-forecast').getAttribute('points').length > 0);
  const arc = root.querySelector('.battery-ring-arc');
  assert.ok(Math.abs(Number(arc.getAttribute('stroke-dashoffset')) - core.RING_CIRCUMFERENCE * 0.38) < 0.5);
  assert.equal(root.querySelector('.battery-hint').hidden, true);
});

test('mountTrajectory zeigt ohne Verlauf den Hinweis und keine Vergangenheitslinie', () => {
  const { core, document } = loadCore();
  const root = document.createElement('div');
  const card = core.mountTrajectory(root);
  card.update(core.viewFrom(evening));
  assert.equal(root.querySelectorAll('polyline.battery-hist').length, 0);
  const hint = root.querySelector('.battery-hint');
  assert.equal(hint.hidden, false);
  assert.match(hint.textContent, /Kein Verlauf/i);
});

test('mountTrajectory passt die Zahl der Verlaufslinien an, ohne den Rest neu zu bauen', () => {
  const { core, document } = loadCore();
  const root = document.createElement('div');
  const card = core.mountTrajectory(root);
  card.update(core.viewFrom({ ...evening, history: series(13, 30 * MINUTE) }));
  const arc = root.querySelector('.battery-ring-arc');
  card.update(core.viewFrom({ ...evening, history: [...series(6, 10 * MINUTE, NOW - 150 * MINUTE), ...series(6, 10 * MINUTE, NOW)] }));
  assert.equal(root.querySelectorAll('polyline.battery-hist').length, 2);
  assert.equal(root.querySelector('.battery-ring-arc'), arc);
});

// jsdom bringt keinen ResizeObserver mit - der Kern muss ohne einen
// auskommen (feste viewBox) und einen nutzen, sobald er da ist.
test('mountTrajectory rechnet den Chart auf die beobachtete Breite um', () => {
  let roCallback = null;
  class FakeResizeObserver {
    constructor(cb) { roCallback = cb; }
    observe() {}
    disconnect() {}
  }
  const { core, document } = loadCore({ ResizeObserver: FakeResizeObserver });

  const root = document.createElement('div');
  const card = core.mountTrajectory(root);
  card.update(core.viewFrom({ ...evening, history: series(13, 30 * MINUTE) }));

  const svg = root.querySelector('.battery-chart svg');
  assert.equal(svg.getAttribute('viewBox'), '0 0 320 130');

  assert.equal(typeof roCallback, 'function', 'kein ResizeObserver angemeldet');
  roCallback([{ contentRect: { width: 900 } }]);

  const width = Number(svg.getAttribute('viewBox').split(' ')[2]);
  assert.ok(width > 600, `viewBox-Breite blieb ${width}`);
  const tickRight = root.querySelector('text.battery-tick[text-anchor="end"]');
  assert.ok(Math.abs(Number(tickRight.getAttribute('x')) - (width - core.VIEW.x0)) < 1,
    `rechte Beschriftung bei x=${tickRight.getAttribute('x')}, viewBox-Breite ${width}`);
});

test('das Karten-CSS nennt keine feste Farbe ohne Fallback-Kette und keine font-family', () => {
  const { core } = loadCore();
  assert.doesNotMatch(core.CARD_CSS, /font-family/);
  // Jede Farbe kommt aus einer var()-Kette, die in beiden Wirten aufgeht.
  assert.match(core.CARD_CSS, /var\(--panel, var\(--ha-card-background/);
  assert.match(core.CARD_CSS, /prefers-reduced-motion/);
});

test('injectStyle legt genau ein style-Element an, auch bei mehreren Karten', () => {
  const { core, document } = loadCore();
  core.injectStyle(document);
  core.injectStyle(document);
  assert.equal(document.head.querySelectorAll('style[data-battery-card-style]').length, 1);
});

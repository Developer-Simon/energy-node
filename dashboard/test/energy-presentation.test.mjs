// Regressionstests fuer energy-presentation.js: die Mechanik, mit der alle
// Energiekacheln den outerHTML-Tausch von #overview-live ueberleben, ohne auf
// ihren Zielwert zu springen. Bis 2026-08 lag sie privat in energy-flow.js.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');
const scriptSource = read('energy-presentation.js');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  vm.runInContext(scriptSource, context);
  return { presentation: dom.window.EnergyPresentation, window: dom.window };
}

test('advanceSpring naehert sich dem Ziel, ohne zu ueberschwingen', () => {
  const { presentation } = load();
  const spring = { value: 0, velocity: 0 };
  let previous = 0;
  for (let step = 0; step < 200; step += 1) {
    presentation.advanceSpring(spring, 1000, 1 / 60);
    assert.ok(spring.value <= 1000, `Ueberschwingen in Schritt ${step}: ${spring.value}`);
    assert.ok(spring.value >= previous, `nicht monoton in Schritt ${step}`);
    previous = spring.value;
  }
  assert.equal(spring.value, 1000);
});

test('advanceSpring meldet Stillstand, sobald die Ruheschwellen unterschritten sind', () => {
  const { presentation } = load();
  const spring = { value: 999.9, velocity: 0 };
  assert.equal(presentation.advanceSpring(spring, 1000, 1 / 60), false);
  assert.equal(spring.value, 1000);
  assert.equal(spring.velocity, 0);
});

test('advanceSpring bleibt bei grossem dt stabil statt zu explodieren', () => {
  const { presentation } = load();
  const spring = { value: 0, velocity: 0 };
  presentation.advanceSpring(spring, 1000, 60);
  assert.equal(spring.value, 1000);
  assert.equal(spring.velocity, 0);
});

test('advancePhase beginnt ohne Vorgaenger bei 0', () => {
  const { presentation } = load();
  assert.equal(presentation.advancePhase(undefined, 1000), 0);
});

test('advancePhase haelt die Phase einer pausierten Animation fest', () => {
  const { presentation } = load();
  const previous = { phase: 0.25, duration: 1.4, at: 0, running: false };
  assert.equal(presentation.advancePhase(previous, 100000), 0.25);
});

test('advancePhase schreibt eine laufende Animation um den verstrichenen Anteil fort', () => {
  const { presentation } = load();
  const previous = { phase: 0.25, duration: 2, at: 1000, running: true };
  // 1 s von 2 s Umlaufzeit = ein halber Umlauf.
  assert.equal(presentation.advancePhase(previous, 2000), 0.75);
});

test('advancePhase laeuft ueber 1 hinaus wieder bei 0 an', () => {
  const { presentation } = load();
  const previous = { phase: 0.75, duration: 2, at: 1000, running: true };
  assert.ok(Math.abs(presentation.advancePhase(previous, 2000) - 0.25) < 1e-9);
});

test('animatable sammelt jede Zahl aus values und jede Menge aus der Bilanz', () => {
  const { presentation } = load();
  const targets = presentation.animatable({
    values: { pv: 1200, grid: -300, kaputt: 'nicht numerisch' },
    balance: { pv: 1200, base: 500, autarkie: 0.75, load_total: 900, load_measured: 400, load_source: 'measured' },
  });
  assert.deepEqual([...targets.keys()].sort(), [
    'balance.autarkie', 'balance.base', 'balance.load_measured', 'balance.load_total', 'balance.pv',
    'values.grid', 'values.pv',
  ]);
  assert.equal(targets.get('values.pv'), 1200);
  assert.equal(targets.get('balance.autarkie'), 0.75);
});

// balanceOf() bevorzugt den fertigen snapshot.balance des Servers, und
// composeBalance() baut die Ringsegmente aus dessen Einzelfeldern. Waeren die
// nicht gefedert, spraenge jedes Segment - genau der Fehler, den diese Liste
// behebt. Schwellen und Typenschilder gehoeren umgekehrt nicht hinein.
test('animatable federt keine Schwellen und keine Typenschilder', () => {
  const { presentation } = load();
  const targets = presentation.animatable({
    values: {},
    balance: { gap_tolerance_w: 25, battery_capacity_kwh: 10, battery_soc: 62, unbalanced: true, gap_applied: 40 },
  });
  assert.deepEqual([...targets.keys()].sort(), ['balance.gap_applied']);
});

test('settleFor gibt Anteilen eine feinere Ruheschwelle als Leistungen', () => {
  const { presentation } = load();
  assert.deepEqual([...presentation.settleFor('balance.pv')], [presentation.SETTLE_VALUE, presentation.SETTLE_VELOCITY]);
  assert.deepEqual([...presentation.settleFor('balance.autarkie')], [presentation.FRACTION_SETTLE_VALUE, presentation.FRACTION_SETTLE_VELOCITY]);
  assert.ok(presentation.FRACTION_SETTLE_VALUE < 0.001, 'muss unter der Rundung von formatPercent() liegen');
});

// Mit SETTLE_VALUE = 0.5 waere ein Anteil zwischen 0 und 1 im ersten Schritt
// "fertig" - der Autarkiegrad in der Ringmitte spraenge trotz Feder.
test('ein Anteil federt, statt an der Watt-Schwelle sofort einzurasten', () => {
  const { presentation } = load();
  const spring = { value: 0.2, velocity: 0 };
  const [settleValue, settleVelocity] = presentation.settleFor('balance.autarkie');
  assert.equal(presentation.advanceSpring(spring, 0.8, 1 / 60, settleValue, settleVelocity), true);
  assert.ok(spring.value > 0.2 && spring.value < 0.8, `nicht gesprungen, sondern gefedert: ${spring.value}`);
});

test('withPresentedValues ersetzt die Zahlen, nicht die Schluesselmenge', () => {
  const { presentation } = load();
  // Eine Rolle mit exakt 0 W muss vorhanden bleiben: hasValue() unterscheidet
  // per hasOwnProperty "nicht zugeordnet" von "genau 0 W", und die ganze
  // Karte steht auf dieser Unterscheidung.
  const snapshot = { values: { pv: 1000, grid: 0 }, balance: { load_total: 900, load_source: 'measured' }, roles: [{ role: 'pv', freshness: 'live' }] };
  const springs = new Map([['values.pv', { value: 500, velocity: 0 }]]);
  const presented = presentation.withPresentedValues(snapshot, springs);
  assert.equal(presented.values.pv, 500);
  assert.equal(presented.values.grid, 0);
  assert.ok(Object.prototype.hasOwnProperty.call(presented.values, 'grid'));
  assert.equal(presented.balance.load_source, 'measured');
  assert.deepEqual(presented.roles, snapshot.roles);
  assert.equal(snapshot.values.pv, 1000, 'der rohe Schnappschuss darf nicht veraendert werden');
});

// Eine Testumgebung mit steuerbarer Uhr und steuerbarer Bildschleife: das
// Modul liest beides ueber window, es genuegt also, window zu bestuecken.
function loadWithClock() {
  const loaded = load();
  const state = { now: 0, frames: [] };
  // window.performance ist in jsdom ein Accessor ohne Setter; eine schlichte
  // Zuweisung wird stillschweigend verworfen. defineProperty ersetzt die
  // Eigenschaft tatsaechlich.
  Object.defineProperty(loaded.window, 'performance', { value: { now: () => state.now }, configurable: true });
  loaded.window.requestAnimationFrame = callback => state.frames.push(callback);
  return { ...loaded, state };
}

test('present setzt den ersten Schnappschuss ohne Bewegung', () => {
  const { presentation, state } = loadWithClock();
  const seen = [];
  const handle = presentation.present({
    key: 'karte-a', raw: { values: { pv: 1000 } }, reducedMotion: false,
    alive: () => true, apply: presented => seen.push(presented.values.pv),
  });
  assert.deepEqual(seen, [1000]);
  assert.equal(state.frames.length, 0, 'der erste Aufbau darf keine Animation sein');
  handle.release();
});

test('present federt einen zweiten Schnappschuss auf demselben Schluessel ein', () => {
  const { presentation, state } = loadWithClock();
  const seen = [];
  const apply = presented => seen.push(presented.values.pv);
  presentation.present({ key: 'karte-b', raw: { values: { pv: 1000 } }, reducedMotion: false, alive: () => true, apply }).release();

  const handle = presentation.present({ key: 'karte-b', raw: { values: { pv: 2000 } }, reducedMotion: false, alive: () => true, apply });
  assert.equal(seen[seen.length - 1], 1000, 'die neue Instanz startet beim sichtbaren Wert');
  assert.equal(state.frames.length, 1, 'und plant ein Bild ein');

  state.now = 16;
  state.frames.shift()();
  const current = seen[seen.length - 1];
  assert.ok(current > 1000 && current < 2000, `unterwegs erwartet, war ${current}`);
  handle.release();
});

test('present setzt bei reduzierter Bewegung sofort auf den Zielwert', () => {
  const { presentation, state } = loadWithClock();
  const seen = [];
  const apply = presented => seen.push(presented.values.pv);
  presentation.present({ key: 'karte-c', raw: { values: { pv: 1000 } }, reducedMotion: true, alive: () => true, apply }).release();
  const handle = presentation.present({ key: 'karte-c', raw: { values: { pv: 2000 } }, reducedMotion: true, alive: () => true, apply });
  assert.equal(seen[seen.length - 1], 2000);
  assert.equal(state.frames.length, 0, 'reduzierte Bewegung darf keine Bildschleife starten');
  handle.release();
});

test('die Bildschleife wirft eine Kachel weg, die nicht mehr im Dokument haengt', () => {
  const { presentation, state } = loadWithClock();
  let alive = true;
  let calls = 0;
  presentation.present({ key: 'karte-d', raw: { values: { pv: 1000 } }, reducedMotion: false, alive: () => true, apply: () => {} }).release();
  presentation.present({
    key: 'karte-d', raw: { values: { pv: 2000 } }, reducedMotion: false,
    alive: () => alive, apply: () => { calls += 1; },
  });
  // Der present()-Aufruf selbst zeichnet immer genau einmal synchron (der
  // sichtbare Wert der Vorgaengerin, noch nicht bewegt) - jede Komponente
  // braucht das fuer ihren ersten Render, bevor ueberhaupt ein Bild lief.
  // Was diese Probe eigentlich prueft: die Kachel wird als "nicht mehr am
  // Leben" erst *danach* abgehaengt, ein spaeteres Bild darf sie darum nicht
  // ein zweites Mal zeichnen.
  assert.equal(calls, 1, 'der synchrone Erstaufbau zeichnet');
  alive = false;
  state.now = 16;
  state.frames.shift()();
  assert.equal(calls, 1, 'eine abgehaengte Kachel darf danach nicht noch einmal gezeichnet werden');
});

test('dashDelay beginnt bei 0 und schreibt die Phase ueber Neuaufbauten fort', () => {
  const { presentation, state } = loadWithClock();
  assert.equal(presentation.dashDelay('karte-e', 'pv', 2), '-0.000s');
  state.now = 1000;   // eine Sekunde von zwei = ein halber Umlauf
  assert.equal(presentation.dashDelay('karte-e', 'pv', 2), '-1.000s');
});

test('pauseDash friert die Phase ein, solange nicht gezeichnet wird', () => {
  const { presentation, state } = loadWithClock();
  presentation.dashDelay('karte-f', 'pv', 2);
  state.now = 1000;
  presentation.pauseDash('karte-f', 'pv');
  state.now = 100000;
  assert.equal(presentation.dashDelay('karte-f', 'pv', 2), '-1.000s');
});

// Ohne update() erreicht ein neuer Schnappschuss die Federn nur ueber Abriss
// und Neumontage der Karte - genau den Neuaufbau, den dieser Plan abschafft.
test('update() setzt neue Zielwerte, ohne die Karte neu zu montieren', () => {
  const { presentation } = load();
  const applied = [];
  const handle = presentation.present({
    key: 'test-karte',
    raw: { values: { pv: 1000 } },
    reducedMotion: true,
    alive: () => true,
    apply: presented => applied.push(presented.values.pv),
  });
  assert.deepEqual(applied, [1000]);

  handle.update({ values: { pv: 2000 } });
  assert.equal(applied.length, 2);
  assert.equal(applied[1], 2000, 'update() muss den neuen Zielwert durchreichen');

  handle.release();
});

// Nach release() darf ein spaeter Aufruf nichts mehr anfassen - sonst
// schreibt eine abgeraeumte Karte weiter in tote Knoten.
test('update() nach release() bleibt wirkungslos', () => {
  const { presentation } = load();
  const applied = [];
  const handle = presentation.present({
    key: 'test-karte-2',
    raw: { values: { pv: 1000 } },
    reducedMotion: true,
    alive: () => true,
    apply: presented => applied.push(presented.values.pv),
  });
  handle.release();
  handle.update({ values: { pv: 2000 } });
  assert.equal(applied.length, 1, 'release() muss update() stilllegen');
});

// Ein Schnappschuss, sieben Karten: der Verteiler ist der Grund, warum keine
// Kartendatei fuer den SSE-Weg angefasst werden muss.
test('publish() erreicht jede angemeldete Karte', () => {
  const { presentation } = load();
  const ring = [];
  const band = [];
  const mk = (key, sink) => presentation.present({
    key, raw: { values: { pv: 100 } }, reducedMotion: true,
    alive: () => true, apply: p => sink.push(p.values.pv),
  });
  const a = mk('ring', ring);
  const b = mk('band', band);

  presentation.publish({ values: { pv: 900 } });

  assert.equal(ring.at(-1), 900);
  assert.equal(band.at(-1), 900);
  a.release();
  b.release();
});

// Eine freigegebene Karte darf keinen Schnappschuss mehr bekommen, sonst
// haelt der Verteiler abgeraeumte Kacheln am Leben.
test('publish() ueberspringt freigegebene Karten', () => {
  const { presentation } = load();
  const applied = [];
  const handle = presentation.present({
    key: 'weg', raw: { values: { pv: 100 } }, reducedMotion: true,
    alive: () => true, apply: p => applied.push(p.values.pv),
  });
  handle.release();
  presentation.publish({ values: { pv: 900 } });
  assert.equal(applied.length, 1);
});

// Die Karte haengt nach dem Tausch noch am alten Teilbaum: alive() meldet
// false. Sie muss ausgetragen werden, nicht bedient.
test('publish() traegt tote Karten aus', () => {
  const { presentation } = load();
  const applied = [];
  let living = true;
  presentation.present({
    key: 'tot', raw: { values: { pv: 100 } }, reducedMotion: true,
    alive: () => living, apply: p => applied.push(p.values.pv),
  });
  living = false;
  presentation.publish({ values: { pv: 900 } });
  assert.equal(applied.length, 1);
});

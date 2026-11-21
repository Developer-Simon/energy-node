// Tests für den hostunabhängigen Kern der Leistungsseite: Kostenmodell,
// Stufen, Budgethinweise und Verteilen. Der Kern bekommt Zeilen aus der API
// (snake_case) und kennt weder Alpine noch DOM.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const CORE = path.join(here, '..', 'internal', 'webui', 'static', 'js', 'performance-core.js');

function loadCore() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  vm.runInContext(fs.readFileSync(CORE, 'utf8'), dom.getInternalVMContext());
  return dom.window.PerformanceCore;
}

const C = loadCore();

// Festwerte der Zeilen wie in der API. Stufen und Grenzen stammen aus dem Entwurf.
const SPEC = {
  live_update: { steps: [1, 2, 3, 5, 10, 15, 30], floor: 1, round_s: 0, right: 'edit_setup', follows: 'half' },
  tick: { steps: [5, 10, 15, 20, 30, 60], floor: 5, round_s: 0.1, right: 'automations', follows: 'half' },
  regulator: { steps: [5, 10, 15, 20, 30, 60], floor: 5, round_s: 0, right: 'automations', follows: 'balance' },
  node: { steps: [5, 10, 15, 30, 60, 120], floor: 5, round_s: 0.1, right: 'system_actions', source: true },
  shelly: { steps: [2, 3, 5, 10, 15, 20, 30, 60], floor: 2, round_s: 0.4, right: 'system_actions', source: true, balance: true },
  trucki: { steps: [2, 3, 5, 10, 15, 20, 30, 60], floor: 2, round_s: 1.2, right: 'system_actions', source: true },
  battery_soc: { steps: [1, 2, 5, 10, 20, 30], floor: 1, round_s: 0, right: 'system_actions', source: true },
  apsystems: { steps: [12, 15, 20, 30, 60, 120], floor: 12, round_s: 3.1, right: 'system_actions', source: true },
  tuya: { steps: [10, 15, 20, 30, 60], floor: 10, round_s: 2.4, right: 'system_actions', source: true },
};

// items: id -> [aktuelle Periode s, Kosten in % bei dieser Periode]
function rowsOf(items) {
  return Object.entries(items).map(([id, [current, costPct]]) => {
    const s = SPEC[id];
    return {
      id, current, cost_pct: costPct, steps: s.steps, floor: s.floor, round_s: s.round_s,
      right: s.right, editable: true, follows: s.follows ?? '', source: !!s.source, balance: !!s.balance,
    };
  });
}

const PI3 = {
  base: 7.7, target: 70, deduction: 0, external: { every_s: 15, name: 'Wärmepumpe' },
  rows: rowsOf({
    live_update: [3, 14], tick: [10, 0.9], regulator: [10, 0.4], node: [60, 0.1], shelly: [20, 5],
    trucki: [30, 2], battery_soc: [10, 1], apsystems: [60, 0.5], tuya: [30, 0.5],
  }),
};
const PI1 = {
  base: 14.6, target: 60, deduction: 10, external: null,
  rows: rowsOf({
    live_update: [3, 31], tick: [10, 1.7], regulator: [10, 0.7], node: [60, 0.3], shelly: [5, 12],
    trucki: [30, 1.8], battery_soc: [10, 0.8], apsystems: [60, 0.3], tuya: [30, 0.4],
  }),
};
const X86 = {
  base: 2.7, target: 70, deduction: 0, external: { every_s: 2, name: 'Zähler' },
  rows: rowsOf({
    live_update: [3, 1.5], tick: [10, 0.2], regulator: [10, 0.1], node: [60, 0.01], shelly: [20, 0.5],
    trucki: [30, 0.2], battery_soc: [10, 0.1], apsystems: [60, 0.05], tuya: [30, 0.05],
  }),
};

// Objekte aus dem vm-Kontext haben einen anderen Prototyp, deshalb der JSON-Umweg.
const plain = (x) => JSON.parse(JSON.stringify(x));

const curOf = (rows) => Object.fromEntries(rows.map((r) => [r.id, r.current]));
const byId = (rows, id) => rows.find((r) => r.id === id);
const available = (dev) => dev.target - dev.deduction;

function ctxOf(dev, { sel, cur, avail, measuring = false } = {}) {
  const c = cur ?? curOf(dev.rows);
  return {
    rows: dev.rows, sel: sel ?? { ...c }, cur: c, base: dev.base,
    available: avail ?? available(dev), external: dev.external, measuring,
  };
}

test('performance-core: floorOf halbiert die schnellste Quelle', () => {
  const sel = curOf(PI3.rows);
  assert.equal(C.floorOf(byId(PI3.rows, 'live_update'), PI3.rows, sel, PI3.external), 5);
  assert.equal(C.floorOf(byId(PI3.rows, 'tick'), PI3.rows, sel, PI3.external), 5);
});

test('performance-core: floorOf folgt der Shelly-Bilanz, wenn Shelly auf 5 s steht', () => {
  const sel = { ...curOf(PI3.rows), shelly: 5 };
  assert.equal(C.floorOf(byId(PI3.rows, 'live_update'), PI3.rows, sel, PI3.external), 3);
  assert.equal(C.floorOf(byId(PI3.rows, 'regulator'), PI3.rows, sel, PI3.external), 10);
});

test('performance-core: floorOf des Reglers folgt der Bilanz bei Shelly 20 s', () => {
  const sel = curOf(PI3.rows);
  assert.equal(C.floorOf(byId(PI3.rows, 'regulator'), PI3.rows, sel, PI3.external), 20);
});

test('performance-core: floorOf nimmt die fremde Quelle mit 2 s beim x86', () => {
  const sel = curOf(X86.rows);
  assert.equal(C.floorOf(byId(X86.rows, 'live_update'), X86.rows, sel, X86.external), 1);
});

test('performance-core: cost und total entsprechen dem Go-Modell', () => {
  const row = byId(PI3.rows, 'shelly');
  // Bei der aktuellen Periode 20 s ist es der Kostenwert selbst.
  assert.equal(C.cost(row, 20), 5);
  // Halbe Periode: Pause plus Runde steigen zusammen, 5 * 20,4 / 10,4.
  assert.ok(Math.abs(C.cost(row, 10) - 5 * 20.4 / 10.4) < 1e-9);
  assert.ok(Math.abs(C.total(PI3.base, PI3.rows, curOf(PI3.rows)) - 32.1) < 1e-9);
});

test('performance-core: learningDeltaPct rechnet den Anteil eines Kerns in Prozent der Gesamtleistung', () => {
  // 0,02 Kern auf 4 Kernen sind 0,5 %, wer 0,6 Kern schätzt, kommt auf 15 % mehr minus 0,5.
  assert.ok(Math.abs(C.learningDeltaPct(0.02, 0.6, 4) - 14.5) < 1e-9);
  assert.ok(Math.abs(C.learningDeltaPct(0.02, 0.01, 4) + 0.25) < 1e-9, 'a lower level lowers the total');
  assert.equal(C.learningDeltaPct(0.02, 0.02, 4), 0, 'the same share is no change');
  assert.equal(C.learningDeltaPct(0.02, 0.6, 0), 0, 'without cores there is no delta');
});

// Pi 3 mit Dashboard auf 10 s: die Untergrenze liegt bei 5 s, also gibt es
// schnellere Stufen. Kosten bei 10 s sind 14 * 3 / 10 = 4,2 %.
const PI3_LIVE10 = {
  ...PI3,
  rows: PI3.rows.map((r) => (r.id === 'live_update' ? { ...r, current: 10, cost_pct: 4.2 } : r)),
};

test('performance-core: budgetNote Reserve bis zur schnelleren Stufe', () => {
  const ctx = ctxOf(PI3_LIVE10, { avail: 70 });
  assert.deepEqual(plain(C.budgetNote(byId(PI3_LIVE10.rows, 'live_update'), ctx)), {
    key: 'performance.note.reserve_until', params: { seconds: 5 }, tone: '',
  });
});

test('performance-core: budgetNote Auswahl senkt das Maximum', () => {
  // Der Takt der Regelprüfung auf 5 s kostet Budget. Damit passt das Dashboard
  // nur noch ab 10 s, obwohl allein 5 s gereicht hätte.
  const sel = { ...curOf(PI3_LIVE10.rows), tick: 5 };
  const ctx = ctxOf(PI3_LIVE10, { sel, avail: 26.6 });
  assert.deepEqual(plain(C.budgetNote(byId(PI3_LIVE10.rows, 'live_update'), ctx)), {
    key: 'performance.note.after_changes_until', params: { seconds: 10 }, tone: 'warn',
  });
});

test('performance-core: budgetNote über der Grenze, Zeile allein reicht', () => {
  // Pi 1 liegt bei 63,6 % gegen 50 % verfügbar. Das Dashboard allein auf 10 s reicht.
  const ctx = ctxOf(PI1, {});
  assert.deepEqual(plain(C.budgetNote(byId(PI1.rows, 'live_update'), ctx)), {
    key: 'performance.note.over_slower_enough', params: { seconds: 10 }, tone: 'warn',
  });
});

test('performance-core: budgetNote keine schnellere Stufe passt', () => {
  // Budget gleich dem heutigen Verbrauch von 22,3 %: schneller als 10 s passt nicht.
  const ctx = ctxOf(PI3_LIVE10, { avail: 22.3 });
  assert.deepEqual(plain(C.budgetNote(byId(PI3_LIVE10.rows, 'live_update'), ctx)), {
    key: 'performance.note.no_faster', params: {}, tone: '',
  });
});

test('performance-core: budgetNote null ohne Abhilfe oder ohne schnellere Stufe', () => {
  // Ueber der Grenze, aber kein Einzelwert bringt genug: Node allein reicht nicht.
  assert.equal(C.budgetNote(byId(PI1.rows, 'node'), ctxOf(PI1, {})), null);
  // Keine schnellere Stufe vorhanden: Node steht schon auf der untersten Stufe.
  const items = { ...curOf(PI3.rows), node: 5 };
  const rows = PI3.rows.map((r) => (r.id === 'node' ? { ...r, current: 5, cost_pct: 0.2 } : r));
  const dev = { ...PI3, rows };
  assert.equal(C.budgetNote(byId(rows, 'node'), ctxOf(dev, { cur: items, avail: 70 })), null);
});

test('performance-core: distribute hält die Grenze, nur editierbare Zeilen ändern sich', () => {
  const cur = curOf(PI1.rows);
  const editable = PI1.rows.map((r) => r.id).filter((id) => id !== 'tick' && id !== 'regulator');
  const sel = C.distribute(PI1.rows, cur, available(PI1), editable, ctxOf(PI1, { cur }));
  assert.ok(C.total(PI1.base, PI1.rows, sel) <= available(PI1) + 1e-9);
  assert.equal(sel.tick, cur.tick);
  assert.equal(sel.regulator, cur.regulator);
});

test('performance-core: distribute mit Reserve geht höchstens bis zur Untergrenze', () => {
  const cur = curOf(PI3.rows);
  const editable = PI3.rows.map((r) => r.id);
  const sel = C.distribute(PI3.rows, cur, available(PI3), editable, ctxOf(PI3, { cur }));
  assert.ok(C.total(PI3.base, PI3.rows, sel) <= available(PI3) + 1e-9);
  for (const row of PI3.rows) {
    const floor = C.floorOf(row, PI3.rows, sel, PI3.external);
    assert.ok(sel[row.id] >= floor, `${row.id}: ${sel[row.id]} unter Untergrenze ${floor}`);
  }
});

test('performance-core: distribute schlägt Battery SoC nie schneller vor als die schnellste Quelle', () => {
  const cur = curOf(PI3.rows);
  const editable = PI3.rows.map((r) => r.id);
  const sel = C.distribute(PI3.rows, cur, available(PI3), editable, ctxOf(PI3, { cur }));
  const inputs = ['shelly', 'trucki', 'apsystems', 'tuya'].map((id) => sel[id]);
  const bound = Math.min(...inputs);
  const steps = C.stepsOf(PI3.rows.find((r) => r.id === 'battery_soc'), PI3.rows, sel, PI3.external);
  if (steps.some((s) => s >= bound)) {
    assert.ok(sel.battery_soc >= bound, `battery_soc ${sel.battery_soc} schneller als ${bound}`);
  }
});

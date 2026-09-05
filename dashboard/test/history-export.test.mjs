// dashboard/test/history-export.test.mjs
// Export der Verlaufsdaten. Zwei Dinge sind hier wichtig genug fuer Tests:
// die Kopfzeilen mit Zeitraum, Stufe und Einheit - ohne sie ist eine
// exportierte Zahlenspalte nicht interpretierbar - und die Ausgabe als
// Stueckliste statt als eine grosse Zeichenkette.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'history-export.js'), 'utf8');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  vm.runInContext(source, dom.getInternalVMContext());
  return dom;
}

const meta = {
  from: 1755000000000,
  to: 1755086400000,
  tier: '1m',
  tierLabel: 'Minutenmittel',
  aggregate: 'avg',
  intervalSeconds: 10,
  timezone: 'Europe/Berlin',
  source: 'browser',
};

const rows = [
  {series: 'role:pv', ts: 1755000060000, min: 100, max: 300, avg: 200, n: 6, u: 'W'},
  {series: 'role:grid', ts: 1755000060000, min: -50, max: -10, avg: -30, n: 6, u: 'W'},
];

test('toCSV stellt Kopfzeilen mit Zeitraum, Stufe und Quelle voran', () => {
  const dom = load();
  const text = dom.window.HistoryExport.toCSV({rows, meta}).join('');
  assert.match(text, /^# Energy Node/m);
  assert.match(text, /# stufe: 1m \(Minutenmittel\)/);
  assert.match(text, /# zeitzone: Europe\/Berlin/);
  assert.match(text, /# quelle: browser/);
  assert.match(text, /# abtastung: 10 s/);
  dom.window.close();
});

test('toCSV schreibt eine Spaltenzeile und je Satz eine Datenzeile mit Semikolon', () => {
  const dom = load();
  const lines = dom.window.HistoryExport.toCSV({rows, meta}).join('').trim().split('\n');
  const header = lines.find(line => !line.startsWith('#'));
  assert.equal(header, 'zeitpunkt;serie;einheit;min;max;mittel;anzahl');
  const dataLines = lines.filter(line => !line.startsWith('#') && line !== header);
  assert.equal(dataLines.length, 2);
  assert.match(dataLines[0], /;role:pv;W;100;300;200;6$/);
  dom.window.close();
});

test('toCSV maskiert ein Semikolon in einer Serien-ID', () => {
  const dom = load();
  const text = dom.window.HistoryExport.toCSV({
    rows: [{series: 'seltsam;name', ts: 1755000060000, min: 1, max: 1, avg: 1, n: 1, u: 'W'}],
    meta,
  }).join('');
  assert.match(text, /"seltsam;name"/);
  dom.window.close();
});

test('toCSV liefert eine Stueckliste statt einer einzigen Zeichenkette', () => {
  const dom = load();
  const many = [];
  for (let index = 0; index < 5000; index += 1) {
    many.push({series: 'role:pv', ts: 1755000000000 + index * 1000, min: 1, max: 1, avg: 1, n: 1, u: 'W'});
  }
  const chunks = dom.window.HistoryExport.toCSV({rows: many, meta});
  assert.ok(chunks.length > 1, `erwartet mehrere Stuecke, waren ${chunks.length}`);
  assert.equal(chunks.join('').trim().split('\n').filter(line => !line.startsWith('#')).length, 5001);
  dom.window.close();
});

test('toJSON traegt Metadaten und Saetze und ist gueltiges JSON', () => {
  const dom = load();
  const parsed = JSON.parse(dom.window.HistoryExport.toJSON({rows, meta}).join(''));
  assert.equal(parsed.meta.tier, '1m');
  assert.equal(parsed.meta.timezone, 'Europe/Berlin');
  assert.equal(parsed.rows.length, 2);
  assert.equal(parsed.rows[0].series, 'role:pv');
  assert.equal(parsed.rows[0].unit, 'W');
  assert.ok(!Number.isNaN(Date.parse(parsed.rows[0].ts)), 'ts muss ein parsebarer Zeitpunkt sein');
  dom.window.close();
});

test('filename traegt Zeitraum und Stufe im Namen', () => {
  const dom = load();
  const name = dom.window.HistoryExport.filename(meta, 'csv');
  assert.match(name, /^energy-node-verlauf-.*-1m\.csv$/);
  dom.window.close();
});

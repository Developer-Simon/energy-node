// Regressionstest fuer den responsiven Feinschliff des Energie-Rollen-Panels
// in manager.css. Kein DOM-Test: jsdom rechnet weder CSS Grid noch Container
// Queries. Geprueft wird der Regeltext - er nagelt fest, dass die vier
// Hausverbrauch-Kacheln per Container-Query 2x2 (volle Breite) stehen, solange
// die Kachel-Erklaerung in eine Zeile passt, sonst 1x4, und die
// Schwellen-Gruppen kompakt in eine Zeile fliessen.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const css = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'css', 'manager.css'),
  'utf8',
);

test('die Hausverbrauch-Kacheln stehen 2x2, solange die Erklaerung in eine Zeile passt', () => {
  // Die Gruppe ist ein Inline-Size-Container ...
  assert.match(css, /\.energy-interpretation-group:has\(> \.energy-load-modes\)\s*\{[^}]*container[^}]*inline-size/s);
  // ... und schaltet ab genug Breite (2 x ~26rem) auf zwei gleich breite
  // Spalten ueber die volle Breite; Basiszustand bleibt 1fr (1x4).
  assert.match(css, /@container[^{(]*\(min-width:\s*5[0-9](?:\.\d+)?rem\)\s*\{[^}]*\.energy-load-modes\s*\{[^}]*grid-template-columns:\s*1fr 1fr/s);
});

test('Toleranz- und Statuskarten-Schwellen flieszen kompakt in eine Zeile', () => {
  assert.match(css, /\.energy-interpretation-group\.is-compact/);
});

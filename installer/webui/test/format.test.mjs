import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadScripts } from './helpers/load.mjs';

const { window } = loadScripts(['format.js']);
const F = window.Format;

test('plainVersion nimmt das fuehrende v weg, wie die Vorlage "1.4.2" zeigt', () => {
  assert.equal(F.plainVersion('v1.4.2'), '1.4.2');
  assert.equal(F.plainVersion('1.4.2'), '1.4.2');
  assert.equal(F.plainVersion(''), '');
  assert.equal(F.plainVersion(null), '');
});

test('duration schreibt m:ss wie "1:42" in der Schrittliste', () => {
  assert.equal(F.duration(102000), '1:42');
  assert.equal(F.duration(4000), '0:04');
  assert.equal(F.duration(0), '0:00');
});

test('elapsed schreibt mm:ss wie "04:12 vergangen"', () => {
  assert.equal(F.elapsed(252000), '04:12');
  assert.equal(F.elapsed(3723000), '62:03');
});

test('clock zeigt HH:MM:SS in der Zeitzone des Rechners', () => {
  const at = new Date(2026, 8, 13, 14, 21, 44).getTime();
  assert.equal(F.clock(at, 'de'), '14:21:44');
});

test('Groessen folgen der Sprache der Oberflaeche', () => {
  assert.equal(F.gigabytes(12698, 'de'), '12,4 GB');
  assert.equal(F.gigabytes(12698, 'en'), '12.4 GB');
  assert.equal(F.megabytes(41 * 1024 * 1024, 'de'), '41 MB');
});

test('bits leitet die Wortbreite aus uname -m ab', () => {
  assert.equal(F.bits('armv6l'), 32);
  assert.equal(F.bits('armv7l'), 32);
  assert.equal(F.bits('aarch64'), 64);
  assert.equal(F.bits('x86_64'), 64);
  assert.equal(F.bits('mips'), 0);
});

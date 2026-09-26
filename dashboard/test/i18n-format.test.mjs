// Number, date and sort formats of i18n.js. The number cases are shared
// with the Go package internal/numfmt so the server's first render and the
// browser's live updates match byte for byte.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { installI18n, catalog } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const cases = JSON.parse(fs.readFileSync(path.join(here, '..', 'internal', 'numfmt', 'testdata', 'cases.json'), 'utf8'));

const fresh = () => new JSDOM('<!doctype html><html><head></head><body></body></html>', { runScripts: 'outside-only' }).window;

test('shared number cases match the Go formatter', () => {
  const I18n = installI18n(fresh());
  for (const c of cases) {
    const style = I18n.number.resolve(c.format, c.grouping, c.catalog[0], c.catalog[1]);
    const got = c.kind === 'value'
      ? I18n.number.value(style, c.raw, c.unit)
      : I18n.number.fixed(style, c.number, c.decimals);
    assert.equal(got, c.want, c.name);
  }
});

test('formatNumber and formatValue read the meta tags and the catalog', () => {
  assert.equal(installI18n(fresh(), { lang: 'de' }).formatNumber(12345.67, 1), '12.345,7');
  assert.equal(installI18n(fresh(), { lang: 'en' }).formatNumber(12345.67, 1), '12,345.7');
  assert.equal(installI18n(fresh(), { lang: 'en', format: 'comma' }).formatValue('21.5', '°C'), '21,5');
  assert.equal(installI18n(fresh(), { lang: 'de', format: 'point', grouping: 'thin' }).formatNumber(12345, 0), '12 345');
});

test('formatNumber works detached from I18n', () => {
  const { formatNumber, formatValue } = installI18n(fresh(), { lang: 'de' });
  assert.equal(formatNumber(1.5, 1), '1,5');
  assert.equal(formatValue('1.5', 'V'), '1,5');
});

test('formatNumber returns an empty string for non-finite input', () => {
  const I18n = installI18n(fresh());
  assert.equal(I18n.formatNumber(NaN, 1), '');
  assert.equal(I18n.formatNumber(Infinity, 1), '');
  assert.equal(I18n.formatNumber('abc', 1), '');
});

test('dates follow meta.locale', () => {
  const at = Date.UTC(2026, 8, 25, 14, 30, 5);
  const utc = { timeZone: 'UTC' };
  const de = installI18n(fresh(), { lang: 'de' });
  const en = installI18n(fresh(), { lang: 'en' });
  assert.equal(de.locale, 'de-DE');
  assert.equal(en.locale, 'en-GB');
  assert.equal(de.formatDateTime(at, utc), '25.9.2026, 14:30:05');
  assert.equal(en.formatDateTime(at, utc), '25/09/2026, 14:30:05');
  assert.equal(de.formatDate(at, utc), '25.9.2026');
  assert.equal(en.formatTime(at, { ...utc, hour: '2-digit', minute: '2-digit' }), '14:30');
  assert.equal(de.formatDateTime('not a date'), '');
});

test('compare sorts with the language collation', () => {
  const de = installI18n(fresh(), { lang: 'de' });
  assert.deepEqual(['b', 'Ä', 'a'].sort(de.compare), ['a', 'Ä', 'b']);
  assert.equal(de.compare(undefined, ''), 0);
});

test('every catalog has number separators that match its own locale', () => {
  const dir = path.join(here, '..', 'internal', 'webui', 'catalogs');
  for (const file of fs.readdirSync(dir).filter(name => name.endsWith('.json'))) {
    const entries = catalog(path.basename(file, '.json'));
    assert.ok(entries['meta.locale'], `${file}: meta.locale missing`);
    const parts = new Intl.NumberFormat(entries['meta.locale']).formatToParts(12345.6);
    const part = type => parts.find(p => p.type === type).value;
    assert.equal(entries['meta.number.decimal'], part('decimal'), `${file}: meta.number.decimal`);
    assert.equal(entries['meta.number.group'], part('group'), `${file}: meta.number.group`);
  }
});

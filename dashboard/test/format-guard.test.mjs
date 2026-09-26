// Guard for A2 of the localization: locale-dependent formatting lives in
// i18n.js only. Every other script calls I18n.formatNumber/formatValue/
// formatDateTime/formatDate/formatTime/compare, so the number format
// setting and the UI language apply everywhere.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const dir = path.join(here, '..', 'internal', 'webui', 'static', 'js');
const forbidden = [
  /de-DE/,
  /\.toLocale(Date|Time)?String\(/,
  /\.localeCompare\(/,
  // Only calls with arguments: Intl.DateTimeFormat().resolvedOptions() reads
  // the time zone for the history export and formats nothing.
  /Intl\.(DateTimeFormat|NumberFormat|Collator)\((?!\))/,
  /\.replace\('\.', ','\)/,
];
// battery-card-core.js also runs inside Home Assistant, which has no I18n.
// Its fallback formatter may use toLocaleString.
const allowed = { 'i18n.js': forbidden, 'battery-card-core.js': [forbidden[1]] };

test('no script formats numbers, dates or sort order on its own', () => {
  const offenders = [];
  for (const name of fs.readdirSync(dir).filter(file => file.endsWith('.js'))) {
    const lines = fs.readFileSync(path.join(dir, name), 'utf8').split('\n');
    lines.forEach((line, index) => {
      if (line.trim().startsWith('//')) return;
      for (const pattern of forbidden) {
        if ((allowed[name] || []).includes(pattern)) continue;
        if (pattern.test(line)) offenders.push(`${name}:${index + 1}: ${line.trim()}`);
      }
    });
  }
  assert.deepEqual(offenders, []);
});

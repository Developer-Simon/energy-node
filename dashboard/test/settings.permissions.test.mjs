// Template check for the design/setup split on the settings page: every
// control of a design field needs canEditSettings, every control of a setup
// field needs canEditSetup, and the TinyTuya wizard needs setup_tiny_tuya.
// The Alpine bindings are read from the template source, so a new control
// without its gate fails here.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const templates = path.join(here, '..', 'internal', 'webui', 'templates');
const read = (name) => fs.readFileSync(path.join(templates, name), 'utf8');

// Opening tags of form controls, with attributes that may contain quotes.
const CONTROL = /<(input|select|button|textarea)\b(?:[^>"']|"[^"]*"|'[^']*')*>/g;

function controlsBetween(source, startMarker, endMarker) {
  const start = source.indexOf(startMarker);
  const end = source.indexOf(endMarker, start + 1);
  assert.ok(start >= 0 && end > start, `markers not found: ${startMarker}`);
  return [...source.slice(start, end).matchAll(CONTROL)].map((m) => m[0]);
}

const isHelpTrigger = (tag) => tag.includes('field-help-trigger');

test('every setup control on the general and history pages needs edit_setup', () => {
  const source = read('settings.html');
  const setupKeys = [
    'healthScoreThreshold', 'sweepIntervalSeconds', 'liveUpdateIntervalSeconds',
    'historySampleIntervalSeconds', 'historyRetentionMode', 'historyRetentionHours',
    'historyBudgetMb', 'historyRawWindowHours', 'historyMinuteWindowDays',
    'historyExchangeDisabled', 'energyStatsRecording',
  ];
  const lines = source.split('\n');
  const checked = lines.filter((line) => setupKeys.some((key) => line.includes(key)) && /<(input|select|button)\b/.test(line));
  assert.ok(checked.length >= 15, `expected the setup controls, found ${checked.length}`);
  for (const line of checked) {
    for (const tag of line.match(CONTROL) || []) {
      if (isHelpTrigger(tag)) continue;
      assert.match(tag, /canEditSetup/, `setup control without edit_setup gate: ${tag.slice(0, 160)}`);
    }
  }
});

test('every design control on the display page needs edit_settings', () => {
  const tags = controlsBetween(read('settings.html'), 'id="settings-display"', 'id="settings-history"');
  assert.ok(tags.length > 20, `expected the display controls, found ${tags.length}`);
  for (const tag of tags) {
    // The UI language is a browser preference, not a settings field.
    if (isHelpTrigger(tag) || tag.includes('name="ui-language"')) continue;
    assert.match(tag, /canEditSettings/, `design control without edit_settings gate: ${tag.slice(0, 160)}`);
  }
});

test('the save bar lets design or setup accounts save and hints only when neither is set', () => {
  const source = read('settings.html');
  assert.match(source, /id="settings-save"[^>]*!canSave/);
  assert.match(source, /settings-read-only-hint" x-show="!canSave"/);
  assert.doesNotMatch(source, /tune_live|canTuneLiveUpdates/);
});

test('every TinyTuya wizard control needs setup_tiny_tuya', () => {
  const tags = [...read('tiny-tuya.html').matchAll(CONTROL)].map((m) => m[0]);
  assert.ok(tags.length > 15, `expected the wizard controls, found ${tags.length}`);
  for (const tag of tags) {
    assert.match(tag, /canSetupTinyTuya/, `TinyTuya control without gate: ${tag.slice(0, 160)}`);
  }
});

test('the changelog history and the update check need edit_setup on the versions page', () => {
  const source = read('versions.html');
  for (const name of ['name="versions-history"', 'id="update-check"']) {
    const tags = [...source.matchAll(CONTROL)].map((m) => m[0]).filter((tag) => tag.includes(name));
    assert.ok(tags.length > 0, `no control with ${name}`);
    for (const tag of tags) assert.match(tag, /canEditSetup/, tag.slice(0, 160));
  }
});

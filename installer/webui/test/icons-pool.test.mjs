import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadScripts } from './helpers/load.mjs';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// Die Modul-Icons sind wörtliche Kopien aus dem Dashboard-Pool. Der Test
// prüft, dass jedes Icon aus Services.ICON_OF (plus 'device') als
// icon-mod-<name> in icons.html steht und dass sein Inhalt im Pool vorkommt.

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.join(here, '..', '..', '..');
const icons = fs.readFileSync(path.join(here, '..', 'templates', 'icons.html'), 'utf8');
const base = fs.readFileSync(path.join(repo, 'dashboard', 'internal', 'webui', 'templates', 'base.html'), 'utf8');
const deviceIcons = fs.readFileSync(path.join(repo, 'dashboard', 'internal', 'webui', 'deviceicons.go'), 'utf8');

function norm(text) {
  return text.replace(/\s+/g, ' ').trim();
}

function moduleInner(name) {
  const re = new RegExp(`\\{\\{define "icon-mod-${name}"\\}\\}<svg viewBox="0 0 24 24" aria-hidden="true" class="mod-svg">([\\s\\S]*?)</svg>\\{\\{end\\}\\}`);
  const match = icons.match(re);
  return match ? norm(match[1]) : null;
}

// Sprite-Symbole aus base.html (ico-*).
const SPRITE = {
  automation: 'ico-automation-link',
  tuya: 'ico-tuya',
  tailscale: 'ico-tailscale',
  lock: 'ico-lock',
  refresh: 'ico-refresh',
  drive: 'ico-drive',
  network: 'ico-network',
  history: 'ico-history',
  play: 'ico-play',
};

// Katalog-Icons mit wörtlichem Markup in deviceicons.go.
const LITERAL = {
  'solar-panel': 'energy-node:solar-panel',
  'plug-smart': 'energy-node:plug-smart',
  device: 'energy-node:chip',
};

// Katalog-Icons, deren Markup in Go gebaut wird (batteryBody, bolt()).
// Gerendert wie der Katalog sie ausgibt; die Quelle prüft der Test unten.
const GENERATED = {
  battery: {
    entry: 'energy-node:battery',
    markup: '<rect x="2.5" y="7" width="16.5" height="10" rx="2.5"/><path d="M19 10h1.5a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H19"/><path d="M6 10v4"/>',
    source: 'batteryBody+`<path d="M6 10v4"/>`',
  },
  'lightning-bolt-circle': {
    entry: 'energy-node:bolt-circle',
    markup: '<circle cx="12" cy="12" r="8.4"/><path d="M13 7 9.25 12.63 11.63 12.63 11 17 14.75 11.38 12.38 11.38Z"/>',
    source: 'bolt(12, 12, 10)',
  },
};

function load() {
  const { window } = loadScripts(['i18n.js', 'services.js']);
  return window.Services;
}

test('every module icon exists and is copied from the dashboard pool', () => {
  const S = load();
  const names = [...new Set([...Object.values(S.ICON_OF), 'device'])];
  assert.equal(names.length, 14, 'ICON_OF plus device ergibt 14 Icons');

  for (const name of names) {
    const inner = moduleInner(name);
    assert.ok(inner, `icon-mod-${name} fehlt in icons.html`);

    if (SPRITE[name]) {
      const id = SPRITE[name];
      assert.ok(norm(base).includes(inner), `${name}: Inhalt von ${id} steht nicht wörtlich in base.html`);
    } else if (LITERAL[name]) {
      assert.ok(norm(deviceIcons).includes(inner), `${name}: Inhalt steht nicht wörtlich in deviceicons.go`);
    } else if (GENERATED[name]) {
      const gen = GENERATED[name];
      assert.equal(inner, norm(gen.markup), `${name}: Inhalt weicht vom gerenderten Katalog-Markup ab`);
      assert.ok(deviceIcons.includes(gen.source), `${name}: ${gen.source} fehlt in deviceicons.go (${gen.entry})`);
    } else {
      assert.fail(`${name}: keine Quelle im Test eingetragen`);
    }
  }
});

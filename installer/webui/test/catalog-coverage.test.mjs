// Katalog-Treue (Plan C-II, Vertrag 7, Waechter 3): jeder benutzte Schluessel
// steht in jedem Katalog, und kein Template traegt sichtbaren Text.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { loadScripts } from './helpers/load.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const webui = path.join(here, '..');
const repo = path.join(webui, '..', '..');
const read = (...parts) => fs.readFileSync(path.join(...parts), 'utf8');
const list = (dir, pattern) => fs.readdirSync(dir).filter((name) => pattern.test(name)).sort();

const catalogs = Object.fromEntries(
  list(path.join(webui, 'catalogs'), /\.json$/).map((name) => [name.slice(0, -5), JSON.parse(read(webui, 'catalogs', name))]),
);

// Die Praefixe aus Vertrag 6.
const PREFIXES = ['app', 'entry', 'language', 'stepper', 'connect', 'prepare', 'precheck', 'configure', 'run', 'result', 'preview',
  'diagnose', 'field', 'action', 'step', 'service', 'component', 'unit', 'number', 'warning', 'fault', 'error'];
const KEY = new RegExp(`^(?:${PREFIXES.join('|')})(?:\\.[A-Za-z0-9_-]+)+$`);

function present(catalog, key) {
  return key in catalog || (`${key}.one` in catalog && `${key}.other` in catalog);
}

function missing(keys) {
  const out = [];
  for (const [lang, catalog] of Object.entries(catalogs)) {
    for (const key of [...new Set(keys)].sort()) {
      if (!present(catalog, key)) {
        out.push(`${lang}.json: ${key}`);
      }
    }
  }
  return out;
}

function literalKeys() {
  const sources = [
    ...list(path.join(webui, 'static', 'js'), /\.js$/).map((name) => read(webui, 'static', 'js', name)),
    ...list(path.join(webui, 'templates'), /\.html$/).map((name) => read(webui, 'templates', name)),
  ];
  const keys = [];
  for (const source of sources) {
    for (const match of source.matchAll(/'([^'\s]+)'/g)) {
      if (KEY.test(match[1])) {
        keys.push(match[1]);
      }
    }
  }
  return keys;
}

function serviceManifests() {
  return fs.readdirSync(path.join(repo, 'services'), { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => path.join(repo, 'services', entry.name, 'manifest.json'))
    .filter((file) => fs.existsSync(file))
    .map((file) => JSON.parse(fs.readFileSync(file, 'utf8')));
}

function errorCodes() {
  const sources = [
    ...list(path.join(webui, 'hostapi'), /\.go$/).filter((name) => !name.endsWith('_test.go')).map((name) => read(webui, 'hostapi', name)),
    read(repo, 'installer', 'internal', 'host', 'host.go'),
    read(repo, 'installer', 'internal', 'host', 'package.go'),
    ...list(path.join(repo, 'installer', 'internal', 'bundlesource'), /\.go$/).filter((name) => !name.endsWith('_test.go')).map((name) => read(repo, 'installer', 'internal', 'bundlesource', name)),
  ];
  const patterns = [/writeError\(\s*w,\s*http\.\w+,\s*"([A-Z_]+)"/g, /Code:\s*"([A-Z_]+)"/g, /Code\w+\s*=\s*"([A-Z_]+)"/g, /payload\["code"\]\s*=\s*"([A-Z_]+)"/g];
  const codes = new Set();
  for (const source of sources) {
    for (const pattern of patterns) {
      for (const match of source.matchAll(pattern)) {
        codes.add(match[1]);
      }
    }
  }
  return [...codes];
}

function composedKeys() {
  const keys = [];
  for (const entry of ['install', 'redeploy', 'diagnose']) {
    keys.push(`entry.${entry}`, `app.title.${entry}`);
  }
  for (const lang of Object.keys(catalogs)) {
    keys.push(`language.${lang}.short`);
  }
  for (const screen of ['connect', 'precheck', 'configure', 'preview', 'run', 'result']) {
    keys.push(`stepper.${screen}`);
  }
  for (let n = 1; n <= 12; n++) {
    keys.push(`number.${n}`);
  }

  const manifests = serviceManifests();
  const serviceSteps = new Set(manifests.map((manifest) => String(manifest.bootstrap_step)));
  for (const manifest of manifests) {
    keys.push(`service.${manifest.service_id}`);
    if (manifest.kind === 'service') {
      keys.push(`configure.hint.service.${manifest.service_id}`);
    }
  }
  for (const script of list(path.join(repo, 'scripts', 'bootstrap'), /^\d\d-.+\.sh$/)) {
    const id = script.slice(0, 2);
    if (!serviceSteps.has(id)) {
      keys.push(`step.${id}`);
    }
  }
  // Die optionalen Systemschritte (E7): Tailscale und Caddy.
  keys.push('configure.hint.step.40', 'configure.hint.step.70');

  const checklist = read(repo, 'installer', 'internal', 'diag', 'checklist.go');
  const fixedUnits = checklist.slice(checklist.indexOf('var fixedUnitSteps'), checklist.indexOf('var fixedPortSteps'));
  for (const match of fixedUnits.matchAll(/"([a-z0-9-]+\.service)"/g)) {
    keys.push(`unit.${match[1]}`);
  }
  const host = read(repo, 'installer', 'internal', 'host', 'host.go');
  for (const match of host.matchAll(/view\.Warnings = append\(view\.Warnings, "([A-Z_]+)"\)/g)) {
    keys.push(`warning.${match[1]}`);
  }
  for (const code of errorCodes()) {
    keys.push(`error.${code}`);
  }

  // Varianten, die ein Bildschirm aus Teilen zusammensetzt.
  keys.push('precheck.arch.fits_plain', 'precheck.arch.mismatch_plain',
    'precheck.disk.detail', 'precheck.disk.detail_plain', 'precheck.disk.low', 'precheck.disk.low_plain',
    'result.todo.count.2', 'result.todo.count.3', 'result.todo.count.4',
    'component.dashboard', 'component.services', 'component.wheel', 'component.dependency',
    'diagnose.group.services', 'diagnose.group.system', 'diagnose.group.config',
    'connect.package.repo_unavailable.OS_UNSUPPORTED', 'connect.package.repo_unavailable.TOOLS_MISSING');
  for (const state of ['active', 'failed', 'inactive', 'activating']) {
    keys.push(`diagnose.unit.${state}`);
  }
  return keys;
}

test('jeder woertlich benutzte Schluessel steht in jedem Katalog', () => {
  const keys = literalKeys();
  assert.ok(keys.length > 150, `only ${keys.length} literal keys found - the scan is broken`);
  assert.deepEqual(missing(keys), []);
});

test('jeder zusammengesetzte Schluessel steht in jedem Katalog', () => {
  assert.deepEqual(missing(composedKeys()), []);
});

test('jeder Skip-Grund der Bootstrap-Skripte hat einen Klartext', () => {
  const { window } = loadScripts(['format.js', 'run-model.js']);
  const reasons = new Set();
  const scan = (dir) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const file = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        scan(file);
      } else if (entry.name.endsWith('.sh')) {
        for (const match of fs.readFileSync(file, 'utf8').matchAll(/step_skip "([^"]+)"/g)) {
          reasons.add(match[1]);
        }
      }
    }
  };
  scan(path.join(repo, 'scripts', 'bootstrap'));
  assert.ok(reasons.size > 0, 'no step_skip found - the scan is broken');
  const unmapped = [...reasons].filter((reason) => !(reason in window.RunModel.SKIP_REASONS));
  assert.deepEqual(unmapped, [], 'add these to SKIP_REASONS in static/js/run-model.js');
  assert.deepEqual(missing([...reasons].map((reason) => `run.skip.${window.RunModel.SKIP_REASONS[reason]}`)), []);
});

test('kein Template traegt sichtbaren Text', () => {
  const offenders = [];
  for (const name of list(path.join(webui, 'templates'), /\.html$/)) {
    const html = read(webui, 'templates', name).replace(/\{\{[\s\S]*?\}\}/g, '');
    const { document } = new JSDOM(`<!doctype html><body>${html}</body>`).window;
    const walk = (node) => {
      for (const child of node.childNodes) {
        if (child.nodeType === 3 && /\p{L}/u.test(child.textContent)) {
          offenders.push(`${name}: text "${child.textContent.trim()}"`);
        }
        if (child.nodeType !== 1) {
          continue;
        }
        for (const attribute of ['placeholder', 'title', 'aria-label', 'alt']) {
          const value = child.getAttribute(attribute);
          if (value && /\p{L}/u.test(value)) {
            offenders.push(`${name}: ${attribute}="${value}"`);
          }
        }
        if (child.localName !== 'script' && child.localName !== 'style') {
          walk(child.localName === 'template' ? child.content : child);
        }
      }
    };
    walk(document.body);
  }
  assert.deepEqual(offenders, []);
});

test('der Waechter beisst: ein erfundener Schluessel und ein Textknoten fallen auf', () => {
  assert.deepEqual(missing(['precheck.does_not_exist']), Object.keys(catalogs).map((lang) => `${lang}.json: precheck.does_not_exist`));
  const { document } = new JSDOM('<!doctype html><body><template><span>Hallo</span></template></body>').window;
  assert.equal(/\p{L}/u.test(document.querySelector('template').content.textContent), true);
});

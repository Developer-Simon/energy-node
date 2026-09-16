// Halterung fuer die Bildschirmtests: laedt die Grundskripte und eine
// Bildschirmdatei in ein jsdom, beantwortet fetch aus einer Tabelle und stellt
// eine Shell-Attrappe unter window.Installer.shell bereit.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { loadScripts } from './load.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const BASE = ['i18n.js', 'format.js', 'api.js'];
const FIRST_SCREEN = { install: 'precheck', redeploy: 'preview', diagnose: 'diagnose' };

export const BOOTSTRAP = {
  host: 'installer', entry_points: ['install', 'redeploy', 'diagnose'], needs_connection: true,
  bundle_version: 'v1.4.2', bundle_arch: 'armv6', language: 'de', language_fixed: false, languages: ['de', 'en'],
};

// realCatalog: die ausgelieferten Texte. Tests, die gegen die Vorlage pruefen,
// muessen gegen den echten Katalog laufen, nicht gegen einen erfundenen.
export function realCatalog(lang) {
  return JSON.parse(fs.readFileSync(path.join(here, '..', '..', 'catalogs', `${lang}.json`), 'utf8'));
}

export function fakeShell(window, overrides = {}) {
  const shell = {
    ready: true,
    bootstrap: Object.assign({}, BOOTSTRAP, overrides.bootstrap),
    entry: 'install',
    screen: 'connect',
    dir: 'forward',
    lang: window.I18n.lang,
    connected: false,
    mutating: false,
    error: null,
    bar: { lead: '', sub: '', status: '', action: null },
    shared: Object.assign({
      target: { host: '', user: '' }, manifest: null, selection: null, selectionAtEntry: null,
      servicesOnly: false, run: null, lastRun: null,
    }, overrides.shared),
    t: (key, params) => window.I18n.t(key, params),
    tn: (key, n, params) => window.I18n.tn(key, n, params),
    number: (n) => window.I18n.number(n),
    navigate(screen, dir) { this.screen = screen; this.dir = dir; this.error = null; },
    go(screen) { this.navigate(screen, 'forward'); },
    back(screen) { this.navigate(screen, 'back'); },
    backToDashboard() { this.backToDashboardCalled = (this.backToDashboardCalled || 0) + 1; },
    switchEntry(name) { this.entry = name; this.navigate(this.connected ? FIRST_SCREEN[name] : 'connect', 'forward'); },
    openDiagnose() { this.entry = 'diagnose'; this.navigate('diagnose', 'forward'); },
    afterConnect(target) { this.connected = true; this.shared.target = target; this.go(FIRST_SCREEN[this.entry]); },
    fail(err) { this.error = { code: err.code, detail: err.detail || '' }; },
    startRun(response, request) {
      this.mutating = true;
      this.shared.run = { runId: response.run_id, mode: request.mode, only: request.only || '', resumed: false };
      this.go('run');
    },
    finishRun(outcome) { this.mutating = false; this.shared.run = null; this.shared.lastRun = outcome; this.go('result'); },
  };
  for (const [key, value] of Object.entries(overrides)) {
    if (key !== 'bootstrap' && key !== 'shared') {
      shell[key] = value;
    }
  }
  return shell;
}

export function mountScreen(file, factoryName, options = {}) {
  const { window } = loadScripts([...BASE, ...(options.scripts || []), file]);
  window.Api.configure({ basePath: '', token: 'tok' });
  window.I18n.lang = options.lang || 'de';
  window.I18n.catalog = options.catalog || {};

  const state = {
    responses: Object.assign({}, options.responses),
    errors: Object.assign({}, options.errors),
  };
  const calls = [];
  window.fetch = async (url, init = {}) => {
    const key = `${init.method || 'GET'} ${String(url).split('?')[0]}`;
    const body = init.body ? JSON.parse(init.body) : null;
    calls.push({ key, body });
    const err = state.errors[key];
    if (err) {
      return { ok: false, status: err.status || 500, json: async () => ({ error: err.code, detail: err.detail || '' }) };
    }
    if (!(key in state.responses)) {
      throw new Error(`no canned response for ${key}`);
    }
    const payload = state.responses[key];
    return { ok: true, status: 200, json: async () => (typeof payload === 'function' ? payload(body) : payload) };
  };
  const opened = [];
  window.open = (url) => { opened.push(url); return null; };

  const shell = fakeShell(window, options.shell);
  window.Installer = { shell };
  const factory = window.Screens && window.Screens[factoryName];
  if (!factory) {
    throw new Error(`${file} did not register ${factoryName} on window.Screens`);
  }
  return { screen: factory(), shell, calls, state, window, opened };
}

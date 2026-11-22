// Der Punkt am Reiter Automationen in der Huelle: sichtbar, solange Regel-
// Befunde offen sind. Die Logik steckt in dashboardShell(); der Test laeuft
// ohne Layout, wie dashboard.energy-dot.test.mjs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'dashboard.js'),
  'utf8',
);

const TABS = `
  <button id="tab-automations" class="tab" role="tab" aria-controls="automations-panel">Automationen</button>
  <button id="tab-settings" class="tab" role="tab" aria-controls="settings-panel">Einstellungen</button>
`;

// respond(url) returns {status, body} or throws to simulate a network failure.
function createShell({ navHtml = TABS, respond = () => ({ status: 200, body: { findings: [] } }) } = {}) {
  const dom = new JSDOM(
    `<!doctype html><html><body><div class="nav-shell"><nav id="dashboard-tabs">${navHtml}</nav></div>`
      + '<section id="automations-panel" class="panel"></section></body></html>',
    { runScripts: 'outside-only', url: 'http://localhost/' },
  );
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, directive: () => {} };
  const intervals = [];
  dom.window.setInterval = (fn, ms) => { intervals.push(ms); return intervals.length; };
  const requests = [];
  dom.window.fetch = async (url) => {
    requests.push(String(url));
    const { status, body } = respond(String(url));
    return { ok: status < 400, status, json: async () => body, text: async () => JSON.stringify(body) };
  };
  vm.runInContext(source, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  const component = factories.dashboardShell();
  component.$nextTick = (fn) => fn();
  component.loadPanel = () => {};
  component.syncLiveTopics = () => {};
  component.fitNav = () => {};
  return { component, window: dom.window, requests, intervals };
}

const settle = () => new Promise(resolve => setImmediate(resolve));
const FINDING = { id: 'f1', rule: 'loop', severity: 'warning', rule_ids: ['r1'] };
// Ein Befund ohne Regel (etwa die Verschattung) gehoert zur Monitoring-Seite, nicht zum Punkt.
const SHADE_FINDING = { id: 'shade:225:15', kind: 'shade', severity: 'info', rule_ids: [] };

test('ein offener Befund zeigt den Punkt am Automationen-Reiter', async () => {
  const { component, requests } = createShell({ respond: () => ({ status: 200, body: { findings: [FINDING] } }) });
  await component.refreshAutomationFindings();
  assert.deepEqual(requests, ['/api/v1/automations/findings']);
  assert.equal(component.automationFindingsOpen, true);
});

test('ein Befund ohne rule_ids laesst den Punkt am Automationen-Reiter aus', async () => {
  const { component } = createShell({ respond: () => ({ status: 200, body: { findings: [SHADE_FINDING] } }) });
  await component.refreshAutomationFindings();
  assert.equal(component.automationFindingsOpen, false);
});

test('keine offenen Befunde lassen den Punkt aus', async () => {
  const { component } = createShell({ respond: () => ({ status: 200, body: { findings: [] } }) });
  await component.refreshAutomationFindings();
  assert.equal(component.automationFindingsOpen, false);
});

test('ein Netzfehler laesst den bisherigen Wert stehen', async () => {
  let fail = false;
  const { component } = createShell({ respond: () => {
    if (fail) throw new Error('network down');
    return { status: 200, body: { findings: [FINDING] } };
  } });
  await component.refreshAutomationFindings();
  assert.equal(component.automationFindingsOpen, true);
  fail = true;
  await assert.doesNotReject(component.refreshAutomationFindings());
  assert.equal(component.automationFindingsOpen, true);
});

test('ohne Automationen-Reiter gibt es keine Anfrage und kein Intervall', async () => {
  const { component, window, requests, intervals } = createShell({ navHtml: '<button id="tab-settings" class="tab" role="tab" aria-controls="settings-panel">Einstellungen</button>' });
  await component.refreshAutomationFindings();
  component.init();
  window.dispatchEvent(new window.CustomEvent('automation-findings-changed'));
  await settle();
  // Der Einstellungen-Reiter fragt den Lernstatus ab; hier zaehlt nur dieser Endpunkt.
  assert.deepEqual(requests.filter(url => url.includes('/automations/')), []);
  assert.deepEqual(intervals.filter(ms => ms === 5 * 60 * 1000), []);
  assert.equal(component.automationFindingsOpen, false);
});

test('mit Automationen-Reiter laeuft die Abfrage alle fuenf Minuten', () => {
  const { component, intervals } = createShell();
  component.init();
  assert.ok(intervals.includes(5 * 60 * 1000));
});

test('das Ereignis automation-findings-changed aktualisiert den Punkt', async () => {
  let findings = [FINDING];
  const { component, window } = createShell({ respond: () => ({ status: 200, body: { findings } }) });
  component.init();
  await settle();
  assert.equal(component.automationFindingsOpen, true);
  findings = [];
  window.dispatchEvent(new window.CustomEvent('automation-findings-changed'));
  await settle();
  assert.equal(component.automationFindingsOpen, false);
});

// Offene Vorschlaege zaehlen mit, solange "Muster lernen" an ist.
const SUGGESTION = { id: 's1', rule: 'habit', severity: 'info' };
const SUGGESTIONS_PATH = '/api/v1/automations/suggestions';

// routed(findings, suggestions) answers the findings and suggestions endpoints separately.
function routed(findings, suggestions) {
  return (url) => (url.endsWith(SUGGESTIONS_PATH)
    ? { status: 200, body: suggestions }
    : { status: 200, body: { findings } });
}

const suggestionsBody = (overrides = {}) => ({
  enabled: true,
  learning: { since: 1794000000, devices: 14, days: 9 },
  suggestions: [SUGGESTION],
  ...overrides,
});

async function refreshBoth(component) {
  await component.refreshAutomationFindings();
  await component.refreshAutomationSuggestions();
}

test('nur Befunde zeigen den Punkt, die Vorschlaege fehlen', async () => {
  const { component } = createShell({ respond: routed([FINDING], suggestionsBody({ suggestions: [] })) });
  await refreshBoth(component);
  assert.equal(component.automationFindingsOpen, true);
});

test('nur offene Vorschlaege zeigen den Punkt, die Befunde fehlen', async () => {
  const { component, requests } = createShell({ respond: routed([], suggestionsBody()) });
  await refreshBoth(component);
  assert.deepEqual(requests, ['/api/v1/automations/findings', SUGGESTIONS_PATH]);
  assert.equal(component.automationFindingsOpen, true);
});

test('weder Befunde noch Vorschlaege lassen den Punkt aus', async () => {
  const { component } = createShell({ respond: routed([], suggestionsBody({ suggestions: [] })) });
  await refreshBoth(component);
  assert.equal(component.automationFindingsOpen, false);
});

test('abgeschaltetes Muster lernen zaehlt seine Vorschlaege nicht', async () => {
  const { component } = createShell({ respond: routed([], suggestionsBody({ enabled: false, learning: {}, suggestions: [SUGGESTION] })) });
  await refreshBoth(component);
  assert.equal(component.automationFindingsOpen, false);
});

test('abgeschaltetes Muster lernen laesst die Befunde weiter den Punkt zeigen', async () => {
  const { component } = createShell({ respond: routed([FINDING], suggestionsBody({ enabled: false, learning: {}, suggestions: [] })) });
  await refreshBoth(component);
  assert.equal(component.automationFindingsOpen, true);
});

test('der Punkt bleibt, solange Befunde offen sind, auch wenn die Vorschlaege leer werden', async () => {
  let suggestions = suggestionsBody();
  const { component, window } = createShell({ respond: (url) => (url.endsWith(SUGGESTIONS_PATH)
    ? { status: 200, body: suggestions }
    : { status: 200, body: { findings: [FINDING] } }) });
  component.init();
  await settle();
  assert.equal(component.automationFindingsOpen, true);
  suggestions = suggestionsBody({ suggestions: [] });
  window.dispatchEvent(new window.CustomEvent('automation-findings-changed'));
  await settle();
  assert.equal(component.automationFindingsOpen, true);
  assert.equal(component.automationSuggestionsHas, false);
});

test('ein Netzfehler bei den Vorschlaegen laesst den bisherigen Wert stehen', async () => {
  let fail = false;
  const { component } = createShell({ respond: (url) => {
    if (url.endsWith(SUGGESTIONS_PATH)) {
      if (fail) throw new Error('network down');
      return { status: 200, body: suggestionsBody() };
    }
    return { status: 200, body: { findings: [] } };
  } });
  await component.refreshAutomationSuggestions();
  assert.equal(component.automationFindingsOpen, true);
  fail = true;
  await assert.doesNotReject(component.refreshAutomationSuggestions());
  assert.equal(component.automationFindingsOpen, true);
});

test('das Ereignis automation-findings-changed aktualisiert auch die Vorschlaege', async () => {
  let suggestions = suggestionsBody();
  const { component, window } = createShell({ respond: (url) => (url.endsWith(SUGGESTIONS_PATH)
    ? { status: 200, body: suggestions }
    : { status: 200, body: { findings: [] } }) });
  component.init();
  await settle();
  assert.equal(component.automationFindingsOpen, true);
  suggestions = suggestionsBody({ suggestions: [] });
  window.dispatchEvent(new window.CustomEvent('automation-findings-changed'));
  await settle();
  assert.equal(component.automationFindingsOpen, false);
});

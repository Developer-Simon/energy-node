// Pure helpers for the automation condition cards. automations-view.js has no
// DOM of its own, so these tests load it into a jsdom window with the real
// German catalog, the way automations-scenario.test.mjs does.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  installI18n(dom.window);
  const context = dom.getInternalVMContext();
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'automations-view.js'), 'utf8'), context);
  return dom.window.__automationsView;
}

const plain = (value) => JSON.parse(JSON.stringify(value));

const chains = [{
  id: 'balkon', label: 'Balkon',
  points: [
    { entity_id: 'sensor.dc', label: 'DC-Eingang', device_id: 'wr' },
    { entity_id: 'sensor.ac', label: 'Wechselrichter', device_id: 'wr' },
  ],
}];

test('describeCondition chain_finding', () => {
  const view = load();
  const card = view.describeCondition(
    { type: 'chain_finding', chain_id: 'balkon', kind: 'frozen', points: ['sensor.dc'], min_age_s: 900, once: true },
    null, null, chains,
  );
  assert.equal(card.icon, 'ico-chain');
  assert.equal(card.title, 'Balkon');
  assert.match(card.summary, /Messwert eingefroren/);
  assert.match(card.summary, /15 min/);
  assert.match(card.summary, /einmal je Meldung/i);
  assert.equal(card.detail, 'DC-Eingang');
});

test('describeCondition chain_finding without age, once or points', () => {
  const view = load();
  const card = view.describeCondition(
    { type: 'chain_finding', chain_id: 'balkon', kind: 'any', points: [], min_age_s: 0, once: false },
    null, null, chains,
  );
  assert.equal(card.summary, 'Jede Meldung');
  assert.equal(card.detail, '');
});

test('describeCondition chain_finding with a missing chain keeps the summary and does not throw', () => {
  const view = load();
  const card = view.describeCondition(
    { type: 'chain_finding', chain_id: 'keller', kind: 'drift', points: [], min_age_s: 0, once: false },
    null, null, [],
  );
  assert.equal(card.icon, 'ico-chain');
  assert.equal(card.title, 'Messkette fehlt');
  assert.match(card.summary, /Verlust außerhalb des Normalbereichs/);
});

test('describeCondition chain_value', () => {
  const view = load();
  const standby = view.describeCondition(
    { type: 'chain_value', chain_id: 'balkon', field: 'standby_w', comparison: 'above', value: 10, hold_seconds: 7200 },
    null, null, chains,
  );
  assert.equal(standby.icon, 'ico-chain');
  assert.equal(standby.title, 'Balkon');
  assert.match(standby.summary, /Standby/);
  assert.match(standby.summary, /10 W/);
  assert.match(standby.summary, /2 h/);

  const efficiency = view.describeCondition(
    { type: 'chain_value', chain_id: 'balkon', field: 'efficiency_pct', comparison: 'below', value: 80, hold_seconds: 0 },
    null, null, chains,
  );
  assert.match(efficiency.summary, /Wirkungsgrad/);
  assert.match(efficiency.summary, /80 %/);
  assert.equal(efficiency.summary.includes(' h'), false);
});

test('describeCondition chain_value with a missing chain shows the placeholder title', () => {
  const view = load();
  const card = plain(view.describeCondition(
    { type: 'chain_value', chain_id: 'keller', field: 'loss_w', comparison: 'above', value: 5, hold_seconds: 0 },
    null, null, undefined,
  ));
  assert.equal(card.title, 'Messkette fehlt');
  assert.match(card.summary, /Verlust/);
});

// Die Vorlage ist die Spezifikation des Aussehens (Plan C-II, Vertrag 7).
// Jede Regel aus test/reference/*.css steht woertlich im Produkt - nur die
// Eintraege in DEVIATIONS duerfen abweichen, und jeder davon muss benutzt
// werden.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseCss, templateBlocks, outsideBlocks, scopeSelector, normalizeSelector, key } from './helpers/css.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = (...parts) => fs.readFileSync(path.join(here, ...parts), 'utf8');

// Vorlagenname -> data-screen. "_shell" steht unverschachtelt in installer.css.
export const SCREENS = {
  _shell: null,
  Main: 'connect',
  Vorpruefung: 'precheck',
  Konfiguration: 'configure',
  Ausfuehrung: 'run',
  Ergebnis: 'result',
  Aktualisieren: 'preview',
  Diagnose: 'diagnose',
};

// Jede Bildschirm-Task (6-12) nimmt hier ihre Vorlage auf.
const IMPLEMENTED = ['_shell', 'Main', 'Vorpruefung', 'Konfiguration', 'Ausfuehrung', 'Ergebnis', 'Aktualisieren'];

// "<screen oder shell>|<Selektor der Vorlage>" -> ersetzte oder ergaenzte
// Deklarationen. Jede Zeile hat einen Eintrag in der Tabelle aus Vertrag 7.
const DEVIATIONS = {
  'shell|.app': { width: '100%', height: '100vh' },          // A1
  'shell|.body': { overflow: 'auto' },                       // A2
  'run|.log': { overflow: 'auto' },                          // A4
  'run|.steps': { overflow: 'auto' },                        // A5
};

function productFile(name) {
  return name === '_shell' ? read('..', 'static', 'css', 'installer.css') : read('..', 'static', 'css', 'screens.css');
}

function expectedRules(name) {
  const screen = SCREENS[name];
  return parseCss(read('reference', `${name}.css`)).map((rule) => {
    const deviation = DEVIATIONS[`${screen || 'shell'}|${rule.selector}`];
    const decls = new Map(rule.decls);
    if (deviation) {
      for (const [prop, value] of Object.entries(deviation)) {
        decls.set(prop, value);
      }
    }
    const selector = screen ? normalizeSelector(scopeSelector(rule.selector, screen)) : rule.selector;
    return { context: rule.context, selector, decls, deviation: deviation ? `${screen || 'shell'}|${rule.selector}` : null };
  });
}

for (const name of IMPLEMENTED) {
  test(`${name}: jede Regel der Vorlage steht woertlich im Produkt`, () => {
    const block = templateBlocks(productFile(name))[name];
    assert.ok(block !== undefined, `no /* == Vorlage: ${name} == */ block in the product CSS`);
    const actual = new Map(parseCss(block).map((rule) => [key(rule), rule]));
    const expected = expectedRules(name);

    for (const rule of expected) {
      const found = actual.get(key(rule));
      assert.ok(found, `missing rule ${key(rule)}`);
      assert.deepEqual(Object.fromEntries(found.decls), Object.fromEntries(rule.decls), `declarations differ for ${key(rule)}`);
    }
    assert.equal(actual.size, expected.length, `the ${name} block carries rules the draft does not have`);
  });

  test(`${name}: Ergaenzungen ueberschreiben nichts aus der Vorlage`, () => {
    const templated = new Map(expectedRules(name).map((rule) => [key(rule), rule]));
    for (const rule of parseCss(outsideBlocks(productFile(name)))) {
      const original = templated.get(key(rule));
      if (!original) {
        continue;
      }
      for (const prop of rule.decls.keys()) {
        assert.ok(!original.decls.has(prop), `${key(rule)} overrides the draft's ${prop} outside the template block`);
      }
    }
  });
}

test('jede Abweichung wird benutzt', () => {
  const used = new Set();
  for (const name of IMPLEMENTED) {
    for (const rule of expectedRules(name)) {
      if (rule.deviation) used.add(rule.deviation);
    }
  }
  for (const entry of Object.keys(DEVIATIONS)) {
    const [screen] = entry.split('|');
    const owner = Object.keys(SCREENS).find((name) => (SCREENS[name] || 'shell') === screen);
    if (IMPLEMENTED.includes(owner)) {
      assert.ok(used.has(entry), `deviation ${entry} matches no rule of the draft - stale or misspelt`);
    }
  }
});

// Markup-Treue (Plan C-II, Vertrag 7, Waechter 2): jeder Bildschirm liegt bei
// 1080x720 dort, wo seine Vorlage liegt, und traegt ihre deutschen Texte.
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { startFakehost, openPage, fillLogin, connect, fillSecrets, startInstall } from './fakehost.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const reference = (file) => fs.readFileSync(path.join(here, '..', 'reference', file), 'utf8');

const BOXES = ['.bar', '.stepper', '.foot', '.foot button.primary'];
const TEXTS = ['.bar-title', '.st-lbl', '.card-h', '.card-p', '.foot-hint', '.foot button'];

// measure laeuft im Browser und darf nichts von aussen benutzen.
function measure({ boxes, texts }) {
  const rect = (el) => {
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width };
  };
  const shown = (el) => el.tagName !== 'TEMPLATE';
  const out = { boxes: {}, texts: {} };
  for (const selector of boxes) {
    const el = document.querySelector(selector);
    out.boxes[selector] = el ? [rect(el)] : [];
  }
  const body = document.querySelector('.body');
  const first = body ? [...body.children].find((el) => shown(el) && !el.classList.contains('stepper')) : null;
  out.boxes['.body > :not(.stepper)'] = first ? [rect(first)] : [];
  out.boxes['.cols > *'] = [...document.querySelectorAll('.cols > *')].filter(shown).map(rect);
  for (const selector of texts) {
    out.texts[selector] = [...document.querySelectorAll(selector)].map((el) => el.textContent.replace(/\s+/g, ' ').trim());
  }
  return out;
}

let browser;
before(async () => { browser = await chromium.launch({ args: ['--no-sandbox'] }); });
after(async () => { await browser.close(); });

async function draftOf(name) {
  const page = await browser.newPage({ viewport: { width: 1080, height: 720 }, reducedMotion: 'reduce' });
  await page.setContent(`<!doctype html><html><head><meta charset="utf-8"><style>${reference('_shell.css')}${reference(`${name}.css`)}</style></head><body>${reference(`${name}.body.html`)}</body></html>`);
  const result = await page.evaluate(measure, { boxes: BOXES, texts: TEXTS });
  await page.close();
  return result;
}

function differences(draft, product) {
  const out = [];
  const fmt = (b) => `x=${b.x.toFixed(1)} y=${b.y.toFixed(1)} w=${b.w.toFixed(1)}`;
  for (const [selector, expected] of Object.entries(draft.boxes)) {
    const actual = product.boxes[selector] || [];
    if (expected.length !== actual.length) {
      out.push(`${selector}: ${expected.length} in the draft, ${actual.length} in the product`);
      continue;
    }
    expected.forEach((box, i) => {
      const got = actual[i];
      if (Math.abs(box.x - got.x) > 4 || Math.abs(box.w - got.w) > 4 || Math.abs(box.y - got.y) > 12) {
        out.push(`${selector}[${i}]: draft ${fmt(box)}, product ${fmt(got)}`);
      }
    });
  }
  for (const [selector, expected] of Object.entries(draft.texts)) {
    if (JSON.stringify(expected) !== JSON.stringify(product.texts[selector])) {
      out.push(`${selector}: draft ${JSON.stringify(expected)}, product ${JSON.stringify(product.texts[selector])}`);
    }
  }
  return out;
}

async function assertMatchesDraft(page, name) {
  await page.waitForTimeout(300);
  const product = await page.evaluate(measure, { boxes: BOXES, texts: TEXTS });
  assert.deepEqual(differences(await draftOf(name), product), [], `${name} drifted from its draft`);
}

async function withHost(args, fn) {
  const host = await startFakehost(args);
  try {
    const { page, context } = await openPage(browser, host.url, { reducedMotion: 'reduce' });
    await fn(page);
    await context.close();
  } finally {
    await host.stop();
  }
}

test('Verbindung, Vorpruefung, Konfiguration und Ergebnis liegen wie ihre Vorlagen', () => withHost([], async (page) => {
  await fillLogin(page);
  await page.getByRole('button', { name: 'Verbinden', exact: true }).click();
  await page.locator('.tofu').waitFor();
  await assertMatchesDraft(page, 'Main');

  await page.getByRole('button', { name: 'Fingerabdruck bestätigen' }).click();
  await page.locator('.app[data-screen="precheck"] .chk').nth(6).waitFor();
  await assertMatchesDraft(page, 'Vorpruefung');

  await page.getByRole('button', { name: 'Weiter', exact: true }).click();
  await page.locator('.app[data-screen="configure"] .tog').nth(3).waitFor();
  await assertMatchesDraft(page, 'Konfiguration');

  await fillSecrets(page);
  await page.getByRole('button', { name: 'Installation starten' }).click();
  await page.locator('.app[data-screen="result"] .todo').nth(2).waitFor({ timeout: 20000 });
  await assertMatchesDraft(page, 'Ergebnis');
}));

test('die Ausfuehrung liegt wie ihre Vorlage, waehrend Tailscale auf die Anmeldung wartet', () => withHost(['--trusted', '--hold-step', '40'], async (page) => {
  await connect(page, { trusted: true });
  await startInstall(page);
  await page.locator('.call-u').waitFor({ timeout: 20000 });
  await assertMatchesDraft(page, 'Ausfuehrung');
}));

test('die Vorschau liegt wie ihre Vorlage', () => withHost(['--trusted', '--scenario', 'vorlage-update'], async (page) => {
  await page.locator('.mode-i', { hasText: 'Aktualisieren' }).click();
  await connect(page, { trusted: true });
  await page.locator('.app[data-screen="preview"] .vr').nth(3).waitFor();
  await assertMatchesDraft(page, 'Aktualisieren');
}));

test('die Diagnose liegt wie ihre Vorlage', () => withHost(['--trusted'], async (page) => {
  await page.locator('.mode-i', { hasText: 'Diagnose' }).click();
  await connect(page, { trusted: true });
  await page.locator('.app[data-screen="diagnose"] .fail').waitFor();
  await assertMatchesDraft(page, 'Diagnose');
}));

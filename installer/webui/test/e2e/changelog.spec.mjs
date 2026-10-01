// "Was ist neu" im Browser: die Zusammenfassung in der Vorschau, der Bildschirm
// mit Breaking-Block und Filtern, und der Weg zurueck. Gegen den Dashboard-Wirt
// im Szenario vorlage-update (installiert: Dashboard 1.4.1, Shelly 0.3.0).
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { startFakehost, openPage } from './fakehost.mjs';

let browser;
before(async () => { browser = await chromium.launch({ args: ['--no-sandbox'] }); });
after(async () => { await browser.close(); });

async function withHost(args, fn) {
  const host = await startFakehost(args);
  try {
    await fn(host);
  } finally {
    await host.stop();
  }
}

const UPDATE = ['--dashboard', '--scenario', 'vorlage-update'];

test('die Vorschau nennt, was seit der installierten Version neu ist, und fuehrt in den Bildschirm', () => withHost(UPDATE, async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await page.locator('.app[data-screen="preview"] .whatsnew').waitFor();
  assert.equal(await page.locator('.whatsnew-l').textContent(), '3 Neuerungen · 2 Korrekturen · 1 Breaking Change');
  assert.ok((await page.locator('.whatsnew-l').getAttribute('class')).includes('has-breaking'));

  await page.getByRole('button', { name: 'Änderungen ansehen' }).click();
  await page.locator('.app[data-screen="changelog"] .cl-head').waitFor();
  assert.equal(await page.locator('.cl-from').textContent(), '1.4.1');
  assert.equal(await page.locator('.cl-to').textContent(), '1.4.2');
  assert.equal(await page.locator('.cl-note').textContent(), '3 Neuerungen · 2 Korrekturen · 1 Breaking Change');
  await context.close();
}));

test('nur Neueres als das Installierte steht da; Breaking Changes stehen oben', () => withHost(UPDATE, async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await page.getByRole('button', { name: 'Änderungen ansehen' }).click();
  await page.locator('.app[data-screen="changelog"] .cl-comp').first().waitFor();

  assert.equal(await page.locator('.cl-breaking .cl-brk').count(), 1);
  assert.match(await page.locator('.cl-breaking').textContent(), /fold the config\.json node block/);
  assert.equal(await page.locator('.cl-comp').count(), 2);
  assert.equal(await page.locator('.app[data-screen="changelog"]').getByText('an older fix the node already has').count(), 0, 'v1.4.1 ist schon installiert');
  assert.equal(await page.locator('.cl-comp', { hasText: 'Dashboard' }).locator('.cl-rel-h').count(), 1, 'nur v1.4.2');
  assert.equal(await page.locator('.cl-grp-h', { hasText: 'Neuerungen' }).first().isVisible(), true, 'Gruppenueberschriften in der Sprache der Oberflaeche');
  await context.close();
}));

test('Filter nach Art, Bereich und Text schneiden die Liste, und ein leerer Treffer wird benannt', () => withHost(UPDATE, async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await page.getByRole('button', { name: 'Änderungen ansehen' }).click();
  await page.locator('.app[data-screen="changelog"] .cl-comp').first().waitFor();

  await page.locator('.cl-chip', { hasText: 'Dienste' }).click();
  assert.equal(await page.locator('.cl-comp').count(), 1);
  assert.match(await page.locator('.cl-comp').textContent(), /Shelly/);
  await page.locator('.cl-chip', { hasText: 'Dienste' }).click();
  assert.equal(await page.locator('.cl-comp').count(), 2, 'ein zweiter Klick nimmt den Filter zurueck');

  await page.locator('.cl-search').fill('badge');
  assert.equal(await page.locator('.cl-entry').count(), 1);
  await page.locator('.cl-search').fill('gibt es nicht');
  await page.getByText('Kein Eintrag passt zu diesem Filter.').waitFor();
  assert.equal(await page.locator('.cl-comp').count(), 0);
  await context.close();
}));

test('Zurueck fuehrt in die Vorschau, mit ihrer Zaehlzeile', () => withHost(UPDATE, async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await page.getByRole('button', { name: 'Änderungen ansehen' }).click();
  await page.locator('.app[data-screen="changelog"] .cl-head').waitFor();
  await page.getByRole('button', { name: 'Zurück', exact: true }).click();
  await page.locator('.app[data-screen="preview"] .whatsnew').waitFor();
  await context.close();
}));

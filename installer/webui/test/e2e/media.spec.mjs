// Medium vorbereiten im Browser, gegen cmd/fakehost: vom Geraet ueber die
// Einstellungen und das Laufwerk bis zur fertigen Karte, dann an die
// Einrichtung uebergeben. Dazu die Platzkontrolle bei 390 px Breite.
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

// layoutAt misst im Browser, ob die Seite waagerecht scrollt, und ob der
// Einstiegs-Umschalter sichtbar und gepolstert ist.
async function layoutAt(page) {
  return page.evaluate(() => {
    const html = document.documentElement;
    const mode = document.querySelector('.bar .mode');
    const style = mode ? getComputedStyle(mode) : null;
    return {
      scrollWidth: html.scrollWidth,
      bodyScrollWidth: document.body.scrollWidth,
      modeVisible: !!mode && mode.getClientRects().length > 0 && style.display !== 'none' && style.visibility !== 'hidden',
      modePadding: style ? [style.paddingTop, style.paddingRight, style.paddingBottom, style.paddingLeft].map((v) => parseFloat(v) || 0).reduce((a, b) => a + b, 0) : 0,
    };
  });
}

function assertNoOverflow(layout, screen) {
  assert.ok(layout.scrollWidth <= 390, `${screen}: documentElement.scrollWidth ${layout.scrollWidth} > 390`);
  assert.ok(layout.bodyScrollWidth <= 390, `${screen}: body.scrollWidth ${layout.bodyScrollWidth} > 390`);
}

function assertModeStyled(layout, screen) {
  assert.equal(layout.modeVisible, true, `${screen}: der Einstiegs-Umschalter ist nicht sichtbar`);
  assert.ok(layout.modePadding > 0, `${screen}: der Einstiegs-Umschalter hat kein Innenabstand`);
}

test('ein Medium laeuft vom Geraet bis zur fertigen Karte und geht an die Einrichtung', () => withHost([], async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await page.locator('.mode-i', { hasText: 'Medium' }).click();
  await page.locator('.app[data-screen="media-device"] .body [role="radiogroup"]').waitFor();

  await page.locator('.media-opt', { hasText: 'Pi 3, 3+, Zero 2 W' }).click();
  await page.getByRole('button', { name: 'Weiter', exact: true }).click();

  await page.locator('.app[data-screen="media-settings"]').waitFor();
  await page.getByRole('button', { name: 'Weiter', exact: true }).click();

  await page.locator('.app[data-screen="media-drive"] .media-opt', { hasText: 'SanDisk' }).waitFor();
  await page.locator('.app[data-screen="media-drive"] .media-opt', { hasText: 'SanDisk' }).click();
  await page.locator('.media-warn-check input').check();
  await page.getByRole('button', { name: 'Löschen und schreiben', exact: true }).click();

  await page.locator('.app[data-screen="media-write"]').waitFor();
  await page.locator('.app[data-screen="media-done"] .card-h', { hasText: 'Die SD-Karte ist fertig' }).waitFor({ timeout: 20000 });

  await page.getByRole('button', { name: 'Weiter zum Einrichten', exact: true }).click();
  await page.locator('.app[data-screen="connect"]').waitFor();
  assert.equal(await page.getByLabel('Adresse', { exact: true }).inputValue(), 'energy-node.local');
  assert.equal(await page.getByLabel('Benutzer', { exact: true }).inputValue(), 'node');
  await context.close();
}));

test('auf 390 px scrollt keiner der Medium-Bildschirme waagerecht und der Umschalter bleibt gestaltet', () => withHost([], async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await page.setViewportSize({ width: 390, height: 800 });
  await page.locator('.mode-i', { hasText: 'Medium' }).click();
  await page.locator('.app[data-screen="media-device"] .body [role="radiogroup"]').waitFor();
  assertModeStyled(await layoutAt(page), 'media-device');
  assertNoOverflow(await layoutAt(page), 'media-device');

  await page.locator('.media-opt', { hasText: 'Pi 3, 3+, Zero 2 W' }).click();
  await page.getByRole('button', { name: 'Weiter', exact: true }).click();
  await page.locator('.app[data-screen="media-settings"]').waitFor();
  assertNoOverflow(await layoutAt(page), 'media-settings');
  assertModeStyled(await layoutAt(page), 'media-settings');

  await page.getByRole('button', { name: 'Weiter', exact: true }).click();
  await page.locator('.app[data-screen="media-drive"] .media-opt', { hasText: 'SanDisk' }).waitFor();
  assertNoOverflow(await layoutAt(page), 'media-drive');
  assertModeStyled(await layoutAt(page), 'media-drive');

  await page.locator('.app[data-screen="media-drive"] .media-opt', { hasText: 'SanDisk' }).click();
  await page.locator('.media-warn-check input').check();
  await page.getByRole('button', { name: 'Löschen und schreiben', exact: true }).click();
  await page.locator('.app[data-screen="media-done"] .card-h', { hasText: 'Die SD-Karte ist fertig' }).waitFor({ timeout: 20000 });
  assertNoOverflow(await layoutAt(page), 'media-done');
  assertModeStyled(await layoutAt(page), 'media-done');
  await context.close();
}));

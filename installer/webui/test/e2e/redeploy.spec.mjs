// Aktualisieren im Browser: gegen einen Wirt ohne Verbindung (Kriterium 10)
// und im Installer-Wirt ueber die Verbindung.
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { startFakehost, openPage, connect } from './fakehost.mjs';

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

test('Kriterium 10: die Vorschau laeuft gegen einen Wirt ohne SSH bis zum Ergebnis', () => withHost(['--dashboard', '--scenario', 'vorlage-update'], async (host) => {
  const { page, context, requests } = await openPage(browser, host.url);
  await page.locator('.app[data-screen="preview"] .vr').nth(3).waitFor();
  assert.equal(await page.locator('.vd-from').textContent(), '1.4.1');
  assert.equal(await page.locator('.vd-to').textContent(), '1.4.2');
  assert.equal(await page.locator('.lang').count(), 0, 'feste Sprache im Dashboard-Wirt');
  assert.equal(await page.locator('.reuse').count(), 0);
  assert.equal(await page.locator('.svc.off', { hasText: 'modbus' }).locator('.svc-tag').count(), 1);

  await page.getByRole('button', { name: 'Aktualisieren', exact: true }).click();
  await page.locator('.app[data-screen="result"] .hero p').waitFor({ timeout: 20000 });
  assert.equal(await page.locator('.hero h1').textContent(), 'Der Node läuft');
  assert.match(await page.locator('.hero p').textContent(), /^Paket 1\.4\.1 → 1\.4\.2 · 3 Dienste neu gestartet · 0:0\d gebraucht$/);
  assert.ok(!requests.includes('/api/connect'), 'kein Aufruf von /api/connect');
  assert.ok(!requests.includes('/api/keypair'), 'kein Aufruf von /api/keypair');
  await context.close();
}));

test('Kriterium 9: ein neuer Dienst kommt nur mit ausdruecklicher Zustimmung, Abbrechen nimmt sie zurueck', () => withHost(['--dashboard', '--scenario', 'vorlage-update'], async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await page.locator('.app[data-screen="preview"] .svc').first().waitFor();

  await page.getByRole('button', { name: 'Dienste ändern' }).click();
  await page.locator('.app[data-screen="configure"] .tog').first().waitFor();
  assert.equal(await page.locator('.card-h', { hasText: 'Zugangsdaten' }).count(), 0, 'nur die Dienste-Karte');
  await page.getByRole('switch', { name: 'modbus' }).click();
  await page.getByRole('button', { name: 'Auswahl übernehmen' }).click();

  await page.locator('.vd-note', { hasText: 'Auswahl geändert' }).waitFor();
  assert.equal(await page.locator('.svc', { hasText: 'modbus' }).getAttribute('class'), 'svc');

  await page.getByRole('button', { name: 'Abbrechen', exact: true }).click();
  await page.locator('.vd-note', { hasText: 'Auswahl unverändert' }).waitFor();
  assert.equal(await page.locator('.svc', { hasText: 'modbus' }).getAttribute('class'), 'svc off');
  await context.close();
}));

test('im Installer-Wirt fuehrt der Einstieg Aktualisieren ueber die Verbindung in dieselbe Vorschau, mit Fortschritt beim ersten Laden', () => withHost(['--trusted', '--scenario', 'vorlage-update'], async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await page.locator('.mode-i', { hasText: 'Aktualisieren' }).click();
  assert.equal(await page.locator('.bar-title').textContent(), 'Energy Node aktualisieren');
  assert.deepEqual(await page.locator('.st-lbl').allTextContents(), ['Verbindung', 'Vorschau', 'Ausführung', 'Ergebnis']);

  // /api/plan kuenstlich verzoegern, bevor ueberhaupt verbunden wird - sonst
  // ist der allererste, durch afterConnect() ausgeloeste Aufruf schon durch,
  // bevor sich pruefen liesse, ob das Banner erscheint (Regression: siehe
  // navigate() in app.js).
  await page.route('**/api/plan', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 800));
    await route.continue();
  });
  await connect(page, { trusted: true });
  await page.locator('.app[data-screen="preview"]').waitFor();
  await page.locator('.alert--progress', { hasText: 'Änderungen werden ermittelt' }).waitFor({ timeout: 500 });
  await page.locator('.app[data-screen="preview"] .vr').first().waitFor();
  await page.locator('.alert--progress').waitFor({ state: 'detached' });
  assert.equal(await page.locator('.reuse').count(), 1);
  await context.close();
}));

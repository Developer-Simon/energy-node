// Erstinstallation, Sprachwechsel, Wiederaufnahme und Reparatur im Browser,
// gegen cmd/fakehost.
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { startFakehost, openPage, connect, startInstall, connectWithPackage, waitForPrepare, getDebugState, SECRETS } from './fakehost.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const en = JSON.parse(fs.readFileSync(path.join(here, '..', '..', 'catalogs', 'en.json'), 'utf8'));

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

test('eine Erstinstallation laeuft von der Verbindung bis zum Ergebnis, ohne ein Passwort zu zeigen (Kriterium 7)', () => withHost([], async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await connect(page);
  await page.locator('.app[data-screen="precheck"] .chk').nth(6).waitFor();
  assert.equal(await page.locator('.bar .chip').textContent(), 'pi@energy-node.local');

  await startInstall(page);
  assert.equal(await page.locator('.bar .mode').count(), 0, 'waehrend des Laufs kein Einstiegs-Umschalter');
  await page.locator('.app[data-screen="result"] .todo').nth(2).waitFor({ timeout: 20000 });
  assert.equal(await page.locator('.hero h1').textContent(), 'Der Node läuft');

  const html = await page.content();
  const storage = await page.evaluate(() => JSON.stringify(Object.assign({}, window.localStorage)));
  for (const secret of Object.values(SECRETS)) {
    assert.ok(!html.includes(secret), `the page contains ${secret}`);
    assert.ok(!storage.includes(secret), `localStorage contains ${secret}`);
  }
  await context.close();
}));

test('die Vorpruefung zeigt Fortschritt und Lade-Skelett schon beim ersten, automatischen Laden nach der Verbindung', () => withHost([], async (host) => {
  const { page, context } = await openPage(browser, host.url);
  // /api/precheck und /api/manifest kuenstlich verzoegern, bevor ueberhaupt
  // verbunden wird - sonst ist der allererste, durch afterConnect()
  // ausgeloeste Aufruf schon durch, bevor sich pruefen liesse, ob Banner und
  // Skelett erscheinen.
  await page.route('**/api/precheck', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 800));
    await route.continue();
  });
  await page.route('**/api/manifest', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 800));
    await route.continue();
  });
  await connect(page);
  await page.locator('.app[data-screen="precheck"]').waitFor();
  await page.locator('.alert--progress', { hasText: 'Zielgerät wird geprüft' }).waitFor({ timeout: 500 });
  assert.equal(await page.locator('.app[data-screen="precheck"] .ic-skeleton').count(), 6, 'sechs Skelett-Zeilen links');
  assert.equal(await page.locator('.app[data-screen="precheck"] .man .skel-bar').count(), 10, 'fuenf Skelett-Zeilen rechts, je zwei Balken');
  assert.equal(await page.locator('.app[data-screen="precheck"] .card-p').first().textContent(), 'Prüfungen laufen …');
  await page.locator('.alert--progress').waitFor({ state: 'detached' });
  assert.equal(await page.locator('.app[data-screen="precheck"] .ic-skeleton').count(), 0, 'das Skelett weicht den echten Zeilen');
  await context.close();
}));

test('der Sprachumschalter wechselt jeden Text samt Fehlerklartext und bleibt gemerkt (Kriterium 12)', () => withHost(['--trusted', '--fail-step', '50:PIP_EXTERNALLY_MANAGED'], async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await page.locator('.lang-i', { hasText: 'EN' }).click();
  await page.locator('.card-h', { hasText: en['connect.access.heading'] }).waitFor();
  assert.equal(await page.locator('.bar-title').textContent(), en['app.title.install']);
  assert.equal(await page.locator('.mode-i').first().textContent(), en['entry.install']);

  await page.getByLabel(en['field.address'], { exact: true }).fill('energy-node.local');
  await page.getByLabel(en['field.user'], { exact: true }).fill('pi');
  await page.getByLabel(en['field.password'], { exact: true }).fill(SECRETS.login);
  await page.getByRole('button', { name: en['action.connect'], exact: true }).click();
  await page.getByRole('button', { name: en['action.next'], exact: true }).click();
  await page.getByLabel(en['field.mqtt_password'], { exact: true }).fill(SECRETS.mqtt);
  await page.getByLabel(en['field.admin_password'], { exact: true }).fill(SECRETS.admin);
  await page.getByRole('button', { name: en['action.start_install'] }).click();

  await page.locator('.app[data-screen="result"] .hero h1').waitFor({ timeout: 20000 });
  assert.equal(await page.locator('.hero h1').textContent(), en['result.fail.heading']);
  assert.equal(await page.locator('.cols .card-p').first().textContent(), en['fault.PIP_EXTERNALLY_MANAGED.message']);

  await page.reload();
  await page.locator('.card-h', { hasText: en['connect.access.heading'] }).waitFor();
  await context.close();
}));

test('ein neu geladenes Fenster nimmt den laufenden Lauf wieder auf, ohne Zeilen doppelt zu zeigen', () => withHost(['--trusted', '--hold-step', '40'], async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await connect(page, { trusted: true });
  await startInstall(page);
  await page.locator('.call-u').waitFor({ timeout: 20000 });
  const before = await page.locator('.log > div').allTextContents();

  await page.reload();
  await page.locator('.app[data-screen="run"] .stp.now .stp-t').waitFor();
  await page.locator('.call-u').waitFor();
  assert.equal(await page.locator('.stp.now .stp-t').textContent(), 'Tailscale');
  assert.deepEqual(await page.locator('.log > div').allTextContents(), before);

  await page.getByRole('button', { name: 'Abbrechen', exact: true }).click();
  await page.locator('.app[data-screen="result"] .hero h1').waitFor();
  assert.equal(await page.locator('.hero h1').textContent(), 'Lauf abgebrochen');
  await context.close();
}));

test('die Diagnose repariert genau den ausgefallenen Dienst (Kriterium 6)', () => withHost(['--trusted'], async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await page.locator('.mode-i', { hasText: 'Diagnose' }).click();
  assert.equal(await page.locator('.bar-title').textContent(), 'Diagnose');
  // /api/diagnose kuenstlich verzoegern, bevor ueberhaupt verbunden wird -
  // sonst ist der allererste, durch afterConnect() ausgeloeste Aufruf schon
  // durch, bevor sich pruefen liesse, ob das Banner erscheint (Regression:
  // siehe navigate() in app.js).
  await page.route('**/api/diagnose', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 800));
    await route.continue();
  });
  await connect(page, { trusted: true });
  await page.locator('.app[data-screen="diagnose"]').waitFor();
  await page.locator('.alert--progress', { hasText: 'Diagnose läuft' }).waitFor({ timeout: 500 });
  await page.locator('.alert--progress').waitFor({ state: 'detached' });

  await page.locator('.app[data-screen="diagnose"] .fail').waitFor();
  assert.equal(await page.locator('.fail .code').textContent(), 'failed');
  await page.getByRole('button', { name: 'Schritt 6 · Shelly erneut ausführen' }).click();

  await page.locator('.app[data-screen="result"] .hero p').waitFor({ timeout: 20000 });
  assert.match(await page.locator('.hero p').textContent(), /^Shelly erneut ausgeführt · 0:0\d gebraucht$/);
  assert.equal(await page.locator('.stepper').count(), 0, 'eine Reparatur hat keinen Stepper');
  await page.getByRole('button', { name: 'Diagnose öffnen' }).click();
  await page.locator('.app[data-screen="diagnose"] .tally').waitFor();
  await context.close();
}));

test('die Paketquellen-Auswahl auf dem Verbindungsbildschirm zeigt alle vier Quellen und bereitet das Paket vor', () => withHost([], async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await page.locator('.app[data-screen="connect"]').waitFor();

  // Wait for the package radiogroup with options to be visible
  await page.locator('[role="radiogroup"] .opt').first().waitFor();

  // Count radio options for package sources - should show 4 sources when packageInfo is available
  const packageOptions = page.locator('[role="radiogroup"] .opt');
  const optCount = await packageOptions.count();
  assert.equal(optCount, 4, 'vier Paketquellen angeboten');

  // Use helper to connect with GitHub package source
  await connectWithPackage(page, 'github');

  // After prepare completes, should be on precheck screen
  await page.locator('.app[data-screen="precheck"] .chk').first().waitFor();

  await context.close();
}));

test('die Paketdatei-Quelle sperrt den Verbinden-Button bis eine Datei ausgewaehlt ist', () => withHost([], async (host) => {
  const { page, context } = await openPage(browser, host.url);
  await page.locator('.app[data-screen="connect"]').waitFor();

  // Wait for the package radiogroup with options to be visible
  await page.locator('[role="radiogroup"] .opt').first().waitFor();

  // Select "Paketdatei wählen"
  const packageOptions = page.locator('[role="radiogroup"] .opt');
  await packageOptions.filter({ hasText: 'Paketdatei wählen' }).click();

  // Fill connection details
  await page.getByLabel('Adresse', { exact: true }).fill('energy-node.local');
  await page.getByLabel('Benutzer', { exact: true }).fill('pi');
  await page.getByLabel('Passwort', { exact: true }).fill(SECRETS.login);

  // Verify connect button is disabled (no file selected yet)
  const connectButton = page.getByRole('button', { name: 'Verbinden', exact: true });
  assert.equal(await connectButton.isDisabled(), true, 'Verbinden-Button bleibt disabled ohne Datei');

  // Set a file (create a temporary file for testing)
  const fileInput = page.locator('input[type="file"]');
  await fileInput.setInputFiles({
    name: 'test-package.tar.gz',
    mimeType: 'application/gzip',
    buffer: Buffer.from('mock file content'),
  });

  // Now connect button should be enabled
  await page.waitForTimeout(100); // Brief wait for UI update
  assert.equal(await connectButton.isDisabled(), false, 'Verbinden-Button wird aktiviert mit Datei');

  // Connect and wait for prepare to complete
  await connectButton.click();
  await page.locator('.tofu').waitFor({ timeout: 5000 }).catch(() => null); // Handle TOFU if present
  // Wait for precheck screen (prepare may be skipped for file uploads)
  await page.locator('.app[data-screen="precheck"] .chk').first().waitFor({ timeout: 60000 });

  // Verify the backend recorded the uploaded file name
  const debugState = await getDebugState(host.url);
  assert.equal(debugState.uploaded_name, 'test-package.tar.gz', 'Backend recorded the uploaded file name');

  await context.close();
}));

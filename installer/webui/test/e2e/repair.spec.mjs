// Passwortdialog der Diagnose fuer die MQTT-Anmeldung, im Browser gegen
// cmd/fakehost im Szenario mqtt-denied.
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

// Sammelt die Reparatur-Anfragen (POST /api/run mit mode repair). Das Verbinden
// startet selbst einen Lauf mit mode prepare, der hier nicht zaehlt.
function trackRunPosts(page) {
  const posts = [];
  page.on('request', (request) => {
    if (request.method() !== 'POST' || new URL(request.url()).pathname !== '/api/run') {
      return;
    }
    const body = request.postDataJSON();
    if (body.mode === 'repair') {
      posts.push(body);
    }
  });
  return posts;
}

// Oeffnet die Diagnose, verbindet und wartet, bis der MQTT-Befund steht.
async function openDiagnoseWithMqttFailure(page) {
  await page.locator('.mode-i', { hasText: 'Diagnose' }).click();
  await connect(page, { trusted: true });
  await page.locator('.app[data-screen="diagnose"] .tally').waitFor();
  return page.locator('.app[data-screen="diagnose"] .fail', { hasText: 'MQTT-Anmeldung' });
}

test('die MQTT-Reparatur fragt nach dem Passwort, setzt es und die Anmeldung wird OK (Kriterium 6, Dialog)', () => withHost(['--trusted', '--scenario', 'mqtt-denied'], async (host) => {
  const { page, context } = await openPage(browser, host.url);
  const runPosts = trackRunPosts(page);
  const failure = await openDiagnoseWithMqttFailure(page);
  await failure.waitFor();

  await failure.locator('button.primary').click();
  const dialog = page.locator('.diag-pw-dialog');
  await dialog.waitFor();
  assert.equal(await dialog.getAttribute('role'), 'dialog');

  const confirm = dialog.getByRole('button', { name: 'Passwort setzen und reparieren', exact: true });
  assert.equal(await confirm.isDisabled(), true, 'Bestaetigen ist bei leerem Feld gesperrt');
  assert.equal(await dialog.locator('input').inputValue(), '');

  await dialog.getByRole('button', { name: 'Zufälliges Passwort', exact: true }).click();
  const value = await dialog.locator('input').inputValue();
  assert.equal(value.length, 24, 'das erzeugte Passwort hat 24 Zeichen');
  assert.equal(await confirm.isDisabled(), false, 'Bestaetigen ist mit gueltigem Passwort frei');

  await confirm.click();
  await page.locator('.app[data-screen="run"], .app[data-screen="result"]').first().waitFor();
  // Der Lauf zeigt seine Schritte, Schritt 60 nur, wenn das Fake ihn fuer req.Only liefert.
  await page.locator('.app[data-screen="result"] .hero h1').waitFor({ timeout: 20000 });
  assert.equal(await dialog.count(), 0, 'der Dialog ist nach dem Start zu');

  assert.equal(runPosts.length, 1, 'genau ein Lauf wurde gestartet');
  assert.deepEqual(runPosts[0], { mode: 'repair', only: '20', mqtt_password: value }, 'der Lauf schickt Schritt 20 mit dem Passwort');

  await page.getByRole('button', { name: 'Diagnose öffnen' }).click();
  await page.locator('.app[data-screen="diagnose"] .tally').waitFor();
  const mqttRow = page.locator('.app[data-screen="diagnose"]').getByText('MQTT-Anmeldung', { exact: true });
  await mqttRow.waitFor();
  // Der Wert in der Zeile der MQTT-Anmeldung, nicht der eines anderen Befunds.
  await page.locator('.app[data-screen="diagnose"] div:has(> .r-n:text-is("MQTT-Anmeldung")) > .r-v', { hasText: 'angemeldet' }).waitFor();
  assert.equal(await page.locator('.app[data-screen="diagnose"] .fail', { hasText: 'MQTT-Anmeldung' }).count(), 0, 'der Befund ist nicht mehr fehlerhaft');
  await context.close();
}));

test('Escape bricht den Passwortdialog ab, ohne einen Lauf zu starten', () => withHost(['--trusted', '--scenario', 'mqtt-denied'], async (host) => {
  const { page, context } = await openPage(browser, host.url);
  const runPosts = trackRunPosts(page);
  const failure = await openDiagnoseWithMqttFailure(page);
  await failure.locator('button.primary').click();
  const dialog = page.locator('.diag-pw-dialog');
  await dialog.waitFor();
  // Das Feld bekommt den Fokus erst nach $nextTick, Escape wirkt nur darin.
  await page.waitForFunction(() => document.activeElement && document.activeElement.matches('.diag-pw-dialog input'));
  await page.keyboard.press('Escape');
  await dialog.waitFor({ state: 'detached' });
  assert.equal(runPosts.length, 0, 'kein Lauf gestartet');
  assert.equal(await page.locator('.app[data-screen="diagnose"] .fail', { hasText: 'MQTT-Anmeldung' }).count(), 1, 'der Befund bleibt');
  await context.close();
}));

test('der MQTT-Passwortdialog passt bei 360 px ohne horizontalen Scroll', () => withHost(['--trusted', '--scenario', 'mqtt-denied'], async (host) => {
  const context = await browser.newContext({ viewport: { width: 360, height: 740 }, reducedMotion: 'no-preference' });
  const page = await context.newPage();
  await page.goto(host.url);
  await page.locator('.app:not([x-cloak]) .bar-title:not(:empty)').waitFor();
  const failure = await openDiagnoseWithMqttFailure(page);
  await failure.waitFor();
  await failure.locator('button.primary').click();
  const dialog = page.locator('.diag-pw-dialog');
  await dialog.waitFor();

  const scroll = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  assert.ok(scroll.scrollWidth <= scroll.clientWidth, `horizontaler Scroll: ${JSON.stringify(scroll)}`);

  const box = await dialog.boundingBox();
  const viewport = page.viewportSize();
  assert.ok(box.x >= 0 && box.x + box.width <= viewport.width, `Dialog ragt horizontal heraus: ${JSON.stringify(box)}`);
  assert.ok(box.y >= 0 && box.y + box.height <= viewport.height, `Dialog ragt vertikal heraus: ${JSON.stringify(box)}`);
  await context.close();
}));

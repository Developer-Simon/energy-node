// Baut cmd/fakehost einmal je Testdatei, startet ihn je Test auf einem freien
// Port und bringt die Handgriffe mit, die mehrere Durchlaeufe brauchen.
import { execFileSync, spawn } from 'node:child_process';
import fs from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const webui = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const binary = path.join(os.tmpdir(), `energy-node-fakehost-${process.pid}${process.platform === 'win32' ? '.exe' : ''}`);
let built = false;

export const SECRETS = { login: 'raspberry-7731', mqtt: 'mqtt-geheim-4711', admin: 'admin-geheim-0815' };

function build() {
  if (!built) {
    execFileSync('go', ['build', '-o', binary, './cmd/fakehost'], { cwd: webui, stdio: 'inherit' });
    process.on('exit', () => fs.rmSync(binary, { force: true }));
    built = true;
  }
}

function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.on('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const { port } = server.address();
      server.close(() => resolve(port));
    });
  });
}

export async function startFakehost(args = []) {
  build();
  const port = await freePort();
  const child = spawn(binary, ['--port', String(port), '--step-delay', '30ms', ...args], { stdio: ['ignore', 'pipe', 'inherit'] });
  const url = await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('fakehost did not print its URL within 20 s')), 20000);
    let output = '';
    child.stdout.on('data', (chunk) => {
      output += chunk;
      const match = /http:\/\/127\.0\.0\.1:\d+\//.exec(output);
      if (match) {
        clearTimeout(timer);
        resolve(match[0]);
      }
    });
    child.once('exit', (code) => {
      clearTimeout(timer);
      reject(new Error(`fakehost exited early with code ${code}`));
    });
  });
  return {
    url,
    stop: () => new Promise((resolve) => {
      if (child.exitCode !== null) {
        resolve();
        return;
      }
      child.once('exit', () => resolve());
      child.kill();
    }),
  };
}

export async function openPage(browser, url, { reducedMotion = 'no-preference' } = {}) {
  const context = await browser.newContext({ viewport: { width: 1080, height: 720 }, reducedMotion });
  const page = await context.newPage();
  const requests = [];
  page.on('request', (request) => requests.push(new URL(request.url()).pathname));
  await page.goto(url);
  await page.locator('.app:not([x-cloak]) .bar-title:not(:empty)').waitFor();
  return { page, context, requests };
}

export async function fillLogin(page) {
  await page.getByLabel('Adresse', { exact: true }).fill('energy-node.local');
  await page.getByLabel('Benutzer', { exact: true }).fill('pi');
  await page.getByLabel('Passwort', { exact: true }).fill(SECRETS.login);
}

// connect: ohne --trusted fragt der Testwirt beim ersten Mal nach dem
// Fingerabdruck, genau wie ein unbekannter Node.
export async function connect(page, { trusted = false } = {}) {
  await fillLogin(page);
  await page.getByRole('button', { name: 'Verbinden', exact: true }).click();
  if (!trusted) {
    await page.locator('.tofu').waitFor();
    await page.getByRole('button', { name: 'Fingerabdruck bestätigen' }).click();
  }
}

export async function fillSecrets(page) {
  await page.getByLabel('MQTT-Passwort', { exact: true }).fill(SECRETS.mqtt);
  await page.getByLabel('Dashboard-Admin-Passwort', { exact: true }).fill(SECRETS.admin);
}

export async function startInstall(page) {
  await page.locator('.app[data-screen="precheck"] .chk').first().waitFor();
  await page.getByRole('button', { name: 'Weiter', exact: true }).click();
  await page.locator('.app[data-screen="configure"] .tog').first().waitFor();
  await fillSecrets(page);
  await page.getByRole('button', { name: 'Installation starten' }).click();
  await page.locator('.app[data-screen="run"]').waitFor();
}

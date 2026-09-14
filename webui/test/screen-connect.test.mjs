import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountScreen, realCatalog } from './helpers/mount.mjs';

const CONNECTED = { connected: true, host: 'energy-node.local', user: 'pi' };
const KEYPAIR = { public_key: 'ssh-ed25519 AAAA installer', private_path: '/home/dev/.energy-node/id_ed25519', installed: true };

// plain() normalizes vm-context objects (from the screen) to test-realm literals
// for comparison, avoiding "not reference-equal" errors in assert.deepEqual.
const plain = (value) => JSON.parse(JSON.stringify(value));

function mount(options = {}) {
  const mounted = mountScreen('screen-connect.js', 'screenConnect', Object.assign({ catalog: realCatalog('de') }, options));
  mounted.screen.init();
  return mounted;
}

function fill(screen) {
  screen.host = 'energy-node.local';
  screen.user = 'pi';
  screen.secret = 'hunter2';
}

test('connect schickt Host, Benutzer und Passwort und fuehrt in die Vorpruefung', async () => {
  const { screen, shell, calls } = mount({ responses: { 'POST /api/connect': CONNECTED, 'POST /api/keypair': KEYPAIR } });
  fill(screen);
  await screen.connect();

  assert.deepEqual(calls[0], {
    key: 'POST /api/connect',
    body: { host: 'energy-node.local', user: 'pi', kind: 'password', secret: 'hunter2', key_path: '', accept_fingerprint: '' },
  });
  assert.equal(shell.connected, true);
  assert.equal(shell.screen, 'precheck');
  assert.deepEqual(plain(shell.shared.target), { host: 'energy-node.local', user: 'pi' });
  assert.equal(screen.secret, '', 'das Passwort bleibt nach der Anmeldung nicht in der Komponente');
});

test('nach einer Passwort-Anmeldung wird das Schluesselpaar hinterlegt', async () => {
  const { screen, calls } = mount({ responses: { 'POST /api/connect': CONNECTED, 'POST /api/keypair': KEYPAIR } });
  fill(screen);
  await screen.connect();
  assert.deepEqual(calls.map((call) => call.key), ['POST /api/connect', 'POST /api/keypair']);
});

test('ohne Haken oder mit Schluesseldatei gibt es kein Schluesselpaar', async () => {
  const unticked = mount({ responses: { 'POST /api/connect': CONNECTED } });
  fill(unticked.screen);
  unticked.screen.makeKey = false;
  await unticked.screen.connect();
  assert.deepEqual(unticked.calls.map((call) => call.key), ['POST /api/connect']);

  const keyfile = mount({ responses: { 'POST /api/connect': CONNECTED } });
  keyfile.screen.host = 'energy-node.local';
  keyfile.screen.user = 'pi';
  keyfile.screen.kind = 'key';
  keyfile.screen.keyPath = '/home/dev/.ssh/id_ed25519';
  await keyfile.screen.connect();
  assert.equal(keyfile.calls.length, 1);
  assert.equal(keyfile.calls[0].body.key_path, '/home/dev/.ssh/id_ed25519');
  assert.equal(keyfile.calls[0].body.secret, '', 'mit Schluesseldatei geht kein Passwort mit');
});

test('ein gescheitertes Schluesselpaar haelt nicht auf und steht danach im Banner', async () => {
  const { screen, shell } = mount({
    responses: { 'POST /api/connect': CONNECTED },
    errors: { 'POST /api/keypair': { code: 'AUTHORIZED_KEYS_FAILED', detail: 'read-only', status: 500 } },
  });
  fill(screen);
  await screen.connect();
  assert.equal(shell.screen, 'precheck');
  assert.equal(shell.error.code, 'AUTHORIZED_KEYS_FAILED');
});

test('ein unbekannter Host-Key zeigt den Fingerabdruck statt eines Fehlers', async () => {
  const { screen, shell } = mount({
    errors: { 'POST /api/connect': { code: 'HOSTKEY_UNKNOWN', detail: 'SHA256:xK9v', status: 409 } },
  });
  fill(screen);
  await screen.connect();

  assert.equal(screen.fingerprint, 'SHA256:xK9v');
  assert.equal(shell.error, null, 'ein unbekannter Key ist eine Frage, kein Fehler');
  assert.equal(shell.connected, false);
  assert.equal(screen.canConnect, false, 'Verbinden bleibt gesperrt, bis der Fingerabdruck bestaetigt ist');
  assert.equal(screen.hint, 'Bestätige zuerst den Fingerabdruck.');
});

test('das Bestaetigen schickt den Fingerabdruck mit', async () => {
  const { screen, state, calls, shell } = mount({
    errors: { 'POST /api/connect': { code: 'HOSTKEY_UNKNOWN', detail: 'SHA256:xK9v', status: 409 } },
    responses: { 'POST /api/keypair': KEYPAIR },
  });
  fill(screen);
  await screen.connect();

  delete state.errors['POST /api/connect'];
  state.responses['POST /api/connect'] = CONNECTED;
  await screen.confirmFingerprint();

  assert.equal(calls[1].body.accept_fingerprint, 'SHA256:xK9v');
  assert.equal(calls[1].body.secret, 'hunter2', 'die Bestaetigung ist dieselbe Anmeldung, nur mit Fingerabdruck');
  assert.equal(screen.fingerprint, '');
  assert.equal(shell.screen, 'precheck');
});

test('ein geaenderter Host-Key bietet keine Bestaetigung an', async () => {
  const { screen, shell } = mount({
    errors: { 'POST /api/connect': { code: 'HOSTKEY_CHANGED', detail: 'key mismatch', status: 409 } },
  });
  fill(screen);
  await screen.connect();
  assert.equal(screen.fingerprint, '');
  assert.equal(shell.error.code, 'HOSTKEY_CHANGED');
});

test('eine abgelehnte Anmeldung landet im Banner und gibt den Knopf wieder frei', async () => {
  const { screen, shell } = mount({ errors: { 'POST /api/connect': { code: 'AUTH_FAILED', status: 401 } } });
  fill(screen);
  await screen.connect();
  assert.equal(shell.error.code, 'AUTH_FAILED');
  assert.equal(screen.busy, false);
  assert.equal(screen.canConnect, true);
});

test('canConnect verlangt Adresse, Benutzer und das Geheimnis der gewaehlten Art', () => {
  const { screen } = mount();
  assert.equal(screen.canConnect, false);
  screen.host = 'energy-node.local';
  screen.user = 'pi';
  assert.equal(screen.canConnect, false);
  screen.secret = 'x';
  assert.equal(screen.canConnect, true);
  screen.kind = 'key';
  assert.equal(screen.canConnect, false);
  screen.keyPath = '/k';
  assert.equal(screen.canConnect, true);
});

test('der Hostname wird gemerkt, das Passwort nicht', async () => {
  const { screen, window } = mount({ responses: { 'POST /api/connect': CONNECTED, 'POST /api/keypair': KEYPAIR } });
  fill(screen);
  await screen.connect();
  assert.equal(window.localStorage.getItem('energy-node-installer.host'), 'energy-node.local');
  for (let i = 0; i < window.localStorage.length; i++) {
    const key = window.localStorage.key(i);
    assert.ok(!window.localStorage.getItem(key).includes('hunter2'), `${key} contains the password`);
  }

  const again = mount();
  again.window.localStorage.setItem('energy-node-installer.host', 'node.fritz.box');
  again.screen.init();
  assert.equal(again.screen.host, 'node.fritz.box');
});

test('das Paket steht mit Version und Architektur da, wie in der Vorlage', () => {
  const { screen } = mount();
  assert.equal(screen.packageLabel, 'Mitgeliefert · 1.4.2 · armv6');
});

test('reset verwirft Passwort und offenen Fingerabdruck', async () => {
  const { screen } = mount({ errors: { 'POST /api/connect': { code: 'HOSTKEY_UNKNOWN', detail: 'SHA256:x', status: 409 } } });
  fill(screen);
  await screen.connect();
  screen.reset();
  assert.equal(screen.fingerprint, '');
  assert.equal(screen.secret, '');
  assert.equal(screen.host, 'energy-node.local', 'die Adresse bleibt stehen');
});

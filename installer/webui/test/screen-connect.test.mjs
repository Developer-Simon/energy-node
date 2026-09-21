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

const PACKAGE_BOOTSTRAP = {
  package: {
    bundled: { version: 'v1.4.2', arch: 'armv6' },
    repo: { available: true, path: '/home/dev/energy-node' },
    resolved: null,
  },
};

test('mit bootstrap.package ist die default Paketart bundled wenn ein Bundle existiert', () => {
  const { screen } = mount({ shell: { bootstrap: PACKAGE_BOOTSTRAP } });
  assert.equal(screen.packageKind, 'bundled');
});

test('mit bootstrap.package ist die default Paketart github wenn kein Bundle existiert', () => {
  const { screen } = mount({ shell: { bootstrap: Object.assign({}, PACKAGE_BOOTSTRAP, { package: Object.assign({}, PACKAGE_BOOTSTRAP.package, { bundled: null }) }) } });
  assert.equal(screen.packageKind, 'github');
});

test('mit bootstrap.package.repo.path wird repoPath vorgefüllt', () => {
  const { screen } = mount({ shell: { bootstrap: PACKAGE_BOOTSTRAP } });
  assert.equal(screen.repoPath, '/home/dev/energy-node');
});

test('canConnect ist false für file ohne gewählte Datei und für repo mit leerem Pfad', () => {
  const { screen, window } = mount({ shell: { bootstrap: PACKAGE_BOOTSTRAP } });
  fill(screen);
  assert.equal(screen.canConnect, true, 'mit bundled ist canConnect true');
  screen.selectPackage('file');
  assert.equal(screen.canConnect, false, 'file ohne packageFile ist false');
  screen.packageFile = new window.File([''], 'bundle.tar.gz');
  assert.equal(screen.canConnect, true, 'file mit Datei ist true');
  screen.selectPackage('repo');
  assert.equal(screen.canConnect, true, 'repo mit vorgefülltem Pfad ist true');
  screen.repoPath = '';
  assert.equal(screen.canConnect, false, 'repo mit leerem Pfad ist false');
});

test('connect mit github schickt PUT /api/package und führt in prepare', async () => {
  const { screen, shell, calls } = mount({
    shell: { bootstrap: PACKAGE_BOOTSTRAP },
    responses: { 'POST /api/connect': CONNECTED, 'POST /api/keypair': KEYPAIR, 'PUT /api/package': {} },
  });
  fill(screen);
  screen.selectPackage('github');
  await screen.connect();
  const packageCall = calls.find((c) => c.key === 'PUT /api/package');
  assert.ok(packageCall);
  assert.deepEqual(packageCall.body, { kind: 'github', path: '' });
  assert.equal(shell.screen, 'prepare');
});

test('connect mit repo schickt PUT /api/package mit dem Pfad', async () => {
  const { screen, calls } = mount({
    shell: { bootstrap: PACKAGE_BOOTSTRAP },
    responses: { 'POST /api/connect': CONNECTED, 'POST /api/keypair': KEYPAIR, 'PUT /api/package': {} },
  });
  fill(screen);
  screen.selectPackage('repo');
  screen.repoPath = '/home/dev/energy-node';
  await screen.connect();
  const packageCall = calls.find((c) => c.key === 'PUT /api/package');
  assert.deepEqual(packageCall.body, { kind: 'repo', path: '/home/dev/energy-node' });
});

test('connect mit file schickt Api.upload statt PUT', async () => {
  const { screen, window, calls } = mount({
    shell: { bootstrap: PACKAGE_BOOTSTRAP },
    responses: { 'POST /api/connect': CONNECTED, 'POST /api/keypair': KEYPAIR },
  });
  fill(screen);
  screen.selectPackage('file');
  const file = new window.File(['content'], 'bundle.tar.gz');
  screen.packageFile = file;
  window.Api.upload = async (path, uploadFile) => {
    calls.push({ key: 'Api.upload', path, file: uploadFile });
  };
  await screen.connect();
  const uploadCall = calls.find((c) => c.key === 'Api.upload');
  assert.ok(uploadCall);
  assert.equal(uploadCall.path, '/api/package/upload');
  assert.equal(uploadCall.file, file);
});

test('PUT /api/package mit Fehler lässt die Verbindung stehen und zeigt den Fehler', async () => {
  const { screen, shell, calls } = mount({
    shell: { bootstrap: PACKAGE_BOOTSTRAP },
    responses: { 'POST /api/connect': CONNECTED },
    errors: { 'PUT /api/package': { code: 'REPO_NOT_A_CHECKOUT', status: 400 } },
  });
  fill(screen);
  screen.selectPackage('repo');
  screen.repoPath = '/tmp';
  await screen.connect();
  assert.equal(shell.screen, 'connect', 'Bildschirm bleibt auf connect');
  assert.equal(shell.error.code, 'REPO_NOT_A_CHECKOUT');
});

test('ohne bootstrap.package: nichts ändert sich, keine package-Calls, endet auf precheck', async () => {
  const { screen, shell, calls } = mount({ responses: { 'POST /api/connect': CONNECTED, 'POST /api/keypair': KEYPAIR } });
  fill(screen);
  await screen.connect();
  const packageCalls = calls.filter((c) => c.key.includes('package'));
  assert.equal(packageCalls.length, 0, 'keine package-Calls');
  assert.equal(shell.screen, 'precheck', 'endet auf precheck statt prepare');
});

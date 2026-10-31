import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountScreen } from './helpers/mount.mjs';

// JSON-Umweg: Werte aus dem vm-Kontext sollen mit deepEqual gegen normale
// Objekte vergleichbar sein.
const plain = (value) => JSON.parse(JSON.stringify(value));

const INPUT = {
  steps: { 40: true, 70: true, 81: true, 82: false },
  mqttUser: 'energynode',
  mqttPassword: 'mqtt-geheim',
  adminPassword: 'admin-geheim',
  targetUser: 'energynode',
  targetBase: '/home/energynode',
};

function mount(options = {}) {
  const mounted = mountScreen('screen-dashboard.js', 'screenDashboard', Object.assign({
    responses: {
      'PUT /api/selection': (body) => ({ source: 'node', steps: body.steps }),
      'POST /api/run': { run_id: 'run-1', seq: 3 },
    },
  }, options, {
    shell: Object.assign({ screen: 'dashboard', connected: true, installInput: INPUT }, options.shell),
  }));
  mounted.screen.init();
  return mounted;
}

test('init legt die Vorgaben am App-Store ab, alles unveraendert', () => {
  const { screen, shell } = mount();
  assert.deepEqual(plain(shell.shared.dashboardPrefs), {
    admin_user: '', users: [], guest_access: null, theme: null, title: '', subtitle: '',
    status_bar_items: null, show_runtime_status: null, wide_panels: null, update_check: null,
    metrics: null, later: [],
  });
  assert.equal(screen.groupState('theme'), 'default');
});

test('ein gesetztes Feld zaehlt als gesetzt, Später macht die Gruppe zu later', () => {
  const { screen, shell } = mount();
  screen.chooseTheme('stromblau');
  assert.equal(screen.groupState('theme'), 'set');
  screen.toggleLater('theme');
  assert.equal(screen.groupState('theme'), 'later');
  assert.deepEqual(plain(shell.shared.dashboardPrefs.later), ['theme']);
  assert.equal(shell.shared.dashboardPrefs.theme, null, 'eine spaetere Gruppe wird nicht konfiguriert');
});

test('Doch jetzt nimmt Später zurueck und stellt den vorigen Wert wieder her', () => {
  const { screen, shell } = mount();
  screen.chooseTheme('stromblau');
  screen.toggleLater('theme');
  screen.toggleLater('theme');
  assert.deepEqual(plain(shell.shared.dashboardPrefs.later), []);
  assert.equal(shell.shared.dashboardPrefs.theme, 'stromblau');
  assert.equal(screen.groupState('theme'), 'set');
});

test('Doch jetzt aus einer unberuehrten Gruppe fuehrt zurueck auf Standard', () => {
  const { screen, shell } = mount();
  screen.toggleLater('guest');
  assert.equal(screen.groupState('guest'), 'later');
  screen.toggleLater('guest');
  assert.equal(screen.groupState('guest'), 'default');
  assert.equal(shell.shared.dashboardPrefs.guest_access, null);
});

test('der Alle-Chip setzt alle Tabs, einzelne Chips schalten um', () => {
  const { screen, shell } = mount();
  screen.setAllChips('tabwidth', true);
  assert.equal(shell.shared.dashboardPrefs.wide_panels.length, 10);
  assert.equal(screen.allChips('tabwidth'), true);
  screen.toggleChip('tabwidth', 'devices');
  assert.deepEqual(plain(shell.shared.dashboardPrefs.wide_panels).includes('devices'), false);
  assert.equal(screen.allChips('tabwidth'), false);
  assert.equal(screen.groupState('tabwidth'), 'set');
});

test('Systemstatus beginnt mit den Vorgaben des Dashboards und behaelt die Reihenfolge', () => {
  const { screen, shell } = mount();
  assert.equal(screen.chipOn('statusbar', 'mqtt'), true);
  assert.equal(screen.chipOn('statusbar', 'cache'), false);
  screen.toggleChip('statusbar', 'cache');
  assert.deepEqual(plain(shell.shared.dashboardPrefs.status_bar_items),
    ['mqtt', 'cache', 'storage', 'uptime', 'version']);
});

test('Benutzerzeile verlangt Namen nach [a-z0-9._-]{3,32}', () => {
  const { screen } = mount();
  assert.equal(screen.nameValid('ab'), false);
  assert.equal(screen.nameValid('a'.repeat(33)), false);
  assert.equal(screen.nameValid('Anna'), false);
  assert.equal(screen.nameValid('anna k'), false);
  assert.equal(screen.nameValid('anna.k_1-2'), true);
  assert.equal(screen.nameValid('a'.repeat(32)), true);
});

test('Benutzerzeile verlangt ein Passwort ab zehn Zeichen', () => {
  const { screen, shell } = mount();
  screen.addUser();
  const user = shell.shared.dashboardPrefs.users[0];
  user.name = 'anna';
  user.password = '123456789';
  assert.equal(screen.userValid(user), false);
  assert.equal(screen.usersValid, false);
  user.password = '1234567890';
  assert.equal(screen.userValid(user), true);
  assert.equal(screen.usersValid, true);
});

test('Benutzer hinzufuegen und entfernen, Vorlage ist Bedienen', () => {
  const { screen, shell } = mount();
  screen.addUser();
  screen.addUser();
  assert.equal(shell.shared.dashboardPrefs.users[0].template, 'operate');
  assert.equal(screen.groupState('users'), 'set');
  screen.removeUser(0);
  assert.equal(shell.shared.dashboardPrefs.users.length, 1);
});

test('der Admin-Name ist optional, sonst gilt die Namensregel', () => {
  const { screen, shell } = mount();
  assert.equal(screen.adminValid, true);
  shell.shared.dashboardPrefs.admin_user = 'Chef';
  assert.equal(screen.adminValid, false);
  shell.shared.dashboardPrefs.admin_user = 'chef';
  assert.equal(screen.adminValid, true);
});

test('Farbschemawahl setzt document.documentElement.dataset.theme, Mint ohne Attribut', () => {
  const { screen, shell, window } = mount();
  screen.chooseTheme('stromblau');
  assert.equal(window.document.documentElement.dataset.theme, 'stromblau');
  assert.equal(shell.shared.dashboardPrefs.theme, 'stromblau');
  screen.chooseTheme('mint');
  assert.equal(window.document.documentElement.dataset.theme, '');
  assert.equal(shell.shared.dashboardPrefs.theme, 'mint');
});

test('Start speichert die Auswahl, startet den Lauf und vergisst die Passwoerter', async () => {
  const { screen, shell, calls } = mount();
  screen.addUser();
  Object.assign(shell.shared.dashboardPrefs.users[0], { name: 'anna', password: '1234567890' });
  assert.equal(screen.canStart, true);
  await screen.start();

  assert.deepEqual(calls.map((call) => call.key), ['PUT /api/selection', 'POST /api/run']);
  assert.deepEqual(calls[0].body, { steps: INPUT.steps });
  const { dashboard_prefs: prefs, ...run } = calls[1].body;
  assert.deepEqual(run, {
    mode: 'install', mqtt_user: 'energynode', mqtt_password: 'mqtt-geheim', admin_password: 'admin-geheim',
    target_user: 'energynode', target_base: '/home/energynode',
  });
  assert.equal(prefs.users[0].name, 'anna');
  assert.equal(shell.screen, 'run');
  assert.equal(shell.mutating, true);
  assert.equal(shell.installInput, null);
  assert.equal(shell.shared.dashboardPrefs.users[0].password, '');
});

test('Start schickt die Vorgaben mit den Benutzerzeilen und deren Passwoertern mit', async () => {
  const { screen, shell, calls } = mount();
  screen.addUser();
  Object.assign(shell.shared.dashboardPrefs.users[0], { name: 'anna', template: 'operate', password: '1234567890' });
  screen.chooseTheme('stromblau');
  await screen.start();
  const prefs = plain(calls[1].body.dashboard_prefs);
  assert.deepEqual(prefs.users, [{ name: 'anna', template: 'operate', password: '1234567890' }]);
  assert.equal(prefs.theme, 'stromblau');
  assert.deepEqual(prefs.later, []);
});

test('ohne Eingaben aus dem Konfigurationsschritt startet nichts', async () => {
  const { screen, calls } = mount({ shell: { installInput: null } });
  assert.equal(screen.canStart, false);
  await screen.start();
  assert.deepEqual(calls, []);
});

test('ein ungueltiger Benutzer blockiert den Start', () => {
  const { screen, shell } = mount();
  screen.addUser();
  assert.equal(screen.canStart, false);
  Object.assign(shell.shared.dashboardPrefs.users[0], { name: 'anna', password: '1234567890' });
  assert.equal(screen.canStart, true);
});

test('ein abgewiesener Lauf bleibt auf dem Dashboard und zeigt den Fehler', async () => {
  const { screen, shell } = mount({ errors: { 'POST /api/run': { code: 'RUN_IN_PROGRESS', status: 409 } } });
  await screen.start();
  assert.equal(shell.screen, 'dashboard');
  assert.equal(shell.error.code, 'RUN_IN_PROGRESS');
  assert.equal(screen.busy, false);
});

test('Zurueck fuehrt in die Konfiguration', () => {
  const { screen, shell } = mount();
  screen.back();
  assert.equal(shell.screen, 'configure');
  assert.equal(shell.dir, 'back');
});

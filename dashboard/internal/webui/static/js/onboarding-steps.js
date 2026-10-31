// Ortsangaben der Einrichtungsschritte und Beispiele für Neu erkunden. Welche
// Einrichtungsschritte es gibt, ihre Reihenfolge und ihr Zustand kommen vom
// Server (setup.items aus /api/v1/onboarding, nach Rechten und installierten
// Diensten gefiltert). Hier steht nur, wo ein Schritt liegt: Seite, Vorbereitung
// und das Ziel, das die Umsetzung ueber data-tour="<id>" findet.
//
// Felder je Einrichtungsschritt:
//   id, panel + page (Ort, wie ?panel=&page=), prepare (Ort oeffnen, z. B. Karte),
//   target (Selektor), check ('change' = hakt sich bei einer Eingabe im Ziel ab,
//   'leave' = gilt mit Weiter als erledigt).
// Titel und Text heissen onboarding.setup.<id>.title und .text und liegen im Katalog.
// emptyText (Katalogschluessel) ersetzt den Text, wenn emptyWhen() beim Zeigen true liefert.
//
// Die Felder areas, installerOnly, explore und welcome sind unveraendert.
(() => {
  const guestCard = () => [...document.querySelectorAll('#settings-users .users-card')]
    .find((card) => card.querySelector('.users-avatar')?.textContent.trim() === 'G');
  const guestSwitch = () => [...(guestCard()?.querySelectorAll('.users-line') || [])]
    .find((line) => line.querySelector('input[role="switch"]') && window.obSpotlight.visibleRect(line));
  // Benutzerseite: erst der Reiter Konten, dann die Karte aufklappen.
  const accountsTab = () => {
    const tab = [...document.querySelectorAll('#settings-users .users-tab')].find((t) => t.textContent.trim() === 'Konten');
    if (tab && tab.getAttribute('aria-selected') !== 'true') tab.click();
  };
  const openCard = (find) => async () => {
    accountsTab();
    await window.obOnboarding.waitFor(find, 1500);
    const card = find();
    if (card && !card.classList.contains('is-open')) card.querySelector('.users-card-head')?.click();
  };
  const firstVisible = (...selectors) => () => selectors.map((sel) => document.querySelector(sel))
    .find((el) => el && window.obSpotlight.visibleRect(el));
  const data = (id) => `[data-tour="${id}"]`;

  // Texte: Die Felder label, title, text, sub und detail enthalten Schlüssel des
  // Katalogs (onboarding.*), onboarding.js löst sie mit $t auf. Die Wörter
  // stehen in de.json und en.json. Version, PR und id bleiben Daten.
  const areas = [
    { id: 'access', label: 'onboarding.area.access' },
    { id: 'display', label: 'onboarding.area.display' },
    { id: 'system', label: 'onboarding.area.system' },
    { id: 'devices', label: 'onboarding.area.devices' },
  ];

  // Was nur der Installer abfragt und deshalb nie als Schritt in der Leiste
  // erscheint, nur als Zeile in der Checkliste.
  const installerOnly = [
    { id: 'admin', area: 'access', title: 'onboarding.setup.admin.title', sub: 'onboarding.setup.admin.sub' },
    { id: 'mqtt', area: 'system', title: 'onboarding.setup.mqtt.title', sub: 'onboarding.setup.mqtt.sub' },
    { id: 'services', area: 'system', title: 'onboarding.setup.services.title', sub: 'onboarding.setup.services.sub' },
  ];

  // Orte der Einrichtungsschritte. Die Reihenfolge, Bereiche und Zustände
  // kommen vom Server, onboarding.js führt beides zusammen.
  const setup = [
    { id: 'users', panel: 'settings-panel', page: 'users', prepare: accountsTab, target: data('users'), check: 'leave' },
    { id: 'guest', panel: 'settings-panel', page: 'users', prepare: openCard(guestCard), target: data('guest'), check: 'change' },
    { id: 'tailscale', panel: 'settings-panel', page: 'tailscale', target: data('tailscale'), check: 'leave' },
    { id: 'companion', panel: 'settings-panel', page: 'users', prepare: accountsTab, target: data('companion'), check: 'leave' },
    { id: 'theme', panel: 'settings-panel', page: 'display', target: data('theme'), check: 'change' },
    { id: 'title', panel: 'settings-panel', page: 'display', target: data('title'), check: 'change' },
    { id: 'statusbar', panel: 'settings-panel', page: 'display', target: data('statusbar'), check: 'change' },
    { id: 'tabwidth', panel: 'settings-panel', page: 'display', target: data('tabwidth'), check: 'change' },
    { id: 'tiles', panel: 'overview-panel', target: data('tiles'), check: 'leave' },
    { id: 'updates', panel: 'settings-panel', page: 'versions', target: data('updates'), check: 'change' },
    { id: 'metrics', panel: 'settings-panel', page: 'mqtt', target: data('metrics'), check: 'change' },
    // Geräte einrichten: Dienste wählen und Geräteinformationen eintragen. Das
    // Formular ist der Bereich dafür, die Schaltleiste darüber bleibt außen vor.
    { id: 'config', panel: 'config-panel', need: 'view_config', target: data('config'), check: 'change' },
    {
      id: 'devices', panel: 'devices-panel', target: data('devices'), check: 'leave',
      // Keine Geräte gefunden: Leertext statt der Erklärung. Die Prüfung läuft
      // beim Zeigen, weil sich die Karten bis dahin füllen können.
      emptyText: 'onboarding.setup.devices.empty',
      emptyWhen: () => !document.querySelector('#devices-panel .device-tile, #devices-panel .compact-card'),
    },
    {
      id: 'roles', panel: 'energy-panel', target: data('roles'), check: 'change',
      prepare: () => {
        const section = document.querySelector('section[data-energy-section="roles"]');
        if (section?.dataset.open !== 'true') document.getElementById('energy-acc-roles-toggle')?.click();
      },
    },
    { id: 'devicemap', panel: 'devicemap-panel', target: data('devicemap'), check: 'leave' },
  ];

  // Neu erkunden: Beispiel aus den letzten Highlights, so wie es aus
  // changelog.json kaeme. tour: false = Highlight ohne Tour (nur Text).
  const explore = [
    {
      version: '0.12.0', current: true,
      items: [
        {
          id: 'h184', pr: 184, title: 'onboarding.explore.h184.title',
          text: 'onboarding.explore.h184.text',
          detail: 'onboarding.explore.h184.detail',
          steps: [{ panel: 'settings-panel', page: 'users', target: guestSwitch, prepare: openCard(guestCard), need: 'edit_users' }],
        },
        {
          id: 'h183', pr: 183, title: 'onboarding.explore.h183.title',
          text: 'onboarding.explore.h183.text',
          detail: 'onboarding.explore.h183.detail',
          steps: [{ panel: 'stats-panel', target: firstVisible('#stats-panel .stats-view-toggle', '#stats-panel .stats-kpis'), need: 'view_stats', device: 'phone' }],
        },
        {
          id: 'h181', pr: 181, title: 'onboarding.explore.h181.title', tour: false,
          text: 'onboarding.explore.h181.text',
          detail: 'onboarding.explore.h181.detail',
        },
      ],
    },
    {
      version: '0.11.0',
      items: [
        {
          id: 'h180', pr: 180, title: 'onboarding.explore.h180.title',
          text: 'onboarding.explore.h180.text',
          detail: 'onboarding.explore.h180.detail',
          steps: [{ panel: 'devices-panel', target: '#devices-panel .device-tile', need: 'view_devices' }],
        },
      ],
    },
  ];

  // Begruessung fuer neue Benutzer und Gaeste: feste kurze Runde durch die
  // Hauptbereiche, nach Rechten gefiltert (Tabs ohne Recht fehlen im DOM und
  // entfallen deshalb von selbst).
  const welcome = [
    { id: 'w-overview', panel: 'overview-panel', target: '[role="tab"][aria-controls="overview-panel"]', title: 'onboarding.welcome.w-overview.title', text: 'onboarding.welcome.w-overview.text' },
    { id: 'w-devices', panel: 'devices-panel', target: '#tab-devices', title: 'onboarding.welcome.w-devices.title', text: 'onboarding.welcome.w-devices.text' },
    { id: 'w-stats', panel: 'stats-panel', target: '#tab-stats', title: 'onboarding.welcome.w-stats.title', text: 'onboarding.welcome.w-stats.text' },
    { id: 'w-settings', panel: 'settings-panel', page: 'checklist', target: '#tab-settings', title: 'onboarding.welcome.w-settings.title', text: 'onboarding.welcome.w-settings.text' },
  ];

  window.obSteps = { areas, setup, installerOnly, explore, welcome };
})();

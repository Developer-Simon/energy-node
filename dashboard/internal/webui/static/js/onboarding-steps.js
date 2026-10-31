// Temporärer fester Beispielkatalog für PR 1, wird in späteren Tasks durch Schritte vom Server ersetzt.
// Schrittkatalog des Entwurfs. In der Umsetzung kommt die Ersteinrichtung
// aus dem Dashboard (Go, nach Rechten und installierten Diensten gefiltert)
// und Neu erkunden aus /api/v1/changelog/highlights (changelog/highlights/<PR>.json).
// Ziele adressiert die Umsetzung ueber data-tour="<id>". Im Entwurf stehen
// hier noch Selektoren auf das heutige Markup, weil der Proxy keine
// Attribute setzt.
//
// Felder je Schritt:
//   id, area (nur Einrichten), panel + page (Ort, wie ?panel=&page=),
//   target (Selektor oder Funktion), prepare (Ort oeffnen, z. B. Karte),
//   title, text, check ('change' = hakt sich bei einer Eingabe im Ziel ab,
//   'leave' = gilt mit Weiter als erledigt), needs (Dienst, sonst entfaellt).
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
  const labelled = (id) => () => document.querySelector(`[aria-labelledby="${id}"]`);
  // Karte einer Einstellungsseite ueber ihre Ueberschrift.
  const card = (page, title) => () => [...document.querySelectorAll(`#settings-${page} h3`)]
    .find((h) => h.textContent.trim() === title)?.closest('.setting-card, section');

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

  // installer: true = dieselbe Einstellung gibt es im Installer-Schritt
  // Dashboard (gleiche Felder, gleiche Texte).

  const setup = [
    {
      id: 'users', area: 'access', installer: true, panel: 'settings-panel', page: 'users',
      target: '#settings-users .users-btn--add', prepare: accountsTab,
      title: 'onboarding.setup.users.title',
      text: 'onboarding.setup.users.text',
      check: 'leave',
    },
    {
      id: 'guest', area: 'access', installer: true, panel: 'settings-panel', page: 'users',
      target: guestSwitch, prepare: openCard(guestCard),
      title: 'onboarding.setup.guest.title',
      text: 'onboarding.setup.guest.text',
      check: 'change',
    },
    {
      id: 'tailscale', area: 'access', panel: 'settings-panel', page: 'tailscale', needs: 'tailscale',
      target: '#settings-tailscale',
      title: 'onboarding.setup.tailscale.title',
      text: 'onboarding.setup.tailscale.text',
      check: 'leave',
    },
    {
      id: 'companion', area: 'access', panel: 'settings-panel', page: 'users',
      target: card('users', 'Home Assistant'), prepare: accountsTab,
      title: 'onboarding.setup.companion.title',
      text: 'onboarding.setup.companion.text',
      check: 'leave',
    },
    {
      id: 'language', area: 'display', panel: 'settings-panel', page: 'display',
      target: labelled('lbl-ui-language'),
      title: 'onboarding.setup.language.title',
      text: 'onboarding.setup.language.text',
      check: 'change',
    },
    {
      id: 'theme', area: 'display', installer: true, panel: 'settings-panel', page: 'display',
      target: labelled('lbl-theme'),
      title: 'onboarding.setup.theme.title',
      text: 'onboarding.setup.theme.text',
      check: 'change',
    },
    {
      id: 'title', area: 'display', installer: true, panel: 'settings-panel', page: 'display',
      target: '#settings-display .masthead-setting',
      title: 'onboarding.setup.title.title',
      text: 'onboarding.setup.title.text',
      check: 'change',
    },
    {
      id: 'statusbar', area: 'display', installer: true, panel: 'settings-panel', page: 'display',
      target: card('display', 'Systemstatus'),
      title: 'onboarding.setup.statusbar.title',
      text: 'onboarding.setup.statusbar.text',
      check: 'change',
    },
    {
      id: 'tabwidth', area: 'display', installer: true, panel: 'settings-panel', page: 'display',
      target: card('display', window.I18n.t('settings.wide_tabs.title')),
      title: 'onboarding.setup.tabwidth.title',
      text: 'onboarding.setup.tabwidth.text',
      check: 'change',
    },
    {
      id: 'tiles', area: 'display', panel: 'overview-panel',
      target: '.layout-btn[data-mode-edit]',
      title: 'onboarding.setup.tiles.title',
      text: 'onboarding.setup.tiles.text',
      check: 'leave',
    },
    {
      id: 'updates', area: 'system', installer: true, panel: 'settings-panel', page: 'versions',
      target: card('versions', 'Updates'),
      title: 'onboarding.setup.updates.title',
      text: 'onboarding.setup.updates.text',
      check: 'change',
    },
    {
      id: 'metrics', area: 'system', installer: true, panel: 'settings-panel', page: 'mqtt',
      target: card('mqtt', 'Systemmetriken publizieren'),
      title: 'onboarding.setup.metrics.title',
      text: 'onboarding.setup.metrics.text',
      check: 'change',
    },
    {
      id: 'devices', area: 'devices', panel: 'devices-panel',
      target: '#devices-panel .device-tile-grid',
      title: 'onboarding.setup.devices.title',
      text: 'onboarding.setup.devices.text',
      check: 'leave',
    },
    {
      id: 'roles', area: 'devices', panel: 'energy-panel',
      target: '#energy-roles .energy-role-row', watch: '#energy-roles',
      prepare: () => {
        const section = document.querySelector('section[data-energy-section="roles"]');
        if (section?.dataset.open !== 'true') document.getElementById('energy-acc-roles-toggle')?.click();
      },
      title: 'onboarding.setup.roles.title',
      text: 'onboarding.setup.roles.text',
      check: 'change',
    },
    {
      id: 'devicemap', area: 'devices', panel: 'devicemap-panel',
      target: '#devicemap-canvas',
      title: 'onboarding.setup.devicemap.title',
      text: 'onboarding.setup.devicemap.text',
      check: 'leave',
    },
    {
      id: 'favorites', area: 'devices', panel: 'devices-panel',
      target: '#devices-panel .device-tile',
      title: 'onboarding.setup.favorites.title',
      text: 'onboarding.setup.favorites.text',
      check: 'leave',
    },
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
          steps: [{ panel: 'stats-panel', target: firstVisible('#stats-panel .stats-view-toggle', '#stats-panel .stats-kpis'), need: 'view_stats' }],
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

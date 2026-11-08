(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const tn = (key, n, params) => (window.I18n ? window.I18n.tn(key, n, params) : key);
  const cssEscape = value => (window.CSS && window.CSS.escape ? window.CSS.escape(value) : value);

  const HIDDEN_IDS_STORAGE_KEY = 'energy-roles-hidden-ids';
  // Live values of the measurement chains are read at most this often.
  const CHAIN_LIVE_INTERVAL_MS = 5000;
  // Chain suggestions are read again this often while the chains section is open.
  const CHAIN_SUGGEST_INTERVAL_MS = 5 * 60 * 1000;
  // Room the sticky action bar takes from a chain menu that opens downward.
  const CHAIN_MENU_BOTTOM_GAP_PX = 80;

  // Das Dashboard veröffentlicht seine eigene Bilanz als HA-MQTT-Gerät und
  // liest die Discovery zurück in die Registry (internal/energydiscovery,
  // DeviceIdentifier). Diese Rück-Entitäten dürfen im Rollen-Editor nicht als
  // zuweisbar erscheinen - sie würden die Bilanz doppelt zählen. Das Backend
  // hält sie schon aus energy.Aggregate heraus; die Zeilenliste hier stammt
  // aber direkt aus /api/v1/devices und braucht denselben Filter.
  const OWN_ENERGY_DEVICE_ID = 'energy_node';
  const GROUP_PREFIX = 'group:';
  const ENERGY_PAGE_SOURCE = 'energy-page';

  // Viele Integrationen stellen jedem Entitätsnamen den Gerätenamen voran
  // ("Trucki T2MG" / "Trucki T2MG DC Power"). Im Rollen-Editor steht der
  // Gerätename ohnehin schon links vom "/", die Wiederholung ist nur Breite.
  const stripDevicePrefix = (deviceName, entityName) => {
    const device = String(deviceName || '').trim();
    const entity = String(entityName || '').trim();
    if (!device || !entity) return entity;
    if (entity.toLowerCase() === device.toLowerCase()) return '';
    if (entity.toLowerCase().startsWith(device.toLowerCase())) {
      const rest = entity.slice(device.length).replace(/^[\s/:·\-–—]+/, '').trim();
      if (rest) return rest;
    }
    return entity;
  };

  const compareLabels = (a, b) => (window.I18n
    ? window.I18n.compare(a, b)
    : (String(a) < String(b) ? -1 : (String(a) > String(b) ? 1 : 0)));

  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(apiError(body));
    return body;
  };

  const SECTIONS = ['interpretation', 'roles', 'groups', 'categories', 'chains', 'subdevices'];
  const readOpenSection = () => {
    try {
      const stored = localStorage.getItem('energy-open-section');
      return stored === '' || SECTIONS.includes(stored) ? stored : 'roles';
    } catch (error) {
      return 'roles';
    }
  };

  const energyRolesPanel = () => ({
    ...window.DevicePicker.mixin(),
    entities: [],
    assignments: {},
    powerRoleOptions: [
      {value: 'pv', label: 'PV'},
      {value: 'battery', label: t('energy.board.row.battery')},
      {value: 'battery_charge', label: t('energy.page.role_option.battery_charge')},
      {value: 'battery_discharge', label: t('energy.page.role_option.battery_discharge')},
      {value: 'grid', label: t('energy.board.row.grid')},
      {value: 'grid_import', label: t('energy.page.role_option.grid_import')},
      {value: 'grid_export', label: t('energy.page.role_option.grid_export')},
      {value: 'load', label: t('energy.role.consumption')},
      {value: 'wallbox', label: t('energy.role.wallbox')},
      {value: 'heat_pump', label: t('energy.role.heat_pump')},
    ],
    socRoleOptions: [
      {value: 'battery_soc', label: t('energy.page.role_option.battery_soc')},
    ],
    switchRoleOptions: [
      {value: 'switch', label: t('energy.page.role_option.switch')},
    ],
    // Starting state of every switch row as loaded: role, channel and whether
    // a saved entry exists. payload() writes a switch row only when it was
    // saved before or differs from this state.
    switchBaseline: {},
    // Saved switch-role entries without a loaded row (device offline, or the
    // switch list failed). The PUT replaces every assignment, so save() writes
    // them back unchanged.
    orphanSwitches: {},
    socWithoutCapacity: 0,
    hiddenIds: {},
    hiddenExpanded: false,
    // The one accordion section that is open: interpretation, roles, groups,
    // categories or '' for none. Remembered per browser.
    openSection: readOpenSection(),
    // Groups whose switch "count as a whole" is on before a category is set.
    groupWhole: {},
    canEdit: true,
    csrfToken: '',
    flash: {},
    flashGroup: '',
    // Open chain finding tiles by finding key; a live refresh keeps the key.
    expandedFindings: {},
    _setTimeout: (fn, ms) => setTimeout(fn, ms),
    _flashTimer: null,
    _groupTimer: null,
    loading: false,
    saving: false,
    interpretation: {
      gap_mode: 'unknown_consumer',
      load_mode: 'auto',
      gap_tolerance_mode: 'absolute',
      gap_tolerance_w: 25,
      gap_tolerance_percent: 2,
      surplus_threshold_w: 800,
      import_threshold_w: 1500,
      battery_reserve_percent: 10,
    },
    unassigned: [],
    unassignedCount: 0,
    balanceTotal: 0,
    liveBalance: null,
    categories: {},
    categoryRemoval: null,
    groups: {},
    devicesList: [],
    iconCatalogue: [],
    subDeviceEntries: [],
    // Measurement chains: the saved chains, their live state, the chains
    // folded on a phone, the entities still needing a role and the chain
    // card that was just focused.
    chains: {},
    chainStates: {},
    chainOpen: {},
    roleNeeded: [],
    flashChain: '',
    // The chain whose direction menu is open, and the side it opens to.
    chainDirectionMenu: '',
    chainDirectionSide: 'bottom',
    _chainTimer: null,
    _chainLiveAt: 0,
    _now: () => Date.now(),
    // Suggested chains from the server, the pair in the confirm dialog and
    // the timer that reads the suggestions again while the section is open.
    chainSuggestions: [],
    chainSuggest: null,
    _chainSuggestTimer: null,
    _setInterval: (fn, ms) => setInterval(fn, ms),
    _clearInterval: id => clearInterval(id),

    init() {
      // Die Device Map speichert Rollen per PATCH. Ohne Neuladen haette diese
      // Seite den alten Stand und schriebe ihn beim naechsten Speichern zurueck.
      // Das eigene Speichern meldet sich ebenfalls, dann ist hier schon alles aktuell.
      window.addEventListener('energy-roles-changed', event => {
        if ((event.detail || {}).source !== ENERGY_PAGE_SOURCE) this.load();
      });
      // Sub-device labels come from the device map, which the map page saves.
      window.addEventListener('device-map-changed', () => this.load());
      window.addEventListener('energy-focus-rows', event => this.focusRows((event.detail && event.detail.ids) || []));
      window.addEventListener('energy-focus-group', event => this.focusGroup((event.detail && event.detail.id) || ''));
      // A draft from the efficiency tab or the device map. While the page loads, load() applies the request instead.
      window.addEventListener('energy-apply-draft', event => {
        if (!this.loading) this.applyDraft((event.detail) || {});
      });
      window.addEventListener('energy-focus-chain', event => {
        window.__energyChainRequest__ = null;
        this.openChain((event.detail && event.detail.id) || '');
      });
      window.addEventListener('registry-updated', () => this.refreshChainStatesLive());
    },

    async loadSession() {
      try {
        const session = await requestJSON('/api/v1/auth/session');
        this.csrfToken = session.csrf_token || '';
        this.canEdit = session.edit_energy !== false;
      } catch (error) {
        // Ohne Sitzungs-API laeuft die Instanz ohne Authentifizierung, dann
        // laesst auch requireEnergyMutation den Schreibzugriff durch.
        this.csrfToken = '';
        this.canEdit = true;
      }
    },

    isSectionOpen(name) {
      return this.openSection === name;
    },

    // Opening a section closes the open one at once. Without help the page
    // would jump: the closed section above takes its height with it and the
    // header just clicked slides up out of view. The scroll offset is
    // corrected so the clicked header stays where the pointer is.
    toggleSection(name, event) {
      this.setOpenSection(this.openSection === name ? '' : name, event && event.currentTarget);
    },

    setOpenSection(name, anchor) {
      const before = anchor && anchor.getBoundingClientRect ? anchor.getBoundingClientRect().top : null;
      this.openSection = name;
      try { localStorage.setItem('energy-open-section', name); } catch (error) { /* private mode: not remembered */ }
      this.syncChainSuggestions();
      if (before === null) return;
      this.$nextTick(() => {
        const shift = anchor.getBoundingClientRect().top - before;
        if (Math.abs(shift) > 1 && window.scrollBy) window.scrollBy(0, shift);
      });
    },

    sectionMeta(name) {
      if (name === 'roles') {
        const n = Object.values(this.assignments).filter(assignment => assignment.role).length;
        return tn('energy.section.roles_meta', n, {n});
      }
      if (name === 'groups') return tn('energy.section.groups_meta', Object.keys(this.groups).length, {n: Object.keys(this.groups).length});
      if (name === 'categories') return tn('energy.section.categories_meta', Object.keys(this.categories).length, {n: Object.keys(this.categories).length});
      if (name === 'subdevices') return tn('energy.section.subdevices_meta', this.subDeviceEntries.length, {n: this.subDeviceEntries.length});
      if (name === 'chains') {
        const n = Object.keys(this.chains).length;
        const meta = tn('energy.section.chains_meta', n, {n});
        const suggestions = this.chainSuggestions.length;
        return suggestions ? `${meta} · ${tn('energy.chains.meta_suggestions', suggestions, {n: suggestions})}` : meta;
      }
      return '';
    },

    focusRows(ids) {
      window.__energyFocusRequest__ = null;
      if (!ids.length) return;
      this.setOpenSection('roles');
      if (ids.some(id => this.isHidden(id))) this.hiddenExpanded = true;
      this.flash = Object.fromEntries(ids.map(id => [id, true]));
      this.$nextTick(() => {
        const escaped = window.CSS && window.CSS.escape ? window.CSS.escape(ids[0]) : ids[0];
        const row = document.querySelector(`[data-entity-id="${escaped}"]`);
        const reduce = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
        if (row && row.scrollIntoView) row.scrollIntoView({behavior: reduce ? 'auto' : 'smooth', block: 'nearest'});
      });
      clearTimeout(this._flashTimer);
      this._flashTimer = this._setTimeout(() => { this.flash = {}; }, 1600);
    },

    onRowClick(entityId) {
      window.dispatchEvent(new CustomEvent('energy-plant-focus', {detail: {entityId}}));
    },

    focusGroup(id) {
      this.flashGroup = id;
      this.setOpenSection('groups');
      this.$nextTick(() => {
        const card = document.querySelector(`[data-group-id="${CSS.escape(id)}"]`);
        if (card) card.scrollIntoView({behavior: window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'nearest'});
      });
      clearTimeout(this._groupTimer);
      this._groupTimer = this._setTimeout(() => { this.flashGroup = ''; }, 1600);
    },

    // Switch rows: every commandable switch, with the role and channel it
    // resolves to. A saved entry wins, otherwise the detection in
    // DeviceMapSwitch.resolveSwitches decides.
    switchRowsFrom(rawSwitches, devices, savedAssignments) {
      const switches = Array.isArray(rawSwitches) ? rawSwitches : [];
      const detected = window.DeviceMapSwitch.resolveSwitches({switches, assignments: savedAssignments, devices});
      const detectedById = new Map([...detected.values()].flat().map(entry => [entry.entityId, entry]));
      return switches
        .filter(item => item.device_id !== OWN_ENERGY_DEVICE_ID)
        .map(item => {
          const device = devices.find(candidate => candidate.id === item.device_id) || {};
          const shortName = stripDevicePrefix(device.name, item.name || item.entity_id);
          const label = device.name ? (shortName ? `${device.name} / ${shortName}` : device.name) : (shortName || item.entity_id);
          const saved = savedAssignments[item.entity_id];
          const hit = detectedById.get(item.entity_id);
          let assignment;
          if (saved) assignment = {role: saved.role || '', channel: saved.channel};
          else if (hit) assignment = {role: 'switch', channel: hit.channel};
          else assignment = {role: '', channel: 1};
          return {id: item.entity_id, label, unit: 'switch', deviceId: item.device_id, assignment};
        });
    },

    switchChannelOf(assignment) {
      return Number(assignment && assignment.channel) || 1;
    },

    isSwitchRow(id) {
      return Object.prototype.hasOwnProperty.call(this.switchBaseline, id);
    },

    get visibleSwitchEntities() {
      return this.entities.filter(entity => entity.unit === 'switch' && !this.isHidden(entity.id));
    },

    // Channels are a device property: a device with two or more switch rows
    // shows the channel field, counting hidden rows too.
    switchChannelCount(id) {
      const row = this.entities.find(entity => entity.id === id);
      if (!row || row.unit !== 'switch') return 0;
      return this.entities.filter(entity => entity.unit === 'switch' && entity.deviceId === row.deviceId).length;
    },

    showChannelField(id) {
      return this.isSwitchRow(id) && this.assignments[id].role === 'switch' && this.switchChannelCount(id) > 1;
    },

    // A switch row changed from its starting state. A row saved before is
    // always written back, so a saved switch cannot silently disappear.
    switchChanged(id) {
      const base = this.switchBaseline[id];
      const row = this.assignments[id];
      if (base.saved) return true;
      if (row.role !== base.role) return true;
      return row.role === 'switch' && this.switchChannelOf(row) !== base.channel;
    },

    switchPayload(id) {
      const row = this.assignments[id];
      const channel = this.switchChannelOf(row);
      return row.role === 'switch' && channel > 1 ? {role: row.role, channel} : {role: row.role};
    },

    async load() {
      this.loading = true;
      try {
        const [, devices, saved, snapshot, icons, switches, deviceMap] = await Promise.all([
          this.loadSession(),
          requestJSON('/api/v1/devices'),
          requestJSON('/api/v1/energy/roles'),
          requestJSON('/api/v1/energy'),
          requestJSON('/api/v1/device/icons').catch(() => []),
          requestJSON('/api/v1/devices/switches').catch(() => []),
          requestJSON('/api/v1/device/map').catch(() => ({})),
        ]);
        const savedAssignments = saved.assignments || {};
        const resolved = Object.fromEntries((snapshot.entities || []).map(item => [item.entity_id, item.role]));
        const powerEntities = devices
          .filter(device => device.id !== OWN_ENERGY_DEVICE_ID)
          .flatMap(device => device.entities
            .filter(entity => ['W', 'kW', '%'].includes(entity.unit_of_measurement))
            .map(entity => {
              const shortName = stripDevicePrefix(device.name, entity.name || entity.object_id);
              // A sub-device row reads "<device> · <sub-device>", the meter
              // device stays visible next to the measurement it belongs to.
              const subLabel = window.DeviceMapSubDevices.labelFor(deviceMap, entity.unique_id);
              let label;
              if (subLabel) label = device.name ? `${device.name} · ${subLabel}` : subLabel;
              else label = shortName ? `${device.name} / ${shortName}` : device.name;
              return {
                id: entity.unique_id,
                label,
                unit: entity.unit_of_measurement,
                assignment: savedAssignments[entity.unique_id] || resolved[entity.unique_id] || {},
              };
            }));
        const switchEntities = this.switchRowsFrom(switches, devices, savedAssignments);
        this.entities = powerEntities.concat(switchEntities);
        const rowIds = new Set(switchEntities.map(entity => entity.id));
        this.orphanSwitches = Object.fromEntries(Object.entries(savedAssignments)
          .filter(([id, entry]) => entry && entry.role === 'switch' && !rowIds.has(id)));
        this.switchBaseline = Object.fromEntries(switchEntities.map(entity => [entity.id, {
          role: entity.assignment.role,
          channel: this.switchChannelOf(entity.assignment),
          saved: Object.prototype.hasOwnProperty.call(savedAssignments, entity.id),
        }]));
        this.assignments = Object.fromEntries(this.entities.map(entity => [entity.id, {
          role: entity.assignment.role || '',
          scale: entity.assignment.scale || 1,
          invert: Boolean(entity.assignment.invert),
          capacity_kwh: Number(entity.assignment.capacity_kwh) || 0,
          channel: this.switchChannelOf(entity.assignment),
        }]));
        await this.reconcileHidden(saved.hidden);
        if (saved.interpretation) this.interpretation = {...this.interpretation, ...saved.interpretation};
        this.categories = JSON.parse(JSON.stringify(saved.categories || {}));
        this.groups = JSON.parse(JSON.stringify(saved.groups || {}));
        this.devicesList = devices
          .filter(device => device.id !== OWN_ENERGY_DEVICE_ID)
          .map(device => ({id: device.id, name: device.name || device.id, manufacturer: device.manufacturer || '', icon: device.icon_name || device.suggested_icon || ''}));
        this.iconCatalogue = icons || [];
        // Read-only overview of the sub-devices, never part of the categories.
        const meterName = deviceId => {
          const device = devices.find(item => item.id === deviceId);
          return device ? (device.name || device.id) : deviceId;
        };
        this.subDeviceEntries = window.DeviceMapSubDevices.list(deviceMap).map(sub => ({
          id: sub.id,
          label: sub.label || sub.entity_id,
          iconMarkup: this.iconMarkupOf(sub.icon),
          meter: meterName(sub.device_id),
        }));
        this.unassigned = snapshot.unassigned || [];
        this.unassignedCount = snapshot.unassigned_count || 0;
        this.balanceTotal = (snapshot.balance && snapshot.balance.total) || 0;
        this.liveBalance = snapshot.balance || null;
        this.socWithoutCapacity = snapshot.battery_soc_without_capacity || 0;
        this.chains = JSON.parse(JSON.stringify(saved.chains || {}));
        this.roleNeeded = [...(saved.role_needed || [])];
        this.chainStates = Object.fromEntries((snapshot.chains || []).map(chain => [chain.id, chain]));
        if (window.__energyFocusRequest__) {
          const ids = window.__energyFocusRequest__;
          window.__energyFocusRequest__ = null;
          this.focusRows(ids);
        }
        if (window.__energyChainRequest__) {
          const id = window.__energyChainRequest__;
          window.__energyChainRequest__ = null;
          this.openChain(id);
        }
        if (window.__energyDraftRequest__) {
          const draft = window.__energyDraftRequest__;
          window.__energyDraftRequest__ = null;
          this.applyDraft(draft);
        }
        this.syncChainSuggestions();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
      }
    },

    get gapModeIncludesInLoad() {
      return this.interpretation.gap_mode !== 'diagnostic';
    },
    set gapModeIncludesInLoad(value) {
      this.interpretation.gap_mode = value ? 'unknown_consumer' : 'diagnostic';
    },

    get hasLoadRole() {
      return Object.values(this.assignments).some(assignment => assignment.role === 'load');
    },

    get showMeasuredWarning() {
      return this.interpretation.load_mode === 'measured' && !this.hasLoadRole;
    },

    // Ein Satz unter den Modus-Kacheln, der den gewaehlten Modus erklaert.
    // Was gerade tatsaechlich gilt, sagt loadStatus darunter.
    get loadModeNote() {
      switch (this.interpretation.load_mode) {
        case 'measured':
          return t('energy.page.load_mode_note.measured');
        case 'calculated':
          return t('energy.page.load_mode_note.calculated');
        case 'auto':
          return t('energy.page.load_mode_note.auto');
        default:
          return '';
      }
    },

    // Der Live-Stand aus snapshot.balance: woher der Hausverbrauch gerade
    // kommt und ob der Hauszaehler zur Berechnung passt.
    // i18n-keys: energy.page.load_status.calculated, energy.page.load_status.measured,
    // energy.page.load_status.fallback_no_grid, energy.page.load_status.missing,
    // energy.page.load_status.meter_ok, energy.page.load_status.meter_deviates
    get loadStatus() {
      const b = this.liveBalance;
      if (!b) return '';
      let text;
      if (b.load_source === 'missing') text = t('energy.page.load_status.missing');
      else if (b.load_fallback === 'no_grid') text = t('energy.page.load_status.fallback_no_grid');
      else if (b.load_source === 'measured') text = t('energy.page.load_status.measured');
      else text = t('energy.page.load_status.calculated');
      if (b.meter_check === 'ok') text += ` ${t('energy.page.load_status.meter_ok')}`;
      else if (b.meter_check === 'deviates') {
        text += ` ${t('energy.page.load_status.meter_deviates', {deviation: `${Math.round(Math.abs(b.meter_deviation || 0))} W`})}`;
      }
      return text;
    },

    get toleranceWEquivalent() {
      if (this.interpretation.gap_tolerance_mode === 'percent') {
        return Math.round((Number(this.interpretation.gap_tolerance_percent) || 0) / 100 * this.balanceTotal);
      }
      return Math.round(Number(this.interpretation.gap_tolerance_w) || 0);
    },

    roleOptionsFor(unit) {
      if (unit === 'switch') return this.switchRoleOptions;
      return unit === '%' ? this.socRoleOptions : this.powerRoleOptions;
    },

    isSocRow(id) {
      return this.assignments[id] && this.assignments[id].role === 'battery_soc';
    },

    // A row with a role (saved or detected) is never hidden, so the list only
    // ever holds rows that have no role.
    hasRole(id) {
      return Boolean(this.assignments[id] && this.assignments[id].role);
    },

    isHidden(id) {
      return Boolean(this.hiddenIds[id]) && !this.hasRole(id);
    },

    canHide(id) {
      return this.canEdit && !this.hasRole(id);
    },

    // The shared list is the server set plus the legacy per-browser list from
    // localStorage, minus rows with a role. Differences go to the server in one
    // PATCH. The legacy key is dropped only after that, so a failed sync is
    // retried on the next load.
    async reconcileHidden(serverHidden) {
      const server = new Set(Array.isArray(serverHidden) ? serverHidden : []);
      const target = new Set(server);
      if (this.canEdit) this.legacyHiddenIds().forEach(id => target.add(id));
      [...target].filter(id => this.hasRole(id)).forEach(id => target.delete(id));
      this.hiddenIds = Object.fromEntries([...target].map(id => [id, true]));
      if (!this.canEdit) return;
      const hidden = {};
      target.forEach(id => { if (!server.has(id)) hidden[id] = true; });
      server.forEach(id => { if (!target.has(id)) hidden[id] = false; });
      try {
        if (Object.keys(hidden).length) await this.patchHidden(hidden);
        this.clearLegacyHidden();
      } catch (error) {
        // Sync is best effort: the rows still show the right state for this
        // load, and the next load tries the legacy list again.
      }
    },

    legacyHiddenIds() {
      try {
        const stored = JSON.parse(localStorage.getItem(HIDDEN_IDS_STORAGE_KEY) || '{}');
        return Object.keys(stored).filter(id => stored[id]);
      } catch (error) {
        return [];
      }
    },

    clearLegacyHidden() {
      try {
        localStorage.removeItem(HIDDEN_IDS_STORAGE_KEY);
      } catch (error) {
        // Private mode: nothing was stored to remove.
      }
    },

    patchHidden(hidden) {
      return requestJSON('/api/v1/energy/roles', {
        method: 'PATCH',
        headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
        body: JSON.stringify({hidden}),
      });
    },

    // The change shows at once and is saved right away. A refused save puts
    // the row back and says why.
    async toggleHidden(id) {
      if (!this.canEdit || this.hasRole(id)) return;
      const previous = {...this.hiddenIds};
      if (this.isHidden(id)) delete this.hiddenIds[id];
      else this.hiddenIds[id] = true;
      try {
        await this.patchHidden({[id]: this.hiddenIds[id] === true});
        this.notifyRolesChanged();
      } catch (error) {
        this.hiddenIds = previous;
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    get visibleEnergyEntities() {
      return this.entities.filter(entity => entity.unit !== '%' && entity.unit !== 'switch' && !this.isHidden(entity.id));
    },

    get visibleBatteryEntities() {
      return this.entities.filter(entity => entity.unit === '%' && !this.isHidden(entity.id));
    },

    get hiddenEntities() {
      return this.entities.filter(entity => this.isHidden(entity.id));
    },

    get hiddenCount() {
      return this.hiddenEntities.length;
    },

    get showSocCapacityWarning() {
      return this.socWithoutCapacity > 0;
    },

    get socCapacityWarning() {
      return tn('energy.page.soc_capacity_warning', this.socWithoutCapacity);
    },

    customRoleOptions() {
      return Object.entries(this.categories)
        .sort(([, a], [, b]) => compareLabels(a.label, b.label))
        .map(([id, def]) => ({value: `custom:${id}`, label: def.label}));
    },

    consumerRoleOptions() {
      return this.customRoleOptions().filter(option => (this.categories[option.value.slice('custom:'.length)] || {}).base === 'consumer');
    },

    // Same rule as DeviceMapModel.slugId (devicemap-model.js). The energy
    // page does not load the device map scripts, hence the copy.
    slug(label, existing) {
      const translit = {ä: 'ae', ö: 'oe', ü: 'ue', ß: 'ss'};
      const base = String(label || '').toLowerCase().replace(/[äöüß]/g, char => translit[char])
        .replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '') || 'eintrag';
      if (!existing.includes(base)) return base;
      for (let n = 2; ; n += 1) if (!existing.includes(`${base}_${n}`)) return `${base}_${n}`;
    },

    addCategory() {
      const label = t('energy.categories.new_label');
      const id = this.slug(label, Object.keys(this.categories));
      this.categories = {...this.categories, [id]: {label, base: 'consumer', color: 'cat_1', icon: (this.iconCatalogue[0] || {}).name || ''}};
      return id;
    },

    // Consumer categories a group can count as, with colour and icon for the
    // chips of the group card.
    groupRoleOptions() {
      return this.consumerRoleOptions().map(option => {
        const def = this.categories[option.value.slice('custom:'.length)] || {};
        return {...option, color: def.color || 'cat_1', iconMarkup: this.iconMarkupOf(def.icon)};
      });
    },

    groupCountsWhole(id) {
      return Boolean((this.groups[id] || {}).role) || Boolean(this.groupWhole[id]);
    },

    // Switch "count as a whole": off clears the role, on picks the first
    // consumer category, or waits for one when there is none yet.
    setGroupWhole(id, on) {
      const group = this.groups[id];
      this.groupWhole = {...this.groupWhole, [id]: on};
      if (!on) { group.role = ''; return; }
      const first = this.groupRoleOptions()[0];
      if (!group.role && first) group.role = first.value;
    },

    setGroupRole(id, value) {
      this.groups[id].role = value;
    },

    // "New category" on a group card: creates a consumer category, gives it
    // to the group and opens the categories section with its name selected,
    // so it can be named right away.
    addCategoryForGroup(groupId) {
      const id = this.addCategory();
      this.groups[groupId].role = `custom:${id}`;
      this.groupWhole = {...this.groupWhole, [groupId]: true};
      this.setOpenSection('categories');
      this.$nextTick(() => {
        const input = document.querySelector(`[data-category-id="${CSS.escape(id)}"] .energy-groups-name`);
        if (input) { input.focus(); if (input.select) input.select(); }
      });
    },

    removeCategory(id) {
      const next = {...this.categories};
      delete next[id];
      this.categories = next;
    },

    // Asks before removing: automations that still use the category are listed.
    async askRemoveCategory(id) {
      this.categoryRemoval = {id, label: this.categories[id].label, loading: true, error: false, rules: []};
      this.$refs.categoryRemoveDialog?.showModal();
      try {
        const body = await requestJSON('/api/v1/energy/category-references?id=' + encodeURIComponent(id));
        if (this.categoryRemoval?.id !== id) return;
        this.categoryRemoval.rules = body.rules || [];
      } catch (error) {
        if (this.categoryRemoval?.id !== id) return;
        this.categoryRemoval.error = true;
      } finally {
        if (this.categoryRemoval?.id === id) this.categoryRemoval.loading = false;
      }
    },

    confirmRemoveCategory() {
      this.removeCategory(this.categoryRemoval.id);
      this.closeCategoryRemoval();
    },

    closeCategoryRemoval() {
      if (this.$refs.categoryRemoveDialog?.open) this.$refs.categoryRemoveDialog.close();
      this.categoryRemoval = null;
    },

    openRemovalRule(ruleId) {
      this.closeCategoryRemoval();
      window.dispatchEvent(new CustomEvent('dashboard-open-panel', {detail: {panel: 'automations-panel', automationFocus: ruleId}}));
    },

    addGroup() {
      const label = t('energy.groups.new_label');
      const id = this.slug(label, Object.keys(this.groups));
      this.groups = {...this.groups, [id]: {label, members: {devices: [], groups: []}}};
    },

    removeGroup(id) {
      const next = {};
      for (const [gid, group] of Object.entries(this.groups)) {
        if (gid === id) continue;
        next[gid] = {...group, members: {...group.members, groups: (group.members.groups || []).filter(child => child !== id)}};
      }
      this.groups = next;
    },

    iconMarkupOf(name) {
      const icon = this.iconCatalogue.find(candidate => candidate.name === name) || this.iconCatalogue[0];
      return icon ? icon.markup : '';
    },

    // Members of a group for its chips. Group members carry a "group:" id so
    // they never collide with a device id.
    groupMembers(groupId) {
      const members = (this.groups[groupId] || {}).members || {};
      const deviceName = id => (this.devicesList.find(device => device.id === id) || {}).name || id;
      return [
        ...(members.groups || []).map(id => ({id: `${GROUP_PREFIX}${id}`, kind: 'group', name: (this.groups[id] || {}).label || id})),
        ...(members.devices || []).map(id => ({id, kind: 'device', name: deviceName(id)})),
      ];
    },

    groupOf(memberId, kind) {
      return Object.entries(this.groups).find(([, group]) => ((group.members || {})[kind] || []).includes(memberId));
    },

    removeGroupMember(groupId, memberId) {
      const group = this.groups[groupId];
      const kind = memberId.startsWith(GROUP_PREFIX) ? 'groups' : 'devices';
      const id = kind === 'groups' ? memberId.slice(GROUP_PREFIX.length) : memberId;
      group.members[kind] = (group.members[kind] || []).filter(other => other !== id);
    },

    // Opens the device picker. A chosen device or group leaves the group it
    // was in before, the picker shows that as "in <group>" up front. Nothing
    // is stored until the page is saved.
    async addGroupMembers(groupId) {
      const group = this.groups[groupId];
      const members = group.members || {};
      const memberOf = (id, kind) => {
        const previous = this.groupOf(id, kind);
        return previous && previous[0] !== groupId ? previous[1].label : '';
      };
      const items = [
        ...Object.entries(this.groups)
          .filter(([other]) => other !== groupId && !(members.groups || []).includes(other))
          .map(([other, def]) => ({id: `${GROUP_PREFIX}${other}`, kind: 'group', name: def.label, section: '', valueText: '', iconMarkup: '', memberOf: memberOf(other, 'groups')})),
        ...this.devicesList
          .filter(device => !(members.devices || []).includes(device.id))
          .map(device => ({id: device.id, kind: 'device', name: device.name, section: device.manufacturer, valueText: '', iconMarkup: this.iconMarkupOf(device.icon), memberOf: memberOf(device.id, 'devices')})),
      ];
      const ids = await this.openPicker({title: t('device_picker.title', {group: group.label}), items});
      if (!ids) return;
      for (const pickedId of ids) {
        const kind = pickedId.startsWith(GROUP_PREFIX) ? 'groups' : 'devices';
        const id = kind === 'groups' ? pickedId.slice(GROUP_PREFIX.length) : pickedId;
        const previous = this.groupOf(id, kind);
        if (previous && previous[0] !== groupId) previous[1].members[kind] = previous[1].members[kind].filter(other => other !== id);
        group.members[kind] = [...(group.members[kind] || []), id];
      }
    },

    payload() {
      // Kein .filter() auf assignment.role: eine leere Rolle ("Keine Rolle"
      // im Dropdown) muss als expliziter Override gespeichert werden, sonst
      // greift beim naechsten Laden wieder die Heuristik (inferRole in
      // energy.go) - z.B. bei ApSystems-Entities, deren Name/Topic "pv"
      // enthaelt und die sonst immer wieder auf PV zurückspringen.
      // Switch rows only go into the body when they were saved before or
      // changed: an untouched detected switch stays out, so the detection
      // keeps working for it.
      const assignments = Object.fromEntries(Object.entries(this.assignments)
        .filter(([id]) => !this.isSwitchRow(id) || this.switchChanged(id))
        .map(([id, assignment]) => [id, this.isSwitchRow(id)
          ? this.switchPayload(id)
          : assignment.role === 'battery_soc'
            ? {role: 'battery_soc', capacity_kwh: Number(assignment.capacity_kwh) || 0}
            : {
              role: assignment.role,
              scale: Number(assignment.scale) || 1,
              invert: Boolean(assignment.invert),
            }]));
      const interpretation = {
        gap_mode: this.interpretation.gap_mode,
        load_mode: this.interpretation.load_mode,
        gap_tolerance_mode: this.interpretation.gap_tolerance_mode,
        gap_tolerance_w: Number(this.interpretation.gap_tolerance_w) || 0,
        gap_tolerance_percent: Number(this.interpretation.gap_tolerance_percent) || 0,
        surplus_threshold_w: Number(this.interpretation.surplus_threshold_w) || 0,
        import_threshold_w: Number(this.interpretation.import_threshold_w) || 0,
        // Number('') ist NaN und faellt hier auf 0 - eine leergeraeumte
        // Eingabe heisst damit dasselbe wie eine getippte 0: keine Reserve.
        battery_reserve_percent: Number(this.interpretation.battery_reserve_percent) || 0,
      };
      const categories = this.categories;
      const groups = Object.fromEntries(Object.entries(this.groups)
        .map(([id, g]) => [id, g.role ? g : {label: g.label, members: g.members}]));
      const orphans = Object.fromEntries(Object.entries(this.orphanSwitches)
        .map(([id, entry]) => [id, entry.channel === undefined ? {role: entry.role} : {role: entry.role, channel: entry.channel}]));
      return {assignments: {...orphans, ...assignments}, interpretation, categories, groups};
    },

    revisionConfig() {
      return {
        basePath: '/api/v1/energy',
        current: () => this.payload(),
        reload: async () => {
          await this.load();
          this.notifyRolesChanged();
        },
        label: t('energy.page.revisions_label'),
        headers: () => ({'X-CSRF-Token': this.csrfToken}),
      };
    },

    // Device map and plant view reload groups, roles and categories on this.
    notifyRolesChanged() {
      window.dispatchEvent(new CustomEvent('energy-roles-changed', {detail: {source: ENERGY_PAGE_SOURCE}}));
    },

    async save() {
      this.saving = true;
      const {assignments, interpretation, categories, groups} = this.payload();
      try {
        await requestJSON('/api/v1/energy/roles', {
          method: 'PUT',
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
          body: JSON.stringify({assignments, interpretation, categories, groups}),
        });
        this.markSwitchesSaved(assignments);
        this.$store.toasts.push(t('energy.page.saved'));
        this.notifyRolesChanged();
        await this.refreshLiveBalance();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.saving = false;
      }
    },

    // After a successful save the written switch rows are the new starting
    // state, so a later change back to the old role is written again.
    markSwitchesSaved(assignments) {
      const next = {...this.switchBaseline};
      for (const id of Object.keys(assignments)) {
        if (!this.isSwitchRow(id)) continue;
        next[id] = {role: this.assignments[id].role, channel: this.switchChannelOf(this.assignments[id]), saved: true};
      }
      this.switchBaseline = next;
    },

    // Holt nach dem Speichern die Bilanz neu, damit die Statuszeile den
    // gerade gewaehlten Modus zeigt. Ein Fehler laesst den alten Stand stehen.
    async refreshLiveBalance() {
      try {
        const snapshot = await requestJSON('/api/v1/energy');
        this.liveBalance = snapshot.balance || null;
        this.balanceTotal = (snapshot.balance && snapshot.balance.total) || 0;
      } catch (error) {
        // Statuszeile ist Zusatzinformation, kein Toast.
      }
    },

    // Measurement chains. Every change goes to the chain endpoints, which
    // answer with the saved energy config. Without edit rights the cards only show.
    chainBody(chain) {
      return {label: chain.label, points: chain.points, counting: chain.counting, direction: chain.direction || ''};
    },

    // Takes chains and role_needed from the answer. The roles come only for
    // the rows the chain touched (old and new points), unsaved drafts on
    // other rows stay as they are.
    applyChainResponse(saved, touched = []) {
      this.chains = saved.chains || {};
      this.roleNeeded = [...(saved.role_needed || [])];
      // An answer without assignments carries no roles: the rows keep theirs.
      if (saved.assignments) {
        for (const id of touched) {
          if (this.assignments[id]) this.assignments[id].role = (saved.assignments[id] && saved.assignments[id].role) || '';
        }
      }
      this.notifyRolesChanged();
    },

    // One chain request. A refused call shows its message and changes nothing.
    async sendChain(method, url, body, touched = []) {
      try {
        const saved = await requestJSON(url, {
          method,
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
          ...(body === undefined ? {} : {body: JSON.stringify(body)}),
        });
        this.applyChainResponse(saved, touched);
        await this.loadChainStates();
        await this.loadChainSuggestions();
        return saved;
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
        return null;
      }
    },

    chainUrl(id) {
      return `/api/v1/energy/chains/${encodeURIComponent(id)}`;
    },

    // Suggested chains. Read while the chains section is open: on opening,
    // after every chain change and every CHAIN_SUGGEST_INTERVAL_MS. A refused
    // read (no data directory) shows no suggestions.
    syncChainSuggestions() {
      this._clearInterval(this._chainSuggestTimer);
      this._chainSuggestTimer = null;
      if (!this.isSectionOpen('chains')) return;
      this.loadChainSuggestions();
      this._chainSuggestTimer = this._setInterval(() => this.loadChainSuggestions(), CHAIN_SUGGEST_INTERVAL_MS);
    },

    async loadChainSuggestions() {
      try {
        const body = await requestJSON('/api/v1/energy/chain-suggestions');
        this.chainSuggestions = body.suggestions || [];
      } catch (error) {
        this.chainSuggestions = [];
      }
    },

    // Device name first (the dialog labels the chain with the device of B),
    // then the entity label. Unknown IDs show as they are.
    chainSuggestName(id) {
      const device = this.devicesList.find(item => item.id === id);
      return device ? device.name : this.chainLabelOf(id);
    },

    chainSuggestionText(suggestion) {
      return window.EnergyChainSuggest.suggestionText(suggestion, id => this.chainSuggestName(id), this.chains);
    },

    chainSuggestionEvidence(suggestion) {
      return window.EnergyChainSuggest.evidenceText(suggestion);
    },

    // Opens the confirm dialog. Nothing is saved before the dialog is confirmed.
    acceptChainSuggestion(suggestion) {
      if (!this.canEdit || !suggestion) return;
      this.chainSuggest = window.EnergyChainSuggest.dialogFor(suggestion, id => this.chainSuggestName(id));
      if (this.$refs.chainSuggestDialog && this.$refs.chainSuggestDialog.showModal) this.$refs.chainSuggestDialog.showModal();
    },

    chainSuggestSwap() {
      if (this.chainSuggest) this.chainSuggest = window.EnergyChainSuggest.swap(this.chainSuggest);
    },

    chainSuggestCancel() {
      if (this.$refs.chainSuggestDialog && this.$refs.chainSuggestDialog.open) this.$refs.chainSuggestDialog.close();
      this.chainSuggest = null;
    },

    // The dialog order goes through EnergyChains.edgeChange, the same rule as
    // the map card. A conflict saves nothing. A refused request keeps the
    // dialog open so the order can be changed.
    async chainSuggestSave() {
      const dialog = this.chainSuggest;
      if (!this.canEdit || !dialog) return;
      const label = (dialog.label || '').trim() || t('energy.chains.default_name');
      const change = window.EnergyChainSuggest.changeFor({...dialog, label}, this.chains);
      if (change.action === 'conflict') {
        this.$store.toasts.push(t('energy.chains.map_conflict'), 'critical');
        this.chainSuggestCancel();
        return;
      }
      let saved = {};
      if (change.action === 'create') {
        saved = await this.sendChain('POST', '/api/v1/energy/chains', change.body, change.body.points);
      } else if (change.action === 'update') {
        saved = await this.sendChain('PUT', this.chainUrl(change.id), this.chainBody(change.body), change.body.points);
      } else {
        // Already neighbours in one chain: nothing to save, only the list is stale.
        await this.loadChainSuggestions();
      }
      if (!saved) return;
      this.chainSuggestCancel();
    },

    // Dismissal is stored at once and survives a reload, no chain changes.
    async dismissChainSuggestion(suggestion) {
      if (!this.canEdit || !suggestion) return;
      try {
        await requestJSON('/api/v1/energy/chain-suggestions/dismiss', {
          method: 'POST',
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
          body: JSON.stringify({a: suggestion.a, b: suggestion.b}),
        });
        this.chainSuggestions = this.chainSuggestions.filter(item => !(item.a === suggestion.a && item.b === suggestion.b));
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    // Power rows only: percent and switch rows are never measuring points.
    chainPickerItems() {
      const rows = this.entities.map(entity => ({
        id: entity.id,
        name: entity.label,
        section: '',
        valueText: '',
        iconMarkup: '',
        unit_of_measurement: entity.unit,
      }));
      return window.EnergyChains.pickerItems(rows, this.chains);
    },

    async loadChainStates() {
      try {
        const snapshot = await requestJSON('/api/v1/energy');
        this.chainStates = Object.fromEntries((snapshot.chains || []).map(chain => [chain.id, chain]));
      } catch (error) {
        // The live values are extra, the saved chain stays on screen.
      }
    },

    // Live values load with at least one chain, also while the chains section
    // is closed, because the notices read the findings from them. At most
    // every CHAIN_LIVE_INTERVAL_MS.
    refreshChainStatesLive() {
      if (!Object.keys(this.chains).length) return;
      const now = this._now();
      if (now - this._chainLiveAt < CHAIN_LIVE_INTERVAL_MS) return;
      this._chainLiveAt = now;
      this.loadChainStates();
    },

    // Picker first, then the chain with the picked points in selection order.
    async addChain() {
      if (!this.canEdit) return;
      const label = t('energy.chains.default_name');
      const ids = await this.openPicker({
        title: t('energy.chains.picker_title', {chain: label}),
        items: this.chainPickerItems(),
      });
      if (!ids) return;
      if (ids.length < 2) {
        this.$store.toasts.push(t('energy.chains.pick_two'), 'info');
        return;
      }
      const before = Object.keys(this.chains);
      const saved = await this.sendChain('POST', '/api/v1/energy/chains', {label, points: ids, counting: ids[0]}, ids);
      if (!saved) return;
      this.setOpenSection('chains');
      const id = Object.keys(this.chains).find(key => !before.includes(key));
      if (!id) return;
      this.$nextTick(() => {
        const input = document.querySelector(`[data-chain-id="${cssEscape(id)}"] .energy-groups-name`);
        if (input) input.focus();
      });
    },

    // The name is saved on change, an empty or unchanged name does nothing.
    async renameChain(id, label) {
      const chain = this.chains[id];
      const next = String(label || '').trim();
      if (!this.canEdit || !chain || !next || next === chain.label) return;
      const saved = await this.sendChain('PUT', this.chainUrl(id), this.chainBody({...chain, label: next}), chain.points);
      if (saved) return;
      // Refused: the typed name goes back to the saved label.
      const input = document.querySelector(`[data-chain-id="${cssEscape(id)}"] .energy-groups-name`);
      if (input) input.value = chain.label;
    },

    async removeChain(id) {
      const chain = this.chains[id];
      if (!this.canEdit || !chain) return;
      await this.sendChain('DELETE', this.chainUrl(id), undefined, chain.points);
    },

    // The picked points go in at index, one after another in selection order.
    async insertChainPoint(id, index) {
      if (!this.canEdit || !this.chains[id]) return;
      const ids = await this.openPicker({
        title: t('energy.chains.picker_title', {chain: this.chains[id].label}),
        items: this.chainPickerItems(),
      });
      const chain = this.chains[id];
      if (!ids || !ids.length || !chain) return;
      let next = chain;
      ids.forEach((pointId, offset) => { next = window.EnergyChains.insertPoint(next, index + offset, pointId); });
      await this.sendChain('PUT', this.chainUrl(id), this.chainBody(next), [...chain.points, ...ids]);
    },

    // A chain needs two points. Removing one that leaves fewer asks first and
    // then deletes the chain.
    async removeChainPoint(id, entityId) {
      const chain = this.chains[id];
      if (!this.canEdit || !chain) return;
      const next = window.EnergyChains.removePoint(chain, entityId);
      if (next === null) {
        const confirmed = await this.$store.modal.confirm({
          title: t('energy.chains.remove'),
          body: t('energy.chains.remove_last_confirm', {chain: chain.label}),
          confirmLabel: t('energy.chains.remove'),
          danger: true,
        });
        if (confirmed) await this.removeChain(id);
        return;
      }
      await this.sendChain('PUT', this.chainUrl(id), this.chainBody(next), chain.points);
    },

    async setChainCounting(id, entityId) {
      const chain = this.chains[id];
      if (!this.canEdit || !chain || chain.counting === entityId) return;
      const next = window.EnergyChains.setCounting(chain, entityId);
      await this.sendChain('PUT', this.chainUrl(id), this.chainBody(next), chain.points);
    },

    async setChainDirection(id, direction) {
      const chain = this.chains[id];
      if (!this.canEdit || !chain || (chain.direction || 'auto') === direction) return;
      // The menu shows the choice at once, a refused call takes it back.
      const previous = chain.direction;
      chain.direction = direction;
      const saved = await this.sendChain('PUT', this.chainUrl(id), this.chainBody({...chain, direction}), chain.points);
      if (!saved && this.chains[id]) this.chains[id].direction = previous;
    },

    chainDirectionOptions() {
      return [
        {value: 'auto', title: t('energy.chains.direction.auto'), desc: t('energy.chains.direction.auto_desc')},
        {value: 'reversed', title: t('energy.chains.direction.reversed'), desc: t('energy.chains.direction.reversed_desc')},
        {value: 'fixed_down', title: t('energy.chains.direction.fixed_down'), desc: t('energy.chains.direction.fixed_down_desc')},
        {value: 'fixed_up', title: t('energy.chains.direction.fixed_up'), desc: t('energy.chains.direction.fixed_up_desc')},
      ];
    },

    chainDirectionTitle(chain) {
      const value = chain.direction || 'auto';
      return (this.chainDirectionOptions().find(option => option.value === value) || {title: value}).title;
    },

    // Opens below the trigger, or above it when the action bar leaves too
    // little room. The focus moves to the current choice, so the arrow keys
    // start there.
    toggleChainDirectionMenu(id, trigger) {
      if (this.chainDirectionMenu === id) {
        this.closeChainDirectionMenu();
        return;
      }
      const root = trigger.closest('.energy-chain-direction');
      const menu = root.querySelector('[role="listbox"]');
      const rect = trigger.getBoundingClientRect();
      const below = window.innerHeight - rect.bottom - CHAIN_MENU_BOTTOM_GAP_PX;
      this.chainDirectionSide = below < menu.offsetHeight && rect.top > below ? 'top' : 'bottom';
      this.chainDirectionMenu = id;
      this.$nextTick(() => {
        const current = menu.querySelector('[aria-selected="true"]') || menu.querySelector('[role="option"]');
        if (current) current.focus({preventScroll: true});
      });
    },

    closeChainDirectionMenu(trigger) {
      this.chainDirectionMenu = '';
      if (trigger) trigger.focus({preventScroll: true});
    },

    async chooseChainDirection(id, direction, option) {
      const root = option.closest('.energy-chain-direction');
      this.closeChainDirectionMenu(root.querySelector('button'));
      await this.setChainDirection(id, direction);
    },

    // Listbox keys: arrows, Home and End move, Enter and Space choose, Escape
    // closes and Tab leaves. Arrows on the closed trigger open the menu.
    onChainDirectionKey(event, id) {
      const root = event.currentTarget;
      const trigger = root.querySelector('button');
      if (this.chainDirectionMenu !== id) {
        if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && !trigger.disabled) {
          event.preventDefault();
          this.toggleChainDirectionMenu(id, trigger);
        }
        return;
      }
      const options = [...root.querySelectorAll('[role="option"]')];
      const at = options.indexOf(document.activeElement);
      const move = index => {
        event.preventDefault();
        options[Math.max(0, Math.min(index, options.length - 1))].focus({preventScroll: true});
      };
      if (event.key === 'ArrowDown') move(at + 1);
      else if (event.key === 'ArrowUp') move(at - 1);
      else if (event.key === 'Home') move(0);
      else if (event.key === 'End') move(options.length - 1);
      else if (event.key === 'Tab') this.closeChainDirectionMenu();
      else if (event.key === 'Escape') {
        event.preventDefault();
        event.stopPropagation();
        this.closeChainDirectionMenu(trigger);
      } else if ((event.key === 'Enter' || event.key === ' ') && at >= 0) {
        event.preventDefault();
        this.chooseChainDirection(id, options[at].dataset.value, options[at]);
      }
    },

    // Name of a measuring point as the rows show it, for the path and the node titles.
    chainLabelOf(entityId) {
      const row = this.entities.find(entity => entity.id === entityId);
      return row ? row.label : entityId;
    },

    chainPathOf(chain) {
      return window.EnergyChains.shortPath(chain, entityId => this.chainLabelOf(entityId));
    },

    chainPointState(chainId, entityId) {
      const points = (this.chainStates[chainId] || {}).points || [];
      const point = points.find(item => item.entity_id === entityId);
      return point && point.has_value ? point : null;
    },

    // Live value of one point, or a dash while it has none.
    chainPointValue(chainId, entityId) {
      const point = this.chainPointState(chainId, entityId);
      return point ? window.DeviceMapModel.formatPower(point.value) : '–';
    },

    // The number behind chainPointValue, for x-roll. Empty sets the text at once.
    chainPointRaw(chainId, entityId) {
      const point = this.chainPointState(chainId, entityId);
      return point ? Number(point.value) : '';
    },

    // Segment between the points at index and index + 1, matched in either flow direction.
    chainSegment(chainId, index) {
      const chain = this.chains[chainId];
      const state = this.chainStates[chainId];
      if (!chain || !state) return null;
      const first = chain.points[index];
      const second = chain.points[index + 1];
      return (state.segments || []).find(item => (item.from === first && item.to === second) || (item.from === second && item.to === first)) || null;
    },

    chainLossText(chainId, index) {
      return window.EnergyChains.lossText(this.chainSegment(chainId, index));
    },

    chainLossPowerText(chainId, index) {
      return window.EnergyChains.lossPowerOf(this.chainSegment(chainId, index));
    },

    chainLossRaw(chainId, index) {
      const segment = this.chainSegment(chainId, index);
      return segment && segment.available ? Number(segment.loss_w) || 0 : '';
    },

    chainEfficiencyText(chainId, index) {
      const percent = window.EnergyChains.efficiencyPercent(this.chainSegment(chainId, index));
      return percent === null ? '' : window.EnergyChains.percentText(percent);
    },

    chainEfficiencyRaw(chainId, index) {
      const percent = window.EnergyChains.efficiencyPercent(this.chainSegment(chainId, index));
      return percent === null ? '' : percent;
    },

    // The hint on a role row: the chain an entity is a reference of, or null.
    chainHintOf(entityId) {
      const hit = window.EnergyChains.chainOf(this.chains, entityId);
      if (!hit) return null;
      return {id: hit.id, label: hit.chain.label, counting: hit.chain.counting};
    },

    isChainReference(entityId) {
      return window.EnergyChains.isReference(this.chains, entityId);
    },

    // Opens the chains section and lets the card of the chain flash, like focusGroup.
    openChain(id) {
      this.flashChain = id;
      this.setOpenSection('chains');
      this.$nextTick(() => {
        const card = document.querySelector(`[data-chain-id="${cssEscape(id)}"]`);
        const reduce = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
        if (card && card.scrollIntoView) card.scrollIntoView({behavior: reduce ? 'auto' : 'smooth', block: 'nearest'});
      });
      clearTimeout(this._chainTimer);
      this._chainTimer = this._setTimeout(() => { this.flashChain = ''; }, 1600);
    },

    // One tile per chain with findings, in the order of the server. Each tile
    // lists its findings; the suggestion of a finding can be taken over when
    // the editor may write and the row exists. The open state keys by chain.
    get findingNotices() {
      const EC = window.EnergyChains;
      return EC.groupFindings(EC.allFindings(this.chainStates)).map(group => ({
        key: group.key,
        domId: `energy-chain-finding-${group.key.replace(/[^A-Za-z0-9_-]/g, '-')}`,
        title: EC.groupTitle(group),
        items: group.findings.map(finding => ({
          key: `${finding.kind}|${finding.from || ''}|${finding.to || ''}`,
          text: EC.findingText(group.chain, finding),
          suggestion: EC.suggestionText(group.chain, finding),
          canApply: this.canEdit && Boolean(finding.suggestion) && Boolean(this.assignments[finding.suggestion.entity_id]),
          finding,
        })),
      }));
    },

    isFindingOpen(key) {
      return Boolean(this.expandedFindings[key]);
    },

    toggleFinding(key) {
      this.expandedFindings = {...this.expandedFindings, [key]: !this.expandedFindings[key]};
    },

    // The efficiency link shows only when the tab is on the page (view_efficiency).
    get hasEfficiencyTab() {
      return Boolean(document.querySelector('[role="tab"][aria-controls="efficiency-panel"]'));
    },

    openEfficiency() {
      window.dispatchEvent(new CustomEvent('dashboard-open-panel', {detail: {panel: 'efficiency-panel'}}));
    },

    applyFinding(item) {
      if (!item || !item.canApply) return;
      this.applyDraft(item.finding.suggestion);
    },

    // Fills the draft of a row: invert flips, scale_factor multiplies the scale.
    // Nothing is saved, the user saves the page as usual. Returns false when
    // the row does not exist or the editor may not write; then the request stays.
    applyDraft(draft) {
      if (!this.canEdit || !draft || !draft.entity_id) return false;
      const row = this.assignments[draft.entity_id];
      if (!row) return false;
      if (draft.invert) row.invert = !row.invert;
      if (draft.scale_factor) row.scale = Number((Number(row.scale || 1) * draft.scale_factor).toPrecision(12));
      window.__energyDraftRequest__ = null;
      this.focusRows([draft.entity_id]);
      this.$store.toasts.push(t('energy.chains.draft_applied'), 'info');
      return true;
    },

    async dismissRoleNeeded(id) {
      if (!this.canEdit) return;
      try {
        await requestJSON('/api/v1/energy/roles', {
          method: 'PATCH',
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
          body: JSON.stringify({role_needed: {[id]: false}}),
        });
        this.roleNeeded = this.roleNeeded.filter(other => other !== id);
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },
  });


  const register = () => {
    if (window.Alpine) window.Alpine.data("energyRolesPanel", energyRolesPanel);
  };
  if (window.Alpine) register(); else document.addEventListener("alpine:init", register, {once: true});
})();

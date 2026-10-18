(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const tn = (key, n, params) => (window.I18n ? window.I18n.tn(key, n, params) : key);

  const HIDDEN_IDS_STORAGE_KEY = 'energy-roles-hidden-ids';

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

  const SECTIONS = ['interpretation', 'roles', 'groups', 'categories'];
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
        if (window.__energyFocusRequest__) {
          const ids = window.__energyFocusRequest__;
          window.__energyFocusRequest__ = null;
          this.focusRows(ids);
        }
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
  });


  const register = () => {
    if (window.Alpine) window.Alpine.data("energyRolesPanel", energyRolesPanel);
  };
  if (window.Alpine) register(); else document.addEventListener("alpine:init", register, {once: true});
})();

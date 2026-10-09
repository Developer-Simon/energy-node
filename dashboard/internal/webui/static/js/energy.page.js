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
    categories: {},
    groups: {},
    devicesList: [],
    iconCatalogue: [],

    init() {
      // Die Device Map speichert Rollen per PATCH. Ohne Neuladen haette diese
      // Seite den alten Stand und schriebe ihn beim naechsten Speichern zurueck.
      // Das eigene Speichern meldet sich ebenfalls, dann ist hier schon alles aktuell.
      window.addEventListener('energy-roles-changed', event => {
        if ((event.detail || {}).source !== ENERGY_PAGE_SOURCE) this.load();
      });
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

    async load() {
      this.loading = true;
      try {
        this.loadHiddenIds();
        const [, devices, saved, snapshot, icons] = await Promise.all([
          this.loadSession(),
          requestJSON('/api/v1/devices'),
          requestJSON('/api/v1/energy/roles'),
          requestJSON('/api/v1/energy'),
          requestJSON('/api/v1/device/icons').catch(() => []),
        ]);
        const savedAssignments = saved.assignments || {};
        const resolved = Object.fromEntries((snapshot.entities || []).map(item => [item.entity_id, item.role]));
        this.entities = devices
          .filter(device => device.id !== OWN_ENERGY_DEVICE_ID)
          .flatMap(device => device.entities
            .filter(entity => ['W', 'kW', '%'].includes(entity.unit_of_measurement))
            .map(entity => {
              const shortName = stripDevicePrefix(device.name, entity.name || entity.object_id);
              return {
                id: entity.unique_id,
                label: shortName ? `${device.name} / ${shortName}` : device.name,
                unit: entity.unit_of_measurement,
                assignment: savedAssignments[entity.unique_id] || resolved[entity.unique_id] || {},
              };
            }));
        this.assignments = Object.fromEntries(this.entities.map(entity => [entity.id, {
          role: entity.assignment.role || '',
          scale: entity.assignment.scale || 1,
          invert: Boolean(entity.assignment.invert),
          capacity_kwh: Number(entity.assignment.capacity_kwh) || 0,
        }]));
        if (saved.interpretation) this.interpretation = {...this.interpretation, ...saved.interpretation};
        this.categories = JSON.parse(JSON.stringify(saved.categories || {}));
        this.groups = JSON.parse(JSON.stringify(saved.groups || {}));
        this.devicesList = devices
          .filter(device => device.id !== OWN_ENERGY_DEVICE_ID)
          .map(device => ({id: device.id, name: device.name || device.id, manufacturer: device.manufacturer || '', icon: device.icon_name || device.suggested_icon || ''}));
        this.iconCatalogue = icons || [];
        this.unassigned = snapshot.unassigned || [];
        this.unassignedCount = snapshot.unassigned_count || 0;
        this.balanceTotal = (snapshot.balance && snapshot.balance.total) || 0;
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

    // Ohne zugeordnete Rolle "Hausverbrauch" gibt es nichts zu separieren -
    // der Modus verhaelt sich dann exakt wie "Berechnet".
    get showCombinedWithoutLoadWarning() {
      return this.interpretation.load_mode === 'combined' && !this.hasLoadRole;
    },

    // Eine ruhige Fusszeile direkt unter den Hausverbrauch-Kacheln: genau ein
    // Satz, der den aktiven Modus erklaert (frueher vier bedingte Hinweise im
    // Streifen oben). Echte Warnungen - kein "load"-Role zugeordnet - bleiben
    // im Hinweisstreifen, weil sie ein Problem melden, keine Erklaerung.
    get loadModeNote() {
      switch (this.interpretation.load_mode) {
        case 'measured':
          return t('energy.page.load_mode_note.measured');
        case 'calculated':
          return t('energy.page.load_mode_note.calculated');
        case 'combined':
          return t('energy.page.load_mode_note.combined');
        case 'auto':
          return this.hasLoadRole
            ? t('energy.page.load_mode_note.auto_measured')
            : t('energy.page.load_mode_note.auto_calculated');
        default:
          return '';
      }
    },

    get toleranceWEquivalent() {
      if (this.interpretation.gap_tolerance_mode === 'percent') {
        return Math.round((Number(this.interpretation.gap_tolerance_percent) || 0) / 100 * this.balanceTotal);
      }
      return Math.round(Number(this.interpretation.gap_tolerance_w) || 0);
    },

    roleOptionsFor(unit) {
      return unit === '%' ? this.socRoleOptions : this.powerRoleOptions;
    },

    isSocRow(id) {
      return this.assignments[id] && this.assignments[id].role === 'battery_soc';
    },

    loadHiddenIds() {
      try {
        this.hiddenIds = JSON.parse(localStorage.getItem(HIDDEN_IDS_STORAGE_KEY) || '{}');
      } catch (error) {
        this.hiddenIds = {};
      }
    },

    saveHiddenIds() {
      localStorage.setItem(HIDDEN_IDS_STORAGE_KEY, JSON.stringify(this.hiddenIds));
    },

    isHidden(id) {
      return Boolean(this.hiddenIds[id]);
    },

    toggleHidden(id) {
      if (this.hiddenIds[id]) delete this.hiddenIds[id];
      else this.hiddenIds[id] = true;
      this.saveHiddenIds();
    },

    get visibleEnergyEntities() {
      return this.entities.filter(entity => entity.unit !== '%' && !this.isHidden(entity.id));
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
      const assignments = Object.fromEntries(Object.entries(this.assignments)
        .map(([id, assignment]) => [id, assignment.role === 'battery_soc'
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
      return {assignments, interpretation, categories, groups};
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
        this.$store.toasts.push(t('energy.page.saved'));
        this.notifyRolesChanged();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.saving = false;
      }
    },
  });


  const register = () => {
    if (window.Alpine) window.Alpine.data("energyRolesPanel", energyRolesPanel);
  };
  if (window.Alpine) register(); else document.addEventListener("alpine:init", register, {once: true});
})();

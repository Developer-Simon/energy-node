(() => {
  const HIDDEN_IDS_STORAGE_KEY = 'energy-roles-hidden-ids';

  // Das Dashboard veröffentlicht seine eigene Bilanz als HA-MQTT-Gerät und
  // liest die Discovery zurück in die Registry (internal/energydiscovery,
  // DeviceIdentifier). Diese Rück-Entitäten dürfen im Rollen-Editor nicht als
  // zuweisbar erscheinen - sie würden die Bilanz doppelt zählen. Das Backend
  // hält sie schon aus energy.Aggregate heraus; die Zeilenliste hier stammt
  // aber direkt aus /api/v1/devices und braucht denselben Filter.
  const OWN_ENERGY_DEVICE_ID = 'energy-node-dashboard-energy';

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

  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json();
    if (!response.ok) throw new Error(body.message || "Anfrage fehlgeschlagen");
    return body;
  };

  const energyRolesPanel = () => ({
    entities: [],
    assignments: {},
    powerRoleOptions: [
      {value: 'pv', label: 'PV'},
      {value: 'battery', label: 'Batterie'},
      {value: 'battery_charge', label: 'Batterie laden'},
      {value: 'battery_discharge', label: 'Batterie entladen'},
      {value: 'grid', label: 'Netz'},
      {value: 'grid_import', label: 'Grid-Import / Netzbezug'},
      {value: 'grid_export', label: 'Grid-Export / Einspeisung'},
      {value: 'load', label: 'Hausverbrauch'},
      {value: 'wallbox', label: 'Wallbox'},
      {value: 'heat_pump', label: 'Wärmepumpe'},
    ],
    socRoleOptions: [
      {value: 'battery_soc', label: 'Batterie-Füllstand'},
    ],
    socWithoutCapacity: 0,
    hiddenIds: {},
    hiddenExpanded: false,
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

    async load() {
      this.loading = true;
      try {
        this.loadHiddenIds();
        const [devices, saved, snapshot] = await Promise.all([
          requestJSON('/api/v1/devices'),
          requestJSON('/api/v1/energy/roles'),
          requestJSON('/api/v1/energy'),
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
        this.unassigned = snapshot.unassigned || [];
        this.unassignedCount = snapshot.unassigned_count || 0;
        this.balanceTotal = (snapshot.balance && snapshot.balance.total) || 0;
        this.socWithoutCapacity = snapshot.battery_soc_without_capacity || 0;
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
          return 'Hausverbrauch kommt direkt von der Entität mit der Rolle „Hausverbrauch".';
        case 'calculated':
          return 'Hausverbrauch = PV + Netzbezug + Entladen − Einspeisung − Laden. „Übriger Verbrauch" ist dabei ein Rest, keine Messung — er nimmt jeden nicht separat erfassten Verbraucher auf.';
        case 'combined':
          return 'Berechnet, gemessene Verbraucher aber separat ausgewiesen. „Hausverbrauch" meint hier den gemessenen Teilverbraucher, nicht den Gesamtwert — ein Hauszähler auf dieser Rolle würde doppelt gezählt. „Übriger Verbrauch" bleibt ein Rest, keine Messung.';
        case 'auto':
          return this.hasLoadRole
            ? 'Gemessen, weil eine Entität die Rolle „Hausverbrauch" trägt — sonst würde berechnet.'
            : 'Keine Entität trägt die Rolle „Hausverbrauch", daher berechnet: PV + Netzbezug + Entladen − Einspeisung − Laden. „Übriger Verbrauch" ist dann ein Rest, keine Messung.';
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
      const count = this.socWithoutCapacity;
      return `${count} ${count === 1 ? 'Batterie' : 'Batterien'} ohne Kapazität — ${count === 1 ? 'sie geht' : 'sie gehen'} nicht in den Füllstand ein.`;
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
      return {assignments, interpretation};
    },

    revisionConfig() {
      return {
        basePath: '/api/v1/energy',
        current: () => this.payload(),
        reload: () => this.load(),
        label: 'Revisionen der Energie-Rollen',
      };
    },

    async save() {
      this.saving = true;
      const {assignments, interpretation} = this.payload();
      try {
        await requestJSON('/api/v1/energy/roles', {
          method: 'PUT',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({assignments, interpretation}),
        });
        this.$store.toasts.push('Energie-Rollen und Interpretation gespeichert.');
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

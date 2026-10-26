(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const view = window.__automationsView;
  const { canTest, conditionState, describeAction, describeCondition, formatSeconds, gateVerdict, historyActionText, historyResultInfo, meterFraction, meterScale } = view;
  const { ACTION_TYPES, CONDITION_TYPES, FIELD_HELP, WEEKDAY_LABELS, WINDOW_TYPES, SERVICE_TEST_POLL_MS, SERVICE_TEST_TIMEOUT_MS, isNumber, requestJSON } = view.constants;
  const { ROLE_GROUPS, actuatorCandidates, meterCandidates, switchCandidates, setpointCandidates, chipList, pickerItems, entityChip, balanceFieldCandidates, deviceRoleGroup } = window.__automationDevices;

  // Filter-Chips im Picker: gleiche Gruppen wie die Kacheln, Label wie dort.
  const ROLE_FILTER_LABEL_KEYS = {
    inverter: 'automations.devices.group.inverter',
    wallbox: 'automations.devices.group.wallbox',
    heat_pump: 'automations.devices.group.heat_pump',
    grid: 'automations.devices.group.grid',
    other: 'automations.devices.group.misc',
  };
  const roleFilters = () => ROLE_GROUPS.map((group) => ({ key: group.key, label: t(ROLE_FILTER_LABEL_KEYS[group.key]) }));

  // Art-Label einer Aktionskarte; der Picker nutzt dasselbe Label als Titel.
  const ACTION_KIND_LABEL_KEYS = {
    switch: 'automations.action_kind.switch',
    setpoint: 'automations.action_kind.setpoint',
    notify: 'automations.action_kind.notify',
    mqtt: 'automations.action_kind.mqtt',
  };

  // Ein gespeichertes Geraet, das im Katalog fehlt, bleibt als Kachel sichtbar.
  const unknownChip = (topic) => ({
    id: '',
    unknown: true,
    deviceName: t('automations.devices.unknown'),
    subtitle: topic || '',
    valueText: '',
    icon: 'ico-device',
    tone: '',
    fits: true,
  });

  // Ein fehlendes gespeichertes Geraet nimmt den ersten Platz ein.
  const withUnknown = (list, topic) => (list.missing ? [unknownChip(topic), ...list.chips] : list.chips);

  // Hysterese neuer Bilanz-Bedingungen, in der Einheit des Felds (meist W).
  const DEFAULT_HYSTERESIS = 5;

  const BALANCE_FIELDS_BASE = [
    ['grid_export', 'automations.metric.grid_export'],
    ['grid_import', 'automations.metric.grid_import'],
    ['pv', 'automations.metric.pv'],
    ['load_total', 'automations.metric.load_total'],
    ['base', 'automations.metric.base'],
    ['wallbox', 'automations.metric.wallbox'],
    ['heat_pump', 'automations.metric.heat_pump'],
    ['battery_charge', 'automations.metric.battery_charge'],
    ['battery_discharge', 'automations.metric.battery_discharge'],
    ['gap_applied', 'automations.metric.gap_applied'],
    ['autarkie', 'automations.metric.autarkie'],
    ['eigenverbrauch', 'automations.metric.eigenverbrauch'],
    ['battery_soc', 'automations.metric.battery_soc'],
    ['battery_capacity_kwh', 'automations.metric.battery_capacity_kwh'],
    ['battery_energy_kwh', 'automations.metric.battery_energy_kwh'],
  ];

  const RULE_LIMIT = 16;
  const SCENARIO_LIMIT = 8;
  const UNDO_MS = 10000;
  // Szenario-Diagramm: Geometrie wie im Entwurf, Punkte und Takte in Sekunden.
  const CHART_WIDTH = 600;
  const CHART_HEIGHT = 170;
  const CHART_MAX_POINTS = 300;
  const CHART_REFRESH_MS = 60000;
  const CATALOG_REFRESH_MS = 30000;
  const EXPORT_REFRESH_MS = 300000;
  const LIVE_WINDOW_S = 1800;
  const LIVE_MAX_POINTS = 180;
  const GRID_NOW_MAX_AGE_S = 120;
  const DEFAULT_RANGE = '30m';
  const SHOW_HISTORY_KEY = 'automations.show_history';
  const SPARK_WIDTH = 150;
  const SPARK_HEIGHT = 34;

  // Schalter "Verlaeufe anzeigen": ohne lesbaren Speicher gilt er nur fuer diese Sitzung.
  function readShowHistory() {
    try {
      return window.localStorage.getItem(SHOW_HISTORY_KEY) === '1';
    } catch (error) {
      return false;
    }
  }
  const DEFAULT_DEADBAND_W = 20;
  const GRID_SERIES = 'role:grid';
  // Wie weit der Bearbeiten-Modus fuer den Live-Balken ohne Dienstmeldung zurueckschaut.
  const EDIT_LIVE_WINDOW_MS = 5 * 60 * 1000;

  const outputSeriesName = (id) => `automation_scenario_${id}_output`;

  // Vorzeichen des Minidiagramms: Netz ist im Verlauf positiv beim Bezug, also
  // ist Einspeisung positiv und Bezug nie negativ.
  const ruleSparkValue = (field, avg) => {
    if (field === 'grid_export') return -avg;
    if (field === 'grid_import') return Math.max(0, avg);
    return avg;
  };

  // Zahl des Live-Balkens im Format der Seite: Watt ganz, sonst hoechstens eine Nachkommastelle.
  const formatLiveNumber = (value, unit) => {
    if (!window.I18n) return String(value);
    const rounded = unit === 'W' ? Math.round(value) : Math.round(value * 10) / 10;
    return window.I18n.formatNumber(rounded, Number.isInteger(rounded) ? 0 : 1);
  };

  // Vorzeichen wie im Entwurf: + Bezug, − Einspeisung.
  const signedW = (watts) => {
    const rounded = Math.round(watts);
    return `${rounded > 0 ? '+' : rounded < 0 ? '−' : ''}${Math.abs(rounded)}`;
  };

  const emptyDocument = () => ({
    version: 1,
    settings: { tick_interval_s: 10, settling_seconds: 60, balance_max_age_s: 30,
                history_limit: 10, history_persist: true,
                publish_allowed_prefixes: [], publish_allowed_prefixes_auto: false,
                publish_min_interval_s: 30, regulator_interval_s: 10 },
    rules: [],
    scenarios: [],
  });

  // Zustand des Vorlagen-Assistenten im Ruhezustand (Dialog zu).
  const assistantIdle = () => ({
    open: false, templateId: '', answers: {}, index: 0, name: '', query: '', category: 'all', context: null, effects: {},
    band: { tolOn: false, errors: null }, surplus: null,
  });
  // Zielband im Assistenten: Griff und Zustand leben ausserhalb der reaktiven Daten.
  let bandHandle = null;
  let bandState = null;
  // Preisskala der Niedrigtarif-Vorlage, ebenfalls ausserhalb der reaktiven Daten.
  let priceHandle = null;
  let priceState = null;
  const bandSigned = (value) => (value > 0 ? '+' : (value < 0 ? '\u2212' : '')) + window.I18n.formatNumber(Math.abs(value));
  const assistantTemplateList = () => (window.__automationTemplates ? window.__automationTemplates.TEMPLATES : []);
  // Sprite-Symbole der Bausteine: Geraet je Rolle, Kachel je Option.
  const ASSIST_DEVICE_ICONS = { inverter: 'ico-inverter', wallbox: 'ico-wallbox', switch: 'ico-switch' };
  const ASSIST_TILE_ICONS = {
    sun: 'ico-sun', min: 'ico-clock', battery: 'ico-battery', always: 'ico-play', fixed: 'ico-calendar',
    import: 'ico-grid', runtime: 'ico-clock', sunset: 'ico-sun', info: 'ico-info', warning: 'ico-warning', critical: 'ico-bell',
  };

  const automationsPanel = () => ({
    ...window.DevicePicker.mixin(),
    loading: false,
    saving: false,
    document: emptyDocument(),
    categories: {},
    entities: [],
    readableEntities: [],
    devices: [],
    assignments: {},
    runtimeState: null,
    online: false,
    csrfToken: '',
    expanded: {},
    flashRule: '',
    liveHistory: {},
    lastHistoryMessageAt: undefined,
    lastStateMessageAt: null,
    historyExpanded: {},
    editing: {},
    // Vorbelegte Live-Werte im Bearbeiten-Modus (Regel-ID -> Bedingungs-Index -> Wert), wenn der Dienst keinen meldet.
    editLive: {},
    // Stand einer Regel beim Start des Bearbeitens (ID -> Kopie), fuer Verwerfen.
    editSnapshot: {},
    savedDocument: null,
    // Unveraenderte Kopie der Antwort des Servers (vor allen Merges), Grundlage
    // des Nur-Bedienen-Speicherns; der Server vergleicht gegen genau diese Datei.
    storedDocument: null,
    lastStateAt: 0,
    hasAutomationsRole: false,
    // Rollen aus der Sitzung; null, solange keine Sitzung mit Rollenliste da ist.
    sessionRoles: null,
    get canEditAutomations() {
      return this.sessionRoles === null || this.sessionRoles.includes('automations');
    },
    get canOperateAutomations() {
      return this.sessionRoles === null || this.sessionRoles.includes('operate_automations');
    },
    nowTick: Date.now(),
    statusPollMs: 1000,
    locating: false,

    settingsOpen: false,
    // Reiter der Seitenschale: Szenarien oder Einzelregeln.
    view: 'rules',
    // Frisch angelegte Eintraege (ID -> true) bis zum Speichern.
    newIds: {},
    // Rueckgaengig-Balken nach dem Anlegen, verschwindet nach 10 Sekunden.
    undo: null,
    scenarioExpanded: {},
    scenarioEditing: {},
    // Diagramm je Szenario: Bereich (Start 30 min), gelesene Punkte, Live-Punkte
    // aus den Zustandsupdates, Zeitpunkt des letzten Ladens, Tagesexport in kWh.
    chartRange: {},
    chartPoints: {},
    ruleSparkPoints: {},
    livePoints: {},
    chartLoadedAt: {},
    todayExportKWh: null,
    todayExportAt: 0,
    showHistory: readShowHistory(),
    // Vorlagen-Assistent: Dialogzustand, nur solange er offen ist.
    assistant: assistantIdle(),

    get balanceFields() {
      return BALANCE_FIELDS_BASE.map(([field, key]) => [field, t(key)]);
    },

    // Eigene Kategorien als Bilanzfelder. Eine Regel, die auf eine inzwischen
    // geloeschte Kategorie zeigt, bleibt waehlbar, sonst wird das Select leer.
    get customBalanceFields() {
      const known = Object.entries(this.categories)
        .map(([id, category]) => [`custom:${id}`, category.label])
        .sort((a, b) => window.I18n.compare(a[1], b[1]));
      const used = new Set();
      (this.document.rules || []).forEach((rule) => {
        (rule.conditions || []).forEach((condition) => {
          if (condition.type === 'balance_threshold') used.add(condition.field);
        });
        (rule.actions || []).forEach((action) => {
          if (action.type === 'publish' && action.payload_source === 'balance') used.add(action.field);
        });
      });
      const unknown = [...used]
        .filter((field) => typeof field === 'string' && field.startsWith('custom:')
          && !(field.slice('custom:'.length) in this.categories))
        .sort()
        .map((field) => [field, t('automations.metric.custom_unknown', { id: field.slice('custom:'.length) })]);
      return [...known, ...unknown];
    },

    get publishAllowedPrefixesText() {
      return (this.document.settings.publish_allowed_prefixes || []).join('\n');
    },
    set publishAllowedPrefixesText(value) {
      this.document.settings.publish_allowed_prefixes = value.split('\n').map((line) => line.trim()).filter(Boolean);
    },

    async load() {
      this.loading = true;
      try {
        const [doc, session, , roles] = await Promise.all([
          requestJSON('/api/v1/configurations/automation_rules'),
          requestJSON('/api/v1/auth/session').catch(() => null),
          this.loadDeviceCatalog(),
          requestJSON('/api/v1/energy/roles').catch(() => null),
        ]);
        this.categories = (roles && roles.categories) || {};
        this.assignments = (roles && roles.assignments) || {};
        this.storedDocument = JSON.parse(JSON.stringify(doc && doc.rules ? doc : emptyDocument()));
        this.document = doc && doc.rules ? doc : emptyDocument();
        this.document.scenarios ||= [];
        this.view = this.document.scenarios.length ? 'scenarios' : 'rules';
        // Regler-Szenarien starten aufgeklappt; ein Klick des Nutzers bleibt erhalten.
        this.document.scenarios.forEach((scenario) => {
          if (scenario.while && scenario.while.mode === 'regulate') this.scenarioExpanded[scenario.id] ??= true;
        });
        this.newIds = {};
        this.undo = null;
        // Ein auf der Platte gespeichertes Dokument kann aelter sein als ein
        // seither neu hinzugekommenes settings-Feld (z.B. history_limit/
        // history_persist) - ohne diesen Merge wuerde ein fehlendes Feld im
        // Formular als "aus"/leer erscheinen, obwohl der Python-Dienst dafuer
        // seinen eigenen, oft anderen Default anwendet (Dataclass-Default ist
        // die echte Semantik von "nicht gesetzt", siehe AGENTS.md).
        this.document.settings = { ...emptyDocument().settings, ...this.document.settings };
        if (this.document.settings.publish_allowed_prefixes_auto) {
          // Der zuletzt gespeicherte Stand enthaelt die vom Dashboard
          // errechneten Topics - angezeigt wird trotzdem ein leeres Feld,
          // sonst wirkt der Default wie eine manuelle Eingabe.
          this.document.settings.publish_allowed_prefixes = [];
        }
        this.savedDocument = JSON.parse(JSON.stringify(this.document));
        if (window.__automationFocusRequest__) this.focusRule(window.__automationFocusRequest__);
        // dashboard.js schickt die Anfrage auch dann, wenn die Seite schon offen ist.
        if (!this._focusRuleListener) {
          this._focusRuleListener = true;
          window.addEventListener('automation-focus-rule', event => this.focusRule(event.detail && event.detail.id));
        }
        this.csrfToken = (session && session.csrf_token) || '';
        // "automations" liefert der Sitzungsendpunkt seit Task 3; ohne die
        // Rolle werden die Test-Knoepfe gar nicht erst angeboten.
        this.hasAutomationsRole = Boolean(session && session.automations);
        this.sessionRoles = session && 'roles' in session ? (Array.isArray(session.roles) ? session.roles : []) : null;
        this.loadRuntimeState();
        // dashboard.js verteilt jedes Registry-Update (SSE, mit
        // 30-Sekunden-Fallback) als 'registry-updated' auf window.
        if (!this._registryListener) {
          // Dieselbe Bremse, die publishRegistryUpdate und notifications.js
          // schon haben: ein Hintergrund-Tab braucht keinen Tick.
          this._registryListener = () => {
            if (document.visibilityState !== 'visible') return;
            this.refreshFromRegistry();
          };
          window.addEventListener('registry-updated', this._registryListener);
        }
        // "erfüllt seit" wird rein aus dem "since"-Zeitstempel und der
        // aktuellen Zeit berechnet - dafuer braucht es keine neuen Daten vom
        // Dienst, nur einen reaktiven Sekundentakt im Browser.
        if (!this._clockTimer) {
          this._clockTimer = setInterval(() => { this.nowTick = Date.now(); this.scenarioTick(); }, 1000);
          // Node hat hier ein Timeout-Objekt mit unref() (haelt den Prozess
          // z.B. in Tests nicht am Leben); im Browser ist es nur eine Zahl.
          if (typeof this._clockTimer.unref === 'function') this._clockTimer.unref();
        }
        this.scenarioTick();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.loading = false;
      }
    },

    flattenCommandableEntities(devices) {
      const result = [];
      for (const device of devices || []) {
        for (const entity of device.entities || []) {
          if (entity.command_topic) result.push({ ...entity, deviceName: device.name });
        }
      }
      return result;
    },

    commandableEntityGroups() {
      const groups = new Map();
      for (const entity of this.entities || []) {
        const name = entity.deviceName || t('automations.wizard.device_without_name');
        if (!groups.has(name)) groups.set(name, []);
        groups.get(name).push(entity);
      }
      return Array.from(groups.entries());
    },

    // Beim Speichern mit leerem Praefix-Feld eingesetzt (siehe save()): die
    // exakten command_topics aller bekannten steuerbaren Entitaeten, nicht
    // ein geratebasierter Wildcard-Praefix - neue Entitaeten an bekannten
    // Geraeten brauchen dafuer ein erneutes Speichern, genau wie neue Geraete.
    computeAutoPublishPrefixes() {
      const topics = new Set((this.entities || []).map((entity) => entity.command_topic).filter(Boolean));
      return Array.from(topics).sort();
    },

    flattenReadableEntities(devices) {
      const result = [];
      for (const device of devices || []) {
        for (const entity of device.entities || []) {
          // template_supported kommt aus Go - ob der Dienst den Wert lesen
          // kann, wird dort und nur dort entschieden.
          if (entity.state_topic && entity.template_supported) {
            result.push({ ...entity, deviceName: device.name });
          }
        }
      }
      return result;
    },

    entityForCondition(condition) {
      if (!condition || !condition.entity_id) return null;
      return (this.readableEntities || []).find((entity) => entity.unique_id === condition.entity_id) || null;
    },

    readableEntityGroups() {
      const groups = new Map();
      for (const entity of this.readableEntities || []) {
        const name = entity.deviceName || t('automations.wizard.device_without_name');
        if (!groups.has(name)) groups.set(name, []);
        groups.get(name).push(entity);
      }
      return Array.from(groups.entries());
    },

    computeConditionDrift(condition, entities) {
      if (!condition || !condition.entity_id) return { drifted: false, missing: false, currentTopic: null, currentTemplate: null };
      const entity = (entities || []).find((candidate) => candidate.unique_id === condition.entity_id);
      if (!entity) return { drifted: false, missing: true, currentTopic: null, currentTemplate: null };
      const currentTemplate = entity.value_template || '';
      const drifted = entity.state_topic !== condition.topic || currentTemplate !== (condition.value_template || '');
      return { drifted, missing: false, currentTopic: entity.state_topic, currentTemplate };
    },

    adoptConditionEntity(condition) {
      const entity = this.entityForCondition(condition);
      if (!entity) return;
      condition.topic = entity.state_topic;
      condition.value_template = entity.value_template || '';
    },

    addEntityValueCondition(rule) {
      rule.conditions.push({ type: 'entity_value', entity_id: '', topic: '', value_template: '',
                             comparison: 'below', value: 20, hold_seconds: 60 });
    },

    onConditionEntityChange(condition) {
      this.adoptConditionEntity(condition);
    },

    entityByUniqueId(uniqueId) {
      return (this.entities || []).find((entity) => entity.unique_id === uniqueId) || null;
    },

    automationDevice() {
      return (this.devices || []).find((device) => device.id === 'automation') || null;
    },

    // Der Geraetekatalog speist die Auswahlfelder und die Auto-Praefixe. Er
    // ist rein strukturell - command_topic, state_topic, template_supported,
    // unique_id, name - und aendert sich nur bei Discovery-Aenderungen, nie
    // im Sekundentakt. Deshalb wird er genau zweimal geholt: beim Oeffnen des
    // Tabs und vor dem Speichern. Bewusst getragene Einschraenkung: ein
    // waehrend geoeffnetem Tab neu entdecktes Geraet erscheint erst nach
    // einem Neuladen in den Dropdowns. Das nicht wieder mit einem Vollabruf
    // pro Tick zu "reparieren" ist der ganze Punkt - der saubere Ersatz ist
    // ?view=index aus dem zweiten Serverlast-Spec.
    async loadDeviceCatalog() {
      this.devices = (await requestJSON('/api/v1/devices')) || [];
      this.entities = this.flattenCommandableEntities(this.devices);
      this.readableEntities = this.flattenReadableEntities(this.devices);
    },

    loadRuntimeState() {
      this.applyRuntimeStateFrom(this.automationDevice());
    },

    // Nimmt das Geraet als Argument statt es aus this.devices zu suchen -
    // damit bedient dieselbe Funktion beide Wege: Erstladen aus der Liste,
    // Tick aus der Einzelroute.
    applyRuntimeStateFrom(automationDevice) {
      if (!automationDevice) {
        this.online = false;
        return;
      }
      const stateEntity = (automationDevice.entities || []).find((entity) => entity.object_id === 'state');
      // Die MQTT-Availability des "state"-Sensors ist zugleich das
      // online/offline-Signal des Dienstes - eine eigene "online"-Entity gibt
      // es im Register nicht.
      this.online = Boolean(stateEntity && stateEntity.available);
      // Das Dokument selbst liefert /api/v1/automation/state: last_message ist
      // auf 4096 Byte gekappt und teilt sich den Platz mit der Availability.
      // Der Zeitstempel zeigt nur an, dass seit dem letzten Abruf etwas auf
      // der Entity ankam.
      const stateAt = stateEntity && stateEntity.last_message && stateEntity.last_message.at;
      if (stateEntity && stateAt !== this.lastStateMessageAt) {
        this.lastStateMessageAt = stateAt;
        this.refreshState();
      }
      // Den Verlauf selbst liefert /api/v1/automation/history: last_message
      // ist auf 4096 Byte gekappt und teilt sich den Platz mit der
      // Availability, ein langer Verlauf kam dort abgeschnitten an. Der
      // Zeitstempel zeigt nur an, dass seit dem letzten Abruf etwas auf der
      // Entity ankam, damit nicht jeder Zustands-Tick den Verlauf neu holt.
      const historyEntity = (automationDevice.entities || []).find((entity) => entity.object_id === 'history');
      const historyAt = historyEntity && historyEntity.last_message && historyEntity.last_message.at;
      if (historyEntity && historyAt !== this.lastHistoryMessageAt) {
        this.lastHistoryMessageAt = historyAt;
        this.refreshHistory();
      }
    },

    async refreshHistory() {
      try {
        this.applyHistoryDocument(await requestJSON('/api/v1/automation/history'));
      } catch (error) {
        // voruebergehender Fehler, der naechste Tick versucht es erneut
        this.lastHistoryMessageAt = null;
      }
    },

    // Pro SSE-Tick wird genau ein Geraet geholt (10,9 KB statt 448 KB): der
    // Tick veraendert ausschliesslich den Laufzeitzustand der Automation.
    // Die Auswahlfelder haengen an loadDeviceCatalog() und bleiben stehen.
    async refreshFromRegistry() {
      try {
        const device = await requestJSON('/api/v1/devices/automation');
        this.applyRuntimeStateFrom(device);
      } catch (error) {
        // 404 = diese Instanz hat keinen Automations-Dienst; das ist derselbe
        // Zustand, den loadRuntimeState() aus einer Liste ohne das Geraet
        // ableitet. Alles andere ist voruebergehend - der naechste Tick
        // versucht es erneut und darf die Ansicht nicht leeren.
        if (error.status === 404) this.applyRuntimeStateFrom(null);
      }
    },

    async refreshState() {
      try {
        this.applyStateObject(await requestJSON('/api/v1/automation/state'));
      } catch (error) {
        // voruebergehender Fehler, der naechste Tick versucht es erneut
        this.lastStateMessageAt = null;
      }
    },

    applyStateObject(document) {
      // 204 (noch kein brauchbares Dokument) kommt als {} an und darf die
      // Ansicht so wenig leeren wie ein fremder Payload.
      if (!document || typeof document.rules !== 'object' || document.rules === null) return;
      this.runtimeState = document;
      this.lastStateAt = Date.now();
      this.recordLivePoints(document);
    },

    applyHistoryDocument(document) {
      // 204 (noch kein brauchbares Dokument) kommt als {} an und darf die
      // Ansicht so wenig leeren wie ein fremder Payload.
      if (!document || typeof document.rules !== 'object' || document.rules === null) return;
      this.liveHistory = document.rules;
    },

    ruleState(rule) {
      const rules = this.runtimeState && this.runtimeState.rules;
      return (rules && rules[rule.id]) || null;
    },

    conditionReport(rule, index) {
      const state = this.ruleState(rule);
      return (state && state.conditions && state.conditions[index]) || null;
    },

    conditionState(rule, index) {
      return this.conditionStateOf(this.conditionReport(rule, index));
    },

    // Zustand einer Bedingungszeile aus ihrem Bericht, null heisst noch kein Wert.
    conditionStateOf(report) {
      return window.__automationsView.conditionState(report);
    },

    // Klasse des Zustands: erfuellt, wartet (Haltezeit laeuft) oder offen.
    condStateClass(report) {
      const state = this.conditionStateOf(report);
      if (state === 'met') return 'is-met';
      if (state === 'pending') return 'is-pending';
      return 'is-open';
    },

    // Schluessel des Zustandsabzeichens passend zu condStateClass.
    condStateKey(report) {
      const tone = this.condStateClass(report);
      if (tone === 'is-met') return 'automations.rule.cond_state.met';
      if (tone === 'is-pending') return 'automations.rule.cond_state.pending';
      return 'automations.rule.cond_state.open';
    },

    describeCondition(condition) {
      return window.__automationsView.describeCondition(condition, this.entityForCondition(condition), this.categories);
    },

    describeAction(action) {
      return window.__automationsView.describeAction(action, this.entityByUniqueId(action.entity_id));
    },

    gateVerdict(rule) {
      return window.__automationsView.gateVerdict(this.ruleState(rule), this.online);
    },

    formatSeconds(seconds) {
      return window.__automationsView.formatSeconds(seconds);
    },

    // Stepper fuer Zahlenfelder: ein leeres Feld startet beim Standard, das
    // Ergebnis wird auf step gerundet und auf min/max geklemmt. Landet der Wert
    // wieder auf dem Standard, faellt der Schluessel weg, damit Standards nie
    // im Dokument stehen.
    stepValue(target, key, delta, { min, max, step = 1, fallback } = {}) {
      const raw = target[key];
      const empty = raw === undefined || raw === null || raw === '';
      const base = empty ? fallback : Number(raw);
      const start = Number.isFinite(base) ? base : 0;
      const decimals = (String(step).split('.')[1] || '').length;
      let next = Number((Math.round((start + delta * step) / step) * step).toFixed(decimals));
      if (min !== undefined && next < min) next = min;
      if (max !== undefined && next > max) next = max;
      if (next === fallback) {
        delete target[key];
      } else {
        target[key] = next;
      }
      return next;
    },

    conditionStateText(rule, index) {
      return this.conditionTextOf(this.conditionReport(rule, index), rule.conditions[index] || {});
    },

    // Statustext einer Bedingungszeile aus Bericht und Bedingung.
    conditionTextOf(report, condition) {
      const state = this.conditionStateOf(report);
      if (state === 'novalue' && condition.type === 'sun_window') {
        return this.hasLocation() ? t('automations.condition_state.sun_no_event') : t('automations.condition_state.no_location');
      }
      if (state === 'novalue') return t('automations.condition_state.no_value');
      if (state === 'unmet' && WINDOW_TYPES.has(condition.type)) return t('automations.condition_state.outside_window');
      if (state === 'unmet') return t('automations.condition_state.threshold_not_reached');
      const duration = report.since ? this.formatSeconds((this.nowTick / 1000) - report.since) : null;
      if (state === 'pending') {
        const key = duration ? 'automations.condition_state.pending_since' : 'automations.condition_state.pending';
        return t(key, { duration, remaining: this.formatSeconds(report.hold_remaining) });
      }
      if (duration) return t('automations.condition_state.met_since', { duration });
      return t('automations.condition_state.met');
    },

    // Liefert eine Liste von Textbausteinen, keine HTML-Zeichenkette - das
    // Template rendert sie ueber x-for/x-text, damit nichts ungeprueft als
    // HTML in die Seite gelangt.
    gateMeta(rule) {
      const state = this.ruleState(rule);
      if (!state) return [];
      const parts = [t('automations.gate_meta.cooldown', { remaining: this.formatSeconds(state.cooldown_remaining), total: this.formatSeconds(rule.cooldown_seconds) })];
      if (state.fired_at) parts.push(t('automations.gate_meta.last_fired', { time: window.I18n.formatTime(state.fired_at * 1000) }));
      parts.push(t('automations.gate_meta.fire_count', { count: state.fire_count || 0 }));
      return parts;
    },

    meterStyle(rule, index) {
      const view = window.__automationsView;
      const condition = rule.conditions[index];
      const report = this.conditionReport(rule, index);
      const scale = view.meterScale(condition, report, this.entityForCondition(condition));
      const value = report && typeof report.value === 'number' ? report.value : null;
      return { width: `${(view.meterFraction(value, scale) * 100).toFixed(1)}%` };
    },

    markStyle(rule, index) {
      const view = window.__automationsView;
      const condition = rule.conditions[index];
      const report = this.conditionReport(rule, index);
      const scale = view.meterScale(condition, report, this.entityForCondition(condition));
      const target = report && typeof report.target === 'number' ? report.target : null;
      if (target === null) return { display: 'none' };
      return { left: `${(view.meterFraction(target, scale) * 100).toFixed(1)}%` };
    },

    actionPreview(rule, index) {
      const state = this.ruleState(rule);
      return this.actionPreviewOf(state && state.actions && state.actions[index], rule.actions[index]);
    },

    // Vorschau einer Aktionszeile aus ihrem Vorschau-Eintrag, null heisst keine Vorschau.
    actionPreviewOf(preview, action) {
      if (!preview) return { blocked: false, text: t('automations.action_preview.no_preview') };
      if (preview.blocked) return { blocked: true, text: preview.reason || t('automations.action_preview.blocked') };
      if (action && action.type === 'notification') {
        return { blocked: false, text: `„${preview.title || ''}" — ${preview.message || ''}` };
      }
      return { blocked: false, text: `${preview.topic} → ${preview.payload}` };
    },

    isDirty(rule) {
      if (!this.savedDocument) return false;
      const saved = (this.savedDocument.rules || []).find((entry) => entry.id === rule.id);
      if (!saved) return true;
      return JSON.stringify(saved) !== JSON.stringify(rule);
    },

    // Der Schnappschuss entsteht nur beim Beginn, damit ein erneuter Klick auf
    // Bearbeiten die urspruengliche Fassung nicht ueberschreibt.
    startEditing(rule) {
      if (!this.canEditAutomations) return;
      if (!this.editing[rule.id]) {
        this.editSnapshot[rule.id] = JSON.parse(JSON.stringify(rule));
        this.loadEditLive(rule);
      }
      this.editing[rule.id] = true;
      this.expanded[rule.id] = true;
    },
    stopEditing(rule) {
      this.editing[rule.id] = false;
      const next = { ...this.editLive };
      delete next[rule.id];
      this.editLive = next;
    },

    // Vorbelegung der Live-Balken im Bearbeiten-Modus, nur fuer Bilanzbedingungen: der neueste
    // Rohwert der letzten fuenf Minuten, mit denselben Vorzeichen wie das Minidiagramm. Jeder
    // Fehler laesst den Balken bei "—", nie eine Ausnahme.
    async loadEditLive(rule) {
      const store = window.HistoryStore;
      const rollup = window.HistoryRollup;
      const values = {};
      if (store && rollup) {
        const now = Date.now();
        const from = now - EDIT_LIVE_WINDOW_MS;
        for (const [index, condition] of (rule.conditions || []).entries()) {
          const series = condition.type === 'balance_threshold' ? this.seriesForCondition(condition) : null;
          if (!series) continue;
          try {
            const raw = await store.readRange('raw', series, from, now);
            const newest = rollup.normalize(raw || [])
              .filter((row) => row.series === series && row.ts >= from && isNumber(row.avg))
              .sort((left, right) => right.ts - left.ts)[0];
            if (newest) values[index] = ruleSparkValue(condition.field, newest.avg);
          } catch (error) {
            // Ohne Vorbelegung bleibt der Balken bei "—".
          }
        }
      }
      this.editLive = { ...this.editLive, [rule.id]: values };
    },

    // Wert eines Balkens im Bearbeiten-Modus ohne Dienstmeldung: bei Bilanz die Vorbelegung,
    // bei Geraeten der aktuelle Wert der Entitaet, sofern er eine Zahl ist.
    editValueOf(rule, condition, index) {
      if (condition.type === 'balance_threshold') {
        const values = this.editLive[rule.id] || {};
        return isNumber(values[index]) ? values[index] : null;
      }
      const entity = this.entityForCondition(condition);
      if (!entity) return null;
      if (typeof entity.value === 'string' && entity.value.trim() === '') return null;
      if (typeof entity.value !== 'number' && typeof entity.value !== 'string') return null;
      const number = Number(entity.value);
      return Number.isFinite(number) ? number : null;
    },
    // Verwerfen setzt die Felder an Ort und Stelle zurueck, damit Alpine dieselbe Referenz behaelt.
    discardEditing(rule) {
      const snapshot = this.editSnapshot[rule.id];
      if (snapshot) {
        for (const key of Object.keys(rule)) delete rule[key];
        Object.assign(rule, JSON.parse(JSON.stringify(snapshot)));
      }
      this.stopEditing(rule);
    },
    // Collapsing closes the editor too.
    toggleExpanded(rule) {
      const collapsing = this.expanded[rule.id];
      this.expanded[rule.id] = !collapsing;
      if (collapsing) this.editing[rule.id] = false;
    },

    toggleScenarioExpanded(scenario) {
      const collapsing = this.scenarioExpanded[scenario.id];
      this.scenarioExpanded[scenario.id] = !collapsing;
      if (collapsing) this.scenarioEditing[scenario.id] = false;
      else this.loadScenarioChart(scenario);
    },

    setChartRange(scenario, range) {
      this.chartRange[scenario.id] = range;
      this.loadScenarioChart(scenario);
    },

    scenarioRange(id) {
      return this.chartRange[id] || DEFAULT_RANGE;
    },

    scenarioState(scenario) {
      const states = this.runtimeState && this.runtimeState.scenarios;
      return (states && states[scenario.id]) || null;
    },

    // Bericht einer Bedingungszeile der Start- (when) oder Endphase (until).
    scenarioConditionReport(scenario, block, index) {
      const state = this.scenarioState(scenario);
      const reports = state && state[block];
      return (Array.isArray(reports) && reports[index]) || null;
    },

    // Scope eines Szenario-Blocks fuer die gemeinsamen Zeilen-Editoren: der
    // Besitzer ist scenario[key] (als Getter, damit ein Moduswechsel greift),
    // block nennt den Bericht-Block (when, until) oder null bei Aktionen.
    scenarioRows(scenario, key, block) {
      const page = this;
      return {
        showCondTiles: false, showActTiles: false,
        rowsCanTest: false,
        get rowsOwner() { return scenario[key]; },
        rowsReport(index) { return block ? page.scenarioConditionReport(scenario, block, index) : null; },
      };
    },

    scenarioStatus(scenario) {
      return window.__automationScenario.scenarioStatus(scenario, this.scenarioState(scenario), this.online, this.nowTick / 1000);
    },

    // Symbol der Kachel: Wechselrichter bei Einspeisung, sonst das Symbol der ersten Startaktion.
    scenarioIcon(scenario) {
      const regulator = scenario.while && scenario.while.regulator;
      if (regulator) return regulator.direction === 'consumer' ? 'ico-flash' : 'ico-inverter';
      const first = ((scenario.on_start && scenario.on_start.actions) || [])[0];
      return first ? this.describeAction(first).icon : 'ico-power-on';
    },

    // Werte der Regler-Karte: Ziel, Toleranz, Grenzen und der aktuelle Stellwert.
    regulatorFigures(scenario) {
      const regulator = (scenario.while && scenario.while.regulator) || {};
      const output = regulator.output || {};
      const state = this.scenarioState(scenario);
      return {
        target: isNumber(regulator.target_w) ? regulator.target_w : 0,
        band: isNumber(regulator.deadband_w) ? regulator.deadband_w : DEFAULT_DEADBAND_W,
        min: isNumber(output.min) ? output.min : 0,
        max: isNumber(output.max) ? output.max : 0,
        output: this.online && state && isNumber(state.output) ? Math.round(state.output) : '—',
        // Verbraucher-Regler zeigen Stellwert und Verlauf in der Verbraucherfarbe.
        direction: regulator.direction === 'consumer' ? 'consumer' : 'producer',
      };
    },

    todayExportText() {
      return this.todayExportKWh === null ? '—' : window.I18n.formatNumber(this.todayExportKWh, 2);
    },

    // Die drei Phasen der Leiste mit Bezeichnung.
    // i18n-keys: automations.scenario.phase.name.start, automations.scenario.phase.name.while, automations.scenario.phase.name.end
    scenarioPhases(scenario) {
      const names = { start: 'automations.scenario.phase.name.start', while: 'automations.scenario.phase.name.while', end: 'automations.scenario.phase.name.end' };
      return window.__automationScenario
        .phaseSummary(scenario, (condition) => this.describeCondition(condition), (action) => this.describeAction(action))
        .map((row) => ({ ...row, label: t(names[row.phase]) }));
    },

    // Index der aktiven Phase in scenarioPhases, -1 wenn keine aktiv ist.
    activePhaseIndex(scenario) {
      if (!this.online) return -1;
      const phase = window.__automationScenario.activePhase(this.scenarioState(scenario), this.nowTick / 1000);
      return { start: 0, while: 1, end: 2 }[phase] ?? -1;
    },

    // Balken des Startfortschritts, nur solange die Karte wartet oder bereit ist.
    scenarioProgress(scenario) {
      if (!this.online) return null;
      const key = this.scenarioStatus(scenario).key;
      if (key !== 'waiting' && key !== 'ready') return null;
      const progress = window.__automationScenario.startProgress(scenario, this.scenarioState(scenario));
      return progress ? { percent: Math.round(progress.fraction * 100), label: progress.label } : null;
    },

    // Liest Rohwerte und Minutenwerte beider Serien aus dem Verlauf (IndexedDB),
    // fuellt die Luecken der Rohstufe mit Minutenwerten und duennt auf
    // CHART_MAX_POINTS aus. Jeder Fehler ergibt leere Serien, nie eine Ausnahme.
    async loadScenarioChart(scenario) {
      const id = scenario.id;
      const range = this.scenarioRange(id);
      this.chartLoadedAt[id] = Date.now();
      const store = window.HistoryStore;
      const rollup = window.HistoryRollup;
      const chart = window.__automationChart;
      let points = { grid: [], output: [] };
      if (store && rollup && chart) {
        try {
          const bounds = chart.rangeBounds(range, Date.now());
          const fromMs = bounds.from * 1000;
          const toMs = bounds.to * 1000;
          const readSeries = async (series) => {
            const raw = await store.readRange('raw', series, fromMs, toMs);
            const minute = await store.readRange('1m', series, fromMs, toMs);
            const { rows } = rollup.fillRawGaps(rollup.normalize(raw || []), rollup.normalize(minute || []), fromMs, toMs);
            const own = rows.filter((row) => row.series === series).sort((left, right) => left.ts - right.ts);
            // Der Chart arbeitet in Sekunden, der Verlauf speichert Millisekunden.
            return rollup.thin(own, CHART_MAX_POINTS).map((row) => ({ ts: row.ts / 1000, value: row.avg }));
          };
          points = { grid: await readSeries(GRID_SERIES), output: await readSeries(outputSeriesName(id)) };
        } catch (error) {
          points = { grid: [], output: [] };
        }
      }
      // Wechselt der Bereich waehrend des Lesens, gehoert das Ergebnis nicht mehr.
      if (this.scenarioRange(id) === range) this.chartPoints[id] = points;
    },

    // Haengt je Zustandsupdate einen Punkt an, mit Zahl in net und output.
    // Punkte aelter als 30 min fallen weg, es bleiben hoechstens LIVE_MAX_POINTS.
    recordLivePoints(document) {
      const nowSec = Date.now() / 1000;
      const cutoff = nowSec - LIVE_WINDOW_S;
      Object.entries(document.scenarios || {}).forEach(([id, state]) => {
        const point = { ts: nowSec };
        if (state && isNumber(state.net)) point.grid = state.net;
        if (state && isNumber(state.output)) point.output = state.output;
        const kept = (this.livePoints[id] || []).filter((entry) => entry.ts >= cutoff);
        if (point.grid !== undefined || point.output !== undefined) kept.push(point);
        this.livePoints[id] = kept.slice(-LIVE_MAX_POINTS);
      });
    },

    // Gelesene Punkte, danach die Live-Punkte, die juenger sind als der letzte gelesene.
    chartSeries(id, key) {
      const read = (this.chartPoints[id] && this.chartPoints[id][key]) || [];
      const lastRead = read.length ? read[read.length - 1].ts : -Infinity;
      const live = (this.livePoints[id] || [])
        .filter((entry) => isNumber(entry[key]) && entry.ts > lastRead)
        .map((entry) => ({ ts: entry.ts, value: entry[key] }));
      return [...read, ...live];
    },

    chartFor(scenario) {
      const chart = window.__automationChart;
      const regulator = (scenario.while && scenario.while.regulator) || {};
      const bounds = chart.rangeBounds(this.scenarioRange(scenario.id), this.nowTick);
      return chart.scenarioChart({
        grid: this.chartSeries(scenario.id, 'grid'),
        output: this.chartSeries(scenario.id, 'output'),
        target: isNumber(regulator.target_w) ? regulator.target_w : 0,
        deadband: isNumber(regulator.deadband_w) ? regulator.deadband_w : DEFAULT_DEADBAND_W,
        events: this.liveHistory[scenario.id] || [],
        from: bounds.from,
        to: bounds.to,
        width: CHART_WIDTH,
        height: CHART_HEIGHT,
      });
    },

    // Netz jetzt: der Zustand, sonst der letzte Netzpunkt, wenn er juenger als 2 min ist.
    gridNowText(scenario) {
      const net = this.gridNetNow(scenario);
      return net === null ? '—' : signedW(net);
    },

    // Netz als Zahl in W oder null, gleiche Quelle wie gridNowText.
    gridNetNow(scenario) {
      if (!this.online) return null;
      const state = this.scenarioState(scenario);
      if (state && isNumber(state.net)) return state.net;
      const grid = this.chartSeries(scenario.id, 'grid');
      const last = grid[grid.length - 1];
      if (last && this.nowTick / 1000 - last.ts < GRID_NOW_MAX_AGE_S) return last.value;
      return null;
    },

    // Schalter in der Kopfleiste. Beim Einschalten laden alle Regler-Szenarien
    // ihr Diagramm, auch die zugeklappten, damit die Minidiagramme Daten haben.
    setShowHistory(on) {
      this.showHistory = !!on;
      try {
        window.localStorage.setItem(SHOW_HISTORY_KEY, on ? '1' : '0');
      } catch (error) {
        // ohne Speicher bleibt der Schalter bis zum Neuladen erhalten
      }
      if (!on) return Promise.resolve();
      const scenarios = (this.document && this.document.scenarios) || [];
      const regulators = scenarios.filter((scenario) => scenario.while && scenario.while.mode === 'regulate');
      const rules = (this.document && this.document.rules) || [];
      const sparks = rules.filter((rule) => this.ruleSeries(rule) !== null).map((rule) => this.loadRuleSpark(rule));
      return Promise.all([...regulators.map((scenario) => this.loadScenarioChart(scenario)), ...sparks]);
    },

    // Minidiagramm der zugeklappten Regler-Zeile: Netzverlauf der letzten 30 min mit Toleranzband.
    sparklineFor(scenario) {
      const regulator = this.regulatorOf(scenario);
      const bounds = window.__automationChart.rangeBounds('30m', this.nowTick);
      return window.__automationChart.sparkline({
        points: this.chartSeries(scenario.id, 'grid'),
        target: isNumber(regulator.target_w) ? regulator.target_w : 0,
        deadband: isNumber(regulator.deadband_w) ? regulator.deadband_w : DEFAULT_DEADBAND_W,
        from: bounds.from,
        to: bounds.to,
        width: SPARK_WIDTH,
        height: SPARK_HEIGHT,
      });
    },

    // Erste Bilanzbedingung der Regel, sonst null.
    ruleSparkCondition(rule) {
      return (rule.conditions || []).find((condition) => condition.type === 'balance_threshold') || null;
    },

    // Serie des Verlaufs fuer das Feld der ersten Bilanzbedingung, sonst null.
    // Eigene Kategorien heissen role:custom:<basis>:<id>, die Basis kommt aus den Kategorien.
    ruleSeries(rule) {
      return this.seriesForCondition(this.ruleSparkCondition(rule));
    },

    // Serie des Verlaufs fuer das Feld einer Bilanzbedingung, sonst null.
    seriesForCondition(condition) {
      if (!condition || typeof condition.field !== 'string') return null;
      const roles = new Map([
        ['grid_import', 'role:grid'], ['grid_export', 'role:grid'], ['pv', 'role:pv'],
        ['load_total', 'role:load'], ['wallbox', 'role:wallbox'], ['heat_pump', 'role:heat_pump'],
        ['battery_soc', 'role:battery_soc'], ['battery_charge', 'role:battery'], ['battery_discharge', 'role:battery'],
      ]);
      if (roles.has(condition.field)) return roles.get(condition.field);
      if (condition.field.startsWith('custom:')) {
        const id = condition.field.slice('custom:'.length);
        const category = this.categories && this.categories[id];
        return category && category.base ? `role:custom:${category.base}:${id}` : null;
      }
      return null;
    },

    // Liest 30 min Verlauf der Serie der Regel (Rohwerte und Minutenwerte), wie loadScenarioChart.
    // Jeder Fehler ergibt keine Punkte, nie eine Ausnahme.
    async loadRuleSpark(rule) {
      const series = this.ruleSeries(rule);
      const field = (this.ruleSparkCondition(rule) || {}).field;
      const store = window.HistoryStore;
      const rollup = window.HistoryRollup;
      const chart = window.__automationChart;
      let points = [];
      if (series && store && rollup && chart) {
        try {
          const bounds = chart.rangeBounds('30m', Date.now());
          const fromMs = bounds.from * 1000;
          const toMs = bounds.to * 1000;
          const raw = await store.readRange('raw', series, fromMs, toMs);
          const minute = await store.readRange('1m', series, fromMs, toMs);
          const { rows } = rollup.fillRawGaps(rollup.normalize(raw || []), rollup.normalize(minute || []), fromMs, toMs);
          const own = rows.filter((row) => row.series === series).sort((left, right) => left.ts - right.ts);
          points = rollup.thin(own, CHART_MAX_POINTS).map((row) => ({ ts: row.ts / 1000, value: ruleSparkValue(field, row.avg) }));
        } catch (error) {
          points = [];
        }
      }
      this.ruleSparkPoints[rule.id] = points;
    },

    // Minidiagramm der Regel: die Schwelle als Linie, ohne Toleranzband.
    ruleSparkFor(rule) {
      const condition = this.ruleSparkCondition(rule) || {};
      const bounds = window.__automationChart.rangeBounds('30m', this.nowTick);
      return window.__automationChart.sparkline({
        points: this.ruleSparkPoints[rule.id] || [],
        target: isNumber(condition.threshold) ? condition.threshold : 0,
        deadband: 0,
        from: bounds.from,
        to: bounds.to,
        width: SPARK_WIDTH,
        height: 30,
      });
    },

    // Kopfzeile des Editors: Status der Regel, dazu die letzte Auslösung, wenn es eine gibt.
    ruleEditStatus(rule) {
      const status = this.gateVerdict(rule).label;
      const marks = this.ruleTriggers(rule).marks;
      if (!marks.length) return status;
      return t('automations.rule.edit.status_last', { status, time: marks[0].label });
    },

    // Kurzstatus der Regel fuer die Listenzeile: erfuellt, wartet, Sperrzeit,
    // nicht erfuellt, Fehler oder aus. Aus gilt vor allem anderen.
    ruleShortStatus(rule) {
      if (!rule.enabled) return { tone: 'disabled', label: t('automations.gate.short.disabled') };
      const verdict = this.gateVerdict(rule);
      const keys = { ok: 'automations.gate.short.ok', wait: 'automations.gate.short.wait', info: 'automations.gate.short.info', off: 'automations.gate.short.off', bad: 'automations.gate.short.bad' };
      return { tone: verdict.tone, label: t(keys[verdict.tone] || 'automations.gate.short.off') };
    },

    // Vorschau des Toleranzbands fuer das Regler-Formular.
    bandPreviewFor(scenario) {
      const regulator = this.regulatorOf(scenario);
      return window.__automationChart.bandPreview({
        target: isNumber(regulator.target_w) ? regulator.target_w : 0,
        deadband: isNumber(regulator.deadband_w) ? regulator.deadband_w : DEFAULT_DEADBAND_W,
        net: this.gridNetNow(scenario),
      });
    },

    // Tagesexport aus den Tagesstatistik. Ein Fehler laesst den alten Wert stehen.
    async refreshTodayExport() {
      try {
        const stats = await requestJSON('/api/v1/energy/stats?period=day');
        const kwh = stats && stats.total && stats.total.kwh && stats.total.kwh.grid_export;
        if (isNumber(kwh)) this.todayExportKWh = kwh;
      } catch (error) {
        // voruebergehender Fehler, der naechste Takt versucht es erneut
      }
    },

    // Takt des Sekundentimers: aufgeklappte Karten alle 60 s neu laden, der
    // Tagesexport alle 5 min, nur bei sichtbarer Seite und mit mindestens einem Regler.
    scenarioTick() {
      this.syncCatalogRefresh();
      if (document.visibilityState !== 'visible') return;
      const nowMs = Date.now();
      const scenarios = this.document.scenarios || [];
      scenarios.forEach((scenario) => {
        if (this.scenarioExpanded[scenario.id] && nowMs - (this.chartLoadedAt[scenario.id] || 0) >= CHART_REFRESH_MS) {
          this.loadScenarioChart(scenario);
        }
      });
      const hasRegulator = scenarios.some((scenario) => scenario.while && scenario.while.mode === 'regulate');
      if (hasRegulator && nowMs - this.todayExportAt >= EXPORT_REFRESH_MS) {
        this.todayExportAt = nowMs;
        this.refreshTodayExport();
      }
    },

    // Zaehlt, was beim naechsten Speichern anders ist: neue, geaenderte und
    // geloeschte Regeln und Szenarien sowie ein Eintrag fuer die Einstellungen.
    unsavedCount() {
      if (!this.savedDocument) return 0;
      const saved = this.savedDocument;
      const countEntries = (kind) => {
        const current = this.document[kind] || [];
        const before = saved[kind] || [];
        const changed = current.filter((entry) => {
          const old = before.find((candidate) => candidate.id === entry.id);
          return !old || JSON.stringify(old) !== JSON.stringify(entry);
        }).length;
        const removed = before.filter((entry) => !current.some((candidate) => candidate.id === entry.id)).length;
        return changed + removed;
      };
      const settingsChanged = JSON.stringify(saved.settings) !== JSON.stringify(this.document.settings) ? 1 : 0;
      return countEntries('rules') + countEntries('scenarios') + settingsChanged;
    },

    canAddRule() { return (this.document.rules || []).length < RULE_LIMIT; },
    canAddScenario() { return (this.document.scenarios || []).length < SCENARIO_LIMIT; },

    // Die Device Map verweist auf eine Regel: aufklappen, zur Regel scrollen
    // und sie kurz hervorheben.
    focusRule(ruleId) {
      window.__automationFocusRequest__ = null;
      if (!ruleId) return;
      this.expanded[ruleId] = true;
      this.flashRule = ruleId;
      setTimeout(() => { if (this.flashRule === ruleId) this.flashRule = ''; }, 1600);
      const scroll = () => {
        const node = document.querySelector(`[data-rule-id="${CSS.escape(ruleId)}"]`);
        if (node) node.scrollIntoView({behavior: window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'start'});
      };
      if (this.$nextTick) this.$nextTick(scroll); else scroll();
    },

    testState(rule, index) {
      const preview = this.actionPreview(rule, index);
      const base = window.__automationsView.canTest({
        online: this.online, dirty: this.isDirty(rule), hasRole: this.hasAutomationsRole,
      });
      if (!base.allowed) return base;
      if (preview.blocked) {
        return { allowed: true, reason: t('automations.test.blocked_action') };
      }
      return base;
    },

    confirmTest(rule, index) {
      const action = rule.actions[index];
      if (!action || action.type !== 'publish') return { required: false, question: '' };
      const preview = this.actionPreview(rule, index);
      return {
        required: true,
        question: t('automations.test.confirm', { action: preview.text }),
      };
    },

    async awaitTestResult(ruleId, actionIndex, sentAt, options = {}) {
      const timeoutMs = options.timeoutMs || SERVICE_TEST_TIMEOUT_MS;
      const pollMs = options.pollMs || SERVICE_TEST_POLL_MS;
      const deadline = Date.now() + timeoutMs;
      // Das Ergebnis-Topic ist retained. Ein Ergebnis zaehlt nur, wenn es
      // juenger ist als die eigene Anfrage - sonst wuerde das letzte Ergebnis
      // vom Vortag als Antwort durchgehen.
      // Wie bei "state" (siehe loadRuntimeState) hat test_result einen
      // value_template ({{ value_json.status }}); das Go-Registry rendert
      // damit .value auf den blossen Status-String (z.B. "published") -
      // kein gueltiges JSON. Das volle Dokument mit rule_id/action_index/at
      // steckt nur im rohen last_message.payload.
      const sentAtSeconds = sentAt / 1000;
      while (Date.now() < deadline) {
        const automationDevice = this.automationDevice();
        const entity = automationDevice
          && (automationDevice.entities || []).find((candidate) => candidate.object_id === 'test_result');
        const rawDocument = entity && entity.last_message && entity.last_message.payload;
        if (rawDocument) {
          try {
            const result = JSON.parse(rawDocument);
            if (result.rule_id === ruleId && result.action_index === actionIndex && result.at >= sentAtSeconds) {
              return result;
            }
          } catch (error) {
            // unvollstaendiges Dokument - beim naechsten Durchlauf erneut versuchen
          }
        }
        await new Promise((resolve) => setTimeout(resolve, pollMs));
        await this.refreshFromRegistry();
      }
      return null;
    },

    async testAction(rule, index, options = {}) {
      const permission = this.testState(rule, index);
      if (!permission.allowed) return;
      if (!options.skipConfirm) {
        const question = this.confirmTest(rule, index);
        if (question.required) {
          const confirmed = await this.$store.modal.confirm({
            title: question.question,
            confirmLabel: t('automations.test.trigger'),
          });
          if (!confirmed) return;
        }
      }
      const sentAt = Date.now();
      try {
        await requestJSON('/api/v1/automations/test', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken }, // i18n-ignore
          body: JSON.stringify({ rule_id: rule.id, action_index: index }),
        });
      } catch (error) {
        this.notify(t('automations.test.failed', { reason: error.message }), 'critical');
        return;
      }
      const result = await this.awaitTestResult(rule.id, index, sentAt, options);
      if (!result) {
        this.notify(t('automations.test.no_response'), 'warning');
        return;
      }
      if (result.status === 'published') {
        this.notify(t('automations.test.published', { topic: result.topic, payload: result.payload }), 'info');
      } else if (result.status === 'blocked') {
        this.notify(t('automations.test.blocked', { reason: result.reason }), 'warning');
      } else {
        this.notify(t('automations.test.error', { reason: result.reason }), 'critical');
      }
    },

    async testRule(rule) {
      const results = [];
      for (let i = 0; i < (rule.actions || []).length; i++) {
        results.push(await this.testAction(rule, i));
      }
      return results;
    },

    notify(message, severity) {
      this.$store.toasts.push(message, severity);
    },

    addRule() {
      if (!this.canAddRule()) {
        this.notify(t('automations.limit.rules', { max: RULE_LIMIT }), 'warning');
        return;
      }
      const id = `regel_${Date.now()}`;
      this.insertEntry('rule', { id, name: t('automations.new_rule'), enabled: true, history_enabled: true, cooldown_seconds: 60, conditions: [], actions: [] });
    },

    addScenario() {
      if (!this.canAddScenario()) {
        this.notify(t('automations.limit.scenarios', { max: SCENARIO_LIMIT }), 'warning');
        return;
      }
      const id = `szenario_${Date.now()}`;
      this.insertEntry('scenario', window.__automationScenario.emptyScenario(id, t('automations.new_scenario')));
    },

    // Vorlagen-Assistent: Zustand, Schrittfolge und Anlegen. Das Markup folgt
    // in den Teilauftraegen 5b/5c.
    async openAssistant() {
      const active = document.activeElement;
      this.assistantOpener = active && active !== document.body ? active : document.querySelector('.auto-assist-open');
      const [, energy] = await Promise.all([
        this.loadDeviceCatalog(),
        requestJSON('/api/v1/energy').catch(() => null),
      ]);
      const snapshot = energy || { values: {}, entities: [] };
      this.assistant = {
        ...assistantIdle(),
        open: true,
        context: window.__automationWizard.buildContext({ devices: this.devices, snapshot, document: this.document }),
      };
      const dialog = this.$refs.assistantDialog;
      if (dialog && typeof dialog.showModal === 'function' && !dialog.open) dialog.showModal();
      this.attachAssistantSheet();
      const search = this.$refs.assistantSearch;
      if (search && typeof search.focus === 'function') search.focus();
    },

    // Schliesst den Dialog (Escape, Klick daneben, Schliessen-Knopf) und verwirft
    // den Entwurf. Angelegt wird nur in assistantCreate().
    closeAssistant() {
      const dialog = this.$refs.assistantDialog;
      this.detachAssistantSheet();
      const opener = this.assistantOpener;
      this.assistantOpener = null;
      const finish = () => {
        if (dialog && typeof dialog.close === 'function' && dialog.open) dialog.close();
        this.destroyAssistantBand();
        this.destroyAssistantPrice();
        this.assistant = assistantIdle();
        if (opener && typeof opener.focus === 'function') opener.focus();
      };
      const slides = typeof window.matchMedia === 'function'
        && window.matchMedia('(max-width: 899.98px) and (prefers-reduced-motion: no-preference)').matches;
      if (dialog && dialog.open && slides) {
        // Auf dem Telefon faehrt das Blatt nach unten weg, dann schliesst der Dialog.
        let done = false;
        const once = () => {
          if (done) return;
          done = true;
          dialog.classList.remove('is-closing');
          finish();
        };
        dialog.style.transform = '';
        dialog.classList.add('is-closing');
        dialog.addEventListener('animationend', (event) => { if (event.target === dialog) once(); }, { once: true });
        setTimeout(once, 480);
        return;
      }
      finish();
    },

    // Blatt unter 900 px: der Griff folgt dem Finger und schliesst, sonst federt es zurueck.
    attachAssistantSheet() {
      this.detachAssistantSheet();
      const dialog = this.$refs.assistantDialog;
      const grab = this.$refs.assistantGrab;
      const sheet = window.__automationSheet;
      const phone = typeof window.matchMedia === 'function' && window.matchMedia('(max-width: 899.98px)').matches;
      if (!sheet || !dialog || !grab || !phone) return;
      this.assistantDetach = sheet.attach(grab, dialog, { onClose: () => this.closeAssistant() });
    },

    detachAssistantSheet() {
      if (typeof this.assistantDetach === 'function') this.assistantDetach();
      this.assistantDetach = null;
    },

    assistantTemplate() {
      const id = this.assistant.templateId;
      if (!id) return null;
      return assistantTemplateList().find((template) => template.id === id) || null;
    },

    // Suchtreffer fuer Galerie: passt zu den Geraeten, und die uebrigen mit Grund.
    assistantTemplates() {
      const { query, category, context } = this.assistant;
      if (!context) return { fitting: [], others: [] };
      return window.__automationWizard.searchTemplates(assistantTemplateList(), { query, category }, context);
    },

    // Geraete fuer einen device-Schritt, mit Sperre und Grund aus dem Kontext.
    assistantDevices(step) {
      if (!step || !this.assistant.context) return [];
      return window.__automationWizard.devicesForStep(step, this.assistant.context, this.assistant.answers);
    },

    startTemplate(id) {
      const template = assistantTemplateList().find((item) => item.id === id);
      if (!template || !this.assistant.context) return;
      this.assistant.templateId = id;
      this.assistant.answers = window.__automationWizard.defaultAnswers(template, this.assistant.context);
      this.assistant.name = t(`automations.templates.${id}.title`);
      this.assistant.index = 0;
      this.assistant.effects = {};
      this.assistant.band = { tolOn: false, errors: null };
      this.refreshEffect(this.assistantStep());
    },

    // Leere Regel oder leeres Szenario: wie addRule/addScenario, der Dialog schliesst
    // nur, wenn wirklich angelegt wurde.
    startBlank(kind) {
      const full = kind === 'scenario' ? !this.canAddScenario() : !this.canAddRule();
      if (kind === 'scenario') this.addScenario();
      else this.addRule();
      if (!full) this.closeAssistant();
    },

    // Sichtbare Schritte samt Zusammenfassung; leer ohne gewaehlte Vorlage.
    assistantSteps() {
      const template = this.assistantTemplate();
      if (!template) return [];
      return window.__automationWizard.visibleSteps(template, this.assistant.answers, this.assistant.context);
    },

    assistantStep() {
      return this.assistantSteps()[this.assistant.index] || null;
    },

    assistantCanAdvance() {
      const step = this.assistantStep();
      if (!step) return false;
      return window.__automationWizard.stepComplete(step, this.assistant.answers[step.id]);
    },

    assistantNext() {
      if (!this.assistantCanAdvance()) return;
      if (this.assistant.index >= this.assistantSteps().length - 1) return;
      this.assistant.index += 1;
      this.refreshEffect(this.assistantStep());
    },

    // Aus dem ersten Schritt zurueck in die Galerie, sonst einen Schritt zurueck.
    assistantBack() {
      if (!this.assistant.templateId) return;
      if (this.assistant.index === 0) {
        this.destroyAssistantBand();
        this.destroyAssistantPrice();
        this.assistant = { ...this.assistant, templateId: '', answers: {}, name: '', effects: {}, band: { tolOn: false, errors: null } };
        return;
      }
      this.assistant.index -= 1;
      this.refreshEffect(this.assistantStep());
    },

    // Springt zu einem Schritt, per ID oder mit dem Schritt selbst (Knopf "aendern").
    assistantGoTo(step) {
      const stepId = typeof step === 'string' ? step : (step && step.id);
      const index = this.assistantSteps().findIndex((item) => item.id === stepId);
      if (index < 0) return;
      this.assistant.index = index;
      this.refreshEffect(this.assistantStep());
    },

    // Wirkung eines Voreinstellungswerts aus den Vortagsdaten; eine inzwischen
    // geaenderte Antwort ueberschreibt den Text nicht.
    async refreshEffect(step) {
      if (!step || step.type !== 'presets' || !step.effect) return;
      if (step.effect === 'band') {
        await this.loadBandErrors();
        return;
      }
      if (step.effect === 'surplus') {
        await this.loadSurplus();
        return;
      }
      if (step.effect === 'price') {
        this.assistantPriceSync();
        return;
      }
      const value = this.assistant.answers[step.id];
      const text = await window.__automationWizard.presetEffect(step, value, this.assistant.context, window.HistoryStore);
      if (this.assistant.open && this.assistant.answers[step.id] === value) {
        this.assistant.effects[step.id] = text;
      }
    },

    // Gestrige Minutenwerte fuer das Zielband, einmal je Assistent. Ein Fehler ergibt keine Daten.
    async loadBandErrors() {
      const { band } = this.assistant;
      if (!band || band.errors !== null) return;
      let errors = [];
      try {
        const [from, to] = window.__automationWizard.yesterdayRange();
        const rows = await window.HistoryStore.readRange('1m', 'role:grid', from, to);
        errors = window.__automationBand.errorsFromRows(rows);
      } catch (error) {
        errors = [];
      }
      band.errors = errors;
      if (bandHandle) bandHandle.redraw();
    },

    // Gestrige Viertelstunden des Überschusses für die Vorschau, einmal je Assistent.
    // Ohne Daten bleibt die Reihe leer, dann zeigt die Vorschau nichts.
    async loadSurplus() {
      if (!this.assistant.open || this.assistant.surplus !== null) return;
      let series = [];
      try {
        const [from, to] = window.__automationWizard.yesterdayRange();
        const rows = await window.HistoryStore.readRange('1m', 'role:grid', from, to);
        series = window.__automationWizard.quarterSurplus(rows);
      } catch (error) {
        series = [];
      }
      if (this.assistant.open) this.assistant.surplus = series;
    },

    // Vorschau des Startwerts: Kurve und Schwelle aus den Antworten, '' ohne Daten.
    assistantSurplusMarkup() {
      const step = this.assistantStep();
      if (!step || step.effect !== 'surplus' || !Array.isArray(this.assistant.surplus)) return '';
      const wizard = window.__automationWizard;
      const threshold = this.assistant.answers[step.id];
      return wizard.surplusMarkup(this.assistant.surplus, threshold, {
        start: t('automations.assistant.surplus.start_at', { value: window.I18n.formatNumber(threshold) }),
        aria: t('automations.assistant.surplus.aria'),
        text: wizard.surplusText(this.assistant.surplus, threshold),
      });
    },

    // Abzeichen und Text einer Kachel, je nach Gerätekontext der Antworten.
    assistantBadge(step, option) {
      return window.__automationWizard.optionBadge(option, this.assistant.context, this.assistant.answers);
    },

    assistantTileText(step, option) {
      const field = window.__automationWizard.optionTextField(option, this.assistant.context, this.assistant.answers);
      return this.assistantOptionText(step, option, field);
    },

    // Zieht das Band neu: Ziel und Toleranz aus den Antworten, Toleranz ohne Schalter 20 W.
    assistantBandSync() {
      if (!bandHandle || !bandState) return;
      const { answers, band } = this.assistant;
      bandState.target = answers.target;
      bandState.tol = band.tolOn ? answers.tolerance : 20;
      bandHandle.redraw();
    },

    // Hinweis unter dem Schritt, etwa der Ladehinweis; '' wenn der Schritt keinen hat.
    assistantStepHint() {
      const step = this.assistantStep();
      if (!step || typeof step.hint !== 'function') return '';
      return step.hint(this.assistant.answers, this.assistant.context) || '';
    },

    // Aktueller Preis des gewaehlten Messwerts in ct/kWh, null ohne Wert.
    assistantPriceNow() {
      const prices = (this.assistant.context && this.assistant.context.prices) || [];
      const entry = prices.find((item) => item.id === this.assistant.answers.price);
      const value = entry && entry.entity ? entry.entity.value : undefined;
      if (value === undefined || value === null || value === '') return null;
      const number = Number(value);
      return Number.isFinite(number) ? number : null;
    },

    // Satz unter der Preisskala: Jetzt-Preis gegen die Schwelle, '' ohne Preis.
    assistantPriceText() {
      const step = this.assistantStep();
      if (!step || step.effect !== 'price') return '';
      return window.__automationWizard.priceNowText(this.assistantPriceNow(), this.assistant.answers[step.id]);
    },

    // Zeichnet die Skala mit der Schwelle aus den Antworten neu, nach Wahl oder Stepper.
    assistantPriceSync() {
      if (!priceHandle || !priceState) return;
      priceState.thr = this.assistant.answers.thr;
      priceHandle.redraw();
    },

    // Legt die Preisskala in den SVG-Knoten des Schritts. Eine vorherige Skala wird vorher aufgeloest.
    mountAssistantPrice() {
      this.destroyAssistantPrice();
      const svg = this.$refs.assistPrice;
      if (!svg || !window.__automationBand) return;
      const page = this;
      priceState = { thr: this.assistant.answers.thr };
      priceHandle = window.__automationBand.mountPriceScale(svg, {
        state: priceState,
        now: () => page.assistantPriceNow(),
        labels: {
          threshold: (value) => t('automations.assistant.price.threshold', { value: window.I18n.formatNumber(value) }),
          valueText: (value) => t('automations.assistant.price.value_text', { value: window.I18n.formatNumber(value) }),
        },
        onChange: (state) => { page.assistant.answers.thr = state.thr; },
      });
    },

    destroyAssistantPrice() {
      if (priceHandle) priceHandle.destroy();
      priceHandle = null;
      priceState = null;
    },

    // Legt das Band in den SVG-Knoten des Schritts. Ein vorheriges Band wird vorher aufgeloest.
    mountAssistantBand() {
      this.destroyAssistantBand();
      const svg = this.$refs.assistBand;
      if (!svg || !window.__automationBand) return;
      const page = this;
      const { answers, band } = this.assistant;
      bandState = { target: answers.target, tol: band.tolOn ? answers.tolerance : 20 };
      bandHandle = window.__automationBand.mountBand(svg, {
        state: bandState,
        tolEditable: () => page.assistant.band.tolOn,
        errors: () => (page.assistant.band.errors || []),
        labels: {
          feedIn: t('automations.assistant.band.feed_in'),
          import: t('automations.assistant.band.import'),
          axis: t('automations.assistant.band.axis'),
          axisShort: t('automations.assistant.band.axis_short'),
          target: (value) => t('automations.assistant.band.target', { value: bandSigned(value) }),
          targetTol: (value, tol) => t('automations.assistant.band.target_tol', { value: bandSigned(value), tol }),
          valueText: (value, tol) => (page.assistant.band.tolOn
            ? t('automations.assistant.band.target_tol', { value: bandSigned(value), tol })
            : t('automations.assistant.band.target', { value: bandSigned(value) })),
        },
        onChange: (state) => {
          page.assistant.answers.target = Math.round(state.target);
          if (page.assistant.band.tolOn) page.assistant.answers.tolerance = Math.round(state.tol);
        },
      });
    },

    destroyAssistantBand() {
      if (bandHandle) bandHandle.destroy();
      bandHandle = null;
      bandState = null;
    },

    // Schalter "Toleranz anpassen": aus setzt die Toleranz zurueck auf 20 W.
    assistantBandToggleTolerance() {
      const { answers, band } = this.assistant;
      band.tolOn = !band.tolOn;
      if (!band.tolOn) answers.tolerance = 20;
      this.assistantBandSync();
    },

    // Preset-Karte: Feder vom aktuellen Wert aus, ohne Schalter die Toleranz 20 W.
    assistantBandPreset(value) {
      const { answers, band } = this.assistant;
      if (!band.tolOn) answers.tolerance = 20;
      if (bandHandle && bandState) {
        if (!band.tolOn) bandState.tol = 20;
        bandHandle.setTarget(value);
      } else {
        answers.target = value;
      }
    },

    // Fusshinweis des Zielbands: das Netz bei diesem Wert halten.
    assistantBandHint() {
      return t('automations.assistant.band.hold', { value: bandSigned(this.assistant.answers.target) });
    },

    // Kennzahlen des Bands fuer die drei Kacheln, null ohne Daten.
    assistantBandStats() {
      const { answers, band } = this.assistant;
      if (!window.__automationBand) return null;
      const stats = window.__automationBand.bandStats(band.errors || [], answers.target, answers.tolerance ?? 20);
      if (!stats) return null;
      return {
        fed: window.I18n.formatNumber(stats.fedKwh, 2),
        drawn: window.I18n.formatNumber(stats.drawnKwh, 2),
        moves: window.I18n.formatNumber(stats.movesPerHour, 1),
      };
    },

    // Der Eintrag der Vorlage mit dem eingegebenen Namen, oder null ohne gueltige Antworten.
    assistantBuild() {
      const template = this.assistantTemplate();
      const { answers, context, name } = this.assistant;
      if (!template || !context) return null;
      let built;
      try {
        built = template.build(answers, context);
      } catch (error) {
        return null;
      }
      if (name && name.trim() !== '') built.entry.name = name.trim();
      return built;
    },

    assistantFull(kind) {
      return kind === 'scenario' ? !this.canAddScenario() : !this.canAddRule();
    },

    // Zeilen der Zusammenfassung: Phase, Text und der Schritt zum Aendern.
    assistantSummary() {
      const template = this.assistantTemplate();
      if (!template || !template.summary) return [];
      return template.summary(this.assistant.answers, this.assistant.context);
    },

    // Vorschau des ersten Grenzwerts, nur bei Reglern.
    assistantPreview() {
      const built = this.assistantBuild();
      if (!built || built.kind !== 'scenario') return '';
      return window.__automationWizard.regulatorPreview(built.entry, this.assistant.context);
    },

    assistantListFull() {
      const built = this.assistantBuild();
      return !!built && this.assistantFull(built.kind);
    },

    // Legt die Vorlage an: ungespeichert, aktiv, mit Rueckgaengig. Bei voller Liste
    // passiert nichts, der Dialog bleibt offen.
    assistantCreate() {
      const built = this.assistantBuild();
      if (!built || this.assistantFull(built.kind)) return;
      this.insertEntry(built.kind, built.entry);
      this.closeAssistant();
    },

    // Anzeige-Helfer des Assistenten-Markups: Texte aus dem Katalog, Symbole,
    // Sperren mit Grund und Voreinstellungen.
    assistantCopy(template, path) {
      return template ? t(`automations.templates.${template.id}.${path}`) : '';
    },

    assistantStepText(step, field) {
      if (!step) return '';
      if (step.id === 'summary') return t('automations.assistant.summary.short');
      return this.assistantCopy(this.assistantTemplate(), `steps.${step.id}.${field}`);
    },

    assistantOptionText(step, option, field) {
      return this.assistantCopy(this.assistantTemplate(), `steps.${step.id}.options.${option.id}.${field}`);
    },

    assistantCategories() {
      return ['all', ...window.__automationWizard.CATEGORIES];
    },

    assistantCategoryLabel(category) {
      return t(`automations.assistant.category.${category}`);
    },

    assistantAvailability(template) {
      return window.__automationWizard.templateAvailability(template, this.assistant.context);
    },

    assistantReason(step, option) {
      return window.__automationWizard.optionUnavailable(option, this.assistant.context, this.assistant.answers);
    },

    assistantDeviceIcon(step) {
      const role = typeof step.filter === 'string' ? step.filter : step.filter.role;
      return ASSIST_DEVICE_ICONS[role] || 'ico-device';
    },

    assistantTileIcon(option) {
      return ASSIST_TILE_ICONS[option.id] || 'ico-flash';
    },

    // Waehlt einen Wert; bei Voreinstellungen wird die Wirkung neu berechnet.
    assistantPick(step, value) {
      window.__automationWizard.chooseOption(step, this.assistant.answers, value);
      if (step.effect === 'band') this.assistantBandSync();
      this.refreshEffect(step);
    },

    // Mehrfachauswahl eines devices-Schritts: ID an- oder abschalten.
    assistantToggleDevice(step, id) {
      this.assistant.answers[step.id] = window.__automationWizard.toggleDevice(this.assistant.answers[step.id], id);
    },

    assistantPicked(step, id) {
      const list = this.assistant.answers[step.id];
      return Array.isArray(list) && list.includes(id);
    },

    // Option mit reveal, die gerade gewaehlt ist, sonst null.
    assistantRevealOption(step) {
      if (!step || !step.options) return null;
      const chosen = step.options.find((option) => option.id === this.assistant.answers[step.id]);
      return chosen && chosen.reveal ? chosen : null;
    },

    // Texte eines numbers-Feldes aus dem Katalog.
    assistantFieldText(step, field, key) {
      return this.assistantCopy(this.assistantTemplate(), `steps.${step.id}.fields.${field.id}.${key}`);
    },

    // Grenzen und Schritt eines Stepper-Feldes, auch fuer reveal-Werte.
    assistantFieldOpts(field) {
      return { min: field.min, max: field.max, step: field.step || 1 };
    },

    assistantIsCustom(step) {
      return !step.options.some((option) => option.id === this.assistant.answers[step.id]);
    },

    // Eigener Wert: der naechste Wert ueber dem aktuellen, der keine Voreinstellung ist.
    // Schrittweite wie beim Stepper, bei Nachkommastellen etwa 0,5.
    assistantCustom(step) {
      const increment = step.step || 5;
      const decimals = window.__automationWizard.decimalsOf(increment);
      let value = Number(this.assistant.answers[step.id]) || 0;
      while (step.options.some((option) => option.id === value)) {
        value = Number((value + increment).toFixed(decimals));
      }
      this.assistantPick(step, value);
    },

    // Schaltet einen Wochentag (0 bis 6) in einer Liste um, etwa bei Zeit und Mehrfachwahl.
    assistantToggleDay(holder, key, day) {
      const days = holder[key] || [];
      holder[key] = days.includes(day) ? days.filter((item) => item !== day) : [...days, day].sort();
    },

    assistantBackLabel() {
      if (this.assistant.index === 0) return t('automations.assistant.gallery');
      return this.assistantStepText(this.assistantSteps()[this.assistant.index - 1], 'short');
    },

    assistantNextLabel() {
      return this.assistantStepText(this.assistantSteps()[this.assistant.index + 1], 'short');
    },

    // Gemeinsamer Kern fuer neue Regeln und Szenarien: anlegen, als neu
    // markieren, aufklappen, Editor oeffnen, Reiter wechseln, Rueckgaengig anbieten.
    insertEntry(kind, entry) {
      if (kind === 'scenario') {
        this.document.scenarios.push(entry);
        this.scenarioExpanded[entry.id] = true;
        this.scenarioEditing[entry.id] = true;
        this.view = 'scenarios';
      } else {
        this.document.rules.push(entry);
        this.expanded[entry.id] = true;
        this.editing[entry.id] = true;
        this.view = 'rules';
      }
      this.newIds[entry.id] = true;
      this.armUndo({
        label: kind === 'scenario' ? t('automations.undo.scenario_added') : t('automations.undo.rule_added'),
        kind,
        id: entry.id,
      });
      return entry;
    },

    armUndo(undo) {
      clearTimeout(this._undoTimer);
      this.undo = undo;
      this._undoTimer = setTimeout(() => { this.undo = null; }, UNDO_MS);
      if (this._undoTimer && this._undoTimer.unref) this._undoTimer.unref();
    },

    // Entfernt den zuletzt angelegten Eintrag per ID, auch wenn er inzwischen
    // bearbeitet wurde.
    undoInsert() {
      const undo = this.undo;
      if (!undo) return;
      if (undo.kind === 'scenario') {
        this.document.scenarios = this.document.scenarios.filter((entry) => entry.id !== undo.id);
        delete this.scenarioExpanded[undo.id];
        delete this.scenarioEditing[undo.id];
      } else {
        this.document.rules = this.document.rules.filter((entry) => entry.id !== undo.id);
        delete this.expanded[undo.id];
        delete this.editing[undo.id];
      }
      delete this.newIds[undo.id];
      clearTimeout(this._undoTimer);
      this.undo = null;
    },

    async removeScenario(scenarioId) {
      const scenario = this.document.scenarios.find((entry) => entry.id === scenarioId);
      const confirmed = await this.$store.modal.confirm({
        title: t('automations.delete_scenario.title', { name: (scenario && scenario.name) || scenarioId }),
        body: t('automations.delete_scenario.body'),
        confirmLabel: t('common.delete'),
        danger: true,
      });
      if (!confirmed) return;
      this.document.scenarios = this.document.scenarios.filter((entry) => entry.id !== scenarioId);
      if (this.undo && this.undo.id === scenarioId) this.undo = null;
    },

    // Der Loeschknopf traegt seit dem Umbau nur noch ein Icon - ein
    // Fehlgriff ist dadurch leichter, also fragt er nach. Endgueltig weg ist
    // die Regel trotzdem erst mit dem naechsten Speichern.
    async removeRule(ruleId) {
      const rule = this.document.rules.find((entry) => entry.id === ruleId);
      const confirmed = await this.$store.modal.confirm({
        title: t('automations.delete_rule.title', { name: (rule && rule.name) || ruleId }),
        body: t('automations.delete_rule.body'),
        confirmLabel: t('common.delete'),
        danger: true,
      });
      if (!confirmed) return;
      this.document.rules = this.document.rules.filter((entry) => entry.id !== ruleId);
    },

    toggleEnabled(ruleId) {
      const rule = this.document.rules.find((entry) => entry.id === ruleId);
      if (rule) rule.enabled = !rule.enabled;
    },

    conditionTypes() { return CONDITION_TYPES.map((type) => ({ ...type, label: t(type.label), hint: t(type.hint), short: t(type.short) })); },
    actionTypes() { return ACTION_TYPES.map((type) => ({ ...type, label: t(type.label), hint: t(type.hint), short: t(type.short) })); },
    fieldHelp(key) { return FIELD_HELP[key] ? t(FIELD_HELP[key]) : ''; },

    addBalanceCondition(rule) {
      rule.conditions.push({ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 800, hysteresis: DEFAULT_HYSTERESIS, hold_seconds: 300 });
    },

    addTimeWindowCondition(rule) {
      rule.conditions.push({ type: 'time_window', start: '08:00', end: '18:00', weekdays: [] });
    },

    addSunWindowCondition(rule) {
      rule.conditions.push({ type: 'sun_window', from: 'sunrise', from_offset_min: 0,
                             to: 'sunset', to_offset_min: 0, weekdays: [] });
    },

    weekdayLabels() { return WEEKDAY_LABELS.map((key) => t(key)); },

    hasWeekday(condition, day) {
      return (condition.weekdays || []).includes(day);
    },

    toggleWeekday(condition, day) {
      const days = new Set(condition.weekdays || []);
      if (days.has(day)) days.delete(day);
      else days.add(day);
      condition.weekdays = [...days].sort((a, b) => a - b);
    },

    hasLocation() {
      const settings = this.document.settings || {};
      return isNumber(settings.latitude) && isNumber(settings.longitude);
    },

    needsLocation(rule) {
      return (rule.conditions || []).some((condition) => condition.type === 'sun_window') && !this.hasLocation();
    },

    // Die Einstellungen liegen im eingeklappten Fortgeschrittenen-Bereich.
    // Der Hinweis an der Sonnenzeit-Karte klappt ihn auf und springt hin.
    openLocationSettings() {
      this.settingsOpen = true;
      // Scroll only after the collapse has rendered, the field is hidden until then.
      const focusField = () => {
        const field = window.document.getElementById('automations-latitude');
        if (field) {
          field.scrollIntoView({ block: 'center' });
          field.focus();
        }
      };
      if (this.$nextTick) this.$nextTick(focusField); else focusField();
    },

    // Der Browser gibt die Position nur in einem sicheren Kontext heraus:
    // HTTPS (z. B. ueber Caddy) oder localhost. Unter reinem HTTP bleibt der
    // Knopf aus und die Felder werden von Hand ausgefuellt.
    geolocationAvailable() {
      return Boolean(window.isSecureContext && window.navigator && window.navigator.geolocation);
    },

    async useCurrentLocation() {
      if (!this.geolocationAvailable() || this.locating) return;
      this.locating = true;
      try {
        const position = await new Promise((resolve, reject) => {
          window.navigator.geolocation.getCurrentPosition(resolve, reject,
            { enableHighAccuracy: false, timeout: 15000, maximumAge: 600000 });
        });
        // Drei Nachkommastellen sind rund 100 m - fuer Sonnenzeiten mehr als
        // genug, und die gespeicherte Adresse bleibt unscharf.
        const round = (value) => Math.round(value * 1000) / 1000;
        this.document.settings.latitude = round(position.coords.latitude);
        this.document.settings.longitude = round(position.coords.longitude);
      } catch (error) {
        const denied = error && error.code === 1;
        this.$store.toasts.push(denied
          ? t('automations.geolocation.denied')
          : t('automations.geolocation.failed'), 'critical');
      } finally {
        this.locating = false;
      }
    },

    // Ein Dispatcher statt drei Knoepfen im Template: die Kacheln kommen aus
    // CONDITION_TYPES, also muss auch das Anlegen ueber denselben Schluessel
    // gehen. Unbekannter Schluessel legt bewusst nichts an.
    addCondition(rule, key) {
      let result;
      if (key === 'balance') result = this.addBalanceCondition(rule);
      if (key === 'entity') result = this.addEntityValueCondition(rule);
      if (key === 'time') result = this.addTimeWindowCondition(rule);
      if (key === 'sun') result = this.addSunWindowCondition(rule);
      this.loadEditLive(rule);
      return result;
    },

    // "entity" oeffnet den Geraete-Picker, die Aktion entsteht erst mit der Wahl.
    addAction(rule, key) {
      if (key === 'notify') return void rule.actions.push(this.buildNotificationAction());
      if (key === 'mqtt') return void rule.actions.push(this.buildMqttAction());
      if (key === 'entity') return this.addDeviceAction(rule);
    },

    // Neue Geraete-Aktion: Schalter und Sollwerte in einem Picker. Ein Schalter
    // startet auf Ein, ein Sollwert mit dem aktuellen Wert, sonst dem Minimum.
    async addDeviceAction(rule) {
      const seen = new Set();
      const candidates = [...switchCandidates(this.devices, this.assignments), ...setpointCandidates(this.devices, this.assignments)]
        .filter((candidate) => !seen.has(candidate.id) && seen.add(candidate.id));
      const uniqueId = await this.openPicker({
        title: t('automations.action.device.pick_title'),
        items: pickerItems(candidates),
        mode: 'single',
        filters: roleFilters(),
      });
      const entity = uniqueId && this.entityByUniqueId(uniqueId);
      if (!entity) return null;
      let action;
      if (entity.payload_on !== undefined && entity.payload_on !== null && entity.payload_on !== '') {
        action = this.buildSwitchAction(entity, 'on');
      } else {
        const current = window.__automationDevices.toNumber(entity.value);
        action = this.buildSetpointAction(entity, { mode: 'constant', value: current ?? entity.min_value });
      }
      rule.actions.push(action);
      return action;
    },

    removeCondition(rule, index) {
      rule.conditions.splice(index, 1);
    },

    buildSwitchAction(entity, targetState) {
      return {
        type: 'publish', topic: entity.command_topic, retain: false, payload_source: 'constant',
        payload: targetState === 'on' ? entity.payload_on : entity.payload_off,
        entity_id: entity.unique_id,
      };
    },

    buildSetpointAction(entity, source) {
      const action = {
        type: 'publish', topic: entity.command_topic, retain: false,
        payload_source: source.mode, entity_id: entity.unique_id,
        min: entity.min_value, max: entity.max_value, step: entity.step,
      };
      if (source.mode === 'balance') {
        action.field = source.field;
        action.scale = source.scale ?? 1;
        action.offset = source.offset ?? 0;
      } else {
        action.payload = String(source.value ?? '');
      }
      return action;
    },

    buildToggleAction(entity) {
      return {
        type: 'publish', topic: entity.command_topic, retain: false, payload_source: 'toggle',
        source_topic: entity.state_topic, value_template: entity.value_template || '',
        payload_on: entity.payload_on, payload_off: entity.payload_off, entity_id: entity.unique_id,
      };
    },

    buildMqttAction() {
      return { type: 'publish', topic: '', retain: false, payload_source: 'constant', payload: '' };
    },

    buildNotificationAction() {
      return { type: 'notification', severity: 'info', title: '', message: '' };
    },

    // Entitaet hinter einer Publish-Aktion: ueber entity_id, sonst ueber das
    // Topic (aeltere Aktionen und reine MQTT-Aktionen auf ein Geraete-Topic).
    actionEntity(action) {
      if (!action || action.type !== 'publish') return null;
      if (action.entity_id) return this.entityByUniqueId(action.entity_id);
      if (!action.topic) return null;
      return (this.entities || []).find((entity) => entity.command_topic === action.topic) || null;
    },

    // Art einer Aktion fuer die Karte: switch, setpoint, notify oder mqtt.
    // Schalter erkennt man am Umschalt-Modus oder an Ein/Aus-Payloads der
    // Entitaet, Sollwerte an den Grenzen min/max der Aktion oder der Entitaet.
    // MQTT bleibt nur, wenn kein Geraet hinter dem Topic steht.
    actionKind(action) {
      if (!action) return 'mqtt';
      if (action.type === 'notification') return 'notify';
      const entity = this.actionEntity(action);
      if (action.type === 'publish' && (entity || action.entity_id)) {
        const hasPayload = action.payload !== undefined && action.payload !== '';
        const isOnOff = Boolean(entity) && hasPayload
          && (action.payload === entity.payload_on || action.payload === entity.payload_off);
        if (action.payload_source === 'toggle' || isOnOff) return 'switch';
        if (isNumber(action.min) && isNumber(action.max)) return 'setpoint';
        if (entity && isNumber(entity.min_value) && isNumber(entity.max_value)) return 'setpoint';
      }
      return 'mqtt';
    },

    // Grenzen eines Sollwerts: aus der Aktion, sonst aus der Entitaet.
    actionBounds(action) {
      const entity = this.actionEntity(action) || {};
      return {
        min: isNumber(action.min) ? action.min : entity.min_value,
        max: isNumber(action.max) ? action.max : entity.max_value,
        step: isNumber(action.step) ? action.step : (isNumber(entity.step) ? entity.step : 1),
        unit: entity.unit_of_measurement || '',
      };
    },

    // Kachel des Geraets einer Aktion mit Rolle und aktuellem Wert.
    actionChip(action) {
      const entity = this.actionEntity(action);
      return (entity && entityChip(this.devices, this.assignments, entity.unique_id)) || unknownChip(action && action.topic);
    },

    actionKindLabel(action) {
      return t(ACTION_KIND_LABEL_KEYS[this.actionKind(action)]);
    },

    // Schaltet eine Schalt-Aktion auf Ein, Aus oder Umschalten. Die Entitaet
    // bleibt, die Aktion wird ueber die Builder neu aufgebaut.
    setSwitchState(owner, index, state) {
      const action = owner.actions[index];
      const entity = this.actionEntity(action);
      if (!entity) return;
      const next = state === 'toggle' ? this.buildToggleAction(entity) : this.buildSwitchAction(entity, state);
      owner.actions.splice(index, 1, next);
    },

    // Geraetewahl fuer Schalter und Sollwert. Der Picker zeigt nur die zur Art
    // passenden Entitaeten; Art sowie Umschalt- bzw. Quellmodus bleiben erhalten.
    async chooseActionDevice(owner, index) {
      const action = owner.actions[index];
      const kind = this.actionKind(action);
      if (kind !== 'switch' && kind !== 'setpoint') return;
      const candidates = kind === 'switch'
        ? switchCandidates(this.devices, this.assignments)
        : setpointCandidates(this.devices, this.assignments);
      const uniqueId = await this.openPicker({
        title: t(ACTION_KIND_LABEL_KEYS[kind]),
        items: pickerItems(candidates),
        mode: 'single',
        filters: roleFilters(),
        selected: this.actionEntity(action) ? [this.actionEntity(action).unique_id] : [],
      });
      if (!uniqueId) return;
      const entity = this.entityByUniqueId(uniqueId);
      if (!entity) return;
      if (kind === 'switch') {
        const previous = this.actionEntity(action);
        const wasOff = Boolean(previous) && action.payload === previous.payload_off;
        const next = action.payload_source === 'toggle'
          ? this.buildToggleAction(entity)
          : this.buildSwitchAction(entity, wasOff ? 'off' : 'on');
        owner.actions.splice(index, 1, next);
        return;
      }
      const source = action.payload_source === 'balance'
        ? { mode: 'balance', field: action.field ?? 'pv', scale: action.scale, offset: action.offset }
        : { mode: 'constant', value: action.payload };
      owner.actions.splice(index, 1, this.buildSetpointAction(entity, source));
    },

    // Quelle eines Sollwerts: fester Wert oder Messwert aus der Bilanz. Die
    // Entitaet und ihre Grenzen bleiben unveraendert.
    setSetpointSource(action, mode) {
      if (mode === 'balance') {
        action.payload_source = 'balance';
        action.field = action.field || 'pv';
        action.scale = action.scale ?? 1;
        action.offset = action.offset ?? 0;
        delete action.payload;
        return;
      }
      action.payload_source = 'constant';
      action.payload = String(action.payload ?? '');
      delete action.field;
      delete action.scale;
      delete action.offset;
    },

    // Ton der Sollwert-Karte: Rollenfarbe des Geraets, sonst neutral.
    actionTone(action) {
      return this.actionChip(action).tone || '';
    },

    // Zahlenfeld des Sollwert-Steppers. Das Payload bleibt Text (so verlangt es
    // der Dienst), der Stepper rechnet mit Zahlen.
    setpointValueTarget(action) {
      return {
        get value() {
          const payload = action.payload;
          return payload === undefined || payload === null || payload === '' ? '' : Number(payload);
        },
        set value(next) {
          const number = Number(next);
          action.payload = next === '' || next === null || next === undefined || !Number.isFinite(number) ? '' : String(number);
        },
      };
    },

    removeAction(rule, index) {
      rule.actions.splice(index, 1);
    },

    computeDrift(action, entities) {
      if (!action.entity_id) return { drifted: false, currentTopic: null };
      const entity = (entities || []).find((candidate) => candidate.unique_id === action.entity_id);
      if (!entity || entity.command_topic === action.topic) return { drifted: false, currentTopic: entity ? entity.command_topic : null };
      return { drifted: true, currentTopic: entity.command_topic };
    },

    badgeLabel(result) {
      // Use badge keys where the text differs from gate verdicts (E8 requirement)
      const badgeMap = {
        fired: 'automations.history_result.fired',
        conditions_not_met: 'automations.gate.conditions_not_met',
        hold_pending: 'automations.gate.hold_pending',
        cooldown: 'automations.badge.cooldown',
        settling: 'automations.badge.settling',
        balance_stale: 'automations.badge.balance_stale',
        blocked: 'automations.history_result.blocked',
        disabled: 'automations.badge.disabled',
        error: 'automations.history_result.error',
      };
      const key = badgeMap[result];
      return key ? t(key) : result;
    },

    // Kleinster Abstand fuer Befehle, vom Dienst mit dem Standard 30 s vorgegeben.
    minPublishInterval() {
      const configured = this.document.settings && this.document.settings.publish_min_interval_s;
      return isNumber(configured) ? configured : 30;
    },

    hasToggleAction(rule) {
      return (rule.actions || []).some((action) => action.type === 'publish' && action.payload_source === 'toggle');
    },

    // Icon der Kachel: die erste Aktion bestimmt sie, ohne Aktion ein Blitz.
    ruleIcon(rule) {
      const first = (rule.actions || [])[0];
      return first ? this.describeAction(first).icon : 'ico-flash';
    },

    // Kurzvorschau der zugeklappten Regel: Bedingungen mit ihrem Live-Zustand,
    // dann die Aktionen, jeweils nur als Icon.
    rulePreview(rule) {
      return {
        conditions: (rule.conditions || []).map((condition, index) => ({
          icon: this.describeCondition(condition).icon,
          state: this.conditionState(rule, index),
        })),
        actions: (rule.actions || []).map((action) => ({ icon: this.describeAction(action).icon })),
      };
    },

    // Live-Balken fuer jede Zahlen-Bedingung (Bilanz, Entitaet ueber/unter).
    liveBars(rule) {
      return (rule.conditions || [])
        .map((condition, index) => {
          const numeric = condition.type === 'balance_threshold'
            || (condition.type === 'entity_value' && (condition.comparison === 'above' || condition.comparison === 'below'));
          if (!numeric) return null;
          const report = this.conditionReport(rule, index);
          const hasReport = report && isNumber(report.value);
          const fallback = this.editing[rule.id] && !hasReport ? this.editValueOf(rule, condition, index) : null;
          const bar = this.liveBarOf(condition, report, fallback);
          return { ...bar, index, title: this.describeCondition(condition).title, tone: this.liveTone(condition) };
        })
        .filter(Boolean);
    },

    // Ton des Live-Balkens nach der Rolle des Felds, sonst Netz.
    liveTone(condition) {
      const tones = {
        grid_export: 'grid', grid_import: 'grid', pv: 'pv', load_total: 'load', base: 'load',
        wallbox: 'wallbox', heat_pump: 'heatpump', battery_charge: 'battery', battery_discharge: 'battery',
      };
      return tones[condition.field] || 'grid';
    },

    // Live-Balken der ersten Bilanzbedingung, wie vor liveBars.
    liveBar(rule) {
      const index = (rule.conditions || []).findIndex((condition) => condition.type === 'balance_threshold');
      if (index < 0) return null;
      return this.liveBarOf(rule.conditions[index], this.conditionReport(rule, index));
    },

    // Balken einer Bedingung. Das Band liegt zwischen Schwelle und
    // Schwelle -/+ Hysterese: bei "above" darunter, bei "below" darueber.
    liveBarOf(condition, report, fallback = null) {
      const threshold = condition.type === 'entity_value' ? condition.value : condition.threshold;
      const reported = report && isNumber(report.value) ? report.value : null;
      const value = reported !== null ? reported : (isNumber(fallback) ? fallback : null);
      const target = report && isNumber(report.target) ? report.target : threshold;
      const view = window.__automationsView;
      const scale = view.meterScale(condition, { value, target }, this.entityForCondition(condition));
      const hysteresis = isNumber(condition.hysteresis) ? condition.hysteresis : 0;
      let band = null;
      if (hysteresis > 0 && isNumber(threshold)) {
        const [low, high] = condition.comparison === 'below'
          ? [threshold, threshold + hysteresis]
          : [threshold - hysteresis, threshold];
        const left = view.meterFraction(low, scale);
        band = { left, width: view.meterFraction(high, scale) - left };
      }
      const unit = this.describeCondition(condition).unit;
      const valueText = value === null ? '—' : `${formatLiveNumber(value, unit)} ${unit}`.trim();
      const thresholdText = isNumber(threshold) ? `${formatLiveNumber(threshold, unit)} ${unit}`.trim() : '';
      return { fraction: view.meterFraction(value, scale), mark: view.meterFraction(target, scale), band, valueText, thresholdText };
    },

    // Auslöse-Leiste der letzten vier Auslösungen, Sekunden wie der Dienst sie liefert.
    ruleTriggers(rule) {
      return window.__automationChart.lastTriggers(this.liveHistory[rule.id] || [], this.nowTick / 1000);
    },

    // "once" loescht das Wiederholintervall, "repeat" setzt es auf den groesseren
    // Wert aus einer Minute und dem Veroeffentlichungs-Minimum. Mit einer
    // Umschalt-Aktion bleibt Wiederholen gesperrt, dann gibt es false zurueck.
    setRepeatMode(rule, mode) {
      if (mode === 'once') {
        delete rule.repeat_seconds;
        return true;
      }
      if (this.hasToggleAction(rule)) return false;
      rule.repeat_seconds = Math.max(60, this.minPublishInterval());
      return true;
    },

    // Bilanz kennt nur ueber/unter, Entitaet und Topic auch gleich/ungleich.
    setComparison(condition, value) {
      const allowed = condition.type === 'balance_threshold' ? ['above', 'below'] : ['above', 'below', 'equals', 'not_equals'];
      if (allowed.includes(value)) condition.comparison = value;
    },

    // Kachel des Bilanzfelds einer Bedingung mit Rollen-Icon.
    conditionFieldChip(condition) {
      const fields = [...this.balanceFields, ...this.customBalanceFields];
      const chip = balanceFieldCandidates(fields).find((candidate) => candidate.id === condition.field);
      return chip || { ...unknownChip(condition.field), deviceName: condition.field || t('automations.devices.unknown') };
    },

    async chooseConditionField(condition, rule) {
      const fields = [...this.balanceFields, ...this.customBalanceFields];
      const field = await this.openPicker({
        title: t('automations.condition.balance.field_label'),
        items: pickerItems(balanceFieldCandidates(fields)),
        mode: 'single',
        filters: roleFilters(),
        selected: condition.field ? [condition.field] : [],
      });
      if (!field) return;
      condition.field = field;
      if (rule) this.loadEditLive(rule);
    },

    // Kachel der Entitaet einer Bedingung, ohne Wahl ein Platzhalter.
    conditionEntityChip(condition) {
      if (!condition.entity_id) return null;
      return entityChip(this.devices, this.assignments, condition.entity_id) || unknownChip(condition.topic);
    },

    async chooseConditionEntity(condition, rule) {
      const uniqueId = await this.openPicker({
        title: t('automations.condition.entity.entity_label'),
        items: pickerItems(meterCandidates(this.devices, this.assignments)),
        mode: 'single',
        filters: roleFilters(),
        selected: condition.entity_id ? [condition.entity_id] : [],
      });
      if (!uniqueId) return;
      condition.entity_id = uniqueId;
      this.adoptConditionEntity(condition);
      if (rule) this.loadEditLive(rule);
    },

    validateRuleBeforeSave(rule) {
      const errors = [];
      if (!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(rule.id || '')) errors.push(t('automations.validation.invalid_id', { name: rule.name }));
      if (!rule.conditions || rule.conditions.length < 1 || rule.conditions.length > 8) errors.push(t('automations.validation.condition_count', { name: rule.name }));
      if (!rule.actions || rule.actions.length < 1 || rule.actions.length > 8) errors.push(t('automations.validation.action_count', { name: rule.name }));
      const hasPublish = (rule.actions || []).some((action) => action.type === 'publish');
      const minimum = this.minPublishInterval();
      const repeat = rule.repeat_seconds || 0;
      if (hasPublish) {
        if ((rule.cooldown_seconds || 0) < minimum) errors.push(t('automations.validation.cooldown_too_short', { name: rule.name, min: minimum }));
        for (const condition of rule.conditions || []) {
          if ('hold_seconds' in condition && condition.hold_seconds < minimum) {
            errors.push(t('automations.validation.hold_seconds_too_short', { name: rule.name, min: minimum }));
          }
        }
      }
      if (repeat > 0) {
        if (this.hasToggleAction(rule)) {
          errors.push(t('automations.validation.repeat_with_toggle', { name: rule.name }));
        } else if (hasPublish && repeat < minimum) {
          errors.push(t('automations.validation.repeat_too_short', { name: rule.name, min: minimum }));
        }
      }
      for (const condition of rule.conditions || []) {
        if (condition.type !== 'sun_window') continue;
        const offsets = [condition.from_offset_min, condition.to_offset_min];
        if (offsets.some((offset) => !Number.isInteger(offset) || Math.abs(offset) > 240)) {
          errors.push(t('automations.validation.sun_offset_invalid', { name: rule.name }));
        }
      }
      return errors;
    },

    // Das Regler-Ziel eines Szenarios, sonst null.
    regulatorOf(scenario) {
      return (scenario.while && scenario.while.regulator) || {};
    },

    // Standard der Schrittweite: Schrittweite der gewaehlten Entitaet, sonst 1.
    regulatorStepDefault(scenario) {
      const entity = this.entityByUniqueId((this.regulatorOf(scenario).output || {}).entity_id);
      return entity && isNumber(entity.step) ? entity.step : 1;
    },

    // Stellglied-Kacheln: gewaehltes zuerst, dazu hoechstens zwei passende.
    actuatorChips(scenario) {
      const regulator = this.regulatorOf(scenario);
      const output = regulator.output || {};
      const list = chipList(actuatorCandidates(this.devices, this.assignments, regulator.direction), output.entity_id, 2);
      return {
        chips: withUnknown(list, output.topic),
        more: list.more,
        hint: list.chips.length ? '' : t('automations.scenario.edit.no_actuator'),
      };
    },

    // Messgeraete fuer den Messwert "Gerät", gleicher Aufbau wie die Stellglieder.
    meterChips(scenario) {
      const base = this.regulatorOf(scenario).base || {};
      const selected = base.source === 'entity' ? base.entity_id : undefined;
      const list = chipList(meterCandidates(this.devices, this.assignments), selected, 2);
      return { chips: withUnknown(list, base.topic), more: list.more };
    },

    async chooseActuator(scenario) {
      const regulator = this.regulatorOf(scenario);
      const selected = regulator.output && regulator.output.entity_id;
      const uniqueId = await this.openPicker({
        title: t('automations.scenario.edit.regulator.output_label'),
        items: pickerItems(actuatorCandidates(this.devices, this.assignments, regulator.direction)),
        mode: 'single',
        filters: roleFilters(),
        selected: selected ? [selected] : [],
      });
      if (uniqueId) this.setRegulatorOutput(scenario, uniqueId);
    },

    async chooseMeter(scenario) {
      const base = this.regulatorOf(scenario).base || {};
      const uniqueId = await this.openPicker({
        title: t('automations.scenario.edit.regulator.base_entity_label'),
        items: pickerItems(meterCandidates(this.devices, this.assignments)),
        mode: 'single',
        filters: roleFilters(),
        selected: base.source === 'entity' && base.entity_id ? [base.entity_id] : [],
      });
      if (uniqueId) this.setRegulatorBase(scenario, 'entity', uniqueId);
    },

    // Solange ein Szenario-Editor offen ist, holt sich die Seite den Geraetekatalog
    // alle 30 s neu, damit die Live-Werte in den Kacheln stimmen. Der Sekundentakt
    // stoppt den Timer, sobald kein Editor mehr offen ist.
    syncCatalogRefresh() {
      const editing = Object.values(this.scenarioEditing).some(Boolean);
      if (editing && !this._catalogTimer) {
        this._catalogTimer = setInterval(() => { this.loadDeviceCatalog().catch(() => {}); }, CATALOG_REFRESH_MS);
        if (typeof this._catalogTimer.unref === 'function') this._catalogTimer.unref();
      } else if (!editing && this._catalogTimer) {
        clearInterval(this._catalogTimer);
        this._catalogTimer = null;
      }
    },

    regulatorTopic(scenario) {
      const current = scenario.while;
      if (!current || current.mode !== 'regulate' || !current.regulator || !current.regulator.output) return null;
      return current.regulator.output.topic || null;
    },

    // Stellglieder fuer den Regler: steuerbare Zahlen-Entitaeten mit Grenzen.
    regulatorOutputEntities() {
      return (this.entities || []).filter((entity) => isNumber(entity.min_value) && isNumber(entity.max_value));
    },

    // Das Stellglied uebernimmt Topic und Grenzen. Beim Wechselrichter ist der
    // Ersatzwert Pflicht, er kommt als Hoechstwert des Stellglieds.
    setRegulatorOutput(scenario, uniqueId) {
      const entity = this.entityByUniqueId(uniqueId);
      const regulator = scenario.while && scenario.while.regulator;
      if (!entity || !regulator) return;
      const output = { topic: entity.command_topic, entity_id: entity.unique_id, min: entity.min_value, max: entity.max_value };
      if (isNumber(entity.step)) output.step = entity.step;
      regulator.output = output;
      // Wallbox und Waermepumpe sind Verbraucher, ein Wechselrichter erzeugt.
      // Sonstige Geraete lassen die Richtung stehen.
      const device = (this.devices || []).find((item) => (item.entities || []).some((other) => other.unique_id === uniqueId));
      const group = device ? deviceRoleGroup(device, this.assignments) : 'other';
      if (group === 'wallbox' || group === 'heat_pump') this.setRegulatorDirection(scenario, 'consumer');
      else if (group === 'inverter') this.setRegulatorDirection(scenario, 'producer');
      if (regulator.direction === 'producer') regulator.fallback = { payload: String(entity.max_value) };
    },

    // Die Basis kommt von einem Geraet (entity), aus der Bilanz (balance)
    // oder vom zuletzt gesendeten Stellwert (last).
    setRegulatorBase(scenario, source, uniqueId) {
      const regulator = scenario.while && scenario.while.regulator;
      if (!regulator) return;
      if (source === 'entity') {
        const entity = (this.readableEntities || []).find((candidate) => candidate.unique_id === uniqueId);
        if (!entity) return;
        regulator.base = { source, entity_id: entity.unique_id, topic: entity.state_topic, value_template: entity.value_template || '' };
      } else if (source === 'balance') {
        regulator.base = { source, field: 'pv' };
      } else {
        regulator.base = { source: 'last' };
      }
    },

    // Der Ersatzwert bleibt beim Wechsel zum Verbraucher stehen, er ist dort
    // nur optional. Fehlt er beim Wechsel zum Wechselrichter, kommt der Hoechstwert.
    setRegulatorDirection(scenario, direction) {
      const regulator = scenario.while && scenario.while.regulator;
      if (!regulator || !['producer', 'consumer'].includes(direction)) return;
      regulator.direction = direction;
      const hasFallback = Boolean(regulator.fallback && regulator.fallback.payload);
      if (direction === 'producer' && !hasFallback && isNumber(regulator.output && regulator.output.max)) {
        regulator.fallback = { payload: String(regulator.output.max) };
      }
    },

    // Schalter "Eigene Endbedingung" und Moduswahl "Regeln, Halten, Nichts".
    toggleUntil(scenario, on) {
      window.__automationScenario.setUntil(scenario, on);
    },

    maxDurationMinutes(scenario) {
      return window.__automationScenario.maxDurationMinutes(scenario);
    },

    setMaxDurationMinutes(scenario, minutes) {
      window.__automationScenario.setMaxDurationMinutes(scenario, minutes);
    },

    minRunMinutes(scenario) {
      return window.__automationScenario.minRunMinutes(scenario);
    },

    setMinRunMinutes(scenario, minutes) {
      window.__automationScenario.setMinRunMinutes(scenario, minutes);
    },

    cooldownMinutes(scenario) {
      return window.__automationScenario.cooldownMinutes(scenario);
    },

    setCooldownMinutes(scenario, minutes) {
      window.__automationScenario.setCooldownMinutes(scenario, minutes);
    },

    changeWhileMode(scenario, mode) {
      window.__automationScenario.setWhileMode(scenario, mode);
    },

    validateDocumentBeforeSave() {
      const errors = (this.document.rules || []).flatMap((rule) => this.validateRuleBeforeSave(rule));
      const scenarios = this.document.scenarios || [];
      const minInterval = this.minPublishInterval();
      for (const scenario of scenarios) {
        const otherOutputs = scenarios
          .filter((other) => other !== scenario)
          .map((other) => this.regulatorTopic(other))
          .filter(Boolean);
        errors.push(...window.__automationScenario.validateScenarioBeforeSave(scenario, { minInterval, otherOutputs }));
      }
      const settings = this.document.settings || {};
      if (isNumber(settings.latitude) !== isNumber(settings.longitude)) {
        errors.push(t('automations.validation.location_incomplete'));
      }
      return errors;
    },

    // Szenarien fuer den Speicherkoerper: Regler-Defaults fallen weg, und
    // ein null taucht nicht im Dokument auf (der Dienst liest null nicht).
    scenariosPayload() {
      const strip = window.__automationScenario.stripRegulatorDefaults;
      const withoutNulls = (value) => {
        if (Array.isArray(value)) return value.map(withoutNulls);
        if (value && typeof value === 'object') {
          const out = {};
          for (const [key, entry] of Object.entries(value)) {
            if (entry !== null && entry !== undefined) out[key] = withoutNulls(entry);
          }
          return out;
        }
        return value;
      };
      return (this.document.scenarios || []).map((scenario) => {
        const copy = { ...scenario };
        if (copy.while && copy.while.regulator) {
          copy.while = { ...copy.while, regulator: strip(copy.while.regulator) };
        }
        return withoutNulls(copy);
      });
    },

    // Nur Ein-Aus-Schalter (operate_automations ohne automations): gespeichert
    // wird der zuletzt gespeicherte Stand mit neuen enabled-Werten.
    operatePayload() {
      const payload = JSON.parse(JSON.stringify(this.storedDocument));
      const enabledById = (list) => new Map((list || []).map((entry) => [entry.id, entry.enabled]));
      const rules = enabledById(this.document.rules);
      const scenarios = enabledById(this.document.scenarios);
      (payload.rules || []).forEach((rule) => { if (rules.has(rule.id)) rule.enabled = rules.get(rule.id); });
      (payload.scenarios || []).forEach((scenario) => { if (scenarios.has(scenario.id)) scenario.enabled = scenarios.get(scenario.id); });
      return payload;
    },

    async saveOperate() {
      this.saving = true;
      try {
        const payload = this.operatePayload();
        const saved = await requestJSON('/api/v1/configurations/automation_rules', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken }, // i18n-ignore
          body: JSON.stringify(payload),
        });
        this.$store.toasts.push(t('automations.saved'));
        this.savedDocument = JSON.parse(JSON.stringify(payload));
        this.storedDocument = JSON.parse(JSON.stringify(payload));
        await this.pollRuntimeStatus(saved.checksum);
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.saving = false;
      }
    },

    async save() {
      if (!this.canEditAutomations) {
        if (this.canOperateAutomations) await this.saveOperate();
        return;
      }
      const errors = this.validateDocumentBeforeSave();
      if (errors.length > 0) {
        // Validierungsfehler vor dem Speichern - bleibt als critical stehen,
        // waehrend der Nutzer korrigiert.
        this.$store.toasts.push(errors.join(' / '), 'critical');
        return;
      }
      this.saving = true;
      try {
        // Ein leeres Praefix-Feld bleibt in der Anzeige leer (kein manuell
        // wirkender Auto-Default), aber gespeichert wird die konkrete Liste
        // aller aktuell bekannten command_topics - siehe
        // computeAutoPublishPrefixes(). this.document selbst bleibt dabei
        // unveraendert, sonst wuerde das Feld nach dem Speichern ploetzlich
        // befuellt angezeigt.
        // Die Auto-Praefixe entstehen aus this.entities, und die wird seit A6
        // nicht mehr pro Tick aktualisiert. Ein veralteter Praefix-Satz waere
        // fail-closed und damit ein fachlicher Fehler (der Dienst darf dann
        // nicht mehr publizieren), kein Darstellungsproblem - deshalb hier
        // einmal frisch holen. Ein Abruf pro Speichervorgang ist zu
        // verschmerzen; das Speichern ist selten und interaktiv ausgeloest.
        await this.loadDeviceCatalog();
        const manualPrefixes = this.document.settings.publish_allowed_prefixes || [];
        const settingsToSave = manualPrefixes.length > 0
          ? { ...this.document.settings, publish_allowed_prefixes_auto: false }
          : { ...this.document.settings, publish_allowed_prefixes: this.computeAutoPublishPrefixes(),
              publish_allowed_prefixes_auto: true };
        // Ein geleertes Zahlenfeld liefert '' - das Schema will eine Zahl oder
        // gar nichts. Ohne Standort bleiben beide Felder weg.
        if (!this.hasLocation()) {
          delete settingsToSave.latitude;
          delete settingsToSave.longitude;
        }
        const sent = { ...this.document, settings: settingsToSave, scenarios: this.scenariosPayload() };
        const saved = await requestJSON('/api/v1/configurations/automation_rules', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken }, // i18n-ignore
          body: JSON.stringify(sent),
        });
        this.$store.toasts.push(t('automations.saved'));
        this.savedDocument = JSON.parse(JSON.stringify(this.document));
        this.storedDocument = JSON.parse(JSON.stringify(sent));
        this.newIds = {};
        clearTimeout(this._undoTimer);
        this.undo = null;
        await this.pollRuntimeStatus(saved.checksum);
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      } finally {
        this.saving = false;
      }
    },

    async pollRuntimeStatus(checksum) {
      const result = await window.ConfigStatus.watch({
        fetchStatus: () => requestJSON('/api/v1/configurations/automation_rules/status'),
        revision: checksum,
        intervalMs: this.statusPollMs ?? 1000,
      });
      if (result.state === 'rejected') {
        this.$store.toasts.push(t('automations.save_status.rejected', { reason: window.ConfigStatus.errorText(result.status) }), 'critical');
      } else if (result.state === 'applied') {
        this.$store.toasts.push(t('automations.save_status.applied'));
      } else {
        this.$store.toasts.push(t('automations.save_status.no_response'), 'warning');
      }
    },

    toggleHistory(rule) {
      this.historyExpanded[rule.id] = !this.historyExpanded[rule.id];
    },

    historyEntries(rule) {
      return this.liveHistory[rule.id] || [];
    },

    historyResultInfo(result) {
      return window.__automationsView.historyResultInfo(result);
    },

    historyActionText(action) {
      return window.__automationsView.historyActionText(action);
    },

    historyTimestamp(event) {
      return window.I18n.formatDateTime(event.at * 1000);
    },
  });

  window.Alpine.data('automationsPanel', automationsPanel);
})();

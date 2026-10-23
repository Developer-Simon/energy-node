(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const view = window.__automationsView;
  const { canTest, conditionState, describeAction, describeCondition, formatSeconds, gateVerdict, historyActionText, historyResultInfo, meterFraction, meterScale } = view;
  const { ACTION_TYPES, CONDITION_TYPES, FIELD_HELP, WEEKDAY_LABELS, WINDOW_TYPES, SERVICE_TEST_POLL_MS, SERVICE_TEST_TIMEOUT_MS, isNumber, requestJSON } = view.constants;

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
  const EXPORT_REFRESH_MS = 300000;
  const LIVE_WINDOW_S = 1800;
  const LIVE_MAX_POINTS = 180;
  const GRID_NOW_MAX_AGE_S = 120;
  const DEFAULT_RANGE = '30m';
  const DEFAULT_DEADBAND_W = 20;
  const GRID_SERIES = 'role:grid';

  const outputSeriesName = (id) => `automation_scenario_${id}_output`;

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

  const automationsPanel = () => ({
    loading: false,
    saving: false,
    document: emptyDocument(),
    categories: {},
    entities: [],
    readableEntities: [],
    devices: [],
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
    savedDocument: null,
    lastStateAt: 0,
    hasAutomationsRole: false,
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
    livePoints: {},
    chartLoadedAt: {},
    todayExportKWh: null,
    todayExportAt: 0,

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
      return window.__automationsView.conditionState(this.conditionReport(rule, index));
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

    conditionValueText(rule, index) {
      const report = this.conditionReport(rule, index);
      if (!report || report.value === null || report.value === undefined) return '—';
      return String(report.value);
    },

    conditionStateText(rule, index) {
      const report = this.conditionReport(rule, index);
      const state = this.conditionState(rule, index);
      const condition = rule.conditions[index] || {};
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
      const preview = state && state.actions && state.actions[index];
      if (!preview) return { blocked: false, text: t('automations.action_preview.no_preview') };
      if (preview.blocked) return { blocked: true, text: preview.reason || t('automations.action_preview.blocked') };
      if (rule.actions[index] && rule.actions[index].type === 'notification') {
        return { blocked: false, text: `„${preview.title || ''}" — ${preview.message || ''}` };
      }
      return { blocked: false, text: `${preview.topic} → ${preview.payload}` };
    },

    renderMiniChain(rule) {
      return {
        conditions: rule.conditions.map((condition, index) => ({
          icon: this.describeCondition(condition).icon,
          state: this.conditionState(rule, index),
        })),
        actions: rule.actions.map((action) => ({ icon: this.describeAction(action).icon })),
        gate: this.gateVerdict(rule),
      };
    },

    isDirty(rule) {
      if (!this.savedDocument) return false;
      const saved = (this.savedDocument.rules || []).find((entry) => entry.id === rule.id);
      if (!saved) return true;
      return JSON.stringify(saved) !== JSON.stringify(rule);
    },

    startEditing(rule) { this.editing[rule.id] = true; this.expanded[rule.id] = true; },
    stopEditing(rule) { this.editing[rule.id] = false; },
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
        output: state && isNumber(state.output) ? Math.round(state.output) : '—',
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
      const phase = window.__automationScenario.activePhase(this.scenarioState(scenario), this.nowTick / 1000);
      return { start: 0, while: 1, end: 2 }[phase] ?? -1;
    },

    // Balken des Startfortschritts, nur solange die Karte wartet oder bereit ist.
    scenarioProgress(scenario) {
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
      const state = this.scenarioState(scenario);
      if (state && isNumber(state.net)) return signedW(state.net);
      const grid = this.chartSeries(scenario.id, 'grid');
      const last = grid[grid.length - 1];
      if (last && this.nowTick / 1000 - last.ts < GRID_NOW_MAX_AGE_S) return signedW(last.value);
      return '—';
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

    notify(message, severity) {
      this.$store.toasts.push(message, severity);
    },

    addRule() {
      if (!this.canAddRule()) {
        this.notify(t('automations.limit.rules', { max: RULE_LIMIT }), 'warning');
        return;
      }
      const id = `regel_${Date.now()}`;
      this.insertEntry('rule', { id, name: t('automations.new_rule'), enabled: false, cooldown_seconds: 60, conditions: [], actions: [] });
    },

    addScenario() {
      if (!this.canAddScenario()) {
        this.notify(t('automations.limit.scenarios', { max: SCENARIO_LIMIT }), 'warning');
        return;
      }
      const id = `szenario_${Date.now()}`;
      this.insertEntry('scenario', window.__automationScenario.emptyScenario(id, t('automations.new_scenario')));
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

    conditionTypes() { return CONDITION_TYPES.map((type) => ({ ...type, label: t(type.label), hint: t(type.hint) })); },
    actionTypes() { return ACTION_TYPES.map((type) => ({ ...type, label: t(type.label), hint: t(type.hint) })); },
    fieldHelp(key) { return FIELD_HELP[key] ? t(FIELD_HELP[key]) : ''; },

    addBalanceCondition(rule) {
      rule.conditions.push({ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 800, hysteresis: 100, hold_seconds: 300 });
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
      if (key === 'balance') return this.addBalanceCondition(rule);
      if (key === 'entity') return this.addEntityValueCondition(rule);
      if (key === 'time') return this.addTimeWindowCondition(rule);
      if (key === 'sun') return this.addSunWindowCondition(rule);
    },

    // scope ist der x-data-Bereich der Regelzeile; "entity" legt keine Aktion
    // an, sondern klappt dort den Geraete-Assistenten auf.
    addAction(rule, key, scope) {
      if (key === 'notify') return void rule.actions.push(this.buildNotificationAction());
      if (key === 'mqtt') return void rule.actions.push(this.buildMqttAction());
      if (key === 'entity' && scope) scope.showEntityWizard = true;
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

    // Live-Balken der ersten Bilanzbedingung. Das Band liegt zwischen Schwelle
    // und Schwelle -/+ Hysterese: bei "above" darunter, bei "below" darueber.
    liveBar(rule) {
      const index = (rule.conditions || []).findIndex((condition) => condition.type === 'balance_threshold');
      if (index < 0) return null;
      const condition = rule.conditions[index];
      const report = this.conditionReport(rule, index);
      const value = report && isNumber(report.value) ? report.value : null;
      const target = report && isNumber(report.target) ? report.target : condition.threshold;
      const view = window.__automationsView;
      const scale = view.meterScale(condition, { value, target }, this.entityForCondition(condition));
      const hysteresis = isNumber(condition.hysteresis) ? condition.hysteresis : 0;
      let band = null;
      if (hysteresis > 0) {
        const [low, high] = condition.comparison === 'below'
          ? [condition.threshold, condition.threshold + hysteresis]
          : [condition.threshold - hysteresis, condition.threshold];
        const left = view.meterFraction(low, scale);
        band = { left, width: view.meterFraction(high, scale) - left };
      }
      const unit = this.describeCondition(condition).unit;
      const valueText = value === null ? '—' : `${value} ${unit}`.trim();
      return { fraction: view.meterFraction(value, scale), mark: view.meterFraction(target, scale), band, valueText };
    },

    // Zeitleiste der letzten sechs Stunden, Sekunden wie der Dienst sie liefert.
    ruleTimeline(rule) {
      const now = this.nowTick / 1000;
      return window.__automationChart.triggerTimeline(this.liveHistory[rule.id] || [], now - 6 * 3600, now, 600);
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

    setComparison(condition, value) {
      if (value === 'above' || value === 'below') condition.comparison = value;
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
    regulatorTopic(scenario) {
      const current = scenario.while;
      if (!current || current.mode !== 'regulate' || !current.regulator || !current.regulator.output) return null;
      return current.regulator.output.topic || null;
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

    async save() {
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
        const saved = await requestJSON('/api/v1/configurations/automation_rules', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken }, // i18n-ignore
          body: JSON.stringify({ ...this.document, settings: settingsToSave, scenarios: this.scenariosPayload() }),
        });
        this.$store.toasts.push(t('automations.saved'));
        this.savedDocument = JSON.parse(JSON.stringify(this.document));
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

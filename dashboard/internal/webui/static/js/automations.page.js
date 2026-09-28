(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  const requestJSON = async (url, options) => {
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) {
      const error = new Error(apiError(body));
      // Der Status wandert mit, damit Aufrufer ein 404 vom Netzwerkfehler
      // unterscheiden koennen: fehlt das Geraet "automation", ist das ein
      // Dauerzustand (kein Automations-Dienst), kein voruebergehender Fehler.
      error.status = response.status;
      throw error;
    }
    return body;
  };

  // i18n-keys: automations.metric.autarkie, automations.metric.base, automations.metric.battery_capacity_kwh, automations.metric.battery_charge, automations.metric.battery_discharge, automations.metric.battery_energy_kwh, automations.metric.battery_soc, automations.metric.eigenverbrauch, automations.metric.gap_applied, automations.metric.grid_export, automations.metric.grid_import, automations.metric.heat_pump, automations.metric.load_total, automations.metric.pv, automations.metric.wallbox, automations.summary.balance, automations.summary.balance_hysteresis, automations.summary.entity_value, automations.summary.entity_value_unit, automations.summary.topic_value, automations.action.notification, automations.action.mqtt_command, automations.action.unknown, automations.test.permission_required, automations.test.service_offline, automations.test.save_first, automations.test.blocked_action, automations.test.confirm, automations.test.trigger, automations.test.failed, automations.test.no_response, automations.test.published, automations.test.blocked, automations.test.error, automations.saved, automations.condition_state.sun_no_event, automations.condition_state.no_location, automations.condition_state.no_value, automations.condition_state.outside_window, automations.condition_state.threshold_not_reached, automations.condition_state.met, automations.condition_state.met_since, automations.condition_state.pending, automations.condition_state.pending_since, automations.gate_meta.cooldown, automations.gate_meta.last_fired, automations.gate_meta.fire_count, automations.action_preview.no_preview, automations.action_preview.blocked, automations.new_rule, automations.delete_rule.title, automations.delete_rule.body, automations.geolocation.denied, automations.geolocation.failed, automations.gate_detail.hold_pending, automations.gate_detail.cooldown, automations.condition_type_unknown, automations.badge.balance_stale, automations.badge.blocked, automations.badge.cooldown, automations.badge.disabled, automations.badge.settling
  // Eine Tabelle je Bilanzfeld: Klartext, Icon-Symbol im Sprite, Einheit.
  const BALANCE_FIELD_INFO = {
    grid_export:       { label: 'automations.metric.grid_export',     icon: 'ico-grid',     unit: 'W' },
    grid_import:       { label: 'automations.metric.grid_import',     icon: 'ico-grid',     unit: 'W' },
    pv:                { label: 'automations.metric.pv',              icon: 'ico-sun',      unit: 'W' },
    load_total:        { label: 'automations.metric.load_total',      icon: 'ico-load',     unit: 'W' },
    base:              { label: 'automations.metric.base',            icon: 'ico-load',     unit: 'W' },
    wallbox:           { label: 'automations.metric.wallbox',         icon: 'ico-wallbox',  unit: 'W' },
    heat_pump:         { label: 'automations.metric.heat_pump',       icon: 'ico-heatpump', unit: 'W' },
    battery_charge:    { label: 'automations.metric.battery_charge',  icon: 'ico-battery',  unit: 'W' },
    battery_discharge: { label: 'automations.metric.battery_discharge', icon: 'ico-battery', unit: 'W' },
    gap_applied:       { label: 'automations.metric.gap_applied',     icon: 'ico-warning',  unit: 'W' },
    autarkie:          { label: 'automations.metric.autarkie',        icon: 'ico-flash',    unit: '%' },
    eigenverbrauch:    { label: 'automations.metric.eigenverbrauch',  icon: 'ico-flash',    unit: '%' },
    battery_soc:          { label: 'automations.metric.battery_soc',        icon: 'ico-battery', unit: '%' },
    battery_capacity_kwh: { label: 'automations.metric.battery_capacity_kwh', icon: 'ico-battery', unit: 'kWh' },
    battery_energy_kwh:   { label: 'automations.metric.battery_energy_kwh',   icon: 'ico-battery', unit: 'kWh' },
  };

  // i18n-keys: automations.comparison.above, automations.comparison.below, automations.comparison.equals, automations.comparison.not_equals
  const COMPARISON_WORDS = { above: 'automations.comparison.above', below: 'automations.comparison.below', equals: 'automations.comparison.equals', not_equals: 'automations.comparison.not_equals' };

  // i18n-keys: automations.gate.balance_stale, automations.gate.blocked, automations.gate.conditions_not_met, automations.gate.cooldown, automations.gate.disabled, automations.gate.error, automations.gate.fired, automations.gate.hold_pending, automations.gate.settling
  const GATE_VERDICTS = {
    fired:              { tone: 'ok',   label: 'automations.gate.fired' },
    hold_pending:       { tone: 'wait', label: 'automations.gate.hold_pending' },
    cooldown:           { tone: 'info', label: 'automations.gate.cooldown' },
    conditions_not_met: { tone: 'off',  label: 'automations.gate.conditions_not_met' },
    settling:           { tone: 'off',  label: 'automations.gate.settling' },
    balance_stale:      { tone: 'bad',  label: 'automations.gate.balance_stale' },
    blocked:            { tone: 'bad',  label: 'automations.gate.blocked' },
    error:              { tone: 'bad',  label: 'automations.gate.error' },
    disabled:           { tone: 'off',  label: 'automations.gate.disabled' },
  };

  // i18n-keys: automations.history_result.blocked, automations.history_result.error, automations.history_result.fired
  const HISTORY_RESULT_INFO = {
    fired:   { tone: 'ok',  label: 'automations.history_result.fired' },
    blocked: { tone: 'bad', label: 'automations.history_result.blocked' },
    error:   { tone: 'bad', label: 'automations.history_result.error' },
  };

  function historyResultInfo(result) {
    const entry = HISTORY_RESULT_INFO[result];
    if (entry) return { tone: entry.tone, label: t(entry.label) };
    return { tone: 'off', label: result || t('automations.unknown') };
  }

  function historyActionText(action) {
    if (!action) return '';
    if (action.severity !== undefined) return `„${action.title || ''}" — ${action.message || ''}`;
    if (action.blocked) return action.reason || 'blockiert';
    return `${action.topic} → ${action.payload}`;
  }

  // i18n-keys: automations.condition_type.balance.hint, automations.condition_type.balance.label, automations.condition_type.entity.hint, automations.condition_type.entity.label, automations.condition_type.sun.hint, automations.condition_type.sun.label, automations.condition_type.time.hint, automations.condition_type.time.label
  // Was der Bearbeiten-Modus als Bedingung anbieten darf. "balance" heisst
  // bewusst Energiewert und nicht PV-Ueberschuss: der Typ balance_threshold
  // deckt jedes Bilanzfeld ab - Netzbezug, Autarkiegrad, Batterie-Fuellstand -,
  // die PV-Einspeisung ist nur die haeufigste Vorbelegung.
  const CONDITION_TYPES = [
    { key: 'balance', icon: 'ico-sun', label: 'automations.condition_type.balance.label',
      hint: 'automations.condition_type.balance.hint' },
    { key: 'entity', icon: 'ico-thermo', label: 'automations.condition_type.entity.label',
      hint: 'automations.condition_type.entity.hint' },
    { key: 'time', icon: 'ico-clock', label: 'automations.condition_type.time.label',
      hint: 'automations.condition_type.time.hint' },
    { key: 'sun', icon: 'ico-sun', label: 'automations.condition_type.sun.label',
      hint: 'automations.condition_type.sun.hint' },
  ];

  // i18n-keys: automations.sun_event.sunrise, automations.sun_event.sunset
  // Reihenfolge wie im Dienst: 0 = Montag ... 6 = Sonntag.
  const WEEKDAY_LABELS = ['automations.weekday.0', 'automations.weekday.1', 'automations.weekday.2', 'automations.weekday.3', 'automations.weekday.4', 'automations.weekday.5', 'automations.weekday.6'];
  const SUN_EVENT_LABELS = { sunrise: 'automations.sun_event.sunrise', sunset: 'automations.sun_event.sunset' };

  // "Mo–Fr", "Sa, So", "Mo, Mi–Fr": zusammenhaengende Tage ab drei als Spanne.
  function formatWeekdays(weekdays) {
    const days = [...new Set(weekdays || [])].filter((day) => day >= 0 && day <= 6).sort((a, b) => a - b);
    if (days.length === 0 || days.length === 7) return t('automations.weekdays_daily');
    const runs = [];
    for (const day of days) {
      const run = runs[runs.length - 1];
      if (run && day === run[run.length - 1] + 1) run.push(day);
      else runs.push([day]);
    }
    return runs.flatMap((run) => (run.length >= 3
      ? [`${t(WEEKDAY_LABELS[run[0]])}–${t(WEEKDAY_LABELS[run[run.length - 1]])}`]
      : run.map((day) => t(WEEKDAY_LABELS[day])))).join(', ');
  }

  function formatSunPoint(event, offset) {
    const label = t(SUN_EVENT_LABELS[event] || event);
    if (!offset) return label;
    return `${label} ${offset > 0 ? '+' : '−'}${Math.abs(offset)} min`;
  }

  function weekdaySuffix(condition) {
    return (condition.weekdays || []).length ? `, ${formatWeekdays(condition.weekdays)}` : '';
  }

  const WINDOW_TYPES = new Set(['time_window', 'sun_window']);

  // i18n-keys: automations.action_type.entity.hint, automations.action_type.entity.label, automations.action_type.mqtt.hint, automations.action_type.mqtt.label, automations.action_type.notify.hint, automations.action_type.notify.label
  // "entity" legt keine Aktion an, sondern oeffnet den Geraete-Assistenten -
  // dort entscheidet erst die Wahl zwischen Ein/Aus/Umschalten/Sollwert,
  // welche publish-Aktion daraus wird.
  const ACTION_TYPES = [
    { key: 'entity', icon: 'ico-switch', label: 'automations.action_type.entity.label', advanced: false,
      hint: 'automations.action_type.entity.hint' },
    { key: 'notify', icon: 'ico-bell', label: 'automations.action_type.notify.label', advanced: false,
      hint: 'automations.action_type.notify.hint' },
    { key: 'mqtt', icon: 'ico-topic', label: 'automations.action_type.mqtt.label', advanced: true,
      hint: 'automations.action_type.mqtt.hint' },
  ];

  // i18n-keys: automations.field_help.balance_max_age_s, automations.field_help.cooldown_seconds, automations.field_help.history_enabled, automations.field_help.history_limit, automations.field_help.history_persist, automations.field_help.hold_seconds, automations.field_help.hysteresis, automations.field_help.json_key, automations.field_help.location, automations.field_help.offset, automations.field_help.payload, automations.field_help.publish_allowed_prefixes, automations.field_help.retain, automations.field_help.scale, automations.field_help.settling_seconds, automations.field_help.tick_interval_s, automations.field_help.topic
  // Die Erklaerungen zu den Fachbegriffen. Die Begriffe selbst bleiben in der
  // Oberflaeche stehen, damit sie in der Doku und in der rules.json
  // wiederzufinden sind - erklaert wird daneben.
  const FIELD_HELP = {
    hysteresis: 'automations.field_help.hysteresis',
    hold_seconds: 'automations.field_help.hold_seconds',
    cooldown_seconds: 'automations.field_help.cooldown_seconds',
    tick_interval_s: 'automations.field_help.tick_interval_s',
    settling_seconds: 'automations.field_help.settling_seconds',
    balance_max_age_s: 'automations.field_help.balance_max_age_s',
    publish_allowed_prefixes: 'automations.field_help.publish_allowed_prefixes',
    history_limit: 'automations.field_help.history_limit',
    history_enabled: 'automations.field_help.history_enabled',
    location: 'automations.field_help.location',
    history_persist: 'automations.field_help.history_persist',
    retain: 'automations.field_help.retain',
    scale: 'automations.field_help.scale',
    offset: 'automations.field_help.offset',
    json_key: 'automations.field_help.json_key',
    topic: 'automations.field_help.topic',
    payload: 'automations.field_help.payload',
  };

  const SERVICE_TEST_TIMEOUT_MS = 10000;
  const SERVICE_TEST_POLL_MS = 1000;

  const isNumber = (candidate) => typeof candidate === 'number' && Number.isFinite(candidate);

  function formatSeconds(seconds) {
    const value = isNumber(seconds) ? Math.max(0, seconds) : 0;
    if (value < 60) return `${Math.round(value)} s`;
    if (value < 3600) return `${Math.round(value / 60)} min`;
    if (value < 86400) return `${Math.round(value / 3600)} h`;
    return `${Math.round(value / 86400)} d`;
  }

  function conditionState(report) {
    if (!report || report.value === null || report.value === undefined) return 'novalue';
    if (!report.raw_met) return 'unmet';
    if (!report.met) return 'pending';
    return 'met';
  }

  function isPercentCondition(condition, entity) {
    if (condition.type === 'entity_value') {
      return Boolean(entity) && entity.unit_of_measurement === '%';
    }
    if (condition.type !== 'balance_threshold') return false;
    return (BALANCE_FIELD_INFO[condition.field] || {}).unit === '%';
  }

  function meterScale(condition, report, entity) {
    if (WINDOW_TYPES.has(condition.type)) return { min: 0, max: 1440, kind: 'time' };
    if (isPercentCondition(condition, entity)) return { min: 0, max: 100, kind: 'percent' };
    const value = isNumber(report && report.value) ? report.value : null;
    const target = isNumber(report && report.target) ? report.target : null;
    if ((value !== null && value < 0) || (target !== null && target < 0)) {
      const bound = Math.max(Math.abs(value || 0), Math.abs(target || 0)) * 1.1 || 1;
      return { min: -bound, max: bound, kind: 'signed' };
    }
    const max = Math.max((target || 0) * 2, (value || 0) * 1.1, 1);
    return { min: 0, max, kind: 'linear' };
  }

  function meterFraction(value, scale) {
    if (!isNumber(value) || !scale || scale.max === scale.min) return 0;
    return Math.min(1, Math.max(0, (value - scale.min) / (scale.max - scale.min)));
  }

  function gateDetail(ruleState) {
    if (!ruleState) return '';
    if (ruleState.result === 'hold_pending') {
      const remaining = (ruleState.conditions || [])
        .map((entry) => (isNumber(entry.hold_remaining) ? entry.hold_remaining : 0))
        .reduce((highest, current) => Math.max(highest, current), 0);
      return t('automations.gate_detail.hold_pending', { duration: formatSeconds(remaining) });
    }
    if (ruleState.result === 'cooldown') return t('automations.gate_detail.cooldown', { duration: formatSeconds(ruleState.cooldown_remaining) });
    if (ruleState.reason) return ruleState.reason;
    return '';
  }

  function gateVerdict(ruleState, online) {
    if (!online) return { tone: 'off', label: t('automations.service_offline'), detail: t('automations.no_live_values') };
    const entry = GATE_VERDICTS[ruleState && ruleState.result];
    if (!entry) return { tone: 'off', label: t('automations.state_unknown'), detail: '' };
    return { tone: entry.tone, label: t(entry.label), detail: gateDetail(ruleState) };
  }

  const DEVICE_CLASS_ICONS = {
    battery: 'ico-battery', power: 'ico-flash', energy: 'ico-flash',
    temperature: 'ico-thermo', current: 'ico-flash', voltage: 'ico-flash',
  };

  function describeCondition(condition, entity) {
    const type = condition && condition.type;
    if (type === 'balance_threshold') {
      const info = BALANCE_FIELD_INFO[condition.field] || { label: condition.field, icon: 'ico-flash', unit: '' };
      const comparison = t(COMPARISON_WORDS[condition.comparison] || condition.comparison);
      const summaryKey = condition.hysteresis ? 'automations.summary.balance_hysteresis' : 'automations.summary.balance';
      return { icon: info.icon, title: t(info.label), unit: info.unit,
               summary: t(summaryKey, { comparison, threshold: condition.threshold, unit: info.unit, hysteresis: condition.hysteresis }) };
    }
    if (type === 'entity_value') {
      // Fehlt die Entitaet - noch nicht geladen, oder aus dem Register
      // verschwunden -, tritt das gespeicherte Topic als Titel ein. Die Regel
      // laeuft in dem Fall unveraendert weiter, nur die Karte ist karger.
      const unit = (entity && entity.unit_of_measurement) || '';
      const icon = (entity && DEVICE_CLASS_ICONS[entity.device_class]) || 'ico-topic';
      const expected = condition.text !== undefined && condition.text !== '' ? condition.text : condition.value;
      const comparison = t(COMPARISON_WORDS[condition.comparison] || condition.comparison);
      const summaryKey = unit ? 'automations.summary.entity_value_unit' : 'automations.summary.entity_value';
      return { icon, title: (entity && entity.name) || condition.topic || t('automations.entity_value'), unit,
               summary: t(summaryKey, { comparison, expected, unit }) };
    }
    if (type === 'topic_value') {
      const expected = condition.text !== undefined && condition.text !== '' ? condition.text : condition.value;
      const comparison = t(COMPARISON_WORDS[condition.comparison] || condition.comparison);
      return { icon: 'ico-topic', title: condition.topic || t('automations.topic_value'), unit: '',
               summary: t('automations.summary.topic_value', { comparison, expected }) };
    }
    if (type === 'time_window') {
      return { icon: 'ico-clock', title: t('automations.time_window'), unit: '',
               summary: `${condition.start} – ${condition.end}${weekdaySuffix(condition)}` };
    }
    if (type === 'sun_window') {
      const from = formatSunPoint(condition.from, condition.from_offset_min);
      const to = formatSunPoint(condition.to, condition.to_offset_min);
      return { icon: 'ico-sun', title: t('automations.sun_window'), unit: '',
               summary: `${from} – ${to}${weekdaySuffix(condition)}` };
    }
    // Vorwaertskompatibilitaet: Spec A bringt weitere Typen. Bis dahin - und
    // falls je ein unbekannter Typ auftaucht - wird eine neutrale Karte
    // gezeichnet, statt die ganze Ansicht scheitern zu lassen.
    return { icon: 'ico-flash', title: type ? t('automations.condition_type_unknown', { type }) : t('automations.unknown_condition'),
             unit: '', summary: '' };
  }

  function describeAction(action, entity) {
    if (action && action.type === 'notification') {
      return { icon: 'ico-bell', kind: 'notify', title: action.title || t('automations.action.notification') };
    }
    if (action && action.type === 'publish') {
      // Steht hinter der Aktion eine bekannte Entitaet (entity_id gesetzt und
      // im Register gefunden), zeigt die Karte deren Namen und Geraeteklasse
      // wie eine entity_value-Bedingung - fehlt die Entitaet, bleibt das
      // rohe Topic als Titel stehen, die Regel laeuft unveraendert weiter.
      const icon = (entity && DEVICE_CLASS_ICONS[entity.device_class]) || 'ico-switch';
      const title = (entity && entity.name) || action.topic || t('automations.action.mqtt_command');
      return { icon, kind: 'command', title };
    }
    return { icon: 'ico-flash', kind: 'command', title: t('automations.action.unknown') };
  }

  function canTest(context) {
    if (!context || context.hasRole === false) {
      return { allowed: false, reason: t('automations.test.permission_required') };
    }
    if (!context.online) return { allowed: false, reason: t('automations.test.service_offline') };
    if (context.dirty) return { allowed: false, reason: t('automations.test.save_first') };
    return { allowed: true, reason: '' };
  }

  window.__automationsView = {
    conditionState, meterScale, meterFraction, gateVerdict, isPercentCondition,
    describeCondition, describeAction, formatSeconds, canTest, formatWeekdays,
    historyResultInfo, historyActionText,
    BALANCE_FIELD_INFO, GATE_VERDICTS, CONDITION_TYPES, ACTION_TYPES, FIELD_HELP,
    WEEKDAY_LABELS, SUN_EVENT_LABELS,
  };

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

  const emptyDocument = () => ({
    version: 1,
    settings: { tick_interval_s: 10, settling_seconds: 60, balance_max_age_s: 30,
                history_limit: 10, history_persist: true,
                publish_allowed_prefixes: [], publish_allowed_prefixes_auto: false },
    rules: [],
  });

  const automationsPanel = () => ({
    loading: false,
    saving: false,
    document: emptyDocument(),
    entities: [],
    readableEntities: [],
    devices: [],
    runtimeState: null,
    online: false,
    csrfToken: '',
    expanded: {},
    liveHistory: {},
    historyExpanded: {},
    editing: {},
    savedDocument: null,
    lastStateAt: 0,
    hasAutomationsRole: false,
    nowTick: Date.now(),
    statusPollMs: 1000,
    locating: false,

    // Der Assistent fuehrt nur durch eine frisch angelegte Regel: Schritt 1
    // WENN, Schritt 2 DANN, Schritt 3 Feineinstellungen. Er legt kein eigenes
    // Datenmodell an - er blendet nur, was schon da ist, schrittweise ein.
    wizard: { active: false, step: 1, ruleId: '' },

    get balanceFields() {
      return BALANCE_FIELDS_BASE.map(([field, key]) => [field, t(key)]);
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
        const [doc, session] = await Promise.all([
          requestJSON('/api/v1/configurations/automation_rules'),
          requestJSON('/api/v1/auth/session').catch(() => null),
          this.loadDeviceCatalog(),
        ]);
        this.document = doc && doc.rules ? doc : emptyDocument();
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
          this._clockTimer = setInterval(() => { this.nowTick = Date.now(); }, 1000);
          // Node hat hier ein Timeout-Objekt mit unref() (haelt den Prozess
          // z.B. in Tests nicht am Leben); im Browser ist es nur eine Zahl.
          if (typeof this._clockTimer.unref === 'function') this._clockTimer.unref();
        }
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
      // entity.value ist der value_template-reduzierte Wert (rules_enabled) -
      // das Go-Registry kennt kein json_attributes_topic, deshalb steckt das
      // volle Zustandsdokument nur im rohen, ungetemplateten Payload des
      // state-Topics (last_message.payload).
      const rawDocument = stateEntity && stateEntity.last_message && stateEntity.last_message.payload;
      this.applyStateDocument(rawDocument, Boolean(stateEntity && stateEntity.available));
      const historyEntity = (automationDevice.entities || []).find((entity) => entity.object_id === 'history');
      const rawHistory = historyEntity && historyEntity.last_message && historyEntity.last_message.payload;
      this.applyHistoryDocument(rawHistory);
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

    applyStateDocument(raw, online) {
      this.online = Boolean(online);
      if (!raw) return;
      try {
        this.runtimeState = JSON.parse(raw);
        this.lastStateAt = Date.now();
      } catch (error) {
        // Ein halb geschriebenes State-Dokument darf die Ansicht nicht leeren -
        // der alte Stand bleibt stehen, bis der naechste Tick kommt.
      }
    },

    applyHistoryDocument(raw) {
      if (!raw) return;
      try {
        const parsed = JSON.parse(raw);
        this.liveHistory = parsed.rules || {};
      } catch (error) {
        // ein halb geschriebenes Dokument darf die Ansicht nicht leeren
      }
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
      return window.__automationsView.describeCondition(condition, this.entityForCondition(condition));
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
    toggleExpanded(rule) { this.expanded[rule.id] = !this.expanded[rule.id]; },

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
      const id = `regel_${Date.now()}`;
      this.document.rules.push({ id, name: t('automations.new_rule'), enabled: false, cooldown_seconds: 60, conditions: [], actions: [] });
    },

    startRuleWizard() {
      this.addRule();
      const rule = this.document.rules[this.document.rules.length - 1];
      this.wizard = { active: true, step: 1, ruleId: rule.id };
      this.expanded[rule.id] = true;
      this.editing[rule.id] = true;
    },

    wizardRule() {
      return this.document.rules.find((rule) => rule.id === this.wizard.ruleId) || null;
    },

    // Schritt 1 braucht eine Bedingung, Schritt 2 eine Aktion - genau die
    // Bedingung, an der validateRuleBeforeSave() sonst erst beim Speichern
    // scheitert. Schritt 3 hat nichts zu erzwingen.
    wizardCanAdvance() {
      const rule = this.wizardRule();
      if (!rule) return false;
      if (this.wizard.step === 1) return rule.conditions.length > 0;
      if (this.wizard.step === 2) return rule.actions.length > 0;
      return true;
    },

    wizardNext() {
      if (!this.wizardCanAdvance()) return;
      if (this.wizard.step >= 3) return this.wizardFinish();
      this.wizard.step += 1;
    },

    wizardBack() {
      if (this.wizard.step > 1) this.wizard.step -= 1;
    },

    // Beendet nur die Fuehrung - die Regel bleibt im Bearbeiten-Modus offen,
    // damit man ohne Bruch weitermachen kann. Gespeichert wird wie immer
    // ausdruecklich ueber die Werkzeugleiste.
    wizardFinish() {
      const ruleId = this.wizard.ruleId;
      this.wizard = { active: false, step: 1, ruleId: '' };
      if (ruleId) {
        this.editing[ruleId] = true;
        this.expanded[ruleId] = true;
      }
    },

    isWizardRule(rule) {
      return this.wizard.active && this.wizard.ruleId === rule.id;
    },

    // Welche Abschnitte der Regelkarte der Assistent gerade zeigt.
    wizardShows(rule, section) {
      if (!this.isWizardRule(rule)) return true;
      if (section === 'conditions') return this.wizard.step >= 1;
      if (section === 'actions') return this.wizard.step >= 2;
      if (section === 'tuning') return this.wizard.step >= 3;
      return true;
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
      const details = window.document.getElementById('automations-advanced');
      if (details) details.open = true;
      const field = window.document.getElementById('automations-latitude');
      if (field) {
        field.scrollIntoView({ block: 'center' });
        field.focus();
      }
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

    validateRuleBeforeSave(rule) {
      const errors = [];
      if (!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(rule.id || '')) errors.push(t('automations.validation.invalid_id', { name: rule.name }));
      if (!rule.conditions || rule.conditions.length < 1 || rule.conditions.length > 8) errors.push(t('automations.validation.condition_count', { name: rule.name }));
      if (!rule.actions || rule.actions.length < 1 || rule.actions.length > 8) errors.push(t('automations.validation.action_count', { name: rule.name }));
      const hasPublish = (rule.actions || []).some((action) => action.type === 'publish');
      if (hasPublish) {
        if ((rule.cooldown_seconds || 0) < 30) errors.push(t('automations.validation.cooldown_too_short', { name: rule.name }));
        for (const condition of rule.conditions || []) {
          if ('hold_seconds' in condition && condition.hold_seconds < 30) {
            errors.push(t('automations.validation.hold_seconds_too_short', { name: rule.name }));
          }
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

    validateDocumentBeforeSave() {
      const errors = (this.document.rules || []).flatMap((rule) => this.validateRuleBeforeSave(rule));
      const settings = this.document.settings || {};
      if (isNumber(settings.latitude) !== isNumber(settings.longitude)) {
        errors.push(t('automations.validation.location_incomplete'));
      }
      return errors;
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
          body: JSON.stringify({ ...this.document, settings: settingsToSave }),
        });
        this.$store.toasts.push(t('automations.saved'));
        this.savedDocument = JSON.parse(JSON.stringify(this.document));
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

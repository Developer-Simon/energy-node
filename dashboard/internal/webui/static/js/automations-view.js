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
  window.__automationsView.constants = {
    ACTION_TYPES, CONDITION_TYPES, FIELD_HELP, WEEKDAY_LABELS, WINDOW_TYPES,
    SERVICE_TEST_POLL_MS, SERVICE_TEST_TIMEOUT_MS, isNumber, requestJSON,
  };
})();

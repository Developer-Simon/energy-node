(() => {
  const view = window.__automationsView;
  const { isNumber } = view.constants;
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);

  const REGULATOR_DEFAULT_GAIN = 0.8;
  const REGULATOR_DEFAULT_DEADBAND = 20;
  // Takt, wenn der Regler keinen eigenen nennt (Dienst: regulator_interval_s).
  const REGULATE_DEFAULT_INTERVAL_S = 10;
  const HOLD_DEFAULT_REPEAT_S = 60;
  const JUST_ENDED_S = 60;
  const ID_PATTERN = /^[a-z0-9][a-z0-9_-]{0,63}$/;

  // i18n-keys: automations.scenario.status.fallback_reason.balance_stale, automations.scenario.status.fallback_reason.base_missing
  const FALLBACK_REASON_KEYS = {
    balance_stale: 'automations.scenario.status.fallback_reason.balance_stale',
    base_missing: 'automations.scenario.status.fallback_reason.base_missing',
  };

  const isEmpty = (value) => value === '' || value === undefined || value === null;

  function status(key, tone, label, detail) {
    return { key, tone, label, detail };
  }

  function sinceTime(since) {
    if (!isNumber(since) || !window.I18n) return '';
    return window.I18n.formatTime(since * 1000);
  }

  function emptyScenario(id, name) {
    return {
      id, name, enabled: false,
      when: { conditions: [] },
      on_start: { actions: [] },
      while: { mode: 'none' },
      on_end: { actions: [] },
    };
  }

  function setUntil(scenario, on) {
    if (!on) {
      delete scenario.until;
      return;
    }
    if (scenario.until) return;
    scenario.until = {
      conditions: [{ type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: 100, hysteresis: 0, hold_seconds: 300 }],
    };
  }

  // Ein Moduswechsel verwirft den alten Inhalt: der Regler und die Aktionen
  // der Wiederholung passen zu keinem anderen Modus.
  function setWhileMode(scenario, mode) {
    if (mode === 'hold') {
      scenario.while = { mode, actions: [], repeat_seconds: HOLD_DEFAULT_REPEAT_S };
    } else if (mode === 'regulate') {
      scenario.while = {
        mode,
        regulator: {
          output: { topic: '', min: 0, max: 800 },
          direction: 'producer',
          target_w: 0,
          base: { source: 'last' },
          fallback: { payload: '800' },
        },
      };
    } else {
      scenario.while = { mode: 'none' };
    }
  }

  // i18n-keys: automations.scenario.status.off, automations.scenario.status.ready, automations.scenario.status.ready_detail, automations.scenario.status.regulating, automations.scenario.status.running, automations.scenario.status.waiting, automations.scenario.status.fallback
  function scenarioStatus(scenario, state, online, nowSec) {
    if (!online) return status('offline', 'off', t('automations.service_offline'), '');
    if (!scenario.enabled || (state && state.phase === 'disabled')) {
      return status('off', 'off', t('automations.scenario.status.off'), '');
    }
    if (!state) return status('unknown', 'off', t('automations.state_unknown'), '');
    if (state.phase === 'fallback') {
      const reasonKey = FALLBACK_REASON_KEYS[state.reason];
      return status('fallback', 'bad', t('automations.scenario.status.fallback'), reasonKey ? t(reasonKey) : '');
    }
    if (state.phase === 'active') {
      const time = sinceTime(state.since);
      if (scenario.while && scenario.while.mode === 'regulate') {
        return status('regulating', 'ok', t('automations.scenario.status.regulating', { time }), '');
      }
      return status('running', 'ok', t('automations.scenario.status.running', { time }), '');
    }
    if (state.phase === 'idle') {
      const conditions = (scenario.when && scenario.when.conditions) || [];
      const reports = state.conditions || [];
      const allRaw = conditions.length > 0 && conditions.every((_, index) => Boolean(reports[index] && reports[index].raw_met));
      if (allRaw) {
        const remaining = reports.reduce((highest, report) => (
          isNumber(report && report.hold_remaining) ? Math.max(highest, report.hold_remaining) : highest
        ), 0);
        return status('ready', 'wait', t('automations.scenario.status.ready'),
          t('automations.scenario.status.ready_detail', { duration: view.formatSeconds(remaining) }));
      }
      return status('waiting', 'info', t('automations.scenario.status.waiting'), '');
    }
    return status('unknown', 'off', t('automations.state_unknown'), '');
  }

  function activePhase(state, nowSec = Date.now() / 1000) {
    if (!state) return null;
    if (state.phase === 'active' || state.phase === 'fallback') return 'while';
    if (state.phase === 'idle' && isNumber(state.since) && nowSec - state.since < JUST_ENDED_S) return 'end';
    return null;
  }

  function startProgress(scenario, state) {
    const conditions = (scenario.when && scenario.when.conditions) || [];
    const reports = (state && state.conditions) || [];
    for (let index = 0; index < conditions.length; index += 1) {
      const condition = conditions[index];
      const report = reports[index];
      if (condition.type !== 'balance_threshold' || !report || !isNumber(report.value)) continue;
      return {
        fraction: view.meterFraction(report.value, { min: 0, max: condition.threshold }),
        label: `${report.value} / ${condition.threshold} W`,
      };
    }
    return null;
  }

  // i18n-keys: automations.scenario.phase.more_conditions, automations.scenario.phase.no_start, automations.scenario.end_when_false, automations.scenario.phase.nothing_more, automations.scenario.phase.regulate_title, automations.scenario.phase.regulate_text, automations.scenario.phase.hold_text
  function conditionRow(phase, conditions, describeCondition) {
    const first = describeCondition(conditions[0]);
    const text = conditions.length > 1
      ? t('automations.scenario.phase.more_conditions', { count: conditions.length - 1 })
      : (first.summary || '');
    return { phase, icon: first.icon, title: first.title, text };
  }

  function whileRow(scenario, describeAction) {
    const current = scenario.while || {};
    if (current.mode === 'regulate' && current.regulator) {
      const regulator = current.regulator;
      const interval = isNumber(regulator.interval_s) ? regulator.interval_s : REGULATE_DEFAULT_INTERVAL_S;
      return {
        phase: 'while', icon: 'ico-grid',
        title: t('automations.scenario.phase.regulate_title', { target: regulator.target_w ?? 0 }),
        text: t('automations.scenario.phase.regulate_text', { interval }),
      };
    }
    if (current.mode === 'hold') {
      const first = (current.actions || [])[0];
      const action = first ? describeAction(first) : null;
      return {
        phase: 'while', icon: action ? action.icon : 'ico-check',
        title: action ? action.title : t('automations.scenario.phase.nothing_more'),
        text: t('automations.scenario.phase.hold_text', { seconds: current.repeat_seconds ?? HOLD_DEFAULT_REPEAT_S }),
      };
    }
    return { phase: 'while', icon: 'ico-check', title: t('automations.scenario.phase.nothing_more'), text: '' };
  }

  function endRow(scenario, describeCondition, describeAction) {
    const onEnd = (scenario.on_end && scenario.on_end.actions) || [];
    const endText = onEnd[0] ? describeAction(onEnd[0]).title : t('automations.scenario.phase.nothing_more');
    const until = (scenario.until && scenario.until.conditions) || [];
    if (until.length > 0) {
      return { ...conditionRow('end', until, describeCondition), text: endText };
    }
    return { phase: 'end', icon: 'ico-power-off', title: t('automations.scenario.end_when_false'), text: endText };
  }

  // i18n-keys: automations.scenario.phase.no_start
  function phaseSummary(scenario, describeCondition, describeAction) {
    const when = (scenario.when && scenario.when.conditions) || [];
    const startRow = when.length > 0
      ? conditionRow('start', when, describeCondition)
      : { phase: 'start', icon: 'ico-warning', title: t('automations.scenario.phase.no_start'), text: '' };
    return [startRow, whileRow(scenario, describeAction), endRow(scenario, describeCondition, describeAction)];
  }

  function hasToggleAction(actions) {
    return (actions || []).some((action) => action.type === 'publish' && action.payload_source === 'toggle');
  }

  function hasPublishAction(actions) {
    return (actions || []).some((action) => action.type === 'publish');
  }

  // i18n-keys: automations.validation.scenario_invalid_id, automations.validation.scenario_condition_count, automations.validation.scenario_until_count, automations.validation.scenario_action_count, automations.validation.scenario_hold_actions, automations.validation.scenario_repeat_with_toggle, automations.validation.scenario_repeat_too_short, automations.validation.scenario_regulator_topic, automations.validation.scenario_regulator_topic_taken, automations.validation.scenario_regulator_range, automations.validation.scenario_gain_range, automations.validation.scenario_regulator_fallback
  function validateScenarioBeforeSave(scenario, ctx = {}) {
    const errors = [];
    const name = scenario.name;
    const minInterval = ctx.minInterval || 0;
    const otherOutputs = ctx.otherOutputs || [];
    const countOk = (list) => Boolean(list) && list.length >= 1 && list.length <= 8;

    if (!ID_PATTERN.test(scenario.id || '')) errors.push(t('automations.validation.scenario_invalid_id', { name }));
    if (!countOk((scenario.when || {}).conditions)) errors.push(t('automations.validation.scenario_condition_count', { name }));
    if (scenario.until && !countOk(scenario.until.conditions)) errors.push(t('automations.validation.scenario_until_count', { name }));

    const phaseActions = [(scenario.on_start || {}).actions, (scenario.on_end || {}).actions];
    if (phaseActions.some((actions) => (actions || []).length > 8)) {
      errors.push(t('automations.validation.scenario_action_count', { name }));
    }

    const current = scenario.while || {};
    if (current.mode === 'hold') {
      const actions = current.actions || [];
      if (!countOk(actions)) errors.push(t('automations.validation.scenario_hold_actions', { name }));
      const repeat = current.repeat_seconds || 0;
      if (hasToggleAction(actions)) {
        errors.push(t('automations.validation.scenario_repeat_with_toggle', { name }));
      } else if (hasPublishAction(actions) && repeat < minInterval) {
        errors.push(t('automations.validation.scenario_repeat_too_short', { name, min: minInterval }));
      }
    }

    if (current.mode === 'regulate') {
      const regulator = current.regulator || {};
      const output = regulator.output || {};
      if (isEmpty(output.topic) || output.topic === '') {
        errors.push(t('automations.validation.scenario_regulator_topic', { name }));
      } else if (otherOutputs.includes(output.topic)) {
        errors.push(t('automations.validation.scenario_regulator_topic_taken', { name }));
      }
      if (!(output.min < output.max)) errors.push(t('automations.validation.scenario_regulator_range', { name }));
      if (!isEmpty(regulator.gain) && !(regulator.gain >= 0.1 && regulator.gain <= 1)) {
        errors.push(t('automations.validation.scenario_gain_range', { name }));
      }
      const fallbackPayload = regulator.fallback ? regulator.fallback.payload : undefined;
      if (regulator.direction === 'producer' && (isEmpty(fallbackPayload) || String(fallbackPayload).trim() === '')) {
        errors.push(t('automations.validation.scenario_regulator_fallback', { name }));
      }
    }
    return errors;
  }

  // Der Dienst setzt diese Werte ohnehin, beim Speichern bleiben sie daher
  // weg. Die Kopie laesst das Original unveraendert.
  function stripRegulatorDefaults(regulator) {
    const out = { ...regulator };
    if (out.output) {
      out.output = { ...out.output };
      for (const key of ['step', 'decimals']) {
        if (isEmpty(out.output[key])) delete out.output[key];
      }
    }
    if (out.gain === REGULATOR_DEFAULT_GAIN) delete out.gain;
    if (out.deadband_w === REGULATOR_DEFAULT_DEADBAND) delete out.deadband_w;
    for (const key of ['max_change_w', 'interval_s']) {
      if (isEmpty(out[key])) delete out[key];
    }
    if (out.direction === 'consumer' && (isEmpty(out.fallback) || isEmpty(out.fallback.payload))) {
      delete out.fallback;
    }
    return out;
  }

  window.__automationScenario = {
    emptyScenario, setUntil, setWhileMode, scenarioStatus, activePhase,
    startProgress, phaseSummary, validateScenarioBeforeSave, stripRegulatorDefaults,
  };
})();

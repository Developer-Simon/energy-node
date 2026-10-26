(() => {
  // Vorlagen des Automations-Assistenten. Jede Vorlage beschreibt ihre Schritte
  // (Antworten), baut daraus einen Eintrag im Format der Dienstvalidierung und
  // fasst die Antworten als Saetze fuer die Zusammenfassung zusammen. Alle Texte
  // kommen aus dem Katalog: automations.templates.<id>.* und automations.assistant.*.
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const num = (value) => (window.I18n ? window.I18n.formatNumber(value) : String(value));
  const { uniqueId, ctToPriceUnit } = window.__automationWizard;

  const ALL_DAYS = [0, 1, 2, 3, 4, 5, 6];

  // Balance-Schwelle ohne Hysterese, wie die Bedingungen des Editors.
  const B = (field, comparison, threshold, holdSeconds) => ({
    type: 'balance_threshold', field, comparison, threshold, hysteresis: 0, hold_seconds: holdSeconds,
  });

  // Schalter ein oder aus, dieselbe Form wie buildSwitchAction in automations.page.js.
  const Sw = (entity, on) => ({
    type: 'publish', topic: entity.command_topic, retain: false, payload_source: 'constant',
    payload: on ? entity.payload_on : entity.payload_off, entity_id: entity.unique_id,
  });

  // Sollwert als konstante Nutzlast auf das Stellglied.
  const setpoint = (entity, payload) => ({
    type: 'publish', topic: entity.command_topic, retain: false, payload_source: 'constant',
    payload, entity_id: entity.unique_id,
  });

  // Stellglied als Regler-Ausgabe. step nur, wenn es groesser als 0 ist.
  const outputOf = (entity) => {
    const output = { topic: entity.command_topic, entity_id: entity.unique_id, min: entity.min_value, max: entity.max_value };
    if (typeof entity.step === 'number' && entity.step > 0) output.step = entity.step;
    return output;
  };

  // Messwert als Regler-Basis aus einer lesbaren Entitaet.
  const entityBase = (entity) => ({
    source: 'entity', entity_id: entity.unique_id, topic: entity.state_topic,
    value_template: entity.value_template || '',
  });

  // Wochentage sortiert, alle sieben als leere Liste (= jeden Tag).
  const weekdaysOf = (days) => {
    const sorted = [...(days || [])].sort((a, b) => a - b);
    return sorted.length === ALL_DAYS.length ? [] : sorted;
  };

  const daysText = (days) => {
    if (!days || days.length === 0 || days.length === ALL_DAYS.length) return t('automations.weekdays_daily');
    return [...days].sort((a, b) => a - b).map((day) => t(`automations.weekday.${day}`)).join(', ');
  };

  const requireEntry = (list, id) => {
    const entry = (list || []).find((item) => item.id === id);
    if (!entry) throw new Error(`automation template: device not found: ${id}`);
    return entry;
  };

  const requireEntity = (entry) => {
    if (!entry.entity) throw new Error(`automation template: no writable entity for ${entry.id}`);
    return entry.entity;
  };

  const sunUnavailable = (ctx) => (ctx.hasLocation ? '' : t('automations.assistant.unavailable.location'));
  const socUnavailable = (ctx) => (ctx.socAvailable ? '' : t('automations.assistant.unavailable.soc'));

  const phase = (name) => t(`automations.assistant.summary.phase.${name}`);

  // ---------------------------------------------------------------- Ueberschuss in die Wallbox

  const surplusWallbox = {
    id: 'surplus_wallbox',
    category: 'charging',
    icon: 'ico-wallbox',
    requires: { devices: 'wallbox' },
    steps: [
      { id: 'device', type: 'device', filter: 'wallbox' },
      {
        id: 'mode', type: 'tiles', default: 'sun',
        options: [
          { id: 'sun' },
          { id: 'min', unavailable: sunUnavailable },
          { id: 'battery', unavailable: socUnavailable },
        ],
      },
      { id: 'soc', type: 'number', min: 10, max: 90, step: 5, default: 40, when: (answers) => answers.mode === 'battery' },
      {
        id: 'start', type: 'presets', step: 100, default: 1400, effect: 'starts',
        options: [{ id: 1400 }, { id: 2000 }, { id: 3000 }],
      },
      {
        id: 'extras', type: 'multi',
        options: [
          {
            id: 'connected', default: true,
            unavailable: (ctx, answers) => (requireEntry(ctx.wallboxes, answers.device).plugEntity
              ? '' : t('automations.assistant.unavailable.plug')),
          },
          { id: 'history', default: true },
        ],
      },
    ],
    build(answers, ctx) {
      const entry = requireEntry(ctx.wallboxes, answers.device);
      const entity = requireEntity(entry);
      const plug = entry.plugEntity;
      const switchEntity = entry.switchEntity;

      const when = [B('grid_export', 'above', answers.start, 180)];
      if (answers.extras.connected && plug) {
        when.push({
          type: 'entity_value', topic: plug.state_topic, entity_id: plug.unique_id,
          value_template: plug.value_template || '', comparison: 'equals', text: plug.payload_on || 'ON',
        });
      }

      let until;
      if (answers.mode === 'sun') {
        until = B('grid_import', 'above', 200, 300);
      } else if (answers.mode === 'min') {
        until = { type: 'sun_window', from: 'sunset', to: 'sunrise', from_offset_min: 0, to_offset_min: 0 };
      } else {
        until = B('battery_soc', 'below', answers.soc, 60);
      }

      const regulator = {
        output: outputOf(entity),
        direction: 'consumer',
        target_w: 0,
        deadband_w: 100,
        base: entry.measured ? entityBase(entry.measured) : { source: 'last' },
        fallback: { payload: String(entity.min_value) },
      };

      const scenario = {
        id: uniqueId('surplus_wallbox', ctx.usedIds),
        name: t('automations.templates.surplus_wallbox.title'),
        enabled: true,
        template: 'surplus_wallbox',
        history_enabled: !!answers.extras.history,
        when: { conditions: when },
        until: { conditions: [until] },
        on_start: { actions: switchEntity ? [Sw(switchEntity, true)] : [] },
        on_end: { actions: switchEntity ? [Sw(switchEntity, false)] : [setpoint(entity, String(entity.min_value))] },
        while: { mode: 'regulate', regulator },
      };
      return { kind: 'scenario', entry: scenario };
    },
    summary(answers, ctx) {
      const entry = requireEntry(ctx.wallboxes, answers.device);
      const entity = requireEntity(entry);
      const rows = [
        {
          label: phase('when'),
          text: t('automations.assistant.summary.wallbox.when', { minutes: 3, start: num(answers.start) }),
          step: 'start',
        },
      ];
      if (answers.extras.connected && entry.plugEntity) {
        rows.push({ label: phase('when'), text: t('automations.assistant.summary.wallbox.connected'), step: 'extras' });
      }
      rows.push({
        label: phase('run'),
        text: t('automations.assistant.summary.wallbox.run', { min: num(entity.min_value) }),
        step: 'device',
      });
      if (answers.mode === 'sun') {
        rows.push({
          label: phase('until'),
          text: t('automations.assistant.summary.wallbox.until_sun', { minutes: 5, value: num(200) }),
          step: 'mode',
        });
      } else if (answers.mode === 'min') {
        rows.push({ label: phase('until'), text: t('automations.assistant.summary.wallbox.until_min'), step: 'mode' });
      } else {
        rows.push({
          label: phase('until'),
          text: t('automations.assistant.summary.wallbox.until_battery', { value: num(answers.soc) }),
          step: 'soc',
        });
      }
      if (entry.switchEntity) {
        rows.push({ label: phase('start'), text: t('automations.assistant.summary.wallbox.start_on'), step: 'device' });
        rows.push({ label: phase('end'), text: t('automations.assistant.summary.wallbox.end_off'), step: 'device' });
      } else {
        rows.push({
          label: phase('end'),
          text: t('automations.assistant.summary.wallbox.end_min', { min: num(entity.min_value) }),
          step: 'device',
        });
      }
      return rows;
    },
  };

  // ---------------------------------------------------------------- Nulleinspeisung

  // Messwert des Reglers: eigener Messwert des Wechselrichters, Netzbilanz oder letzter Sollwert.
  const measureBase = (measure, entry) => {
    if (measure === 'balance') return { source: 'balance', field: 'pv' };
    if (measure === 'last') return { source: 'last' };
    if (!entry.measured) throw new Error(`automation template: no own reading for ${entry.id}`);
    return entityBase(entry.measured);
  };

  const feedInWhen = (answers) => {
    if (answers.period === 'sun') {
      return { type: 'sun_window', from: 'sunrise', to: 'sunset', from_offset_min: 0, to_offset_min: 0 };
    }
    if (answers.period === 'fixed') {
      return {
        type: 'time_window', start: answers.window.from, end: answers.window.to,
        weekdays: weekdaysOf(answers.window.days),
      };
    }
    return { type: 'time_window', start: '00:00', end: '00:00', weekdays: [] };
  };

  const periodText = (answers) => {
    if (answers.period === 'sun') {
      return t('automations.assistant.summary.sun_window', {
        from: t('automations.sun_event.sunrise'),
        to: t('automations.sun_event.sunset'),
      });
    }
    if (answers.period === 'fixed') {
      return t('automations.assistant.summary.fixed', {
        days: daysText(answers.window.days),
        from: answers.window.from,
        to: answers.window.to,
      });
    }
    return t('automations.assistant.summary.always');
  };

  const zeroFeed = {
    id: 'zero_feed',
    category: 'feed_in',
    icon: 'ico-inverter',
    requires: { devices: 'inverter' },
    steps: [
      { id: 'device', type: 'device', filter: 'inverter' },
      {
        id: 'measure', type: 'choice', default: 'own',
        options: [
          {
            id: 'own',
            unavailable: (ctx, answers) => (requireEntry(ctx.inverters, answers.device).measured
              ? '' : t('automations.assistant.unavailable.measured')),
          },
          { id: 'balance' },
          { id: 'last' },
        ],
      },
      {
        id: 'target', type: 'presets', step: 5, default: 30, effect: 'band', options: [{ id: 60 }, { id: 30 }, { id: 10 }],
        tolerance: { default: 20, min: 5, max: 60 },
      },
      {
        id: 'period', type: 'tiles', default: 'sun',
        options: [
          { id: 'sun', unavailable: sunUnavailable },
          { id: 'always' },
          { id: 'fixed' },
        ],
      },
      {
        id: 'window', type: 'time', when: (answers) => answers.period === 'fixed',
        default: { from: '08:00', to: '18:00', days: ALL_DAYS },
      },
      {
        id: 'extras', type: 'multi',
        options: [{ id: 'reset', default: true }, { id: 'notify', default: false }, { id: 'history', default: true }],
      },
    ],
    build(answers, ctx) {
      const entry = requireEntry(ctx.inverters, answers.device);
      const entity = requireEntity(entry);
      const max = entity.max_value;

      const scenario = {
        id: uniqueId('zero_feed', ctx.usedIds),
        name: t('automations.templates.zero_feed.title'),
        enabled: true,
        template: 'zero_feed',
        history_enabled: !!answers.extras.history,
        when: { conditions: [feedInWhen(answers)] },
        on_start: {
          actions: answers.extras.notify
            ? [{
              type: 'notification', severity: 'info',
              title: t('automations.templates.zero_feed.notice.title'),
              message: t('automations.templates.zero_feed.notice.message'),
            }]
            : [],
        },
        on_end: { actions: answers.extras.reset ? [setpoint(entity, String(max))] : [] },
        while: {
          mode: 'regulate',
          regulator: {
            output: { ...outputOf(entity), decimals: 0 },
            direction: 'producer',
            target_w: answers.target,
            deadband_w: Number.isFinite(answers.tolerance) ? answers.tolerance : 20,
            base: measureBase(answers.measure, entry),
            fallback: { payload: String(max) },
          },
        },
      };
      return { kind: 'scenario', entry: scenario };
    },
    summary(answers, ctx) {
      const entry = requireEntry(ctx.inverters, answers.device);
      const entity = requireEntity(entry);
      const rows = [
        { label: phase('when'), text: periodText(answers), step: 'period' },
        {
          label: phase('run'),
          text: t(`automations.assistant.summary.measure.${answers.measure}`),
          step: 'measure',
        },
        {
          label: phase('run'),
          text: t('automations.assistant.summary.regulate', { target: num(answers.target) }),
          step: 'target',
        },
      ];
      if (answers.extras.notify) {
        rows.push({ label: phase('start'), text: t('automations.assistant.summary.notify'), step: 'extras' });
      }
      if (answers.extras.reset) {
        rows.push({
          label: phase('end'),
          text: t('automations.assistant.summary.reset', { value: num(entity.max_value) }),
          step: 'extras',
        });
      }
      return rows;
    },
  };

  // ---------------------------------------------------------------- Gerät bei Überschuss

  const surplusSwitch = {
    id: 'surplus_switch',
    category: 'switching',
    icon: 'ico-switch',
    requires: { devices: 'switch' },
    steps: [
      { id: 'device', type: 'device', filter: 'switch' },
      {
        id: 'on', type: 'presets', step: 100, default: 1500, effect: 'starts',
        options: [{ id: 1000 }, { id: 1500 }, { id: 2500 }],
      },
      {
        id: 'off', type: 'tiles', default: 'import',
        options: [
          { id: 'import' },
          { id: 'runtime' },
          { id: 'sunset', unavailable: sunUnavailable },
        ],
      },
      {
        id: 'runtime', type: 'number', min: 10, max: 600, step: 10, default: 120,
        when: (answers) => answers.off === 'runtime',
      },
      {
        id: 'extras', type: 'multi',
        options: [
          { id: 'weekdays', default: false, reveal: { default: [0, 1, 2, 3, 4] } },
          { id: 'repeat', default: true },
        ],
      },
    ],
    build(answers, ctx) {
      const entry = requireEntry(ctx.switches, answers.device);
      const entity = requireEntity(entry);
      const repeat = !!answers.extras.repeat;

      const when = [B('grid_export', 'above', answers.on, 120)];
      if (answers.extras.weekdays) {
        when.push({
          type: 'time_window', start: '00:00', end: '00:00', weekdays: weekdaysOf(answers.extras.weekdays_value),
        });
      }

      const scenario = {
        id: uniqueId('surplus_switch', ctx.usedIds),
        name: t('automations.templates.surplus_switch.title'),
        enabled: true,
        template: 'surplus_switch',
        when: { conditions: when },
        on_start: { actions: repeat ? [] : [Sw(entity, true)] },
        on_end: { actions: [Sw(entity, false)] },
        while: repeat
          ? { mode: 'hold', actions: [Sw(entity, true)], repeat_seconds: 60 }
          : { mode: 'none' },
      };
      // Ende nur ueber die Zeit (feste Laufzeit) oder ueber eine Bedingung.
      if (answers.off === 'runtime') {
        scenario.max_duration_s = answers.runtime * 60;
      } else if (answers.off === 'sunset') {
        scenario.until = {
          conditions: [{ type: 'sun_window', from: 'sunset', to: 'sunrise', from_offset_min: 0, to_offset_min: 0 }],
        };
      } else {
        scenario.until = { conditions: [B('grid_import', 'above', 100, 120)] };
      }
      return { kind: 'scenario', entry: scenario };
    },
    summary(answers) {
      const rows = [{
        label: phase('when'),
        text: t('automations.assistant.summary.switch.when', { minutes: 2, value: num(answers.on) }),
        step: 'on',
      }];
      if (answers.extras.weekdays) {
        rows.push({
          label: phase('when'),
          text: t('automations.assistant.summary.switch.days', { days: daysText(answers.extras.weekdays_value) }),
          step: 'extras',
        });
      }
      if (answers.extras.repeat) {
        rows.push({ label: phase('run'), text: t('automations.assistant.summary.switch.repeat'), step: 'extras' });
      } else {
        rows.push({ label: phase('start'), text: t('automations.assistant.summary.switch.start_on'), step: 'device' });
      }
      if (answers.off === 'runtime') {
        rows.push({
          label: phase('until'),
          text: t('automations.assistant.summary.switch.until_runtime', { minutes: num(answers.runtime) }),
          step: 'runtime',
        });
      } else if (answers.off === 'sunset') {
        rows.push({ label: phase('until'), text: t('automations.assistant.summary.switch.until_sunset'), step: 'off' });
      } else {
        rows.push({
          label: phase('until'),
          text: t('automations.assistant.summary.switch.until_import', { minutes: 2, value: num(100) }),
          step: 'off',
        });
      }
      rows.push({ label: phase('end'), text: t('automations.assistant.summary.switch.end_off'), step: 'device' });
      return rows;
    },
  };

  // ---------------------------------------------------------------- Akku-Warnung

  const batteryWarn = {
    id: 'battery_warn',
    category: 'warning',
    icon: 'ico-battery',
    requires: { soc: true },
    steps: [
      { id: 'below', type: 'presets', step: 5, default: 20, effect: 'soc', options: [{ id: 10 }, { id: 20 }, { id: 30 }] },
      {
        id: 'severity', type: 'tiles', default: 'warning',
        options: [{ id: 'info' }, { id: 'warning' }, { id: 'critical' }],
      },
    ],
    build(answers, ctx) {
      const rule = {
        id: uniqueId('battery_warn', ctx.usedIds),
        name: t('automations.templates.battery_warn.title'),
        enabled: true,
        cooldown_seconds: 3600,
        conditions: [{ ...B('battery_soc', 'below', answers.below, 60), hysteresis: 2 }],
        actions: [{
          type: 'notification',
          severity: answers.severity,
          title: t('automations.templates.battery_warn.notice.title'),
          message: t('automations.templates.battery_warn.notice.message', { value: num(answers.below) }),
        }],
      };
      return { kind: 'rule', entry: rule };
    },
    summary(answers) {
      return [
        {
          label: phase('when'),
          text: t('automations.assistant.summary.battery.when', { value: num(answers.below) }),
          step: 'below',
        },
        {
          label: phase('then'),
          text: t('automations.assistant.summary.battery.then', {
            severity: t(`automations.templates.battery_warn.steps.severity.options.${answers.severity}.title`),
          }),
          step: 'severity',
        },
      ];
    },
  };

  // ---------------------------------------------------------------- Schalter nach Uhrzeit

  const timeSwitch = {
    id: 'time_switch',
    category: 'time',
    icon: 'ico-clock',
    requires: { devices: 'switch' },
    steps: [
      { id: 'device', type: 'device', filter: 'switch' },
      { id: 'window', type: 'time', default: { from: '11:00', to: '15:00', days: ALL_DAYS } },
    ],
    build(answers, ctx) {
      const entry = requireEntry(ctx.switches, answers.device);
      const entity = requireEntity(entry);
      const scenario = {
        id: uniqueId('time_switch', ctx.usedIds),
        name: t('automations.templates.time_switch.title'),
        enabled: true,
        template: 'time_switch',
        when: {
          conditions: [{
            type: 'time_window', start: answers.window.from, end: answers.window.to,
            weekdays: weekdaysOf(answers.window.days),
          }],
        },
        while: { mode: 'none' },
        on_start: { actions: [Sw(entity, true)] },
        on_end: { actions: [Sw(entity, false)] },
      };
      return { kind: 'scenario', entry: scenario };
    },
    summary(answers) {
      return [
        {
          label: phase('when'),
          text: t('automations.assistant.summary.time.when', { from: answers.window.from, to: answers.window.to }),
          step: 'window',
        },
        {
          label: phase('when'),
          text: t('automations.assistant.summary.switch.days', { days: daysText(answers.window.days) }),
          step: 'window',
        },
        { label: phase('start'), text: t('automations.assistant.summary.switch.start_on'), step: 'device' },
        { label: phase('end'), text: t('automations.assistant.summary.switch.end_off'), step: 'device' },
      ];
    },
  };

  // ---------------------------------------------------------------- Waermepumpe anheben

  // Aktueller Sollwert als Zahl, NaN ohne Stellglied oder ohne Wert.
  const currentSetpoint = (entry) => {
    const value = entry.setpoint ? entry.setpoint.value : undefined;
    if (value === undefined || value === null || value === '') return NaN;
    return Number(value);
  };
  const hasSetpoint = (entry) => Number.isFinite(currentSetpoint(entry));

  // Eigener Schalter der Wärmepumpe, zuerst vorgewählt im Kontakt-Schritt.
  const ownSwitchOf = (ctx, deviceId) => {
    const entry = (ctx.heatPumps || []).find((item) => item.id === deviceId);
    return entry && entry.ownSwitch ? entry.ownSwitch.unique_id : '';
  };

  const heatPump = {
    id: 'heat_pump',
    category: 'heating',
    icon: 'ico-heatpump',
    requires: { devices: 'heat_pump' },
    steps: [
      { id: 'device', type: 'device', filter: 'heat_pump' },
      {
        id: 'control', type: 'tiles', default: 'sg',
        options: [
          {
            id: 'sg',
            unavailable: (ctx) => (ctx.switches.length ? '' : t('automations.assistant.unavailable.switch')),
            badge: (ctx, answers) => (ownSwitchOf(ctx, answers.device)
              ? t('automations.templates.heat_pump.steps.control.options.sg.badge') : ''),
            textField: (ctx, answers) => (ownSwitchOf(ctx, answers.device) ? 'text_own' : 'text'),
          },
          {
            id: 'setpoint',
            unavailable: (ctx, answers) => (hasSetpoint(requireEntry(ctx.heatPumps, answers.device))
              ? '' : t('automations.assistant.unavailable.heat_pump_setpoint')),
          },
        ],
      },
      {
        id: 'contact', type: 'device', filter: 'switch', when: (answers) => answers.control === 'sg',
        default: (ctx, answers) => ownSwitchOf(ctx, answers.device),
        first: (ctx, answers) => ownSwitchOf(ctx, answers.device),
      },
      {
        id: 'lift', type: 'number', min: 1, max: 10, step: 1, default: 5,
        when: (answers) => answers.control === 'setpoint',
      },
      {
        id: 'start', type: 'presets', step: 100, default: 2500, effect: 'surplus',
        options: [{ id: 1500 }, { id: 2500 }, { id: 3500 }],
      },
      {
        id: 'battery', type: 'tiles', default: 'first',
        options: [
          { id: 'first', reveal: { id: 'soc', default: 80, min: 20, max: 100, step: 5 } },
          { id: 'parallel' },
        ],
        when: (answers, ctx) => !ctx || !socUnavailable(ctx),
      },
      {
        id: 'protect', type: 'numbers',
        fields: [
          { id: 'run', min: 10, max: 120, step: 5, default: 30 },
          { id: 'pause', min: 5, max: 60, step: 5, default: 20 },
        ],
      },
      {
        id: 'period', type: 'tiles', default: 'sun',
        options: [
          { id: 'sun', unavailable: sunUnavailable },
          { id: 'always' },
          { id: 'fixed' },
        ],
      },
      {
        id: 'window', type: 'time', when: (answers) => answers.period === 'fixed',
        default: { from: '10:00', to: '17:00', days: ALL_DAYS },
      },
      {
        id: 'extras', type: 'multi',
        options: [{ id: 'notify', default: false }, { id: 'history', default: true }],
      },
    ],
    build(answers, ctx) {
      const entry = requireEntry(ctx.heatPumps, answers.device);
      const start = answers.start;
      const { run, pause } = answers.protect;

      // Ohne Ladestand-Signal entfällt der Akku-Schritt, also auch seine Bedingung.
      const battery = answers.battery === 'first' && !socUnavailable(ctx);
      const when = [B('grid_export', 'above', start, 300)];
      if (battery) when.push(B('battery_soc', 'above', answers.soc, 0));
      when.push(feedInWhen(answers));

      // Ende, sobald der Überschuss zehn Minuten unter dem Haltewert liegt (Wolken).
      const until = [B('grid_export', 'below', Math.round((start * 0.4) / 100) * 100, 600)];

      let onStart;
      let onEnd;
      if (answers.control === 'sg') {
        const contact = requireEntity(requireEntry(ctx.switches, answers.contact));
        onStart = [Sw(contact, true)];
        onEnd = [Sw(contact, false)];
      } else {
        if (!hasSetpoint(entry)) throw new Error(`automation template: no setpoint for ${entry.id}`);
        const setpointEntity = entry.setpoint;
        // Kein Gedaechtnis im Dienst: zurueck auf den Wert, der beim Anlegen galt.
        const now = currentSetpoint(entry);
        onStart = [setpoint(setpointEntity, String(now + answers.lift))];
        onEnd = [setpoint(setpointEntity, String(now))];
      }
      if (answers.extras.notify) {
        onStart.push({
          type: 'notification', severity: 'info',
          title: t('automations.templates.heat_pump.notice.title'),
          message: t('automations.templates.heat_pump.notice.message'),
        });
      }

      const scenario = {
        id: uniqueId('heat_pump', ctx.usedIds),
        name: t('automations.templates.heat_pump.title'),
        enabled: true,
        template: 'heat_pump',
        history_enabled: !!answers.extras.history,
        when: { conditions: when },
        until: { conditions: until },
        on_start: { actions: onStart },
        on_end: { actions: onEnd },
        while: { mode: 'none' },
        min_run_s: run * 60,
        cooldown_s: pause * 60,
      };
      return { kind: 'scenario', entry: scenario };
    },
    summary(answers, ctx) {
      const entry = requireEntry(ctx.heatPumps, answers.device);
      const start = answers.start;
      const off = Math.round((start * 0.4) / 100) * 100;
      const contact = answers.control === 'sg';
      const now = contact ? 0 : currentSetpoint(entry);
      let when = t('automations.assistant.summary.heat_pump.when', { start: num(start), minutes: 5 });
      if (answers.battery === 'first' && !socUnavailable(ctx)) {
        when += t('automations.assistant.summary.heat_pump.when_battery', { value: num(answers.soc) });
      }
      if (answers.period === 'sun') when += t('automations.assistant.summary.heat_pump.when_sun');
      return [
        {
          label: phase('start'),
          text: contact
            ? t('automations.assistant.summary.heat_pump.start_contact')
            : t('automations.assistant.summary.heat_pump.start_setpoint', { now: num(now), target: num(now + answers.lift) }),
          step: 'control',
        },
        { label: phase('when'), text: when, step: 'start' },
        {
          label: phase('run'),
          text: t('automations.assistant.summary.heat_pump.run', { min: num(answers.protect.run), value: num(off) }),
          step: 'protect',
        },
        {
          label: phase('end'),
          text: (contact
            ? t('automations.assistant.summary.heat_pump.end_contact')
            : t('automations.assistant.summary.heat_pump.end_setpoint', { now: num(now) }))
            + t('automations.assistant.summary.heat_pump.pause', { min: num(answers.protect.pause) }),
          step: 'control',
        },
      ];
    },
  };

  // ---------------------------------------------------------------- Akku laden bei Niedrigtarif

  // Wert eines Stellglieds als Zahl, NaN ohne Wert.
  const valueOf = (entity) => {
    const value = entity ? entity.value : undefined;
    if (value === undefined || value === null || value === '') return NaN;
    return Number(value);
  };

  // Satz zum Ladefortschritt: Energie von Ladestand bis Ziel und Dauer bei der Leistung.
  // Ohne Kapazität, Ladestand oder Leistung gibt es keinen Satz.
  function chargeFit({ soc, target, power, capacity }) {
    if (!Number.isFinite(soc) || !Number.isFinite(target) || !(power > 0)) return '';
    if (!Number.isFinite(capacity) || capacity <= 0) return '';
    const kwh = Math.max(0, ((target - soc) / 100) * capacity);
    const minutes = Math.round((kwh / (power / 1000)) * 60);
    return t('automations.templates.tariff.steps.charge.fit', {
      soc, target, kwh: window.I18n ? window.I18n.formatNumber(kwh, 1) : kwh.toFixed(1), power, h: Math.floor(minutes / 60), m: minutes % 60,
    });
  }

  const batteryOf = (ctx, answers) => (ctx.batteries || []).find((item) => item.id === answers.battery) || {};

  const tariff = {
    id: 'tariff',
    category: 'tariff',
    icon: 'ico-battery',
    requires: { devices: 'battery' },
    steps: [
      {
        id: 'source', type: 'tiles', default: 'fixed',
        options: [
          { id: 'fixed' },
          { id: 'price', unavailable: (ctx) => (ctx.prices.length ? '' : t('automations.assistant.unavailable.price')) },
        ],
      },
      {
        id: 'window', type: 'time', when: (answers) => answers.source === 'fixed',
        default: { from: '22:00', to: '06:00', days: ALL_DAYS },
      },
      { id: 'price', type: 'device', filter: 'price', when: (answers) => answers.source === 'price' },
      {
        id: 'thr', type: 'presets', step: 0.5, default: 15, effect: 'price',
        options: [{ id: 10 }, { id: 15 }, { id: 20 }],
        when: (answers) => answers.source === 'price',
      },
      { id: 'battery', type: 'device', filter: 'battery' },
      {
        id: 'charge', type: 'numbers',
        fields: [
          { id: 'target', min: 30, max: 100, step: 5, default: 80 },
          { id: 'power', min: 500, max: 5000, step: 250, default: 2000 },
        ],
        // Hinweis unter dem Schritt: Energie und Dauer aus dem Ladestand des Kontexts.
        hint: (answers, ctx) => chargeFit({
          soc: ctx.snapshot && ctx.snapshot.values ? ctx.snapshot.values.battery_soc : NaN,
          capacity: ctx.snapshot && ctx.snapshot.values ? ctx.snapshot.values.battery_capacity_kwh : NaN,
          target: answers.charge.target,
          power: answers.charge.power,
        }),
      },
      {
        id: 'extras', type: 'multi',
        options: [
          {
            id: 'block', default: true,
            unavailable: (ctx, answers) => (batteryOf(ctx, answers).dischargeLimit
              ? '' : t('automations.assistant.unavailable.discharge_limit')),
          },
          { id: 'only', default: false },
          { id: 'notify', default: false },
          { id: 'history', default: true },
        ],
      },
    ],
    build(answers, ctx) {
      const battery = requireEntry(ctx.batteries, answers.battery);
      if (!battery.setpoint) throw new Error(`automation template: no setpoint for ${battery.id}`);
      if (!Number.isFinite(valueOf(battery.setpoint))) throw new Error(`automation template: no value for ${battery.id}`);
      const { target, power } = answers.charge;
      const setpointEntity = battery.setpoint;

      const when = [];
      if (answers.source === 'fixed') {
        when.push({
          type: 'time_window', start: answers.window.from, end: answers.window.to, weekdays: weekdaysOf(answers.window.days),
        });
      } else {
        const price = requireEntry(ctx.prices, answers.price).entity;
        when.push({
          type: 'entity_value', topic: price.state_topic, entity_id: price.unique_id,
          value_template: price.value_template || '', comparison: 'below',
          value: ctToPriceUnit(answers.thr, price.unit_of_measurement), hold_seconds: 0,
        });
      }
      if (answers.extras.only) when.push(B('battery_soc', 'below', 30, 0));
      when.push(B('battery_soc', 'below', target, 0));

      // Beim Start Ladeleistung oder Ladestand setzen, am Ende den beim Anlegen gefundenen Wert zurück.
      const percent = setpointEntity.unit_of_measurement === '%';
      const onStart = [setpoint(setpointEntity, percent ? String(target) : String(power))];
      const onEnd = [setpoint(setpointEntity, String(valueOf(setpointEntity)))];
      if (answers.extras.block) {
        const limit = battery.dischargeLimit;
        if (!limit) throw new Error(`automation template: no discharge limit for ${battery.id}`);
        if (!Number.isFinite(valueOf(limit))) throw new Error(`automation template: no value for ${limit.unique_id}`);
        onStart.push(setpoint(limit, '0'));
        onEnd.push(setpoint(limit, String(valueOf(limit))));
      }
      if (answers.extras.notify) {
        onStart.push({
          type: 'notification', severity: 'info',
          title: t('automations.templates.tariff.notice.title'),
          message: t('automations.templates.tariff.notice.message'),
        });
      }

      // until entfällt: Das Szenario endet, sobald eine when-Bedingung nicht mehr gilt.
      const scenario = {
        id: uniqueId('tariff', ctx.usedIds),
        name: t('automations.templates.tariff.title'),
        enabled: true,
        template: 'tariff',
        history_enabled: !!answers.extras.history,
        when: { conditions: when },
        on_start: { actions: onStart },
        on_end: { actions: onEnd },
        while: { mode: 'none' },
      };
      return { kind: 'scenario', entry: scenario };
    },
    summary(answers, ctx) {
      const battery = requireEntry(ctx.batteries, answers.battery);
      const fixed = answers.source === 'fixed';
      const when = fixed
        ? t('automations.assistant.summary.tariff.when_fixed', { from: answers.window.from, to: answers.window.to })
        : t('automations.assistant.summary.tariff.when_price', {
          name: requireEntry(ctx.prices, answers.price).deviceName, thr: num(answers.thr),
        });
      const block = answers.extras.block;
      return [
        {
          label: phase('when'),
          text: when + (answers.extras.only ? t('automations.assistant.summary.tariff.only') : ''),
          step: fixed ? 'window' : 'thr',
        },
        {
          label: phase('start'),
          text: t('automations.assistant.summary.tariff.start', { name: battery.deviceName, power: num(answers.charge.power) })
            + (block ? t('automations.assistant.summary.tariff.block') : ''),
          step: 'charge',
        },
        {
          label: phase('run'),
          text: t('automations.assistant.summary.tariff.run', { target: num(answers.charge.target) }),
          step: 'charge',
        },
        {
          label: phase('end'),
          text: t('automations.assistant.summary.tariff.end') + (block ? t('automations.assistant.summary.tariff.end_block') : ''),
          step: 'charge',
        },
      ];
    },
  };

  // ---------------------------------------------------------------- Netzbezug deckeln

  // Abstand zur Grenze, bei dem die Wallbox den Bezug regelt.
  const GRID_CAP_MARGIN = 500;
  // Abstand zur Grenze, unter dem das Szenario nach zehn Minuten endet.
  const GRID_CAP_END_MARGIN = 1500;
  const gridCapChange = (answers) => (answers.speed === 'fast' ? 3000 : 1000);

  const gridCap = {
    id: 'grid_cap',
    category: 'grid',
    icon: 'ico-grid',
    requires: { devices: 'wallbox' },
    steps: [
      {
        id: 'limit', type: 'presets', step: 100, default: 11000,
        options: [{ id: 11000 }, { id: 16000 }, { id: 25000 }],
      },
      { id: 'device', type: 'device', filter: 'wallbox' },
      {
        id: 'speed', type: 'tiles', default: 'calm',
        options: [{ id: 'calm' }, { id: 'fast' }],
      },
      {
        id: 'extras', type: 'multi',
        options: [{ id: 'notify', default: false }, { id: 'history', default: true }],
      },
    ],
    build(answers, ctx) {
      const entry = requireEntry(ctx.wallboxes, answers.device);
      const entity = requireEntity(entry);
      const threshold = answers.limit - GRID_CAP_MARGIN;

      const regulator = {
        output: outputOf(entity),
        direction: 'consumer',
        target_w: threshold,
        deadband_w: 200,
        max_change_w: gridCapChange(answers),
        base: entry.measured ? entityBase(entry.measured) : { source: 'last' },
        fallback: { payload: String(entity.min_value) },
      };

      const onStart = [];
      if (answers.extras.notify) {
        onStart.push({
          type: 'notification', severity: 'info',
          title: t('automations.templates.grid_cap.notice.title'),
          message: t('automations.templates.grid_cap.notice.message'),
        });
      }

      // Ende erst, wenn der Bezug zehn Minuten deutlich unter der Grenze liegt.
      const scenario = {
        id: uniqueId('grid_cap', ctx.usedIds),
        name: t('automations.templates.grid_cap.title'),
        enabled: true,
        template: 'grid_cap',
        history_enabled: !!answers.extras.history,
        when: { conditions: [B('grid_import', 'above', threshold, 10)] },
        until: { conditions: [B('grid_import', 'below', answers.limit - GRID_CAP_END_MARGIN, 600)] },
        on_start: { actions: onStart },
        on_end: { actions: [setpoint(entity, String(entity.max_value))] },
        while: { mode: 'regulate', regulator },
      };
      return { kind: 'scenario', entry: scenario };
    },
    summary(answers, ctx) {
      const entry = requireEntry(ctx.wallboxes, answers.device);
      const entity = requireEntity(entry);
      const threshold = answers.limit - GRID_CAP_MARGIN;
      const rows = [
        {
          label: phase('when'),
          text: t('automations.assistant.summary.grid_cap.when', { seconds: 10, value: num(threshold) }),
          step: 'limit',
        },
        {
          label: phase('run'),
          text: t('automations.assistant.summary.grid_cap.run', {
            value: num(threshold), change: num(gridCapChange(answers)),
          }),
          step: 'speed',
        },
        {
          label: phase('end'),
          text: t('automations.assistant.summary.grid_cap.end', { max: num(entity.max_value) }),
          step: 'device',
        },
      ];
      rows.push({
        label: phase('until'),
        text: t('automations.assistant.summary.grid_cap.until', { value: num(answers.limit - GRID_CAP_END_MARGIN) }),
        step: 'limit',
      });
      if (answers.extras.notify) {
        rows.push({ label: phase('start'), text: t('automations.assistant.summary.grid_cap.notify'), step: 'extras' });
      }
      return rows;
    },
  };

  // ---------------------------------------------------------------- Steuersignal nach §14a

  // Sollwert eines Ziels aus der Leistung in kW: W direkt, A dreiphasig bei 230 V.
  const signalValue = (kw, entity) => (entity.unit_of_measurement === 'A'
    ? Math.round((kw * 1000) / 690)
    : Math.round(kw * 1000));

  const gridSignal = {
    id: 'grid_signal',
    category: 'grid',
    icon: 'ico-gate',
    requires: { devices: 'grid_target' },
    steps: [
      { id: 'input', type: 'device', filter: 'input' },
      { id: 'targets', type: 'devices', filter: 'grid_target', default: [] },
      { id: 'power', type: 'number', min: 1, max: 11, step: 0.1, default: 4.2 },
      { id: 'extras', type: 'multi', options: [{ id: 'notify', default: true }] },
    ],
    build(answers, ctx) {
      const input = requireEntry(ctx.inputs, answers.input).entity;
      if (!answers.targets.length) throw new Error('automation template: no target chosen');
      const targets = answers.targets.map((id) => {
        const entry = requireEntry(ctx.gridTargets, id);
        if (entry.disabled) throw new Error(`automation template: target not usable: ${id}`);
        return requireEntity(entry);
      });

      const onStart = targets.map((entity) => setpoint(entity, String(signalValue(answers.power, entity))));
      const onEnd = targets.map((entity) => setpoint(entity, String(entity.max_value)));
      if (answers.extras.notify) {
        onStart.push({
          type: 'notification', severity: 'info',
          title: t('automations.templates.grid_signal.notice.title'),
          message: t('automations.templates.grid_signal.notice.message'),
        });
      }

      const scenario = {
        id: uniqueId('grid_signal', ctx.usedIds),
        name: t('automations.templates.grid_signal.title'),
        enabled: true,
        template: 'grid_signal',
        when: {
          conditions: [{
            type: 'entity_value', topic: input.state_topic, entity_id: input.unique_id,
            value_template: input.value_template || '', comparison: 'equals', text: input.payload_on || 'on',
          }],
        },
        on_start: { actions: onStart },
        on_end: { actions: onEnd },
        while: { mode: 'none' },
      };
      return { kind: 'scenario', entry: scenario };
    },
    summary(answers, ctx) {
      const input = (ctx.inputs || []).find((item) => item.id === answers.input) || {};
      const rows = [{
        label: phase('when'),
        text: t('automations.assistant.summary.grid_signal.when', { name: input.deviceName || '' }),
        step: 'input',
      }];
      for (const id of answers.targets) {
        const entry = requireEntry(ctx.gridTargets, id);
        const entity = requireEntity(entry);
        rows.push({
          label: phase('start'),
          text: t('automations.assistant.summary.grid_signal.start', {
            name: entry.deviceName, value: num(signalValue(answers.power, entity)), unit: entity.unit_of_measurement,
          }),
          step: 'targets',
        });
      }
      rows.push({ label: phase('end'), text: t('automations.assistant.summary.grid_signal.end'), step: 'targets' });
      if (answers.extras.notify) {
        rows.push({ label: phase('start'), text: t('automations.assistant.summary.grid_signal.notify'), step: 'extras' });
      }
      return rows;
    },
  };

  // ---------------------------------------------------------------- PV-Ertrag fehlt

  const pvMissing = {
    id: 'pv_missing',
    category: 'warning',
    icon: 'ico-sun',
    requires: { devices: 'inverter_measured' },
    steps: [
      { id: 'device', type: 'device', filter: 'inverter_measured' },
      { id: 'limit', type: 'number', min: 5, max: 200, step: 5, default: 20 },
      { id: 'minutes', type: 'number', min: 10, max: 120, step: 5, default: 30 },
      {
        id: 'severity', type: 'tiles', default: 'warning',
        options: [{ id: 'info' }, { id: 'warning' }, { id: 'critical' }],
      },
    ],
    build(answers, ctx) {
      const entry = requireEntry(ctx.inverters, answers.device);
      const measured = entry.measured;
      if (!measured) throw new Error(`automation template: no own reading for ${entry.id}`);

      const rule = {
        id: uniqueId('pv_missing', ctx.usedIds),
        name: t('automations.templates.pv_missing.title'),
        enabled: true,
        cooldown_seconds: 21600,
        conditions: [
          { type: 'sun_window', from: 'sunrise', to: 'sunset', from_offset_min: 60, to_offset_min: -60 },
          {
            type: 'entity_value', topic: measured.state_topic, entity_id: measured.unique_id,
            value_template: measured.value_template || '', comparison: 'below', value: answers.limit,
            hold_seconds: answers.minutes * 60,
          },
        ],
        actions: [{
          type: 'notification',
          severity: answers.severity,
          title: t('automations.templates.pv_missing.notice.title'),
          message: t('automations.templates.pv_missing.notice.message', {
            minutes: num(answers.minutes), value: num(answers.limit),
          }),
        }],
      };
      return { kind: 'rule', entry: rule };
    },
    summary(answers, ctx) {
      requireEntry(ctx.inverters, answers.device);
      return [
        { label: phase('when'), text: t('automations.assistant.summary.pv_missing.window'), step: 'device' },
        {
          label: phase('when'),
          text: t('automations.assistant.summary.pv_missing.under', {
            minutes: num(answers.minutes), value: num(answers.limit),
          }),
          step: 'minutes',
        },
        {
          label: phase('then'),
          text: t('automations.assistant.summary.battery.then', {
            severity: t(`automations.templates.pv_missing.steps.severity.options.${answers.severity}.title`),
          }),
          step: 'severity',
        },
        { label: phase('end'), text: t('automations.assistant.summary.pv_missing.cooldown'), step: 'severity' },
      ];
    },
  };

  window.__automationTemplates = {
    TEMPLATES: [zeroFeed, surplusWallbox, surplusSwitch, batteryWarn, timeSwitch, heatPump, tariff, gridCap, gridSignal, pvMissing],
    chargeFit,
  };
})();

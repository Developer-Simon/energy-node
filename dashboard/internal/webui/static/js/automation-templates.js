(() => {
  // Vorlagen des Automations-Assistenten. Jede Vorlage beschreibt ihre Schritte
  // (Antworten), baut daraus einen Eintrag im Format der Dienstvalidierung und
  // fasst die Antworten als Saetze fuer die Zusammenfassung zusammen. Alle Texte
  // kommen aus dem Katalog: automations.templates.<id>.* und automations.assistant.*.
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const num = (value) => (window.I18n ? window.I18n.formatNumber(value) : String(value));
  const { uniqueId } = window.__automationWizard;

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
      { id: 'soc', type: 'number', min: 10, max: 90, default: 40, when: (answers) => answers.mode === 'battery' },
      {
        id: 'start', type: 'presets', default: 1400, effect: 'starts',
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
      { id: 'target', type: 'presets', default: 30, effect: 'band', options: [{ id: 60 }, { id: 30 }, { id: 10 }] },
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
            deadband_w: 20,
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

  window.__automationTemplates = { TEMPLATES: [zeroFeed, surplusWallbox] };
})();

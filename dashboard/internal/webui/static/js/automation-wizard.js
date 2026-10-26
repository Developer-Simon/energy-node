(() => {
  // Geraetekontext fuer den Automations-Assistenten: welche Geraete Wechselrichter,
  // Wallboxen oder Schalter sind, welches Stellglied und welcher Messwert dazu
  // gehoeren, und ob sie schon von einem Regler-Szenario belegt sind. Keine
  // DOM-Zugriffe. Eingaben sind die Geraete aus /api/v1/devices, der Snapshot aus
  // /api/v1/energy und das aktuelle Regeldokument.
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const formatNumber = (value, decimals) => (window.I18n
    ? window.I18n.formatNumber(value, decimals) : String(value));

  const hasOwn = (object, key) => Object.prototype.hasOwnProperty.call(object || {}, key);
  const isNumber = (value) => typeof value === 'number' && Number.isFinite(value);
  const hasText = (value) => value !== undefined && value !== null && value !== '';
  const hasValue = (entity) => !!entity && entity.has_value !== false && hasText(entity.value);

  // Stellglied: schreibbar, mit Grenzen und ohne Ein-/Aus-Payload.
  const isActuator = (entity) => hasText(entity.command_topic)
    && isNumber(entity.min_value) && isNumber(entity.max_value) && !hasText(entity.payload_on);
  // Schalter: schreibbar mit Ein- und Aus-Payload.
  const isSwitch = (entity) => hasText(entity.command_topic)
    && hasText(entity.payload_on) && hasText(entity.payload_off);
  // Lesbarer Messwert: Zustandsthema mit Template-Unterstuetzung.
  const isReadable = (entity) => hasText(entity.state_topic) && !!entity.template_supported;

  // Rollen aus dem Snapshot: je Entitaet und je Geraet.
  function roleIndex(snapshot) {
    const entityRoles = new Map();
    const deviceRoles = new Map();
    for (const item of (snapshot && snapshot.entities) || []) {
      const role = item.role && item.role.role;
      if (!role) continue;
      entityRoles.set(item.entity_id, role);
      if (!deviceRoles.has(item.device_id)) deviceRoles.set(item.device_id, new Set());
      deviceRoles.get(item.device_id).add(role);
    }
    return { entityRoles, deviceRoles };
  }

  // Ausgaben vorhandener Regler-Szenarien: Topic -> Szenarioname.
  function regulatedTopicsOf(document) {
    const topics = {};
    for (const scenario of (document && document.scenarios) || []) {
      const regulator = scenario.while && scenario.while.regulator;
      const topic = regulator && regulator.output && regulator.output.topic;
      if (hasText(topic) && !hasOwn(topics, topic)) topics[topic] = scenario.name || '';
    }
    return topics;
  }

  // Preis mit fester Nachkommazahl: Euro mit drei, Cent mit zwei Stellen.
  function formatPrice(value, unit) {
    const number = typeof value === 'number' ? value : Number(value);
    if (!Number.isFinite(number)) return `${value} ${unit || ''}`.trim();
    const decimals = /€|EUR/i.test(String(unit || '')) ? 3 : 2;
    const text = formatNumber(number, decimals);
    return unit ? `${text} ${unit}` : text;
  }

  function priceText(entity) {
    if (!hasValue(entity)) return '—';
    return formatPrice(entity.value, entity.unit_of_measurement);
  }

  function liveText(entity) {
    if (!hasValue(entity)) return '—';
    const number = typeof entity.value === 'number' ? entity.value : Number(entity.value);
    const text = Number.isFinite(number) && String(entity.value).trim() !== ''
      ? formatNumber(number)
      : String(entity.value);
    return entity.unit_of_measurement ? `${text} ${entity.unit_of_measurement}` : text;
  }

  function disabledText(entity, regulatedTopics) {
    if (entity && hasText(entity.command_topic) && hasOwn(regulatedTopics, entity.command_topic)) {
      return t('automations.assistant.device.already_regulated', { name: regulatedTopics[entity.command_topic] });
    }
    return '';
  }

  // Ein Eintrag je Geraet mit Rolle. Ohne Stellglied bleibt entity leer und
  // der Eintrag ist abgeblendet. Die ID faellt dann auf den Messwert oder die
  // erste Entitaet zurueck, damit sie eindeutig bleibt.
  function hardwareEntry(device, role, index, regulatedTopics, extra = {}) {
    const entities = device.entities || [];
    const readable = entities.filter((entity) => isReadable(entity)
      && index.entityRoles.get(entity.unique_id) === role);
    const measured = readable[0];
    const actuator = entities.find((entity) => isActuator(entity));
    const fallback = actuator || measured || entities[0] || {};
    const entry = {
      id: fallback.unique_id || device.id,
      deviceName: device.name,
      entity: actuator || null,
      live: liveText(measured || actuator),
      disabled: disabledText(actuator, regulatedTopics) || (actuator ? '' : t('automations.assistant.device.no_limit')),
      ...extra,
    };
    if (measured) entry.measured = measured;
    return entry;
  }

  function inverterEntries(devices, index, regulatedTopics) {
    return devices
      .filter((device) => (index.deviceRoles.get(device.id) || new Set()).has('pv'))
      .map((device) => hardwareEntry(device, 'pv', index, regulatedTopics));
  }

  function wallboxEntries(devices, index, regulatedTopics) {
    return devices
      .filter((device) => (index.deviceRoles.get(device.id) || new Set()).has('wallbox'))
      .map((device) => {
        const entities = device.entities || [];
        const switchEntity = entities.find((entity) => isSwitch(entity)) || null;
        const plugEntity = entities.find((entity) => isReadable(entity)
          && ['plug', 'connectivity'].includes(entity.device_class)) || null;
        return hardwareEntry(device, 'wallbox', index, regulatedTopics, { switchEntity, plugEntity });
      });
  }

  function switchEntries(devices, regulatedTopics) {
    const result = [];
    for (const device of devices) {
      for (const entity of device.entities || []) {
        if (!isSwitch(entity)) continue;
        result.push({
          id: entity.unique_id,
          deviceName: device.name,
          entity,
          live: liveText(entity),
          disabled: disabledText(entity, regulatedTopics),
        });
      }
    }
    return result;
  }

  const deviceHasRole = (index, deviceId, role) => (index.deviceRoles.get(deviceId) || new Set()).has(role);
  const BATTERY_ROLES = ['battery', 'battery_soc'];
  const RESERVED_ROD_ROLES = ['pv', 'wallbox', 'heat_pump', ...BATTERY_ROLES];
  const POWER_OR_PERCENT = /^(W|%)$/;
  const PRICE_UNIT = /^(ct|€|EUR)\s*\/\s*kWh$/i;
  const INPUT_CLASSES = ['power', 'running', 'problem', 'plug'];

  // Waermepumpe: Sollwert und eigener Schalter am selben Geraet. Gesperrt nur,
  // wenn beides fehlt und im System kein Schalter existiert.
  function heatPumpEntries(devices, index, switches) {
    return devices
      .filter((device) => deviceHasRole(index, device.id, 'heat_pump'))
      .map((device) => {
        const entities = device.entities || [];
        const setpoint = entities.find((entity) => isActuator(entity)) || null;
        const ownSwitch = entities.find((entity) => isSwitch(entity)) || null;
        const measured = entities.find((entity) => isReadable(entity)
          && index.entityRoles.get(entity.unique_id) === 'heat_pump') || null;
        const blocked = !setpoint && !ownSwitch && switches.length === 0;
        const fallback = setpoint || ownSwitch || measured || entities[0] || {};
        return {
          id: fallback.unique_id || device.id,
          deviceName: device.name,
          live: liveText(measured),
          setpoint,
          ownSwitch,
          disabled: blocked ? t('automations.assistant.unavailable.heat_pump_control') : '',
        };
      });
  }

  // Akku: Geraete mit Rolle battery oder battery_soc. Ohne Stellglied in W oder %
  // ist der Eintrag gesperrt.
  function batteryEntries(devices, index) {
    return devices
      .filter((device) => BATTERY_ROLES.some((role) => deviceHasRole(index, device.id, role)))
      .map((device) => {
        const entities = device.entities || [];
        const setpoint = entities.find((entity) => isActuator(entity)
          && POWER_OR_PERCENT.test(String(entity.unit_of_measurement || ''))) || null;
        const measured = entities.find((entity) => isReadable(entity)
          && BATTERY_ROLES.includes(index.entityRoles.get(entity.unique_id))) || null;
        const fallback = setpoint || measured || entities[0] || {};
        return {
          id: fallback.unique_id || device.id,
          deviceName: device.name,
          live: liveText(measured),
          setpoint,
          disabled: setpoint ? '' : t('automations.assistant.unavailable.battery_setpoint'),
        };
      });
  }

  // Heizstab: ohne die Rollen der Erzeuger und Speicher, mit Stellglied in W oder %
  // und einem Temperaturmesswert.
  function rodEntries(devices, index, regulatedTopics) {
    const result = [];
    for (const device of devices) {
      if (RESERVED_ROD_ROLES.some((role) => deviceHasRole(index, device.id, role))) continue;
      const entities = device.entities || [];
      const entity = entities.find((item) => isActuator(item)
        && POWER_OR_PERCENT.test(String(item.unit_of_measurement || ''))) || null;
      const thermometer = entities.find((item) => isReadable(item)
        && item.device_class === 'temperature') || null;
      if (!entity || !thermometer) continue;
      result.push({
        id: entity.unique_id,
        deviceName: device.name,
        live: liveText(thermometer),
        entity,
        thermometer,
        disabled: disabledText(entity, regulatedTopics),
      });
    }
    return result;
  }

  // Lesbare Entitaeten je Geraet, die das Praedikat erfuellen.
  function readableEntries(devices, predicate) {
    const result = [];
    for (const device of devices) {
      for (const entity of device.entities || []) {
        if (isReadable(entity) && predicate(entity)) {
          result.push({ id: entity.unique_id, deviceName: device.name, entity, live: liveText(entity) });
        }
      }
    }
    return result;
  }

  function buildContext({ devices = [], snapshot, document } = {}) {
    const index = roleIndex(snapshot);
    const regulatedTopics = regulatedTopicsOf(document);
    const readable = [];
    for (const device of devices) {
      for (const entity of device.entities || []) {
        if (isReadable(entity)) readable.push(entity);
      }
    }
    const settings = (document && document.settings) || {};
    const values = (snapshot && snapshot.values) || {};
    const switches = switchEntries(devices, regulatedTopics);
    return {
      inverters: inverterEntries(devices, index, regulatedTopics),
      wallboxes: wallboxEntries(devices, index, regulatedTopics),
      switches,
      heatPumps: heatPumpEntries(devices, index, switches),
      batteries: batteryEntries(devices, index),
      rods: rodEntries(devices, index, regulatedTopics),
      prices: readableEntries(devices, (entity) => PRICE_UNIT.test(String(entity.unit_of_measurement || '')))
        .map((item) => ({ ...item, live: priceText(item.entity) })),
      inputs: readableEntries(devices, (entity) => hasText(entity.payload_on)
        || INPUT_CLASSES.includes(entity.device_class)),
      socAvailable: hasOwn(values, 'battery_soc'),
      hasLocation: isNumber(settings.latitude) && isNumber(settings.longitude),
      regulatedTopics,
      usedIds: [
        ...((document && document.rules) || []).map((rule) => rule.id),
        ...((document && document.scenarios) || []).map((scenario) => scenario.id),
      ],
      snapshot,
      readable,
    };
  }

  function devicesFor(filter, ctx) {
    if (filter === 'inverter') return ctx.inverters;
    if (filter === 'wallbox') return ctx.wallboxes;
    if (filter === 'switch') return ctx.switches;
    if (filter === 'heat_pump') return ctx.heatPumps;
    if (filter === 'battery') return ctx.batteries;
    if (filter === 'rod') return ctx.rods;
    if (filter === 'price') return ctx.prices;
    if (filter === 'input') return ctx.inputs;
    return [];
  }

  // Spiegel von gridImportPower - gridExportPower in energy-model.js: die
  // getrennten Rollen grid_import/grid_export haben Vorrang, sonst das Vorzeichen
  // der Rolle grid. null, wenn keine Netzrolle im Snapshot steht.
  function gridNet(snapshot) {
    const values = (snapshot && snapshot.values) || {};
    if (!hasOwn(values, 'grid_import') && !hasOwn(values, 'grid_export') && !hasOwn(values, 'grid')) return null;
    const grid = values.grid || 0;
    const importPower = hasOwn(values, 'grid_import')
      ? Math.abs(values.grid_import)
      : (hasOwn(values, 'grid') && grid > 0 ? grid : 0);
    const exportPower = hasOwn(values, 'grid_export')
      ? Math.abs(values.grid_export)
      : (hasOwn(values, 'grid') && grid < 0 ? Math.abs(grid) : 0);
    return importPower - exportPower;
  }

  // Vorlagenkategorien der Suche. Die Vorlagen selbst liegen in Task 4.
  const CATEGORIES = ['feed_in', 'charging', 'switching', 'heating', 'tariff', 'grid', 'warning', 'time'];
  const TIME_PATTERN = /^([01]\d|2[0-3]):[0-5]\d$/;
  const WEEKDAYS = [0, 1, 2, 3, 4, 5, 6];

  // Grund, warum eine Option gerade nicht wählbar ist, oder ''.
  function optionUnavailable(option, ctx, answers) {
    if (!option || typeof option.unavailable !== 'function') return '';
    return option.unavailable(ctx, answers) || '';
  }

  // Texte einer Vorlage: fertige Strings am Objekt haben Vorrang, sonst der
  // Katalogschluessel automations.templates.<id>.<feld>.
  function templateText(template, field) {
    if (typeof template[field] === 'string') return template[field];
    return t(`automations.templates.${template.id}.${field}`);
  }

  function templateKeywords(template) {
    if (Array.isArray(template.keywords)) return template.keywords.join(' ');
    return templateText(template, 'keywords');
  }

  // Verfuegbarkeit einer Vorlage aus den Geraetelisten und dem Akkusignal.
  function templateAvailability(template, ctx) {
    const requires = template.requires || {};
    let reason = '';
    let hint = '';
    if (requires.devices) {
      const usable = devicesFor(requires.devices, ctx).filter((item) => !item.disabled);
      if (!usable.length) {
        reason = t(`automations.assistant.unavailable.${requires.devices}`);
      } else {
        hint = usable.length === 1
          ? t('automations.assistant.hint.device_one', { name: usable[0].deviceName })
          : t('automations.assistant.hint.device_many', { n: usable.length });
      }
    }
    if (!reason && requires.soc) {
      if (!ctx.socAvailable) reason = t('automations.assistant.unavailable.soc');
      else if (!hint) hint = t('automations.assistant.hint.soc');
    }
    return { available: !reason, reason, hint: reason ? '' : hint };
  }

  // Suche nach Wortern in Titel, Kurztext und Schluesselwoertern (Kleinschreibung,
  // Umlaute bleiben). Jedes Wort muss treffen. Ohne Suche gelten alle Vorlagen.
  function searchTemplates(templates, { query = '', category = 'all' } = {}, ctx) {
    const words = String(query || '').trim().toLowerCase().split(/\s+/).filter(Boolean);
    const fitting = [];
    const others = [];
    for (const template of templates || []) {
      if (category && category !== 'all' && template.category !== category) continue;
      if (words.length) {
        const haystack = [templateText(template, 'title'), templateText(template, 'blurb'), templateKeywords(template)]
          .join(' ').toLowerCase();
        if (!words.every((word) => haystack.includes(word))) continue;
      }
      (templateAvailability(template, ctx).available ? fitting : others).push(template);
    }
    return { fitting, others };
  }

  // Standardwert je Schritt. Ist der Standard nicht verfügbar, gilt die erste
  // verfügbare Option bzw. das erste verfügbare Gerät.
  function defaultAnswer(step, ctx, answers) {
    const options = step.options || [];
    switch (step.type) {
      case 'device': {
        const kind = typeof step.filter === 'string' ? step.filter : (step.filter && step.filter.role);
        const list = devicesFor(kind, ctx);
        const wanted = typeof step.default === 'function' ? step.default(ctx, answers) : step.default;
        const preferred = list.find((item) => item.id === wanted && !item.disabled);
        const first = list.find((item) => !item.disabled);
        return (preferred || first || {}).id || '';
      }
      case 'choice':
      case 'tiles': {
        const available = (option) => !optionUnavailable(option, ctx, answers);
        const preferred = options.find((option) => option.id === step.default && available(option));
        const first = options.find((option) => available(option));
        if (preferred) return preferred.id;
        if (first) return first.id;
        return step.default === undefined ? '' : step.default;
      }
      case 'numbers': {
        const result = {};
        for (const field of step.fields || []) {
          result[field.id] = field.default !== undefined ? field.default : field.min;
        }
        return result;
      }
      case 'devices': {
        const list = devicesFor(typeof step.filter === 'string' ? step.filter : (step.filter && step.filter.role), ctx);
        const usable = new Set(list.filter((item) => !item.disabled).map((item) => item.id));
        return (Array.isArray(step.default) ? step.default : []).filter((id) => usable.has(id));
      }
      case 'presets':
        return step.default !== undefined ? step.default : (options[0] || {}).id;
      case 'number':
        return step.default !== undefined ? step.default : step.min;
      case 'time': {
        const source = step.default || { from: '', to: '', days: WEEKDAYS };
        return { from: source.from, to: source.to, days: [...(source.days || [])] };
      }
      case 'multi': {
        const result = {};
        for (const option of options) {
          result[option.id] = !!option.default && !optionUnavailable(option, ctx, answers);
          if (option.reveal) {
            const value = option.reveal.default;
            result[`${option.id}_value`] = Array.isArray(value) ? [...value] : value;
          }
        }
        return result;
      }
      default:
        return step.default;
    }
  }

  function defaultAnswers(template, ctx) {
    const answers = {};
    for (const step of template.steps || []) {
      answers[step.id] = defaultAnswer(step, ctx, answers);
      const chosen = (step.options || []).find((option) => option.id === answers[step.id]);
      if (chosen && chosen.reveal && answers[chosen.reveal.id] === undefined) {
        answers[chosen.reveal.id] = chosen.reveal.default;
      }
      if (step.tolerance) answers.tolerance = step.tolerance.default;
    }
    return answers;
  }

  // Sichtbare Schritte: ohne when oder mit when(answers) === true, danach die
  // Zusammenfassung. Die Liste ist eine Funktion der Antworten und wird bei jeder
  // Änderung neu berechnet.
  function visibleSteps(template, answers, ctx) {
    const steps = (template.steps || []).filter((step) => !step.when || step.when(answers, ctx) === true);
    return [...steps, { id: 'summary', type: 'summary' }];
  }

  function stepComplete(step, answer) {
    switch (step.type) {
      case 'device':
        return typeof answer === 'string' && answer !== '';
      case 'number':
        return isNumber(answer)
          && (step.min === undefined || answer >= step.min)
          && (step.max === undefined || answer <= step.max);
      case 'presets':
        return isNumber(answer) && answer > 0;
      case 'numbers':
        return !!answer && (step.fields || []).every((field) => isNumber(answer[field.id])
          && (field.min === undefined || answer[field.id] >= field.min)
          && (field.max === undefined || answer[field.id] <= field.max));
      case 'devices':
        return Array.isArray(answer) && answer.length > 0;
      case 'time':
        return !!answer && TIME_PATTERN.test(answer.from) && TIME_PATTERN.test(answer.to)
          && answer.from !== answer.to;
      default:
        return true;
    }
  }

  // Mehrfachauswahl: ID hinzufuegen oder entfernen, neue Liste zurueck.
  function toggleDevice(current, id) {
    const list = Array.isArray(current) ? current : [];
    return list.includes(id) ? list.filter((item) => item !== id) : [...list, id];
  }

  // Waehlt eine Option. Hat sie ein reveal, wird dessen Standardwert gesetzt, falls
  // noch kein Wert da ist. Ein frueher gesetzter Wert bleibt beim erneuten Waehlen.
  function chooseOption(step, answers, optionId) {
    answers[step.id] = optionId;
    const option = (step.options || []).find((item) => item.id === optionId);
    if (option && option.reveal && answers[option.reveal.id] === undefined) {
      answers[option.reveal.id] = option.reveal.default;
    }
    return answers;
  }

  // Nachkommastellen eines Schritts, z. B. 0.5 -> 1.
  function decimalsOf(step) {
    const text = String(step);
    return text.includes('.') ? text.split('.')[1].length : 0;
  }

  // Rundet auf ein Vielfaches von step und entfernt Fliesskommafehler.
  function roundToStep(value, step) {
    if (!isNumber(value) || !isNumber(step) || step <= 0) return value;
    return Number((Math.round(value / step) * step).toFixed(decimalsOf(step)));
  }

  // Kleinste freie ID: base, sonst base_2, base_3 ...
  function uniqueId(base, usedIds) {
    const used = new Set(usedIds || []);
    if (!used.has(base)) return base;
    let counter = 2;
    while (used.has(`${base}_${counter}`)) counter += 1;
    return `${base}_${counter}`;
  }

  // Zeitstempel der frühesten Minute mit mindestens thresholdW Einspeisung.
  // Einspeisung ist negativ in role:grid.
  function firstReached(rows, thresholdW) {
    let found = null;
    for (const row of rows || []) {
      if (!isNumber(row.avg) || row.avg > -thresholdW) continue;
      if (found === null || row.ts < found) found = row.ts;
    }
    return found;
  }

  // Kleinster Minimalwert in den Zeilen, null ohne Zeilen.
  function lowest(rows) {
    const values = (rows || []).map((row) => row.min).filter(isNumber);
    return values.length ? Math.min(...values) : null;
  }

  // Vortag als Zeitraum in Millisekunden, beide Enden eingeschlossen (wie readRange).
  function yesterdayRange(now = new Date()) {
    const year = now.getFullYear();
    const month = now.getMonth();
    const day = now.getDate();
    const todayStart = new Date(year, month, day).getTime();
    const yesterdayStart = new Date(year, month, day - 1).getTime();
    return [yesterdayStart, todayStart - 1];
  }

  function clockText(ts) {
    const date = new Date(ts);
    const pad = (value) => String(value).padStart(2, '0');
    return `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  }

  // Überschuss je Viertelstunde des Vortags: Mittel des Exports (-role:grid), ohne
  // Import. Viertelstunden ohne Messwerte bleiben null. Index 0 ist 00:00.
  const QUARTERS = 96;
  function quarterSurplus(rows) {
    const sums = new Array(QUARTERS).fill(0);
    const counts = new Array(QUARTERS).fill(0);
    for (const row of rows || []) {
      if (!isNumber(row.avg) || !isNumber(row.ts)) continue;
      const date = new Date(row.ts);
      const slot = date.getHours() * 4 + Math.floor(date.getMinutes() / 15);
      sums[slot] += Math.max(0, -row.avg);
      counts[slot] += 1;
    }
    return sums.map((sum, slot) => (counts[slot] ? sum / counts[slot] : null));
  }

  // Erste Viertelstunde mit mindestens threshold, Anzahl solcher Viertelstunden und
  // deren Summe als Stunden und Minuten. first ist -1, wenn keine erreicht wird.
  function surplusStats(series, threshold) {
    let first = -1;
    let count = 0;
    (series || []).forEach((value, slot) => {
      if (!isNumber(value) || value < threshold) return;
      count += 1;
      if (first < 0) first = slot;
    });
    return { first, count, hours: Math.floor(count / 4), minutes: (count % 4) * 15 };
  }

  function quarterClock(slot) {
    const pad = (value) => String(value).padStart(2, '0');
    return `${pad(Math.floor(slot / 4))}:${pad((slot % 4) * 15)}`;
  }

  // Wirkung eines Voreinstellungswerts aus den Vortagsdaten. Ohne Zeilen oder bei
  // einem Fehler des Verlaufs bleibt der Text leer.
  async function presetEffect(step, value, ctx, store) {
    if (step.effect === 'band') return '';
    if (step.effect !== 'starts' && step.effect !== 'soc' && step.effect !== 'surplus') return '';
    if (!store || typeof store.readRange !== 'function') return '';
    const [from, to] = yesterdayRange();
    try {
      if (step.effect === 'starts') {
        const rows = await store.readRange('1m', 'role:grid', from, to);
        if (!rows || !rows.length) return '';
        const ts = firstReached(rows, value);
        return ts === null
          ? t('automations.assistant.effect.not_reached')
          : t('automations.assistant.effect.reached_at', { time: clockText(ts) });
      }
      if (step.effect === 'surplus') {
        const rows = await store.readRange('1m', 'role:grid', from, to);
        if (!rows || !rows.length) return '';
        const stats = surplusStats(quarterSurplus(rows), value);
        if (stats.first < 0) return t('automations.assistant.effect.surplus_missed');
        return t('automations.assistant.effect.surplus_reached', {
          time: quarterClock(stats.first), hours: stats.hours, minutes: stats.minutes,
        });
      }
      const rows = await store.readRange('1m', 'role:battery_soc', from, to);
      if (!rows || !rows.length) return '';
      const low = lowest(rows);
      return low === null ? '' : t('automations.assistant.effect.lowest_soc', { value: formatNumber(low) });
    } catch (error) {
      return '';
    }
  }

  // Spiegel von compute_step() in services/automation/regulator.py, gleiche
  // Reihenfolge: Fehler, Totband, Vorzeichen, max_change_w, Abrunden auf step,
  // Grenzen, unveraendert. Fehlende gain und deadband_w fallen auf 0.8 und 20.
  function regulatorStep(regulator, { net, base, lastValue }) {
    const hasLast = isNumber(lastValue);
    const target = isNumber(regulator.target_w) ? regulator.target_w : 0;
    const deadband = isNumber(regulator.deadband_w) ? regulator.deadband_w : 20;
    const gain = isNumber(regulator.gain) ? regulator.gain : 0.8;
    const error = net - target;
    if (Math.abs(error) <= deadband) return { value: null, payload: null, reason: 'deadband', error };
    const sign = regulator.direction === 'producer' ? 1 : -1;
    let value = base + sign * gain * error;
    if (isNumber(regulator.max_change_w) && hasLast) {
      value = Math.min(Math.max(value, lastValue - regulator.max_change_w), lastValue + regulator.max_change_w);
    }
    const output = regulator.output || {};
    if (isNumber(output.step) && output.step > 0) value = Math.floor(value / output.step) * output.step;
    if (isNumber(output.min)) value = Math.max(value, output.min);
    if (isNumber(output.max)) value = Math.min(value, output.max);
    const payload = isNumber(output.decimals) ? value.toFixed(output.decimals) : String(value);
    if (hasLast && value === lastValue) return { value: null, payload: null, reason: 'unchanged', error };
    return { value, payload, reason: null, error };
  }

  // Messwert der Basis: Geraetemesswert, Bilanzfeld oder der letzte Sollwert.
  function regulatorBase(base, output, ctx) {
    const source = base && base.source;
    if (source === 'last') return isNumber(output.max) ? output.max : null;
    if (source === 'balance') {
      const value = ((ctx.snapshot && ctx.snapshot.values) || {})[base.field];
      return isNumber(value) ? value : null;
    }
    if (source === 'entity') {
      const entity = (ctx.readable || []).find((item) => item.state_topic === base.topic);
      if (!entity || !hasValue(entity)) return null;
      const number = Number(entity.value);
      return Number.isFinite(number) ? number : null;
    }
    return null;
  }

  // Vorschau des naechsten Grenzwerts eines Regler-Szenarios: "sinkt" oder "steigt"
  // von der Basis auf den neuen Wert, sonst "unveraendert".
  function regulatorPreview(scenario, ctx) {
    const scenarioWhile = scenario && scenario.while;
    if (!scenarioWhile || scenarioWhile.mode !== 'regulate' || !scenarioWhile.regulator) return '';
    const regulator = scenarioWhile.regulator;
    const output = regulator.output || {};
    const net = gridNet(ctx.snapshot);
    if (net === null) return '';
    const base = regulatorBase(regulator.base, output, ctx);
    if (base === null) return '';
    const lastValue = regulator.direction === 'consumer' ? output.min : output.max;
    const step = regulatorStep(regulator, { net, base, lastValue: isNumber(lastValue) ? lastValue : null });
    if (step.value === null || step.value === base) return t('automations.assistant.preview.hold');
    const key = step.value < base ? 'automations.assistant.preview.lower' : 'automations.assistant.preview.raise';
    return t(key, { from: formatNumber(base), to: formatNumber(step.value) });
  }

  window.__automationWizard = {
    CATEGORIES,
    buildContext,
    defaultAnswers,
    devicesFor,
    chooseOption,
    decimalsOf,
    firstReached,
    formatPrice,
    gridNet,
    lowest,
    optionUnavailable,
    presetEffect,
    regulatorPreview,
    regulatorStep,
    roundToStep,
    searchTemplates,
    surplusStats,
    stepComplete,
    toggleDevice,
    templateAvailability,
    uniqueId,
    visibleSteps,
    yesterdayRange,
  };
})();

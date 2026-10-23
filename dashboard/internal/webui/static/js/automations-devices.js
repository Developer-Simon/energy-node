(() => {
  // Reine Kandidaten-Listen fuer die Automationen-Editoren: Geraete nach
  // Rollen-Gruppen, Stellglieder mit Grenzen, Messwerte, Schalter und die
  // Chip-Auswahl. Keine DOM-Zugriffe, Eingaben sind die Geraete aus
  // /api/v1/devices und die Rollen-Zuordnung aus /api/v1/energy/roles.
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const formatNumber = (value) => (window.I18n ? window.I18n.formatNumber(value) : String(value));
  const compare = (a, b) => (window.I18n ? window.I18n.compare(a, b) : (String(a) < String(b) ? -1 : String(a) > String(b) ? 1 : 0));

  const ROLE_GROUPS = [
    { key: 'inverter', icon: 'ico-inverter', tone: 'pv' },
    { key: 'wallbox', icon: 'ico-wallbox', tone: 'wallbox' },
    { key: 'heat_pump', icon: 'ico-heatpump', tone: 'heatpump' },
    { key: 'grid', icon: 'ico-grid', tone: 'grid' },
    { key: 'other', icon: 'ico-device', tone: '' },
  ];
  const GROUP_BY_KEY = Object.fromEntries(ROLE_GROUPS.map((group) => [group.key, group]));

  // Vorrang bei Geraeten mit mehreren Rollen: pv vor wallbox vor heat_pump vor grid.
  const ROLE_PRIORITY = [
    ['pv', 'inverter'],
    ['wallbox', 'wallbox'],
    ['heat_pump', 'heat_pump'],
    ['grid', 'grid'],
    ['grid_import', 'grid'],
    ['grid_export', 'grid'],
  ];
  const PRODUCER_GROUPS = ['inverter', 'other'];
  const CONSUMER_GROUPS = ['wallbox', 'heat_pump', 'other'];
  // Netz zuerst in der Messwert-Liste, danach die uebrigen Gruppen in ihrer Reihenfolge.
  const METER_ORDER = ['grid', 'inverter', 'wallbox', 'heat_pump', 'other'];

  const isNumber = (value) => typeof value === 'number' && Number.isFinite(value);
  const hasText = (value) => value !== undefined && value !== null && value !== '';
  // Die Registry liefert Messwerte als Text ("800", "-1432"), Grenzen als Zahl.
  const toNumber = (value) => {
    if (isNumber(value)) return value;
    if (typeof value !== 'string' || value.trim() === '') return null;
    const number = Number(value);
    return Number.isFinite(number) ? number : null;
  };

  function deviceRoleGroup(device, assignments) {
    const roles = new Set(
      (device.entities || []).map((entity) => (assignments && assignments[entity.unique_id] || {}).role),
    );
    const match = ROLE_PRIORITY.find(([role]) => roles.has(role));
    return match ? match[1] : 'other';
  }

  // i18n-keys: automations.devices.group.inverter, automations.devices.group.wallbox, automations.devices.group.heat_pump, automations.devices.group.grid, automations.devices.group.misc
  // Das Label von "other" heisst misc: ein Schluessel mit Endung .other gilt im
  // Katalogtest als Plural-Form.
  const LABEL_KEY = {
    inverter: 'automations.devices.group.inverter',
    wallbox: 'automations.devices.group.wallbox',
    heat_pump: 'automations.devices.group.heat_pump',
    grid: 'automations.devices.group.grid',
    other: 'automations.devices.group.misc',
  };

  function groupLabel(key) {
    return t(LABEL_KEY[key] || LABEL_KEY.other);
  }

  // Aktueller Wert als "jetzt ...": Zahlen mit Einheit, Watt mit Vorzeichen,
  // kurze Texte (ON, OFF) unveraendert, ohne Wert leer.
  function valueText(entity) {
    const number = toNumber(entity.value);
    if (number === null) {
      const text = typeof entity.value === 'string' ? entity.value.trim() : '';
      return text && text.length <= 16 ? t('automations.devices.value_now', { value: text }) : '';
    }
    const unit = entity.unit_of_measurement || '';
    if (unit === 'W') {
      const sign = number > 0 ? '+' : '';
      return t('automations.devices.value_now', { value: `${sign}${formatNumber(number)} W` });
    }
    const suffix = unit ? ` ${unit}` : '';
    return t('automations.devices.value_now', { value: `${formatNumber(number)}${suffix}` });
  }

  function candidate(device, entity, assignments, subtitle, text, fits) {
    const group = deviceRoleGroup(device, assignments);
    const style = GROUP_BY_KEY[group];
    return {
      id: entity.unique_id,
      deviceName: device.name,
      entityName: entity.name,
      group,
      icon: style.icon,
      tone: style.tone,
      subtitle,
      valueText: text,
      fits,
      topic: entity.command_topic || entity.state_topic || '',
    };
  }

  function actuatorFits(group, direction) {
    if (direction === 'producer') return PRODUCER_GROUPS.includes(group);
    if (direction === 'consumer') return CONSUMER_GROUPS.includes(group);
    return true;
  }

  function actuatorCandidates(devices, assignments, direction) {
    const result = [];
    for (const device of devices || []) {
      for (const entity of device.entities || []) {
        if (!hasText(entity.command_topic) || !isNumber(entity.min_value) || !isNumber(entity.max_value)) continue;
        const group = deviceRoleGroup(device, assignments);
        const range = t('automations.devices.range', { min: formatNumber(entity.min_value), max: formatNumber(entity.max_value) });
        const unit = entity.unit_of_measurement ? ` ${entity.unit_of_measurement}` : '';
        const subtitle = `${entity.name} · ${range}${unit}`;
        result.push(candidate(device, entity, assignments, subtitle, valueText(entity), actuatorFits(group, direction)));
      }
    }
    return result;
  }

  function meterCandidates(devices, assignments) {
    const result = [];
    for (const device of devices || []) {
      for (const entity of device.entities || []) {
        if (!hasText(entity.state_topic) || !entity.template_supported) continue;
        result.push(candidate(device, entity, assignments, entity.name, valueText(entity), true));
      }
    }
    const rank = (c) => METER_ORDER.indexOf(c.group);
    return result.sort((a, b) => rank(a) - rank(b) || compare(a.deviceName, b.deviceName));
  }

  function switchCandidates(devices, assignments) {
    const result = [];
    for (const device of devices || []) {
      for (const entity of device.entities || []) {
        if (!hasText(entity.command_topic) || !hasText(entity.payload_on) || !hasText(entity.payload_off)) continue;
        result.push(candidate(device, entity, assignments, entity.name, valueText(entity), true));
      }
    }
    return result;
  }

  function setpointCandidates(devices, assignments) {
    return actuatorCandidates(devices, assignments, 'any');
  }

  // Gewaehltes Item zuerst (auch wenn es abgeblendet ist), danach die passenden
  // in Listenreihenfolge, insgesamt hoechstens limit Chips. Die Kachelreihe
  // zeigt davon so viele, wie in eine Zeile passen. missing meldet eine
  // gespeicherte ID, die nicht mehr in der Liste steht.
  function chipList(candidates, selectedId, limit = 12) {
    const list = candidates || [];
    const selected = selectedId ? list.find((c) => c.id === selectedId) : undefined;
    const chips = selected ? [selected] : [];
    for (const c of list) {
      if (chips.length >= limit) break;
      if (c !== selected && c.fits) chips.push(c);
    }
    return { chips, more: list.length, missing: Boolean(selectedId) && !selected };
  }

  function pickerItems(candidates) {
    return (candidates || []).map((c) => ({
      id: c.id,
      name: c.deviceName,
      section: groupLabel(c.group),
      filter: c.group,
      iconRef: c.icon,
      tone: c.tone || undefined,
      subtitle: c.subtitle,
      valueText: c.valueText,
      iconMarkup: '',
      memberOf: '',
      kind: 'device',
      dim: !c.fits,
    }));
  }

  // Kachel fuer eine beliebige Entitaet (Aktion, Bedingung): Geraet, Rolle,
  // Entitaetsname und aktueller Wert. null, wenn die Entitaet fehlt.
  function entityChip(devices, assignments, uniqueId) {
    if (!uniqueId) return null;
    for (const device of devices || []) {
      const entity = (device.entities || []).find((item) => item.unique_id === uniqueId);
      if (entity) return candidate(device, entity, assignments, entity.name, valueText(entity), true);
    }
    return null;
  }

  // Bilanzfelder als Picker-Kandidaten mit Rollen-Icon. fields ist die Liste
  // [Feld, Label] der Seite, eigene Kategorien kommen in die Gruppe "other".
  const FIELD_STYLE = {
    grid_export: 'grid', grid_import: 'grid', gap_applied: 'grid',
    pv: 'inverter', autarkie: 'inverter', eigenverbrauch: 'inverter',
    wallbox: 'wallbox', heat_pump: 'heat_pump',
  };
  const FIELD_ICON = {
    load_total: 'ico-load', base: 'ico-load',
    battery_charge: 'ico-battery', battery_discharge: 'ico-battery', battery_soc: 'ico-battery',
    battery_capacity_kwh: 'ico-battery', battery_energy_kwh: 'ico-battery',
    pv: 'ico-sun', autarkie: 'ico-sun', eigenverbrauch: 'ico-sun',
  };
  function balanceFieldCandidates(fields) {
    return (fields || []).map(([field, label]) => {
      const group = FIELD_STYLE[field] || 'other';
      const style = GROUP_BY_KEY[group];
      return {
        id: field, deviceName: label, entityName: '', group,
        icon: FIELD_ICON[field] || (field.startsWith('custom:') ? 'ico-layers' : style.icon),
        tone: style.tone, subtitle: '', valueText: '', fits: true, topic: '',
      };
    });
  }

  window.__automationDevices = {
    toNumber,
    entityChip,
    balanceFieldCandidates,
    ROLE_GROUPS,
    deviceRoleGroup,
    actuatorCandidates,
    meterCandidates,
    switchCandidates,
    setpointCandidates,
    chipList,
    pickerItems,
  };
})();

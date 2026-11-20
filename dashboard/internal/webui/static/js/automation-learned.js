// Lernwerte in Regeln: Bezugsangabe aufloesen, Vorbelegung, Auswahl und Pruefung.
// resolve spiegelt resolve_ref in services/automation/learned.py und ist gegen dieselben Vektoren getestet.
(() => {
  const DAY = 1440;
  const DEFAULT_MAX_AGE_S = 604800;
  const FUTURE_SKEW_S = 300;
  const CLOCK_RE = /^([01][0-9]|2[0-3]):([0-5][0-9])$/;
  // Ganze Katalogschluessel statt Praefix plus Name, damit die Textpruefung sie findet.
  const KEY_LABELS = {
    usual_on: 'automations.learned.key.usual_on',
    usual_off: 'automations.learned.key.usual_off',
    usual_runtime_s: 'automations.learned.key.usual_runtime_s',
    day_start: 'automations.learned.key.day_start',
    night_rest: 'automations.learned.key.night_rest',
    house_state: 'automations.learned.key.house_state',
  };
  const FIELD_KIND = { start: 'time', end: 'time', hold_seconds: 'duration', threshold: 'number', value: 'number' };
  const LEARNABLE = {
    balance_threshold: ['threshold', 'hold_seconds'],
    topic_value: ['value', 'hold_seconds'],
    entity_value: ['value', 'hold_seconds'],
    time_window: ['start', 'end', 'hold_seconds'],
    sun_window: ['hold_seconds'],
    learned_state: ['hold_seconds'],
  };

  const norm = (m) => ((m % DAY) + DAY) % DAY;
  const isNumber = (v) => typeof v === 'number' && Number.isFinite(v);
  const round60 = (s) => Math.round(s / 60) * 60;

  function clockMinutes(value) {
    if (typeof value !== 'string') return null;
    const m = CLOCK_RE.exec(value);
    return m ? Number(m[1]) * 60 + Number(m[2]) : null;
  }

  function clockText(minutes) {
    const n = norm(minutes);
    return String(Math.floor(n / 60)).padStart(2, '0') + ':' + String(n % 60).padStart(2, '0');
  }

  // Wert aus dem Payload, oder null bei fehlendem, altem, zukuenftigem oder falsch typisiertem Wert.
  function freshValue(payload, kind, nowS, maxAge) {
    if (typeof payload !== 'string') return null;
    let data;
    try {
      data = JSON.parse(payload);
    } catch (e) {
      return null;
    }
    if (!data || typeof data !== 'object' || Array.isArray(data)) return null;
    const at = data.at;
    if (!isNumber(at) || at - nowS > FUTURE_SKEW_S || nowS - at > maxAge) return null;
    const value = data.value;
    if (kind === 'time') return clockMinutes(value) === null ? null : value;
    if (kind === 'state') return typeof value === 'string' && value ? value : null;
    return isNumber(value) ? value : null;
  }

  function resolveNumber(ref, learned, offset) {
    if (learned === null) return { value: ref.fallback + offset, source: 'fallback' };
    if (learned < ref.min) return { value: ref.min + offset, source: 'clamped' };
    if (learned > ref.max) return { value: ref.max + offset, source: 'clamped' };
    return { value: learned + offset, source: 'learned' };
  }

  // Uhrzeiten liegen auf dem Kreis: ausserhalb des Bogens geht der Wert auf die naehere Grenze, bei gleichem Abstand auf min.
  function resolveClock(ref, learned, offset) {
    if (learned === null) return { value: clockText(clockMinutes(ref.fallback) + offset), source: 'fallback' };
    const low = clockMinutes(ref.min);
    const high = clockMinutes(ref.max);
    const value = clockMinutes(learned);
    if (norm(value - low) <= norm(high - low)) return { value: clockText(value + offset), source: 'learned' };
    const toHigh = norm(value - high);
    const toLow = norm(low - value);
    return { value: clockText(toLow <= toHigh ? low + offset : high + offset), source: 'clamped' };
  }

  // Liefert {value, source} fuer kind time, number oder state. Wirft nie, bei Fehlern gilt der Ersatzwert.
  function resolve(ref, kind, payload, nowS) {
    try {
      const maxAge = ref.max_age_s ?? DEFAULT_MAX_AGE_S;
      const learned = freshValue(payload, kind, nowS, maxAge);
      if (kind === 'state') {
        return learned === null
          ? { value: ref.fallback, source: 'fallback' }
          : { value: learned, source: 'learned' };
      }
      const offset = ref.offset ?? 0;
      return kind === 'time' ? resolveClock(ref, learned, offset) : resolveNumber(ref, learned, offset);
    } catch (e) {
      return { value: ref ? ref.fallback : undefined, source: 'fallback' };
    }
  }

  function isRef(value) {
    return value !== null && typeof value === 'object' && !Array.isArray(value) && 'learned' in value;
  }

  // Art des Werts eines Feldes: time, duration, number oder state (Hauszustand). Null, wenn das Feld nicht lernbar ist.
  function kindOf(type, field) {
    if (type === 'learned_state' && field === 'learned') return 'state';
    const fields = LEARNABLE[type] || [];
    return fields.includes(field) ? FIELD_KIND[field] : null;
  }

  // Vorbelegung der Bezugsangabe aus dem festen Wert. Ohne gueltigen Wert keine Vorbelegung.
  function prefill(kind, value) {
    if (kind === 'time') {
      const v = clockMinutes(value);
      if (v === null) return null;
      return { min: clockText(v - 60), max: clockText(v + 60), fallback: clockText(v) };
    }
    if (!isNumber(value)) return null;
    // Bei negativen Werten tauschen die Grenzen die Seiten, damit min nie ueber max liegt.
    const pair = (a, b) => ({ min: Math.min(a, b), max: Math.max(a, b) });
    if (kind === 'duration') {
      const v = round60(value);
      return { ...pair(round60(0.5 * v), round60(3 * v)), fallback: v };
    }
    if (kind === 'number') return { ...pair(0.5 * value, 1.5 * value), fallback: value };
    return null;
  }

  // Lernwerte der passenden Art. Feldnamen wie hold_seconds werden auf ihre Art abgebildet.
  function choices(values, kind) {
    const want = FIELD_KIND[kind] || kind;
    return (Array.isArray(values) ? values : []).filter((v) => v && v.kind === want);
  }

  function deviceName(id, devices) {
    if (Array.isArray(devices)) {
      const hit = devices.find((d) => d && d.id === id);
      return (hit && hit.name) || id;
    }
    const hit = devices && devices[id];
    return (hit && hit.name) || id;
  }

  function keyLabel(key, devices, t) {
    const parts = String(key || '').split('/');
    const name = parts[parts.length - 1];
    if (!Object.prototype.hasOwnProperty.call(KEY_LABELS, name)) return t('automations.learned.key.unknown', { key });
    const params = parts[0] === 'device' ? { device: deviceName(parts[1], devices) } : {};
    return t(KEY_LABELS[name], params);
  }

  // Fehlercodes der Bezugsangabe. Uhrzeiten brauchen keine Pruefung min gegen max, der Bogen ist immer gueltig.
  function validate(ref, kind) {
    const r = ref || {};
    const errors = [];
    if (typeof r.learned !== 'string' || !r.learned) errors.push('learned.missing');
    if (kind === 'time') {
      const low = clockMinutes(r.min);
      const high = clockMinutes(r.max);
      const fallback = clockMinutes(r.fallback);
      // Leere oder ungueltige Grenzen zaehlen wie fehlende.
      if (low === null || high === null || fallback === null) {
        errors.push('learned.missing');
        return errors;
      }
      if (norm(fallback - low) > norm(high - low)) errors.push('learned.fallback_range');
      return errors;
    }
    if (kind === 'state') return errors;
    if (!isNumber(r.min) || !isNumber(r.max) || !isNumber(r.fallback)) {
      errors.push('learned.missing');
      return errors;
    }
    if (r.min > r.max) errors.push('learned.min_max');
    else if (r.fallback < r.min || r.fallback > r.max) errors.push('learned.fallback_range');
    return errors;
  }


  // Herkunftszeile der Bezugsangabe. entry ist der Eintrag des Endpunkts (kind, value, n, at, changed_at, prev), now in Sekunden.
  // Ohne Eintrag bleibt nur der Ersatzwert sagbar, sonst gibt es keine Zeile.
  // i18n-keys: automations.learned.origin.days, automations.learned.origin.days_new, automations.learned.origin.runs, automations.learned.origin.runs_new, automations.learned.origin.clamped, automations.learned.origin.fallback, automations.learned.state.active, automations.learned.state.quiet
  const ORIGIN_KEYS = {
    days: 'automations.learned.origin.days', days_new: 'automations.learned.origin.days_new',
    runs: 'automations.learned.origin.runs', runs_new: 'automations.learned.origin.runs_new',
    clamped: 'automations.learned.origin.clamped', fallback: 'automations.learned.origin.fallback',
  };

  // Wert wie im Feld: Uhrzeit als HH:MM, Laufzeit in Minuten, Zustand als Wort, sonst die Zahl.
  function originValue(value, kind, t) {
    if (kind === 'state') return t(value === 'active' ? 'automations.learned.state.active' : 'automations.learned.state.quiet');
    if (kind === 'time') return String(value);
    if (kind === 'duration' && isNumber(value)) return `${Math.round(value / 60)} min`;
    return String(value);
  }

  function origin(sourceCode, entry, now, t) {
    if (sourceCode === 'fallback') return t(ORIGIN_KEYS.fallback);
    if (!entry || typeof entry !== 'object') return '';
    if (sourceCode === 'clamped') return t(ORIGIN_KEYS.clamped, { value: originValue(entry.value, entry.kind, t) });
    if (sourceCode !== 'learned') return '';
    const runs = entry.kind === 'duration';
    const isNew = entry.prev === undefined || entry.prev === null;
    const since = isNumber(entry.changed_at) && entry.changed_at > 0 ? entry.changed_at : entry.at;
    const params = { n: entry.n, age: window.__automationsView.formatSeconds(now - since) };
    if (!isNew) params.prev = originValue(entry.prev, entry.kind, t);
    const base = runs ? 'runs' : 'days';
    return t(ORIGIN_KEYS[isNew ? base + '_new' : base], params);
  }

  // Skala einer Bezugsangabe: Abschnitte auf 0 bis 1 und die Marke am Wert. Uhrzeiten sind ein Tag,
  // ueber Mitternacht (min nach max) entstehen zwei Abschnitte. Null bei Zustand oder ungueltigen Grenzen.
  function scale(kind, min, max, value) {
    if (kind === 'time') {
      const low = clockMinutes(min);
      const high = clockMinutes(max);
      if (low === null || high === null) return null;
      const segments = low <= high
        ? [{ from: low / DAY, to: high / DAY }]
        : [{ from: low / DAY, to: 1 }, { from: 0, to: high / DAY }];
      const now = clockMinutes(value);
      return { segments, marker: now === null ? null : now / DAY };
    }
    if (kind !== 'number' && kind !== 'duration') return null;
    if (!isNumber(min) || !isNumber(max)) return null;
    const span = max - min;
    const marker = isNumber(value) && span !== 0 ? Math.min(1, Math.max(0, (value - min) / span)) : null;
    return { segments: [{ from: 0, to: 1 }], marker };
  }

  window.AutomationLearned = { isRef, kindOf, resolve, prefill, choices, keyLabel, validate, origin, scale };
})();

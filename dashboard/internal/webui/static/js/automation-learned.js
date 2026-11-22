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
    base_w: 'automations.learned.key.base_w',
    battery_full_at: 'automations.learned.key.battery_full_at',
    night_rest: 'automations.learned.key.night_rest',
    night_wh: 'automations.learned.key.night_wh',
    house_state: 'automations.learned.key.house_state',
    op_power_w: 'automations.learned.key.op_power_w',
    cycle_wh: 'automations.learned.key.cycle_wh',
    pv_reserve_w: 'automations.learned.key.pv_reserve_w',
    surplus_left_wh: 'automations.learned.key.surplus_left_wh',
  };
  const FIELD_KIND = { start: 'time', end: 'time', hold_seconds: 'duration', threshold: 'number', value: 'number', learned: 'number' };
  const LEARNABLE = {
    balance_threshold: ['threshold', 'hold_seconds'],
    topic_value: ['value', 'hold_seconds'],
    entity_value: ['value', 'hold_seconds'],
    time_window: ['start', 'end', 'hold_seconds'],
    sun_window: ['hold_seconds'],
    learned_state: ['hold_seconds'],
    // learned ist der gelernte Zahlwert selbst, threshold darf eine Bezugsangabe sein.
    learned_value: ['learned', 'threshold', 'hold_seconds'],
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

  // Einheit aus dem Namensende eines Lernwerts: _w ist W, _wh ist Wh, sonst keine.
  function unitOfKey(key) {
    const name = String(key || '');
    if (name.endsWith('_wh')) return 'Wh';
    if (name.endsWith('_w')) return 'W';
    return '';
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
  // Optional unit (W, Wh, ...) filters number values, so a threshold in W does not offer energy values.
  function choices(values, kind, unit) {
    const want = FIELD_KIND[kind] || kind;
    return (Array.isArray(values) ? values : []).filter((v) => v && v.kind === want && (!unit || v.unit === unit));
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

  // Zusatz zum Wert in der Zeile "Aktuell": Einheit bei Zahlen, dann "Schaetzung" bei geschaetzten Werten.
  // i18n-keys: automations.learned.estimate
  function valueSuffix(entry, kind, t) {
    if (!entry || typeof entry !== 'object') return '';
    const unit = kind === 'number' && entry.unit ? ` ${entry.unit}` : '';
    const estimate = entry.estimate ? ` (${t('automations.learned.estimate')})` : '';
    return unit + estimate;
  }

  // Die Herkunftszeile endet mit dem Satz zu den anderen Programmen, wenn der Eintrag sie mitbringt (Zyklusenergie).
  // i18n-keys: automations.learned.origin.programs
  function origin(sourceCode, entry, now, t) {
    const base = originBase(sourceCode, entry, now, t);
    const programs = entry && Array.isArray(entry.programs) ? entry.programs.filter(isNumber) : [];
    if (!programs.length) return base;
    const list = programs.map((p) => Math.round(p)).join(', ');
    return [base, t('automations.learned.origin.programs', { programs: list })].filter(Boolean).join(' ');
  }

  function originBase(sourceCode, entry, now, t) {
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

  // Skala einer Bezugsangabe als Strahl von min (0) bis max (1) mit der Marke am Wert.
  // Uhrzeiten laufen den Bogen im Uhrzeigersinn ab, so bleibt auch 23:00 bis 01:00 ein Strahl.
  // Ein Wert ausserhalb liegt auf der naeheren Grenze. Null bei Zustand oder ungueltigen Grenzen.
  function scale(kind, min, max, value) {
    if (kind === 'time') {
      const low = clockMinutes(min);
      const high = clockMinutes(max);
      if (low === null || high === null) return null;
      const span = norm(high - low);
      const now = clockMinutes(value);
      if (now === null || span === 0) return { marker: null };
      const along = norm(now - low);
      if (along <= span) return { marker: along / span };
      return { marker: norm(now - high) < norm(low - now) ? 1 : 0 };
    }
    if (kind !== 'number' && kind !== 'duration') return null;
    if (!isNumber(min) || !isNumber(max)) return null;
    const span = max - min;
    const marker = isNumber(value) && span !== 0 ? Math.min(1, Math.max(0, (value - min) / span)) : null;
    return { marker };
  }

  // Jahresverlauf eines Lernwerts. entry.year liefert hoechstens 53 Wochen als {week: "2026-W30", value}.
  // Unter 8 Wochen gibt es weder Zeile noch Linie.
  const YEAR_MIN_WEEKS = 8;
  const WEEK_RE = /^(\d{4})-W(\d{2})$/;
  const DAY_MS = 86400000;

  // Montag der ISO-Woche als UTC-Datum, null bei ungueltiger Woche.
  function weekMonday(week) {
    const m = WEEK_RE.exec(String(week || ''));
    if (!m) return null;
    const jan4 = new Date(Date.UTC(Number(m[1]), 0, 4));
    const weekday = jan4.getUTCDay() || 7;
    return new Date(jan4.getTime() - (weekday - 1) * DAY_MS + (Number(m[2]) - 1) * 7 * DAY_MS);
  }

  // Monatsname des Montags ueber I18n.formatDate, damit Sprache und Datumsformat der Oberflaeche gelten.
  function monthName(week) {
    const monday = weekMonday(week);
    return monday && window.I18n && window.I18n.formatDate ? window.I18n.formatDate(monday, { month: 'long', timeZone: 'UTC' }) : '';
  }

  // Wochenpunkte mit Zahlenwert zum Sortieren und Vergleichen. Uhrzeiten als Minuten, Zahlen wie sie sind.
  function yearPoints(entry) {
    const list = entry && Array.isArray(entry.year) ? entry.year : [];
    const out = [];
    for (const p of list) {
      if (!p || typeof p.week !== 'string') continue;
      const num = typeof p.value === 'string' ? clockMinutes(p.value) : (isNumber(p.value) ? p.value : null);
      if (num !== null) out.push({ week: p.week, value: p.value, num });
    }
    return out;
  }

  function yearValueText(point) {
    if (typeof point.value === 'string') return clockText(point.num);
    return window.I18n && window.I18n.formatNumber ? window.I18n.formatNumber(point.value) : String(point.value);
  }

  // Satz zum Jahresverlauf: hoechster und tiefster Wochenwert mit ihrem Monat. Leer unter 8 Wochen.
  // i18n-keys: automations.learned.year.line
  function yearLine(entry, t) {
    const points = yearPoints(entry);
    if (points.length < YEAR_MIN_WEEKS) return '';
    let high = points[0];
    let low = points[0];
    for (const p of points) {
      if (p.num > high.num) high = p;
      if (p.num < low.num) low = p;
    }
    return t('automations.learned.year.line', {
      high_month: monthName(high.week),
      high: yearValueText(high),
      low_month: monthName(low.week),
      low: yearValueText(low),
    });
  }

  // Linie des Jahresverlaufs als SVG-Punkte im Kasten 100 x 24. Die Grenzen sind die volle Hoehe des Kastens.
  // Null unter 8 Wochen oder ohne Skala. Werte ausserhalb der Grenzen liegen auf der naeheren Kante.
  function yearPlot(kind, min, max, entry) {
    const points = yearPoints(entry);
    if (points.length < YEAR_MIN_WEEKS) return null;
    const coords = [];
    points.forEach((p, i) => {
      const s = scale(kind, min, max, p.value);
      if (!s || !isNumber(s.marker)) return;
      const x = (i / (points.length - 1)) * 100;
      const y = 2 + (1 - s.marker) * 20;
      coords.push(`${x.toFixed(1)},${y.toFixed(1)}`);
    });
    return coords.length >= YEAR_MIN_WEEKS ? coords.join(' ') : null;
  }

  window.AutomationLearned = { isRef, kindOf, resolve, prefill, choices, keyLabel, unitOfKey, validate, origin, valueSuffix, scale, yearLine, yearPlot };
})();

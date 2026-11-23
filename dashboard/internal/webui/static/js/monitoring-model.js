// Seitenmodell der Monitoring-Seite: reine Funktionen ohne DOM und Alpine.
// t ist (key, params) => string, can ein Objekt mit Rechten, now Unix-Sekunden.
(() => {
  const DAY_S = 86400;
  const COMPASS = ['n', 'ne', 'e', 'se', 's', 'sw', 'w', 'nw'];
  const SHADE_ID_RE = /^learn:shade:(\d+):(\d+)$/;
  const pad = (n) => String(n).padStart(2, '0');

  // Quelle für Filter und Gruppen: Energie-Bilanz und Messketten zusammen, Vorschläge
  // für sich, sonst Lernfunktionen.
  function sourceOf(notice) {
    if (notice.source === 'energy' || notice.source === 'chain') return 'energy';
    return notice.source === 'suggest' ? 'suggest' : 'learn';
  }

  // Lernbefunde und Vorschläge teilen Texte und Belege der Automationen.
  function isLearned(notice) {
    const source = sourceOf(notice);
    return source === 'learn' || source === 'suggest';
  }

  function filterNotices(notices, filter) {
    if (filter === 'energy' || filter === 'learn' || filter === 'suggest') {
      return notices.filter((n) => sourceOf(n) === filter);
    }
    return notices.slice();
  }

  // Neu vor quittiert, Warnungen vor Hinweisen, dann die neuesten zuerst.
  function sortNotices(notices) {
    const rank = (n) => (n.severity === 'warn' ? 0 : 1);
    return notices.slice().sort((a, b) => (!!a.ack - !!b.ack)
      || (rank(a) - rank(b))
      || (b.first_seen - a.first_seen));
  }

  function groups(notices) {
    const out = [
      { id: 'new', items: notices.filter((n) => !n.ack) },
      { id: 'known', items: notices.filter((n) => !!n.ack) },
    ];
    return out.filter((g) => g.items.length);
  }

  function sinceText(firstSeen, now, t) {
    const days = Math.floor((now - firstSeen) / DAY_S);
    if (days <= 0) return t('monitoring.since.today');
    if (days === 1) return t('monitoring.since.yesterday');
    return t('monitoring.since.days', { n: days });
  }

  // Himmelsrichtung als Katalogteil: 'n', 'ne', ... 'nw'. Der Aufrufer übersetzt mit monitoring.compass.<name>.
  function compass(az) {
    const idx = Math.round((((az % 360) + 360) % 360) / 45) % 8;
    return COMPASS[idx];
  }

  // Datum für den Kettenbefund als TT.MM.
  function shortDate(value) {
    const d = new Date(value);
    return Number.isNaN(d.getTime()) ? '' : pad(d.getDate()) + '.' + pad(d.getMonth() + 1) + '.';
  }

  // Kalenderwochen als Bereiche: [4,5,6,7,44,45] -> "4 bis 7, 44 und 45".
  function weekRanges(weeks, t) {
    const sorted = (weeks || []).slice().sort((a, b) => a - b);
    const items = [];
    for (let i = 0; i < sorted.length;) {
      let j = i;
      while (j + 1 < sorted.length && sorted[j + 1] === sorted[j] + 1) j++;
      if (j - i >= 2) {
        items.push(t('monitoring.weeks.range', { from: sorted[i], to: sorted[j] }));
      } else {
        for (let k = i; k <= j; k++) items.push(String(sorted[k]));
      }
      i = j + 1;
    }
    if (items.length < 2) return items.join('');
    return t('monitoring.weeks.and', { a: items.slice(0, -1).join(', '), b: items[items.length - 1] });
  }

  // Platzhalter eines Lernbefunds ohne Verursacher, wie im Vorschlagsband.
  function learnParams(notice, t) {
    const p = { ...(notice.params || {}) };
    if (notice.code === 'baseload.drift' && !p.candidate) p.candidate = t('automations.learn.baseload.candidate.none');
    if (notice.code === 'battery.empty_early' && !p.candidates) p.candidates = t('automations.learn.battery.candidate.none');
    return p;
  }

  // Code und Platzhalter eines Lernbefunds. Ist das Vorschlagsband geladen
  // (automation-suggestions.js), gelten dieselben Umrechnungen wie dort.
  function learnSummary(notice, t) {
    const summary = { code: notice.code, params: learnParams(notice, t) };
    const band = typeof window !== 'undefined' ? window.AutomationSuggestions : null;
    return band && typeof band.resolveSummary === 'function' ? band.resolveSummary(summary, t) : summary;
  }

  function shadeIdParts(id) {
    const m = SHADE_ID_RE.exec(id || '');
    return m ? { az: Number(m[1]), el: Number(m[2]) } : null;
  }

  // Zahlen formatiert I18n, damit Zahlenformat und Sprache der Oberfläche gelten.
  function kwhText(value) {
    const I18n = window.I18n;
    return typeof value === 'number' && I18n && I18n.formatNumber ? I18n.formatNumber(value, 1) : (value == null ? '' : String(value));
  }

  function shadeFacts(notice, p, t) {
    const rows = [
      [t('monitoring.fact.time'), t('monitoring.fact.time_value', { from: p.from, to: p.to })],
      [t('monitoring.fact.weeks'), weekRanges(p.weeks, t)],
    ];
    const id = shadeIdParts(notice.id);
    if (id) {
      rows.push([t('monitoring.fact.direction'), t('monitoring.fact.direction_value', {
        az: id.az, compass: t(`monitoring.compass.${compass(id.az)}`), el: id.el,
      })]);
    }
    rows.push([t('monitoring.fact.loss'), t('monitoring.fact.loss_value', { kwh: kwhText(p.kwh_lost) })]);
    return rows;
  }

  function title(notice, t) {
    if (isLearned(notice)) {
      const s = learnSummary(notice, t);
      const key = 'automations.learn.' + s.code + '.title';
      return t(key, s.params);
    }
    const key = 'monitoring.notice.' + notice.code + '.title';
    return t(key, notice.params);
  }

  function text(notice, t) {
    const p = notice.params || {};
    if (isLearned(notice)) {
      const s = learnSummary(notice, t);
      return t(`automations.learn.${s.code}.text`, s.params);
    }
    if (notice.code === 'measured_no_meter') return t('energy.roles.notices.measured_warning');
    if (notice.code === 'role_needed') return t('energy.chains.role_needed');
    if (notice.code === 'chain') {
      return t(`chains.finding.${p.kind}`, { chain: p.chain, from: p.from, to: p.to, since: shortDate(p.since) });
    }
    return t(`monitoring.notice.${notice.code}.text`, notice.params);
  }

  // Die Zahlen eines Lernbefunds mit dem Format des Vorschlagsbands. Die Seite lädt
  // das Band mit; ohne es bleiben die Zahlen weg.
  function learnFacts(list, t) {
    const band = typeof window !== 'undefined' ? window.AutomationSuggestions : null;
    if (band && typeof band.factRows === 'function') return band.factRows(list, t).map((r) => [r.label, r.value]);
    return [];
  }

  function facts(notice, t) {
    const p = notice.params || {};
    if (notice.code === 'unassigned') {
      const rows = (p.names || []).map((name) => [t('monitoring.fact.value'), name]);
      if ((p.more || 0) > 0) rows.push(['', t('monitoring.fact.more', { n: p.more })]);
      return rows;
    }
    if (sourceOf(notice) === 'learn' && p.kind === 'shade') return shadeFacts(notice, p, t);
    if (isLearned(notice) && Array.isArray(notice.facts) && notice.facts.length) return learnFacts(notice.facts, t);
    return [];
  }

  // Rechte je Sprungziel. Unbekannte Ziele bekommen kein Recht.
  function allowed(target, can) {
    if (!target) return false;
    if (target === 'learned') return true;
    if (target.startsWith('energy')) return !!can.view_energy;
    if (target === 'efficiency') return !!can.view_efficiency;
    if (target === 'devicemap') return !!can.view_device_map;
    if (target === 'action.role_keep') return !!can.edit_energy;
    if (target === 'suggestion') return !!can.view_automations;
    return false;
  }

  function steps(notice, can, t) {
    const rights = can || {};
    const shade = sourceOf(notice) === 'learn' ? shadeIdParts(notice.id) : null;
    return (notice.steps || []).map((s) => {
      const key = 'monitoring.step.' + s.code;
      const ok = allowed(s.target, rights);
      const params = s.code === 'shade_look' && shade
        ? { direction: t(`monitoring.compass.${compass(shade.az)}`), height: shade.el }
        : undefined;
      return {
        text: t(key, params),
        target: ok ? s.target : '',
        ids: s.ids || [],
        chain: s.chain || '',
        label: ok ? t(key + '.button') : '',
      };
    });
  }

  window.MonitoringModel = {
    sourceOf,
    isLearned,
    filterNotices,
    sortNotices,
    groups,
    sinceText,
    title,
    text,
    facts,
    steps,
    compass,
    weekRanges,
  };
})();

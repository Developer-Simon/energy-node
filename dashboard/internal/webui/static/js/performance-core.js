// Reiner Kern der Leistungsseite: Kostenmodell, Stufen, Budgethinweise und das
// Verteilen. Ohne Alpine und DOM. Die Zeilen kommen aus der API (snake_case),
// das Kostenmodell entspricht performance.RowCost und performance.Estimate.
(() => {
  const EPS = 1e-9;

  // Id-Schlüssel der Auswahl: sel[id] in Sekunden, fällt auf die aktuelle Periode zurück.
  const selOf = (row, sel) => (sel && sel[row.id] !== undefined ? sel[row.id] : row.current);

  // Wirkliche Periode: der Scheduler schläft nach der Runde die volle Pause.
  const period = (row, v) => v + row.round_s;

  // Kosten in Prozent bei der Periode v, skaliert mit Pause plus Runde.
  function cost(row, v) {
    const denom = v + row.round_s;
    if (denom <= 0) return 0;
    return row.cost_pct * (row.current + row.round_s) / denom;
  }

  // Grundlast plus Kosten jeder vorhandenen Zeile.
  function total(base, rows, sel) {
    return rows.reduce((sum, row) => (row.present === false ? sum : sum + cost(row, selOf(row, sel))), base);
  }

  // Schnellste Quelle: das fremde Gerät, dazu die eigenen Dienste mit ihrer Auswahl.
  function fastestSource(rows, sel, external) {
    let best = external ? { every: external.every_s, name: external.name, measured: true } : null;
    rows.filter((row) => row.source).forEach((row) => {
      const p = period(row, selOf(row, sel));
      if (!best || p < best.every) best = { every: p, name: row.id, measured: false };
    });
    return best;
  }

  // Netzbilanz: die Zeile, die die Bilanz liefert, mit ihrer wirklichen Periode.
  function balanceSource(rows, sel) {
    const row = rows.find((r) => r.balance);
    return row ? { every: period(row, selOf(row, sel)), name: row.id } : null;
  }

  // Kleinste Stufe, die nicht unter dem Wert liegt.
  function stepAtLeast(row, v) {
    const fit = row.steps.filter((s) => s >= v - EPS);
    return fit.length ? fit[0] : row.steps[row.steps.length - 1];
  }

  // Untergrenze der Stufen. 'half' beginnt bei der Hälfte der schnellsten Quelle,
  // schneller zeigt oder prüft nur früher, nicht öfter. 'balance' wartet auf
  // die Netzbilanz und die Regelprüfung.
  function floorOf(row, rows, sel, external) {
    if (row.follows === 'half') {
      const src = fastestSource(rows, sel, external);
      return src ? Math.max(row.floor, stepAtLeast(row, src.every / 2)) : row.floor;
    }
    if (row.follows === 'balance') {
      const tickRow = rows.find((r) => r.id === 'tick');
      const tick = tickRow ? selOf(tickRow, sel) : 0;
      const bal = balanceSource(rows, sel);
      const need = Math.max(row.floor, tick, bal ? bal.every : 0);
      const fit = row.steps.filter((s) => s <= need + EPS);
      return Math.max(tick, fit.length ? fit[fit.length - 1] : row.steps[0]);
    }
    return row.floor;
  }

  // Wählbare Stufen ab der Untergrenze.
  function stepsOf(row, rows, sel, external) {
    const floor = floorOf(row, rows, sel, external);
    return row.steps.filter((s) => s >= floor);
  }

  // 'ok' oder 'budget': passt die Stufe mit den anderen Auswahlen ins Budget.
  // ctx: {rows, sel, base, available, measuring}.
  function stepState(row, s, ctx) {
    if (ctx.measuring) return 'ok';
    const cur = selOf(row, ctx.sel);
    if (s >= cur) return 'ok';
    const others = total(ctx.base, ctx.rows, ctx.sel) - cost(row, cur);
    return others + cost(row, s) <= ctx.available + EPS ? 'ok' : 'budget';
  }

  // Schnellste Stufe, die allein passt, wenn die anderen Zeilen bei ihrer aktuellen Periode bleiben.
  function aloneMax(row, ctx) {
    const others = total(ctx.base, ctx.rows, ctx.cur) - cost(row, selOf(row, ctx.cur));
    const opts = stepsOf(row, ctx.rows, ctx.sel, ctx.external)
      .filter((v) => v >= row.floor && others + cost(row, v) <= ctx.available);
    return opts.length ? opts[0] : null;
  }

  // Schnellste Stufe mit der aktuellen Auswahl der anderen Zeilen.
  function withSelMax(row, ctx) {
    return stepsOf(row, ctx.rows, ctx.sel, ctx.external).find((s) => stepState(row, s, ctx) === 'ok');
  }

  // Hinweis zum Budget der Zeile, als Katalogschlüssel. Null, wenn nichts zu sagen ist.
  // Reihenfolge: über der Grenze, dann keine schnellere Stufe, dann Auswahl der
  // anderen Zeilen, dann Reserve. Die Auswahl hat Vorrang vor der Reserve, weil
  // die Reserve sonst zu schnell zugesagt würde.
  function budgetNote(row, ctx) {
    if (ctx.measuring) return null;
    const cur = selOf(row, ctx.cur);
    const alone = aloneMax(row, ctx);
    const todayTotal = total(ctx.base, ctx.rows, ctx.cur);
    if (todayTotal > ctx.available + 0.05) {
      return alone === null ? null : { key: 'performance.note.over_slower_enough', params: { seconds: alone }, tone: 'warn' };
    }
    const faster = stepsOf(row, ctx.rows, ctx.sel, ctx.external).some((s) => s < cur);
    if (!faster) return null;
    if (alone === null || alone >= cur) return { key: 'performance.note.no_faster', params: {}, tone: '' };
    const now = withSelMax(row, ctx);
    if (now !== undefined && now > alone) {
      return { key: 'performance.note.after_changes_until', params: { seconds: now }, tone: 'warn' };
    }
    return { key: 'performance.note.reserve_until', params: { seconds: alone }, tone: '' };
  }

  // Auswahl nach "Gleichmäßig verteilen". editable ist die Liste der Zeilen-Ids,
  // die der Nutzer ändern darf. Nicht editierbare Zeilen bleiben auf cur.
  // ctx liefert base, external und measuring. available wird separat übergeben.
  function distribute(rows, cur, available, editable, ctx) {
    const sel = { ...cur };
    const keys = rows.filter((row) => editable.includes(row.id));
    keys.forEach((row) => { sel[row.id] = selOf(row, cur); });
    const state = () => ({ rows, sel, base: ctx.base, available, measuring: ctx.measuring, external: ctx.external });
    const next = (row, dir) => {
      const st = stepsOf(row, rows, sel, ctx.external);
      const i = st.indexOf(sel[row.id]);
      if (i < 0) return st.find((s) => (dir > 0 ? s > sel[row.id] : s < sel[row.id]));
      return st[i + dir];
    };

    let guard = 0;
    while (total(ctx.base, rows, sel) > available && guard++ < 80) {
      const candidates = keys.filter((row) => next(row, 1) !== undefined)
        .sort((a, b) => cost(b, sel[b.id]) - cost(a, sel[a.id]));
      if (!candidates.length) break;
      sel[candidates[0].id] = next(candidates[0], 1);
    }

    if (total(ctx.base, rows, sel) < available) {
      let moved = true;
      while (moved) {
        moved = false;
        keys.forEach((row) => {
          const v = [...stepsOf(row, rows, sel, ctx.external)].reverse().find((s) => s < sel[row.id]);
          if (v !== undefined && stepState(row, v, state()) === 'ok') {
            sel[row.id] = v;
            moved = true;
          }
        });
      }
    }
    return sel;
  }

  const api = {
    period, cost, total, fastestSource, floorOf, stepsOf, stepState, aloneMax, budgetNote, distribute,
  };
  if (typeof window !== 'undefined') window.PerformanceCore = api;
  if (typeof module !== 'undefined') module.exports = api;
})();

// dashboard/internal/webui/static/js/history-rollup.js
// Reine Rechenfunktionen der Verlaufs-Historie: Normalisieren, Bucketing,
// Stufenwahl, Anzeige-Verdichtung, Speicherabschaetzung. Bewusst ohne DOM-
// und ohne IndexedDB-Zugriff - dadurch ist der gesamte Rechenteil der
// Historie ohne Browser und ohne Chart testbar (siehe
// test/history-rollup.test.mjs). Alles Zustandsbehaftete liegt in
// history-store.js, alles Anzeigende in history.js.
(() => {
  const MINUTE_MS = 60000;
  const FIVE_MINUTE_MS = 300000;

  // Ein Rohsatz {series, ts, v, u} und ein Verdichtungssatz
  // {series, ts, min, max, avg, n, u} werden hier auf die zweite Form
  // vereinheitlicht. Dadurch braucht bucket() nur einen Codepfad und
  // funktioniert fuer roh -> 1m genauso wie fuer 1m -> 5m.
  const normalize = rows => rows.map(row => {
    if (typeof row.avg === 'number') return row;
    const value = Number(row.v);
    return {series: row.series, ts: row.ts, min: value, max: value, avg: value, n: 1, u: row.u};
  });

  const bucket = (rows, bucketMs) => {
    const groups = new Map();
    normalize(rows).forEach(row => {
      const start = Math.floor(row.ts / bucketMs) * bucketMs;
      // \u0000 als Trenner, weil es in keiner Entitaets-ID und in keinem
      // Rollennamen vorkommen kann - ein Doppelpunkt waere an "role:pv"
      // bereits mehrdeutig. Die Escape-Schreibweise bitte so lassen: ein
      // literales Nullbyte im Quelltext macht die Datei fuer grep und
      // diff zur Binaerdatei.
      const key = `${row.series}\u0000${start}`;
      const current = groups.get(key);
      if (!current) {
        groups.set(key, {series: row.series, ts: start, min: row.min, max: row.max, sum: row.avg * row.n, n: row.n, u: row.u});
        return;
      }
      current.min = Math.min(current.min, row.min);
      current.max = Math.max(current.max, row.max);
      // Gewichtete Summe statt Mittelwert der Mittelwerte: sonst zaehlt ein
      // Bucket mit einem einzigen Messwert genauso viel wie einer mit
      // sechzig, und der Verlauf driftet mit jeder Verdichtungsstufe weiter.
      current.sum += row.avg * row.n;
      current.n += row.n;
      if (!current.u) current.u = row.u;
    });
    return [...groups.values()]
      .map(group => ({series: group.series, ts: group.ts, min: group.min, max: group.max, avg: group.sum / group.n, n: group.n, u: group.u}))
      .sort((left, right) => left.series === right.series ? left.ts - right.ts : (left.series < right.series ? -1 : 1));
  };

  const selectTier = (spanMs, windows) => {
    if (spanMs <= windows.rawWindowMs) return 'raw';
    if (spanMs <= windows.minuteWindowMs) return '1m';
    return '5m';
  };

  const thin = (rows, maxPoints) => {
    if (rows.length <= maxPoints) return rows;
    const timestamps = rows.map(row => row.ts);
    const span = Math.max(...timestamps) - Math.min(...timestamps);
    // bucket() richtet Buckets an absoluten Epoch-Vielfachen aus, nicht am
    // fruehesten Zeitstempel der Reihe - das ist Absicht, denn nur so
    // landen bei der echten Verdichtung (Task 7) zu unterschiedlichen
    // Zeitpunkten geschriebene Werte in denselben Minuten-/Fuenf-Minuten-
    // Buckets. Dadurch kann der erste Zeitstempel an einer beliebigen
    // Stelle innerhalb eines Buckets liegen, und die Reihe kann dadurch
    // einen Bucket mehr als span/bucketMs ueberdecken. Der Nenner
    // maxPoints - 1 statt maxPoints faengt genau dieses eine
    // Randbucket ab, damit die Zusage "hoechstens maxPoints Punkte" auch
    // im ungluecklichsten Ausrichtungsfall haelt.
    const bucketMs = Math.max(1, Math.ceil(span / Math.max(1, maxPoints - 1)));
    return bucket(rows, bucketMs);
  };

  const median = values => {
    const sorted = [...values].sort((left, right) => left - right);
    const mid = Math.floor(sorted.length / 2);
    return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
  };

  // Findet Zeitraeume ohne echte Aufzeichnung (Tab inaktiv, Geraet im
  // Schlaf), damit history.js dort keine erfundene Gerade zeichnet. Der
  // "uebliche" Abstand wird aus den Punkten selbst geschaetzt (Median der
  // Abstaende) statt aus dem konfigurierten Aufzeichnungsintervall - so
  // funktioniert die Erkennung unveraendert nach dem Verdichten (thin())
  // und unabhaengig von der gewaehlten Stufe (roh/1m/5m).
  //
  // Mindestschwelle DEFAULT_MIN_GAP_MS zusaetzlich zum Multiplikator: bei
  // kurzen Aufzeichnungsintervallen (Standard 10s) laege die reine
  // Multiplikator-Schwelle bei 50s, aber Browser drosseln Timer in
  // Hintergrund-/reduzierten Tabs auf bis zu 60s zwischen Aufrufen. Ohne
  // Untergrenze wuerde dieses normale Throttling faelschlich als
  // Aufzeichnungsluecke erkannt.
  const DEFAULT_MIN_GAP_MS = MINUTE_MS * 1.5;

  const detectGaps = (points, thresholdMultiplier = 5, minGapMs = DEFAULT_MIN_GAP_MS) => {
    if (points.length < 2) return [];
    const deltas = [];
    for (let index = 1; index < points.length; index += 1) deltas.push(points[index].ts - points[index - 1].ts);
    const typical = median(deltas);
    if (!(typical > 0)) return [];
    const limit = Math.max(typical * thresholdMultiplier, minGapMs);
    const gaps = [];
    for (let index = 1; index < points.length; index += 1) {
      const from = points[index - 1].ts;
      const to = points[index].ts;
      if (to - from > limit) gaps.push({from, to});
    }
    return gaps;
  };

  // Wie weit zwei Ergaenzungssaetze auseinanderliegen duerfen, damit die
  // Linie zwischen ihnen durchgezogen wird. Die Saetze der 1m-Stufe stehen
  // auf dem Minutenraster; fehlt dazwischen einer, hat auch der Austausch
  // nichts, und das bleibt eine Luecke.
  const FILL_MAX_STEP_MS = 2 * MINUTE_MS;

  // Zeitfenster, in denen dieses Geraet nicht aufgezeichnet hat. Anders als
  // detectGaps() zaehlen hier auch die beiden Fensterraender mit - und genau
  // die sind der haeufigste Fall: war der Browser die erste Haelfte des
  // Zeitraums geschlossen, liegt zwischen zwei vorhandenen Punkten gar keine
  // Luecke, wohl aber vor dem ersten.
  const recordingGaps = (rows, from, to) => {
    const ticks = [...new Set(rows.map(row => row.ts))].sort((left, right) => left - right);
    if (!ticks.length) return [{from, to}];
    const gaps = detectGaps(ticks.map(ts => ({ts})));
    const first = ticks[0];
    const last = ticks[ticks.length - 1];
    if (first - from > DEFAULT_MIN_GAP_MS) gaps.unshift({from, to: first});
    if (to - last > DEFAULT_MIN_GAP_MS) gaps.push({from: last, to});
    return gaps;
  };

  // Binaere Suche statt eines Durchlaufs ueber alle Luecken je Satz: der
  // Chart laedt bei jedem Aufzeichnungstakt neu, und "fuer jeden 1m-Satz
  // einmal linear durch die Luecken" war O(Saetze x Luecken).
  const insideGap = (gaps, ts) => {
    let low = 0;
    let high = gaps.length - 1;
    while (low <= high) {
      const mid = (low + high) >> 1;
      if (ts <= gaps[mid].from) high = mid - 1;
      else if (ts >= gaps[mid].to) low = mid + 1;
      else return true;
    }
    return false;
  };

  // Was von einer Luecke uebrigbleibt, nachdem der Austausch sie teilweise
  // gefuellt hat. Ein einzelner Ergaenzungssatz mitten in einer
  // mehrstuendigen Luecke macht daraus keine durchgehende Aufzeichnung -
  // links und rechts von ihm bleibt je eine Rest-Luecke stehen.
  const uncoveredGaps = (gaps, fill) => {
    if (!fill.length) return gaps;
    const stamps = [...new Set(fill.map(row => row.ts))].sort((left, right) => left - right);
    const rest = [];
    let index = 0;
    gaps.forEach(gap => {
      while (index < stamps.length && stamps[index] <= gap.from) index += 1;
      let edge = gap.from;
      while (index < stamps.length && stamps[index] < gap.to) {
        if (stamps[index] - edge > FILL_MAX_STEP_MS) rest.push({from: edge, to: stamps[index]});
        edge = stamps[index];
        index += 1;
      }
      if (gap.to - edge > FILL_MAX_STEP_MS) rest.push({from: edge, to: gap.to});
    });
    return rest;
  };

  // Luecken je Serie, ohne Fuellung - fuer die 1m-/5m-Stufen, die direkt aus
  // derselben Tabelle lesen und nichts zu ergaenzen haben.
  const gapsBySeries = (rows, from, to) => {
    const bySeries = new Map();
    rows.forEach(row => {
      if (!bySeries.has(row.series)) bySeries.set(row.series, []);
      bySeries.get(row.series).push(row);
    });
    const gaps = new Map();
    bySeries.forEach((points, series) => gaps.set(series, recordingGaps(points, from, to)));
    return gaps;
  };

  // Fuellt Aufzeichnungsluecken der Rohstufe mit Minutenmitteln aus der
  // 1m-Stufe - eigene Vorabverdichtung wie vom Austausch empfangene stehen
  // dort nebeneinander (siehe exchange_compacted_until in
  // history-maintenance.js), und dieser Code muss sie nicht unterscheiden.
  //
  // Zurueck kommen die ergaenzten Saetze UND die Rest-Luecken. Die Luecken
  // hier mitzuliefern ist der Kern: sonst muesste das Chart sie aus der
  // fertig gemischten Reihe zurueckrechnen, und dort stehen 10s-Rohpunkte
  // neben 60s-Mittelwerten - detectGaps() schaetzt seine Schwelle aus dem
  // Median der Abstaende und kippt dann je nach Mischungsverhaeltnis.
  //
  // Ergaenzte Saetze bleiben bewusst unmarkiert: eine Kennzeichnung im Chart
  // kostete einen Marker je Punkt und eine Annotation je Abschnitt, und beide
  // sind auf einem Pi teurer als der Erkenntnisgewinn. Woher die Werte
  // stammen, sagt der Hinweis ueber dem Diagramm (exchangeNotice).
  const fillRawGaps = (rawRows, rollupRows, from, to) => {
    const bySeries = new Map();
    const ensure = series => {
      if (!bySeries.has(series)) bySeries.set(series, {raw: [], fill: []});
      return bySeries.get(series);
    };
    rawRows.forEach(row => ensure(row.series).raw.push(row));

    const gaps = new Map();
    bySeries.forEach((entry, series) => gaps.set(series, recordingGaps(entry.raw, from, to)));
    // Eine Serie, die dieses Geraet nie selbst gemessen hat, kommt in rawRows
    // gar nicht vor - fuer sie ist das ganze Fenster eine Luecke.
    rollupRows.forEach(row => {
      if (gaps.has(row.series)) return;
      ensure(row.series);
      gaps.set(row.series, [{from, to}]);
    });

    rollupRows.forEach(row => {
      const seriesGaps = gaps.get(row.series);
      if (seriesGaps && insideGap(seriesGaps, row.ts)) ensure(row.series).fill.push(row);
    });

    const rows = [];
    const rest = new Map();
    bySeries.forEach((entry, series) => {
      // Einzeln anhaengen statt push(...entry.raw): der Spread legt jeden
      // Satz als eigenes Argument auf den Stack, und ein Rohfenster hat
      // ohne Weiteres fuenfstellig viele.
      entry.raw.forEach(row => rows.push(row));
      entry.fill.forEach(row => rows.push(row));
      rest.set(series, uncoveredGaps(gaps.get(series) || [], entry.fill));
    });
    return {rows, gaps: rest};
  };

  // Setzt direkt nach dem letzten echten Punkt vor einer Luecke einen
  // Null-Punkt - ApexCharts zieht dann keine Linie mehr durch den
  // unbelegten Zeitraum. 1ms Abstand reicht, damit der Punkt zeitlich vor
  // der Luecke bleibt, aber auf der Zeitachse nicht sichtbar abweicht.
  //
  // Gesucht wird der letzte Punkt VOR dem Luecken-Anfang, nicht ein Punkt
  // MIT genau diesem Zeitstempel: die Luecken stehen auf den ungeduennten
  // Saetzen fest, gezeichnet wird die von thin() verdichtete Reihe, und
  // deren Zeitstempel liegen auf Bucket-Grenzen. Ein exakter Treffer wie in
  // der vorigen Fassung setzt voraus, dass Luecken und Punkte aus derselben
  // Reihe stammen - eine Bedingung, die kein Aufrufer erzwingen konnte.
  const withGapBreaks = (pairs, gaps) => {
    if (!gaps.length || !pairs.length) return pairs;
    const result = [];
    let index = 0;
    pairs.forEach((pair, position) => {
      result.push(pair);
      const next = pairs[position + 1];
      if (!next) return;
      while (index < gaps.length && gaps[index].from < pair[0]) index += 1;
      if (index < gaps.length && gaps[index].from < next[0]) {
        result.push([pair[0] + 1, null]);
        index += 1;
      }
    });
    return result;
  };

  // Sortiert Intervalle ({from,to}) und verschmilzt echt ueberlappende zu
  // disjunkten. Die abgeleitete Hausverbrauch-Serie erbt die Vereinigung der
  // Rollen-Luecken - ohne diesen Schritt stehen dort Dutzende deckungsgleicher
  // Intervalle, die gapStubs zu quer ueber den Chart laufenden Strichen
  // verketten wuerde. Nur beruehrende Intervalle (from == to des Vorgaengers)
  // bleiben getrennt: dazwischen liegt ein echter Punkt.
  const mergeIntervals = (intervals) => {
    const sorted = [...intervals].sort((a, b) => a.from - b.from);
    const out = [];
    for (const iv of sorted) {
      const last = out[out.length - 1];
      if (last && iv.from < last.to) last.to = Math.max(last.to, iv.to);
      else out.push({from: iv.from, to: iv.to});
    }
    return out;
  };

  // Gegenstueck zu withGapBreaks: die blasse "Geist"-Serie, die eine Luecke
  // nicht mehr grau hinterlegt, sondern den letzten bekannten Wert an jedem
  // Luecken-Rand ein kurzes Stueck flach in die Luecke hinein weiterzieht und
  // dort auslaufen laesst. reachMs begrenzt, wie weit ein Stummel reicht -
  // hoechstens bis zur Luecken-Mitte, damit sich die beiden Raender nie
  // beruehren. Jeder Stummel liegt komplett zwischen gap.from und gap.to und
  // ist beidseitig von einem Null-Punkt eingezaeunt: so verbindet ApexCharts
  // ihn nie mit echten Daten oder mit dem naechsten Stummel, egal in welcher
  // Reihenfolge oder wie oft eine Luecke in gaps steht.
  const gapStubs = (pairs, gaps, reachMs) => {
    if (!gaps.length || !pairs.length || !(reachMs > 0)) return [];
    const real = pairs.filter(point => point[1] !== null);
    if (!real.length) return [];
    const out = [];
    // Jeder x-Wert wird gegen den bisher hoechsten geklemmt: die Ausgabe ist
    // damit global monoton steigend, egal wie die Luecken liegen. Ohne das
    // zieht ApexCharts bei einem x-Ruecksprung eine Linie quer ueber den
    // ganzen Chart.
    let cursor = -Infinity;
    const push = (x, value) => {
      const cx = x > cursor ? x : cursor;
      out.push([cx, value]);
      cursor = cx;
    };
    mergeIntervals(gaps).forEach(gap => {
      const span = Math.max(gap.to - gap.from, 0);
      const reach = Math.min(reachMs, span > 0 ? span / 2 : reachMs);
      if (!(reach > 0)) return;
      let before = null;
      for (const point of real) {
        if (point[0] <= gap.from) before = point;
        else break;
      }
      const after = real.find(point => point[0] >= gap.to) || null;
      if (before) {
        const end = Math.min(gap.from + reach, gap.to);
        push(gap.from - 1, null);
        push(gap.from, before[1]);
        push(end, before[1]);
        push(end + 1, null);
      }
      if (after) {
        const start = Math.max(gap.to - reach, gap.from);
        push(start - 1, null);
        push(start, after[1]);
        push(gap.to, after[1]);
        push(gap.to + 1, null);
      }
    });
    return out;
  };

  const estimateBytesPerDay = ({intervalSeconds, seriesCount, rawBytes, rollupBytes}) => {
    const perDay = Math.floor(86400 / Math.max(1, intervalSeconds));
    return {
      raw: perDay * seriesCount * rawBytes,
      minute: 1440 * seriesCount * rollupBytes,
      fiveMinute: 288 * seriesCount * rollupBytes,
    };
  };

  window.HistoryRollup = {
    MINUTE_MS, FIVE_MINUTE_MS, normalize, bucket, selectTier, thin, detectGaps, withGapBreaks,
    gapStubs, mergeIntervals, estimateBytesPerDay, recordingGaps, uncoveredGaps, gapsBySeries, fillRawGaps,
  };
})();

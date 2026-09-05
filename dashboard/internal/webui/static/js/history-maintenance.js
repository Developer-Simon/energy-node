// dashboard/internal/webui/static/js/history-maintenance.js
// Wartung der Verlaufs-Historie: Verdichtung (diese Datei, compact) und
// Aufbewahrung (evict, Task 8). Laeuft ausschliesslich im fuehrenden Tab -
// zwei gleichzeitig verdichtende Tabs wuerden sich gegenseitig Saetze unter
// den Fuessen wegloeschen.
//
// Die Wasserstandsmarken in "meta" sind der Grund, warum ein Lauf guenstig
// ist: verdichtet wird immer nur die Spanne zwischen der letzten Marke und
// dem aktuellen Fensterrand, nie der gesamte Bestand.
(() => {
  const HOUR_MS = 3600 * 1000;
  const DAY_MS = 24 * HOUR_MS;

  // Wie lange nach Ablauf einer Minute gewartet wird, bevor die
  // Vorabverdichtung sie anfasst (siehe exchangeCompactedFrom unten). Der
  // Zeitstempel eines Rohsatzes ist die Server-Schnappschusszeit beim Abruf,
  // nie rueckdatiert - eine abgelaufene Minute bekommt also nie mehr einen
  // Rohwert nachgereicht. Die Karenz federt nur Wartungstakt und
  // Netzlatenz ab, nicht ein echtes Nachliefer-Risiko.
  const EXCHANGE_GRACE_MS = 2 * 60 * 1000;

  const compactTier = async ({from, to, sourceTier, targetTier, bucketMs, watermark, deleteSource = true}) => {
    const store = window.HistoryStore;
    if (to <= from) return [];
    const rows = await store.readRange(sourceTier, null, from, to);
    if (!rows.length) {
      await store.setMeta(watermark, to);
      return [];
    }
    const bucketed = window.HistoryRollup.bucket(rows, bucketMs);
    await store.writeRollup(targetTier, bucketed);
    if (deleteSource) await store.deleteKeys(sourceTier, rows.map(row => [row.series, row.ts]));
    await store.setMeta(watermark, to);
    return bucketed;
  };

  const compact = async (config, now) => {
    const store = window.HistoryStore;
    const rollup = window.HistoryRollup;

    const rawFrom = Number(await store.meta('raw_compacted_until')) || 0;
    const rawTo = now - Number(config.rawWindowHours) * HOUR_MS;
    // minuteRows sind auch das, was der Verlauf-Austausch anbietet (siehe
    // afterMaintenance() in history-recorder.js) - vor Ablauf von
    // rawWindowHours (Standard 24h) hat ein frisch startender Tab also
    // nichts zum Tauschen, egal wie lange zwei Geraete gleichzeitig laufen.
    const minuteRows = await compactTier({
      from: rawFrom, to: rawTo, sourceTier: 'raw', targetTier: '1m',
      bucketMs: rollup.MINUTE_MS, watermark: 'raw_compacted_until',
    });

    // Vorabverdichtung fuer den Austausch: dieselben Rohdaten noch einmal,
    // aber schon zwei Minuten nach ihrem Ende statt erst nach
    // rawWindowHours - und OHNE sie aus raw zu loeschen (deleteSource:
    // false), damit die Rohansicht des laufenden Tages unveraendert 24h
    // erhalten bleibt. Die eigene Wasserstandsmarke laeuft der von oben
    // immer voraus (rawWindowHours ist mindestens eine Stunde, die Karenz
    // hier zwei Minuten), deshalb liest sie nur Saetze, die der obere Lauf
    // in diesem Durchgang bereits geloescht hat, wenn beide Fenster sich
    // ueberschneiden - kein doppeltes Verdichten derselben Saetze in einem
    // Durchgang. Erreicht die obere Marke Tage spaeter dieselbe Spanne,
    // verdichtet sie sie ein zweites Mal (deckungsgleich, da bucket() bei
    // unveraenderten Eingaben deterministisch ist) und loescht dann
    // tatsaechlich - das ist der Preis fuer eine unveraenderte Rohansicht,
    // nicht ein Fehler.
    const exchangeFrom = Number(await store.meta('exchange_compacted_until')) || 0;
    const exchangeTo = now - EXCHANGE_GRACE_MS;
    const exchangeRows = await compactTier({
      from: exchangeFrom, to: exchangeTo, sourceTier: 'raw', targetTier: '1m',
      bucketMs: rollup.MINUTE_MS, watermark: 'exchange_compacted_until', deleteSource: false,
    });

    const minuteFrom = Number(await store.meta('minute_compacted_until')) || 0;
    const minuteTo = now - Number(config.minuteWindowDays) * DAY_MS;
    const fiveMinuteRows = await compactTier({
      from: minuteFrom, to: minuteTo, sourceTier: '1m', targetTier: '5m',
      bucketMs: rollup.FIVE_MINUTE_MS, watermark: 'minute_compacted_until',
    });

    // toMinute/toFiveMinute bleiben als Anzahl des regulaeren Laufs
    // erhalten - bestehende Tests und Anzeigen zaehlen damit; minuteRows
    // traegt zusaetzlich die frueh verdichteten Saetze zum Server-Ringpuffer
    // und ins naechste Angebot.
    return {
      toMinute: minuteRows.length, toFiveMinute: fiveMinuteRows.length, toMinuteEarly: exchangeRows.length,
      minuteRows: [...minuteRows, ...exchangeRows], fiveMinuteRows,
    };
  };

  const TIERS = ['raw', '1m', '5m'];

  // Wieviel unter das Budget evakuiert wird. Ohne diese Hysterese laege das
  // Konto nach jedem Lauf exakt auf der Grenze und der naechste Messpunkt
  // loeste sofort den naechsten Lauf aus.
  const EVICT_TARGET_RATIO = 0.9;

  // Groesse eines Evakuierungsschritts. Kleiner heisst genauer, aber mehr
  // Durchlaeufe ueber den Bestand; eine Stunde ist bei jeder sinnvollen
  // Abtastrate ein spuerbarer Happen.
  const EVICT_CHUNK_MS = HOUR_MS;

  const evictBefore = async cutoff => {
    let removed = 0;
    for (const tier of TIERS) {
      // eslint-disable-next-line no-await-in-loop
      removed += await window.HistoryStore.deleteRange(tier, null, 0, cutoff);
    }
    return removed;
  };

  const evictByTime = async (config, now) => {
    const store = window.HistoryStore;
    const cutoff = now - Number(config.retentionHours) * HOUR_MS;
    // Die Marke erspart den Durchlauf ueber den Bestand, wenn seit dem
    // letzten Lauf nichts abgelaufen ist - das ist der Normalfall.
    const evictedUntil = Number(await store.meta('evicted_until')) || 0;
    if (cutoff <= evictedUntil) return {mode: 'time', removed: 0};
    const removed = await evictBefore(cutoff);
    await store.setMeta('evicted_until', cutoff);
    return {mode: 'time', removed};
  };

  const evictBySize = async config => {
    const store = window.HistoryStore;
    const budgetBytes = Number(config.budgetMB) * 1024 * 1024;
    // Ein einzelner meta-Lesevorgang - deshalb ist der Normalfall "Budget
    // reicht" hier genauso guenstig wie im Zeitmodus.
    if (await store.bytesUsed() <= budgetBytes) return {mode: 'size', removed: 0};

    const target = budgetBytes * EVICT_TARGET_RATIO;
    const bounds = await store.bounds();
    if (bounds.oldest === null) return {mode: 'size', removed: 0};

    let cutoff = bounds.oldest;
    let removed = 0;
    // Stundenweise von vorne, bis das Konto unter der Zielmarke liegt. Die
    // Obergrenze verhindert eine Endlosschleife, falls das Byte-Konto durch
    // einen Fehler von der Wirklichkeit abgewichen ist.
    for (let step = 0; step < 100000; step += 1) {
      cutoff += EVICT_CHUNK_MS;
      // eslint-disable-next-line no-await-in-loop
      removed += await evictBefore(cutoff);
      // eslint-disable-next-line no-await-in-loop
      if (await store.bytesUsed() <= target) break;
      if (bounds.newest !== null && cutoff >= bounds.newest) break;
    }
    return {mode: 'size', removed};
  };

  const evict = (config, now) => config.retentionMode === 'size'
    ? evictBySize(config)
    : evictByTime(config, now);

  const run = async (config, now) => ({
    compacted: await compact(config, now),
    evicted: await evict(config, now),
  });

  window.HistoryMaintenance = {compact, evict, run, HOUR_MS, DAY_MS, EXCHANGE_GRACE_MS};
})();

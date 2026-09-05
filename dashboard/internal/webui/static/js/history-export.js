// dashboard/internal/webui/static/js/history-export.js
// Serialisierung der Verlaufsdaten fuer den Export.
//
// Die Ausgabe ist bewusst eine Stueckliste und keine zusammengesetzte
// Zeichenkette: bei einem Speicherbudget von einem Gigabyte wuerde ein
// join('') ueber alle Zeilen den Tab vor dem Download umbringen. Blob nimmt
// die Liste direkt entgegen.
//
// Trennzeichen ist das Semikolon, weil die Dateien hier in einem deutschen
// Tabellenprogramm geoeffnet werden, das Komma als Dezimaltrenner liest.
(() => {
  const CHUNK_LINES = 1000;
  const SEPARATOR = ';';

  const isoOf = ts => new Date(ts).toISOString();

  const escape = value => {
    const text = String(value);
    return text.includes(SEPARATOR) || text.includes('"') || text.includes('\n')
      ? `"${text.replace(/"/g, '""')}"`
      : text;
  };

  const headerLines = meta => [
    '# Energy Node Verlaufs-Export',
    `# erzeugt: ${isoOf(Date.now())}`,
    `# zeitraum: ${isoOf(meta.from)} bis ${isoOf(meta.to)}`,
    `# stufe: ${meta.tier} (${meta.tierLabel})`,
    `# kennwert: ${meta.aggregate}`,
    `# abtastung: ${meta.intervalSeconds} s`,
    `# zeitzone: ${meta.timezone}`,
    `# quelle: ${meta.source}`,
    // '#' statt '' als Trennzeile: eine leere Zeile hier waere selbst keine
    // Kommentarzeile mehr und wuerde in jeder Zaehlung "Zeilen ohne '#'"
    // faelschlich als Datenzeile mitzaehlen.
    '#',
  ].map(line => `${line}\n`).join('');

  const toCSV = ({rows, meta}) => {
    const chunks = [headerLines(meta), `zeitpunkt${SEPARATOR}serie${SEPARATOR}einheit${SEPARATOR}min${SEPARATOR}max${SEPARATOR}mittel${SEPARATOR}anzahl\n`];
    let buffer = [];
    rows.forEach((row, index) => {
      buffer.push([
        isoOf(row.ts), escape(row.series), escape(row.u || ''),
        row.min, row.max, row.avg, row.n,
      ].join(SEPARATOR));
      if (buffer.length >= CHUNK_LINES || index === rows.length - 1) {
        chunks.push(`${buffer.join('\n')}\n`);
        buffer = [];
      }
    });
    return chunks;
  };

  const toJSON = ({rows, meta}) => {
    const chunks = [`{"meta":${JSON.stringify({
      generated: isoOf(Date.now()),
      from: isoOf(meta.from),
      to: isoOf(meta.to),
      tier: meta.tier,
      tier_label: meta.tierLabel,
      aggregate: meta.aggregate,
      interval_seconds: meta.intervalSeconds,
      timezone: meta.timezone,
      source: meta.source,
    })},"rows":[`];
    let buffer = [];
    rows.forEach((row, index) => {
      buffer.push(JSON.stringify({
        ts: isoOf(row.ts), series: row.series, unit: row.u || '',
        min: row.min, max: row.max, avg: row.avg, n: row.n,
      }));
      if (buffer.length >= CHUNK_LINES || index === rows.length - 1) {
        chunks.push(buffer.join(',') + (index === rows.length - 1 ? '' : ','));
        buffer = [];
      }
    });
    chunks.push(']}');
    return chunks;
  };

  const filename = (meta, extension) => {
    const stamp = isoOf(meta.from).slice(0, 10);
    const until = isoOf(meta.to).slice(0, 10);
    return `energy-node-verlauf-${stamp}_bis_${until}-${meta.tier}.${extension}`;
  };

  const download = (chunks, name, mime) => {
    const blob = new Blob(chunks, {type: mime});
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = name;
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
    // Ohne revoke bleibt der Blob bis zum Neuladen der Seite im Speicher -
    // bei einem Export von hunderten Megabyte ist das spuerbar.
    URL.revokeObjectURL(url);
  };

  window.HistoryExport = {toCSV, toJSON, filename, download};
})();

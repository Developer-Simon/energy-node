// dashboard/internal/webui/static/js/history-store.js
// IndexedDB-Speicherschicht der Verlaufs-Historie. Version 2 loest die
// Version-1-Struktur ab, die den Zeitstempel als ISO-String im keyPath
// fuehrte und bei jedem Schreibvorgang den gesamten Store per Cursor nach
// abgelaufenen Saetzen durchsuchte. Beides traegt keine grossen Datenmengen:
// erst ein numerischer Zeitstempel erlaubt IDBKeyRange-Bereichsabfragen und
// -loeschungen, und erst die drei Stufen halten den Umfang beherrschbar.
//
// Version 1 wird nicht migriert, sondern verworfen - sie hielt hoechstens
// sechs Stunden Browser-Daten.
(() => {
  const databaseName = 'energy-node-dashboard';
  const databaseVersion = 2;

  const TIERS = {raw: 'samples_raw', '1m': 'samples_1m', '5m': 'samples_5m'};
  const META_STORE = 'meta';

  // Geschaetzte Kosten eines Satzes inklusive Index-Overhead, einmal auf der
  // Zielplattform ausgemessen. Sie sind der Regler fuer den Speichermodus:
  // navigator.storage.estimate() liefert nur den Verbrauch des gesamten
  // Origins, grob gerundet, und taugt deshalb nicht zum Regeln - wohl aber
  // als angezeigte Gegenprobe. Aendert sich die Satzform, muessen diese
  // beiden Zahlen neu gemessen werden.
  const ROW_BYTES_RAW = 120;
  const ROW_BYTES_ROLLUP = 160;

  let databasePromise = null;

  const open = () => {
    if (databasePromise) return databasePromise;
    databasePromise = new Promise((resolve, reject) => {
      const request = window.indexedDB.open(databaseName, databaseVersion);
      request.onupgradeneeded = () => {
        const database = request.result;
        // Version 1 hiess "energy-samples" und ist unbrauchbar geworden.
        if (database.objectStoreNames.contains('energy-samples')) database.deleteObjectStore('energy-samples');
        Object.values(TIERS).forEach(name => {
          if (!database.objectStoreNames.contains(name)) database.createObjectStore(name, {keyPath: ['series', 'ts']});
        });
        if (!database.objectStoreNames.contains(META_STORE)) database.createObjectStore(META_STORE, {keyPath: 'key'});
      };
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error || new Error('IndexedDB konnte nicht geöffnet werden'));
    });
    return databasePromise;
  };

  const storeNameFor = tier => {
    const name = TIERS[tier];
    if (!name) throw new Error(`Unbekannte Verdichtungsstufe: ${tier}`);
    return name;
  };

  const rangeFor = (series, fromTs, toTs) => series === null || series === undefined
    ? window.IDBKeyRange.bound([-Infinity, fromTs], [String.fromCharCode(0xffff), toTs])
    : window.IDBKeyRange.bound([series, fromTs], [series, toTs]);

  // Ein Bereich ueber alle Serien laesst sich mit einem zusammengesetzten
  // keyPath nicht sauber als eine Range ausdruecken - der Bereich
  // [[*, from], [*, to]] enthaelt auch Saetze anderer Serien mit
  // Zeitstempeln ausserhalb. Deshalb laeuft hier ein Cursor.
  //
  // Er fasst aber nicht jeden Satz an, sondern springt: der Schluessel ist
  // [series, ts], innerhalb einer Serie also nach Zeit sortiert. Vor dem
  // Fenster wird auf [series, fromTs] vorgespult, hinter dem Fenster direkt
  // auf die naechste Serie. Ohne diese Spruenge kostete jeder Aufruf einen
  // Durchlauf ueber den GESAMTEN Store - bei sieben Tagen Minutenmitteln
  // fuenfstellig viele Cursor-Schritte, und der Chart liest bei jedem
  // Aufzeichnungstakt neu (siehe announceUpdate() in history-recorder.js).
  const readAllFiltered = (database, tier, fromTs, toTs) => new Promise((resolve, reject) => {
    const rows = [];
    const request = database.transaction(storeNameFor(tier), 'readonly').objectStore(storeNameFor(tier)).openCursor();
    request.onsuccess = () => {
      const cursor = request.result;
      if (!cursor) {
        resolve(rows);
        return;
      }
      const {series, ts} = cursor.value;
      if (ts < fromTs) {
        cursor.continue([series, fromTs]);
        return;
      }
      if (ts > toTs) {
        // [series, Infinity] liegt hinter jedem Zeitstempel DIESER Serie und
        // vor dem ersten Schluessel der naechsten - Arrays werden Element
        // fuer Element verglichen, der Seriennamen bleibt also unangetastet.
        //
        // NICHT series + String.fromCharCode(0xffff) wie in rangeFor(): ist
        // ein Serienname Praefix eines anderen (entity:shelly1 neben
        // entity:shelly11), sortiert 0xffff HINTER dem laengeren Namen, und
        // der Sprung ueberspraenge die ganze Serie.
        cursor.continue([series, Infinity]);
        return;
      }
      rows.push(cursor.value);
      cursor.continue();
    };
    request.onerror = () => reject(request.error || new Error('Historie konnte nicht gelesen werden'));
  });

  const readRange = async (tier, series, fromTs, toTs) => {
    const database = await open();
    if (series === null || series === undefined) return readAllFiltered(database, tier, fromTs, toTs);
    return new Promise((resolve, reject) => {
      const request = database.transaction(storeNameFor(tier), 'readonly')
        .objectStore(storeNameFor(tier))
        .getAll(rangeFor(series, fromTs, toTs));
      request.onsuccess = () => resolve(request.result || []);
      request.onerror = () => reject(request.error || new Error('Historie konnte nicht gelesen werden'));
    });
  };

  const metaGet = (database, key) => new Promise((resolve, reject) => {
    const request = database.transaction(META_STORE, 'readonly').objectStore(META_STORE).get(key);
    request.onsuccess = () => resolve(request.result ? request.result.value : undefined);
    request.onerror = () => reject(request.error || new Error('Historie-Metadaten konnten nicht gelesen werden'));
  });

  const meta = async key => metaGet(await open(), key);

  const setMeta = async (key, value) => {
    const database = await open();
    return new Promise((resolve, reject) => {
      const transaction = database.transaction(META_STORE, 'readwrite');
      transaction.objectStore(META_STORE).put({key, value});
      transaction.oncomplete = () => resolve();
      transaction.onerror = () => reject(transaction.error || new Error('Historie-Metadaten konnten nicht geschrieben werden'));
    });
  };

  const bytesUsed = async () => Number(await meta('bytes')) || 0;

  // Schreibt Saetze und fuehrt das Byte-Konto in derselben Transaktion nach.
  // Nur wirklich neue Schluessel zaehlen: ein erneutes put() auf denselben
  // [series, ts] ersetzt den Satz, belegt aber keinen zusaetzlichen Platz.
  const write = async (tier, rows, bytesPerRow) => {
    if (!rows.length) return;
    const database = await open();
    const name = storeNameFor(tier);
    return new Promise((resolve, reject) => {
      const transaction = database.transaction([name, META_STORE], 'readwrite');
      const target = transaction.objectStore(name);
      const metaStore = transaction.objectStore(META_STORE);
      let added = 0;
      let settled = 0;
      rows.forEach(row => {
        const existing = target.getKey([row.series, row.ts]);
        existing.onsuccess = () => {
          if (existing.result === undefined) added += 1;
          target.put(row);
          settled += 1;
          if (settled === rows.length) {
            const current = metaStore.get('bytes');
            current.onsuccess = () => {
              const value = (current.result ? Number(current.result.value) : 0) + added * bytesPerRow;
              metaStore.put({key: 'bytes', value});
            };
          }
        };
      });
      transaction.oncomplete = () => resolve();
      transaction.onerror = () => reject(transaction.error || new Error('Historie konnte nicht geschrieben werden'));
      transaction.onabort = () => reject(transaction.error || new Error('Historie konnte nicht geschrieben werden'));
    });
  };

  const writeRaw = rows => write('raw', rows, ROW_BYTES_RAW);
  const writeRollup = (tier, rows) => write(tier, rows, ROW_BYTES_ROLLUP);

  const deleteRange = async (tier, series, fromTs, toTs) => {
    const database = await open();
    const name = storeNameFor(tier);
    const bytesPerRow = tier === 'raw' ? ROW_BYTES_RAW : ROW_BYTES_ROLLUP;
    return new Promise((resolve, reject) => {
      const transaction = database.transaction([name, META_STORE], 'readwrite');
      const target = transaction.objectStore(name);
      const metaStore = transaction.objectStore(META_STORE);
      let removed = 0;
      const cursorRequest = series === null || series === undefined
        ? target.openCursor()
        : target.openCursor(rangeFor(series, fromTs, toTs));
      cursorRequest.onsuccess = () => {
        const cursor = cursorRequest.result;
        if (cursor) {
          if (cursor.value.ts >= fromTs && cursor.value.ts <= toTs) {
            cursor.delete();
            removed += 1;
          }
          cursor.continue();
          return;
        }
        const current = metaStore.get('bytes');
        current.onsuccess = () => {
          const value = Math.max(0, (current.result ? Number(current.result.value) : 0) - removed * bytesPerRow);
          metaStore.put({key: 'bytes', value});
        };
      };
      transaction.oncomplete = () => resolve(removed);
      transaction.onerror = () => reject(transaction.error || new Error('Historie konnte nicht bereinigt werden'));
    });
  };

  // Loescht genau die genannten Schluessel. Der Verdichtungslauf weiss nach
  // dem Lesen exakt, welche Saetze er verbraucht hat - ein Bereichsloeschen
  // ueber alle Serien muesste stattdessen den gesamten Store durchlaufen.
  const deleteKeys = async (tier, keys) => {
    if (!keys.length) return 0;
    const database = await open();
    const name = storeNameFor(tier);
    const bytesPerRow = tier === 'raw' ? ROW_BYTES_RAW : ROW_BYTES_ROLLUP;
    return new Promise((resolve, reject) => {
      const transaction = database.transaction([name, META_STORE], 'readwrite');
      const target = transaction.objectStore(name);
      const metaStore = transaction.objectStore(META_STORE);
      let removed = 0;
      let settled = 0;
      keys.forEach(key => {
        const existing = target.getKey(key);
        existing.onsuccess = () => {
          if (existing.result !== undefined) {
            target.delete(key);
            removed += 1;
          }
          settled += 1;
          if (settled === keys.length) {
            const current = metaStore.get('bytes');
            current.onsuccess = () => {
              const value = Math.max(0, (current.result ? Number(current.result.value) : 0) - removed * bytesPerRow);
              metaStore.put({key: 'bytes', value});
            };
          }
        };
      });
      transaction.oncomplete = () => resolve(removed);
      transaction.onerror = () => reject(transaction.error || new Error('Historie konnte nicht bereinigt werden'));
    });
  };

  // Deckungsraster einer Stufe: je Serie die Anzahl vorhandener Saetze pro
  // Rasterbucket. openKeyCursor liest nur die Schluessel, nicht die Werte -
  // das ist der Grund, warum ein Angebot auch bei Wochen an Minutenwerten
  // guenstig bleibt.
  const coverage = async (tier, fromTs, toTs, stepMs) => {
    const database = await open();
    const name = storeNameFor(tier);
    const buckets = Math.max(0, Math.ceil((toTs - fromTs) / stepMs));
    return new Promise((resolve, reject) => {
      const result = {};
      const request = database.transaction(name, 'readonly').objectStore(name).openKeyCursor();
      request.onsuccess = () => {
        const cursor = request.result;
        if (!cursor) {
          resolve(result);
          return;
        }
        const series = String(cursor.key[0]);
        const ts = Number(cursor.key[1]);
        if (ts >= fromTs && ts < toTs) {
          let census = result[series];
          if (!census) {
            census = {from: fromTs, step: stepMs, n: new Array(buckets).fill(0)};
            result[series] = census;
          }
          census.n[Math.floor((ts - fromTs) / stepMs)] += 1;
        }
        cursor.continue();
      };
      request.onerror = () => reject(request.error || new Error('Deckung konnte nicht ermittelt werden'));
    });
  };

  // Schreibt NUR Saetze, deren Schluessel noch fehlt. Der Unterschied zu
  // write() ist die eine Zeile, auf der der ganze Austausch ruht: fremde
  // Daten duerfen das eigene Messgut nicht ersetzen. Dadurch ist ein
  // zweites Empfangen derselben Lieferung folgenlos.
  const writeMissing = async (tier, rows) => {
    if (!rows.length) return 0;
    const database = await open();
    const name = storeNameFor(tier);
    const bytesPerRow = tier === 'raw' ? ROW_BYTES_RAW : ROW_BYTES_ROLLUP;
    return new Promise((resolve, reject) => {
      const transaction = database.transaction([name, META_STORE], 'readwrite');
      const target = transaction.objectStore(name);
      const metaStore = transaction.objectStore(META_STORE);
      let added = 0;
      let settled = 0;
      rows.forEach(row => {
        const existing = target.getKey([row.series, row.ts]);
        existing.onsuccess = () => {
          if (existing.result === undefined) {
            target.put(row);
            added += 1;
          }
          settled += 1;
          if (settled === rows.length) {
            const current = metaStore.get('bytes');
            current.onsuccess = () => {
              const value = (current.result ? Number(current.result.value) : 0) + added * bytesPerRow;
              metaStore.put({key: 'bytes', value});
            };
          }
        };
      });
      transaction.oncomplete = () => resolve(added);
      transaction.onerror = () => reject(transaction.error || new Error('Historie konnte nicht ergaenzt werden'));
      transaction.onabort = () => reject(transaction.error || new Error('Historie konnte nicht ergaenzt werden'));
    });
  };

  const seriesNames = async () => {
    const database = await open();
    const names = [];
    const seen = new Set();
    for (const tier of Object.keys(TIERS)) {
      // eslint-disable-next-line no-await-in-loop
      await new Promise((resolve, reject) => {
        const request = database.transaction(storeNameFor(tier), 'readonly').objectStore(storeNameFor(tier)).openCursor();
        request.onsuccess = () => {
          const cursor = request.result;
          if (!cursor) {
            resolve();
            return;
          }
          const series = String(cursor.value.series);
          if (!seen.has(series)) {
            seen.add(series);
            names.push(series);
          }
          cursor.continue();
        };
        request.onerror = () => reject(request.error || new Error('Serien konnten nicht gelesen werden'));
      });
    }
    return names;
  };

  const bounds = async () => {
    const database = await open();
    let oldest = null;
    let newest = null;
    for (const tier of Object.keys(TIERS)) {
      // eslint-disable-next-line no-await-in-loop
      await new Promise((resolve, reject) => {
        const request = database.transaction(storeNameFor(tier), 'readonly').objectStore(storeNameFor(tier)).openCursor();
        request.onsuccess = () => {
          const cursor = request.result;
          if (!cursor) {
            resolve();
            return;
          }
          const ts = Number(cursor.value.ts);
          if (oldest === null || ts < oldest) oldest = ts;
          if (newest === null || ts > newest) newest = ts;
          cursor.continue();
        };
        request.onerror = () => reject(request.error || new Error('Zeitgrenzen konnten nicht gelesen werden'));
      });
    }
    return {oldest, newest};
  };

  const estimate = async () => {
    if (!navigator.storage || !navigator.storage.estimate) return null;
    try {
      const value = await navigator.storage.estimate();
      return {usage: Number(value.usage) || 0, quota: Number(value.quota) || 0};
    } catch (error) {
      return null;
    }
  };

  // Bittet den Browser, diese Datenbank nicht bei Speicherdruck zu
  // verwerfen. Ohne diese Bitte ist die Historie "best effort" und kann
  // ersatzlos verschwinden - auf iOS bereits nach wenigen Wochen ohne
  // Seitenaufruf. Rueckgabe null heisst: der Browser kennt die API nicht.
  const persist = async () => {
    if (!navigator.storage || !navigator.storage.persist) return null;
    try {
      if (navigator.storage.persisted && await navigator.storage.persisted()) return true;
      return await navigator.storage.persist();
    } catch (error) {
      return null;
    }
  };

  window.HistoryStore = {
    ROW_BYTES_RAW, ROW_BYTES_ROLLUP, TIERS,
    open, writeRaw, writeRollup, readRange, deleteRange, deleteKeys, seriesNames, bounds,
    meta, setMeta, bytesUsed, estimate, persist, coverage, writeMissing,
  };
})();

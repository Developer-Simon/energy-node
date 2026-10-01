// Das Modell hinter dem Changelog-Bildschirm und der Zusammenfassung in der
// Vorschau: aus der Antwort von /api/changelog wird "was ist seit deiner
// Version neu". Reine Funktionen ohne DOM und ohne Shell, damit beide Stellen
// dieselbe Rechnung benutzen und sie sich ohne Browser pruefen laesst.
//
// Eingabe ist die Antwort selbst: { bundle_version, installed, document }, mit
// document.components[] = { id, label, kind, version, releases[] } und
// releases[].groups[].entries[] wie scripts/build/make_changelog_json.py sie
// schreibt.
(function () {
  'use strict';

  // Reihenfolge der Arten auf dem Bildschirm; eine unbekannte Art kommt zuletzt.
  var KIND_ORDER = ['app', 'shared', 'service', 'library', 'tool', 'integration'];

  function parts(version) {
    var match = /^v?(\d+)\.(\d+)\.(\d+)/.exec(String(version || ''));
    return match ? [Number(match[1]), Number(match[2]), Number(match[3])] : null;
  }

  // compare: -1, 0, 1 nach X.Y.Z. Eine Version, die keine ist ("dev"), gilt als
  // gleich - sie lässt sich nicht einordnen, und "gleich" schneidet nichts ab.
  function compare(a, b) {
    var pa = parts(a);
    var pb = parts(b);
    if (!pa || !pb) {
      return 0;
    }
    for (var i = 0; i < 3; i += 1) {
      if (pa[i] !== pb[i]) {
        return pa[i] < pb[i] ? -1 : 1;
      }
    }
    return 0;
  }

  function entryCount(release) {
    return (release.groups || []).reduce(function (sum, group) {
      return sum + (group.entries || []).length;
    }, 0);
  }

  function kindRank(kind) {
    var index = KIND_ORDER.indexOf(kind);
    return index < 0 ? KIND_ORDER.length : index;
  }

  // slice: je Komponente die Releases oberhalb der installierten Version bis
  // einschliesslich der Version des Pakets. Ohne installierte Version (erste
  // Installation, oder eine Komponente, die neu ins Paket kam) nur das
  // neueste Release - "alles" waere bei einer Erstinstallation ein Roman.
  // Komponenten ohne einen einzigen Eintrag fallen weg.
  function slice(view) {
    var installed = (view && view.installed) || {};
    var components = (view && view.document && view.document.components) || [];
    var rows = [];
    components.forEach(function (component, order) {
      var from = installed[component.id] && parts(installed[component.id]) ? installed[component.id] : null;
      var releases = (component.releases || []).filter(function (release) {
        return from === null || (compare(release.version, from) > 0 && compare(release.version, component.version) <= 0);
      });
      if (from === null) {
        releases = releases.slice(0, 1);
      }
      releases = releases.filter(function (release) {
        return entryCount(release) > 0;
      });
      if (!releases.length) {
        return;
      }
      rows.push({
        id: component.id, label: component.label, kind: component.kind,
        from: from, to: component.version, isNew: from === null, releases: releases, order: order,
      });
    });
    return rows.sort(function (a, b) {
      return kindRank(a.kind) - kindRank(b.kind) || a.order - b.order;
    });
  }

  function eachEntry(rows, visit) {
    rows.forEach(function (row) {
      row.releases.forEach(function (release) {
        (release.groups || []).forEach(function (group) {
          (group.entries || []).forEach(function (entry) {
            visit(entry, group, release, row);
          });
        });
      });
    });
  }

  // summarize: die Zaehlzeile "3 Neuerungen · 12 Korrektionen · 1 Breaking".
  // "breaking" zaehlt zusaetzlich zu feat/fix/other; total zaehlt jeden Eintrag
  // genau einmal.
  function summarize(rows) {
    var out = { feat: 0, fix: 0, other: 0, breaking: 0, total: 0, components: rows.length };
    eachEntry(rows, function (entry, group) {
      out.total += 1;
      if (entry.breaking) {
        out.breaking += 1;
      }
      if (group.type === 'feat') {
        out.feat += 1;
      } else if (group.type === 'fix') {
        out.fix += 1;
      } else {
        out.other += 1;
      }
    });
    return out;
  }

  // breaking: alle Breaking-Eintraege flach, damit der Bildschirm sie als
  // eigenen Block ueber die Liste stellen kann.
  function breaking(rows) {
    var out = [];
    eachEntry(rows, function (entry, group, release, row) {
      if (entry.breaking) {
        out.push({ component: row.label, version: release.version, text: entry.text, scope: entry.scope || '', pr: entry.pr || null });
      }
    });
    return out;
  }

  function scopes(rows) {
    var seen = {};
    eachEntry(rows, function (entry) {
      if (entry.scope) {
        seen[entry.scope] = true;
      }
    });
    return Object.keys(seen).sort();
  }

  // kinds: die Arten, die in rows vorkommen, in Bildschirmreihenfolge.
  function kinds(rows) {
    var seen = {};
    rows.forEach(function (row) {
      seen[row.kind] = true;
    });
    return Object.keys(seen).sort(function (a, b) {
      return kindRank(a) - kindRank(b);
    });
  }

  // filter: Art, Scope und Suchtext ("query" trifft Text und Scope, ohne
  // Gross-/Kleinschreibung). Ergebnis sind Kopien; leere Gruppen, Releases und
  // Komponenten fallen weg.
  function filter(rows, options) {
    var wanted = options || {};
    var query = String(wanted.query || '').trim().toLowerCase();
    var out = [];
    rows.forEach(function (row) {
      if (wanted.kind && row.kind !== wanted.kind) {
        return;
      }
      var releases = [];
      row.releases.forEach(function (release) {
        var groups = [];
        (release.groups || []).forEach(function (group) {
          var entries = (group.entries || []).filter(function (entry) {
            if (wanted.scope && entry.scope !== wanted.scope) {
              return false;
            }
            return !query || (entry.text + ' ' + (entry.scope || '')).toLowerCase().indexOf(query) >= 0;
          });
          if (entries.length) {
            groups.push(Object.assign({}, group, { entries: entries }));
          }
        });
        if (groups.length) {
          releases.push(Object.assign({}, release, { groups: groups }));
        }
      });
      if (releases.length) {
        out.push(Object.assign({}, row, { releases: releases }));
      }
    });
    return out;
  }

  window.ChangelogModel = {
    KIND_ORDER: KIND_ORDER,
    compare: compare,
    slice: slice,
    summarize: summarize,
    breaking: breaking,
    scopes: scopes,
    kinds: kinds,
    filter: filter,
  };
})();

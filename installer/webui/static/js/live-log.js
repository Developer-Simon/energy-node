// Live-Log der Diagnose: Ringpuffer je Unit, Schwaerzung fuer den Export,
// SSE-Klient fuer /api/unit-logs und die gemerkte Zeilenstufe. Das Wiederverbinden
// macht EventSource selbst; die id jeder Zeile ist der Journal-Cursor, der Server
// setzt damit nach einem Abbruch an der richtigen Stelle fort.
(function () {
  'use strict';

  var UNITS = ['energy-node-dashboard.service', 'energy-node-updater.service'];
  var STAGES = [0, 100, 500, 2000];
  var DEFAULT_STAGE = 100;
  var CAPACITY = 2000;
  var ISSUE_URL = 'https://github.com/Developer-Simon/energy-node/issues/new';
  // GitHub erlaubt 65 536 Zeichen, der Rest bleibt fuer den eigenen Text.
  var MARKDOWN_BUDGET = 60000;
  var STAGE_KEY = 'energy-node.diagnose.log-lines';
  var CLOSED = 2; // EventSource.CLOSED

  var IP_LITERAL = /^(?:\d{1,3}\.){3}\d{1,3}$/;

  function pad(n, width) {
    var text = String(n);
    while (text.length < width) {
      text = '0' + text;
    }
    return text;
  }

  function escapeRegExp(text) {
    return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  }

  function buffer() {
    var rings = {};
    var totals = {};
    UNITS.forEach(function (unit) {
      rings[unit] = [];
      totals[unit] = 0;
    });
    return {
      push: function (line) {
        var ring = rings[line.unit];
        if (!ring) {
          return;
        }
        ring.push(line);
        totals[line.unit] += 1;
        if (ring.length > CAPACITY) {
          ring.shift();
        }
      },
      lines: function (unit) {
        return (rings[unit] || []).slice();
      },
      // total zaehlt alle Zeilen seit dem Oeffnen, auch die aus dem Ringpuffer gefallenen.
      total: function (unit) {
        return totals[unit] || 0;
      },
    };
  }

  function formatLine(line) {
    var d = new Date(line.at);
    var time = pad(d.getHours(), 2) + ':' + pad(d.getMinutes(), 2) + ':' +
      pad(d.getSeconds(), 2) + '.' + pad(d.getMilliseconds(), 3);
    var prefix = '';
    if (line.ident) {
      prefix = line.ident + (line.pid ? '[' + line.pid + ']' : '') + ': ';
    }
    return time + ' ' + prefix + line.msg;
  }

  // scrubber ersetzt Geheimnisse, Adressen und Namen. Dieselbe Adresse bekommt
  // dieselbe Nummer, gezaehlt wird je Art ab 1 in der Reihenfolge des Auftretens.
  function scrubber(known) {
    known = known || {};
    var secrets = (known.secrets || []).filter(function (secret) {
      return secret && secret.length >= 3;
    });
    var seen = { mac: new Map(), ipv6: new Map(), ip: new Map(), tailnet: new Map() };

    function numbered(kind, key) {
      var table = seen[kind];
      if (!table.has(key)) {
        table.set(key, table.size + 1);
      }
      return '<' + kind + '-' + table.get(key) + '>';
    }

    function names() {
      var hostname = known.hostname || '';
      var host = known.host || '';
      var user = known.user || '';
      // Ein IP-Literal als Host ist schon als Adresse maskiert, sonst wuerde es hier doppelt zaehlen.
      return [
        [hostname, '<hostname>'],
        [IP_LITERAL.test(host) || host.indexOf(':') !== -1 ? '' : host, '<host>'],
        [user, '<user>'],
      ];
    }

    return {
      text: function (value) {
        var out = String(value);
        secrets.forEach(function (secret) {
          out = out.split(secret).join('***');
        });
        out = out.replace(/\b[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}\b/g, function (mac) {
          return numbered('mac', mac.toLowerCase());
        });
        // Ein Token aus Hex-Ziffern und Doppelpunkten gilt als IPv6, wenn es "::" enthaelt
        // oder mindestens fuenf Gruppen hat. ::1 und :: bleiben, Uhrzeiten haben nur drei Gruppen.
        out = out.replace(/(?<![0-9A-Za-z_:])[0-9A-Fa-f:]+(?![0-9A-Za-z_:])/g, function (token) {
          if (token === '::1' || token === '::') {
            return token;
          }
          if (token.indexOf('::') === -1 && token.split(':').length < 5) {
            return token;
          }
          return numbered('ipv6', token.toLowerCase());
        });
        out = out.replace(/\b(?:\d{1,3}\.){3}\d{1,3}\b/g, function (ip) {
          var octets = ip.split('.').map(Number);
          if (octets.some(function (octet) { return octet > 255; })) {
            return ip;
          }
          if (octets[0] === 127 || ip === '0.0.0.0') {
            return ip;
          }
          return numbered('ip', ip);
        });
        out = out.replace(/[a-z0-9-]+(\.[a-z0-9-]+)*\.ts\.net/gi, function (name) {
          return numbered('tailnet', name.toLowerCase());
        });
        names().forEach(function (pair) {
          var name = pair[0];
          if (!name || name.length < 2) {
            return;
          }
          var pattern = new RegExp('(?<![A-Za-z0-9_-])' + escapeRegExp(name) + '(?![A-Za-z0-9_-])', 'g');
          out = out.replace(pattern, pair[1]);
        });
        return out;
      },
    };
  }

  // open verbindet mit /api/unit-logs. options.EventSource ersetzt die Klasse in Tests.
  function open(options) {
    var Source = options.EventSource || window.EventSource;
    var source = new Source(window.Api.url('/api/unit-logs'));

    function notify(name, value) {
      if (options[name]) {
        options[name](value);
      }
    }

    function state(name) {
      notify('onState', name);
    }

    function on(type, handle) {
      source.addEventListener(type, function (event) {
        var data;
        try {
          data = JSON.parse(event.data);
        } catch (err) {
          return;
        }
        handle(data);
      });
    }

    on('hello', function (data) {
      notify('onHello', data);
      state('live');
    });
    on('line', function (data) {
      notify('onLine', data);
    });
    on('status', function (data) {
      notify('onStatus', data);
    });
    on('end', function (data) {
      source.close();
      notify('onEnd', data);
      state('ended');
    });
    source.onerror = function () {
      // Der Browser verbindet selbst neu. Gibt er auf, kommt nichts mehr.
      if (source.readyState !== CLOSED) {
        state('reconnecting');
      } else {
        state('ended');
      }
    };

    return {
      close: function () {
        source.close();
      },
    };
  }

  function loadStage() {
    try {
      var raw = window.localStorage.getItem(STAGE_KEY);
      if (raw !== null) {
        var stage = Number(raw);
        if (STAGES.indexOf(stage) !== -1) {
          return stage;
        }
      }
    } catch (err) {
      // Ohne Speicher gilt die Vorgabe.
    }
    return DEFAULT_STAGE;
  }

  function saveStage(stage) {
    if (STAGES.indexOf(stage) === -1) {
      return;
    }
    try {
      window.localStorage.setItem(STAGE_KEY, String(stage));
    } catch (err) {
      // Ohne Speicher gilt bei der naechsten Diagnose wieder die Vorgabe.
    }
  }

  window.LiveLog = {
    UNITS: UNITS,
    STAGES: STAGES,
    DEFAULT_STAGE: DEFAULT_STAGE,
    CAPACITY: CAPACITY,
    ISSUE_URL: ISSUE_URL,
    MARKDOWN_BUDGET: MARKDOWN_BUDGET,
    buffer: buffer,
    open: open,
    scrubber: scrubber,
    formatLine: formatLine,
    loadStage: loadStage,
    saveStage: saveStage,
  };
})();

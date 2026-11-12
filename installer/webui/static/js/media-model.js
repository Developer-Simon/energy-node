// Reine Hilfen des Mediums: die Namensregeln des Go-Codes (internal/media),
// die Gruppen der Geraeteliste, die Vorgaben des Formulars und das, was sich
// der Rechner merkt. Ohne DOM und ohne fetch, darum einzeln pruefbar.
(function () {
  'use strict';

  // Dieselben Muster wie hostnamePattern und userPattern in internal/media.
  var HOSTNAME = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;
  var USER = /^[a-z_][a-z0-9_-]{0,31}$/;
  var COUNTRY = /^[A-Z]{2}$/;
  // Bandzusatz am Ende eines WLAN-Namens: "5 GHz", "5G", "_5G", "2.4GHz", "2,4 G".
  var BAND_SUFFIX = /[\s_.-]*(5|2[.,]?4)\s*g(hz)?$/i;

  // bandless: der Name ohne Bandzusatz, klein geschrieben, zum Vergleichen.
  function bandless(name) {
    return String(name || '').replace(BAND_SUFFIX, '').trim().toLowerCase();
  }

  // signalLevel: drei Balken wie bei WLAN-Anzeigen, aus 0 bis 100.
  function signalLevel(signal) {
    if (signal >= 67) {
      return 3;
    }
    if (signal >= 34) {
      return 2;
    }
    return 1;
  }

  function validHostname(name) {
    return typeof name === 'string' && HOSTNAME.test(name);
  }

  function validUser(name) {
    return typeof name === 'string' && name !== 'root' && USER.test(name);
  }

  window.MediaModel = {
    validHostname: validHostname,
    validUser: validUser,

    // groups: die Geraete nach ihrer Gruppe, in der Reihenfolge, in der die
    // Gruppen zum ersten Mal vorkommen.
    groups: function (devices) {
      var order = [];
      var byId = Object.create(null);
      (devices || []).forEach(function (device) {
        if (!byId[device.group]) {
          byId[device.group] = { id: device.group, devices: [] };
          order.push(byId[device.group]);
        }
        byId[device.group].devices.push(device);
      });
      return order;
    },

    // countryFor: das WLAN-Land zur Zeitzone, wie der Imager es aus der
    // Hauptstadt ableitet. Ohne Treffer das Land aus der Sprache des Rechners.
    countryFor: function (timezone, defaults) {
      var d = defaults || {};
      var zones = d.zone_countries || {};
      return (Object.prototype.hasOwnProperty.call(zones, timezone) && zones[timezone]) || d.wifi_country || '';
    },

    // fiveGhzOnly: das gewaehlte Netz ist nur auf 5 GHz zu sehen und das
    // Geraet ist kein Pi 4 oder 5. Pi 3B und Zero W erreichen nur 2,4 GHz.
    // Steht das Netz nicht in der Liste, zaehlt die Frequenz des erkannten.
    fiveGhzOnly: function (device, ssid, defaults) {
      var d = defaults || {};
      if (!ssid || device === 'pi4' || device === 'pi5') {
        return false;
      }
      var net = (d.wifi_networks || []).find(function (n) {
        return n.ssid === ssid;
      });
      if (net) {
        return !!net.band_5 && !net.band_24;
      }
      return ssid === d.wifi_ssid && d.wifi_freq_mhz >= 5000;
    },

    signalLevel: signalLevel,

    // networkRows: die Netze in Reichweite als Zeilen der Auswahl, hoechstens
    // sechs, das gewaehlte immer dabei. only5: nur auf 5 GHz zu sehen.
    networkRows: function (defaults, ssid) {
      var nets = (defaults || {}).wifi_networks || [];
      var rows = nets.slice(0, 6);
      var listed = rows.some(function (n) {
        return n.ssid === ssid;
      });
      if (ssid && !listed) {
        var chosen = nets.find(function (n) {
          return n.ssid === ssid;
        });
        if (chosen) {
          rows = rows.slice(0, 5).concat([chosen]);
        }
      }
      return rows.map(function (n) {
        return { ssid: n.ssid, only5: !!n.band_5 && !n.band_24, level: signalLevel(n.signal) };
      });
    },

    // mergeNetworks: ein neuer Suchlauf ersetzt die Werte, nicht die
    // Reihenfolge. Bekannte Netze bleiben an ihrem Platz, neue kommen dahinter,
    // verschwundene fallen weg. So springt keine Zeile unter dem Zeiger.
    mergeNetworks: function (old, fresh) {
      var byName = Object.create(null);
      (fresh || []).forEach(function (n) {
        byName[n.ssid] = n;
      });
      var known = Object.create(null);
      var kept = (old || [])
        .filter(function (n) {
          return byName[n.ssid];
        })
        .map(function (n) {
          known[n.ssid] = true;
          return byName[n.ssid];
        });
      return kept.concat((fresh || []).filter(function (n) {
        return !known[n.ssid];
      }));
    },

    // siblingFor: dasselbe Netz auf 2,4 GHz. Viele Router senden zwei Namen,
    // etwa "Haus 5 GHz" und "Haus". Gesucht wird ein anderes Netz mit 2,4 GHz,
    // dessen Name ohne Bandzusatz gleich ist. '' ohne Treffer.
    siblingFor: function (ssid, defaults) {
      var base = bandless(ssid);
      if (!ssid || !base) {
        return '';
      }
      var hit = ((defaults || {}).wifi_networks || []).find(function (n) {
        return n.ssid !== ssid && n.band_24 && bandless(n.ssid) === base;
      });
      return hit ? hit.ssid : '';
    },

    // defaultSettings: die Vorgaben des Rechners und des Installers. Die
    // Passwoerter beginnen leer, der Notfall-Zugang und das WLAN sind aus. Der
    // erkannte WLAN-Name steht vor, das Passwort nie. group ist fuer
    // gruppenspezifische Vorgaben reserviert und hat heute keine Wirkung.
    defaultSettings: function (defaults, group) {
      var d = defaults || {};
      return {
        hostname: d.hostname || '',
        user: d.user || '',
        emergency: false,
        password: '',
        password2: '',
        timezone: d.timezone || '',
        keyboard: d.keyboard || '',
        wifiOn: false,
        ssid: d.wifi_ssid || '',
        wifiPassword: '',
        country: window.MediaModel.countryFor(d.timezone || '', d),
      };
    },

    // settingsComplete: ob das Formular abgeschickt werden kann. Das Passwort
    // zaehlt nur, wenn der Notfall-Zugang an ist, das WLAN nur, wenn es an ist.
    settingsComplete: function (s) {
      if (!validHostname(s.hostname) || !validUser(s.user)) {
        return false;
      }
      if (s.emergency && !(s.password.length >= 8 && s.password === s.password2)) {
        return false;
      }
      if (s.wifiOn && (!s.ssid || !COUNTRY.test(s.country))) {
        return false;
      }
      return true;
    },

    // toRequest: der Rumpf von POST /api/media/write. Ohne Schalter geht kein
    // Passwort und kein WLAN mit.
    toRequest: function (s) {
      return {
        hostname: s.hostname,
        user: s.user,
        password: s.emergency ? s.password : '',
        timezone: s.timezone,
        keyboard: s.keyboard,
        wifi: s.wifiOn ? { ssid: s.ssid, password: s.wifiPassword, country: s.country } : null,
      };
    },

    // confirmationFor: was POST /api/media/write als confirm schickt. Ein
    // Wechselmedium bestaetigt mit "yes", ein internes Laufwerk mit seinem Namen.
    confirmationFor: function (drive) {
      return drive.removable ? 'yes' : drive.model;
    },

    // confirmed: ob das Loeschen freigegeben ist. Wechselmedium: das Haekchen.
    // Internes Laufwerk: der abgetippte Name, ohne Modellnamen nie.
    confirmed: function (drive, checkbox, typed) {
      if (!drive) {
        return false;
      }
      if (drive.removable) {
        return checkbox === true;
      }
      return !!drive.model && String(typed || '').trim() === drive.model;
    },

    // stepRows: die Schritte des Auftrags mit ihren Textschluesseln.
    stepRows: function (job) {
      return ((job && job.steps) || []).map(function (step) {
        return { id: step.id, state: step.state, labelKey: 'media.step.' + step.id };
      });
    },

    // remaining: Sekunden bis zum Ende, null ohne Rate.
    remaining: function (job) {
      if (!job || !(job.rate > 0)) {
        return null;
      }
      return Math.max(0, Math.round((job.bytes_total - job.bytes_done) / job.rate));
    },

    // remembered: was der Rechner fuer das naechste Medium behaelt. Keine
    // Passwoerter und kein WLAN-Land, das folgt der Zeitzone.
    remembered: function (s) {
      return {
        hostname: s.hostname,
        user: s.user,
        timezone: s.timezone,
        keyboard: s.keyboard,
        wifiOn: s.wifiOn,
        ssid: s.ssid,
      };
    },
  };
})();

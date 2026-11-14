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

  // regionName: der Name eines Landes in der Sprache der Oberflaeche, '' ohne
  // Treffer oder ohne Intl.DisplayNames.
  function regionName(code, lang) {
    if (!code) {
      return '';
    }
    try {
      return new Intl.DisplayNames([lang || 'de'], { type: 'region' }).of(String(code).toUpperCase()) || '';
    } catch (err) {
      return '';
    }
  }

  // fold: klein und ohne Akzente, zum Suchen ("Zürich" findet "zurich").
  function fold(text) {
    return String(text || '').normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase();
  }

  // zoneRow: Gruppe (erster Teil, klein), Stadt (letzter Teil ohne
  // Unterstriche) und Land einer Zeitzone. UTC und Co. landen in "other".
  function zoneRow(zone, countries, lang) {
    var parts = String(zone).split('/');
    return {
      zone: zone,
      group: parts.length > 1 ? parts[0].toLowerCase() : 'other',
      city: parts[parts.length - 1].replace(/_/g, ' '),
      country: regionName((countries || {})[zone], lang),
    };
  }

  // Die Gruppen in fester Reihenfolge. Unbekannte Gruppen kommen dahinter.
  var GROUP_ORDER = ['pi', 'debian'];

  // Ein Auftrag ist fuer Debian ohne Pi-Schritte, die Wartezeit ist dort laenger.
  // Die Einheit ist Millisekunden wie SLOW_AFTER in screen-media.js.
  var WAIT_PI = 10 * 60 * 1000;
  var WAIT_DEBIAN = 30 * 60 * 1000;

  // installLangFor: die Sprache der Installation. Nur x86 64-bit mit deutscher
  // Oberflaeche bekommt Deutsch, ARM-Geraete zeigen immer Englisch.
  function installLangFor(device, uiLang) {
    return device === 'deb-amd64' && (uiLang || 'de') === 'de' ? 'de' : 'en';
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

    // groupFor: die Gruppe eines Geraets aus seiner ID. Debian-Geraete heissen
    // deb-..., alles andere ist ein Pi. Der Katalog ergaenzt nur die Vorgaben,
    // damit ein fehlgeschlagener Abruf keinen Debian-Lauf als Pi schickt.
    groupFor: function (deviceId) {
      return String(deviceId || '').indexOf('deb-') === 0 ? 'debian' : 'pi';
    },

    // groups: die Geraete nach ihrer Gruppe, pi vor debian, unbekannte Gruppen
    // danach in der Reihenfolge des ersten Vorkommens. Innerhalb einer Gruppe
    // bleibt die Reihenfolge, die der Server schickt.
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
      var known = GROUP_ORDER.filter(function (id) {
        return byId[id];
      }).map(function (id) {
        return byId[id];
      });
      var rest = order.filter(function (group) {
        return GROUP_ORDER.indexOf(group.id) < 0;
      });
      return known.concat(rest);
    },

    // installLangChoice: die Auswahl der Installationssprache. Nur x86 64-bit
    // mit deutscher Oberflaeche hat eine Wahl, sonst gibt es keine Liste.
    installLangChoice: function (device, uiLang) {
      if (device !== 'deb-amd64' || uiLang === 'en') {
        return [];
      }
      return [uiLang || 'de', 'en'];
    },

    // installLangFor: die Sprache, mit der die Installation laeuft.
    installLangFor: installLangFor,

    // waitLimit: wie lange der Rechner hoechstens auf sich warten lassen darf,
    // in Millisekunden. Debian braucht laenger als ein Pi.
    waitLimit: function (group) {
      return group === 'debian' ? WAIT_DEBIAN : WAIT_PI;
    },

    // countryFor: das WLAN-Land zur Zeitzone, wie der Imager es aus der
    // Hauptstadt ableitet. Ohne Treffer das Land aus der Sprache des Rechners.
    countryFor: function (timezone, defaults) {
      var d = defaults || {};
      var zones = d.zone_countries || {};
      return (Object.prototype.hasOwnProperty.call(zones, timezone) && zones[timezone]) || d.wifi_country || '';
    },

    // zoneGroups: die Zeitzonen gefiltert nach query (Stadt, Land oder
    // Zonenname, ohne Akzente), nach Kontinent gruppiert in der Reihenfolge
    // der Liste, innerhalb einer Gruppe nach Stadt sortiert.
    zoneGroups: function (zones, defaults, lang, query) {
      var countries = (defaults || {}).zone_countries || {};
      var q = fold(query).trim();
      var order = [];
      var byId = Object.create(null);
      (zones || []).forEach(function (zone) {
        var row = zoneRow(zone, countries, lang);
        if (q && fold(row.city + ' ' + row.country + ' ' + row.zone).indexOf(q) < 0) {
          return;
        }
        if (!byId[row.group]) {
          byId[row.group] = { id: row.group, rows: [] };
          order.push(byId[row.group]);
        }
        byId[row.group].rows.push(row);
      });
      order.forEach(function (group) {
        group.rows.sort(function (a, b) {
          return a.city.localeCompare(b.city);
        });
      });
      return order;
    },

    // zoneSummary: "Berlin, Deutschland" fuer die Zeile der Region.
    zoneSummary: function (zone, defaults, lang) {
      if (!zone) {
        return '';
      }
      var row = zoneRow(zone, (defaults || {}).zone_countries, lang);
      return row.country ? row.city + ', ' + row.country : row.city;
    },

    // keyboardLabel: "Deutschland (de)". Die Belegungen heissen wie Laender.
    keyboardLabel: function (code, lang) {
      var name = regionName(code, lang);
      return name ? name + ' (' + code + ')' : String(code || '');
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
    // erkannte WLAN-Name steht vor, das Passwort nie. Bei Debian sind Notfall-
    // Zugang und Rueckfrage an, dazu kommen die Sprachen. device und uiLang
    // gehoeren zur Debian-Vorgabe, Pi-Aufrufe mit group allein bleiben gleich.
    defaultSettings: function (defaults, group, device, uiLang) {
      var d = defaults || {};
      var s = {
        hostname: d.hostname || '',
        user: d.user || '',
        emergency: false,
        password: '',
        password2: '',
        sshPassword: false,
        timezone: d.timezone || '',
        keyboard: d.keyboard || '',
        wifiOn: false,
        ssid: d.wifi_ssid || '',
        wifiPassword: '',
        country: window.MediaModel.countryFor(d.timezone || '', d),
      };
      if (group === 'debian') {
        var lang = uiLang || 'de';
        s.emergency = true;
        s.askBeforeErase = true;
        s.installLang = installLangFor(device, lang);
        s.systemLang = lang;
      }
      return s;
    },

    // settingsComplete: ob das Formular abgeschickt werden kann. Das Passwort
    // zaehlt nur, wenn der Notfall-Zugang an ist. Das WLAN zaehlt bei Pi nur,
    // wenn es an ist, bei Debian nie.
    settingsComplete: function (s, group) {
      if (!validHostname(s.hostname) || !validUser(s.user)) {
        return false;
      }
      if (s.emergency && !(s.password.length >= 8 && s.password === s.password2)) {
        return false;
      }
      if (group !== 'debian' && s.wifiOn && (!s.ssid || !COUNTRY.test(s.country))) {
        return false;
      }
      return true;
    },

    // toRequest: der Rumpf von POST /api/media/write. Ohne Schalter geht kein
    // Passwort und kein WLAN mit. Debian schickt Rueckfrage und Sprachen mit
    // und nie WLAN, Pi schickt sie nicht.
    toRequest: function (s, group) {
      var req = {
        hostname: s.hostname,
        user: s.user,
        password: s.emergency ? s.password : '',
        ssh_password: s.emergency && s.sshPassword,
        timezone: s.timezone,
        keyboard: s.keyboard,
        wifi: s.wifiOn ? { ssid: s.ssid, password: s.wifiPassword, country: s.country } : null,
      };
      if (group === 'debian') {
        req.wifi = null;
        req.ask_before_erase = !!s.askBeforeErase;
        req.install_lang = s.installLang;
        req.system_lang = s.systemLang;
      }
      return req;
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

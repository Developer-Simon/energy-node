// Uebersetzung der Oberflaeche. Der Ereignisstrom aus Schicht 2 ist
// sprachneutral - jeder Text, den ein Betreiber liest, entsteht hier.
(function () {
  'use strict';

  var STORAGE_KEY = 'energy-node-installer.lang';

  function fill(text, params) {
    if (!params) {
      return text;
    }
    return text.replace(/\{(\w+)\}/g, function (match, name) {
      return Object.prototype.hasOwnProperty.call(params, name) ? String(params[name]) : match;
    });
  }

  var I18n = {
    lang: 'en',
    catalog: {},

    // preferred: die bewusste Wahl des Betreibers schlaegt die aus der
    // OS-Locale geratene Vorauswahl des Servers.
    preferred: function (serverLanguage) {
      var stored = null;
      try {
        stored = window.localStorage.getItem(STORAGE_KEY);
      } catch (err) {
        stored = null; // gesperrter Speicher ist kein Grund zu scheitern
      }
      return stored || serverLanguage || 'en';
    },

    load: function (basePath, token, lang) {
      var url = (basePath || '') + '/api/catalog/' + encodeURIComponent(lang);
      if (token) {
        url += '?token=' + encodeURIComponent(token);
      }
      var self = this;
      return window.fetch(url).then(function (response) {
        if (!response.ok) {
          throw new Error('catalog ' + lang + ': HTTP ' + response.status);
        }
        return response.json();
      }).then(function (catalog) {
        self.catalog = catalog;
        self.lang = lang;
        try {
          window.localStorage.setItem(STORAGE_KEY, lang);
        } catch (err) {
          // dann merkt sich der Rechner die Sprache eben nicht
        }
        window.document.documentElement.setAttribute('lang', lang);
        return catalog;
      });
    },

    // t: ein fehlender Schluessel wird als Schluessel gezeigt - eine sichtbare
    // Luecke ist ein Fehlerbericht, eine unsichtbare ein Raetsel.
    t: function (key, params) {
      var text = this.catalog[key];
      if (text === undefined || text === null || text === '') {
        return key;
      }
      return fill(text, params);
    },

    // tn: genau zwei Formen, ".one" bei 1 und ".other" sonst; {n} ist gesetzt.
    tn: function (key, n, params) {
      var merged = { n: n };
      if (params) {
        Object.keys(params).forEach(function (name) { merged[name] = params[name]; });
      }
      return this.t(key + (n === 1 ? '.one' : '.other'), merged);
    },

    // number: "Sieben" statt "7", wo der Katalog ein Zahlwort hat.
    number: function (n) {
      var word = this.catalog['number.' + n];
      return word ? word : String(n);
    },

    apply: function (root) {
      var scope = root || window.document.body;
      var self = this;
      [['data-i18n', null], ['data-i18n-placeholder', 'placeholder'], ['data-i18n-title', 'title'], ['data-i18n-aria-label', 'aria-label']]
        .forEach(function (pair) {
          var nodes = scope.querySelectorAll('[' + pair[0] + ']');
          for (var i = 0; i < nodes.length; i++) {
            var text = self.t(nodes[i].getAttribute(pair[0]));
            if (pair[1]) {
              nodes[i].setAttribute(pair[1], text);
            } else {
              nodes[i].textContent = text;
            }
          }
        });
    },
  };

  window.I18n = I18n;
})();

// Zahlen, Zeiten und Versionen so, wie die Vorlagen sie schreiben. Die
// Locale ist die der Oberflaeche, nicht die des Rechners.
(function () {
  'use strict';

  function pad(n) {
    return (n < 10 ? '0' : '') + n;
  }

  function number(value, lang, digits) {
    return new Intl.NumberFormat(lang === 'de' ? 'de-DE' : 'en-GB', {
      minimumFractionDigits: digits,
      maximumFractionDigits: digits,
    }).format(value);
  }

  window.Format = {
    plainVersion: function (version) {
      return version ? String(version).replace(/^v/, '') : '';
    },

    clock: function (at) {
      var d = new Date(at);
      return pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
    },

    duration: function (ms) {
      var total = Math.max(0, Math.round(ms / 1000));
      return Math.floor(total / 60) + ':' + pad(total % 60);
    },

    elapsed: function (ms) {
      var total = Math.max(0, Math.floor(ms / 1000));
      return pad(Math.floor(total / 60)) + ':' + pad(total % 60);
    },

    megabytes: function (bytes, lang) {
      return number(Math.round(bytes / (1024 * 1024)), lang, 0) + ' MB';
    },

    gigabytes: function (mb, lang) {
      return number(Math.round((mb / 1024) * 10) / 10, lang, 1) + ' GB';
    },

    bits: function (arch) {
      if (/^(armv[67]l|i[3-6]86)$/.test(arch || '')) {
        return 32;
      }
      if (/^(aarch64|arm64|x86_64|amd64)$/.test(arch || '')) {
        return 64;
      }
      return 0;
    },
  };
})();

// Zahlen, Zeiten und Versionen so, wie die Vorlagen sie schreiben. Die
// Locale ist die der Oberflaeche, nicht die des Rechners.
(function () {
  'use strict';

  function pad(n) {
    return (n < 10 ? '0' : '') + n;
  }

  var MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

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

    // day: der Tag eines Zeitstempels kurz, "04.10." bzw. "4 Oct". Leer bei
    // fehlendem oder unlesbarem Wert.
    day: function (at, lang) {
      var d = new Date(at);
      if (!at || isNaN(d.getTime())) {
        return '';
      }
      if (lang === 'de') {
        return pad(d.getDate()) + '.' + pad(d.getMonth() + 1) + '.';
      }
      return d.getDate() + ' ' + MONTHS[d.getMonth()];
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

    // verifyDetail words the facts the host reports for a rejected package
    // ("abi=cp313 expected=cp311", "missing=<file>", "tampered=<file>",
    // "files=empty", "more=<n>"), one per line, with the catalog of the window.
    // Any other line (an architecture fact, a free text) is shown as it came.
    verifyDetail: function (detail, t) {
      if (!detail) {
        return '';
      }
      return String(detail).split('\n').map(function (line) {
        var abi = /^abi=(\S+) expected=(\S+)$/.exec(line);
        if (abi) {
          return t('verify.abi', { device: abi[1], package: abi[2] });
        }
        var file = /^(missing|tampered)=(.+)$/.exec(line);
        if (file) {
          return t('verify.' + file[1], { file: file[2] });
        }
        var more = /^more=(\d+)$/.exec(line);
        if (more) {
          return t('verify.more', { count: more[1] });
        }
        return line === 'files=empty' ? t('verify.files_empty') : line;
      }).join('\n');
    },
  };
})();

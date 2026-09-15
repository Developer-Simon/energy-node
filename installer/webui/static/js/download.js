// Speichert Text als Datei - im --app-Fenster und im Dashboard derselbe Weg:
// ein Blob-Link mit download-Attribut, kein Endpunkt.
(function () {
  'use strict';

  function pad(n) {
    return (n < 10 ? '0' : '') + n;
  }

  window.Download = {
    stamp: function (date) {
      return '' + date.getFullYear() + pad(date.getMonth() + 1) + pad(date.getDate()) + '-' +
        pad(date.getHours()) + pad(date.getMinutes()) + pad(date.getSeconds());
    },

    text: function (filename, content) {
      var blob = new window.Blob([content], { type: 'text/plain;charset=utf-8' });
      var url = window.URL.createObjectURL(blob);
      var link = window.document.createElement('a');
      link.href = url;
      link.download = filename;
      link.style.display = 'none';
      window.document.body.appendChild(link);
      link.click();
      link.remove();
      window.setTimeout(function () { window.URL.revokeObjectURL(url); }, 0);
    },
  };
})();

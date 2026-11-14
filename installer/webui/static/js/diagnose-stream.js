// SSE-Klient fuer /api/diagnose/stream. Der Server schickt je Abschnitt ein
// progress-Ereignis und zum Schluss end. Die Quelle schliesst sich selbst nach dem
// letzten progress (leeres pending) und nach end, damit der Browser nicht neu
// verbindet. Bricht die Verbindung vorher ab, meldet onEnd den Code DIAGNOSE_STREAM_LOST.
(function () {
  'use strict';

  var STREAM_LOST = 'DIAGNOSE_STREAM_LOST';

  // open verbindet mit /api/diagnose/stream. options.EventSource ersetzt die Klasse in Tests.
  function open(options) {
    options = options || {};
    var Source = options.EventSource || window.EventSource;
    var source = new Source(window.Api.url('/api/diagnose/stream'));
    var finished = false;

    function notify(name, value) {
      if (options[name]) {
        options[name](value);
      }
    }

    function finish() {
      finished = true;
      source.close();
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

    on('progress', function (data) {
      if (finished) {
        return;
      }
      var last = !!data && Array.isArray(data.pending) && data.pending.length === 0;
      if (last) {
        finish();
      }
      notify('onProgress', data);
    });
    on('end', function (data) {
      if (finished) {
        return;
      }
      finish();
      notify('onEnd', data);
    });
    source.onerror = function () {
      // Keine Wiederverbindung: die Werte bis hierher bleiben stehen.
      if (finished) {
        return;
      }
      finish();
      notify('onEnd', { code: STREAM_LOST });
    };

    return {
      close: function () {
        finished = true;
        source.close();
      },
    };
  }

  window.DiagnoseStream = {
    open: open,
  };
})();

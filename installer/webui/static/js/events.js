// SSE-Klient mit Wiederaufnahme (Vertrag 2). EventSource verbindet sich zwar
// selbst neu, aber mit derselben URL - und ?since= hat auf dem Server Vorrang
// vor Last-Event-ID. Das Nachholen ab dem zuletzt gesehenen seq macht deshalb
// dieser Klient, und er verwirft, was er schon kennt.
(function () {
  'use strict';

  var TYPES = ['hello', 'run-started', 'step', 'log', 'run-finished', 'connection'];
  var FIRST_DELAY = 500;
  var MAX_DELAY = 5000;

  function open(options) {
    var lastSeq = options.since || 0;
    // busId ist der Bus, zu dem lastSeq gehoert. Meldet hello einen anderen,
    // ist der Wirt neu gestartet (Plan D: Schritt 60 ersetzt das Dashboard)
    // und zaehlt wieder ab 1 - ab dem alten seq kaeme nichts mehr an.
    var busId = '';
    var delay = FIRST_DELAY;
    var source = null;
    var timer = null;
    var stopped = false;
    var wait = options.setTimeout || window.setTimeout.bind(window);
    var clear = options.clearTimeout || window.clearTimeout.bind(window);

    function state(name) {
      if (options.onState) {
        options.onState(name);
      }
    }

    function connect() {
      source = new window.EventSource(window.Api.url('/api/events', { since: lastSeq }));
      source.onopen = function () {
        delay = FIRST_DELAY;
        state('open');
      };
      source.onerror = function () {
        source.close();
        if (stopped) {
          return;
        }
        state('reconnecting');
        timer = wait(function () {
          timer = null;
          if (!stopped) {
            connect();
          }
        }, delay);
        delay = Math.min(delay * 2, MAX_DELAY);
      };
      TYPES.forEach(function (type) {
        source.addEventListener(type, function (event) {
          var seq = Number(event.lastEventId) || 0;
          // hello traegt den Stand des Busses, nicht den eines Ereignisses.
          if (type !== 'hello') {
            if (seq <= lastSeq) {
              return;
            }
            lastSeq = seq;
          }
          var data;
          try {
            data = JSON.parse(event.data);
          } catch (err) {
            return;
          }
          if (type === 'hello' && data.bus) {
            if (busId && data.bus !== busId) {
              busId = data.bus;
              lastSeq = 0;
              source.close();
              if (options.onRestart) {
                options.onRestart(data);
              }
              connect();
              return;
            }
            busId = data.bus;
          }
          options.onEvent(type, data, seq);
        });
      });
    }

    connect();
    return {
      close: function () {
        stopped = true;
        if (timer) {
          clear(timer);
        }
        if (source) {
          source.close();
        }
      },
      get lastSeq() {
        return lastSeq;
      },
    };
  }

  // hello fragt nur den Stand ab: laeuft gerade ein Lauf? since liegt hinter
  // jedem denkbaren seq, also kommt kein Rueckstand mit.
  function hello(options) {
    options = options || {};
    var wait = options.setTimeout || window.setTimeout.bind(window);
    return new Promise(function (resolve) {
      var done = false;
      var source = null;
      function finish(value) {
        if (done) {
          return;
        }
        done = true;
        if (source) {
          source.close();
        }
        resolve(value);
      }
      try {
        source = new window.EventSource(window.Api.url('/api/events', { since: Number.MAX_SAFE_INTEGER }));
      } catch (err) {
        finish(null);
        return;
      }
      source.addEventListener('hello', function (event) {
        try {
          finish(JSON.parse(event.data));
        } catch (err) {
          finish(null);
        }
      });
      source.onerror = function () {
        finish(null);
      };
      wait(function () {
        finish(null);
      }, options.timeout || 5000);
    });
  }

  window.Events = { open: open, hello: hello };
})();

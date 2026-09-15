// Der eine HTTP-Klient der Oberflaeche. Jede Anfrage traegt das Token, jede
// Fehlerantwort wird zu einem ApiError mit sprachneutralem Code - damit gibt
// es genau eine Stelle, an der aus einem Status ein Fehler wird.
(function () {
  'use strict';

  function ApiError(code, detail, status) {
    this.name = 'ApiError';
    this.code = code || 'BACKEND_ERROR';
    this.detail = detail || '';
    this.status = status || 0;
    this.message = this.code + (this.detail ? ': ' + this.detail : '');
  }
  ApiError.prototype = Object.create(Error.prototype);
  ApiError.prototype.constructor = ApiError;

  var config = { basePath: '', token: '' };

  function handle(response) {
    return response.json().catch(function () {
      return {};
    }).then(function (payload) {
      if (!response.ok) {
        throw new ApiError(payload.error, payload.detail, response.status);
      }
      return payload;
    });
  }

  // unreachable: der Wirt antwortet gar nicht - etwa weil der Installer
  // beendet wurde, waehrend das Fenster noch offen ist.
  function unreachable(err) {
    throw new ApiError('NETWORK', err && err.message ? err.message : '', 0);
  }

  function send(method, path, body) {
    var init = { method: method, headers: { 'X-Installer-Token': config.token } };
    if (body !== undefined) {
      init.headers['Content-Type'] = 'application/json';
      init.body = JSON.stringify(body);
    }
    return window.fetch(config.basePath + path, init).then(handle, unreachable);
  }

  window.Api = {
    configure: function (options) {
      config.basePath = options.basePath || '';
      config.token = options.token || '';
    },

    // url baut eine URL samt Token als Abfrageparameter. Nur EventSource
    // braucht das - es kann keine Kopfzeilen setzen.
    url: function (path, params) {
      var query = [];
      if (config.token) {
        query.push('token=' + encodeURIComponent(config.token));
      }
      Object.keys(params || {}).forEach(function (key) {
        query.push(encodeURIComponent(key) + '=' + encodeURIComponent(params[key]));
      });
      return config.basePath + path + (query.length ? '?' + query.join('&') : '');
    },

    get: function (path) {
      return send('GET', path);
    },

    post: function (path, body) {
      return send('POST', path, body === undefined ? {} : body);
    },

    put: function (path, body) {
      return send('PUT', path, body === undefined ? {} : body);
    },
  };
  window.ApiError = ApiError;
})();

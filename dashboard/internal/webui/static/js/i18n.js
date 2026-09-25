// Runtime for the dashboard's UI texts. The catalog arrives before this file
// as window.__I18N__ (a blocking <script> served by /i18n/<lang>.js), so t()
// works synchronously from the first deferred script on. API and semantics
// match installer/webui/static/js/i18n.js: {name} placeholders, ".one" for
// n === 1 and ".other" otherwise, and a missing key is shown as the key so
// gaps stay visible.
(function () {
  'use strict';

  var COOKIE = 'lang';
  var boot = window.__I18N__ || {};

  function fill(text, params) {
    if (!params) {
      return text;
    }
    return text.replace(/\{(\w+)\}/g, function (match, name) {
      return Object.prototype.hasOwnProperty.call(params, name) ? String(params[name]) : match;
    });
  }

  var I18n = {
    lang: boot.lang || document.documentElement.getAttribute('lang') || 'de',
    catalog: boot.catalog || {},

    t: function (key, params) {
      var text = this.catalog[key];
      if (text === undefined || text === null || text === '') {
        return key;
      }
      return fill(text, params);
    },

    tn: function (key, n, params) {
      var merged = { n: n };
      if (params) {
        Object.keys(params).forEach(function (name) { merged[name] = params[name]; });
      }
      return this.t(key + (n === 1 ? '.one' : '.other'), merged);
    },

    // Matches the thumb's transition in base.css (.lang-pill-thumb).
    settleMs: 300,

    // Kept as a method so tests can replace it; jsdom cannot reload.
    reload: function () {
      window.location.reload();
    },

    // The cookie path mirrors the server's basepath.CookiePath: the proxy
    // prefix plus "/", so a dashboard under /node/ does not write the cookie
    // for the whole proxy host.
    setLanguage: function (lang) {
      var base = document.documentElement.getAttribute('data-base-path') || '';
      document.cookie = COOKIE + '=' + encodeURIComponent(lang) + '; Path=' + base + '/; Max-Age=31536000; SameSite=Lax';
      this.reload();
    },
  };
  window.I18n = I18n;

  // One delegated handler serves the switcher on every page, including the
  // login page, which has no Alpine. The pill's thumb starts moving the
  // moment the radio changes; the reload waits until it has settled, so the
  // choice is visibly confirmed before the page is replaced. With reduced
  // motion there is no slide to wait for.
  document.addEventListener('change', function (event) {
    var target = event.target;
    if (target && target.matches && target.matches('[data-lang-select]')) {
      var lang = target.value;
      var reduced = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      window.setTimeout(function () { I18n.setLanguage(lang); }, reduced ? 0 : I18n.settleMs);
    }
  });

  // The settings page toggles the masthead switcher without a reload.
  document.addEventListener('language-switch-setting-changed', function (event) {
    var visible = !(event.detail && event.detail.visible === false);
    document.querySelectorAll('.lang-pill').forEach(function (pill) { pill.hidden = !visible; });
  });

  document.addEventListener('alpine:init', function () {
    window.Alpine.magic('t', function () {
      return function (key, params) { return I18n.t(key, params); };
    });
    window.Alpine.magic('tn', function () {
      return function (key, n, params) { return I18n.tn(key, n, params); };
    });
  });
})();

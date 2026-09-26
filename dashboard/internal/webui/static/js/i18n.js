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

  // --- Formats ---------------------------------------------------------
  // Numbers follow the operator's global setting (<meta name="number-format">
  // and <meta name="number-grouping">, rendered from settings.json), not the
  // language. "auto" takes the separators from the active catalog. Dates,
  // times and sorting follow the catalog's meta.locale. The number rules
  // mirror dashboard/internal/numfmt, both run internal/numfmt/testdata/cases.json.
  // The functions never use `this`, so callers may pass them around detached.
  var THIN_SPACE = ' ';
  var MIN_GROUP_DIGITS = 5;
  var PLAIN_DECIMAL = /^-?\d+(\.\d+)?$/;

  function single(value) {
    return typeof value === 'string' && Array.from(value).length === 1;
  }

  function resolveNumberStyle(format, grouping, catalogDecimal, catalogGroup) {
    var style = { decimal: '.', group: ',' };
    if (format === 'comma') {
      style = { decimal: ',', group: '.' };
    } else if (format !== 'point' && single(catalogDecimal) && single(catalogGroup) && catalogDecimal !== catalogGroup) {
      style = { decimal: catalogDecimal, group: catalogGroup };
    }
    if (grouping === 'thin') {
      style.group = THIN_SPACE;
    }
    return style;
  }

  function digits(style, text) {
    var sign = '';
    if (text.charAt(0) === '-') {
      sign = '-';
      text = text.slice(1);
    }
    var dot = text.indexOf('.');
    var whole = dot < 0 ? text : text.slice(0, dot);
    var fraction = dot < 0 ? null : text.slice(dot + 1);
    if (whole.length >= MIN_GROUP_DIGITS) {
      whole = whole.replace(/\B(?=(\d{3})+$)/g, style.group);
    }
    return sign + whole + (fraction === null ? '' : style.decimal + fraction);
  }

  function formatValueWith(style, raw, unit) {
    if (typeof raw !== 'string' || !String(unit || '').trim() || !PLAIN_DECIMAL.test(raw)) {
      return raw;
    }
    return digits(style, raw);
  }

  function formatNumberWith(style, value, decimals) {
    var number = typeof value === 'number' ? value : Number(value);
    if (!Number.isFinite(number)) {
      return '';
    }
    var text = number.toFixed(decimals || 0);
    if (/^-[0.]+$/.test(text)) {
      text = text.slice(1);
    }
    return digits(style, text);
  }

  function metaContent(name) {
    var element = document.querySelector('meta[name="' + name + '"]');
    return element ? element.getAttribute('content') || '' : '';
  }

  function toDate(value) {
    var date = value instanceof Date ? value : new Date(value);
    return Number.isNaN(date.getTime()) ? null : date;
  }

  // A changed setting reloads the page (settings.page.js), so resolving the
  // style once per page load is enough.
  var numberStyle = null;
  var collator = null;

  I18n.locale = I18n.catalog['meta.locale'] || I18n.lang;
  I18n.number = { resolve: resolveNumberStyle, value: formatValueWith, fixed: formatNumberWith };
  I18n.numberStyle = function () {
    if (!numberStyle) {
      numberStyle = resolveNumberStyle(
        metaContent('number-format') || 'auto',
        metaContent('number-grouping') || 'match',
        I18n.catalog['meta.number.decimal'],
        I18n.catalog['meta.number.group']
      );
    }
    return numberStyle;
  };
  I18n.formatNumber = function (value, decimals) {
    return formatNumberWith(I18n.numberStyle(), value, decimals);
  };
  I18n.formatValue = function (raw, unit) {
    return formatValueWith(I18n.numberStyle(), raw, unit);
  };
  I18n.formatDateTime = function (value, options) {
    var date = toDate(value);
    return date ? date.toLocaleString(I18n.locale, options) : '';
  };
  I18n.formatDate = function (value, options) {
    var date = toDate(value);
    return date ? date.toLocaleDateString(I18n.locale, options) : '';
  };
  I18n.formatTime = function (value, options) {
    var date = toDate(value);
    return date ? date.toLocaleTimeString(I18n.locale, options) : '';
  };
  I18n.compare = function (a, b) {
    if (!collator) {
      collator = new Intl.Collator(I18n.locale);
    }
    return collator.compare(a == null ? '' : String(a), b == null ? '' : String(b));
  };

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

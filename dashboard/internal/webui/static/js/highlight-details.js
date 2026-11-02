// Die Details eines Highlights (langer Text, Bild mit Ausschnitt-Umschalter,
// Doku-Link). Die Versionsseite und die Checkliste rendern dasselbe Template
// (templates/highlight-details.html) und holen die Logik von hier.
(() => {
  const localized = (entry) => (window.I18n ? window.I18n.localized(entry.i18n, entry.text) : entry.text);

  window.highlightDetails = {
    // Der Text eines Eintrags mit PR-Nummer, in der Seitensprache.
    text(entry) {
      const text = localized(entry);
      return entry.pr ? `${text} (#${entry.pr})` : text;
    },

    // Der Detailtext eines Eintrags in der Seitensprache, Englisch als Fallback.
    detailText(entry) {
      const text = entry.details.text;
      return window.I18n ? window.I18n.localized(text, text.en) : text.en;
    },

    // Position und Groesse eines Bildes in seinem Rahmen. crop ist [x, y, w, h]
    // als Anteile am Bild; ohne crop oder mit cropped false fuellt das ganze
    // Bild den Rahmen.
    imageFrame(image, cropped) {
      const [x, y, w, h] = cropped && image.crop ? image.crop : [0, 0, 1, 1];
      return {
        aspect: `${w * image.width} / ${h * image.height}`,
        left: `${(-x / w) * 100}%`,
        top: `${(-y / h) * 100}%`,
        width: `${100 / w}%`,
        height: `${100 / h}%`,
      };
    },

    // Scrollt eine geoeffnete Zeile ins Blickfeld, nachdem die Oeffnungsanimation
    // gelaufen ist.
    onToggle(event) {
      const row = event.target;
      if (!row.open) return;
      const reduced = Boolean(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
      setTimeout(() => row.scrollIntoView({block: 'nearest', behavior: reduced ? 'auto' : 'smooth'}), 330);
    },
  };
})();

// Device picker shared by the device map's group panel and the energy page's
// group cards: a modal with a search field, devices grouped by manufacturer
// (the closest thing to "which bridge or service" a Discovery device carries)
// and multi-select. Each page mixes mixin() into its Alpine component and
// renders the "device-picker" template from devicemap.html inside it.
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);

  const OTHER_SECTION = '\u0000other';

  // Sort order follows the dashboard language (I18n.compare), like every
  // other sorted list.
  const compare = (a, b) => (window.I18n ? window.I18n.compare(a, b) : (String(a) < String(b) ? -1 : String(a) > String(b) ? 1 : 0));

  const normalize = text => String(text || '').toLocaleLowerCase().normalize('NFKD').replace(/[̀-ͯ]/g, '');

  // items: [{id, name, section, valueText, iconMarkup, memberOf}], groups use
  // section '' and come first under their own heading. Returns
  // [{key, label, items}] with empty sections dropped.
  const sections = (items, query = '') => {
    const needle = normalize(query).trim();
    const matches = item => !needle || normalize(item.name).includes(needle) || normalize(item.section).includes(needle);
    const groups = items.filter(item => item.kind === 'group' && matches(item)).sort((a, b) => compare(a.name, b.name));
    const bySection = new Map();
    for (const item of items) {
      if (item.kind === 'group' || !matches(item)) continue;
      const key = item.section ? item.section : OTHER_SECTION;
      if (!bySection.has(key)) bySection.set(key, []);
      bySection.get(key).push(item);
    }
    const keys = [...bySection.keys()].sort((a, b) => (a === OTHER_SECTION) - (b === OTHER_SECTION) || compare(a, b));
    const result = groups.length ? [{key: 'groups', label: t('device_picker.section.groups'), items: groups}] : [];
    for (const key of keys) {
      result.push({
        key,
        label: key === OTHER_SECTION ? t('device_picker.section.misc') : key,
        items: bySection.get(key).sort((a, b) => compare(a.name, b.name)),
      });
    }
    return result;
  };

  const mixin = () => ({
    picker: null, // {title, items, query, selected, resolve} while the dialog is open

    // Resolves with the chosen ids, or null when cancelled.
    openPicker({title, items}) {
      if (this.picker) this.picker.resolve(null);
      return new Promise(resolve => {
        this.picker = {title, items, query: '', selected: [], resolve};
        const dialog = this.$refs && this.$refs.devicePicker;
        if (dialog && typeof dialog.showModal === 'function' && !dialog.open) dialog.showModal();
      });
    },

    pickerSections() {
      return this.picker ? sections(this.picker.items, this.picker.query) : [];
    },

    isPicked(id) {
      return Boolean(this.picker) && this.picker.selected.includes(id);
    },

    togglePick(id) {
      if (!this.picker) return;
      const selected = this.picker.selected;
      this.picker.selected = selected.includes(id) ? selected.filter(other => other !== id) : [...selected, id];
    },

    closePicker(confirmed) {
      if (!this.picker) return;
      const {resolve, selected} = this.picker;
      this.picker = null;
      const dialog = this.$refs && this.$refs.devicePicker;
      if (dialog && dialog.open && typeof dialog.close === 'function') dialog.close();
      resolve(confirmed && selected.length ? [...selected] : null);
    },

    pickerConfirmLabel() {
      const n = this.picker ? this.picker.selected.length : 0;
      return n ? (window.I18n ? window.I18n.tn('device_picker.confirm', n, {n}) : String(n)) : t('device_picker.confirm_none');
    },
  });

  window.DevicePicker = {sections, mixin};
})();

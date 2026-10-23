// Device picker shared by the device map's group panel and the energy page's
// group cards: a modal with a search field, role filter chips, devices grouped
// by manufacturer (the closest thing to "which bridge or service" a Discovery
// device carries) and either multi-select or single choice. Each page mixes
// mixin() into its Alpine component and renders the "device-picker" template
// from devicemap.html inside it.
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);

  const OTHER_SECTION = '\u0000other';

  // Sort order follows the dashboard language (I18n.compare), like every
  // other sorted list.
  const compare = (a, b) => (window.I18n ? window.I18n.compare(a, b) : (String(a) < String(b) ? -1 : String(a) > String(b) ? 1 : 0));

  const normalize = text => String(text || '').toLocaleLowerCase().normalize('NFKD').replace(/[̀-ͯ]/g, '');

  // items: [{id, name, section, valueText, iconMarkup, memberOf, subtitle, iconRef, tone, dim, filter}],
  // groups use section '' and come first under their own heading. A non-empty
  // filterKey keeps only devices with that filter (groups are not filtered).
  // Returns [{key, label, items}] with empty sections dropped.
  const sections = (items, query = '', filterKey = '') => {
    const needle = normalize(query).trim();
    const matches = item => !needle || normalize(item.name).includes(needle) || normalize(item.section).includes(needle);
    const groups = items.filter(item => item.kind === 'group' && matches(item)).sort((a, b) => compare(a.name, b.name));
    const bySection = new Map();
    for (const item of items) {
      if (item.kind === 'group' || !matches(item)) continue;
      if (filterKey && item.filter !== filterKey) continue;
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
    picker: null, // {title, items, mode, filters, filter, query, selected, confirmLabel, resolve} while the dialog is open

    // Multi (default) resolves with the chosen ids, single resolves with one
    // id; both resolve with null when cancelled.
    openPicker({title, items, mode = 'multi', filters = [], selected = [], confirmLabel = ''}) {
      if (this.picker) this.picker.resolve(null);
      return new Promise(resolve => {
        const initial = mode === 'single' ? selected.slice(0, 1) : [...selected];
        this.picker = {title, items, mode, filters, filter: '', query: '', selected: initial, confirmLabel, resolve};
        const dialog = this.$refs && this.$refs.devicePicker;
        if (dialog && typeof dialog.showModal === 'function' && !dialog.open) dialog.showModal();
      });
    },

    pickerSections() {
      return this.picker ? sections(this.picker.items, this.picker.query, this.picker.filter) : [];
    },

    // Chips above the list: "Alle" first, then the caller's role filters.
    pickerFilterChips() {
      const filters = this.picker ? this.picker.filters : [];
      return [{key: '', label: t('device_picker.filter.all')}, ...filters];
    },

    isPicked(id) {
      return Boolean(this.picker) && this.picker.selected.includes(id);
    },

    togglePick(id) {
      if (!this.picker) return;
      const selected = this.picker.selected;
      if (this.picker.mode === 'single') {
        this.picker.selected = [id];
        return;
      }
      this.picker.selected = selected.includes(id) ? selected.filter(other => other !== id) : [...selected, id];
    },

    // Double click on a single-choice item: pick it and confirm at once.
    pickerChoose(id) {
      if (!this.picker || this.picker.mode !== 'single') return;
      this.togglePick(id);
      this.closePicker(true);
    },

    closePicker(confirmed) {
      if (!this.picker) return;
      const {resolve, selected, mode} = this.picker;
      this.picker = null;
      const dialog = this.$refs && this.$refs.devicePicker;
      if (dialog && dialog.open && typeof dialog.close === 'function') dialog.close();
      if (!confirmed || !selected.length) {
        resolve(null);
        return;
      }
      resolve(mode === 'single' ? selected[0] : [...selected]);
    },

    pickerConfirmLabel() {
      if (!this.picker) return t('device_picker.confirm_none');
      const {selected, mode, confirmLabel} = this.picker;
      if (mode === 'single') return confirmLabel || t('device_picker.confirm_none');
      const n = selected.length;
      if (confirmLabel && n) return confirmLabel;
      return n ? (window.I18n ? window.I18n.tn('device_picker.confirm', n, {n}) : String(n)) : t('device_picker.confirm_none');
    },
  });

  window.DevicePicker = {sections, mixin};
})();

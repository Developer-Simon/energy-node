// Das Geraete-Modal: Detailansicht, Tabs, Aktivitaet, Darstellung, Unter-
// geraete und Discovery-Loeschen. devicesPanel (dashboard.js) laedt diese
// Datei erst beim ersten Oeffnen eines Geraets und haengt deviceModalMixin()
// in seine eigenen Daten ein; bis dahin rendert das Modal-Markup nicht.
// Hier stehen auch die beiden Direktiven, die nur das Modal benutzt
// (x-roll, x-flip).
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const tn = (key, n, params) => (window.I18n ? window.I18n.tn(key, n, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(apiError(body));
    return body;
  };

  // Leistungs-Einheiten, fuer die ein Untergeraet angelegt werden kann (wie
  // isSubDevicePowerUnit in der Go-API).
  const SUB_DEVICE_POWER_UNITS = ['W', 'kW'];
  const ACTIVITY_LOG_LIMIT = 200;
  // Hoechstzahl der Favoriten je Geraet (maxFavoriteRefs in settings.go).
  const FAVORITE_LIMIT = 3;

  window.deviceModalMixin = () => ({
    deviceDetail: null,
    // Aktivitaet seit dem Oeffnen des Modals (mergeActivity), neueste oben.
    activityLog: [],
    detailLoading: false,
    detailError: '',
    reloadLoading: false,
    detailRequestToken: 0,
    activeDeviceTab: 'measurements',
    mergedTab: false,
    // Erfahrungswert, kein Naturgesetz: ab dieser verfuegbaren Hoehe der
    // device-modal-scrollarea (Sidebar+Switcher zusammen, siehe
    // observeMergeThreshold) legen sich Messwerte und Steuerung zu einem Tab
    // zusammen. Kalibriert in Task 8 dieses Plans gegen echte Geraete.
    mergeHeightThreshold: 520,
    mergeObserver: null,
    // Zuletzt gemessene Scrollflaechen-Hoehe - damit setDeviceTab() beim
    // Wechsel auf Messwerte/Steuerung den Merge-Zustand nachziehen kann,
    // ohne auf das naechste ResizeObserver-Ereignis zu warten.
    availableHeight: 0,
    // true, sobald das Modal schmaler als die 860px-Container-Schwelle ist
    // (Akkordeon statt Tableiste). Rein aus JS beobachtet, weil die
    // Aktivitaets-Sektion in "Verwaltung & Analyse" nur im Akkordeon zeigt.
    isAccordion: false,
    // Sekundentakt fuer die relative "aktualisiert vor X"-Anzeige.
    nowTick: Date.now(),
    clockTimer: null,
    deletePreview: null,
    deletePreviewDeviceId: '',
    deletePreviewLoading: false,
    deletePreviewError: '',
    deleteLoading: false,

    // Darstellung je Geraet (device-prefs.json). Stern und Icon speichern
    // sofort, darum spiegelt prefsDraft nur das gespeicherte Icon.
    deviceIcons: [],
    prefsDraft: {icon: ''},
    prefsSaving: false,
    prefsMessage: '',
    // Zaehlt jeden Stern, der am Limit scheitert (siehe toggleFavorite).
    favoriteLimitHit: 0,
    // Icon-Auswahl und Mehr-Menue im Modal-Kopf. Beide schliessen beim
    // Gerätewechsel und bei Escape (closeFloatingMenus).
    iconPickerOpen: false,
    moreMenuOpen: false,
    subDevices: [],
    subEdgeProps: [],
    // False until the map of the open device has loaded. The create action
    // stays hidden until then, and after a failed load.
    subDevicesReady: false,
    subLoadToken: 0,
    subForm: null,
    subSaving: false,
    // Opt-in switch "Manage sub-devices". Off whenever a device opens or
    // closes, never persisted. Create, edit and delete need it.
    subManage: false,

    async loadDeviceIcons() {
      if (this.deviceIcons.length) return;
      try {
        this.deviceIcons = await requestJSON('/api/v1/device/icons');
      } catch (error) {
        this.deviceIcons = [];
      }
    },

    // Das Icon, fuer das eine leere Auswahl steht: der Vorschlag zum
    // Geraetetyp (suggestedDeviceIcon in deviceicons.go), sonst der Chip.
    get defaultIconName() {
      return this.deviceDetail?.suggested_icon || 'energy-node:chip';
    },

    // Was die Karte gerade zeigt - fuer den Modal-Kopf.
    get savedIconName() {
      return this.deviceDetail?.icon_name || this.defaultIconName;
    },

    get draftIconName() {
      return this.prefsDraft.icon || this.defaultIconName;
    },

    // Picker-Gruppen in Katalogreihenfolge: der Katalog liefert jede
    // Kategorie als zusammenhaengenden Block (deviceicons.go).
    get deviceIconGroups() {
      const groups = [];
      for (const icon of this.deviceIcons) {
        const last = groups[groups.length - 1];
        if (last && last.category === icon.category) last.icons.push(icon);
        else groups.push({category: icon.category, categoryKey: icon.categoryKey, icons: [icon]});
      }
      return groups;
    },

    // Baut denselben SVG-Rumpf wie deviceIcon() in deviceicons.go, damit
    // Picker und gerenderte Karte identisch aussehen.
    iconMarkup(name) {
      const fallback = this.deviceIcons.find(icon => icon.name === 'energy-node:chip');
      const icon = this.deviceIcons.find(candidate => candidate.name === name) || fallback;
      const markup = icon ? icon.markup : '';
      return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">${markup}</svg>`;
    },

    // prefsDraft spiegelt nur das gespeicherte Icon (Anzeige im Picker).
    seedPrefsDraft() {
      this.prefsDraft = {icon: this.deviceDetail?.icon_name || ''};
    },

    // Favoriten und Icon, so wie das offene Geraet sie gerade hat. Jede
    // Schreibaktion sendet beide Felder, darum gehen sie immer zusammen.
    appearanceOf() {
      return {
        icon: this.deviceDetail?.icon_name || '',
        favorite_refs: [...(this.deviceDetail?.favorite_refs || [])],
      };
    },

    // Setzt Favoriten und Icon im Modal, solange es noch dasselbe Geraet zeigt.
    applyAppearance(deviceId, appearance) {
      if (this.selectedDeviceId !== deviceId || !this.deviceDetail) return;
      this.deviceDetail = {
        ...this.deviceDetail,
        icon_name: appearance.icon,
        favorite_refs: [...appearance.favorite_refs],
      };
      this.prefsDraft = {icon: appearance.icon};
    },

    // Speichert Favoriten und Icon fuer deviceId. Der Stand im Modal wechselt
    // sofort; scheitert das Speichern, geht er auf previous zurueck. deviceId
    // ist das Geraet beim Ausloesen - nie selectedDeviceId, denn beim
    // Rueckgaengig kann laengst ein anderes Geraet offen sein. undoable false
    // ist das Rueckgaengig selbst, es bietet keine weitere Aktion an.
    async writeAppearance(deviceId, next, previous, messageKey, undoable = true) {
      this.applyAppearance(deviceId, next);
      try {
        await requestJSON(
          `/api/v1/device/prefs/${encodeURIComponent(deviceId)}`,
          {...this.mutationOptions({icon: next.icon, favorite_refs: next.favorite_refs}), method: 'PUT'},
        );
      } catch (error) {
        this.applyAppearance(deviceId, previous);
        this.$store.toasts.push(error.message, 'critical');
        return false;
      }
      // Das neu geladene Detail bringt die card_refs mit. Scheitert nur das
      // Nachladen, stimmt der Stand im Modal schon; der Fehler wird gemeldet.
      try {
        await this.loadDeviceDetail(deviceId);
      } catch (error) {
        if (this.selectedDeviceId === deviceId) this.detailError = error.message;
      }
      const options = undoable
        ? {action: {run: () => this.writeAppearance(deviceId, previous, next, messageKey, false)}}
        : undefined;
      this.$store.toasts.push(t(messageKey), 'info', options);
      return true;
    },

    // Stern am Entity. Ohne Recht passiert nichts. Ein viertes Favorit wird
    // nicht gesendet, sondern nur gemeldet.
    async toggleFavorite(entity) {
      const ref = entity?.unique_id;
      const deviceId = this.selectedDeviceId;
      if (!this.canEditAppearance || !ref || !deviceId || !this.deviceDetail) return;
      const previous = this.appearanceOf();
      const favorited = previous.favorite_refs.includes(ref);
      if (!favorited && previous.favorite_refs.length >= FAVORITE_LIMIT) {
        this.favoriteLimitHit += 1;
        this.$store.toasts.push(t('devices.favourites.max'), 'warning');
        return;
      }
      const favorite_refs = favorited
        ? previous.favorite_refs.filter(item => item !== ref)
        : [...previous.favorite_refs, ref];
      await this.writeAppearance(deviceId, {icon: previous.icon, favorite_refs}, previous, 'devices.favourites.saved');
    },

    // Icon aus dem Katalog. Das Vorschlagsicon speichert als leere Auswahl,
    // damit das Geraet weiter seinem Typ folgt.
    async saveIcon(name) {
      const deviceId = this.selectedDeviceId;
      if (!this.canEditAppearance || !name || !deviceId || !this.deviceDetail) return;
      const previous = this.appearanceOf();
      const icon = name === this.defaultIconName ? '' : name;
      await this.writeAppearance(deviceId, {icon, favorite_refs: previous.favorite_refs}, previous, 'devices.icon.saved');
    },

    // Der Picker-Klick speichert sofort (saveIcon).
    pickIcon(name) {
      return this.saveIcon(name);
    },

    closeFloatingMenus() {
      this.iconPickerOpen = false;
      this.moreMenuOpen = false;
    },

    async ignoreDevice(deviceId) {
      if (!deviceId) return;
      const confirmed = await this.$store.modal.confirm({
        title: t('devices.ignore.title'),
        confirmLabel: t('devices.ignore.confirm'),
        danger: true,
      });
      if (!confirmed) return;
      try {
        await requestJSON(`/api/v1/devices/${encodeURIComponent(deviceId)}/ignore`, this.mutationOptions());
        this.closeDeviceDetail();
        await this.refreshAfterMutation();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    async openDiscoveryDelete(deviceId) {
      if (!deviceId || !this.canDeleteDiscovery) return;
      this.deletePreviewDeviceId = deviceId;
      this.deletePreview = null;
      this.deletePreviewError = '';
      this.deletePreviewLoading = true;
      this.$nextTick(() => {
        const dialog = this.$refs.discoveryDeleteDialog;
        if (dialog && !dialog.open) dialog.showModal();
      });
      try {
        this.deletePreview = await requestJSON(`/api/v1/devices/${encodeURIComponent(deviceId)}/discovery-delete/preview`);
      } catch (error) {
        this.deletePreviewError = error.message;
      } finally {
        this.deletePreviewLoading = false;
      }
    },

    closeDiscoveryDelete() {
      if (this.$refs.discoveryDeleteDialog?.open) this.$refs.discoveryDeleteDialog.close();
      this.deletePreview = null;
      this.deletePreviewDeviceId = '';
      this.deletePreviewError = '';
    },

    async confirmDiscoveryDelete() {
      if (!this.deletePreviewDeviceId || !this.deletePreview || this.deleteLoading) return;
      const confirmed = await this.$store.modal.confirm({
        title: t('devices.discovery_delete.title'),
        confirmLabel: t('devices.discovery_delete.confirm'),
        danger: true,
      });
      if (!confirmed) return;
      this.deleteLoading = true;
      try {
        await requestJSON(`/api/v1/devices/${encodeURIComponent(this.deletePreviewDeviceId)}/discovery-delete`, this.mutationOptions({confirm: true}));
        this.closeDiscoveryDelete();
        await this.refreshAfterMutation();
      } catch (error) {
        this.deletePreviewError = error.message;
      } finally {
        this.deleteLoading = false;
      }
    },

    formatDetailTime(value) {
      if (!value) return '-';
      return window.I18n.formatDateTime(value) || '-';
    },

    // Kurzform "vor Xs/min/h" fuer die Kurzinfo an den Modal-Trigger. now
    // ist ein optionaler zweiter Parameter, damit der Test ohne echte Uhr
    // arbeitet - im Betrieb bleibt er weg und faellt auf new Date() zurueck.
    relativeTime(value, now = new Date()) {
      const date = value instanceof Date ? value : new Date(value);
      if (Number.isNaN(date.getTime())) return '';
      const seconds = Math.max(0, Math.round((now.getTime() - date.getTime()) / 1000));
      if (seconds < 60) return tn('time.ago.seconds', seconds);
      if (seconds < 3600) return tn('time.ago.minutes', Math.round(seconds / 60));
      return tn('time.ago.hours', Math.round(seconds / 3600));
    },

    get sortedWarnings() {
      const severityRank = {critical: 3, warning: 2, info: 1};
      return [...(this.deviceDetail?.warnings || [])].sort((left, right) =>
        (severityRank[right.severity] || 0) - (severityRank[left.severity] || 0)
      );
    },

    get groupedWarnings() {
      const groups = new Map();
      for (const warning of this.sortedWarnings) {
        const key = `${warning.rule_id}\u0000${warning.key || warning.message}\u0000${warning.key ? '' : warning.hint}`;
        let group = groups.get(key);
        if (!group) {
          group = {...warning, affectedEntityIds: [], count: 0};
          groups.set(key, group);
        }
        group.count += 1;
        if (warning.entity_id && !group.affectedEntityIds.includes(warning.entity_id)) {
          group.affectedEntityIds.push(warning.entity_id);
        }
      }
      return [...groups.values()];
    },

    // Betriebswarnungen (Gerät offline, Bridge) und Konfigurationswarnungen
    // teilen sich dieselben Gruppen; warning.operational trennt sie.
    get operationalWarnings() {
      return this.groupedWarnings.filter(warning => warning.operational);
    },

    get configWarnings() {
      return this.groupedWarnings.filter(warning => !warning.operational);
    },

    entityLabel(entityID) {
      const entity = (this.deviceDetail?.entities || []).find(item => item.unique_id === entityID);
      if (!entity) return entityID;
      return `${entity.name || entity.object_id} (${entityID})`;
    },

    // entityDisplayName strips a leading device-name prefix from the entity
    // name for display within a single device's detail modal, where the
    // device name is already shown in the dialog header and repeating it on
    // every entity just wastes space. Falls back to the full name whenever
    // it doesn't start with the device name, since not every MQTT bridge
    // prefixes entity names with the device name.
    entityDisplayName(entity, fallbackKey = 'object_id') {
      const name = (entity?.name || '').trim();
      const fallback = entity?.[fallbackKey] || entity?.unique_id || '';
      if (!name) return fallback;
      const deviceName = (this.deviceDetail?.name || '').trim();
      if (!deviceName || !name.toLowerCase().startsWith(deviceName.toLowerCase())) return name;
      const rest = name.slice(deviceName.length).trim();
      return rest || name;
    },

    // Nur noch ein Lesepfad fuer das Template (Hinweis "keine Aktivitaet")
    // und managementTeaser. Task 12 entfernt ihn zusammen mit den
    // Template-Verweisen.
    get recentMessages() {
      return this.activityLog.filter(entry => entry.kind === 'message');
    },

    formatStateValue(value) {
      if (value === null || value === undefined) return '–';
      if (typeof value === 'object') return JSON.stringify(value);
      return String(value);
    },

    // i18n-keys: diagnostics.rule.duplicate_unique_id.message, diagnostics.rule.duplicate_unique_id.hint, diagnostics.rule.discovery_invalid_json.message, diagnostics.rule.discovery_invalid_json.hint, diagnostics.rule.discovery_mismatch.message, diagnostics.rule.discovery_mismatch.hint, diagnostics.rule.discovery_not_retained.message, diagnostics.rule.discovery_not_retained.hint, diagnostics.rule.missing_topic.message, diagnostics.rule.missing_topic.hint, diagnostics.rule.missing_availability.message, diagnostics.rule.missing_availability.hint, diagnostics.rule.missing_unit.message, diagnostics.rule.missing_unit.hint, diagnostics.rule.no_state_update.message, diagnostics.rule.no_state_update.hint, diagnostics.rule.offline.message, diagnostics.rule.offline.hint, diagnostics.rule.empty_state_payload.message, diagnostics.rule.empty_state_payload.hint, diagnostics.rule.invalid_discovery_payload.message, diagnostics.rule.invalid_discovery_payload.hint, diagnostics.rule.ignored_device_discovery_stale.message, diagnostics.rule.ignored_device_discovery_stale.hint, diagnostics.rule.configured_device_missing.message, diagnostics.rule.configured_device_missing.hint
    warningText(item, part) {
      const key = item && item.key ? `diagnostics.rule.${item.key}.${part}` : '';
      if (key && window.I18n && window.I18n.has(key)) return t(key);
      return (item && item[part]) || '';
    },

    // Die Aktivitaet kommt vom Server (GET /api/v1/devices/{id}/activity), der
    // pro Geraet nur die letzten Eintraege kennt. Das Log waechst trotzdem
    // weiter: neue Zeilen kommen oben dazu, bekannte bleiben darunter stehen.
    // Ein Fehler laesst die Anzeige stehen, eine Antwort fuer ein anderes
    // Geraet wird verworfen.
    async loadActivity(deviceId, requestToken = this.detailRequestToken) {
      let body;
      try {
        body = await requestJSON(`/api/v1/devices/${encodeURIComponent(deviceId)}/activity`);
      } catch (error) {
        return;
      }
      if (requestToken !== this.detailRequestToken || this.selectedDeviceId !== deviceId) return;
      this.mergeActivity(body?.entries || []);
    },

    // Gedeckelt, damit ein schnell sendendes Geraet den Speicher nicht fuellt.
    mergeActivity(entries = []) {
      const known = new Set(this.activityLog.map(entry => entry.key));
      const fresh = entries.filter(entry => !known.has(entry.key)).map(entry => this.activityRow(entry));
      if (fresh.length) this.activityLog = [...fresh, ...this.activityLog].slice(0, ACTIVITY_LOG_LIMIT);
    },

    // Serverzeile -> Zeile fuers Template (key, at, kind, text). Der Text
    // entsteht hier, damit das Template nur text ausgibt. Ein leerer
    // MQTT-Payload kommt als Key ...-empty ohne Wert.
    activityRow(entry) {
      let text;
      if (entry.kind === 'command') {
        const result = entry.ok ? t('devices.activity.command_sent') : t('devices.activity.command_failed');
        text = `${entry.label}: ${result}`;
      } else {
        const value = String(entry.key).endsWith('-empty') ? t('devices.messages.empty_payload') : entry.value;
        text = `${entry.label}: ${value}`;
      }
      return {key: entry.key, at: entry.at, kind: entry.kind, text};
    },

    entityCategory(entity) {
      const category = String(entity.entity_category || '').toLowerCase();
      if (category === 'diagnostic') return 'diagnostics';
      if (category === 'config') return 'configuration';
      const component = String(entity.component || '').toLowerCase();
      if (entity.commandable || ['button', 'number', 'select', 'text'].includes(component)) return 'controls';
      const text = `${entity.name || ''} ${entity.object_id || ''} ${entity.device_class || ''}`.toLowerCase();
      if (entity.default_hidden || /diagnos|diagnostic|error|fault|rssi|signal|uptime|firmware|update|version|status/.test(text)) return 'diagnostics';
      if (/config|configuration|setting|einstellung|option|mode|modus/.test(text)) return 'configuration';
      return 'measurements';
    },

    get deviceAvailability() {
      const entities = this.deviceDetail?.entities || [];
      const withAvailability = entities.filter(entity => entity.has_availability);
      if (!withAvailability.length) return 'unknown';
      return withAvailability.some(entity => entity.available) ? 'online' : 'offline';
    },

    get measurementEntities() {
      return (this.deviceDetail?.entities || []).filter(entity => this.entityCategory(entity) === 'measurements');
    },

    get controlEntities() {
      return (this.deviceDetail?.entities || []).filter(entity => this.entityCategory(entity) === 'controls');
    },

    get configDiagEntities() {
      return (this.deviceDetail?.entities || []).filter(entity => ['configuration', 'diagnostics'].includes(this.entityCategory(entity)));
    },

    // Der "Konfiguration & Diagnose"-Tab teilt sich in zwei Abschnitte:
    // Konfigurations-Entitaeten sind haeufig schaltbar (number/select/switch
    // mit entity_category=config) und bekommen deshalb die Steuer-Kachel;
    // Diagnose-Entitaeten bleiben schreibgeschuetzte Wert-Kacheln.
    get configEntities() {
      return (this.deviceDetail?.entities || []).filter(entity => this.entityCategory(entity) === 'configuration');
    },
    get diagnosticEntities() {
      return (this.deviceDetail?.entities || []).filter(entity => this.entityCategory(entity) === 'diagnostics');
    },

    // Zusammenlegen von Messwerte + Steuerung ist nur sinnvoll, wenn beide
    // Seiten ueberhaupt etwas zu zeigen haben - sonst bliebe eine leere
    // Untersektion stehen und der "Steuerung"-Tab waere gar nicht da.
    get canMerge() {
      return this.measurementEntities.length > 0 && this.controlEntities.length > 0;
    },

    // "aktualisiert vor X" - rechtsbuendige, sekundengenaue Relativzeit
    // (nowTick tickt im Sekundentakt, siehe init()).
    get updatedRelative() {
      const at = this.deviceDetail?.last_updated;
      if (!at) return '';
      const relative = this.relativeTime(at, this.nowTick ? new Date(this.nowTick) : new Date());
      return relative ? t('devices.detail.updated', {relative}) : '';
    },

    get measurementsTeaser() {
      const first = this.measurementEntities[0];
      if (!first || !first.has_value) return '';
      return `${first.value}${first.unit_of_measurement ? ` ${first.unit_of_measurement}` : ''}`;
    },

    get controlsTeaser() {
      const first = this.controlEntities[0];
      if (!first) return '';
      if (!first.has_availability) return t('devices.controls.availability_unknown');
      return first.has_value ? first.value : (first.available ? t('status.online') : t('status.offline'));
    },

    get configDiagTeaser() {
      if (!this.configDiagEntities.length) return '';
      const staleDiagnostic = this.configDiagEntities.some(entity => this.entityCategory(entity) === 'diagnostics' && entity.stale);
      return `${this.configDiagEntities.length}${staleDiagnostic ? ' · ⚠' : ''}`;
    },

    // Stub bis zum Template-Task: devices.html liest pinnedFavorites.length
    // beim Rendern. Der Anheft-Block faellt mit pin_favorites weg.
    get pinnedFavorites() {
      return [];
    },

    // Die Hero-Kacheln des Geraets: bis zu drei Entitaeten aus card_refs.
    // auto markiert die, die nicht als Favorit gesetzt sind, sondern nur
    // automatisch vorgeschlagen werden.
    get heroEntities() {
      if (!this.deviceDetail) return [];
      const byID = new Map((this.deviceDetail.entities || []).map(entity => [entity.unique_id, entity]));
      const favorites = new Set(this.deviceDetail.favorite_refs || []);
      return (this.deviceDetail.card_refs || [])
        .map(ref => ({entity: byID.get(ref), auto: !favorites.has(ref)}))
        .filter(item => item.entity)
        .slice(0, 3);
    },

    isFavorite(entity) {
      return (this.deviceDetail?.favorite_refs || []).includes(entity?.unique_id);
    },

    managementTeaser(now = new Date()) {
      const warningCount = this.groupedWarnings.length;
      const latest = [...(this.deviceDetail?.command_actions || []).map(a => a.at), ...this.recentMessages.map(m => m.at)]
        .filter(Boolean)
        .sort((left, right) => new Date(right) - new Date(left))[0];
      const parts = [];
      if (warningCount) parts.push(`⚠ ${warningCount}`);
      if (latest) parts.push(this.relativeTime(latest, now));
      return parts.join(' · ');
    },

    setDeviceTab(key) {
      this.activeDeviceTab = key;
      // Merge/Split nur anfassen, wenn der Nutzer gerade Messwerte/Steuerung
      // waehlt - so springt der Tab-Satz bei allen anderen Tabs nicht mehr um.
      if (['measurements', 'controls', 'measurementsControls'].includes(key)) {
        this.applyMergeThreshold(this.availableHeight);
      }
      if (this.$nextTick) this.$nextTick(() => this.positionUnderline());
    },

    // Der Auswahlstrich wird ueber gemessene Rechtecke positioniert (nicht
    // ueber offsetLeft): das absolute Element haengt sonst an seiner
    // Flex-Static-Position und sitzt versetzt statt mittig unter dem Tab.
    positionUnderline() {
      if (!this.$refs) return;
      const underline = this.$refs.deviceModalUnderline;
      const switcher = this.$refs.deviceModalSwitcher;
      if (!underline || !switcher || typeof switcher.getBoundingClientRect !== 'function') return;
      const active = switcher.querySelector('.device-modal-trigger.active');
      if (!active || getComputedStyle(underline).display === 'none') return;
      const base = switcher.getBoundingClientRect();
      const rect = active.getBoundingClientRect();
      underline.style.width = `${rect.width}px`;
      underline.style.transform = `translateX(${rect.left - base.left}px)`;
      underline.style.top = `${rect.bottom - base.top}px`;
    },

    // Beim Oeffnen eines Geraets ist immer derselbe Tab dran, unabhaengig
    // vom Ausloeser (Kachel oder Kompakt-Karte): der erste Entitaets-Tab,
    // der etwas zu zeigen hat. "Verwaltung & Analyse" existiert immer und
    // ist deshalb der Boden dieser Kette.
    defaultDeviceTab() {
      if (this.measurementEntities.length) return this.mergedTab ? 'measurementsControls' : 'measurements';
      if (this.controlEntities.length) return this.mergedTab ? 'measurementsControls' : 'controls';
      if (this.configDiagEntities.length) return 'configDiag';
      return 'management';
    },

    applyMergeThreshold(height) {
      this.availableHeight = height;
      // Nur umschalten, solange Messwerte/Steuerung (oder ihre Fusion) aktiv
      // sind. Sitzt der Nutzer z. B. auf "Verwaltung & Analyse" und dort
      // entsteht durch Inhalt eine Scrollbar, bleibt der Merge-Zustand
      // einfach stehen - kein Herumspringen des Tab-Satzes.
      if (!['measurements', 'controls', 'measurementsControls'].includes(this.activeDeviceTab)) return;
      const shouldMerge = height >= this.mergeHeightThreshold && this.canMerge;
      if (shouldMerge === this.mergedTab) return;
      this.mergedTab = shouldMerge;
      if (shouldMerge) {
        this.activeDeviceTab = 'measurementsControls';
      } else {
        this.activeDeviceTab = this.measurementEntities.length ? 'measurements' : 'controls';
      }
      if (this.$nextTick) this.$nextTick(() => this.positionUnderline());
    },

    observeMergeThreshold() {
      if (typeof ResizeObserver === 'undefined') return;
      const scrollarea = this.$refs.deviceModalScrollarea;
      const dialog = this.$refs.deviceModalDialog;
      if (!scrollarea && !dialog) return;
      this.mergeObserver = new ResizeObserver(entries => {
        for (const entry of entries) {
          if (entry.target === dialog) {
            this.isAccordion = entry.contentRect.width > 0 && entry.contentRect.width < 860;
          } else {
            this.applyMergeThreshold(entry.contentRect.height);
          }
        }
        this.positionUnderline();
      });
      if (scrollarea) this.mergeObserver.observe(scrollarea);
      if (dialog) this.mergeObserver.observe(dialog);
    },

    async openDevice(deviceId) {
      if (!deviceId) return;
      const requestToken = ++this.detailRequestToken;
      this.selectedDeviceId = deviceId;
      this.deviceDetail = null;
      this.activityLog = [];
      this.closeFloatingMenus();
      this.detailError = '';
      this.detailLoading = true;
      this.loadDeviceIcons();
      // Jedes Geraet startet getrennt; der ResizeObserver legt gleich wieder
      // zusammen, falls die Hoehe reicht.
      this.mergedTab = false;
      this.$nextTick(() => {
        const dialog = this.$refs.deviceModalDialog;
        if (dialog && !dialog.open) dialog.showModal();
        if (dialog && typeof dialog.clientWidth === 'number' && dialog.clientWidth > 0) {
          this.isAccordion = dialog.clientWidth < 860;
        }
      });
      try {
        this.resetSubDevices();
        const subToken = this.subLoadToken;
        await this.loadDeviceDetail(deviceId, requestToken);
        await this.loadSubDevices(deviceId, subToken);
        // Erstauswahl des Tabs nur beim Oeffnen (nicht bei jedem Live-Refresh).
        if (requestToken === this.detailRequestToken && this.selectedDeviceId === deviceId) {
          this.activeDeviceTab = this.defaultDeviceTab();
        }
      } catch (error) {
        if (requestToken !== this.detailRequestToken || this.selectedDeviceId !== deviceId) return;
        this.detailError = error.message;
      } finally {
        if (requestToken === this.detailRequestToken) this.detailLoading = false;
      }
    },

    // Laedt bzw. frischt die Detaildaten auf. Der aktive Tab wird hier
    // bewusst NICHT gesetzt - sonst wuerde jede Live-Wert-Aktualisierung
    // (SSE registry-updated -> refreshSelectedDevice) den Nutzer zurueck auf
    // "Messwerte" werfen. Die Erstauswahl passiert einmalig in selectDevice.
    async loadDeviceDetail(deviceId, requestToken = this.detailRequestToken) {
      const detail = await requestJSON(`/api/v1/devices/${encodeURIComponent(deviceId)}`);
      if (requestToken !== this.detailRequestToken || this.selectedDeviceId !== deviceId) return;
      this.deviceDetail = detail;
      this.loadActivity(deviceId, requestToken);
      this.seedPrefsDraft();
      this.loadDeviceIcons();
      if (this.$nextTick) this.$nextTick(() => this.positionUnderline());
    },

    async refreshSelectedDevice() {
      const deviceId = this.selectedDeviceId;
      if (!deviceId || document.visibilityState !== 'visible') return;
      const requestToken = ++this.detailRequestToken;
      try {
        await this.loadDeviceDetail(deviceId, requestToken);
        this.detailError = '';
      } catch (error) {
        if (requestToken !== this.detailRequestToken || this.selectedDeviceId !== deviceId) return;
        this.detailError = error.message;
      } finally {
        if (this.selectedDeviceId === deviceId) this.detailLoading = false;
      }
    },

    // Untergeraete: Messwert-Kacheln mit Leistung (W, kW) im Messwert-Tab
    // tragen eine Zeile mit Icon und Name, darunter Bearbeiten und Loeschen.
    // Clears the sub-device state when a device is opened. The token moves
    // on every open and every write, so an answer that arrives late is dropped.
    resetSubDevices() {
      this.subLoadToken += 1;
      this.subManage = false;
      this.subDevices = [];
      this.subEdgeProps = [];
      this.subDevicesReady = false;
    },

    // Takes a full device map answer (load, create, change, delete).
    applySubDeviceMap(deviceMap) {
      this.subDevices = deviceMap.sub_devices || [];
      this.subEdgeProps = deviceMap.edge_props || [];
      this.subDevicesReady = true;
    },

    // Loads the map for the device opened with the given token. A stale
    // answer is ignored. On failure the list stays empty and the create action
    // stays hidden, so no duplicate create is offered.
    async loadSubDevices(deviceId = this.selectedDeviceId, token = this.subLoadToken) {
      try {
        const deviceMap = await requestJSON('/api/v1/device/map');
        if (token !== this.subLoadToken || this.selectedDeviceId !== deviceId) return;
        this.applySubDeviceMap(deviceMap);
      } catch (error) {
        if (token !== this.subLoadToken || this.selectedDeviceId !== deviceId) return;
        this.subDevices = [];
        this.subEdgeProps = [];
        this.subDevicesReady = false;
        this.$store?.toasts?.push(error.message, 'critical');
      }
    },

    // The create action needs a loaded map, else an existing sub-device could
    // be missed.
    subCreateVisible(entity) {
      return this.subActionsVisible(entity) && this.subDevicesReady && !this.subDeviceFor(entity);
    },

    isSubDeviceCandidate(entity) {
      return this.entityCategory(entity) === 'measurements' && SUB_DEVICE_POWER_UNITS.includes(entity.unit_of_measurement);
    },

    subDeviceFor(entity) {
      return (this.subDevices || []).find(sub => sub.entity_id === entity.unique_id) || null;
    },

    subDeviceRowVisible(entity) {
      return this.isSubDeviceCandidate(entity) && Boolean(this.subDeviceFor(entity) || this.subCreateVisible(entity));
    },

    // The switch is only offered when the open device has a power value that
    // could carry a sub-device, or already has one.
    subManageOffered() {
      if (!this.canEditEnergy) return false;
      return (this.deviceDetail?.entities || []).some(entity => this.isSubDeviceCandidate(entity));
    },

    subActionsVisible(entity) {
      return this.canEditEnergy && this.subManage && this.isSubDeviceCandidate(entity);
    },

    // Oeffnet den Dialog: ohne sub legt er ein Untergeraet an, mit sub
    // aendert er Name und Icon. Ein Umhaengen oder ein Kanal gibt es hier
    // nicht, der Kanal eines bestehenden Untergeraets bleibt erhalten.
    openSubDeviceForm(entity, sub = null) {
      if (!this.canEditEnergy || !this.subManage || !this.isSubDeviceCandidate(entity)) return;
      if (!sub && !this.subCreateVisible(entity)) return;
      const prop = sub ? this.subEdgeProps.find(item => item.child_id === sub.id) : null;
      this.subForm = {
        subId: sub ? sub.id : null,
        deviceId: this.selectedDeviceId,
        entityId: entity.unique_id,
        name: sub ? sub.label : this.entityDisplayName(entity),
        icon: sub ? sub.icon : this.savedIconName,
        channel: prop && Number.isInteger(prop.channel) ? prop.channel : 0,
        error: '',
      };
      this.$nextTick?.(() => {
        const dialog = this.$refs.subDeviceDialog;
        if (dialog && !dialog.open) dialog.showModal();
      });
    },

    pickSubIcon(name) {
      if (this.subForm) this.subForm.icon = name;
    },

    closeSubDeviceForm() {
      if (this.$refs?.subDeviceDialog?.open) this.$refs.subDeviceDialog.close();
      this.subForm = null;
    },

    // Legt das Untergeraet an (POST, ohne x und y: die Map platziert es beim
    // naechsten Render) oder aendert es (PUT). Jede Antwort ist die volle
    // Geraetekarte, daraus kommen Liste und Kanaele.
    async saveSubDeviceForm() {
      const form = this.subForm;
      if (!form || this.subSaving || !this.canEditEnergy) return;
      const label = form.name.trim();
      if (!label) {
        this.subForm = {...form, error: t('devices.sub.name_required')};
        return;
      }
      const body = {label, icon: form.icon, device_id: form.deviceId, entity_id: form.entityId, channel: form.channel};
      const path = form.subId ? `/api/v1/device/map/sub-devices/${encodeURIComponent(form.subId)}` : '/api/v1/device/map/sub-devices';
      const options = {...this.mutationOptions(body), method: form.subId ? 'PUT' : 'POST'};
      this.subSaving = true;
      try {
        this.subLoadToken += 1;
        const deviceMap = await requestJSON(path, options);
        this.applySubDeviceMap(deviceMap);
        this.closeSubDeviceForm();
        this.$store.toasts.push(t('devices.sub.saved'));
        this.notifySubDeviceChanged();
      } catch (error) {
        if (this.subForm) this.subForm = {...this.subForm, error: error.message};
      } finally {
        this.subSaving = false;
      }
    },

    // Entfernt das Untergeraet nach einer Bestaetigung. Die Entitaet zeigt
    // danach wieder ihren Messwert.
    async deleteSubDevice(sub) {
      if (!sub || !this.canEditEnergy || !this.subManage) return;
      const confirmed = await this.$store.modal.confirm({
        title: t('devices.sub.delete_title', {name: sub.label || sub.id}),
        confirmLabel: t('devices.sub.delete'),
        danger: true,
      });
      if (!confirmed) return;
      try {
        this.subLoadToken += 1;
        const deviceMap = await requestJSON(`/api/v1/device/map/sub-devices/${encodeURIComponent(sub.id)}`, {...this.mutationOptions(), method: 'DELETE'});
        this.applySubDeviceMap(deviceMap);
        this.$store.toasts.push(t('devices.sub.deleted'));
        this.notifySubDeviceChanged();
      } catch (error) {
        this.$store.toasts.push(error.message, 'critical');
      }
    },

    // Map, Anlagengrafik und Energie-Seite laden nach.
    notifySubDeviceChanged() {
      window.dispatchEvent(new CustomEvent('device-map-changed', {detail: {source: 'devices'}}));
    },

    closeDeviceDetail() {
      this.detailRequestToken += 1;
      if (this.$refs.deviceModalDialog?.open) this.$refs.deviceModalDialog.close();
      this.selectedDeviceId = '';
      this.deviceDetail = null;
      this.activityLog = [];
      this.detailError = '';
      this.closeFloatingMenus();
      this.closeSubDeviceForm();
      this.subLoadToken += 1;
      this.subManage = false;
      if (this.livePatchStale) {
        this.livePatchStale = false;
        this.refreshLiveFragment();
      }
    },

    async reloadDevice() {
      if (!this.selectedDeviceId || this.reloadLoading) return;
      this.reloadLoading = true;
      this.detailError = '';
      try {
        const result = await requestJSON(`/api/v1/devices/${encodeURIComponent(this.selectedDeviceId)}/reload`, {method: 'POST'});
        await this.selectDevice(this.selectedDeviceId);
        this.$store.toasts.push(t(result.scope === 'all_devices' ? 'devices.reload.done_all' : 'devices.reload.done_device'));
        await this.refreshLiveFragment();
      } catch (error) {
        this.detailError = error.message;
      } finally {
        this.reloadLoading = false;
      }
    },

    // Haengt das Modal ein, sobald sein Markup steht (devicesPanel rendert es
    // erst nach dem Laden dieser Datei): Schliessen per Esc/Formular, die
    // Hoehenbeobachtung fuer den Tab-Merge und der Sekundentakt.
    attachDeviceModal() {
      this.dialogClosed = event => {
        if (event.target === this.$refs.deviceModalDialog && this.selectedDeviceId) this.closeDeviceDetail();
      };
      this.$refs.deviceModalDialog?.addEventListener('close', this.dialogClosed);
      // Esc schliesst erst ein offenes Icon-Feld oder Mehr-Menue, das Modal
      // bleibt dabei stehen.
      this.dialogKeydown = event => {
        if (event.key !== 'Escape' || !(this.iconPickerOpen || this.moreMenuOpen)) return;
        event.preventDefault();
        this.closeFloatingMenus();
      };
      this.$refs.deviceModalDialog?.addEventListener('keydown', this.dialogKeydown);
      this.observeMergeThreshold();
      // Sekundentakt fuer die relative "aktualisiert vor X"-Anzeige im
      // Modal. Laeuft nur weiter, solange ein Geraet offen ist.
      this.clockTimer = window.setInterval(() => {
        if (this.deviceDetail && document.visibilityState === 'visible') this.nowTick = Date.now();
      }, 1000);
    },

    detachDeviceModal() {
      if (this.dialogClosed) this.$refs.deviceModalDialog?.removeEventListener('close', this.dialogClosed);
      if (this.dialogKeydown) this.$refs.deviceModalDialog?.removeEventListener('keydown', this.dialogKeydown);
      this.mergeObserver?.disconnect();
      if (this.clockTimer) window.clearInterval(this.clockTimer);
    },
  });

  const prefersReducedMotion = () =>
    typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  // x-roll: laesst eine per x-text gesetzte Zahl wie ein Zaehler zum neuen
  // Wert laufen, statt hart umzuspringen (apple-design: sanfter Uebergang
  // statt Teleport). Reiner Text-Tween ueber requestAnimationFrame - es
  // gibt keinen CSS-Weg, angezeigten Text zu interpolieren. Waehrend des
  // eigenen Schreibens wird der MutationObserver getrennt, damit er nicht
  // rekursiv ausloest. Interruptierbar: ein neuer Zielwert startet von der
  // zuletzt *gezeigten* Zahl, nicht vom alten Ziel. Nicht-numerische Werte
  // ("Ja", "-") werden sofort gesetzt. Den Rohwert liest die Direktive aus
  // data-roll-value, weil der sichtbare Text schon im Zahlenformat steht
  // (Tausenderpunkt und Dezimalkomma sind nicht eindeutig rueckwaerts lesbar).
  const registerRollDirective = Alpine => {
    Alpine.directive('roll', (el, meta, { cleanup }) => {
      const observeOptions = {childList: true, characterData: true, subtree: true};
      const parse = raw => {
        const text = String(raw == null ? '' : raw).trim();
        if (!/^-?\d*\.?\d+$/.test(text)) return null;
        const number = Number(text);
        return Number.isFinite(number) ? number : null;
      };
      let committed = parse(el.dataset.rollValue);
      let displayed = committed;
      let frame = 0;
      const observer = new MutationObserver(() => {
        const text = el.textContent;
        const raw = el.dataset.rollValue;
        const target = parse(raw);
        if (prefersReducedMotion() || committed === null || target === null || target === committed) {
          committed = target;
          displayed = target;
          return;
        }
        const from = displayed === null ? committed : displayed;
        const decimals = (String(raw).split('.')[1] || '').length;
        const unit = el.dataset.rollUnit || '';
        const startedAt = performance.now();
        const duration = 300;
        cancelAnimationFrame(frame);
        const write = value => {
          observer.disconnect();
          el.textContent = value;
          observer.observe(el, observeOptions);
        };
        const step = now => {
          const progress = Math.min(1, (now - startedAt) / duration);
          const eased = 1 - Math.pow(1 - progress, 3);
          if (progress < 1) {
            const value = from + (target - from) * eased;
            displayed = value;
            write(window.I18n.formatValue(value.toFixed(decimals), unit));
            frame = requestAnimationFrame(step);
          } else {
            write(text);
            committed = target;
            displayed = target;
          }
        };
        frame = requestAnimationFrame(step);
      });
      observer.observe(el, observeOptions);
      cleanup(() => {
        cancelAnimationFrame(frame);
        observer.disconnect();
      });
    });
  };

  // x-flip: laesst die Eintraege einer kurzen Liste an ihre neue Position
  // gleiten (FLIP), statt hart umzuspringen, wenn oben ein Eintrag
  // dazukommt. Positionen zaehlen relativ zur Liste, damit Scrollen und
  // Layout ueber der Liste keine Scheinbewegung erzeugen. Neue Eintraege
  // blenden von oben ein. Nur Transform/Opacity,
  // <=240ms ease-out, bei prefers-reduced-motion komplett aus.
  const registerFlipDirective = Alpine => {
    Alpine.directive('flip', (el, {expression}, { cleanup }) => {
      if (prefersReducedMotion()) return;
      const selector = expression || '.device-modal-log-entry';
      const easing = 'cubic-bezier(0.23, 1, 0.32, 1)';
      const rects = new Map();
      const items = () => [...el.querySelectorAll(selector)];
      // Laufender Versatz einer Gleitbewegung (translateY der Animation).
      const shift = item => new DOMMatrix(getComputedStyle(item).transform).m42;
      // Layout-Position relativ zur Liste selbst, ohne laufende Transforms.
      // offsetTop allein bezieht sich auf den Scrollbereich, und Inhalt
      // ueber der Liste (Warnungen, Chips) verschob dort alle Zeilen, ohne
      // dass die Liste sich aenderte.
      const position = item => item.getBoundingClientRect().top - el.getBoundingClientRect().top - shift(item);
      const snapshot = () => {
        rects.clear();
        for (const item of items()) rects.set(item, position(item));
      };
      // Eine Grundlinie gibt es nur, solange die Liste sichtbar ist und schon
      // Eintraege hatte. Ohne sie (Modal gerade geoeffnet, Daten erst jetzt
      // geladen) wird still aufgenommen, sonst floegen alle Eintraege ein.
      let baseline = false;
      const rebase = () => {
        if (el.offsetParent === null) { rects.clear(); baseline = false; return; }
        snapshot();
        baseline = rects.size > 0;
      };
      requestAnimationFrame(rebase);
      const resizeObserver = new ResizeObserver(() => { if (!baseline) rebase(); });
      resizeObserver.observe(el);
      const observer = new MutationObserver(() => {
        if (!baseline) { rebase(); return; }
        // Neu hinzugekommene Eintraege blenden oben ein, die uebrigen
        // rutschen gleitend nach unten (Verschiebung = alte minus neue
        // Position). Laeuft noch eine Gleitbewegung, startet die neue an der
        // gerade sichtbaren Stelle statt zurueckzuspringen.
        for (const item of items()) {
          const previous = rects.get(item);
          const next = position(item);
          if (previous === undefined) {
            item.animate(
              [{opacity: 0, transform: 'translateY(-10px)'}, {opacity: 1, transform: 'none'}],
              {duration: 240, easing},
            );
          } else if (Math.abs(previous - next) >= 0.5) {
            const running = shift(item);
            for (const animation of item.getAnimations()) animation.cancel();
            item.animate(
              [{transform: `translateY(${previous - next + running}px)`}, {transform: 'none'}],
              {duration: 240, easing},
            );
          }
        }
        rebase();
      });
      observer.observe(el, {childList: true, subtree: true});
      cleanup(() => { observer.disconnect(); resizeObserver.disconnect(); });
    });
  };

  // Alpine laeuft schon, wenn devicesPanel diese Datei nachlaedt; die
  // Direktiven greifen dann fuer das Modal-Markup, das erst danach rendert.
  const registerDirectives = Alpine => {
    registerRollDirective(Alpine);
    registerFlipDirective(Alpine);
  };
  if (window.Alpine) registerDirectives(window.Alpine);
  else document.addEventListener('alpine:init', () => registerDirectives(window.Alpine));
})();

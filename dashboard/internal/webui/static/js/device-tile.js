(() => {
  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    const body = await response.json();
    if (!response.ok) throw new Error(body.message || 'Anfrage fehlgeschlagen');
    return body;
  };

  window.deviceTileMixin = () => ({
    commandStates: {},

    async sendCommand(event, commandType = '') {
      const control = event.target?.closest?.('[data-entity-id]') || event.currentTarget;
      if (!control || control.disabled) return;
      const entityID = control.dataset.entityId;
      if (!entityID) return;
      const isNumber = commandType === 'number' || control.dataset.commandType === 'number' || control.matches?.('input[type="range"]');
      const originalTitle = control.title;
      control.disabled = true;
      control.setAttribute('aria-busy', 'true');
      control.title = 'Befehl wird gesendet ...';
      this.commandStates[entityID] = 'Befehl wird gesendet ...';
      let pending = false;
      try {
        const options = {method: 'POST'};
        if (isNumber) {
          options.headers = {'Content-Type': 'application/json'};
          options.body = JSON.stringify({value: Number(control.value)});
        } else if (control.dataset.payloadOn && control.dataset.payloadOff) {
          const currentValue = control.dataset.commandValue || '';
          const payload = currentValue === control.dataset.payloadOn ? control.dataset.payloadOff : control.dataset.payloadOn;
          options.headers = {'Content-Type': 'application/json'};
          options.body = JSON.stringify({payload});
        }
        // The dashboard never shows a write as successful before MQTT
        // confirms it (P1.3): the response only reports that the command
        // was published and is now pending, not that it took effect. The
        // control stays disabled - the next live-update refresh re-renders
        // it from the server's Pending/LastCommandResult truth, so no
        // optimistic value is applied here.
        const result = await requestJSON(`/api/v1/entities/${encodeURIComponent(entityID)}/command`, options);
        pending = true;
        control.title = 'Warte auf Bestätigung ...';
        this.commandStates[entityID] = '';
        this.schedulePendingTimeout(control, entityID, result.pending_deadline, originalTitle);
      } catch (error) {
        control.title = error.message;
        this.commandStates[entityID] = `Fehler: ${error.message}`;
      } finally {
        if (!pending) {
          control.disabled = false;
          control.removeAttribute('aria-busy');
          window.setTimeout(() => {
            if (control.isConnected) control.title = originalTitle;
          }, 2500);
        }
      }
    },

    // The authoritative resolution (success or timeout) comes from the
    // server via the next live-update refresh, which re-renders the control
    // from EntityView.Pending/LastCommandResult. This local timer is only a
    // fallback for when that refresh doesn't arrive in time - e.g. a
    // backgrounded tab, where refreshLiveFragment() is a no-op (see
    // dashboard.js) - so the control doesn't stay stuck disabled forever.
    // control.disabled still being true at the deadline is how it knows a
    // refresh hasn't already resolved (and possibly replaced) this control.
    schedulePendingTimeout(control, entityID, deadlineISO, originalTitle) {
      const deadline = deadlineISO ? Date.parse(deadlineISO) : NaN;
      const delay = (Number.isFinite(deadline) ? Math.max(0, deadline - Date.now()) : 10000) + 250;
      window.setTimeout(() => {
        if (!control.isConnected || !control.disabled) return;
        control.disabled = false;
        control.removeAttribute('aria-busy');
        control.title = originalTitle;
        this.commandStates[entityID] = 'Zeitüberschreitung – keine Bestätigung erhalten, vorheriger Wert bleibt bestehen.';
      }, delay);
    },

    sendNumberCommand(control) {
      return this.sendCommand({currentTarget: control}, 'number');
    },

    // Used by the JSON-driven control dialog (devices.html), where entity
    // state is reactive: unlike the server-rendered tile fragment, it needs
    // no template-side x-init to pick up EntityView.Pending/LastCommandResult
    // after a live-update refresh - reading them here on every re-render is
    // enough. commandStates still wins while it holds a transient sending/
    // error message.
    commandStatusText(entity) {
      const explicit = this.commandStates[entity.unique_id];
      if (explicit) return explicit;
      if (entity.pending) return 'Warte auf Bestätigung über MQTT ...';
      if (entity.last_command_result === 'timeout') return 'Zeitüberschreitung – keine Bestätigung erhalten, vorheriger Wert bleibt bestehen.';
      return '';
    },
  });
})();

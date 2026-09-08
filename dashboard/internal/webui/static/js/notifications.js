// Automations-Poller: liest das letzte {at, message}-Ereignis der Automation
// aus /api/v1/automation/notification und schiebt neue Ereignisse in den
// Toast-Store. Die Darstellung liegt in notify.js und base.html - diese Datei
// baut kein DOM. Der Endpunkt liefert das Dokument bereits geparst aus dem
// last_event-State-Topic (lastStateMessage), nicht aus dem last_message-Slot
// der Einzelgeraete-Route: den teilt sich der State-Payload mit dem
// Availability-Heartbeat, der ihn ueberschreiben konnte - dann schlug hier das
// JSON.parse bis zum naechsten echten Ereignis fehl.
(() => {
  const STORAGE_KEY = 'automation-last-event-seen-at';
  const POLL_INTERVAL_MS = 10000;

  const requestJSON = async (url) => {
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`);
    // 404 heisst hier: diese Instanz hat keinen Automations-Dienst - das ist
    // kein Fehler, sondern ein Dauerzustand. 204: der Dienst ist da, hat aber
    // noch kein Ereignis veroeffentlicht. Beide enden still.
    if (response.status === 404 || response.status === 204) return null;
    if (!response.ok) throw new Error('Anfrage fehlgeschlagen');
    return response.json();
  };

  function lastSeenAt() {
    return Number(localStorage.getItem(STORAGE_KEY) || 0);
  }

  function markSeen(at) {
    localStorage.setItem(STORAGE_KEY, String(at));
  }

  function severityFromMessage(message) {
    if (message.startsWith('critical:')) return 'critical';
    if (message.startsWith('warning:')) return 'warning';
    return 'info';
  }

  async function poll() {
    if (document.visibilityState !== 'visible') return;
    try {
      const payload = await requestJSON('/api/v1/automation/notification');
      if (!payload || !payload.message || !(payload.at > lastSeenAt())) return;
      window.Alpine.store('toasts').push(payload.message, severityFromMessage(payload.message));
      markSeen(payload.at);
    } catch (error) {
      // transient fetch/parse error - retried on the next tick, nothing to surface to the user
    }
  }

  // Erst ab alpine:init: davor gibt es den Store noch nicht, und der erste
  // poll() lief bisher noch vor dem defer-geladenen Alpine.
  document.addEventListener('alpine:init', () => {
    setInterval(poll, POLL_INTERVAL_MS);
    poll();
  });

  window.__automationNotifications = { severityFromMessage, poll };
})();

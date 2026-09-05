// Automations-Poller: liest das last_event der Automation aus
// /api/v1/devices/automation und schiebt neue Ereignisse in den Toast-Store.
// Die Darstellung liegt in notify.js und base.html - diese Datei baut kein
// DOM mehr. Bewusst die Einzelgeraete-Route und nicht die Liste: die wiegt am
// Live-System 448 KB gegen 10,9 KB, alle 10 Sekunden, auf jedem offenen Tab.
(() => {
  const STORAGE_KEY = 'automation-last-event-seen-at';
  const POLL_INTERVAL_MS = 10000;

  const requestJSON = async (url) => {
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`);
    // 404 heisst hier: diese Instanz hat keinen Automations-Dienst. Das ist
    // kein Fehler, sondern ein Dauerzustand - vorher lieferte das find() ueber
    // die Geraeteliste in dem Fall undefined und poll() stieg still aus.
    // Ohne diesen Zweig wuerde der Poller alle 10 s ins Leere feuern.
    if (response.status === 404) return null;
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
      const device = await requestJSON('/api/v1/devices/automation');
      // Der Geraetename "automation" war schon vorher fest verdrahtet (im
      // find() darunter) - er wandert hier nur in den Pfad.
      const lastEventEntity = device && (device.entities || []).find((entity) => entity.object_id === 'last_event');
      // .value ist der value_template-reduzierte Nachrichtentext (kein JSON);
      // das volle {at, message}-Dokument steckt im rohen, ungetemplateten
      // last_message.payload - siehe automations.page.js loadRuntimeState().
      const rawDocument = lastEventEntity && lastEventEntity.last_message && lastEventEntity.last_message.payload;
      if (!rawDocument) return;
      const payload = JSON.parse(rawDocument);
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

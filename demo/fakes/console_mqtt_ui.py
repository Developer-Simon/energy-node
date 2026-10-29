"""Oberflaeche des Reiters MQTT-Broker (HTML-Fragment fuer die Fake-Konsole).

Wie das Companion-Fragment: Bausteine der Seite (Gruppen, Zeilen, Schalter,
Segmente), nur Tabellenregeln als eigenes Stylesheet. Das Skript setzt
window.tabs.mqtt = {render}. Aktionen laufen ueber window.konsole.post.
Listen werden nur neu aufgebaut, wenn sich ihr Inhalt geaendert hat und kein
Feld darin den Fokus hat.
"""

MQTT_HTML = """<div id="tab-mqtt">
<style>
#tab-mqtt .mq-field { flex: 1; min-width: 10em; }
</style>

<div class="panel-head">
  <h2>MQTT-Broker</h2>
  <p class="panel-meta" data-meta></p>
</div>

<div class="grp" data-group="simulate">
  <h3>Simulation</h3>
  <div class="list">
    <div class="row">Leistungswerte bewegen
      <label class="switch"><input type="checkbox" data-simulate aria-label="Leistungswerte bewegen"><span></span></label>
    </div>
  </div>
  <p class="note">Sinuskurven um die Fixture-Werte für PV, Netz, Batterie und Hauslast.</p>
  <p class="grp-err"></p>
</div>

<div class="grp" data-group="devices">
  <h3>Geräte</h3>
  <div class="list" data-devices></div>
  <p class="note">Aus meldet das Gerät über sein Verfügbarkeits-Topic als nicht verfügbar.</p>
  <p class="grp-err"></p>
</div>

<div class="grp" data-group="services">
  <h3>Dienste</h3>
  <div class="list" data-services></div>
  <p class="note">Ablehnen meldet die Konfiguration als abgelehnt. Schweigen stoppt Status und Herzschlag, das Dashboard meldet den Dienst nach seiner Frist als inaktiv.</p>
  <p class="grp-err"></p>
</div>

<div class="grp" data-group="publish">
  <h3>Nachricht senden</h3>
  <div class="list">
    <div class="row">
      <input type="text" class="mq-field" data-topic list="mq-topics" placeholder="Topic" autocomplete="off" spellcheck="false" aria-label="Topic">
      <datalist id="mq-topics" data-topics></datalist>
      <span class="v">retained</span>
      <label class="switch"><input type="checkbox" data-retain aria-label="retained"><span></span></label>
    </div>
    <div class="row">
      <input type="text" class="mq-field mono" data-payload placeholder="Nutzlast" autocomplete="off" spellcheck="false" aria-label="Nutzlast">
      <span class="end"><button type="button" class="btn btn-primary" data-act="publish">Senden</button></span>
    </div>
  </div>
  <p class="grp-err"></p>
</div>

<div class="grp" data-group="connections">
  <h3>Verbindungen</h3>
  <div class="list" data-connections></div>
  <div class="row"><button type="button" class="btn btn-danger" data-act="disconnect">Alle trennen</button></div>
  <p class="note">Wirkt wie ein Neustart des Brokers, die Clients verbinden sich neu.</p>
  <p class="grp-err"></p>
</div>
</div>
<script>
(function () {
  const root = document.getElementById("tab-mqtt");
  if (!root) return;

  const $ = (sel) => root.querySelector(sel);
  const esc = (v) => window.konsole.esc(v);
  const setUnlessFocused = (el, value) => window.konsole.setUnlessFocused(el, value);

  const keys = {};

  // Eine Liste mit Fokus in einem Eingabefeld wird nicht neu aufgebaut
  const editing = (container) => {
    const a = document.activeElement;
    return !!a && container.contains(a) && (a.tagName === "INPUT" || a.tagName === "SELECT");
  };

  function rebuild(name, container, items, html) {
    const key = JSON.stringify(items);
    if (keys[name] === key || editing(container)) return;
    keys[name] = key;
    container.innerHTML = html(items);
  }

  function clock(epoch) {
    const d = new Date(epoch * 1000);
    return String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
  }

  function deviceRow(d) {
    return '<div class="row"><span class="dot ' + (d.online ? "lvl-ok" : "lvl-off") + '"></span>' +
      esc(d.name) + '<span class="v">' + (d.online ? "online" : "offline") + '</span>' +
      '<label class="switch"><input type="checkbox" data-device="' + esc(d.topic) + '"' +
      (d.online ? " checked" : "") + ' aria-label="' + esc(d.name) + '"><span></span></label></div>';
  }

  const STATUS = {
    ok: { text: "angewendet", pill: "pill", dot: "lvl-ok" },
    rejected: { text: "abgelehnt", pill: "pill pill-warn", dot: "lvl-error" },
    silent: { text: "schweigt", pill: "pill pill-warn", dot: "lvl-changed" },
  };
  const MODES = [["accept", "Annehmen"], ["reject", "Ablehnen"], ["silent", "Schweigen"]];

  function serviceRow(s) {
    const status = STATUS[s.status] || STATUS.ok;
    const segs = MODES.map(([value, label]) =>
      '<button type="button" data-act="service" data-value="' + value + '" aria-pressed="' +
      (s.mode === value ? "true" : "false") + '">' + label + '</button>'
    ).join("");
    return '<div class="row"><span class="dot ' + status.dot + '"></span>' + esc(s.config) +
      ' <span class="' + status.pill + '">' + status.text + '</span>' +
      '<span class="end"><span class="seg" data-service="' + esc(s.config) + '">' + segs + '</span></span></div>';
  }

  function connectionRow(c) {
    return '<div class="row">' + esc(c.client_id) + '<span class="v">seit ' + clock(c.since) + '</span></div>';
  }

  function renderDevices(devices) {
    rebuild("devices", $("[data-devices]"), devices, (list) => list.map(deviceRow).join(""));
  }

  function renderServices(services) {
    rebuild("services", $("[data-services]"), services, (list) =>
      list.map(serviceRow).join("") || '<div class="row">Noch keine Konfiguration gesehen.</div>');
  }

  function renderConnections(connections) {
    rebuild("connections", $("[data-connections]"), connections, (list) => list.map(connectionRow).join(""));
  }

  function renderTopics(names) {
    rebuild("topics", $("[data-topics]"), names, (list) =>
      list.map((name) => '<option value="' + esc(name) + '">').join(""));
  }

  function render(state) {
    if (state.reachable === false) {
      $("[data-meta]").textContent = "nicht erreichbar";
      return;
    }
    $("[data-meta]").textContent = "Port " + state.port + ", " + state.connections.length +
      " Verbindungen, " + state.topics + " Topics";
    setUnlessFocused($("[data-simulate]"), state.simulate);
    renderDevices(state.devices);
    renderServices(state.services);
    renderTopics(state.topic_names);
    renderConnections(state.connections);
  }

  function bodyFor(btn) {
    const act = btn.dataset.act;
    if (act === "service") return { config: btn.closest("[data-service]").dataset.service, mode: btn.dataset.value };
    if (act === "publish") {
      return {
        topic: $("[data-topic]").value,
        payload: $("[data-payload]").value,
        retain: $("[data-retain]").checked,
      };
    }
    return {};
  }

  root.addEventListener("click", (event) => {
    const btn = event.target.closest("[data-act]");
    if (!btn || !root.contains(btn)) return;
    window.konsole.post("mqtt", btn.dataset.act, bodyFor(btn), btn);
  });

  root.addEventListener("change", (event) => {
    const el = event.target;
    if (el.matches("[data-simulate]")) {
      window.konsole.post("mqtt", "simulate", { on: el.checked }, el);
    } else if (el.matches("[data-device]")) {
      window.konsole.post("mqtt", "device", { topic: el.dataset.device, online: el.checked }, el);
    }
  });

  window.tabs = window.tabs || {};
  window.tabs.mqtt = { render: render };
})();
</script>
</div>
"""

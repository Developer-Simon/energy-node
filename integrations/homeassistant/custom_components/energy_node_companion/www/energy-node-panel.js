// Panel in der Seitenleiste von Home Assistant: holt eine Panel-Sitzung und
// bettet das Dashboard ueber den Proxy der Integration ein. Der iframe liegt
// damit auf derselben Origin wie Home Assistant.
// Bei jeder Aenderung PANEL_JS_VERSION in const.py hochzaehlen.

// PANEL_SESSION_TTL_S in const.py ist 2 h, erneuert wird lange vorher.
const REFRESH_MS = 30 * 60 * 1000;

const TEXTS = {
  de: { unreachable: 'Das Dashboard ist gerade nicht erreichbar.', forbidden: 'Du darfst das Dashboard nicht öffnen.' },
  en: { unreachable: 'The dashboard cannot be reached right now.', forbidden: 'You are not allowed to open the dashboard.' },
};

class EnergyNodePanel extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' });
    this.shadowRoot.innerHTML = `
      <style>
        :host { display: flex; flex-direction: column; height: 100%; background: var(--primary-background-color); }
        .toolbar { display: flex; align-items: center; gap: 12px; height: var(--header-height, 56px); padding: 0 12px;
          background: var(--app-header-background-color); color: var(--app-header-text-color, #fff); font-size: 20px; }
        iframe { flex: 1; width: 100%; border: 0; }
        .message { padding: 16px; color: var(--error-color); }
      </style>
      <div class="toolbar"><ha-menu-button></ha-menu-button><span>Energy Node</span></div>
      <div class="message" hidden></div>`;
    this._frame = null;
    this._timer = null;
    this._wasConnected = true;
    this._onVisible = () => {
      if (document.visibilityState === 'visible') this._refresh(false);
    };
  }

  set hass(hass) {
    const first = !this._hass;
    this._hass = hass;
    this.shadowRoot.querySelector('ha-menu-button').hass = hass;
    // Nach einem HA-Neustart sind alle Panel-Sitzungen weg. Sobald die
    // Verbindung zurueck ist: neu ausstellen und den iframe neu laden.
    const reconnected = hass.connected && !this._wasConnected;
    this._wasConnected = hass.connected;
    if (first || reconnected) this._refresh(reconnected);
  }

  set narrow(narrow) {
    this.shadowRoot.querySelector('ha-menu-button').narrow = narrow;
  }

  set panel(panel) {
    const first = !this._panel;
    this._panel = panel;
    if (first) this._refresh(false);
  }

  connectedCallback() {
    this._timer = setInterval(() => this._refresh(false), REFRESH_MS);
    document.addEventListener('visibilitychange', this._onVisible);
  }

  disconnectedCallback() {
    clearInterval(this._timer);
    document.removeEventListener('visibilitychange', this._onVisible);
  }

  _text(key) {
    const lang = (this._hass && this._hass.language) || 'en';
    return (TEXTS[lang.split('-')[0]] || TEXTS.en)[key];
  }

  async _refresh(reload) {
    if (!this._hass || !this._panel || this._busy) return;
    this._busy = true;
    const message = this.shadowRoot.querySelector('.message');
    try {
      const session = await this._hass.callApi('POST', `energy_node_companion/panel_session/${this._panel.config.entry_id}`);
      message.hidden = true;
      if (!this._frame) {
        this._frame = document.createElement('iframe');
        this._frame.src = session.path;
        this.shadowRoot.appendChild(this._frame);
      } else if (reload) {
        this._frame.src = session.path;
      }
    } catch (err) {
      message.textContent = this._text(err && err.status_code === 403 ? 'forbidden' : 'unreachable');
      message.hidden = false;
    } finally {
      this._busy = false;
    }
  }
}

customElements.define('energy-node-panel', EnergyNodePanel);

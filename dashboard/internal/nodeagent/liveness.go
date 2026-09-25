package nodeagent

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
)

const livenessSlackSeconds = 60

type serviceState struct {
	online     bool
	lastUpdate int64 // unix seconds aus settings/status
	received   bool  // mindestens ein settings/status gesehen
	status     statusPayload
}

// statusPayload ist der Teil von settings/status, den die
// Konfigurationsseite nach dem Speichern auswertet.
type statusPayload struct {
	LastUpdate      *int64 `json:"last_update"`
	RuntimeStatus   string `json:"runtime_status"`
	Error           string `json:"error"`
	ErrorCode       string `json:"error_code"`
	ConfigRevision  string `json:"config_revision"`
	AppliedRevision string `json:"applied_revision"`
}

// ServiceRuntimeStatus ist das Ergebnis des letzten Ladeversuchs eines
// Dienstes, wie er es retained auf settings/status meldet.
type ServiceRuntimeStatus struct {
	ServiceID       string `json:"service_id"`
	Received        bool   `json:"received"`
	Online          bool   `json:"online"`
	RuntimeStatus   string `json:"runtime_status"`
	Error           string `json:"error"`
	ErrorCode       string `json:"error_code"`
	ConfigRevision  string `json:"config_revision"`
	AppliedRevision string `json:"applied_revision"`
}

type liveness struct {
	mu    sync.RWMutex
	ids   []string
	poll  map[string]appconfig.ServicePoll
	state map[string]*serviceState
}

type ServiceLiveness struct {
	ID    string `json:"id"`
	State string `json:"state"` // "active" | "configured"
}

func (a *Agent) SetServiceCatalog(ids []string, poll map[string]appconfig.ServicePoll) {
	a.live.mu.Lock()
	defer a.live.mu.Unlock()
	a.live.ids = append([]string(nil), ids...)
	a.live.poll = poll
	if a.live.state == nil {
		a.live.state = map[string]*serviceState{}
	}
	for _, id := range ids {
		if _, ok := a.live.state[id]; !ok {
			a.live.state[id] = &serviceState{}
		}
	}
}

// WatchTopicsFor liefert die Topics, die cmd/dashboard an
// mqttclient.WatchTopics uebergibt.
func (a *Agent) WatchTopicsFor() []string {
	a.live.mu.RLock()
	defer a.live.mu.RUnlock()
	topics := make([]string, 0, len(a.live.ids)*2)
	for _, id := range a.live.ids {
		topics = append(topics, "outstation/"+id+"/status/online", "outstation/"+id+"/settings/status")
	}
	return topics
}

func (a *Agent) ObserveLiveness(topic string, payload []byte) {
	parts := strings.Split(topic, "/")
	if len(parts) < 3 || parts[0] != "outstation" {
		return
	}
	id := parts[1]
	a.live.mu.Lock()
	defer a.live.mu.Unlock()
	st := a.live.state[id]
	if st == nil {
		st = &serviceState{}
		a.live.state[id] = st
	}
	switch {
	case strings.HasSuffix(topic, "/status/online"):
		st.online = strings.TrimSpace(string(payload)) == "1"
	case strings.HasSuffix(topic, "/settings/status"):
		var s statusPayload
		if json.Unmarshal(payload, &s) != nil {
			return
		}
		st.received = true
		st.status = s
		if s.LastUpdate != nil {
			st.lastUpdate = *s.LastUpdate
		}
	}
}

func (a *Agent) ServiceLiveness(now time.Time) []ServiceLiveness {
	a.live.mu.RLock()
	defer a.live.mu.RUnlock()
	out := make([]ServiceLiveness, 0, len(a.live.ids))
	for _, id := range a.live.ids {
		st := a.live.state[id]
		state := "configured"
		if st != nil && st.online && a.fresh(id, st, now) {
			state = "active"
		}
		out = append(out, ServiceLiveness{ID: id, State: state})
	}
	return out
}

func (a *Agent) fresh(id string, st *serviceState, now time.Time) bool {
	if st.lastUpdate == 0 {
		return false
	}
	p := a.live.poll[id]
	window := p.PollIntervalS * p.DiagnosticPollMultiplier
	if window <= 0 {
		window = 60
	}
	window += livenessSlackSeconds
	return now.Unix()-st.lastUpdate <= int64(window)
}

// ServiceStatus returns the last settings/status a service published.
// Received stays false until one arrived.
func (a *Agent) ServiceStatus(id string) ServiceRuntimeStatus {
	a.live.mu.RLock()
	defer a.live.mu.RUnlock()
	out := ServiceRuntimeStatus{ServiceID: id}
	st := a.live.state[id]
	if st == nil {
		return out
	}
	out.Online = st.online
	out.Received = st.received
	out.RuntimeStatus = st.status.RuntimeStatus
	out.Error = st.status.Error
	out.ErrorCode = st.status.ErrorCode
	out.ConfigRevision = st.status.ConfigRevision
	out.AppliedRevision = st.status.AppliedRevision
	return out
}

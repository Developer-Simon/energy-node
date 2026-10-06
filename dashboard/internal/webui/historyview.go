package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// historyViewCardView ist alles, was das Template "history-view-card"
// braucht. Die Aufloesung (gebunden, eigene Einstellung, verschwundene
// Sicht) passiert hier und nicht im Browser - der Client bekommt eine
// fertige Konfiguration und muss die Settings nicht selbst laden.
type historyViewCardView struct {
	DOMID      string
	Title      string // "" = Standardtitel aus dem Katalog
	Missing    bool
	ConfigJSON string
}

// historyViewConfig ist der Inhalt von data-history-view. Die Feldnamen
// entsprechen denen einer gespeicherten Sicht, damit history-chart.js'
// rangeBounds() beide gleich behandelt.
type historyViewConfig struct {
	Series     []string `json:"series"`
	RangeMode  string   `json:"range_mode"`
	RangeHours int      `json:"range_hours"`
	RangeFrom  int64    `json:"range_from,omitempty"`
	RangeTo    int64    `json:"range_to,omitempty"`
	Aggregate  string   `json:"aggregate"`
}

func historyViewCard(item settings.Item, views []settings.HistoryView) historyViewCardView {
	card := historyViewCardView{Title: item.Title}
	var config historyViewConfig
	if item.Ref != "" {
		card.Missing = true
		for _, view := range views {
			if view.ID != item.Ref {
				continue
			}
			card.Missing = false
			card.Title = view.Name
			config = historyViewConfig{Series: view.Series, RangeMode: settings.HistoryRangeModeRelative, RangeHours: view.RangeHours, Aggregate: view.Aggregate}
			if view.RangeMode == settings.HistoryRangeModeCustom && view.RangeFrom > 0 && view.RangeTo > view.RangeFrom {
				config.RangeMode = settings.HistoryRangeModeCustom
				config.RangeFrom, config.RangeTo = view.RangeFrom, view.RangeTo
			}
			break
		}
	} else {
		hours, err := strconv.Atoi(item.HistoryRangeHours)
		if err != nil || hours <= 0 {
			hours = 24
		}
		config = historyViewConfig{Series: item.HistorySeries, RangeMode: settings.HistoryRangeModeRelative, RangeHours: hours, Aggregate: item.HistoryAggregate}
	}
	if config.Series == nil {
		config.Series = []string{}
	}
	if config.Aggregate == "" {
		config.Aggregate = "avg"
	}
	data, _ := json.Marshal(config)
	card.ConfigJSON = string(data)
	// Die ID ist der Schalter fuer hx-preserve: gleiche ID heisst "alten
	// Knoten samt Apex-Instanz behalten" - und zwar mit seinen alten
	// Attributen. Jede Aenderung an Titel, Zustand oder Konfiguration muss
	// deshalb eine neue ID ergeben. Reines Hex, weil Item-IDs Doppelpunkte
	// und Punkte enthalten koennen.
	sum := sha256.Sum256([]byte(item.ID + "\x00" + card.Title + "\x00" + strconv.FormatBool(card.Missing) + "\x00" + card.ConfigJSON))
	card.DOMID = "hv-" + hex.EncodeToString(sum[:6])
	return card
}

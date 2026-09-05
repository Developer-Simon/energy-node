package registry

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

type valueTemplateCase struct {
	Name     string `json:"name"`
	Template string `json:"template"`
	Payload  string `json:"payload"`
	Expect   string `json:"expect"`
	TZ       string `json:"tz,omitempty"`
}

// Die Fixture haelt Go und die Python-Entsprechung in
// src/automation/ha_template.py zusammen - dieselbe Konstruktion, mit der
// internal/energy/testdata/balance-cases.json Go und JS zusammenhaelt. Fehlt
// die Datei, schlaegt der Test fehl, statt still zu ueberspringen.
func TestExtractValueMatchesTheSharedFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/value-template-cases.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var cases []valueTemplateCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture has no cases")
	}

	// timestamp_local haengt an time.Local. Beide Seiten nageln dieselbe Zone
	// fest; Europe/Berlin und nicht UTC, weil Gos RFC3339 UTC als "Z" schreibt
	// und Pythons isoformat() als "+00:00".
	original := time.Local
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("loading Europe/Berlin: %v", err)
	}
	t.Cleanup(func() { time.Local = original })

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if c.TZ != "" {
				if c.TZ != "Europe/Berlin" {
					t.Fatalf("fixture case uses unsupported tz %q", c.TZ)
				}
				time.Local = berlin
			} else {
				time.Local = original
			}
			got := extractValue([]byte(c.Payload), c.Template)
			if got != c.Expect {
				t.Fatalf("extractValue(%q, %q) = %q, want %q", c.Payload, c.Template, got, c.Expect)
			}
		})
	}
}

func TestEntityViewReportsTemplateSupport(t *testing.T) {
	cases := []struct {
		name     string
		template string
		want     bool
	}{
		{"leeres Template ist lesbar", "", true},
		{"flaches Feld", "{{ value_json.soc }}", true},
		{"verschachtelter Pfad", "{{ value_json.bank['soc'] }}", true},
		{"Bool-Form", "{{ 'ON' if value_json.flag else 'OFF' }}", true},
		{"mit Filter", "{{ value_json.ts | int }}", true},
		{"unlesbar", "{{ value_json.a.b.c }}", false},
		{"kein Template-Ausdruck", "soc", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reg := New()
			reg.UpsertEntity(Discovery{
				Device: DeviceInfo{ID: "bms", Name: "BMS"},
				Entity: EntityInfo{UniqueID: "soc", ObjectID: "soc", Name: "SoC", ValueTemplate: c.template},
			})
			views := reg.Snapshot()
			if len(views) != 1 || len(views[0].Entities) != 1 {
				t.Fatalf("unexpected snapshot: %#v", views)
			}
			if got := views[0].Entities[0].TemplateSupported; got != c.want {
				t.Fatalf("TemplateSupported = %v, want %v (template %q)", got, c.want, c.template)
			}
		})
	}
}

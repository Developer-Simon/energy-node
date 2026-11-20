package learnrec

import (
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestPowerOf(t *testing.T) {
	tests := []struct {
		name   string
		entity registry.EntityView
		want   float64
		wantOK bool
	}{
		{"plain watts", registry.EntityView{Value: "120", HasValue: true, UnitOfMeasurement: "W"}, 120, true},
		{"absolute value", registry.EntityView{Value: "-35.5", HasValue: true, UnitOfMeasurement: "W"}, 35.5, true},
		{"kilowatts converted", registry.EntityView{Value: "1.5", HasValue: true, UnitOfMeasurement: "kW"}, 1500, true},
		{"stale dropped", registry.EntityView{Value: "100", HasValue: true, UnitOfMeasurement: "W", Stale: true}, 0, false},
		{"unavailable dropped", registry.EntityView{Value: "100", HasValue: true, UnitOfMeasurement: "W", HasAvailability: true, Available: false}, 0, false},
		{"no value dropped", registry.EntityView{Value: "100", HasValue: false, UnitOfMeasurement: "W"}, 0, false},
		{"wrong unit dropped", registry.EntityView{Value: "100", HasValue: true, UnitOfMeasurement: "%"}, 0, false},
		{"unparsable dropped", registry.EntityView{Value: "abc", HasValue: true, UnitOfMeasurement: "W"}, 0, false},
		{"NaN dropped", registry.EntityView{Value: "NaN", HasValue: true, UnitOfMeasurement: "W"}, 0, false},
		{"Inf dropped", registry.EntityView{Value: "+Inf", HasValue: true, UnitOfMeasurement: "W"}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := PowerOf(tt.entity)
			if ok != tt.wantOK {
				t.Fatalf("PowerOf ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("PowerOf = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSwitchOf(t *testing.T) {
	tests := []struct {
		name   string
		entity registry.EntityView
		wantOn bool
		wantOK bool
	}{
		{"switch on by payload", registry.EntityView{Component: "switch", HasValue: true, Value: "ON", PayloadOn: "ON", PayloadOff: "OFF"}, true, true},
		{"switch off by payload", registry.EntityView{Component: "switch", HasValue: true, Value: "OFF", PayloadOn: "ON", PayloadOff: "OFF"}, false, true},
		{"light on fallback word", registry.EntityView{Component: "light", HasValue: true, Value: "On"}, true, true},
		{"fan off fallback word", registry.EntityView{Component: "fan", HasValue: true, Value: "off"}, false, true},
		{"true is on", registry.EntityView{Component: "switch", HasValue: true, Value: "TRUE"}, true, true},
		{"1 is on", registry.EntityView{Component: "switch", HasValue: true, Value: "1"}, true, true},
		{"0 is off", registry.EntityView{Component: "switch", HasValue: true, Value: "0"}, false, true},
		{"false is off", registry.EntityView{Component: "switch", HasValue: true, Value: "False"}, false, true},
		{"unknown value", registry.EntityView{Component: "switch", HasValue: true, Value: "maybe"}, false, false},
		{"no value", registry.EntityView{Component: "switch", HasValue: false, Value: "on"}, false, false},
		{"sensor ignored", registry.EntityView{Component: "sensor", HasValue: true, Value: "on"}, false, false},
		{"binary sensor ignored", registry.EntityView{Component: "binary_sensor", HasValue: true, Value: "on"}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			on, ok := SwitchOf(tt.entity)
			if on != tt.wantOn || ok != tt.wantOK {
				t.Errorf("SwitchOf = (%v, %v), want (%v, %v)", on, ok, tt.wantOn, tt.wantOK)
			}
		})
	}
}

package performance

import (
	"strings"
	"testing"
	"time"
)

// testAttribute bildet die Zuordnung ab, die der Monitor liefern wird:
// outstation/<id>/ ist ein Dienst, plug_balkon haengt am Dienst shelly,
// alles andere ist ein fremdes Geraet.
func testAttribute(topic string) (string, string) {
	parts := strings.Split(topic, "/")
	if len(parts) >= 2 && parts[0] == "outstation" {
		if parts[1] == "plug_balkon" {
			return "shelly", ""
		}
		return parts[1], ""
	}
	if len(parts) >= 2 {
		return "", parts[1]
	}
	return "", ""
}

func TestCounterAttributesServiceAndExternal(t *testing.T) {
	c := NewCounter(testAttribute)
	c.SetDeviceName(func(id string) string {
		if id == "zaehler" {
			return "Zaehler Keller"
		}
		return ""
	})
	base := time.Unix(1000, 0)

	c.Observe("outstation/shelly/state", base)
	c.Observe("outstation/plug_balkon/state", base.Add(time.Second))

	// Fremdes Geraet "zaehler": Abstaende 3 s, 2 s, 4 s, Median 3 s.
	c.Observe("tasmota/zaehler/SENSOR", base)
	c.Observe("tasmota/zaehler/SENSOR", base.Add(3*time.Second))
	c.Observe("tasmota/zaehler/SENSOR", base.Add(5*time.Second))
	c.Observe("tasmota/zaehler/SENSOR", base.Add(9*time.Second))

	// Langsameres fremdes Geraet, darf nicht gewinnen.
	c.Observe("tasmota/heizung/SENSOR", base)
	c.Observe("tasmota/heizung/SENSOR", base.Add(10*time.Second))

	if got := c.Count("shelly"); got != 2 {
		t.Fatalf("Count(shelly) = %d, want 2 (plug_balkon is attributed to shelly)", got)
	}
	if got := c.Count("tasmota"); got != 0 {
		t.Fatalf("Count(tasmota) = %d, want 0 for a foreign topic", got)
	}

	ext := c.External(base)
	if ext == nil {
		t.Fatal("External() = nil, want the fastest foreign device")
	}
	if ext.Name != "Zaehler Keller" {
		t.Fatalf("External().Name = %q, want the registry name", ext.Name)
	}
	if ext.EveryS != 3 {
		t.Fatalf("External().EveryS = %v, want median 3", ext.EveryS)
	}
}

func TestCounterExternalNilWithoutIntervals(t *testing.T) {
	c := NewCounter(testAttribute)
	c.Observe("tasmota/zaehler/SENSOR", time.Unix(1000, 0))
	if got := c.External(time.Unix(1000, 0)); got != nil {
		t.Fatalf("External() = %+v, want nil with a single message", got)
	}
}

func TestCounterExternalUsesLastTwentyIntervals(t *testing.T) {
	c := NewCounter(testAttribute)
	base := time.Unix(1000, 0)
	// Erst 20 Abstaende von 10 s, dann 20 von 2 s: der Median darf nur die
	// letzten 20 sehen.
	at := base
	c.Observe("tasmota/zaehler/SENSOR", at)
	for i := 0; i < 20; i++ {
		at = at.Add(10 * time.Second)
		c.Observe("tasmota/zaehler/SENSOR", at)
	}
	for i := 0; i < 20; i++ {
		at = at.Add(2 * time.Second)
		c.Observe("tasmota/zaehler/SENSOR", at)
	}
	ext := c.External(base)
	if ext == nil || ext.EveryS != 2 {
		t.Fatalf("External() = %+v, want EveryS 2 from the last 20 intervals", ext)
	}
}

func TestCounterDropsDevicesSilentForTenMinutes(t *testing.T) {
	c := NewCounter(testAttribute)
	base := time.Unix(1000, 0)
	// Das schnelle Geraet sendet nur kurz, das langsame bleibt aktiv.
	c.Observe("tasmota/zaehler/SENSOR", base)
	c.Observe("tasmota/zaehler/SENSOR", base.Add(2*time.Second))
	c.Observe("tasmota/heizung/SENSOR", base)
	c.Observe("tasmota/heizung/SENSOR", base.Add(10*time.Second))
	c.Observe("tasmota/heizung/SENSOR", base.Add(20*time.Second))

	if ext := c.External(base.Add(20 * time.Second)); ext == nil || ext.EveryS != 2 {
		t.Fatalf("External() = %+v, want the fast device while it sends", ext)
	}

	// Elf Minuten nach der letzten Nachricht des schnellen Geraets: es ist weg,
	// das heizung-Geraet mit 10 s Abstand bleibt.
	later := base.Add(2*time.Second + 11*time.Minute)
	c.Observe("tasmota/heizung/SENSOR", later.Add(-10*time.Second))
	c.Observe("tasmota/heizung/SENSOR", later)
	ext := c.External(later)
	if ext == nil || ext.EveryS != 10 {
		t.Fatalf("External() = %+v, want the still-active device after the fast one went silent", ext)
	}
	if _, ok := c.devices["zaehler"]; ok {
		t.Fatal("silent device still in the counter map after Observe")
	}

	// Nur das stille Geraet: nach zehn Minuten meldet External nichts mehr.
	quiet := NewCounter(testAttribute)
	quiet.Observe("tasmota/zaehler/SENSOR", base)
	quiet.Observe("tasmota/zaehler/SENSOR", base.Add(3*time.Second))
	if got := quiet.External(base.Add(10*time.Minute + 3*time.Second)); got == nil {
		t.Fatal("External() = nil at exactly 10 minutes, want the device still reported")
	}
	if got := quiet.External(base.Add(10*time.Minute + 4*time.Second)); got != nil {
		t.Fatalf("External() = %+v, want nil once the only device has been silent for over 10 minutes", got)
	}
}
